package httpserver

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/auth"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	commandparser "github.com/FortyTwoCn/cyber-amber/internal/command/parser"
	"github.com/FortyTwoCn/cyber-amber/internal/config"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/httpserver/security"
	"github.com/FortyTwoCn/cyber-amber/internal/observability"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/FortyTwoCn/cyber-amber/internal/system"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed templates/*.html assets/*
var webFiles embed.FS

type Readiness struct {
	mu     sync.RWMutex
	checks map[string]string
}

func NewReadiness() *Readiness { return &Readiness{checks: map[string]string{}} }
func (r *Readiness) Set(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		delete(r.checks, name)
	} else {
		r.checks[name] = err.Error()
	}
}
func (r *Readiness) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := make(map[string]string, len(r.checks))
	for key, value := range r.checks {
		copy[key] = value
	}
	return copy
}

type Server struct {
	cfg           config.Config
	store         *store.Store
	resolver      video.VideoResolver
	accounts      *auth.Service
	metrics       *observability.Metrics
	ready         *Readiness
	logger        *slog.Logger
	templates     *template.Template
	adminSessions *security.SessionManager
	secureCookie  bool
	mentionWake   func()
	mentionPoll   func(context.Context) error
}

func New(cfg config.Config, store *store.Store, resolver video.VideoResolver, accounts *auth.Service, metrics *observability.Metrics, ready *Readiness, logger *slog.Logger) (*Server, error) {
	templates, err := template.ParseFS(webFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse web templates: %w", err)
	}
	server := &Server{cfg: cfg, store: store, resolver: resolver, accounts: accounts, metrics: metrics, ready: ready, logger: logger, templates: templates}
	base, _ := url.Parse(cfg.App.BaseURL)
	server.secureCookie = base.Scheme == "https"
	if len(cfg.Security.SessionSecret) >= 32 {
		server.adminSessions, err = security.NewSessionManager(cfg.Security.SessionSecret, 12*time.Hour)
		if err != nil {
			return nil, err
		}
	}
	return server, nil
}

// SetMentionPollWake connects successful account operations to the durable
// mention poller without coupling HTTP handlers to its concrete type.
func (s *Server) SetMentionPollWake(wake func()) { s.mentionWake = wake }

// SetMentionPoll connects the administrator's explicit read-only diagnostic
// action to the serialized durable poller.
func (s *Server) SetMentionPoll(poll func(context.Context) error) { s.mentionPoll = poll }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /admin", s.adminPage)
	assets, _ := fs.Sub(webFiles, "assets")
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("POST /api/v1/videos/resolve", s.resolveVideo)
	mux.HandleFunc("POST /api/v1/jobs", s.createJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/events", s.jobEvents)
	mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/artifact", s.artifact)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.metrics.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("POST /api/v1/admin/login", s.adminLogin)
	mux.HandleFunc("POST /api/v1/admin/logout", s.requireAdminCSRF(s.adminLogout))
	mux.HandleFunc("GET /api/v1/admin/account", s.requireAdmin(s.adminAccount))
	mux.HandleFunc("GET /api/v1/admin/settings", s.requireAdmin(s.adminSettings))
	mux.HandleFunc("PUT /api/v1/admin/settings", s.requireAdminCSRF(s.adminSaveSettings))
	mux.HandleFunc("DELETE /api/v1/admin/settings", s.requireAdminCSRF(s.adminResetSettings))
	mux.HandleFunc("POST /api/v1/admin/account/check", s.requireAdminCSRF(s.adminCheckAccount))
	mux.HandleFunc("POST /api/v1/admin/account/cookie", s.requireAdminCSRF(s.adminImportCookie))
	mux.HandleFunc("POST /api/v1/admin/account/qrcode", s.requireAdminCSRF(s.adminGenerateQR))
	mux.HandleFunc("GET /api/v1/admin/account/qrcode/{id}", s.requireAdmin(s.adminPollQR))
	mux.HandleFunc("POST /api/v1/admin/bot/pause", s.requireAdminCSRF(s.adminPause))
	mux.HandleFunc("POST /api/v1/admin/bot/resume", s.requireAdminCSRF(s.adminResume))
	mux.HandleFunc("POST /api/v1/admin/bot/poll", s.requireAdminCSRF(s.adminPollMentions))
	mux.HandleFunc("GET /api/v1/admin/mentions", s.requireAdmin(s.adminMentions))
	mux.HandleFunc("GET /api/v1/admin/jobs", s.requireAdmin(s.adminJobs))
	mux.HandleFunc("GET /api/v1/admin/jobs/{id}", s.requireAdmin(s.adminJobDetail))
	mux.HandleFunc("POST /api/v1/admin/jobs/{id}/retry", s.requireAdminCSRF(s.adminRetry))
	mux.HandleFunc("GET /api/v1/admin/blocked", s.requireAdmin(s.adminListBlocked))
	mux.HandleFunc("POST /api/v1/admin/blocked", s.requireAdminCSRF(s.adminSetBlocked))
	mux.HandleFunc("DELETE /api/v1/admin/blocked/{type}/{id}", s.requireAdminCSRF(s.adminDeleteBlocked))
	mux.HandleFunc("GET /api/v1/admin/audit", s.requireAdmin(s.adminAuditLogs))
	mux.HandleFunc("POST /api/v1/admin/cache/cleanup", s.requireAdminCSRF(s.adminClearCaches))
	return s.middleware(mux)
}

