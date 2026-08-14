package ass

import (
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
)

func TestEscapeText(t *testing.T) {
	got := EscapeText(`a{b}\c}` + "\x00\n下一行")
	if got != `a\{b\}\\c\}  下一行` {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateModesAndDeterministicLayout(t *testing.T) {
	items := []danmaku.Item{{ID: 2, At: time.Second, Mode: 1, FontSize: 25, Color: 0xff0000, Content: "滚动"}, {ID: 1, At: time.Second, Mode: 5, FontSize: 25, Color: 0xffffff, Content: "顶部"}, {ID: 3, At: 2 * time.Second, Mode: 4, FontSize: 25, Color: 0x00ff00, Content: "底部"}, {ID: 4, At: 3 * time.Second, Mode: 7, Content: "高级"}}
	style := Style{FontFamily: "Noto Sans CJK SC", Opacity: .8, Density: 1}
	a, stats, err := Generate(items, 640, 360, 10*time.Second, style)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Generate(items, 640, 360, 10*time.Second, style)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("layout is nondeterministic")
	}
	text := string(a)
	for _, want := range []string{`\move(`, `\an8\pos`, `\an2\pos`, "&H0000FF&"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if stats.Rendered != 3 || stats.FilteredUnsupported != 1 {
		t.Fatalf("unexpected stats %#v", stats)
	}
}

func TestOpacityAppliesToTextAndOppositeDirectionsDoNotCross(t *testing.T) {
	items := []danmaku.Item{
		{ID: 1, At: 0, Mode: 1, Content: "normal"},
		{ID: 2, At: time.Second, Mode: 6, Content: "reverse"},
	}
	content, stats, err := Generate(items, 320, 20, 5*time.Second, Style{Opacity: .5, Density: 1, Reverse: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `\alpha&H80&`) {
		t.Fatalf("text alpha was not emitted: %s", content)
	}
	if stats.Rendered != 1 || stats.DroppedDensity != 1 {
		t.Fatalf("opposite trajectories overlapped: %#v", stats)
	}
}

func TestCollisionDropsWhenDensityExceeded(t *testing.T) {
	items := make([]danmaku.Item, 20)
	for i := range items {
		items[i] = danmaku.Item{ID: int64(i), At: 0, Mode: 5, Content: "同时顶部弹幕"}
	}
	_, stats, err := Generate(items, 320, 100, 5*time.Second, Style{Density: .2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.DroppedDensity == 0 {
		t.Fatal("expected density drops")
	}
}
