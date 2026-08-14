package ass

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
)

type Style struct {
	FontFamily     string
	FontScale      float64
	Opacity        float64
	Density        float64
	Outline        float64
	ScrollDuration time.Duration
	FixedDuration  time.Duration
	Reverse        bool
}
type Stats struct {
	Input               int
	Rendered            int
	DroppedDensity      int
	FilteredUnsupported int
}
type placed struct {
	item  danmaku.Item
	track int
	width float64
	size  float64
	end   time.Duration
}
type occupancy struct {
	start, end time.Duration
	width      float64
	duration   time.Duration
	mode       int32
}

func Generate(items []danmaku.Item, width, height int, clipDuration time.Duration, style Style) ([]byte, Stats, error) {
	if width < 2 || height < 2 || clipDuration <= 0 {
		return nil, Stats{}, fmt.Errorf("invalid ASS canvas or duration")
	}
	if style.FontFamily == "" {
		style.FontFamily = "Noto Sans CJK SC"
	}
	if style.FontScale <= 0 {
		style.FontScale = 1
	}
	if style.Opacity <= 0 || style.Opacity > 1 {
		style.Opacity = .85
	}
	if style.Density <= 0 || style.Density > 1 {
		style.Density = 1
	}
	if style.Outline <= 0 {
		style.Outline = 2
	}
	if style.ScrollDuration <= 0 {
		style.ScrollDuration = 8 * time.Second
	}
	if style.FixedDuration <= 0 {
		style.FixedDuration = 4 * time.Second
	}
	copyItems := append([]danmaku.Item(nil), items...)
	sort.SliceStable(copyItems, func(i, j int) bool {
		if copyItems[i].At == copyItems[j].At {
			if copyItems[i].Weight == copyItems[j].Weight {
				return copyItems[i].ID < copyItems[j].ID
			}
			return copyItems[i].Weight > copyItems[j].Weight
		}
		return copyItems[i].At < copyItems[j].At
	})
	baseFont := float64(height) / 18 * style.FontScale
	if baseFont < 16 {
		baseFont = 16
	}
	lineHeight := baseFont * 1.25
	maxTracks := int(float64(height) * .82 / lineHeight * style.Density)
	if maxTracks < 1 {
		maxTracks = 1
	}
	fixedTracks := max(1, maxTracks/2)
	scrollTracks := make([]occupancy, maxTracks)
	topTracks := make([]occupancy, fixedTracks)
	bottomTracks := make([]occupancy, fixedTracks)
	stats := Stats{Input: len(copyItems)}
	placements := make([]placed, 0, len(copyItems))
	for _, item := range copyItems {
		if strings.TrimSpace(item.Content) == "" {
			stats.FilteredUnsupported++
			continue
		}
		size := baseFont
		if item.FontSize > 0 {
			size *= float64(item.FontSize) / 25
		}
		if size < 12 {
			size = 12
		}
		textWidth := estimateWidth(item.Content, size)
		duration := style.ScrollDuration
		tracks := scrollTracks
		mode := item.Mode
		if mode == 4 || mode == 5 {
			duration = style.FixedDuration
			if mode == 4 {
				tracks = bottomTracks
			} else {
				tracks = topTracks
			}
		} else if mode == 6 && style.Reverse {
			tracks = scrollTracks
		} else if mode < 1 || mode > 3 {
			stats.FilteredUnsupported++
			continue
		}
		end := item.At + duration
		if end > clipDuration {
			end = clipDuration
		}
		if end <= item.At {
			continue
		}
		track := -1
		for index, last := range tracks {
			if available(last, item.At, textWidth, float64(width), duration, mode) {
				track = index
				break
			}
		}
		if track < 0 {
			stats.DroppedDensity++
			continue
		}
		tracks[track] = occupancy{start: item.At, end: end, width: textWidth, duration: duration, mode: mode}
		switch mode {
		case 4:
			bottomTracks = tracks
		case 5:
			topTracks = tracks
		default:
			scrollTracks = tracks
		}
		placements = append(placements, placed{item: item, track: track, width: textWidth, size: size, end: end})
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nScaledBorderAndShadow: yes\nWrapStyle: 2\n\n", width, height)
	textAlpha := alpha(style.Opacity)
	fmt.Fprintf(&out, "[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Danmaku,%s,%.1f,&H%02XFFFFFF,&H%02XFFFFFF,&H%02X000000,&H%02X000000,0,0,0,0,100,100,0,0,1,%.1f,0,7,0,0,0,1\n\n", escapeStyle(style.FontFamily), baseFont, textAlpha, textAlpha, textAlpha, textAlpha, style.Outline)
	out.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, p := range placements {
		y := lineHeight * (float64(p.track) + .65)
		tag := ""
		switch p.item.Mode {
		case 4:
			y = float64(height) - lineHeight*(float64(p.track)+.7)
			tag = fmt.Sprintf(`\an2\pos(%d,%.0f)`, width/2, y)
		case 5:
			tag = fmt.Sprintf(`\an8\pos(%d,%.0f)`, width/2, y)
		case 6:
			tag = fmt.Sprintf(`\move(%.0f,%.0f,%d,%.0f)`, -p.width, y, width, y)
		default:
			tag = fmt.Sprintf(`\move(%d,%.0f,%.0f,%.0f)`, width, y, -p.width, y)
		}
		color := assColor(p.item.Color)
		fmt.Fprintf(&out, "Dialogue: 0,%s,%s,Danmaku,,0,0,0,,{%s\\fs%.1f\\c%s\\alpha&H%02X&}%s\n", assTime(p.item.At), assTime(p.end), tag, p.size, color, textAlpha, EscapeText(p.item.Content))
	}
	stats.Rendered = len(placements)
	return out.Bytes(), stats, nil
}

func available(last occupancy, at time.Duration, newWidth, screenWidth float64, duration time.Duration, mode int32) bool {
	if last.end == 0 || at >= last.end {
		return true
	}
	if mode == 4 || mode == 5 {
		return false
	}
	// Opposite-direction comments share the same visual lane, but their
	// trajectories necessarily cross while both are active. Reserve the lane
	// until the previous one exits instead of applying the same-direction speed
	// formula to a head-on collision.
	if (last.mode == 6) != (mode == 6) {
		return false
	}
	elapsed := at - last.start
	if elapsed <= 0 {
		return false
	}
	oldSpeed := (screenWidth + last.width) / last.duration.Seconds()
	oldRight := screenWidth - oldSpeed*elapsed.Seconds() + last.width
	if oldRight > screenWidth {
		return false
	}
	newSpeed := (screenWidth + newWidth) / duration.Seconds()
	if newSpeed <= oldSpeed {
		return true
	}
	remaining := last.end - at
	gap := screenWidth - oldRight
	return gap+(oldSpeed-newSpeed)*remaining.Seconds() >= 0
}

func EscapeText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "{", "\\{")
	value = strings.ReplaceAll(value, "}", "\\}")
	return value
}
func escapeStyle(value string) string {
	return strings.NewReplacer(",", " ", "\n", " ", "\r", " ").Replace(value)
}
func estimateWidth(value string, size float64) float64 {
	units := 0.0
	for _, r := range value {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || r > 0x1F000 {
			units += 1
		} else {
			units += .56
		}
	}
	return math.Max(size, units*size)
}
func alpha(opacity float64) int {
	value := int(math.Round((1 - opacity) * 255))
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}
func assColor(rgb uint32) string {
	return fmt.Sprintf("&H%02X%02X%02X&", rgb&0xff, (rgb>>8)&0xff, (rgb>>16)&0xff)
}
func assTime(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	centis := value.Milliseconds() / 10
	h := centis / 360000
	centis %= 360000
	m := centis / 6000
	centis %= 6000
	s := centis / 100
	cs := centis % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}