func (s *Server) HTTPServer() *http.Server {
	return &http.Server{Addr: s.cfg.App.Addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
}

type ctxKey int

const requestIDKey ctxKey = 1

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if len(requestID) < 8 || len(requestID) > 128 {
			buffer := make([]byte, 12)
			_, _ = rand.Read(buffer)
			requestID = hex.EncodeToString(buffer)
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.secureCookie {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https://*.hdslb.com https://*.bilibili.com; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		started := time.Now()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID)))
		path := r.URL.Path
		if strings.Contains(path, "/qrcode/") {
			path = "/api/v1/admin/account/qrcode/[REDACTED]"
		}
		s.logger.Info("http request", "request_id", requestID, "method", r.Method, "path", path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Web.PublicEnabled {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "index.html", map[string]any{"Defaults": s.cfg.Defaults, "Limits": s.cfg.Limits, "Media": s.cfg.Media})
}
func (s *Server) adminPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "admin.html", nil)
}

type resolveRequest struct {
	Input string `json:"input"`
	Page  int    `json:"page"`
}

func (s *Server) resolveVideo(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Web.PublicEnabled {
		writeError(w, r, http.StatusNotFound, "PUBLIC_WEB_DISABLED", "公开网页已关闭")
		return
	}
	resolveLimit := max(30, s.cfg.Limits.PublicJobsPerIPHour*6)
	if allowed, _, err := s.store.TakeRateLimit(r.Context(), "web_resolve", s.remoteIP(r), time.Hour, resolveLimit); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "限流检查失败")
		return
	} else if !allowed {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "视频解析请求过于频繁")
		return
	}
	var request resolveRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Input) < 2 || len(request.Input) > 4096 {
		writeError(w, r, http.StatusBadRequest, "INVALID_VIDEO_INPUT", "视频输入长度无效")
		return
	}
	videoInfo, err := s.resolver.Resolve(r.Context(), request.Input, request.Page)
	if err != nil {
		s.logger.Warn("public video resolve failed", "request_id", requestID(r), "error", client.Redact(err.Error()))
		writePublicError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, videoInfo)
}

