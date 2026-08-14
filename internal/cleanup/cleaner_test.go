package cleanup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

func TestCleanerEnforcesArtifactQuotaOldestFirst(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(root, "cleaner.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	artifactDir := filepath.Join(root, "artifacts")
	tempDir := filepath.Join(root, "tmp")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	now := time.Now().UTC()
	for index := 0; index < 2; index++ {
		jobID := []string{"01CLEANJOB0000000000000001", "01CLEANJOB0000000000000002"}[index]
		artifactID := []string{"01CLEANART0000000000000001", "01CLEANART0000000000000002"}[index]
		path := filepath.Join(artifactDir, artifactID+".gif")
		if err := os.WriteFile(path, []byte("GIF89a"), 0o600); err != nil {
			t.Fatal(err)
		}
		job := domain.Job{ID: jobID, Source: domain.SourceWeb, Page: 1, Start: 0, End: time.Second, Requested: params, Final: params, DedupeKey: jobID, Status: domain.JobSucceeded, MaxAttempts: 3, AvailableAt: now, CreatedAt: now.Add(time.Duration(index) * time.Second), UpdatedAt: now}
		if err := database.Enqueue(ctx, job); err != nil {
			t.Fatal(err)
		}
		artifact := domain.Artifact{ID: artifactID, JobID: jobID, Path: path, SHA256: artifactID, SizeBytes: 100, Width: 640, Height: 360, FrameCount: 10, Duration: time.Second, Requested: params, Final: params, OriginalSizeBytes: 100, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Duration(index) * time.Second)}
		if err := database.SaveArtifact(ctx, artifact); err != nil {
			t.Fatal(err)
		}
	}
	cleaner := New(database, tempDir, artifactDir, time.Hour, time.Hour, 150, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cleaner.clean(ctx)
	used, err := database.ArtifactStorageBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if used != 100 {
		t.Fatalf("quota usage=%d", used)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "01CLEANART0000000000000001.gif")); !os.IsNotExist(err) {
		t.Fatalf("oldest artifact was not evicted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "01CLEANART0000000000000002.gif")); err != nil {
		t.Fatalf("newest artifact was evicted: %v", err)
	}
}

func TestCleanupPathCannotTargetParentOrSymlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	guard := filepath.Join(parent, "guard")
	if err := os.WriteFile(guard, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeWithin(parent, root, ""); err == nil {
		t.Fatal("parent cleanup target was accepted")
	}
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("parent contents were removed: %v", err)
	}
	outside := filepath.Join(parent, "outside.gif")
	if err := os.WriteFile(outside, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.gif")
	if err := os.Symlink(outside, link); err == nil {
		if err := removeWithin(link, root, ".gif"); err == nil {
			t.Fatal("artifact symlink cleanup target was accepted")
		}
	}
	if err := removeWithin(filepath.Join(root, "already-missing.gif"), root, ".gif"); err != nil {
		t.Fatalf("missing artifact should allow database cleanup: %v", err)
	}
}

func TestCleanerRemovesOnlyOldUntrackedMediaFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(root, "orphan.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	artifacts := filepath.Join(root, "artifacts")
	temp := filepath.Join(root, "temp")
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(temp, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(artifacts, "orphan.gif")
	palette := filepath.Join(artifacts, "orphan-palette.png")
	unrelated := filepath.Join(artifacts, "keep.txt")
	for _, path := range []string{orphan, palette, unrelated} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	cleaner := New(database, temp, artifacts, time.Hour, time.Hour, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cleaner.clean(ctx)
	for _, path := range []string{orphan, palette} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("orphan was not removed: %s (%v)", path, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}
