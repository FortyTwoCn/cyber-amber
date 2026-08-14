package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	commandparser "github.com/FortyTwoCn/cyber-amber/internal/command/parser"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/oklog/ulid/v2"
)

type Ingestor struct {
	store       *store.Store
	defaults    commandparser.Defaults
	limits      commandparser.Limits
	maxAttempts int
	botMID      int64
	quota       Quota
}

type Quota struct {
	PerMIDDay, Concurrent, PerVideoHour int
	Cooldown                            time.Duration
}

func NewIngestor(store *store.Store, defaults commandparser.Defaults, limits commandparser.Limits, maxAttempts int, botMID int64, quota Quota) *Ingestor {
	if quota.PerMIDDay < 1 {
		quota.PerMIDDay = 20
	}
	if quota.Concurrent < 1 {
		quota.Concurrent = 2
	}
	if quota.PerVideoHour < 1 {
		quota.PerVideoHour = 20
	}
	if quota.Cooldown < time.Second {
		quota.Cooldown = 30 * time.Second
	}
	return &Ingestor{store: store, defaults: defaults, limits: limits, maxAttempts: maxAttempts, botMID: botMID, quota: quota}
}

func (i *Ingestor) RunOnce(ctx context.Context) error {
	mentions, err := i.store.PendingMentions(ctx, 100)
	if err != nil {
		return err
	}
	for _, mention := range mentions {
		if err := i.ingest(ctx, mention); err != nil {
			return err
		}
	}
	return nil
}
func (i *Ingestor) ingest(ctx context.Context, m store.MentionRecord) error {
	botMID := i.botMID
	if botMID == 0 {
		if account, err := i.store.LoadAccount(ctx); err == nil {
			botMID = account.MID
		}
	}
	if botMID > 0 && m.SenderMID == botMID {
		return i.store.MarkMention(ctx, m.NotificationID, "ignored_self", "")
	}
	defaults := i.defaults
	if m.URI != "" {
		defaults.Page = inferPage(m.URI, defaults.Page)
	}
	cmd, err := commandparser.Parse(m.Message, defaults, i.limits)
	if err != nil {
		var parseErr *commandparser.Error
		code := "INVALID_COMMAND"
		if errors.As(err, &parseErr) {
			code = parseErr.Code
		}
		return i.store.MarkMention(ctx, m.NotificationID, "invalid", code)
	}
	midText := strconv.FormatInt(m.SenderMID, 10)
	if blocked, err := i.store.IsBlocked(ctx, "bili_mid", midText); err != nil {
		return err
	} else if blocked {
		return i.store.MarkMention(ctx, m.NotificationID, "blocked", "USER_BLOCKED")
	}
	if active, err := i.store.ActiveJobsForMID(ctx, m.SenderMID); err != nil {
		return err
	} else if active >= i.quota.Concurrent {
		return i.store.MarkMention(ctx, m.NotificationID, "rate_limited", "USER_CONCURRENCY_LIMIT")
	}
	if allowed, _, err := i.store.TakeRateLimit(ctx, "bili_mid_daily", midText, 24*time.Hour, i.quota.PerMIDDay); err != nil {
		return err
	} else if !allowed {
		return i.store.MarkMention(ctx, m.NotificationID, "rate_limited", "MID_DAILY_LIMIT")
	}
	if allowed, _, err := i.store.TakeRateLimit(ctx, "bili_mid_cooldown", midText, i.quota.Cooldown, 1); err != nil {
		return err
	} else if !allowed {
		return i.store.MarkMention(ctx, m.NotificationID, "rate_limited", "USER_COOLDOWN")
	}
	input := m.BVID
	if input == "" {
		input = fmt.Sprintf("av%d", m.AID)
	}
	if allowed, _, err := i.store.TakeRateLimit(ctx, "bili_video_hour", input, time.Hour, i.quota.PerVideoHour); err != nil {
		return err
	} else if !allowed {
		return i.store.MarkMention(ctx, m.NotificationID, "rate_limited", "VIDEO_RATE_LIMIT")
	}
	now := time.Now().UTC()
	job := domain.Job{ID: ulid.Make().String(), Source: domain.SourceMention, CreatorMID: m.SenderMID, CreatorName: m.SenderName, NotificationID: m.NotificationID, SourceCommentRPID: m.RPID, Aid: m.AID, BVID: m.BVID, Page: cmd.Page, Start: cmd.Start, End: cmd.End, Requested: cmd.Params, Final: cmd.Params, DedupeKey: domain.MakeDedupeKey(input, 0, cmd.Start, cmd.End, cmd.Params, "v1"), Status: domain.JobQueued, MaxAttempts: i.maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := i.store.Enqueue(ctx, job); err != nil {
		if containsConstraint(err.Error()) {
			return i.store.MarkMention(ctx, m.NotificationID, "deduplicated", "")
		}
		return err
	}
	return i.store.MarkMention(ctx, m.NotificationID, "enqueued", "")
}
func inferPage(value string, fallback int) int {
	u, err := url.Parse(value)
	if err != nil {
		return fallback
	}
	page, err := strconv.Atoi(u.Query().Get("p"))
	if err != nil || page < 1 {
		return fallback
	}
	return page
}
func containsConstraint(value string) bool {
	return strings.Contains(strings.ToLower(value), "unique constraint")
}
