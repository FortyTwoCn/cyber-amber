package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testJob(id string) domain.Job {
	now := time.Now().UTC()
	p := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	return domain.Job{ID: id, Source: domain.SourceWeb, Page: 1, Start: time.Second, End: 2 * time.Second, Requested: p, Final: p, DedupeKey: id, Status: domain.JobQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
}

func TestEnqueueClaimAndTransition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Enqueue(ctx, testJob("01TEST00000000000000000000")); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(ctx, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 1 || job.LeaseOwner != "worker-1" {
		t.Fatalf("unexpected claim: %#v", job)
	}
	if err := s.Transition(ctx, job.ID, "worker-1", domain.JobResolvingVideo, 5); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobResolvingVideo || got.Progress != 5 {
		t.Fatalf("unexpected job: %#v", got)
	}
}

func TestConcurrentClaimOnlyOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Enqueue(ctx, testJob("01TEST00000000000000000001")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	for _, worker := range []string{"a", "b"} {
		go func() { defer wg.Done(); _, err := s.Claim(ctx, worker, time.Minute); results <- err }()
	}
	wg.Wait()
	close(results)
	claimed, empty := 0, 0
	for err := range results {
		if err == nil {
			claimed++
		} else if errors.Is(err, ErrNotFound) || isBusy(err) {
			empty++
		} else {
			t.Fatal(err)
		}
	}
	if claimed != 1 || empty != 1 {
		t.Fatalf("claimed=%d empty=%d", claimed, empty)
	}
}

func TestLeaseRecovery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	j := testJob("01TEST00000000000000000002")
	if err := s.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(ctx, "dead", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, job.ID, "dead", domain.JobResolvingVideo, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_until=? WHERE id=?`, utcText(time.Now().Add(-time.Second)), job.ID); err != nil {
		t.Fatal(err)
	}
	count, err := s.RecoverExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered %d", count)
	}
	if _, err := s.Claim(ctx, "alive", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestClaimRecoversLeaseThatExpiredAfterRestart(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	j := testJob("01TEST00000000000000000020")
	if err := s.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(ctx, "dead", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, job.ID, "dead", domain.JobResolvingVideo, 5); err != nil {
		t.Fatal(err)
	}
	// Simulate a fast restart: the one-shot startup recovery sees a lease that
	// is still valid. Expire it explicitly so the test is deterministic even
	// under the race detector, then let the normal claim loop recover it.
	if count, err := s.RecoverExpired(ctx); err != nil || count != 0 {
		t.Fatalf("early recovery count=%d err=%v", count, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_until=? WHERE id=?`, utcText(time.Now().Add(-time.Second)), job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Claim(ctx, "alive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != job.ID || recovered.Attempts != 2 {
		t.Fatalf("unexpected recovered job %#v", recovered)
	}
	var outcome string
	if err := s.db.QueryRowContext(ctx, `SELECT outcome FROM job_attempts WHERE job_id=? AND attempt=1`, job.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "interrupted" {
		t.Fatalf("first attempt outcome=%q", outcome)
	}
}

func TestExpiredLeaseAtMaximumAttemptsFailsPermanently(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	j := testJob("01TEST00000000000000000021")
	j.MaxAttempts = 1
	if err := s.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(ctx, "dead", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, job.ID, "dead", domain.JobResolvingVideo, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_until=? WHERE id=?`, utcText(time.Now().Add(-time.Second)), job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "alive", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exhausted job was claimable: %v", err)
	}
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobFailed || got.ErrorCode != "WORKER_INTERRUPTED" {
		t.Fatalf("unexpected exhausted job %#v", got)
	}
}

func TestPausedBotDoesNotClaimMentionButWebContinues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mention := testJob("01TEST00000000000000000022")
	mention.Source = domain.SourceMention
	mention.SourceCommentRPID = 22
	web := testJob("01TEST00000000000000000023")
	web.CreatedAt = web.CreatedAt.Add(time.Millisecond)
	if err := s.Enqueue(ctx, mention); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, web); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBotPaused(ctx, true, "ADMIN_PAUSED", nil); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != web.ID {
		t.Fatalf("paused mention was claimed: %#v", claimed)
	}
}

func TestParkedClaimPreservesRetryBudget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	job := testJob("01TEST00000000000000000024")
	job.Source = domain.SourceMention
	job.SourceCommentRPID = 24
	job.MaxAttempts = 1
	if err := s.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ParkClaim(ctx, claimed.ID, "worker", "BOT_PAUSED", "paused"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBotPaused(ctx, false, "", nil); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := s.Claim(ctx, "worker-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Attempts != 2 || reclaimed.MaxAttempts != 2 {
		t.Fatalf("retry budget not preserved: %#v", reclaimed)
	}
}

func TestRateLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		allowed, count, err := s.TakeRateLimit(ctx, "ip", "127.0.0.1", time.Hour, 2)
		if err != nil {
			t.Fatal(err)
		}
		if allowed != (i <= 2) || count != i {
			t.Fatalf("i=%d allowed=%v count=%d", i, allowed, count)
		}
	}
}

func TestBlocklistAndAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour).UTC()
	if err := s.SetBlocked(ctx, "bili_mid", "42", "abuse", &expires); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.IsBlocked(ctx, "bili_mid", "42")
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	items, err := s.ListBlocked(ctx, 10)
	if err != nil || len(items) != 1 || items[0].SubjectID != "42" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := s.DeleteBlocked(ctx, "bili_mid", "42"); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit(ctx, "admin", "block_delete", "bili_mid:42", "request-1", nil); err != nil {
		t.Fatal(err)
	}
	audits, err := s.ListAudits(ctx, 10)
	if err != nil || len(audits) != 1 || audits[0].Action != "block_delete" || string(audits[0].Details) != "{}" {
		t.Fatalf("audits=%#v err=%v", audits, err)
	}
}

func TestCursorErrorPreservesPosition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SaveMentionsAndCursor(ctx, nil, CursorRecord{Source: "bili_at", ID: 99, Time: 100, Initialized: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCursorError(ctx, "bili_at", "HTTP 412"); err != nil {
		t.Fatal(err)
	}
	cursor, err := s.LoadCursor(ctx, "bili_at")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.ID != 99 || cursor.Time != 100 || !cursor.Initialized || cursor.LastError != "HTTP 412" {
		t.Fatalf("unexpected cursor %#v", cursor)
	}
}

func TestListMentionsIncludesRejectedEventsNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	older := time.Now().Add(-time.Minute).UTC()
	newer := older.Add(time.Second)
	mentions := []MentionRecord{
		{ID: "01MENTIONLIST000000000000001", NotificationID: "101", OccurredAt: older, SenderMID: 42, SenderName: "older", Message: "00:01-00:20", AID: 170001, RPID: 1001, BusinessType: "reply", RawJSON: `{}`, Status: "received"},
		{ID: "01MENTIONLIST000000000000002", NotificationID: "102", OccurredAt: newer, SenderMID: 43, SenderName: "newer", Message: "00:01-00:10", BVID: "BV17x411w7KC", RPID: 1002, RawJSON: `{}`, Status: "received"},
	}
	if err := s.SaveMentionsAndCursor(ctx, mentions, CursorRecord{Source: "bili_at", ID: 102, Time: newer.Unix(), Initialized: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkMention(ctx, "101", "invalid", "CLIP_TOO_LONG"); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListMentions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].NotificationID != "102" || items[1].BusinessType != "reply" || items[1].ErrorCode != "CLIP_TOO_LONG" || items[1].Status != "invalid" {
		t.Fatalf("unexpected mentions %#v", items)
	}
}

func TestClearCaches(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, table := range []string{"video_cache", "danmaku_cache"} {
		if err := s.CacheSet(ctx, table, "key", []byte("value"), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ClearCaches(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"video_cache", "danmaku_cache"} {
		if _, found, err := s.CacheGet(ctx, table, "key"); err != nil || found {
			t.Fatalf("table=%s found=%v err=%v", table, found, err)
		}
	}
}

func TestReusableArtifactCanServeDistinctDeliveryJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := testJob("01TEST00000000000000000030")
	first.Source = domain.SourceMention
	first.SourceCommentRPID = 98
	first.DedupeKey = "same-render"
	if err := s.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.gif")
	if err := os.WriteFile(artifactPath, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC()
	artifact := domain.Artifact{ID: "01ARTIFACT0000000000000000", JobID: first.ID, Path: artifactPath, SHA256: "abc", SizeBytes: 6, Width: 640, Height: 360, FrameCount: 10, Duration: time.Second, Requested: first.Requested, Final: first.Final, OriginalSizeBytes: 6, ExpiresAt: created.Add(time.Hour), CreatedAt: created}
	if err := s.SaveArtifact(ctx, artifact); err != nil {
		t.Fatal(err)
	}

	second := testJob("01TEST00000000000000000031")
	second.Source = domain.SourceMention
	second.SourceCommentRPID = 99
	second.DedupeKey = first.DedupeKey
	if err := s.Enqueue(ctx, second); err != nil {
		t.Fatalf("same render must not drop a distinct delivery job: %v", err)
	}
	claimed, err := s.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The older web job is claimed first; move it out of the queue so the
	// delivery job can be used to exercise artifact attachment.
	if claimed.ID == first.ID {
		if err := s.Transition(ctx, first.ID, "worker", domain.JobResolvingVideo, 5); err != nil {
			t.Fatal(err)
		}
		if err := s.Transition(ctx, first.ID, "worker", domain.JobFailed, 5); err != nil {
			t.Fatal(err)
		}
		claimed, err = s.Claim(ctx, "worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
	}
	if claimed.ID != second.ID {
		t.Fatalf("unexpected claimed delivery job %s", claimed.ID)
	}
	reusable, err := s.FindReusableArtifact(ctx, "same-render")
	if err != nil {
		t.Fatal(err)
	}
	if reusable.ID != artifact.ID {
		t.Fatalf("unexpected artifact %#v", reusable)
	}
	if err := s.AttachReusableArtifact(ctx, second.ID, "worker", *reusable); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJob(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactID != artifact.ID || got.ArtifactPath != artifactPath {
		t.Fatalf("artifact was not attached: %#v", got)
	}
}

func TestSettingsRoundTripAndValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SaveSetting(ctx, "runtime", []byte(`{"version":1,"fps":12}`)); err != nil {
		t.Fatal(err)
	}
	raw, found, err := s.LoadSetting(ctx, "runtime")
	if err != nil || !found || string(raw) != `{"version":1,"fps":12}` {
		t.Fatalf("raw=%s found=%v err=%v", raw, found, err)
	}
	if err := s.SaveSetting(ctx, "runtime", []byte(`not-json`)); err == nil {
		t.Fatal("expected invalid JSON setting to fail")
	}
	if _, found, err := s.LoadSetting(ctx, "missing"); err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}
}

func TestActiveWebDedupeIsDatabaseAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := testJob("01WEBDEDUPE000000000000001")
	first.DedupeKey = "active-web-dedupe"
	if err := s.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := testJob("01WEBDEDUPE000000000000002")
	second.DedupeKey = first.DedupeKey
	if err := s.Enqueue(ctx, second); err == nil {
		t.Fatal("duplicate active web job was inserted")
	}
	firstMention := testJob("01MENTIONDEDUPE00000000001")
	firstMention.Source = domain.SourceMention
	firstMention.SourceCommentRPID = 1001
	firstMention.DedupeKey = first.DedupeKey
	secondMention := testJob("01MENTIONDEDUPE00000000002")
	secondMention.Source = domain.SourceMention
	secondMention.SourceCommentRPID = 1002
	secondMention.DedupeKey = first.DedupeKey
	if err := s.Enqueue(ctx, firstMention); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, secondMention); err != nil {
		t.Fatalf("distinct mention delivery was incorrectly deduplicated: %v", err)
	}
}

func TestWebDedupeMigrationClosesPreexistingRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	ctx := context.Background()
	s, err := Open(ctx, path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP INDEX idx_jobs_active_web_dedupe; DELETE FROM schema_migrations WHERE version=3`); err != nil {
		t.Fatal(err)
	}
	first := testJob("01UPGRADEDEDUPE00000000001")
	first.DedupeKey = "upgrade-race"
	second := testJob("01UPGRADEDEDUPE00000000002")
	second.DedupeKey = first.DedupeKey
	if err := s.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	firstAfter, err := s.GetJob(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondAfter, err := s.GetJob(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstAfter.Status != domain.JobQueued || secondAfter.Status != domain.JobCancelled || secondAfter.ErrorCode != "DEDUPLICATED" {
		t.Fatalf("first=%#v second=%#v", firstAfter, secondAfter)
	}
}

func isBusy(err error) bool {
	return err != nil && !errors.Is(err, ErrNotFound) && (contains(err.Error(), "locked") || contains(err.Error(), "busy"))
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
