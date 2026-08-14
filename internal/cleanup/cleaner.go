package cleanup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

type Cleaner struct {
	store                *store.Store
	tempDir, artifactDir string
	tempMaxAge, interval time.Duration
	artifactQuotaBytes   uint64
	logger               *slog.Logger
}

func New(store *store.Store, tempDir, artifactDir string, tempMaxAge, interval time.Duration, artifactQuotaBytes uint64, logger *slog.Logger) *Cleaner {
	if interval <= 0 {
		interval = time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Cleaner{store: store, tempDir: tempDir, artifactDir: artifactDir, tempMaxAge: tempMaxAge, interval: interval, artifactQuotaBytes: artifactQuotaBytes, logger: logger}
}
func (c *Cleaner) Run(ctx context.Context) {
	c.clean(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.clean(ctx)
		}
	}
}
func (c *Cleaner) clean(ctx context.Context) {
	now := time.Now().UTC()
	items, err := c.store.ExpiredArtifacts(ctx, now, 100)
	if err != nil {
		c.logger.Error("list expired artifacts", "error", err)
	}
	for _, item := range items {
		c.removeArtifact(ctx, item, "expired")
	}
	c.enforceArtifactQuota(ctx)
	c.cleanOrphanArtifacts(ctx, now)
	if err := c.store.CleanupExpiredCaches(ctx, now); err != nil {
		c.logger.Error("clean caches", "error", err)
	}
	entries, err := os.ReadDir(c.tempDir)
	if err != nil {
		if !os.IsNotExist(err) && ctx.Err() == nil {
			c.logger.Warn("read temporary directory for cleanup", "error", err)
		}
		return
	}
	root, _ := filepath.Abs(c.tempDir)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "ca-") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err == nil && now.Sub(info.ModTime()) > c.tempMaxAge {
			if err := removeWithin(path, root, ""); err != nil {
				c.logger.Warn("remove stale temp directory", "error", err)
			}
		}
	}
}

func (c *Cleaner) cleanOrphanArtifacts(ctx context.Context, now time.Time) {
	entries, err := os.ReadDir(c.artifactDir)
	if err != nil {
		if !os.IsNotExist(err) && ctx.Err() == nil {
			c.logger.Warn("read artifact directory for orphan cleanup", "error", err)
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		isGIF := strings.HasSuffix(name, ".gif")
		isPalette := strings.HasSuffix(name, "-palette.png")
		if !isGIF && !isPalette {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= c.tempMaxAge {
			continue
		}
		path := filepath.Join(c.artifactDir, entry.Name())
		if isGIF {
			tracked, err := c.store.HasArtifactPath(ctx, path)
			if err != nil {
				c.logger.Warn("check orphan artifact", "error", err)
				continue
			}
			if tracked {
				continue
			}
		}
		suffix := ".gif"
		if isPalette {
			suffix = ".png"
		}
		if err := removeWithin(path, c.artifactDir, suffix); err != nil {
			c.logger.Warn("remove orphan media file", "error", err)
		}
	}
}

func (c *Cleaner) enforceArtifactQuota(ctx context.Context) {
	if c.artifactQuotaBytes == 0 {
		return
	}
	for {
		used, err := c.store.ArtifactStorageBytes(ctx)
		if err != nil {
			c.logger.Error("measure artifact quota", "error", err)
			return
		}
		if used <= c.artifactQuotaBytes {
			return
		}
		items, err := c.store.OldestArtifacts(ctx, 100)
		if err != nil || len(items) == 0 {
			c.logger.Error("list quota eviction artifacts", "error", err)
			return
		}
		for _, item := range items {
			if c.removeArtifact(ctx, item, "quota") {
				if item.SizeBytes > 0 && uint64(item.SizeBytes) < used {
					used -= uint64(item.SizeBytes)
				} else {
					used = 0
				}
				if used <= c.artifactQuotaBytes {
					return
				}
			}
		}
		remaining, err := c.store.ArtifactStorageBytes(ctx)
		if err != nil || remaining <= c.artifactQuotaBytes {
			return
		}
		if remaining >= used {
			c.logger.Error("artifact quota eviction made no progress", "used_bytes", remaining, "quota_bytes", c.artifactQuotaBytes)
			return
		}
	}
}

func (c *Cleaner) removeArtifact(ctx context.Context, item store.ExpiredArtifact, reason string) bool {
	if err := removeWithin(item.Path, c.artifactDir, ".gif"); err != nil {
		c.logger.Warn("remove artifact", "artifact_id", item.ID, "reason", reason, "error", err)
		return false
	}
	if err := c.store.DeleteArtifact(ctx, item.ID); err != nil {
		c.logger.Error("delete artifact row", "artifact_id", item.ID, "reason", reason, "error", err)
		return false
	}
	return true
}
func removeWithin(path, root, requiredSuffix string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(rootAbs, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || filepath.Dir(relative) != "." {
		return fmt.Errorf("cleanup path escaped allowed root")
	}
	if requiredSuffix != "" && !strings.HasSuffix(strings.ToLower(absolute), requiredSuffix) {
		return fmt.Errorf("cleanup target has unexpected suffix")
	}
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if requiredSuffix != "" {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact cleanup target is not a regular file")
		}
		return os.Remove(absolute)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("temporary cleanup target is not a directory")
	}
	return os.RemoveAll(absolute)
}
