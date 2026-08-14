package ffprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	Format                    string
	Codec                     string
	Width, Height, FrameCount int
	Duration                  time.Duration
	InfiniteLoop              bool
}
type Prober struct{ path string }

func New(path string) *Prober { return &Prober{path: path} }
func (p *Prober) Check(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.path, "-version")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffprobe unavailable: %w: %s", err, string(output))
	}
	return nil
}

func (p *Prober) Probe(ctx context.Context, path string) (Info, error) {
	cmd := exec.CommandContext(ctx, p.path, "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,nb_read_frames,duration:format=duration,format_name", "-of", "json", path)
	output, err := cmd.Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe artifact: %w", err)
	}
	var raw struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Frames   string `json:"nb_read_frames"`
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			Name     string `json:"format_name"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return Info{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	if len(raw.Streams) != 1 {
		return Info{}, errors.New("artifact does not contain exactly one video stream")
	}
	stream := raw.Streams[0]
	frames, _ := strconv.Atoi(stream.Frames)
	durationText := stream.Duration
	if durationText == "" || durationText == "N/A" {
		durationText = raw.Format.Duration
	}
	seconds, err := strconv.ParseFloat(durationText, 64)
	if err != nil {
		return Info{}, fmt.Errorf("invalid probed duration %q: %w", durationText, err)
	}
	return Info{Format: raw.Format.Name, Codec: stream.Codec, Width: stream.Width, Height: stream.Height, FrameCount: frames, Duration: time.Duration(seconds * float64(time.Second))}, nil
}

func VerifyGIF(info Info, expected time.Duration, width, fps int) error {
	var errs []error
	if info.Codec != "gif" || !strings.Contains(info.Format, "gif") {
		errs = append(errs, errors.New("artifact is not GIF"))
	}
	if info.Width != width || info.Height < 2 || info.Height%2 != 0 {
		errs = append(errs, fmt.Errorf("unexpected dimensions %dx%d", info.Width, info.Height))
	}
	if fps < 1 {
		errs = append(errs, errors.New("expected FPS must be positive"))
		fps = 1
	}
	tolerance := max(150*time.Millisecond, time.Second/time.Duration(fps)+50*time.Millisecond)
	if info.Duration < expected-tolerance || info.Duration > expected+tolerance {
		errs = append(errs, fmt.Errorf("duration %s differs from requested %s", info.Duration, expected))
	}
	expectedFrames := int(math.Round(expected.Seconds() * float64(fps)))
	if info.FrameCount < expectedFrames-1 || info.FrameCount > expectedFrames+1 {
		errs = append(errs, fmt.Errorf("frame count %d differs from expected %d at %d FPS", info.FrameCount, expectedFrames, fps))
	}
	return errors.Join(errs...)
}

func HasInfiniteLoop(data []byte) bool {
	return bytes.Contains(data, []byte("NETSCAPE2.0\x03\x01\x00\x00")) || bytes.Contains(data, []byte("ANIMEXTS1.0\x03\x01\x00\x00"))
}
