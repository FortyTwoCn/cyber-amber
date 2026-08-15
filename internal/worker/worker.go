package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/auth"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/comments"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/images"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ass"
	gifrenderer "github.com/FortyTwoCn/cyber-amber/internal/media/gif"
	"github.com/FortyTwoCn/cyber-amber/internal/observability"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/FortyTwoCn/cyber-amber/internal/system"
)

type StreamResolver interface {
	Resolve(context.Context, string, int64, int64, int, map[int]bool) (*video.Stream, error)
}
type GIFRenderer interface {
	Render(context.Context, gifrenderer.Request) (*domain.Artifact, error)
}
type SessionProvider interface {
	Session(context.Context) (auth.Session, error)
}

type Config struct {
	Concurrency, PublishConcurrency                      int
	Lease, Timeout                                       time.Duration
	TempDir, ArtifactDir, FontDir, FontFamily, UserAgent string
	ArtifactRetention                                    time.Duration
	MaxGIFBytes                                          int64
	MinFreeDiskBytes                                     uint64
	MinFPS, MinWidth, MinColors                          int
	MaxPublishPerDay                                     int
	DryRun, ReplyOriginal                                bool
}
type Manager struct {
	cfg        Config
	store      *store.Store
	resolver   video.VideoResolver
	streams    StreamResolver
	danmaku    danmaku.Provider
	renderer   GIFRenderer
	uploader   images.Uploader
	publisher  comments.Publisher
	sessions   SessionProvider
	decoders   map[int]bool
	logger     *slog.Logger
	metrics    *observability.Metrics
	publishSem chan struct{}
	renderMu   sync.Mutex
	renders    map[string]*renderFlight
	wg         sync.WaitGroup
}

type renderFlight struct{ done chan struct{} }

type parkedError struct{ reason string }

func (e *parkedError) Error() string { return e.reason }

func New(cfg Config, store *store.Store, resolver video.VideoResolver, streams StreamResolver, danmakuProvider danmaku.Provider, renderer GIFRenderer, uploader images.Uploader, publisher comments.Publisher, sessions SessionProvider, decoders map[int]bool, metrics *observability.Metrics, logger *slog.Logger) *Manager {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.PublishConcurrency < 1 {
		cfg.PublishConcurrency = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, store: store, resolver: resolver, streams: streams, danmaku: danmakuProvider, renderer: renderer, uploader: uploader, publisher: publisher, sessions: sessions, decoders: decoders, metrics: metrics, logger: logger, publishSem: make(chan struct{}, cfg.PublishConcurrency), renders: make(map[string]*renderFlight)}
}

func (m *Manager) Run(ctx context.Context) {
	if recovered, err := m.store.RecoverExpired(ctx); err != nil {
		m.logger.Error("recover expired jobs", "error", err)
	} else if recovered > 0 {
		m.logger.Warn("recovered expired jobs", "count", recovered)
	}
	claimCtx, stopClaims := context.WithCancel(ctx)
	defer stopClaims()
	processCtx, stopProcesses := context.WithCancel(context.Background())
	defer stopProcesses()
	for index := 0; index < m.cfg.Concurrency; index++ {
		m.wg.Add(1)
		go m.loop(claimCtx, processCtx, fmt.Sprintf("worker-%d-%d", os.Getpid(), index+1))
	}
	<-ctx.Done()
	stopClaims()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	grace := 20 * time.Second
	if m.cfg.Timeout < grace {
		grace = m.cfg.Timeout
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		stopProcesses()
		<-done
	}
}

func (m *Manager) loop(claimCtx, processCtx context.Context, workerID string) {
	defer m.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-claimCtx.Done():
			return
		case <-timer.C:
		}
		job, err := m.store.Claim(claimCtx, workerID, m.cfg.Lease)
		if errors.Is(err, store.ErrNotFound) {
			timer.Reset(500 * time.Millisecond)
			continue
		}
		if err != nil {
			m.logger.Error("claim job", "worker_id", workerID, "error", err)
			timer.Reset(time.Second)
			continue
		}
		m.processClaim(processCtx, workerID, job)
		timer.Reset(10 * time.Millisecond)
	}
}

