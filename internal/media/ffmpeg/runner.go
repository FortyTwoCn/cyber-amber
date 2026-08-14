package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	biliclient "github.com/FortyTwoCn/cyber-amber/internal/bili/client"
)

type Runner struct {
	path     string
	sem      chan struct{}
	mu       sync.RWMutex
	observer func(time.Duration)
}

func (r *Runner) SetObserver(observer func(time.Duration)) {
	r.mu.Lock()
	r.observer = observer
	r.mu.Unlock()
}

func NewRunner(path string, concurrency int) *Runner {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Runner{path: path, sem: make(chan struct{}, concurrency)}
}

type CommandError struct {
	Command string
	Stderr  string
	Cause   error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("ffmpeg failed: %v; command: %s; stderr: %s", e.Cause, e.Command, e.Stderr)
}
func (e *CommandError) Unwrap() error { return e.Cause }

func (r *Runner) Run(ctx context.Context, args ...string) (time.Duration, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, r.path, args...)
	configureProcess(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd.Process) }
	cmd.WaitDelay = 3 * time.Second
	tail := newTailBuffer(32 << 10)
	cmd.Stderr = tail
	cmd.Stdout = io.Discard
	started := time.Now()
	err := cmd.Run()
	elapsed := time.Since(started)
	r.mu.RLock()
	observer := r.observer
	r.mu.RUnlock()
	if observer != nil {
		observer(elapsed)
	}
	if err != nil {
		return elapsed, &CommandError{Command: redactedSummary(r.path, args), Stderr: biliclient.Redact(tail.String()), Cause: err}
	}
	return elapsed, nil
}

func redactedSummary(path string, args []string) string {
	safe := make([]string, len(args))
	for i, arg := range args {
		safe[i] = biliclient.Redact(arg)
		if len(safe[i]) > 256 {
			safe[i] = safe[i][:256] + "…"
		}
	}
	return path + " " + strings.Join(safe, " ")
}

type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }
func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.max:]...)
	}
	return len(p), nil
}
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.ToValidUTF8(b.buf, []byte("?")))
}

func (r *Runner) Check(ctx context.Context) (map[int]bool, error) {
	cmd := exec.CommandContext(ctx, r.path, "-hide_banner", "-filters")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run ffmpeg filter check: %w: %s", err, string(output))
	}
	text := string(output)
	missing := make([]string, 0)
	for _, name := range []string{"palettegen", "paletteuse", "subtitles"} {
		if !strings.Contains(text, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("ffmpeg missing required filters: %s", strings.Join(missing, ", "))
	}
	cmd = exec.CommandContext(ctx, r.path, "-hide_banner", "-decoders")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run ffmpeg decoder check: %w", err)
	}
	decoders := string(output)
	supported := map[int]bool{7: strings.Contains(decoders, " h264 "), 12: strings.Contains(decoders, " hevc "), 13: strings.Contains(decoders, " av1 ")}
	if !supported[7] && !supported[12] && !supported[13] {
		return nil, errors.New("ffmpeg has no AVC, HEVC or AV1 video decoder")
	}
	return supported, nil
}
