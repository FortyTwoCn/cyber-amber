package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	gifrenderer "github.com/FortyTwoCn/cyber-amber/internal/media/gif"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

func TestScaledHeightEven(t *testing.T) {
	if got := scaledHeight(640, 1920, 1080); got != 360 {
		t.Fatalf("got %d", got)
	}
	if got := scaledHeight(854, 640, 360); got%2 != 0 {
		t.Fatalf("not even: %d", got)
	}
}

func TestCancellationWatcherCancelsRunningContext(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "worker.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	job := domain.Job{ID: "01CANCELWATCH0000000000000", Source: domain.SourceWeb, Page: 1, Start: time.Second, End: 2 * time.Second, Requested: params, Final: params, DedupeKey: "cancel-watch", Status: domain.JobQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := database.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Claim(ctx, "worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	running, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	manager := &Manager{store: database}
	go manager.watchCancellation(running, done, cancel, job.ID)
	if err := database.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-running.Done():
	case <-time.After(2 * time.Second):
		close(done)
		t.Fatal("watcher did not cancel running task")
	}
	close(done)
}

func TestCancellationDoesNotAbortAmbiguousPublishStage(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "publish-cancel.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	job := domain.Job{ID: "01PUBLISHCANCEL00000000000", Source: domain.SourceMention, SourceCommentRPID: 91, Page: 1, Start: time.Second, End: 2 * time.Second, Requested: params, Final: params, DedupeKey: "publish-cancel", Status: domain.JobQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := database.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Claim(ctx, "worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.JobStatus{domain.JobResolvingVideo, domain.JobFetchingStream, domain.JobRendering, domain.JobOptimizing, domain.JobUploading} {
		if err := database.Transition(ctx, job.ID, "worker", state, 50); err != nil {
			t.Fatal(err)
		}
	}
	running, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	manager := &Manager{store: database}
	go manager.watchCancellation(running, done, cancel, job.ID)
	if err := database.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-running.Done():
		t.Fatal("publish-stage context was cancelled")
	case <-time.After(700 * time.Millisecond):
	}
	close(done)
	cancel()
}

type backupStreamResolver struct{}

func (backupStreamResolver) Resolve(context.Context, string, int64, int64, int, map[int]bool) (*video.Stream, error) {
	return &video.Stream{Selected: video.Track{BaseURL: "https://primary.bilivideo.com/v", BackupURLs: []string{"https://backup.bilivideo.com/v"}}}, nil
}

type backupRenderer struct{ inputs []string }

func (r *backupRenderer) Render(_ context.Context, request gifrenderer.Request) (*domain.Artifact, error) {
	r.inputs = append(r.inputs, request.Input)
	if len(r.inputs) == 1 {
		return nil, errors.New("primary CDN unavailable")
	}
	path := filepath.Join(request.OutputDir, "01BACKUPARTIFACT000000000.gif")
	if err := os.WriteFile(path, []byte("GIF89a"), 0o600); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &domain.Artifact{ID: "01BACKUPARTIFACT000000000", JobID: request.JobID, Path: path, SHA256: "fixture", SizeBytes: 6, Width: request.Params.Width, Height: 360, FrameCount: 10, Duration: request.Duration, Requested: request.Params, Final: request.Params, OriginalSizeBytes: 6, ExpiresAt: now.Add(time.Hour), CreatedAt: now}, nil
}

func TestRenderFreshFallsBackToValidatedBackupURL(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "backup.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	root := t.TempDir()
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	now := time.Now().UTC()
	job := domain.Job{ID: "01BACKUPJOB000000000000000", Source: domain.SourceWeb, BVID: "BV17x411w7KC", Aid: 170001, CID: 2, Page: 1, Start: 0, End: time.Second, Requested: params, Final: params, DedupeKey: "backup", Status: domain.JobQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := database.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := database.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Transition(ctx, job.ID, "worker", domain.JobResolvingVideo, 5); err != nil {
		t.Fatal(err)
	}
	if err := database.Transition(ctx, job.ID, "worker", domain.JobFetchingStream, 20); err != nil {
		t.Fatal(err)
	}
	renderer := &backupRenderer{}
	manager := &Manager{cfg: Config{TempDir: root, ArtifactDir: root, ArtifactRetention: time.Hour, MaxGIFBytes: 1024, MinFPS: 5, MinWidth: 320, MinColors: 64}, store: database, streams: backupStreamResolver{}, renderer: renderer, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	artifact, err := manager.renderFresh(ctx, "worker", claimed, video.Page{CID: 2, Number: 1, DurationSeconds: 1, Width: 640, Height: 360})
	if err != nil {
		t.Fatal(err)
	}
	if artifact == nil || len(renderer.inputs) != 2 || renderer.inputs[1] != "https://backup.bilivideo.com/v" {
		t.Fatalf("artifact=%#v inputs=%v", artifact, renderer.inputs)
	}
}

func TestReusableArtifactPathMustBeDirectRegularGIF(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "artifact.gif")
	if err := os.WriteFile(valid, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !reusableArtifactFile(valid, root) {
		t.Fatal("valid direct GIF was rejected")
	}
	nestedDir := filepath.Join(root, "nested")
	if err := os.Mkdir(nestedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(nestedDir, "artifact.gif")
	if err := os.WriteFile(nested, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reusableArtifactFile(nested, root) || reusableArtifactFile(filepath.Join(t.TempDir(), "outside.gif"), root) {
		t.Fatal("artifact path escaped the direct artifact directory")
	}
	link := filepath.Join(root, "link.gif")
	if err := os.Symlink(valid, link); err == nil && reusableArtifactFile(link, root) {
		t.Fatal("artifact symlink was accepted")
	}
}

func TestRenderFlightCoalescesSameDedupeKey(t *testing.T) {
	manager := &Manager{renders: make(map[string]*renderFlight)}
	first, leader := manager.beginRender("same")
	if !leader {
		t.Fatal("first caller must lead")
	}
	second, leader := manager.beginRender("same")
	if leader || second != first {
		t.Fatal("second caller was not coalesced")
	}
	manager.finishRender("same", first)
	select {
	case <-second.done:
	default:
		t.Fatal("waiting render was not released")
	}
	_, leader = manager.beginRender("same")
	if !leader {
		t.Fatal("new render could not lead after completion")
	}
}
