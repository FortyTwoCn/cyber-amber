package notifications

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/oklog/ulid/v2"
)

type Poller struct {
	source   *Source
	store    *store.Store
	interval time.Duration
	backfill bool
	maxPages int
	onError  func()
	logger   *slog.Logger
	wake     chan struct{}
}

func (p *Poller) SetErrorObserver(observer func()) { p.onError = observer }
func (p *Poller) SetLogger(logger *slog.Logger) {
	if logger != nil {
		p.logger = logger
	}
}

func NewPoller(source *Source, store *store.Store, interval time.Duration, backfill bool) *Poller {
	return &Poller{source: source, store: store, interval: interval, backfill: backfill, maxPages: 20, logger: slog.Default(), wake: make(chan struct{}, 1)}
}

// Wake requests an immediate poll after an account login or an administrator
// resume. The buffered signal also covers the small window before Run starts
// waiting, and repeated requests are intentionally coalesced.
func (p *Poller) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Poller) RunOnce(ctx context.Context) error {
	cursorRecord, err := p.store.LoadCursor(ctx, "bili_at")
	if err != nil {
		return err
	}
	previous := Cursor{ID: cursorRecord.ID, Time: cursorRecord.Time, Initialized: cursorRecord.Initialized}
	if !previous.Initialized && !p.backfill {
		page, err := p.source.Poll(ctx, Cursor{})
		if err != nil {
			return err
		}
		newest := page.Next
		if len(page.Events) > 0 {
			newest = Cursor{ID: mustInt(page.Events[0].NotificationID), Time: page.Events[0].At, Initialized: true}
		}
		newest.Initialized = true
		return p.store.SaveMentionsAndCursor(ctx, nil, store.CursorRecord{Source: "bili_at", ID: newest.ID, Time: newest.Time, Initialized: true})
	}
	events, newest, err := p.source.CollectSince(ctx, previous, p.maxPages)
	if err != nil {
		return err
	}
	records := make([]store.MentionRecord, 0, len(events))
	for _, event := range events {
		records = append(records, store.MentionRecord{ID: ulid.Make().String(), NotificationID: event.NotificationID, OccurredAt: time.Unix(event.At, 0).UTC(), SenderMID: event.SenderMID, SenderName: event.SenderName, SenderAvatar: event.SenderAvatar, Message: event.Message, SubjectID: event.SubjectID, RootID: event.RootID, SourceID: event.SourceID, TargetID: event.TargetID, BusinessType: event.BusinessType, URI: event.URI, AID: event.AID, BVID: event.BVID, RPID: event.RPID, RootRPID: event.RootRPID, RawJSON: event.RawJSON, Status: "received"})
	}
	return p.store.SaveMentionsAndCursor(ctx, records, store.CursorRecord{Source: "bili_at", ID: newest.ID, Time: newest.Time, Initialized: true})
}
func (p *Poller) Run(ctx context.Context) {
	backoff := p.interval
	for {
		if err := p.RunOnce(ctx); err != nil {
			redacted := client.Redact(err.Error())
			p.logger.Error("Bili mention poll failed", "error", redacted, "backoff", backoff)
			if storeErr := p.store.RecordCursorError(context.WithoutCancel(ctx), "bili_at", redacted); storeErr != nil {
				p.logger.Error("persist mention poll error", "error", storeErr)
			}
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.Risk {
				until := time.Now().Add(30 * time.Minute)
				if storeErr := p.store.SetBotPaused(context.WithoutCancel(ctx), true, "BILI_RISK_CONTROL", &until); storeErr != nil {
					p.logger.Error("persist Bili risk circuit", "error", storeErr)
				}
			} else if errors.As(err, &apiErr) && (apiErr.HTTPStatus == 401 || apiErr.HTTPStatus == 403 || apiErr.Code == -101 || apiErr.Code == -111) {
				if storeErr := p.store.MarkAccountInvalid(context.WithoutCancel(ctx), "BILI_AUTH_INVALID"); storeErr != nil {
					p.logger.Error("persist invalid Bili account", "error", storeErr)
				}
				if storeErr := p.store.SetBotPaused(context.WithoutCancel(ctx), true, "BILI_AUTH_INVALID", nil); storeErr != nil {
					p.logger.Error("persist Bili auth circuit", "error", storeErr)
				}
			}
			if p.onError != nil {
				p.onError()
			}
			backoff = min(backoff*2, 10*time.Minute)
		} else {
			backoff = p.interval
		}
		jitter := time.Duration(rand.Int64N(max(int64(backoff/5), 1)))
		timer := time.NewTimer(backoff + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.wake:
			timer.Stop()
			backoff = p.interval
		case <-timer.C:
		}
	}
}
