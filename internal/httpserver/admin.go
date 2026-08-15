package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/config"
	"github.com/FortyTwoCn/cyber-amber/internal/httpserver/security"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
	qrcode "github.com/skip2/go-qrcode"
)

const adminCookie = "ca_admin"

func (s *Server) adminEnabled() bool {
	return s.adminSessions != nil && s.cfg.Security.AdminPasswordHash != ""
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.adminEnabled() {
		writeError(w, r, http.StatusServiceUnavailable, "ADMIN_DISABLED", "管理员登录尚未配置")
		return
	}
	allowed, _, err := s.store.TakeRateLimit(r.Context(), "admin_login", s.remoteIP(r), 15*time.Minute, 5)
	if err != nil || !allowed {
		writeError(w, r, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "登录尝试过多，请稍后再试")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Password) > 1024 {
		writeError(w, r, http.StatusBadRequest, "INVALID_CREDENTIALS", "密码无效")
		return
	}
	ok, err := security.VerifyPassword(s.cfg.Security.AdminPasswordHash, request.Password)
	if err != nil {
		s.logger.Error("invalid admin password hash", "error", err)
		writeError(w, r, http.StatusServiceUnavailable, "ADMIN_CONFIG_INVALID", "管理员密码配置无效")
		return
	}
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "密码错误")
		return
	}
	token, csrf, err := s.adminSessions.Create(time.Now())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "创建登录会话失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", MaxAge: 12 * 3600, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "ca_admin_csrf", Value: csrf, Path: "/", MaxAge: 12 * 3600, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	s.audit(r, "login", "admin")
	writeJSON(w, http.StatusOK, map[string]any{"logged_in": true, "csrf_token": csrf})
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "ca_admin_csrf", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	s.audit(r, "logout", "admin")
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": false})
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.adminEnabled() {
			writeError(w, r, http.StatusServiceUnavailable, "ADMIN_DISABLED", "管理员登录尚未配置")
			return
		}
		cookie, err := r.Cookie(adminCookie)
		if err != nil || !s.adminSessions.Verify(cookie.Value, time.Now()) {
			writeError(w, r, http.StatusUnauthorized, "ADMIN_AUTH_REQUIRED", "需要管理员登录")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdminCSRF(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie(adminCookie)
		csrfCookie, err := r.Cookie("ca_admin_csrf")
		provided := r.Header.Get("X-CSRF-Token")
		if err != nil || provided == "" || csrfCookie.Value != provided || !s.adminSessions.VerifyCSRF(cookie.Value, provided) {
			writeError(w, r, http.StatusForbidden, "CSRF_FAILED", "CSRF 校验失败")
			return
		}
		next(w, r)
	})
}

func (s *Server) adminAccount(w http.ResponseWriter, r *http.Request) {
	status, err := s.accounts.Status(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取账号状态失败")
		return
	}
	bot, _ := s.store.BotState(r.Context())
	cursor, _ := s.store.LoadCursor(r.Context(), "bili_at")
	queued, _ := s.store.QueueDepth(r.Context())
	active, _ := s.store.ActiveJobs(r.Context())
	statusCounts, _ := s.store.JobStatusCounts(r.Context())
	ffmpegActive := statusCounts["rendering"]
	cookie, _ := r.Cookie(adminCookie)
	writeJSON(w, http.StatusOK, map[string]any{"account": status, "bot": bot, "cursor": cursor, "queue_depth": queued, "active_jobs": active, "ffmpeg_active": ffmpegActive, "job_status_counts": statusCounts, "dry_run": s.cfg.Bili.DryRun, "poll_interval": s.cfg.Bili.PollInterval.String(), "timezone": s.cfg.App.Timezone, "limits": map[string]any{"default_fps": s.cfg.Defaults.FPS, "default_width": s.cfg.Defaults.Width, "default_danmaku": s.cfg.Defaults.Danmaku, "default_danmaku_opacity": s.cfg.Defaults.DanmakuOpacity, "default_danmaku_font_scale": s.cfg.Defaults.DanmakuFontScale, "default_danmaku_density": s.cfg.Defaults.DanmakuDensity, "max_clip_duration": s.cfg.Limits.MaxClipDuration.String(), "absolute_max_clip_duration": s.cfg.Limits.AbsoluteMaxClipDuration.String(), "max_gif_bytes": s.cfg.Media.MaxGIFBytes, "max_output_width": s.cfg.Limits.MaxOutputWidth, "max_output_fps": s.cfg.Limits.MaxOutputFPS, "public_jobs_per_ip_hour": s.cfg.Limits.PublicJobsPerIPHour, "bili_jobs_per_mid_day": s.cfg.Limits.BiliJobsPerMIDDay, "user_concurrent_jobs": s.cfg.Limits.UserConcurrentJobs, "video_jobs_per_hour": s.cfg.Limits.VideoJobsPerHour, "user_cooldown": s.cfg.Limits.UserCooldown.String(), "artifact_retention": s.cfg.Limits.ArtifactRetention.String(), "artifact_quota_bytes": s.cfg.Limits.ArtifactQuotaBytes, "worker_concurrency": s.cfg.Jobs.WorkerConcurrency, "ffmpeg_concurrency": s.cfg.Jobs.FFmpegConcurrency, "publish_concurrency": s.cfg.Jobs.PublishConcurrency, "max_publish_per_day": s.cfg.Bili.MaxPublishPerDay}, "csrf_token": s.adminSessions.CSRF(cookie.Value)})
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	effective := s.cfg.OperationalSettings()
	pending := effective
	restartRequired := false
	if raw, found, err := s.store.LoadSetting(r.Context(), config.OperationalSettingsKey); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取运行设置失败")
		return
	} else if found {
		if err := json.Unmarshal(raw, &pending); err != nil {
			writeError(w, r, http.StatusInternalServerError, "SETTINGS_CORRUPTED", "持久化运行设置无法解析")
			return
		}
		restartRequired = pending != effective
	}
	writeJSON(w, http.StatusOK, map[string]any{"effective": effective, "pending": pending, "restart_required": restartRequired})
}

