package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
)

var eventBVID = regexp.MustCompile(`BV1[1-9A-HJ-NP-Za-km-z]{9}`)

type Source struct{ client *client.Client }

func New(client *client.Client) *Source { return &Source{client: client} }

func (s *Source) Poll(ctx context.Context, cursor Cursor) (Page, error) {
	query := url.Values{"platform": {"web"}, "build": {"0"}, "mobi_app": {"web"}}
	if cursor.ID > 0 {
		query.Set("id", strconv.FormatInt(cursor.ID, 10))
	}
	if cursor.Time > 0 {
		query.Set("at_time", strconv.FormatInt(cursor.Time, 10))
	}
	var result response
	if err := s.client.GetJSONWithPageContext(ctx, endpoints.MentionFeed, query, "https://message.bilibili.com/", "https://message.bilibili.com", &result); err != nil {
		return Page{}, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return Page{}, err
	}
	if result.Data == nil {
		return Page{}, errors.New("NOTIFICATION_STRUCTURE_CHANGED: data 缺失")
	}
	page := Page{Next: Cursor{ID: int64(result.Data.Cursor.ID), Time: int64(result.Data.Cursor.Time), Initialized: true}, End: result.Data.Cursor.IsEnd}
	for _, item := range result.Data.Items {
		event, err := decodeEvent(item)
		if err != nil {
			return Page{}, fmt.Errorf("NOTIFICATION_STRUCTURE_CHANGED: %w", err)
		}
		page.Events = append(page.Events, event)
	}
	for index := 1; index < len(page.Events); index++ {
		previous, current := page.Events[index-1], page.Events[index]
		if current.At > previous.At || (current.At == previous.At && mustInt(current.NotificationID) > mustInt(previous.NotificationID)) {
			return Page{}, errors.New("NOTIFICATION_STRUCTURE_CHANGED: 通知列表不是按时间倒序排列")
		}
	}
	return page, nil
}

func decodeEvent(item notification) (Event, error) {
	if item.ID == 0 || item.AtTime == 0 || item.User.MID == 0 {
		return Event{}, errors.New("通知缺少 id、at_time 或 user.mid")
	}
	message := item.Item.SourceContent
	if message == "" {
		message = item.Item.Content
	}
	if message == "" {
		message = item.Item.Title
	}
	if strings.TrimSpace(message) == "" {
		return Event{}, errors.New("通知缺少原评论文字")
	}
	raw, _ := json.Marshal(item)
	event := Event{NotificationID: strconv.FormatInt(int64(item.ID), 10), At: int64(item.AtTime), SenderMID: int64(item.User.MID), SenderName: item.User.Nickname, SenderAvatar: item.User.Avatar, Message: message, SubjectID: int64(item.Item.SubjectID), RootID: int64(item.Item.RootID), SourceID: int64(item.Item.SourceID), TargetID: int64(item.Item.TargetID), Business: item.Item.Business, BusinessType: string(item.Item.Type), URI: item.Item.URI, RPID: int64(item.Item.SourceID), RootRPID: int64(item.Item.RootID), Page: 1, RawJSON: string(raw)}
	seenMention := make(map[int64]struct{})
	for _, detail := range item.Item.AtDetails {
		mid := int64(detail.MID)
		if mid <= 0 {
			continue
		}
		if _, exists := seenMention[mid]; exists {
			continue
		}
		seenMention[mid] = struct{}{}
		event.MentionedMIDs = append(event.MentionedMIDs, mid)
	}
	if match := eventBVID.FindString(item.Item.URI); match != "" {
		event.BVID = match
	}
	if event.SubjectID > 0 {
		event.AID = event.SubjectID
	}
	if u, err := url.Parse(item.Item.URI); err == nil {
		if p, err := strconv.Atoi(u.Query().Get("p")); err == nil && p > 0 {
			event.Page = p
		}
		if event.AID == 0 {
			pathMatch := regexp.MustCompile(`(?:av|video/)([0-9]+)`).FindStringSubmatch(u.Path)
			if len(pathMatch) == 2 {
				event.AID, _ = strconv.ParseInt(pathMatch[1], 10, 64)
			}
		}
	}
	if event.RPID == 0 {
		return Event{}, errors.New("通知缺少 source_id（原评论 rpid）")
	}
	if event.AID == 0 && event.BVID == "" {
		return Event{}, errors.New("无法从 subject_id 或 URI 确定视频")
	}
	return event, nil
}

func (s *Source) CollectSince(ctx context.Context, previous Cursor, maxPages int) ([]Event, Cursor, error) {
	if maxPages < 1 {
		maxPages = 20
	}
	var events []Event
	next := Cursor{}
	newest := previous
	seenPrevious := false
	seenCursors := make(map[Cursor]bool)
	for pageNo := 0; pageNo < maxPages; pageNo++ {
		if seenCursors[next] {
			return nil, previous, errors.New("NOTIFICATION_STRUCTURE_CHANGED: 通知翻页游标形成循环")
		}
		seenCursors[next] = true
		page, err := s.Poll(ctx, next)
		if err != nil {
			return nil, previous, err
		}
		if pageNo == 0 && len(page.Events) > 0 {
			newest = Cursor{ID: mustInt(page.Events[0].NotificationID), Time: page.Events[0].At, Initialized: true}
		}
		for _, event := range page.Events {
			eventID := mustInt(event.NotificationID)
			if previous.Initialized && (eventID == previous.ID || event.At < previous.Time || (event.At == previous.Time && eventID <= previous.ID)) {
				seenPrevious = true
				break
			}
			events = append(events, event)
		}
		if seenPrevious || page.End || len(page.Events) == 0 {
			break
		}
		next = page.Next
		if next.ID == 0 && next.Time == 0 {
			return nil, previous, errors.New("NOTIFICATION_STRUCTURE_CHANGED: 非末页缺少翻页游标")
		}
		if pageNo == maxPages-1 {
			return nil, previous, errors.New("NOTIFICATION_BACKLOG_EXCEEDED: 通知翻页超过安全上限")
		}
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, newest, nil
}
func mustInt(value string) int64 { result, _ := strconv.ParseInt(value, 10, 64); return result }
