package notifications

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

func TestPollNotificationFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "bili", "notifications_at_redacted_variant.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	source := New(client.New(server.Client(), server.URL, server.URL, "test"))
	page, err := source.Poll(context.Background(), Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("events %d", len(page.Events))
	}
	event := page.Events[0]
	if event.SenderMID != 420000 || event.AID != 170001 || event.BVID != "BV17x411w7KC" || event.RPID != 9000000001 || event.RootRPID != 9000000000 || event.Page != 2 || len(event.MentionedMIDs) != 2 || event.MentionedMIDs[1] != 440000 {
		t.Fatalf("unexpected %#v", event)
	}
}

func TestFirstRunWithoutBackfillReadsOnlyNewestPage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":false,"id":20,"time":100},"items":[{"id":30,"at_time":103,"user":{"mid":1},"item":{"subject_id":170001,"source_id":30,"source_content":"10:10-10:11"}}]}}`)
	}))
	defer server.Close()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "poller.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	poller := NewPoller(New(client.New(server.Client(), server.URL, server.URL, "test")), database, time.Second, false)
	if err := poller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("first run fetched %d pages", calls)
	}
	mentions, err := database.PendingMentions(context.Background(), 10)
	if err != nil || len(mentions) != 0 {
		t.Fatalf("mentions=%#v err=%v", mentions, err)
	}
	cursor, err := database.LoadCursor(context.Background(), "bili_at")
	if err != nil || !cursor.Initialized || cursor.ID != 30 || cursor.Time != 103 {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
}

func TestWakeInterruptsAuthenticationBackoff(t *testing.T) {
	var calls atomic.Int32
	firstCall := make(chan struct{})
	secondCall := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(firstCall)
			_, _ = fmt.Fprint(w, `{"code":-101,"message":"账号未登录"}`)
		default:
			select {
			case <-secondCall:
			default:
				close(secondCall)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":true,"id":0,"time":0},"items":[]}}`)
		}
	}))
	defer server.Close()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wake.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	poller := NewPoller(New(client.New(server.Client(), server.URL, server.URL, "test")), database, time.Hour, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	select {
	case <-firstCall:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("initial notification poll did not run")
	}
	poller.Wake()
	select {
	case <-secondCall:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("wake did not interrupt authentication backoff")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop")
	}
}

func TestStructureDriftIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":true},"items":[{"id":1}]}}`)
	}))
	defer server.Close()
	source := New(client.New(server.Client(), server.URL, server.URL, "test"))
	if _, err := source.Poll(context.Background(), Cursor{}); err == nil {
		t.Fatal("expected structure error")
	}
}

func TestCollectReversesToChronologicalAndStopsAtCursor(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":true},"items":[{"id":3,"at_time":103,"user":{"mid":1},"item":{"subject_id":170001,"source_id":30,"source_content":"10:10-10:11"}},{"id":2,"at_time":102,"user":{"mid":1},"item":{"subject_id":170001,"source_id":20,"source_content":"10:10-10:11"}},{"id":1,"at_time":101,"user":{"mid":1},"item":{"subject_id":170001,"source_id":10,"source_content":"10:10-10:11"}}]}}`)
	}))
	defer server.Close()
	source := New(client.New(server.Client(), server.URL, server.URL, "test"))
	events, newest, err := source.CollectSince(context.Background(), Cursor{ID: 1, Time: 101, Initialized: true}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].NotificationID != "2" || events[1].NotificationID != "3" || newest.ID != 3 || calls != 1 {
		t.Fatalf("unexpected %#v newest=%#v calls=%d", events, newest, calls)
	}
}

func TestPollerPersistsOriginalNotificationJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":true},"items":[{"id":7,"at_time":103,"user":{"mid":42,"nickname":"user"},"item":{"subject_id":170001,"source_id":30,"source_content":"10:10-10:11","business":"reply"}}]}}`)
	}))
	defer server.Close()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "raw.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	poller := NewPoller(New(client.New(server.Client(), server.URL, server.URL, "test")), database, time.Second, true)
	if err := poller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := database.GetMention(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(record.RawJSON, `"source_content":"10:10-10:11"`) || strings.Contains(record.RawJSON, `"RawJSON"`) {
		t.Fatalf("not original notification JSON: %s", record.RawJSON)
	}
}

func TestNotificationOrderingDriftAndCursorLoopAreExplicit(t *testing.T) {
	unordered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":true},"items":[{"id":1,"at_time":100,"user":{"mid":1},"item":{"subject_id":170001,"source_id":10,"source_content":"00:00-00:01"}},{"id":2,"at_time":101,"user":{"mid":1},"item":{"subject_id":170001,"source_id":20,"source_content":"00:00-00:01"}}]}}`)
	}))
	defer unordered.Close()
	if _, err := New(client.New(unordered.Client(), unordered.URL, unordered.URL, "test")).Poll(context.Background(), Cursor{}); err == nil || !strings.Contains(err.Error(), "倒序") {
		t.Fatalf("unordered feed error=%v", err)
	}

	looping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"cursor":{"is_end":false,"id":42,"time":100},"items":[{"id":3,"at_time":103,"user":{"mid":1},"item":{"subject_id":170001,"source_id":30,"source_content":"00:00-00:01"}}]}}`)
	}))
	defer looping.Close()
	_, _, err := New(client.New(looping.Client(), looping.URL, looping.URL, "test")).CollectSince(context.Background(), Cursor{ID: 1, Time: 100, Initialized: true}, 5)
	if err == nil || !strings.Contains(err.Error(), "游标形成循环") {
		t.Fatalf("cursor loop error=%v", err)
	}
}