func (m *Manager) processClaim(parent context.Context, workerID string, job *domain.Job) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, m.cfg.Timeout)
	defer cancel()
	cancelDone := make(chan struct{})
	go m.watchCancellation(ctx, cancelDone, cancel, job.ID)
	defer close(cancelDone)
	leaseDone := make(chan struct{})
	go m.renewLease(ctx, leaseDone, job.ID, workerID, cancel)
	err := m.process(ctx, workerID, job)
	close(leaseDone)
	if err == nil {
		if attemptErr := m.store.CompleteAttempt(context.WithoutCancel(parent), job.ID, job.Attempts, "succeeded", "", ""); attemptErr != nil {
			m.logger.Error("complete successful job attempt", "job_id", job.ID, "error", attemptErr)
		}
		m.observeJob(job, "succeeded", started)
		return
	}
	var parked *parkedError
	if errors.As(err, &parked) {
		if parkErr := m.store.ParkClaim(context.WithoutCancel(parent), job.ID, workerID, "BOT_PAUSED", "机器人写操作已暂停，任务将在恢复后继续"); parkErr != nil {
			m.logger.Error("park paused publish job", "job_id", job.ID, "error", parkErr)
		} else {
			m.logger.Info("publish job parked while bot is paused", "job_id", job.ID, "reason", parked.reason)
		}
		return
	}
	retryable := isRetryable(err)
	code, userMessage := classifyError(err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if cancelled, _ := m.store.IsCancelRequested(context.WithoutCancel(parent), job.ID); cancelled {
			current, _ := m.store.GetJob(context.WithoutCancel(parent), job.ID)
			if current != nil && !current.Status.Terminal() {
				if transitionErr := m.store.Transition(context.WithoutCancel(parent), job.ID, workerID, domain.JobCancelled, current.Progress); transitionErr != nil {
					m.logger.Error("mark job cancelled", "job_id", job.ID, "error", transitionErr)
				}
			}
			if attemptErr := m.store.CompleteAttempt(context.WithoutCancel(parent), job.ID, job.Attempts, "cancelled", "CANCELLED", err.Error()); attemptErr != nil {
				m.logger.Error("complete cancelled job attempt", "job_id", job.ID, "error", attemptErr)
			}
			m.observeJob(job, "cancelled", started)
			return
		}
		retryable = true
		code = "INTERRUPTED"
		userMessage = "任务执行被中断，将自动重试"
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Risk {
		until := time.Now().Add(30 * time.Minute)
		if pauseErr := m.store.SetBotPaused(context.WithoutCancel(parent), true, "BILI_RISK_CONTROL", &until); pauseErr != nil {
			m.logger.Error("persist Bili risk circuit", "job_id", job.ID, "error", pauseErr)
		}
		if parkErr := m.store.ParkClaim(context.WithoutCancel(parent), job.ID, workerID, "BILI_RISK_CONTROL", "B站触发风控，任务将在机器人恢复后继续"); parkErr != nil {
			m.logger.Error("park risk-controlled publish job", "job_id", job.ID, "error", parkErr)
		}
		return
	} else if (errors.As(err, &apiErr) && isAuthenticationError(apiErr)) || code == "BILI_AUTH_INVALID" {
		authCode := code
		if apiErr != nil {
			authCode = strconv.Itoa(apiErr.Code)
		}
		if invalidErr := m.store.MarkAccountInvalid(context.WithoutCancel(parent), authCode); invalidErr != nil {
			m.logger.Error("persist invalid Bili account", "job_id", job.ID, "error", invalidErr)
		}
		if pauseErr := m.store.SetBotPaused(context.WithoutCancel(parent), true, "BILI_AUTH_INVALID", nil); pauseErr != nil {
			m.logger.Error("persist Bili auth circuit", "job_id", job.ID, "error", pauseErr)
		}
		if parkErr := m.store.ParkClaim(context.WithoutCancel(parent), job.ID, workerID, "BILI_AUTH_INVALID", "B站账号凭据已失效，任务将在重新登录后继续"); parkErr != nil {
			m.logger.Error("park authentication-blocked publish job", "job_id", job.ID, "error", parkErr)
		}
		return
	}
	diagnostic := client.Redact(err.Error())
	if failErr := m.store.Fail(context.WithoutCancel(parent), job.ID, workerID, code, userMessage, diagnostic, retryable); failErr != nil {
		m.logger.Error("persist job failure", "job_id", job.ID, "worker_id", workerID, "error", failErr)
		return
	}
	outcome := "failed"
	if current, getErr := m.store.GetJob(context.WithoutCancel(parent), job.ID); getErr == nil && current.Status == domain.JobQueued {
		outcome = "retrying"
	}
	m.observeJob(job, outcome, started)
	m.logger.Error("job failed", "job_id", job.ID, "worker_id", workerID, "code", code, "retryable", retryable, "error", diagnostic)
}

