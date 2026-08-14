package ffmpeg

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type RenderSpec struct {
	Input              string
	Start, Duration    time.Duration
	FPS, Width, Colors int
	Dither             string
	ASSPath            string
	FontDir            string
	PalettePath        string
	OutputPath         string
	UserAgent          string
	Referer            string
}

func PaletteArgs(s RenderSpec) ([]string, error) {
	if err := validate(s); err != nil {
		return nil, err
	}
	filter := baseFilter(s) + fmt.Sprintf(",palettegen=max_colors=%d:stats_mode=full", s.Colors)
	args := inputArgs(s)
	args = append(args, "-vf", filter, "-frames:v", "1", "-y", s.PalettePath)
	return args, nil
}
func GIFArgs(s RenderSpec) ([]string, error) {
	if err := validate(s); err != nil {
		return nil, err
	}
	filter := fmt.Sprintf("[0:v]%s[v];[v][1:v]paletteuse=dither=%s:diff_mode=rectangle[out]", baseFilter(s), ditherValue(s.Dither))
	args := inputArgs(s)
	args = append(args, "-i", s.PalettePath, "-filter_complex", filter, "-map", "[out]", "-loop", "0", "-gifflags", "+transdiff", "-y", s.OutputPath)
	return args, nil
}

func inputArgs(s RenderSpec) []string {
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "warning"}
	if strings.HasPrefix(s.Input, "https://") {
		args = append(args, "-user_agent", s.UserAgent, "-referer", s.Referer, "-rw_timeout", "15000000")
	}
	args = append(args, "-ss", formatSeconds(s.Start), "-t", formatSeconds(s.Duration), "-i", s.Input)
	return args
}
func baseFilter(s RenderSpec) string {
	filter := fmt.Sprintf("fps=%d,scale=%d:-2:flags=lanczos", s.FPS, s.Width)
	if s.ASSPath != "" {
		filter += ",subtitles=filename='" + escapeFilterPath(s.ASSPath) + "'"
		if s.FontDir != "" {
			filter += ":fontsdir='" + escapeFilterPath(s.FontDir) + "'"
		}
	}
	return filter
}
func validate(s RenderSpec) error {
	if s.Input == "" || s.OutputPath == "" || s.PalettePath == "" {
		return errors.New("input, palette and output paths are required")
	}
	if s.Start < 0 || s.Duration <= 0 {
		return errors.New("invalid render time range")
	}
	if s.FPS < 1 || s.FPS > 120 || s.Width < 2 || s.Width%2 != 0 || s.Colors < 4 || s.Colors > 256 {
		return errors.New("render values outside safe range")
	}
	switch s.Dither {
	case "sierra2_4a", "floyd_steinberg", "bayer", "none":
	default:
		return errors.New("unsupported dither")
	}
	return nil
}
func ditherValue(value string) string {
	if value == "bayer" {
		return "bayer:bayer_scale=3"
	}
	return value
}
func formatSeconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 3, 64)
}
func escapeFilterPath(path string) string {
	path = filepath.ToSlash(path)
	replacer := strings.NewReplacer("\\", "\\\\", "'", "\\'", ":", "\\:", "[", "\\[", "]", "\\]", ",", "\\,")
	return replacer.Replace(path)
}
