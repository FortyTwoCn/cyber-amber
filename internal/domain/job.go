package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type JobSource string

const (
	SourceMention JobSource = "bili_mention"
	SourceWeb     JobSource = "web"
	SourceAdmin   JobSource = "admin"
)

type JobStatus string

const (
	JobQueued          JobStatus = "queued"
	JobResolvingVideo  JobStatus = "resolving_video"
	JobFetchingStream  JobStatus = "fetching_stream"
	JobFetchingDanmaku JobStatus = "fetching_danmaku"
	JobRendering       JobStatus = "rendering"
	JobOptimizing      JobStatus = "optimizing"
	JobUploading       JobStatus = "uploading"
	JobPublishing      JobStatus = "publishing"
	JobVerifying       JobStatus = "verifying"
	JobSucceeded       JobStatus = "succeeded"
	JobFailed          JobStatus = "failed"
	JobCancelled       JobStatus = "cancelled"
	JobInterrupted     JobStatus = "interrupted"
)

func (s JobStatus) Terminal() bool { return s == JobSucceeded || s == JobFailed || s == JobCancelled }

var allowedTransitions = map[JobStatus]map[JobStatus]bool{
	JobQueued:          {JobResolvingVideo: true, JobCancelled: true},
	JobResolvingVideo:  {JobFetchingStream: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobFetchingStream:  {JobFetchingDanmaku: true, JobRendering: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobFetchingDanmaku: {JobRendering: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobRendering:       {JobOptimizing: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobOptimizing:      {JobUploading: true, JobSucceeded: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobUploading:       {JobPublishing: true, JobFailed: true, JobCancelled: true, JobInterrupted: true},
	JobPublishing:      {JobVerifying: true, JobFailed: true, JobInterrupted: true},
	JobVerifying:       {JobSucceeded: true, JobFailed: true, JobInterrupted: true},
	JobInterrupted:     {JobQueued: true, JobFailed: true, JobCancelled: true},
	JobFailed:          {JobQueued: true},
}

func ValidateTransition(from, to JobStatus) error {
	if from == to {
		return nil
	}
	if !allowedTransitions[from][to] {
		return fmt.Errorf("invalid job transition %s -> %s", from, to)
	}
	return nil
}

type RenderParams struct {
	FPS              int     `json:"fps"`
	Width            int     `json:"width"`
	Resolution       string  `json:"resolution,omitempty"`
	Danmaku          bool    `json:"danmaku"`
	DanmakuOpacity   float64 `json:"danmaku_opacity"`
	DanmakuFontScale float64 `json:"danmaku_font_scale"`
	DanmakuDensity   float64 `json:"danmaku_density"`
	Colors           int     `json:"colors"`
	Dither           string  `json:"dither"`
}

type Job struct {
	ID                string        `json:"id"`
	Source            JobSource     `json:"source"`
	CreatorMID        int64         `json:"creator_mid,omitempty"`
	CreatorName       string        `json:"creator_name,omitempty"`
	AnonymousID       string        `json:"-"`
	NotificationID    string        `json:"notification_id,omitempty"`
	SourceCommentRPID int64         `json:"source_comment_rpid,omitempty"`
	Aid               int64         `json:"aid,omitempty"`
	BVID              string        `json:"bvid,omitempty"`
	CID               int64         `json:"cid,omitempty"`
	Page              int           `json:"page"`
	Start             time.Duration `json:"-"`
	End               time.Duration `json:"-"`
	Requested         RenderParams  `json:"requested"`
	Final             RenderParams  `json:"final"`
	DedupeKey         string        `json:"-"`
	Status            JobStatus     `json:"status"`
	Progress          int           `json:"progress"`
	Attempts          int           `json:"attempts"`
	MaxAttempts       int           `json:"max_attempts"`
	AvailableAt       time.Time     `json:"-"`
	LeaseOwner        string        `json:"-"`
	LeaseUntil        *time.Time    `json:"-"`
	CancelRequested   bool          `json:"cancel_requested"`
	ErrorCode         string        `json:"error_code,omitempty"`
	UserError         string        `json:"error_message,omitempty"`
	Diagnostic        string        `json:"-"`
	TempDir           string        `json:"-"`
	ArtifactID        string        `json:"artifact_id,omitempty"`
	ArtifactPath      string        `json:"-"`
	ArtifactSHA256    string        `json:"sha256,omitempty"`
	ArtifactBytes     int64         `json:"artifact_bytes,omitempty"`
	BiliImageURL      string        `json:"bili_image_url,omitempty"`
	PublishedRPID     int64         `json:"published_rpid,omitempty"`
	PublishStatus     string        `json:"publish_status,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	StartedAt         *time.Time    `json:"started_at,omitempty"`
	CompletedAt       *time.Time    `json:"completed_at,omitempty"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type JobView struct {
	Job
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

func (j Job) View() JobView {
	return JobView{Job: j, StartMS: j.Start.Milliseconds(), EndMS: j.End.Milliseconds()}
}

func (j *Job) Validate(maxDuration time.Duration, maxFPS, maxWidth int) error {
	var errs []error
	if j.Source != SourceMention && j.Source != SourceWeb && j.Source != SourceAdmin {
		errs = append(errs, errors.New("invalid source"))
	}
	if j.End <= j.Start {
		errs = append(errs, errors.New("end time must be after start time"))
	}
	if j.Start < 0 || j.End-j.Start > maxDuration {
		errs = append(errs, errors.New("clip duration exceeds limit"))
	}
	if j.Page < 1 {
		errs = append(errs, errors.New("page must be positive"))
	}
	if j.Requested.FPS < 1 || j.Requested.FPS > maxFPS {
		errs = append(errs, errors.New("fps outside allowed range"))
	}
	if j.Requested.Width < 1 || j.Requested.Width > maxWidth || j.Requested.Width%2 != 0 {
		errs = append(errs, errors.New("width outside allowed range or not even"))
	}
	return errors.Join(errs...)
}

func MakeDedupeKey(bvid string, cid int64, start, end time.Duration, p RenderParams, rendererVersion string) string {
	payload, _ := json.Marshal(struct {
		BVID       string `json:"bvid"`
		CID        int64  `json:"cid"`
		Start, End int64
		Params     RenderParams
		Renderer   string
	}{bvid, cid, start.Milliseconds(), end.Milliseconds(), p, rendererVersion})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type Artifact struct {
	ID                  string
	JobID               string
	Path                string
	SHA256              string
	SizeBytes           int64
	Width               int
	Height              int
	FrameCount          int
	Duration            time.Duration
	Requested           RenderParams
	Final               RenderParams
	OriginalSizeBytes   int64
	CompressionAttempts int
	Degraded            bool
	ExpiresAt           time.Time
	CreatedAt           time.Time
}