func (s *Server) adminSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings config.OperationalSettings
	if !decodeJSON(w, r, &settings) {
		return
	}
	candidate := s.cfg
	if err := candidate.ApplyOperationalSettings(settings); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_SETTINGS", err.Error())
		return
	}
	canonical, err := json.Marshal(candidate.OperationalSettings())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "编码运行设置失败")
		return
	}
	if err := s.store.SaveSetting(r.Context(), config.OperationalSettingsKey, canonical); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "保存运行设置失败")
		return
	}
	s.audit(r, "settings_update", config.OperationalSettingsKey)
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "restart_required": candidate.OperationalSettings() != s.cfg.OperationalSettings(), "pending": candidate.OperationalSettings()})
}

func (s *Server) adminResetSettings(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSetting(r.Context(), config.OperationalSettingsKey); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "重置运行设置失败")
		return
	}
	s.audit(r, "settings_reset", config.OperationalSettingsKey)
	writeJSON(w, http.StatusOK, map[string]any{"reset": true, "restart_required": true})
}
func (s *Server) adminCheckAccount(w http.ResponseWriter, r *http.Request) {
	status, err := s.accounts.HealthCheck(r.Context())
	if err != nil {
		s.ready.Set("bili_account", err)
		writeError(w, r, http.StatusBadGateway, "BILI_ACCOUNT_CHECK_FAILED", err.Error())
		return
	}
	if status.LoggedIn {
		s.ready.Set("bili_account", nil)
	} else {
		s.ready.Set("bili_account", errors.New("B站账号未登录"))
	}
	s.audit(r, "account_check", "bili_account")
	writeJSON(w, http.StatusOK, status)
}
func (s *Server) adminImportCookie(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Cookie string `json:"cookie"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.accounts.Import(r.Context(), request.Cookie); err != nil {
		if previous, statusErr := s.accounts.Status(r.Context()); statusErr == nil && previous.LoggedIn {
			s.ready.Set("bili_account", nil)
		} else {
			s.ready.Set("bili_account", err)
		}
		writeError(w, r, http.StatusBadRequest, "BILI_LOGIN_FAILED", err.Error())
		return
	}
	s.ready.Set("bili_account", nil)
	if s.mentionWake != nil {
		s.mentionWake()
	}
	s.audit(r, "account_cookie_import", "bili_account")
	writeJSON(w, http.StatusOK, map[string]bool{"imported": true})
}
func (s *Server) adminGenerateQR(w http.ResponseWriter, r *http.Request) {
	qr, err := s.accounts.GenerateQR(r.Context())
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "BILI_QR_FAILED", err.Error())
		return
	}
	png, err := qrcode.Encode(qr.URL, qrcode.Medium, 256)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "QR_ENCODE_FAILED", "生成二维码图片失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": qr.Key, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}
func (s *Server) adminPollQR(w http.ResponseWriter, r *http.Request) {
	result, err := s.accounts.PollQR(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "BILI_QR_FAILED", err.Error())
		return
	}
	if result.State == "confirmed" {
		s.ready.Set("bili_account", nil)
		if s.mentionWake != nil {
			s.mentionWake()
		}
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) adminPause(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Reason == "" {
		request.Reason = "ADMIN_PAUSED"
	}
	if err := s.store.SetBotPaused(r.Context(), true, request.Reason, nil); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "暂停机器人失败")
		return
	}
	s.audit(r, "bot_pause", request.Reason)
	writeJSON(w, http.StatusOK, map[string]bool{"paused": true})
}
func (s *Server) adminResume(w http.ResponseWriter, r *http.Request) {
	status, err := s.accounts.Status(r.Context())
	if err != nil || !status.LoggedIn {
		writeError(w, r, http.StatusConflict, "BILI_AUTH_INVALID", "请先导入有效 Cookie 或完成扫码登录")
		return
	}
	if err := s.store.SetBotPaused(r.Context(), false, "", nil); err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "恢复机器人失败")
		return
	}
	if s.mentionWake != nil {
		s.mentionWake()
	}
	s.audit(r, "bot_resume", "bot")
	writeJSON(w, http.StatusOK, map[string]bool{"paused": false})
}
func (s *Server) adminJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs(r.Context(), 100, 0)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务列表失败")
		return
	}
	views := make([]any, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, job.View())
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views})
}

func (s *Server) adminMentions(w http.ResponseWriter, r *http.Request) {
	mentions, err := s.store.ListMentions(r.Context(), 100)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取 @ 通知失败")
		return
	}
	type mentionView struct {
		NotificationID string    `json:"notification_id"`
		OccurredAt     time.Time `json:"occurred_at"`
		SenderMID      int64     `json:"sender_mid"`
		SenderName     string    `json:"sender_name"`
		Message        string    `json:"message"`
		AID            int64     `json:"aid"`
		BVID           string    `json:"bvid,omitempty"`
		RPID           int64     `json:"rpid"`
		Status         string    `json:"status"`
		ErrorCode      string    `json:"error_code,omitempty"`
	}
	views := make([]mentionView, 0, len(mentions))
	for _, mention := range mentions {
		views = append(views, mentionView{
			NotificationID: mention.NotificationID,
			OccurredAt:     mention.OccurredAt,
			SenderMID:      mention.SenderMID,
			SenderName:     mention.SenderName,
			Message:        mention.Message,
			AID:            mention.AID,
			BVID:           mention.BVID,
			RPID:           mention.RPID,
			Status:         mention.Status,
			ErrorCode:      mention.ErrorCode,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mentions": views})
}

func (s *Server) adminJobDetail(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务失败")
		return
	}
	attempts, err := s.store.ListJobAttempts(r.Context(), job.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务尝试记录失败")
		return
	}
	published, publishErr := s.store.LoadPublished(r.Context(), job.ID)
	if publishErr != nil && !errors.Is(publishErr, store.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取发布记录失败")
		return
	}
	response := map[string]any{"job": job.View(), "diagnostic": job.Diagnostic, "attempts": attempts}
	if publishErr == nil {
		response["published_comment"] = published
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *Server) adminRetry(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Retry(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusConflict, "JOB_NOT_RETRYABLE", "任务不可重试或已达最大次数")
		return
	}
	s.audit(r, "job_retry", r.PathValue("id"))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) adminListBlocked(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListBlocked(r.Context(), 200)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取黑名单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"blocked": items})
}

func (s *Server) adminSetBlocked(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SubjectType string `json:"subject_type"`
		SubjectID   string `json:"subject_id"`
		Reason      string `json:"reason"`
		ExpiresAt   string `json:"expires_at"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	request.SubjectType = strings.TrimSpace(request.SubjectType)
	request.SubjectID = strings.TrimSpace(request.SubjectID)
	request.Reason = strings.TrimSpace(request.Reason)
	var expires *time.Time
	if request.ExpiresAt != "" {
		value, err := time.Parse(time.RFC3339, request.ExpiresAt)
		if err != nil || value.Before(time.Now()) {
			writeError(w, r, http.StatusBadRequest, "INVALID_EXPIRY", "expires_at 必须是未来的 RFC3339 时间")
			return
		}
		value = value.UTC()
		expires = &value
	}
	if err := s.store.SetBlocked(r.Context(), request.SubjectType, request.SubjectID, request.Reason, expires); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_BLOCK_ENTRY", err.Error())
		return
	}
	s.audit(r, "block_set", request.SubjectType+":"+request.SubjectID)
	writeJSON(w, http.StatusCreated, map[string]bool{"blocked": true})
}

func (s *Server) adminDeleteBlocked(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("type") + ":" + r.PathValue("id")
	if err := s.store.DeleteBlocked(r.Context(), r.PathValue("type"), r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusNotFound, "BLOCK_ENTRY_NOT_FOUND", "黑名单条目不存在")
		return
	}
	s.audit(r, "block_delete", target)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAudits(r.Context(), 200)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "读取审计日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": items})
}

func (s *Server) adminClearCaches(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearCaches(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "CACHE_CLEANUP_FAILED", "清理元数据与弹幕缓存失败")
		return
	}
	s.audit(r, "cache_cleanup", "video_and_danmaku")
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}

func (s *Server) audit(r *http.Request, action, target string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	if err := s.store.Audit(r.Context(), "admin", action, target, requestID, nil); err != nil {
		s.logger.Error("write admin audit", "action", action, "error", err)
	}
}
