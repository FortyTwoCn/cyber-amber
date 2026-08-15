package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/auth"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/comments"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/images"
	biliinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/notifications"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/wbi"
	"github.com/FortyTwoCn/cyber-amber/internal/cleanup"
	commandparser "github.com/FortyTwoCn/cyber-amber/internal/command/parser"
	"github.com/FortyTwoCn/cyber-amber/internal/config"
	"github.com/FortyTwoCn/cyber-amber/internal/httpserver"
	"github.com/FortyTwoCn/cyber-amber/internal/httpserver/security"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffmpeg"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffprobe"
	"github.com/FortyTwoCn/cyber-amber/internal/media/fonts"
	gifrenderer "github.com/FortyTwoCn/cyber-amber/internal/media/gif"
	"github.com/FortyTwoCn/cyber-amber/internal/observability"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/FortyTwoCn/cyber-amber/internal/system"
	"github.com/FortyTwoCn/cyber-amber/internal/worker"
)

const version = "0.1.5"

func main() {
	configPath := flag.String("config", "config.yaml", "configuration file; missing default file is allowed")
	hashPassword := flag.String("hash-password", "", "use '-' to read an admin password from stdin, print its Argon2id hash, and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	healthcheck := flag.Bool("healthcheck", false, "check the local HTTP health endpoint and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *hashPassword != "" {
		if *hashPassword != "-" {
			fmt.Fprintln(os.Stderr, "-hash-password only accepts '-' so the password is not exposed in the process list")
			os.Exit(2)
		}
		password, err := readPassword(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println(hash)
		return
	}
	if *healthcheck {
		cfg, err := config.Load(*configPath)
		if err != nil {
			os.Exit(1)
		}
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get(healthcheckURL(cfg.App.Addr))
		if err != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				_ = response.Body.Close()
			}
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(2)
	}
	logger := newLogger(cfg)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func readPassword(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 1025))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	if len(data) > 1024 {
		return "", errors.New("administrator password exceeds 1024 bytes")
	}
	password := strings.TrimRight(string(data), "\r\n")
	if strings.ContainsAny(password, "\r\n\x00") {
		return "", errors.New("administrator password contains an invalid control character")
	}
	return password, nil
}

func healthcheckURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	for _, path := range []string{cfg.Paths.DataDir, cfg.Paths.CacheDir, cfg.Paths.TempDir, filepath.Dir(cfg.Database.Path)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create runtime directory %s: %w", path, err)
		}
	}
	artifactDir := filepath.Join(cfg.Paths.DataDir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return err
	}
	database, err := store.Open(ctx, cfg.Database.Path, cfg.Database.BusyTimeout)
	if err != nil {
		return err
	}
	defer database.Close()
	if raw, found, err := database.LoadSetting(ctx, config.OperationalSettingsKey); err != nil {
		return err
	} else if found {
		var settings config.OperationalSettings
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("decode persisted operational settings: %w", err)
		}
		if err := cfg.ApplyOperationalSettings(settings); err != nil {
			return fmt.Errorf("validate persisted operational settings: %w", err)
		}
		logger.Info("applied persisted operational settings", "version", settings.Version)
	}
	metrics := observability.NewMetrics(database)
	ready := httpserver.NewReadiness()
	biliClient := client.New(nil, cfg.Bili.APIBaseURL, cfg.Bili.PassportBaseURL, cfg.Bili.UserAgent)
	biliClient.SetObserver(func(operation string, status int) {
		metrics.BiliRequests.WithLabelValues(operation, observability.Outcome(status)).Inc()
	})
	accountService := auth.NewService(biliClient, database, cfg.Security.CookieEncryptionKey)
	if cfg.Bili.Cookie != "" {
		if err := accountService.Import(ctx, cfg.Bili.Cookie); err != nil {
			ready.Set("bili_account", err)
			logger.Error("import BILI_COOKIE", "error", err)
		}
	} else if err := accountService.Load(ctx); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			ready.Set("bili_account", err)
			logger.Error("load Bili account", "error", err)
		}
	}
	buvidCtx, cancelBuvid := context.WithTimeout(ctx, 10*time.Second)
	if err := biliClient.EnsureBuvid(buvidCtx); err != nil {
		logger.Warn("buvid initialization failed; continuing with API fallback", "error", err)
	}
	cancelBuvid()
	inputParser := biliinput.New(nil)
	signer := wbi.New(wbi.NavSource{Client: biliClient})
	rawResolver := video.NewResolver(inputParser, biliClient, signer)
	resolver := video.NewCachedResolver(rawResolver, database, 10*time.Minute)
	streamResolver := video.NewStreamResolver(biliClient, signer)
	dmProvider := danmaku.NewCachedProvider(danmaku.New(biliClient), database, 5*time.Minute)
	runner := ffmpeg.NewRunner(cfg.Media.FFmpegPath, cfg.Jobs.FFmpegConcurrency)
	runner.SetObserver(func(elapsed time.Duration) { metrics.FFmpegDuration.Observe(elapsed.Seconds()) })
	prober := ffprobe.New(cfg.Media.FFprobePath)
	checkCtx, cancelCheck := context.WithTimeout(ctx, 15*time.Second)
	decoders, ffmpegErr := runner.Check(checkCtx)
	probeErr := prober.Check(checkCtx)
	fontErr := fonts.Check(checkCtx, cfg.Media.FontFamily)
	cancelCheck()
	ready.Set("ffmpeg", ffmpegErr)
	ready.Set("ffprobe", probeErr)
	ready.Set("font", fontErr)
	if free, err := system.FreeBytes(cfg.Paths.DataDir); err != nil {
		ready.Set("disk", err)
	} else if free < cfg.Limits.MinFreeDiskBytes {
		ready.Set("disk", fmt.Errorf("free bytes %d below minimum %d", free, cfg.Limits.MinFreeDiskBytes))
	}
	renderer := gifrenderer.New(runner, prober)
	uploader := images.New(biliClient)
	publisher := comments.New(biliClient)
	workerManager := worker.New(worker.Config{Concurrency: cfg.Jobs.WorkerConcurrency, PublishConcurrency: cfg.Jobs.PublishConcurrency, Lease: cfg.Jobs.LeaseDuration, Timeout: cfg.Jobs.Timeout, TempDir: cfg.Paths.TempDir, ArtifactDir: artifactDir, FontFamily: cfg.Media.FontFamily, UserAgent: cfg.Bili.UserAgent, ArtifactRetention: cfg.Limits.ArtifactRetention, MaxGIFBytes: cfg.Media.MaxGIFBytes, MinFreeDiskBytes: cfg.Limits.MinFreeDiskBytes, MinFPS: cfg.Media.MinFPS, MinWidth: cfg.Media.MinWidth, MinColors: cfg.Media.MinColors, MaxPublishPerDay: cfg.Bili.MaxPublishPerDay, DryRun: cfg.Bili.DryRun, ReplyOriginal: cfg.Bili.ReplyOriginal}, database, resolver, streamResolver, dmProvider, renderer, uploader, publisher, accountService, decoders, metrics, logger)
	server, err := httpserver.New(cfg, database, resolver, accountService, metrics, ready, logger)
	if err != nil {
		return err
	}
	httpSrv := server.HTTPServer()
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var background sync.WaitGroup
	startBackground := func(run func()) {
		background.Add(1)
		go func() {
			defer background.Done()
			run()
		}()
	}
	if ffmpegErr == nil && probeErr == nil && fontErr == nil {
		startBackground(func() {
			workerManager.Run(appCtx)
		})
	} else {
		logger.Error("media worker disabled because readiness checks failed", "ffmpeg", ffmpegErr, "ffprobe", probeErr, "font", fontErr)
	}
	cleaner := cleanup.New(database, cfg.Paths.TempDir, artifactDir, cfg.Jobs.Timeout*2, time.Hour, cfg.Limits.ArtifactQuotaBytes, logger)
	startBackground(func() { cleaner.Run(appCtx) })
	ingestor := notifications.NewIngestor(database, commandparser.Defaults{FPS: cfg.Defaults.FPS, Width: cfg.Defaults.Width, Danmaku: cfg.Defaults.Danmaku, DanmakuOpacity: cfg.Defaults.DanmakuOpacity, DanmakuFontScale: cfg.Defaults.DanmakuFontScale, DanmakuDensity: cfg.Defaults.DanmakuDensity, Page: 1}, commandparser.Limits{MinFPS: cfg.Media.MinFPS, MaxFPS: cfg.Limits.MaxOutputFPS, MinWidth: cfg.Media.MinWidth, MaxWidth: cfg.Limits.MaxOutputWidth, MaxDuration: cfg.Limits.MaxClipDuration}, cfg.Jobs.MaxAttempts, 0, notifications.Quota{PerMIDDay: cfg.Limits.BiliJobsPerMIDDay, Concurrent: cfg.Limits.UserConcurrentJobs, PerVideoHour: cfg.Limits.VideoJobsPerHour, Cooldown: cfg.Limits.UserCooldown})
	startBackground(func() { runIngestor(appCtx, ingestor, logger) })
	if cfg.Bili.NotificationEnabled {
		poller := notifications.NewPoller(notifications.New(biliClient), database, cfg.Bili.PollInterval, cfg.Bili.BackfillOnFirstRun)
		poller.SetErrorObserver(metrics.NotificationErrors.Inc)
		poller.SetLogger(logger)
		server.SetMentionPollWake(poller.Wake)
		server.SetMentionPoll(poller.RunOnce)
		startBackground(func() { poller.Run(appCtx) })
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", cfg.App.Addr, "dry_run", cfg.Bili.DryRun)
		serverErrors <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		httpErr := httpSrv.Shutdown(shutdownCtx)
		backgroundErr := waitBackground(shutdownCtx, &background)
		if httpErr != nil {
			return fmt.Errorf("shutdown HTTP server: %w", httpErr)
		}
		if backgroundErr != nil {
			return backgroundErr
		}
		return ctx.Err()
	case err := <-serverErrors:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		if waitErr := waitBackground(shutdownCtx, &background); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func waitBackground(ctx context.Context, group *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("background shutdown: %w", ctx.Err())
	}
}

func runIngestor(ctx context.Context, ingestor *notifications.Ingestor, logger *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := ingestor.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("ingest mention", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.App.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	options := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(cfg.App.LogFormat, "text") {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}