type createRequest struct {
	Input            string  `json:"input"`
	Page             int     `json:"page"`
	Start            string  `json:"start"`
	End              string  `json:"end"`
	FPS              int     `json:"fps"`
	Width            int     `json:"width"`
	Resolution       string  `json:"resolution"`
	Danmaku          *bool   `json:"danmaku"`
	DanmakuOpacity   float64 `json:"danmaku_opacity"`
	DanmakuFontScale float64 `json:"danmaku_font_scale"`
	DanmakuDensity   float64 `json:"danmaku_density"`
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Web.PublicEnabled {
		writeError(w, r, http.StatusNotFound, "PUBLIC_WEB_DISABLED", "公开网页已关闭")
		return
	}
	anonID := anonymousID(w, r, s.secureCookie)
	allowed, _, err := s.store.TakeRateLimit(r.Context(), "web_ip", s.remoteIP(r), time.Hour, s.cfg.Limits.PublicJobsPerIPHour)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "限流检查失败")
		return
	}
	if !allowed {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "每小时创建任务数已达上限")
		return
	}
	var request createRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defaults := commandparser.Defaults{FPS: s.cfg.Defaults.FPS, Width: s.cfg.Defaults.Width, Danmaku: s.cfg.Defaults.Danmaku, DanmakuOpacity: s.cfg.Defaults.DanmakuOpacity, DanmakuFontScale: s.cfg.Defaults.DanmakuFontScale, DanmakuDensity: s.cfg.Defaults.DanmakuDensity, Page: max(request.Page, 1)}
	parts := []string{request.Start + "-" + request.End}
	if request.FPS > 0 {
		parts = append(parts, fmt.Sprintf("fps=%d", request.FPS))
	}
	if request.Width > 0 {
		parts = append(parts, fmt.Sprintf("width=%d", request.Width))
	}
	if request.Resolution != "" {
		parts = append(parts, "resolution="+request.Resolution)
	}
	if request.Danmaku != nil {
		if *request.Danmaku {
			parts = append(parts, "danmaku=on")
		} else {
			parts = append(parts, "danmaku=off")
		}
	}
	cmd, err := commandparser.Parse(strings.Join(parts, " "), defaults, commandparser.Limits{MinFPS: s.cfg.Media.MinFPS, MaxFPS: s.cfg.Limits.MaxOutputFPS, MinWidth: s.cfg.Media.MinWidth, MaxWidth: s.cfg.Limits.MaxOutputWidth, MaxDuration: s.cfg.Limits.MaxClipDuration})
	if err != nil {
		writePublicError(w, r, err)
		return
	}
	if request.DanmakuOpacity != 0 {
		cmd.Params.DanmakuOpacity = request.DanmakuOpacity
	}
	if request.DanmakuFontScale != 0 {
		cmd.Params.DanmakuFontScale = request.DanmakuFontScale
	}
	if request.DanmakuDensity != 0 {
		cmd.Params.DanmakuDensity = request.DanmakuDensity
	}
	if cmd.Params.DanmakuOpacity <= 0 || cmd.Params.DanmakuOpacity > 1 || cmd.Params.DanmakuFontScale < .5 || cmd.Params.DanmakuFontScale > 2 || cmd.Params.DanmakuDensity <= 0 || cmd.Params.DanmakuDensity > 1 {
		writeError(w, r, http.StatusBadRequest, "INVALID_PARAMETER", "弹幕透明度/字号/密度超出允许范围")
		return
	}
	resolved, err := s.resolver.Resolve(r.Context(), request.Input, cmd.Page)
	if err != nil {
		s.logger.Warn("public job video resolve failed", "request_id", requestID(r), "error", client.Redact(err.Error()))
		writePublicError(w, r, err)
		return
	}
	page := resolved.Pages[cmd.Page-1]
	if cmd.End > page.Duration() {
		writeError(w, r, http.StatusBadRequest, "INVALID_TIME_RANGE", "结束时间超过所选分P时长")
		return
	}
	dedupe := domain.MakeDedupeKey(resolved.BVID, page.CID, cmd.Start, cmd.End, cmd.Params, "v1")
	if existing, err := s.store.FindJobByDedupe(r.Context(), domain.SourceWeb, dedupe); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": existing.View(), "deduplicated": true, "cancel_allowed": existing.AnonymousID == anonID})
		return
	}
	if active, err := s.store.ActiveJobsForAnonymous(r.Context(), anonID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "并发任务检查失败")
		return
	} else if active >= s.cfg.Limits.UserConcurrentJobs {
		writeError(w, r, http.StatusTooManyRequests, "USER_CONCURRENCY_LIMIT", "当前并发任务数已达上限")
		return
	}
	if allowed, _, err := s.store.TakeRateLimit(r.Context(), "web_video_hour", resolved.BVID, time.Hour, s.cfg.Limits.VideoJobsPerHour); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "视频频率检查失败")
		return
	} else if !allowed {
		writeError(w, r, http.StatusTooManyRequests, "VIDEO_RATE_LIMIT", "该视频近期任务数已达上限")
		return
	}
	now := time.Now().UTC()
	job := domain.Job{ID: ulid.Make().String(), Source: domain.SourceWeb, AnonymousID: anonID, Aid: resolved.AID, BVID: resolved.BVID, CID: page.CID, Page: cmd.Page, Start: cmd.Start, End: cmd.End, Requested: cmd.Params, Final: cmd.Params, DedupeKey: dedupe, Status: domain.JobQueued, MaxAttempts: s.cfg.Jobs.MaxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := job.Validate(s.cfg.Limits.MaxClipDuration, s.cfg.Limits.MaxOutputFPS, s.cfg.Limits.MaxOutputWidth); err != nil {
		writePublicError(w, r, err)
		return
	}
	if err := s.store.Enqueue(r.Context(), job); err != nil {
		// The partial unique index closes the race between the lookup above and
		// concurrent inserts from another request or process.
		if existing, findErr := s.store.FindJobByDedupe(r.Context(), domain.SourceWeb, dedupe); findErr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"job": existing.View(), "deduplicated": true, "cancel_allowed": existing.AnonymousID == anonID})
			return
		}
		writeError(w, r, http.StatusInternalServerError, "QUEUE_ERROR", "任务入队失败")
		return
	}
	s.metrics.JobsTotal.WithLabelValues(string(job.Source), "queued").Inc()
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job.View(), "deduplicated": false, "cancel_allowed": true})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务失败")
		return
	}
	writeJSON(w, http.StatusOK, job.View())
}
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusConflict, "JOB_NOT_CANCELLABLE", "任务不存在或已结束")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务失败")
		return
	}
	owner, cookieErr := r.Cookie("ca_anon")
	if job.Source != domain.SourceWeb || cookieErr != nil || owner.Value == "" || owner.Value != job.AnonymousID {
		writeError(w, r, http.StatusForbidden, "JOB_OWNERSHIP_REQUIRED", "只能取消当前浏览器创建的网页任务")
		return
	}
	if err := s.store.Cancel(r.Context(), r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusConflict, "JOB_NOT_CANCELLABLE", "任务不存在或已结束")
		return
	} else if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "取消任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancellation_requested"})
}
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "SSE_UNAVAILABLE", "服务器不支持事件流")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	last := ""
	lastHeartbeat := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"code\":\"JOB_NOT_FOUND\"}\n\n")
			flusher.Flush()
			return
		}
		data, _ := json.Marshal(job.View())
		signature := string(data)
		if signature != last {
			fmt.Fprintf(w, "id: %d\nevent: job\ndata: %s\n\n", job.UpdatedAt.UnixNano(), data)
			flusher.Flush()
			last = signature
			lastHeartbeat = time.Now()
		} else if time.Since(lastHeartbeat) >= 15*time.Second {
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
			lastHeartbeat = time.Now()
		}
		if job.Status.Terminal() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil || job.Status != domain.JobSucceeded || job.ArtifactPath == "" {
		writeError(w, r, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "产物不存在或尚未完成")
		return
	}
	artifactRoot := filepath.Join(s.cfg.Paths.DataDir, "artifacts")
	if !isDirectRegularFileWithin(job.ArtifactPath, artifactRoot, ".gif") {
		writeError(w, r, http.StatusGone, "ARTIFACT_EXPIRED", "产物已过期")
		return
	}
	stat, err := os.Stat(job.ArtifactPath)
	if err != nil {
		writeError(w, r, http.StatusGone, "ARTIFACT_EXPIRED", "产物已过期")
		return
	}
	file, err := os.Open(filepath.Clean(job.ArtifactPath))
	if err != nil {
		writeError(w, r, http.StatusGone, "ARTIFACT_EXPIRED", "产物已过期")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/gif")
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="cyber-amber-%s.gif"`, disposition, job.ID))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", stat.ModTime(), file)
}
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	checks := s.ready.Snapshot()
	delete(checks, "disk")
	if err := s.store.Ping(r.Context()); err != nil {
		checks["database"] = err.Error()
	}
	if free, err := system.FreeBytes(s.cfg.Paths.DataDir); err != nil {
		checks["disk"] = err.Error()
	} else if free < s.cfg.Limits.MinFreeDiskBytes {
		checks["disk"] = fmt.Sprintf("free bytes %d below minimum %d", free, s.cfg.Limits.MinFreeDiskBytes)
	}
	if len(checks) > 0 {
		publicChecks := make(map[string]string, len(checks))
		for name := range checks {
			switch name {
			case "ffmpeg":
				publicChecks[name] = "FFmpeg 不可用或缺少必需滤镜/解码器"
			case "ffprobe":
				publicChecks[name] = "ffprobe 不可用"
			case "font":
				publicChecks[name] = "配置的中文字体不可用"
			case "disk":
				publicChecks[name] = "可用磁盘空间低于安全阈值"
			case "database":
				publicChecks[name] = "数据库不可用"
			case "bili_account":
				publicChecks[name] = "已保存的 B站账号凭据无法读取或已失效"
			default:
				publicChecks[name] = "依赖项尚未就绪"
			}
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "checks": publicChecks})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func anonymousID(w http.ResponseWriter, r *http.Request, secure bool) string {
	if cookie, err := r.Cookie("ca_anon"); err == nil && len(cookie.Value) >= 20 && len(cookie.Value) <= 128 {
		return cookie.Value
	}
	bytes := make([]byte, 18)
	_, _ = rand.Read(bytes)
	value := base64.RawURLEncoding.EncodeToString(bytes)
	http.SetCookie(w, &http.Cookie{Name: "ca_anon", Value: value, Path: "/", MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	return value
}

func isDirectRegularFileWithin(path, root, suffix string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(rootAbsolute, absolute)
	if err != nil || filepath.IsAbs(relative) || filepath.Dir(relative) != "." || relative == "." || strings.HasPrefix(relative, "..") {
		return false
	}
	if !strings.EqualFold(filepath.Ext(relative), suffix) {
		return false
	}
	info, err := os.Lstat(absolute)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}
func (s *Server) remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return host
	}
	if !s.isTrustedProxy(peer) {
		return peer.String()
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		clientIP := net.ParseIP(strings.TrimSpace(forwarded[index]))
		if clientIP == nil {
			return peer.String()
		}
		if !s.isTrustedProxy(clientIP) {
			return clientIP.String()
		}
	}
	return peer.String()
}

func (s *Server) isTrustedProxy(ip net.IP) bool {
	for _, raw := range s.cfg.Web.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "请求 JSON 无效")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": requestID}})
}
func writePublicError(w http.ResponseWriter, r *http.Request, err error) {
	code := "INVALID_REQUEST"
	message := err.Error()
	var commandErr *commandparser.Error
	if errors.As(err, &commandErr) {
		code = commandErr.Code
		message = commandErr.Message
	} else if before, after, ok := strings.Cut(message, ":"); ok && before == strings.ToUpper(before) && len(before) < 64 {
		code = before
		message = strings.TrimSpace(after)
	} else {
		message = "请求无法处理，请检查视频输入和参数后重试"
	}
	status := http.StatusBadRequest
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		status = http.StatusBadGateway
		if apiErr.Code == -404 {
			status = http.StatusNotFound
			code = "VIDEO_NOT_FOUND"
			message = "B站未找到该视频或视频不可访问"
		} else {
			code = "BILI_API_ERROR"
			message = "B站接口暂时无法完成请求"
		}
	}
	writeError(w, r, status, code, message)
}

func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey).(string)
	return value
}
