package gif

import (
	"testing"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
)

func TestAdaptiveDegradeOrder(t *testing.T) {
	p := domain.RenderParams{FPS: 10, Width: 640, Resolution: "360p", Colors: 256, Dither: "sierra2_4a"}
	var stages []domain.RenderParams
	for {
		next, ok := degrade(p, 5, 320, 64)
		if !ok {
			break
		}
		stages = append(stages, next)
		p = next
	}
	if len(stages) < 6 {
		t.Fatalf("too few stages: %d", len(stages))
	}
	seenWidth := false
	seenColors := false
	for _, s := range stages {
		if s.Width < 640 {
			seenWidth = true
			if s.Resolution != "" {
				t.Fatal("degraded width retained a misleading resolution label")
			}
			if s.FPS != 5 {
				t.Fatal("width reduced before fps minimum")
			}
		}
		if s.Colors < 256 {
			seenColors = true
			if s.Width != 320 {
				t.Fatal("colors reduced before width minimum")
			}
		}
	}
	if !seenWidth || !seenColors || stages[len(stages)-1].Dither != "bayer" {
		t.Fatalf("bad stages %#v", stages)
	}
}
