package gif

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ass"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffmpeg"
	"github.com/FortyTwoCn/cyber-amber/internal/media/ffprobe"
)

func TestMediaPipelineWithAndWithoutDanmaku(t *testing.T) {
	if os.Getenv("MEDIA_E2E") != "true" {
		t.Skip("set MEDIA_E2E=true when ffmpeg/ffprobe are installed")
	}
	ffmpegPath, err := exec.LookPath(envOr("FFMPEG_PATH", "ffmpeg"))
	if err != nil {
		t.Fatal(err)
	}
	ffprobePath, err := exec.LookPath(envOr("FFPROBE_PATH", "ffprobe"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate source: %v: %s", err, output)
	}
	runner := ffmpeg.NewRunner(ffmpegPath, 1)
	prober := ffprobe.New(ffprobePath)
	renderer := New(runner, prober)
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a", DanmakuOpacity: .85, DanmakuFontScale: 1, DanmakuDensity: 1}
	for _, withDanmaku := range []bool{false, true} {
		assPath := ""
		if withDanmaku {
			content, stats, err := ass.Generate([]danmaku.Item{{ID: 1, At: 500 * time.Millisecond, Mode: 1, FontSize: 25, Color: 0xffb02e, Content: "中文弹幕 {测试} ✦"}, {ID: 2, At: time.Second, Mode: 5, FontSize: 25, Color: 0xffffff, Content: "顶部"}}, 640, 360, 3*time.Second, ass.Style{FontFamily: "Noto Sans CJK SC", Opacity: .85, Density: 1})
			if err != nil || stats.Rendered != 2 {
				t.Fatalf("ASS: %v %#v", err, stats)
			}
			assPath = filepath.Join(dir, "danmaku.ass")
			if err := os.WriteFile(assPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		artifact, err := renderer.Render(ctx, Request{JobID: "media-test", Input: source, OutputDir: filepath.Join(dir, "artifacts"), ASSPath: assPath, Start: 500 * time.Millisecond, Duration: 3 * time.Second, Params: params, MaxBytes: 5 << 20, MinFPS: 5, MinWidth: 320, MinColors: 64, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("withDanmaku=%v: %v", withDanmaku, err)
		}
		if artifact.Width != 640 || artifact.Duration < 2800*time.Millisecond || artifact.FrameCount < 25 || artifact.SizeBytes == 0 {
			t.Fatalf("bad artifact %#v", artifact)
		}
		t.Logf("with_danmaku=%v bytes=%d frames=%d", withDanmaku, artifact.SizeBytes, artifact.FrameCount)
	}
	degraded, err := renderer.Render(ctx, Request{JobID: "media-degrade", Input: source, OutputDir: filepath.Join(dir, "degraded"), Start: 500 * time.Millisecond, Duration: 3 * time.Second, Params: params, MaxBytes: 750000, MinFPS: 5, MinWidth: 320, MinColors: 64, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !degraded.Degraded || degraded.SizeBytes > 750000 || degraded.Final.FPS >= params.FPS || degraded.Duration < 2800*time.Millisecond {
		t.Fatalf("adaptive degradation failed: %#v", degraded)
	}
}

func TestFFmpegCancellation(t *testing.T) {
	if os.Getenv("MEDIA_E2E") != "true" {
		t.Skip("set MEDIA_E2E=true")
	}
	path, err := exec.LookPath(envOr("FFMPEG_PATH", "ffmpeg"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = ffmpeg.NewRunner(path, 1).Run(ctx, "-hide_banner", "-loglevel", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30", "-t", "30", "-f", "null", "-")
	if err == nil {
		t.Fatal("expected cancellation")
	}
}
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
