package fonts

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func Check(ctx context.Context, family string) error {
	if family == "" {
		return fmt.Errorf("font family is empty")
	}
	cmd := exec.CommandContext(ctx, "fc-match", "--format=%{family}", family)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fontconfig/fc-match unavailable: %w: %s", err, string(output))
	}
	matched := strings.TrimSpace(string(output))
	if matched == "" {
		return fmt.Errorf("fontconfig returned no font for %q", family)
	}
	wanted := strings.ToLower(family)
	if !strings.Contains(strings.ToLower(matched), wanted) && strings.Contains(wanted, "cjk") {
		return fmt.Errorf("required CJK font %q not found; matched %q", family, matched)
	}
	cmd = exec.CommandContext(ctx, "fc-match", "--format=%{charset}", family)
	charset, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect font charset: %w: %s", err, string(charset))
	}
	if !charsetContains(string(charset), 0x4e2d) {
		return fmt.Errorf("font %q does not contain a representative Chinese glyph", family)
	}
	return nil
}

func charsetContains(charset string, target int64) bool {
	for _, token := range strings.Fields(charset) {
		startText, endText, ranged := strings.Cut(token, "-")
		start, err := strconv.ParseInt(startText, 16, 32)
		if err != nil {
			continue
		}
		end := start
		if ranged {
			end, err = strconv.ParseInt(endText, 16, 32)
			if err != nil {
				continue
			}
		}
		if target >= start && target <= end {
			return true
		}
	}
	return false
}