func (m *Manager) watchCancellation(ctx context.Context, done <-chan struct{}, cancel context.CancelFunc, jobID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			requested, err := m.store.IsCancelRequested(ctx, jobID)
			if err == nil && requested {
				job, getErr := m.store.GetJob(ctx, jobID)
				if getErr == nil && (job.Status == domain.JobUploading || job.Status == domain.JobPublishing || job.Status == domain.JobVerifying) {
					// Once an external write may be in flight, cancellation would
					// create an ambiguous publish outcome. Let the idempotent write
					// and verification path finish instead.
					return
				}
				cancel()
				return
			}
		}
	}
}

func (m *Manager) renewLease(ctx context.Context, done <-chan struct{}, jobID, workerID string, cancel context.CancelFunc) {
	interval := m.cfg.Lease / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := m.store.RenewLease(ctx, jobID, workerID, m.cfg.Lease); err != nil {
				select {
				case <-done:
					return
				default:
				}
				m.logger.Error("job lease renewal failed; cancelling local work", "job_id", jobID, "worker_id", workerID, "error", err)
				cancel()
				return
			}
		}
	}
}

func (m *Manager) process(ctx context.Context, workerID string, job *domain.Job) error {
	if free, err := system.FreeBytes(m.cfg.ArtifactDir); err != nil {
		return fmt.Errorf("DISK_CHECK_FAILED: %w", err)
	} else if free < m.cfg.MinFreeDiskBytes {
		return fmt.Errorf("DISK_SPACE_LOW: 可用磁盘 %d 字节，低于阈值 %d", free, m.cfg.MinFreeDiskBytes)
	}
	if err := m.stage(ctx, job.ID, workerID, domain.JobResolvingVideo, 5); err != nil {
		return err
	}
	input := job.BVID
	if input == "" {
		input = "av" + strconv.FormatInt(job.Aid, 10)
	}
	resolved, err := m.resolver.Resolve(ctx, input, job.Page)
	if err != nil {
		return err
	}
	page := resolved.Pages[resolved.SelectedPage-1]
	if job.Start < 0 || job.End <= job.Start || job.End > page.Duration() {
		return fmt.Errorf("INVALID_TIME_RANGE: 时间范围必须位于所选分P的 0-%s 内", page.Duration())
	}
	if err := m.store.UpdateJobVideo(ctx, job.ID, workerID, resolved.AID, resolved.BVID, page.CID, page.Number); err != nil {
		return err
	}
	job.Aid, job.BVID, job.CID, job.Page = resolved.AID, resolved.BVID, page.CID, page.Number
	canonicalDedupe := domain.MakeDedupeKey(job.BVID, job.CID, job.Start, job.End, job.Requested, "v1")
	if err := m.store.UpdateJobDedupeKey(ctx, job.ID, workerID, canonicalDedupe); err != nil {
		return err
	}
	job.DedupeKey = canonicalDedupe
	if err := m.stage(ctx, job.ID, workerID, domain.JobFetchingStream, 20); err != nil {
		return err
	}
	artifact, reused, err := m.obtainArtifact(ctx, workerID, job, page)
	if err != nil {
		return err
	}
	if reused {
		m.logger.Info("reused rendered artifact", "job_id", job.ID, "artifact_id", artifact.ID)
	}
	if job.Source != domain.SourceMention || m.cfg.DryRun {
		if m.cfg.DryRun && job.Source == domain.SourceMention {
			if err := m.store.SetJobPublishStatus(ctx, job.ID, "dry_run"); err != nil {
				return err
			}
		}
		return m.stage(ctx, job.ID, workerID, domain.JobSucceeded, 100)
	}
	return m.publish(ctx, workerID, job, *artifact)
}

