package parser

import (
	"testing"
	"time"
)

func defaultsAndLimits() (Defaults, Limits) {
	return Defaults{FPS: 10, Width: 640, Danmaku: false, DanmakuOpacity: .85, DanmakuFontScale: 1, DanmakuDensity: 1, Page: 1}, Limits{MinFPS: 5, MaxFPS: 20, MinWidth: 320, MaxWidth: 1280, MaxDuration: 30 * time.Second}
}

func TestParseTimeSeparatorsAndAliases(t *testing.T) {
	d, l := defaultsAndLimits()
	tests := []struct {
		in         string
		start, end time.Duration
	}{
		{"@机器人 10:10-10:20", 610 * time.Second, 620 * time.Second},
		{"@甲 @机器人 10：10 ～ 10：20 帧率=12 宽度=640 弹幕=开", 610 * time.Second, 620 * time.Second},
		{"01:10:10至01:10:20 分P=2 分辨率=480p 无弹幕", 4210 * time.Second, 4220 * time.Second},
		{"10:10\u00a0—\u200b10:20", 610 * time.Second, 620 * time.Second},
	}
	for _, tc := range tests {
		got, err := Parse(tc.in, d, l)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got.Start != tc.start || got.End != tc.end {
			t.Fatalf("%q: %s-%s", tc.in, got.Start, got.End)
		}
	}
}

func TestParseParameters(t *testing.T) {
	d, l := defaultsAndLimits()
	got, err := Parse("10:10-10:20 fps=12 resolution=480p danmaku=on p=2", d, l)
	if err != nil {
		t.Fatal(err)
	}
	if got.Params.FPS != 12 || got.Params.Width != 854 || !got.Params.Danmaku || got.Page != 2 {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestInvalidRangeAndBounds(t *testing.T) {
	d, l := defaultsAndLimits()
	for _, input := range []string{"10:20-10:10", "10:10-10:50", "10:10-10:20 fps=99", "10:10-10:20 width=641", "10:99-11:00", "10:10-10:20 width=640 resolution=480p"} {
		if _, err := Parse(input, d, l); err == nil {
			t.Fatalf("%q should fail", input)
		}
	}
}
