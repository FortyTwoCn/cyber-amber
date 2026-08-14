package client

import (
	"net/url"
	"regexp"
)

var secretPattern = regexp.MustCompile(`(?i)(SESSDATA|bili_jct|DedeUserID|buvid3|buvid4|refresh_token|csrf|session|cookie|access_key|w_rid|deadline|sign|token)(=|%3D)[^&;\s]+`)
var signedURLPattern = regexp.MustCompile(`(https?://[^\s?]+)\?[^\s]+`)

func Redact(value string) string {
	value = signedURLPattern.ReplaceAllString(value, "$1?[REDACTED]")
	value = secretPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	if u, err := url.Parse(value); err == nil && u.Host != "" {
		if u.RawQuery != "" {
			u.RawQuery = ""
			u.Fragment = ""
			return u.String() + "?[REDACTED]"
		}
		return u.String()
	}
	return value
}