func (m *Manager) obtainArtifact(ctx context.Context, workerID string, job *domain.Job, page video.Page) (*domain.Artifact, bool, error) {
	for {
		artifact, err := m.store.FindReusableArtifact(ctx, job.DedupeKey)
		if err == nil {
			if reusableArtifactFile(artifact.Path, m.cfg.ArtifactDir) {
				if err := m.stage(ctx, job.ID, workerID, domain.JobRendering, 70); err != nil {
					return nil, false, err
				}
				if err := m.stage(ctx, job.ID, workerID, domain.JobOptimizing, 78); err != nil {
					return nil, false, err
				}
				if err := m.store.AttachReusableArtifact(ctx, job.ID, workerID, *artifact); err != nil {
					return nil, false, err
				}
				return artifact, true, nil
			}
			// A stale row must not make every future matching task fail forever.
			_ = m.store.DeleteArtifact(context.WithoutCancel(ctx), artifact.ID)
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, false, err
		}

		flight, leader := m.beginRender(job.DedupeKey)
		if !leader {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-flight.done:
				continue
			}
		}
		artifact, err = m.renderFresh(ctx, workerID, job, page)
		m.finishRender(job.DedupeKey, flight)
		return artifact, false, err
	}
}

func reusableArtifactFile(path, artifactDir string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(artifactDir)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || filepath.IsAbs(relative) || filepath.Dir(relative) != "." || strings.HasPrefix(relative, "..") || !strings.EqualFold(filepath.Ext(relative), ".gif") {
		return false
	}
	info, err := os.Lstat(absolute)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func (m *Manager) beginRender(key string) (*renderFlight, bool) {
	m.renderMu.Lock()
	defer m.renderMu.Unlock()
	if flight := m.renders[key]; flight != nil {
		return flight, false
	}
	flight := &renderFlight{done: make(chan struct{})}
	m.renders[key] = flight
	return flight, true
}

func (m *Manager) finishRender(key string, flight *renderFlight) {
	m.renderMu.Lock()
	defer m.renderMu.Unlock()
	if m.renders[key] == flight {
		delete(m.renders, key)
		close(flight.done)
	}
}

func (m *Manager) renderFresh(ctx context.Context, workerID string, job *domain.Job, page video.Page) (*domain.Artifact, error) {
	stream, err := m.streams.Resolve(ctx, job.BVID, job.Aid, job.CID, job.Requested.Width, m.decoders)
	if err != nil {
		return nil, err
	}
	if stream.Selected.BaseURL == "" {
		return nil, errors.New("STREAM_UNAVAILABLE: 未选择到视频轨道")
	}
	tempDir, err := os.MkdirTemp(m.cfg.TempDir, "ca-"+job.ID+"-")
	if err != nil {
		return nil, fmt.Errorf("create job temp directory: %w", err)
	}
	if err := os.Chmod(tempDir, 0o700); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	if err := m.store.UpdateJobTempDir(ctx, job.ID, workerID, tempDir); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	defer func() { _ = m.store.UpdateJobTempDir(context.WithoutCancel(ctx), job.ID, workerID, "") }()
	defer os.RemoveAll(tempDir)
	assPath := ""
	sourceWidth, sourceHeight := page.Width, page.Height
	if sourceWidth <= 0 || sourceHeight <= 0 {
		sourceWidth, sourceHeight = stream.Selected.Width, stream.Selected.Height
	}
	height := scaledHeight(job.Requested.Width, sourceWidth, sourceHeight)
	if job.Requested.Danmaku {
		if err := m.stage(ctx, job.ID, workerID, domain.JobFetchingDanmaku, 35); err != nil {
			return nil, err
		}
		items, err := m.danmaku.FetchRange(ctx, job.Aid, job.CID, job.Start, job.End)
		if err != nil {
			return nil, err
		}
		content, stats, err := ass.Generate(items, job.Requested.Width, height, job.End-job.Start, ass.Style{FontFamily: m.cfg.FontFamily, FontScale: job.Requested.DanmakuFontScale, Opacity: job.Requested.DanmakuOpacity, Density: job.Requested.DanmakuDensity, Outline: 2, Reverse: true})
		if err != nil {
			return nil, err
		}
		assPath = filepath.Join(tempDir, "danmaku.ass")
		if err := os.WriteFile(assPath, content, 0o600); err != nil {
			return nil, fmt.Errorf("write ASS: %w", err)
		}
		m.logger.Info("danmaku layout", "job_id", job.ID, "input", stats.Input, "rendered", stats.Rendered, "density_drops", stats.DroppedDensity, "unsupported", stats.FilteredUnsupported)
	}
	if err := m.stage(ctx, job.ID, workerID, domain.JobRendering, 50); err != nil {
		return nil, err
	}
	urls := make([]string, 0, 1+len(stream.Selected.BackupURLs))
	urls = append(urls, stream.Selected.BaseURL)
	urls = append(urls, stream.Selected.BackupURLs...)
	var artifact *domain.Artifact
	var renderErr error
	for index, candidate := range urls {
		artifact, renderErr = m.renderer.Render(ctx, gifrenderer.Request{JobID: job.ID, Input: candidate, OutputDir: m.cfg.ArtifactDir, ASSPath: assPath, FontDir: m.cfg.FontDir, UserAgent: m.cfg.UserAgent, Referer: "https://www.bilibili.com/", Start: job.Start, Duration: job.End - job.Start, Params: job.Requested, MaxBytes: m.cfg.MaxGIFBytes, MinFPS: m.cfg.MinFPS, MinWidth: m.cfg.MinWidth, MinColors: m.cfg.MinColors, ExpiresAt: time.Now().Add(m.cfg.ArtifactRetention)})
		if renderErr == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		m.logger.Warn("video CDN candidate failed", "job_id", job.ID, "candidate_index", index, "candidate_count", len(urls), "error", renderErr)
	}
	if renderErr != nil {
		return nil, errors.New("STREAM_UNAVAILABLE: 所有安全 CDN 地址均无法完成视频读取")
	}
	artifactSaved := false
	defer func() {
		if !artifactSaved {
			_ = os.Remove(artifact.Path)
		}
	}()
	if err := m.stage(ctx, job.ID, workerID, domain.JobOptimizing, 78); err != nil {
		return nil, err
	}
	if err := m.store.SaveArtifact(ctx, *artifact); err != nil {
		return nil, err
	}
	artifactSaved = true
	if m.metrics != nil {
		m.metrics.GIFBytes.Observe(float64(artifact.SizeBytes))
	}
	return artifact, nil
}

func (m *Manager) publish(ctx context.Context, workerID string, job *domain.Job, artifact domain.Artifact) error {
	select {
	case m.publishSem <- struct{}{}:
		defer func() { <-m.publishSem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	state, err := m.store.BotState(ctx)
	if err != nil {
		return err
	}
	if state.Paused {
		if state.CircuitUntil == nil || time.Now().Before(*state.CircuitUntil) {
			return &parkedError{reason: state.CircuitReason}
		}
		if err := m.store.SetBotPaused(ctx, false, "", nil); err != nil {
			return err
		}
	}
	marker := "CA" + job.ID
	session, err := m.sessions.Session(ctx)
	if err != nil {
		return err
	}
	botMID, err := strconv.ParseInt(session.Cookies["DedeUserID"], 10, 64)
	if err != nil || botMID <= 0 {
		return errors.New("BILI_AUTH_INVALID: 账号会话缺少合法 DedeUserID")
	}
	if existing, err := m.publisher.FindTaskMarker(ctx, job.Aid, marker, botMID); err != nil {
		return err
	} else if existing != nil {
		if m.metrics != nil {
			m.metrics.BiliPublish.WithLabelValues("reconciled").Inc()
		}
		now := time.Now().UTC()
		if err := m.store.SavePublished(ctx, store.PublishedRecord{JobID: job.ID, AID: job.Aid, RPID: existing.RPID, ImageURL: existing.ImageURL, TaskMarker: marker, Status: existing.Status, VerifiedAt: &now}); err != nil {
			return err
		}
		if err := m.stage(ctx, job.ID, workerID, domain.JobUploading, 82); err != nil {
			return err
		}
		if err := m.stage(ctx, job.ID, workerID, domain.JobPublishing, 90); err != nil {
			return err
		}
		if err := m.stage(ctx, job.ID, workerID, domain.JobVerifying, 96); err != nil {
			return err
		}
		return m.stage(ctx, job.ID, workerID, domain.JobSucceeded, 100)
	}
	if err := m.stage(ctx, job.ID, workerID, domain.JobUploading, 82); err != nil {
		return err
	}
	uploaded, err := m.uploader.Upload(ctx, artifact.Path, session.CSRF())
	if err != nil {
		return err
	}
	if uploaded.Width != artifact.Width || uploaded.Height != artifact.Height {
		return errors.New("IMAGE_UPLOAD_STRUCTURE_CHANGED: 上传响应尺寸与 GIF 产物不一致")
	}
	if m.cfg.MaxPublishPerDay > 0 {
		allowed, _, err := m.store.TakeRateLimit(ctx, "bili_publish_global", "all", 24*time.Hour, m.cfg.MaxPublishPerDay)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("PUBLISH_QUOTA_EXCEEDED: B站每日评论发布额度已用尽")
		}
	}
	if err := m.stage(ctx, job.ID, workerID, domain.JobPublishing, 90); err != nil {
		return err
	}
	published, err := m.publisher.PublishRootImageComment(ctx, comments.PublishRequest{AID: job.Aid, UserMID: job.CreatorMID, Username: job.CreatorName, TaskID: marker, Start: job.Start, End: job.End, Image: *uploaded, CSRF: session.CSRF()})
	if err != nil {
		if m.metrics != nil {
			m.metrics.BiliPublish.WithLabelValues("failed").Inc()
		}
		return err
	}
	if err := m.store.SavePublished(ctx, store.PublishedRecord{JobID: job.ID, AID: job.Aid, RPID: published.RPID, ImageURL: uploaded.URL, TaskMarker: marker, Status: comments.PublishedUnverified, ResponseJSON: published.RawJSON}); err != nil {
		return err
	}
	if err := m.stage(ctx, job.ID, workerID, domain.JobVerifying, 96); err != nil {
		return err
	}
	verified, verifyErr := m.verifyRootImageComment(ctx, job.Aid, published.RPID, marker, botMID, uploaded.URL)
	status := comments.PublishedUnverified
	var verifiedAt *time.Time
	if verifyErr == nil && verified != nil {
		status = verified.Status
		now := time.Now().UTC()
		verifiedAt = &now
	} else if verifyErr == nil {
		status = comments.PendingReview
	}
	if verifyErr != nil {
		m.logger.Warn("published comment could not be verified", "job_id", job.ID, "error", client.Redact(verifyErr.Error()))
	}
	if err := m.store.SavePublished(ctx, store.PublishedRecord{JobID: job.ID, AID: job.Aid, RPID: published.RPID, ImageURL: uploaded.URL, TaskMarker: marker, Status: status, VerifiedAt: verifiedAt, ResponseJSON: published.RawJSON}); err != nil {
		return err
	}
	if status == comments.Deleted {
		until := time.Now().Add(30 * time.Minute)
		if err := m.store.SetBotPaused(ctx, true, "BILI_COMMENT_DELETED", &until); err != nil {
			return err
		}
		if m.metrics != nil {
			m.metrics.BiliPublish.WithLabelValues(status).Inc()
		}
		return errors.New("BILI_COMMENT_DELETED: B站已接受发布请求，但随后删除了带图评论；机器人写操作已暂停 30 分钟")
	}
	if m.cfg.ReplyOriginal {
		m.replyOriginal(ctx, job, marker, session.CSRF())
	}
	if m.metrics != nil {
		m.metrics.BiliPublish.WithLabelValues(status).Inc()
	}
	return m.stage(ctx, job.ID, workerID, domain.JobSucceeded, 100)
}

func (m *Manager) verifyRootImageComment(ctx context.Context, aid, rpid int64, marker string, botMID int64, imageURL string) (*comments.PublishedComment, error) {
	var last *comments.PublishedComment
	var lastErr error
	for attempt, delay := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return last, ctx.Err()
			case <-timer.C:
			}
		}
		verified, err := m.publisher.VerifyRootImageComment(ctx, aid, rpid, marker, botMID, imageURL)
		if err != nil {
			if last == nil {
				lastErr = err
			}
			continue
		}
		last, lastErr = verified, nil
		if verified != nil && verified.Status == comments.Published {
			return verified, nil
		}
		m.logger.Info("published comment not publicly visible yet", "rpid", rpid, "attempt", attempt+1, "status", verifiedStatus(verified))
	}
	if last != nil {
		return last, nil
	}
	return nil, lastErr
}

