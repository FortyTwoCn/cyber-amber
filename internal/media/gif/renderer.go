package gif

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffmpeg"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffprobe"
	"github.com/oklog/ulid/v2"
)

type Request struct {
	JobID, Input, OutputDir, ASSPath, FontDir, UserAgent, Referer string
	Start, Duration                                               time.Duration
	Params                                                        domain.RenderParams
	MaxBytes                                                      int64
	MinFPS, MinWidth, MinColors                                   int
	ExpiresAt                                                     time.Time
}
type Renderer struct {
	runner *ffmpeg.Runner
	prober *ffprobe.Prober
}

func New(runner *ffmpeg.Runner, prober *ffprobe.Prober) *Renderer {
	return &Renderer{runner: runner, prober: prober}
}

func (r *Renderer) Render(ctx context.Context, request Request) (*domain.Artifact, error) {
	if err := os.MkdirAll(request.OutputDir, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	actual := request.Params
	if actual.Colors == 0 {
		actual.Colors = 256
	}
	if actual.Dither == "" {
		actual.Dither = "sierra2_4a"
	}
	artifactID := ulid.Make().String()
	output := filepath.Join(request.OutputDir, artifactID+".gif")
	palette := filepath.Join(request.OutputDir, artifactID+"-palette.png")
	succeeded := false
	defer func() {
		_ = os.Remove(palette)
		if !succeeded {
			_ = os.Remove(output)
		}
	}()
	attempts := 0
	var originalSize int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempts++
		_ = os.Remove(output)
		spec := ffmpeg.RenderSpec{Input: request.Input, Start: request.Start, Duration: request.Duration, FPS: actual.FPS, Width: actual.Width, Colors: actual.Colors, Dither: actual.Dither, ASSPath: request.ASSPath, FontDir: request.FontDir, PalettePath: palette, OutputPath: output, UserAgent: request.UserAgent, Referer: request.Referer}
		paletteArgs, err := ffmpeg.PaletteArgs(spec)
		if err != nil {
			return nil, err
		}
		if _, err = r.runner.Run(ctx, paletteArgs...); err != nil {
			return nil, err
		}
		gifArgs, err := ffmpeg.GIFArgs(spec)
		if err != nil {
			return nil, err
		}
		if _, err = r.runner.Run(ctx, gifArgs...); err != nil {
			return nil, err
		}
		stat, err := os.Stat(output)
		if err != nil {
			return nil, fmt.Errorf("stat rendered GIF: %w", err)
		}
		if originalSize == 0 {
			originalSize = stat.Size()
		}
		if stat.Size() <= request.MaxBytes {
			info, err := r.prober.Probe(ctx, output)
			if err != nil {
				return nil, err
			}
			if err := ffprobe.VerifyGIF(info, request.Duration, actual.Width, actual.FPS); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(output)
			if err != nil {
				return nil, fmt.Errorf("read rendered GIF: %w", err)
			}
			if !ffprobe.HasInfiniteLoop(data) {
				return nil, errors.New("rendered GIF is not configured for infinite loop")
			}
			sum := sha256.Sum256(data)
			succeeded = true
			return &domain.Artifact{ID: artifactID, JobID: request.JobID, Path: output, SHA256: hex.EncodeToString(sum[:]), SizeBytes: stat.Size(), Width: info.Width, Height: info.Height, FrameCount: info.FrameCount, Duration: info.Duration, Requested: request.Params, Final: actual, OriginalSizeBytes: originalSize, CompressionAttempts: attempts - 1, Degraded: attempts > 1, ExpiresAt: request.ExpiresAt, CreatedAt: time.Now().UTC()}, nil
		}
		next, ok := degrade(actual, request.MinFPS, request.MinWidth, request.MinColors)
		if !ok {
			_ = os.Remove(output)
			return nil, fmt.Errorf("GIF_TOO_LARGE: 最低质量下仍超过 %d 字节", request.MaxBytes)
		}
		actual = next
	}
}

func degrade(p domain.RenderParams, minFPS, minWidth, minColors int) (domain.RenderParams, bool) {
	if p.FPS > minFPS {
		p.FPS = max(minFPS, p.FPS-2)
		return p, true
	}
	if p.Width > minWidth {
		width := int(float64(p.Width) * .85)
		if width%2 != 0 {
			width--
		}
		p.Width = max(minWidth, width)
		if p.Width%2 != 0 {
			p.Width--
		}
		p.Resolution = ""
		return p, true
	}
	if p.Colors > minColors {
		p.Colors = max(minColors, p.Colors/2)
		return p, true
	}
	if p.Dither != "bayer" {
		p.Dither = "bayer"
		return p, true
	}
	return p, false
}
