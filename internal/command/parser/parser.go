package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
)

var (
	rangeRE = regexp.MustCompile(`(?i)(\d{1,3}(?::\d{1,2}){1,2})\s*(?:-|~|～|—|–|至)\s*(\d{1,3}(?::\d{1,2}){1,2})`)
	paramRE = regexp.MustCompile(`(?i)(fps|width|resolution|danmaku|p|帧率|宽度|分辨率|弹幕|分[pP])\s*=\s*([^\s,，;；]+)`)
)

type Limits struct {
	MinFPS      int
	MaxFPS      int
	MinWidth    int
	MaxWidth    int
	MaxDuration time.Duration
}

type Defaults struct {
	FPS              int
	Width            int
	Danmaku          bool
	DanmakuOpacity   float64
	DanmakuFontScale float64
	DanmakuDensity   float64
	Page             int
}

type Command struct {
	Start  time.Duration
	End    time.Duration
	Page   int
	Params domain.RenderParams
}

type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

func Parse(input string, defaults Defaults, limits Limits) (Command, error) {
	input = normalize(input)
	match := rangeRE.FindStringSubmatch(input)
	if len(match) != 3 {
		return Command{}, &Error{Code: "INVALID_TIME_RANGE", Message: "未找到时间范围，请使用 10:10-10:20 或 01:10:10-01:10:20"}
	}
	start, err := parseClock(match[1])
	if err != nil {
		return Command{}, &Error{Code: "INVALID_TIME_RANGE", Message: "开始时间格式无效"}
	}
	end, err := parseClock(match[2])
	if err != nil {
		return Command{}, &Error{Code: "INVALID_TIME_RANGE", Message: "结束时间格式无效"}
	}
	if end <= start {
		return Command{}, &Error{Code: "INVALID_TIME_RANGE", Message: "结束时间必须晚于开始时间"}
	}
	if end-start > limits.MaxDuration {
		return Command{}, &Error{Code: "CLIP_TOO_LONG", Message: fmt.Sprintf("剪辑时长不能超过 %s", limits.MaxDuration)}
	}
	page := defaults.Page
	if page < 1 {
		page = 1
	}
	p := domain.RenderParams{FPS: defaults.FPS, Width: defaults.Width, Danmaku: defaults.Danmaku, DanmakuOpacity: defaults.DanmakuOpacity, DanmakuFontScale: defaults.DanmakuFontScale, DanmakuDensity: defaults.DanmakuDensity, Colors: 256, Dither: "sierra2_4a"}
	widthExplicit := false
	for _, item := range paramRE.FindAllStringSubmatch(input, -1) {
		key, value := strings.ToLower(item[1]), strings.ToLower(item[2])
		switch key {
		case "fps", "帧率":
			parsed, e := strconv.Atoi(value)
			if e != nil {
				return Command{}, badParam("帧率必须是整数")
			}
			p.FPS = parsed
		case "width", "宽度":
			parsed, e := strconv.Atoi(value)
			if e != nil {
				return Command{}, badParam("宽度必须是整数")
			}
			p.Width, widthExplicit = parsed, true
		case "resolution", "分辨率":
			width, ok := map[string]int{"360p": 640, "480p": 854, "720p": 1280}[value]
			if !ok {
				return Command{}, badParam("分辨率仅支持 360p、480p、720p")
			}
			p.Resolution = value
			if !widthExplicit {
				p.Width = width
			}
		case "danmaku", "弹幕":
			enabled, ok := parseToggle(value)
			if !ok {
				return Command{}, badParam("弹幕参数仅支持 on/off/开/关")
			}
			p.Danmaku = enabled
		case "p", "分p":
			parsed, e := strconv.Atoi(value)
			if e != nil || parsed < 1 {
				return Command{}, badParam("分P必须是正整数")
			}
			page = parsed
		}
	}
	if strings.Contains(input, "无弹幕") {
		p.Danmaku = false
	} else if strings.Contains(input, "有弹幕") {
		p.Danmaku = true
	}
	if p.Resolution != "" && widthExplicit {
		expected := map[string]int{"360p": 640, "480p": 854, "720p": 1280}[p.Resolution]
		if p.Width != expected {
			return Command{}, badParam("宽度与分辨率参数冲突，请只设置一个或使用匹配宽度")
		}
	}
	if p.FPS < limits.MinFPS || p.FPS > limits.MaxFPS {
		return Command{}, badParam(fmt.Sprintf("帧率范围为 %d-%d", limits.MinFPS, limits.MaxFPS))
	}
	if p.Width < limits.MinWidth || p.Width > limits.MaxWidth || p.Width%2 != 0 {
		return Command{}, badParam(fmt.Sprintf("宽度必须是 %d-%d 范围内的偶数", limits.MinWidth, limits.MaxWidth))
	}
	return Command{Start: start, End: end, Page: page, Params: p}, nil
}

func normalize(value string) string {
	value = strings.NewReplacer("：", ":", "＝", "=", "\u00a0", " ", "\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "").Replace(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value)
}

func parseClock(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, errors.New("clock must have two or three components")
	}
	values := make([]int64, len(parts))
	for i, part := range parts {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return 0, errors.New("invalid clock component")
		}
		values[i] = n
	}
	var h, m, s int64
	if len(values) == 2 {
		m, s = values[0], values[1]
	} else {
		h, m, s = values[0], values[1], values[2]
		if m >= 60 {
			return 0, errors.New("minute outside range")
		}
	}
	if s >= 60 {
		return 0, errors.New("second outside range")
	}
	return time.Duration(h*3600+m*60+s) * time.Second, nil
}

func parseToggle(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "on", "true", "1", "开", "有":
		return true, true
	case "off", "false", "0", "关", "无":
		return false, true
	default:
		return false, false
	}
}

func badParam(message string) error { return &Error{Code: "INVALID_PARAMETER", Message: message} }
