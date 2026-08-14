package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTypedFFmpegArgs(t *testing.T) {
	s := RenderSpec{Input: "https://x.bilivideo.com/v?deadline=secret", Start: 10 * time.Second, Duration: 5 * time.Second, FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a", ASSPath: "/tmp/job/a.ass", PalettePath: "/tmp/job/p.png", OutputPath: "/tmp/job/o.gif", UserAgent: "ua", Referer: "https://www.bilibili.com/"}
	palette, err := PaletteArgs(s)
	if err != nil {
		t.Fatal(err)
	}
	gif, err := GIFArgs(s)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(append(palette, gif...), " ")
	for _, want := range []string{"-ss 10.000", "-t 5.000", "fps=10", "scale=640:-2:flags=lanczos", "palettegen=max_colors=256", "paletteuse=dither=sierra2_4a", "subtitles=filename="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q: %s", want, joined)
		}
	}
	if strings.Contains(redactedSummary("ffmpeg", gif), "secret") {
		t.Fatal("signed URL not redacted")
	}
}

func TestRejectUnvalidatedDither(t *testing.T) {
	_, err := GIFArgs(RenderSpec{Input: "in", Start: 0, Duration: time.Second, FPS: 10, Width: 640, Colors: 256, Dither: "x;movie=evil", PalettePath: "p", OutputPath: "o"})
	if err == nil {
		t.Fatal("expected error")
	}
}
