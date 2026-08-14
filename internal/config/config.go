package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const OperationalSettingsKey = "operational_v1"

type Config struct {
	App      AppConfig      `yaml:"app"`
	Paths    PathsConfig    `yaml:"paths"`
	Database DatabaseConfig `yaml:"database"`
	Jobs     JobsConfig     `yaml:"jobs"`
	Media    MediaConfig    `yaml:"media"`
	Defaults DefaultsConfig `yaml:"defaults"`
	Limits   LimitsConfig   `yaml:"limits"`
	Bili     BiliConfig     `yaml:"bili"`
	Web      WebConfig      `yaml:"web"`
	Security SecurityConfig `yaml:"security"`
}

type AppConfig struct {
	Addr      string `yaml:"addr"`
	BaseURL   string `yaml:"base_url"`
	Timezone  string `yaml:"timezone"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

type PathsConfig struct {
	DataDir  string `yaml:"data_dir"`
	CacheDir string `yaml:"cache_dir"`
	TempDir  string `yaml:"temp_dir"`
}

type DatabaseConfig struct {
	Path        string        `yaml:"path"`
	BusyTimeout time.Duration `yaml:"-"`
	BusyText    string        `yaml:"busy_timeout"`
}

type JobsConfig struct {
	WorkerConcurrency  int           `yaml:"worker_concurrency"`
	FFmpegConcurrency  int           `yaml:"ffmpeg_concurrency"`
	PublishConcurrency int           `yaml:"publish_concurrency"`
	Timeout            time.Duration `yaml:"-"`
	TimeoutText        string        `yaml:"timeout"`
	LeaseDuration      time.Duration `yaml:"-"`
	LeaseText          string        `yaml:"lease_duration"`
	MaxAttempts        int           `yaml:"max_attempts"`
}

type MediaConfig struct {
	FFmpegPath  string `yaml:"ffmpeg_path"`
	FFprobePath string `yaml:"ffprobe_path"`
	FontFamily  string `yaml:"font_family"`
	MaxGIFBytes int64  `yaml:"max_gif_bytes"`
	MinFPS      int    `yaml:"min_fps"`
	MinWidth    int    `yaml:"min_width"`
	MinColors   int    `yaml:"min_colors"`
}

type DefaultsConfig struct {
	FPS              int     `yaml:"fps"`
	Width            int     `yaml:"width"`
	Danmaku          bool    `yaml:"danmaku"`
	DanmakuOpacity   float64 `yaml:"danmaku_opacity"`
	DanmakuFontScale float64 `yaml:"danmaku_font_scale"`
	DanmakuDensity   float64 `yaml:"danmaku_density"`
}

type LimitsConfig struct {
	MaxClipDuration         time.Duration `yaml:"-"`
	MaxClipDurationText     string        `yaml:"max_clip_duration"`
	AbsoluteMaxClipDuration time.Duration `yaml:"-"`
	AbsoluteMaxText         string        `yaml:"absolute_max_clip_duration"`
	MaxOutputWidth          int           `yaml:"max_output_width"`
	MaxOutputFPS            int           `yaml:"max_output_fps"`
	PublicJobsPerIPHour     int           `yaml:"public_jobs_per_ip_hour"`
	BiliJobsPerMIDDay       int           `yaml:"bili_jobs_per_mid_day"`
	UserConcurrentJobs      int           `yaml:"user_concurrent_jobs"`
	VideoJobsPerHour        int           `yaml:"video_jobs_per_hour"`
	UserCooldown            time.Duration `yaml:"-"`
	UserCooldownText        string        `yaml:"user_cooldown"`
	ArtifactRetention       time.Duration `yaml:"-"`
	ArtifactRetentionText   string        `yaml:"artifact_retention"`
	ArtifactQuotaBytes      uint64        `yaml:"artifact_quota_bytes"`
	MinFreeDiskBytes        uint64        `yaml:"min_free_disk_bytes"`
}

type BiliConfig struct {
	APIBaseURL          string        `yaml:"api_base_url"`
	PassportBaseURL     string        `yaml:"passport_base_url"`
	PollInterval        time.Duration `yaml:"-"`
	PollIntervalText    string        `yaml:"poll_interval"`
	BackfillOnFirstRun  bool          `yaml:"backfill_on_first_run"`
	DryRun              bool          `yaml:"dry_run"`
	MaxPublishPerDay    int           `yaml:"max_publish_per_day"`
	ReplyOriginal       bool          `yaml:"reply_original_comment"`
	UserAgent           string        `yaml:"user_agent"`
	Cookie              string        `yaml:"-"`
	NotificationEnabled bool          `yaml:"notification_enabled"`
}

type WebConfig struct {
	PublicEnabled     bool     `yaml:"public_enabled"`
	TrustedProxyCIDRs []string `yaml:"trusted_proxy_cidrs"`
}

type SecurityConfig struct {
	CookieEncryptionKey     []byte `yaml:"-"`
	CookieEncryptionKeyFile string `yaml:"cookie_encryption_key_file"`
	AdminPasswordHash       string `yaml:"admin_password_hash"`
	AdminPasswordHashFile   string `yaml:"admin_password_hash_file"`
	SessionSecret           []byte `yaml:"-"`
	SessionSecretFile       string `yaml:"session_secret_file"`
}

// OperationalSettings is the non-secret, administrator-managed subset of
// configuration. It is stored as one versioned JSON document in SQLite and is
// applied during startup before workers and HTTP handlers are constructed.
// Network paths, executable paths and all credentials remain deployment-only
// configuration so a compromised admin browser cannot redirect trusted I/O.
type OperationalSettings struct {
	Version                  int     `json:"version"`
	DefaultFPS               int     `json:"default_fps"`
	DefaultWidth             int     `json:"default_width"`
	DefaultDanmaku           bool    `json:"default_danmaku"`
	DefaultDanmakuOpacity    float64 `json:"default_danmaku_opacity"`
	DefaultDanmakuFontScale  float64 `json:"default_danmaku_font_scale"`
	DefaultDanmakuDensity    float64 `json:"default_danmaku_density"`
	MaxClipDuration          string  `json:"max_clip_duration"`
	AbsoluteMaxClipDuration  string  `json:"absolute_max_clip_duration"`
	MaxOutputWidth           int     `json:"max_output_width"`
	MaxOutputFPS             int     `json:"max_output_fps"`
	MaxGIFBytes              int64   `json:"max_gif_bytes"`
	PublicJobsPerIPHour      int     `json:"public_jobs_per_ip_hour"`
	BiliJobsPerMIDDay        int     `json:"bili_jobs_per_mid_day"`
	UserConcurrentJobs       int     `json:"user_concurrent_jobs"`
	VideoJobsPerHour         int     `json:"video_jobs_per_hour"`
	UserCooldown             string  `json:"user_cooldown"`
	ArtifactRetention        string  `json:"artifact_retention"`
	ArtifactQuotaBytes       uint64  `json:"artifact_quota_bytes"`
	MinFreeDiskBytes         uint64  `json:"min_free_disk_bytes"`
	WorkerConcurrency        int     `json:"worker_concurrency"`
	FFmpegConcurrency        int     `json:"ffmpeg_concurrency"`
	PublishConcurrency       int     `json:"publish_concurrency"`
	JobTimeout               string  `json:"job_timeout"`
	JobLeaseDuration         string  `json:"job_lease_duration"`
	JobMaxAttempts           int     `json:"job_max_attempts"`
	BiliPollInterval         string  `json:"bili_poll_interval"`
	BiliNotificationEnabled  bool    `json:"bili_notification_enabled"`
	BiliDryRun               bool    `json:"bili_dry_run"`
	BiliMaxPublishPerDay     int     `json:"bili_max_publish_per_day"`
	BiliReplyOriginalComment bool    `json:"bili_reply_original_comment"`
}

func (c Config) OperationalSettings() OperationalSettings {
	return OperationalSettings{
		Version: 1, DefaultFPS: c.Defaults.FPS, DefaultWidth: c.Defaults.Width,
		DefaultDanmaku: c.Defaults.Danmaku, DefaultDanmakuOpacity: c.Defaults.DanmakuOpacity,
		DefaultDanmakuFontScale: c.Defaults.DanmakuFontScale, DefaultDanmakuDensity: c.Defaults.DanmakuDensity,
		MaxClipDuration: c.Limits.MaxClipDurationText, AbsoluteMaxClipDuration: c.Limits.AbsoluteMaxText,
		MaxOutputWidth: c.Limits.MaxOutputWidth, MaxOutputFPS: c.Limits.MaxOutputFPS, MaxGIFBytes: c.Media.MaxGIFBytes,
		PublicJobsPerIPHour: c.Limits.PublicJobsPerIPHour, BiliJobsPerMIDDay: c.Limits.BiliJobsPerMIDDay,
		UserConcurrentJobs: c.Limits.UserConcurrentJobs, VideoJobsPerHour: c.Limits.VideoJobsPerHour,
		UserCooldown: c.Limits.UserCooldownText, ArtifactRetention: c.Limits.ArtifactRetentionText,
		ArtifactQuotaBytes: c.Limits.ArtifactQuotaBytes, MinFreeDiskBytes: c.Limits.MinFreeDiskBytes,
		WorkerConcurrency: c.Jobs.WorkerConcurrency, FFmpegConcurrency: c.Jobs.FFmpegConcurrency,
		PublishConcurrency: c.Jobs.PublishConcurrency, JobTimeout: c.Jobs.TimeoutText,
		JobLeaseDuration: c.Jobs.LeaseText, JobMaxAttempts: c.Jobs.MaxAttempts,
		BiliPollInterval: c.Bili.PollIntervalText, BiliNotificationEnabled: c.Bili.NotificationEnabled,
		BiliDryRun: c.Bili.DryRun, BiliMaxPublishPerDay: c.Bili.MaxPublishPerDay,
		BiliReplyOriginalComment: c.Bili.ReplyOriginal,
	}
}

// ApplyOperationalSettings validates the complete resulting configuration and
// changes the receiver only on success.
func (c *Config) ApplyOperationalSettings(settings OperationalSettings) error {
	if settings.Version != 1 {
		return fmt.Errorf("unsupported operational settings version %d", settings.Version)
	}
	candidate := *c
	candidate.Defaults = DefaultsConfig{FPS: settings.DefaultFPS, Width: settings.DefaultWidth, Danmaku: settings.DefaultDanmaku, DanmakuOpacity: settings.DefaultDanmakuOpacity, DanmakuFontScale: settings.DefaultDanmakuFontScale, DanmakuDensity: settings.DefaultDanmakuDensity}
	candidate.Limits.MaxClipDurationText = settings.MaxClipDuration
	candidate.Limits.AbsoluteMaxText = settings.AbsoluteMaxClipDuration
	candidate.Limits.MaxOutputWidth = settings.MaxOutputWidth
	candidate.Limits.MaxOutputFPS = settings.MaxOutputFPS
	candidate.Media.MaxGIFBytes = settings.MaxGIFBytes
	candidate.Limits.PublicJobsPerIPHour = settings.PublicJobsPerIPHour
	candidate.Limits.BiliJobsPerMIDDay = settings.BiliJobsPerMIDDay
	candidate.Limits.UserConcurrentJobs = settings.UserConcurrentJobs
	candidate.Limits.VideoJobsPerHour = settings.VideoJobsPerHour
	candidate.Limits.UserCooldownText = settings.UserCooldown
	candidate.Limits.ArtifactRetentionText = settings.ArtifactRetention
	candidate.Limits.ArtifactQuotaBytes = settings.ArtifactQuotaBytes
	candidate.Limits.MinFreeDiskBytes = settings.MinFreeDiskBytes
	candidate.Jobs.WorkerConcurrency = settings.WorkerConcurrency
	candidate.Jobs.FFmpegConcurrency = settings.FFmpegConcurrency
	candidate.Jobs.PublishConcurrency = settings.PublishConcurrency
	candidate.Jobs.TimeoutText = settings.JobTimeout
	candidate.Jobs.LeaseText = settings.JobLeaseDuration
	candidate.Jobs.MaxAttempts = settings.JobMaxAttempts
	candidate.Bili.PollIntervalText = settings.BiliPollInterval
	candidate.Bili.NotificationEnabled = settings.BiliNotificationEnabled
	candidate.Bili.DryRun = settings.BiliDryRun
	candidate.Bili.MaxPublishPerDay = settings.BiliMaxPublishPerDay
	candidate.Bili.ReplyOriginal = settings.BiliReplyOriginalComment
	if err := candidate.normalize(); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*c = candidate
	return nil
}

func Default() Config {
	return Config{
		App:      AppConfig{Addr: ":8080", BaseURL: "http://localhost:8080", Timezone: "Asia/Shanghai", LogLevel: "info", LogFormat: "json"},
		Paths:    PathsConfig{DataDir: "data", CacheDir: "cache", TempDir: "tmp"},
		Database: DatabaseConfig{Path: filepath.Join("data", "cyber-amber.db"), BusyText: "5s"},
		Jobs:     JobsConfig{WorkerConcurrency: 2, FFmpegConcurrency: 2, PublishConcurrency: 1, TimeoutText: "10m", LeaseText: "2m", MaxAttempts: 3},
		Media:    MediaConfig{FFmpegPath: "ffmpeg", FFprobePath: "ffprobe", FontFamily: "Noto Sans CJK SC", MaxGIFBytes: 8 * 1024 * 1024, MinFPS: 5, MinWidth: 320, MinColors: 64},
		Defaults: DefaultsConfig{FPS: 10, Width: 640, Danmaku: false, DanmakuOpacity: 0.85, DanmakuFontScale: 1, DanmakuDensity: 1},
		Limits:   LimitsConfig{MaxClipDurationText: "15s", AbsoluteMaxText: "30s", MaxOutputWidth: 1280, MaxOutputFPS: 20, PublicJobsPerIPHour: 10, BiliJobsPerMIDDay: 20, UserConcurrentJobs: 2, VideoJobsPerHour: 20, UserCooldownText: "30s", ArtifactRetentionText: "168h", ArtifactQuotaBytes: 20 * 1024 * 1024 * 1024, MinFreeDiskBytes: 1024 * 1024 * 1024},
		Bili:     BiliConfig{APIBaseURL: "https://api.bilibili.com", PassportBaseURL: "https://passport.bilibili.com", PollIntervalText: "20s", DryRun: true, MaxPublishPerDay: 50, ReplyOriginal: true, UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/127 Safari/537.36 CyberAmber/1.0", NotificationEnabled: true},
		Web:      WebConfig{PublicEnabled: true},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("parse config: %w", err)
			}
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

func (c *Config) normalize() error {
	var err error
	if c.Database.BusyTimeout, err = time.ParseDuration(c.Database.BusyText); err != nil {
		return fmt.Errorf("database.busy_timeout: %w", err)
	}
	if c.Jobs.Timeout, err = time.ParseDuration(c.Jobs.TimeoutText); err != nil {
		return fmt.Errorf("jobs.timeout: %w", err)
	}
	if c.Jobs.LeaseDuration, err = time.ParseDuration(c.Jobs.LeaseText); err != nil {
		return fmt.Errorf("jobs.lease_duration: %w", err)
	}
	if c.Limits.MaxClipDuration, err = time.ParseDuration(c.Limits.MaxClipDurationText); err != nil {
		return fmt.Errorf("limits.max_clip_duration: %w", err)
	}
	if c.Limits.AbsoluteMaxClipDuration, err = time.ParseDuration(c.Limits.AbsoluteMaxText); err != nil {
		return fmt.Errorf("limits.absolute_max_clip_duration: %w", err)
	}
	if c.Limits.ArtifactRetention, err = time.ParseDuration(c.Limits.ArtifactRetentionText); err != nil {
		return fmt.Errorf("limits.artifact_retention: %w", err)
	}
	if c.Limits.UserCooldown, err = time.ParseDuration(c.Limits.UserCooldownText); err != nil {
		return fmt.Errorf("limits.user_cooldown: %w", err)
	}
	if c.Bili.PollInterval, err = time.ParseDuration(c.Bili.PollIntervalText); err != nil {
		return fmt.Errorf("bili.poll_interval: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	var errs []error
	if _, _, err := net.SplitHostPort(c.App.Addr); err != nil {
		errs = append(errs, fmt.Errorf("app.addr: %w", err))
	}
	if !containsFold([]string{"debug", "info", "warn", "error"}, c.App.LogLevel) {
		errs = append(errs, errors.New("app.log_level must be debug, info, warn or error"))
	}
	if !containsFold([]string{"json", "text"}, c.App.LogFormat) {
		errs = append(errs, errors.New("app.log_format must be json or text"))
	}
	if u, err := url.Parse(c.App.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("app.base_url must be an absolute http(s) URL"))
	}
	if _, err := time.LoadLocation(c.App.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("app.timezone: %w", err))
	}
	if c.Paths.DataDir == "" || c.Paths.CacheDir == "" || c.Paths.TempDir == "" {
		errs = append(errs, errors.New("data, cache and temp directories are required"))
	}
	if c.Database.Path == "" || c.Database.BusyTimeout <= 0 {
		errs = append(errs, errors.New("database path and positive busy timeout are required"))
	}
	if c.Jobs.WorkerConcurrency < 1 || c.Jobs.FFmpegConcurrency < 1 || c.Jobs.PublishConcurrency < 1 {
		errs = append(errs, errors.New("all concurrency values must be at least 1"))
	}
	if c.Jobs.Timeout <= 0 || c.Jobs.LeaseDuration < 3*time.Second || c.Jobs.MaxAttempts < 1 {
		errs = append(errs, errors.New("job timeout and attempts must be positive, and lease must be at least 3s"))
	}
	if c.Jobs.LeaseDuration > c.Jobs.Timeout {
		errs = append(errs, errors.New("job lease duration cannot exceed job timeout"))
	}
	if c.Media.FFmpegPath == "" || c.Media.FFprobePath == "" || strings.TrimSpace(c.Media.FontFamily) == "" {
		errs = append(errs, errors.New("ffmpeg, ffprobe and font family are required"))
	}
	if c.Media.MinFPS < 1 || c.Media.MinFPS > c.Limits.MaxOutputFPS {
		errs = append(errs, errors.New("minimum FPS is outside configured limits"))
	}
	if c.Media.MinWidth < 2 || c.Media.MinWidth > c.Limits.MaxOutputWidth || c.Media.MinWidth%2 != 0 || c.Limits.MaxOutputWidth%2 != 0 {
		errs = append(errs, errors.New("minimum and maximum output widths must be even and ordered"))
	}
	if c.Limits.MaxOutputFPS < 1 || c.Limits.MaxOutputFPS > 60 || c.Defaults.FPS < c.Media.MinFPS || c.Defaults.FPS > c.Limits.MaxOutputFPS {
		errs = append(errs, errors.New("default fps is outside configured limits"))
	}
	if c.Limits.MaxOutputWidth > 3840 || c.Defaults.Width < c.Media.MinWidth || c.Defaults.Width > c.Limits.MaxOutputWidth || c.Defaults.Width%2 != 0 {
		errs = append(errs, errors.New("default width is outside configured limits"))
	}
	if c.Media.MinColors < 4 || c.Media.MinColors > 256 || c.Media.MaxGIFBytes < 1024 {
		errs = append(errs, errors.New("invalid GIF quality limits"))
	}
	if c.Defaults.DanmakuOpacity <= 0 || c.Defaults.DanmakuOpacity > 1 || c.Defaults.DanmakuFontScale < .5 || c.Defaults.DanmakuFontScale > 2 || c.Defaults.DanmakuDensity <= 0 || c.Defaults.DanmakuDensity > 1 {
		errs = append(errs, errors.New("default danmaku style is outside safe limits"))
	}
	if c.Limits.MaxClipDuration <= 0 || c.Limits.AbsoluteMaxClipDuration < c.Limits.MaxClipDuration {
		errs = append(errs, errors.New("clip duration limits are inconsistent"))
	}
	if c.Limits.BiliJobsPerMIDDay < 1 || c.Limits.UserConcurrentJobs < 1 || c.Limits.VideoJobsPerHour < 1 || c.Limits.UserCooldown < time.Second {
		errs = append(errs, errors.New("mention abuse limits must be positive"))
	}
	if c.Limits.PublicJobsPerIPHour < 1 || c.Limits.ArtifactRetention <= 0 || c.Limits.MinFreeDiskBytes < 1 {
		errs = append(errs, errors.New("public rate limit, artifact retention and disk threshold must be positive"))
	}
	if c.Limits.ArtifactQuotaBytes < uint64(c.Media.MaxGIFBytes) {
		errs = append(errs, errors.New("artifact quota must be at least max_gif_bytes"))
	}
	if c.Bili.PollInterval < time.Second {
		errs = append(errs, errors.New("bili poll interval must be at least one second"))
	}
	if c.Bili.MaxPublishPerDay < 1 {
		errs = append(errs, errors.New("bili max publish per day must be positive"))
	}
	if c.Bili.Cookie != "" && len(c.Security.CookieEncryptionKey) != 32 {
		errs = append(errs, errors.New("BILI_COOKIE requires a 32-byte COOKIE_ENCRYPTION_KEY"))
	}
	if len(c.Security.CookieEncryptionKey) != 0 && len(c.Security.CookieEncryptionKey) != 32 {
		errs = append(errs, errors.New("COOKIE_ENCRYPTION_KEY must decode to exactly 32 bytes"))
	}
	if len(c.Security.SessionSecret) != 0 && len(c.Security.SessionSecret) < 32 {
		errs = append(errs, errors.New("SESSION_SECRET must be at least 32 bytes"))
	}
	if c.Security.AdminPasswordHash != "" && len(c.Security.SessionSecret) < 32 {
		errs = append(errs, errors.New("ADMIN_PASSWORD_HASH requires SESSION_SECRET or SESSION_SECRET_FILE"))
	}
	if c.Security.AdminPasswordHash != "" && !strings.HasPrefix(c.Security.AdminPasswordHash, "$argon2id$v=19$") {
		errs = append(errs, errors.New("ADMIN_PASSWORD_HASH must be an Argon2id v19 encoded hash"))
	}
	for _, cidr := range c.Web.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err))
		}
	}
	return errors.Join(errs...)
}

func applyEnv(c *Config) error {
	stringVars := map[string]*string{
		"APP_ADDR": &c.App.Addr, "APP_BASE_URL": &c.App.BaseURL, "APP_TIMEZONE": &c.App.Timezone,
		"APP_LOG_LEVEL": &c.App.LogLevel, "APP_LOG_FORMAT": &c.App.LogFormat,
		"DATA_DIR": &c.Paths.DataDir, "CACHE_DIR": &c.Paths.CacheDir, "TEMP_DIR": &c.Paths.TempDir,
		"DATABASE_PATH": &c.Database.Path, "DATABASE_BUSY_TIMEOUT": &c.Database.BusyText,
		"FFMPEG_PATH": &c.Media.FFmpegPath, "FFPROBE_PATH": &c.Media.FFprobePath, "FONT_FAMILY": &c.Media.FontFamily,
		"JOB_TIMEOUT": &c.Jobs.TimeoutText, "JOB_LEASE_DURATION": &c.Jobs.LeaseText,
		"ARTIFACT_RETENTION": &c.Limits.ArtifactRetentionText, "USER_COOLDOWN": &c.Limits.UserCooldownText,
		"MAX_CLIP_DURATION": &c.Limits.MaxClipDurationText, "ABSOLUTE_MAX_CLIP_DURATION": &c.Limits.AbsoluteMaxText,
		"BILI_POLL_INTERVAL": &c.Bili.PollIntervalText, "BILI_API_BASE_URL": &c.Bili.APIBaseURL,
		"BILI_PASSPORT_BASE_URL": &c.Bili.PassportBaseURL, "BILI_USER_AGENT": &c.Bili.UserAgent,
	}
	for key, dst := range stringVars {
		if value, ok := os.LookupEnv(key); ok {
			*dst = value
		}
	}
	intVars := map[string]*int{
		"FFMPEG_CONCURRENCY": &c.Jobs.FFmpegConcurrency, "WORKER_CONCURRENCY": &c.Jobs.WorkerConcurrency, "BILI_PUBLISH_CONCURRENCY": &c.Jobs.PublishConcurrency,
		"JOB_MAX_ATTEMPTS": &c.Jobs.MaxAttempts,
		"DEFAULT_FPS":      &c.Defaults.FPS, "DEFAULT_WIDTH": &c.Defaults.Width,
		"MIN_FPS": &c.Media.MinFPS, "MIN_WIDTH": &c.Media.MinWidth, "MIN_COLORS": &c.Media.MinColors,
		"MAX_OUTPUT_WIDTH": &c.Limits.MaxOutputWidth, "MAX_OUTPUT_FPS": &c.Limits.MaxOutputFPS,
		"PUBLIC_RATE_LIMIT": &c.Limits.PublicJobsPerIPHour, "BILI_MAX_PUBLISH_PER_DAY": &c.Bili.MaxPublishPerDay,
		"BILI_JOBS_PER_MID_DAY": &c.Limits.BiliJobsPerMIDDay, "USER_CONCURRENT_JOBS": &c.Limits.UserConcurrentJobs,
		"VIDEO_JOBS_PER_HOUR": &c.Limits.VideoJobsPerHour,
	}
	for key, dst := range intVars {
		if value, ok := os.LookupEnv(key); ok {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			*dst = parsed
		}
	}
	if value, ok := os.LookupEnv("MAX_GIF_BYTES"); ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("MAX_GIF_BYTES: %w", err)
		}
		c.Media.MaxGIFBytes = parsed
	}
	if value, ok := os.LookupEnv("MIN_FREE_DISK_BYTES"); ok {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("MIN_FREE_DISK_BYTES: %w", err)
		}
		c.Limits.MinFreeDiskBytes = parsed
	}
	if value, ok := os.LookupEnv("ARTIFACT_QUOTA_BYTES"); ok {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("ARTIFACT_QUOTA_BYTES: %w", err)
		}
		c.Limits.ArtifactQuotaBytes = parsed
	}
	floatVars := map[string]*float64{
		"DEFAULT_DANMAKU_OPACITY":    &c.Defaults.DanmakuOpacity,
		"DEFAULT_DANMAKU_FONT_SCALE": &c.Defaults.DanmakuFontScale,
		"DEFAULT_DANMAKU_DENSITY":    &c.Defaults.DanmakuDensity,
	}
	for key, dst := range floatVars {
		if value, ok := os.LookupEnv(key); ok {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			*dst = parsed
		}
	}
	boolVars := map[string]*bool{
		"PUBLIC_WEB_ENABLED": &c.Web.PublicEnabled, "DEFAULT_DANMAKU": &c.Defaults.Danmaku,
		"BILI_BACKFILL_ON_FIRST_RUN": &c.Bili.BackfillOnFirstRun, "BILI_DRY_RUN": &c.Bili.DryRun,
		"BILI_REPLY_ORIGINAL_COMMENT": &c.Bili.ReplyOriginal,
		"BILI_NOTIFICATION_ENABLED":   &c.Bili.NotificationEnabled,
	}
	if value, ok := os.LookupEnv("TRUSTED_PROXY_CIDRS"); ok {
		c.Web.TrustedProxyCIDRs = nil
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				c.Web.TrustedProxyCIDRs = append(c.Web.TrustedProxyCIDRs, item)
			}
		}
	}
	for key, dst := range boolVars {
		if value, ok := os.LookupEnv(key); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			*dst = parsed
		}
	}
	c.Bili.Cookie = os.Getenv("BILI_COOKIE")
	var err error
	if c.Security.CookieEncryptionKey, err = secretBytes("COOKIE_ENCRYPTION_KEY", "COOKIE_ENCRYPTION_KEY_FILE", c.Security.CookieEncryptionKeyFile, true); err != nil {
		return err
	}
	if c.Security.SessionSecret, err = secretBytes("SESSION_SECRET", "SESSION_SECRET_FILE", c.Security.SessionSecretFile, false); err != nil {
		return err
	}
	if c.Security.AdminPasswordHash, err = secretString("ADMIN_PASSWORD_HASH", "ADMIN_PASSWORD_HASH_FILE", c.Security.AdminPasswordHashFile, c.Security.AdminPasswordHash); err != nil {
		return err
	}
	return nil
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func secretBytes(valueEnv, fileEnv, configuredFile string, decodeBase64 bool) ([]byte, error) {
	value := os.Getenv(valueEnv)
	file := configuredFile
	if v := os.Getenv(fileEnv); v != "" {
		file = v
	}
	if value != "" && file != "" {
		return nil, fmt.Errorf("set only one of %s and %s", valueEnv, fileEnv)
	}
	if file != "" {
		// #nosec G703 -- secret file paths are trusted startup configuration, never public request input.
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", fileEnv, err)
		}
		value = strings.TrimSpace(string(data))
	}
	if value == "" {
		return nil, nil
	}
	if decodeBase64 {
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", valueEnv, err)
		}
		return data, nil
	}
	return []byte(value), nil
}

func secretString(valueEnv, fileEnv, configuredFile, fallback string) (string, error) {
	value := os.Getenv(valueEnv)
	file := configuredFile
	if v := os.Getenv(fileEnv); v != "" {
		file = v
	}
	if value != "" && file != "" {
		return "", fmt.Errorf("set only one of %s and %s", valueEnv, fileEnv)
	}
	if file != "" {
		// #nosec G703 -- secret file paths are trusted startup configuration, never public request input.
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileEnv, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if value != "" {
		return value, nil
	}
	return fallback, nil
}