func verifiedStatus(comment *comments.PublishedComment) string {
	if comment == nil {
		return "missing"
	}
	return comment.Status
}

func (m *Manager) observeJob(job *domain.Job, status string, started time.Time) {
	if m.metrics == nil {
		return
	}
	m.metrics.JobsTotal.WithLabelValues(string(job.Source), status).Inc()
	if status == "succeeded" || status == "failed" || status == "cancelled" {
		origin := job.CreatedAt
		if origin.IsZero() {
			origin = started
		}
		m.metrics.JobDuration.Observe(time.Since(origin).Seconds())
	}
}

func (m *Manager) replyOriginal(ctx context.Context, job *domain.Job, marker, csrf string) {
	mention, err := m.store.GetMention(ctx, job.NotificationID)
	if err != nil {
		m.logger.Warn("load source mention for optional hint", "job_id", job.ID, "error", err)
		return
	}
	root := mention.RootRPID
	if root == 0 {
		root = mention.RPID
	}
	allowed, _, err := m.store.TakeRateLimit(ctx, "original_reply", strconv.FormatInt(job.CreatorMID, 10), time.Hour, 5)
	if err != nil {
		m.logger.Warn("rate-limit optional original hint", "job_id", job.ID, "error", err)
		return
	}
	if !allowed {
		return
	}
	message := fmt.Sprintf("你的赛博琥珀已生成，任务编号 %s，请查看本视频的新主楼。", marker)
	if _, err := m.publisher.ReplyOriginal(ctx, job.Aid, root, mention.RPID, message, csrf); err != nil {
		m.logger.Warn("original comment hint failed", "job_id", job.ID, "error", err)
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Risk {
			until := time.Now().Add(30 * time.Minute)
			if storeErr := m.store.SetBotPaused(context.WithoutCancel(ctx), true, "BILI_RISK_CONTROL", &until); storeErr != nil {
				m.logger.Error("persist risk circuit from original hint", "job_id", job.ID, "error", storeErr)
			}
		} else if errors.As(err, &apiErr) && isAuthenticationError(apiErr) {
			if storeErr := m.store.MarkAccountInvalid(context.WithoutCancel(ctx), strconv.Itoa(apiErr.Code)); storeErr != nil {
				m.logger.Error("persist invalid account from original hint", "job_id", job.ID, "error", storeErr)
			}
			if storeErr := m.store.SetBotPaused(context.WithoutCancel(ctx), true, "BILI_AUTH_INVALID", nil); storeErr != nil {
				m.logger.Error("persist auth circuit from original hint", "job_id", job.ID, "error", storeErr)
			}
		}
	}
}
func (m *Manager) stage(ctx context.Context, id, workerID string, status domain.JobStatus, progress int) error {
	if cancelled, err := m.store.IsCancelRequested(ctx, id); err == nil && cancelled {
		current, getErr := m.store.GetJob(ctx, id)
		if getErr == nil && current.Status != domain.JobUploading && current.Status != domain.JobPublishing && current.Status != domain.JobVerifying {
			return context.Canceled
		}
	}
	return m.store.Transition(ctx, id, workerID, status, progress)
}
func scaledHeight(width, sourceWidth, sourceHeight int) int {
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return width * 9 / 16 / 2 * 2
	}
	height := int(math.Round(float64(width) * float64(sourceHeight) / float64(sourceWidth)))
	if height%2 != 0 {
		height++
	}
	return max(height, 2)
}
func isRetryable(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return !apiErr.Permanent && !apiErr.Risk
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	text := strings.ToUpper(err.Error())
	return strings.Contains(text, "TIMEOUT") || strings.Contains(text, "TEMPORARY") || strings.Contains(text, "CONNECTION")
}

func isAuthenticationError(err *client.APIError) bool {
	return err != nil && (err.HTTPStatus == 401 || err.HTTPStatus == 403 || err.Code == -101 || err.Code == -111)
}
func classifyError(err error) (string, string) {
	text := err.Error()
	if before, _, ok := strings.Cut(text, ":"); ok && before == strings.ToUpper(before) && len(before) < 64 {
		return before, strings.TrimSpace(strings.TrimPrefix(text, before+":"))
	}
	return "INTERNAL_ERROR", "任务处理失败，请稍后重试"
}
