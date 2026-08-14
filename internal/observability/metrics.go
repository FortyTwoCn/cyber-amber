package observability

import (
	"context"
	"strconv"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/store"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Registry           *prometheus.Registry
	JobsTotal          *prometheus.CounterVec
	JobDuration        prometheus.Histogram
	FFmpegDuration     prometheus.Histogram
	GIFBytes           prometheus.Histogram
	BiliRequests       *prometheus.CounterVec
	BiliPublish        *prometheus.CounterVec
	NotificationErrors prometheus.Counter
}

func NewMetrics(s *store.Store) *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{Registry: registry, JobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cyber_amber_jobs_total", Help: "Job lifecycle outcomes by source, including queued, retrying and terminal states."}, []string{"source", "status"}), JobDuration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "cyber_amber_job_duration_seconds", Help: "End-to-end duration of terminal jobs from persisted creation time.", Buckets: prometheus.DefBuckets}), FFmpegDuration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "cyber_amber_ffmpeg_duration_seconds", Help: "FFmpeg invocation duration.", Buckets: prometheus.DefBuckets}), GIFBytes: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "cyber_amber_gif_bytes", Help: "Final GIF sizes.", Buckets: prometheus.ExponentialBuckets(64*1024, 2, 10)}), BiliRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cyber_amber_bili_requests_total", Help: "Bilibili HTTP requests by operation and outcome."}, []string{"operation", "outcome"}), BiliPublish: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cyber_amber_bili_publish_total", Help: "Bilibili comment publish outcomes."}, []string{"status"}), NotificationErrors: prometheus.NewCounter(prometheus.CounterOpts{Name: "cyber_amber_notification_poll_errors_total", Help: "Notification polling failures."})}
	queue := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cyber_amber_queue_depth", Help: "Persisted queued jobs."}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		value, err := s.QueueDepth(ctx)
		if err != nil {
			return 0
		}
		return float64(value)
	})
	active := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cyber_amber_jobs_active", Help: "Jobs currently executing."}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		value, err := s.ActiveJobs(ctx)
		if err != nil {
			return 0
		}
		return float64(value)
	})
	registry.MustRegister(m.JobsTotal, m.JobDuration, m.FFmpegDuration, m.GIFBytes, m.BiliRequests, m.BiliPublish, m.NotificationErrors, queue, active)
	return m
}
func Outcome(code int) string {
	if code >= 200 && code < 300 {
		return "success"
	}
	return strconv.Itoa(code)
}
