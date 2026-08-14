package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var allowedCookies = map[string]bool{"SESSDATA": true, "bili_jct": true, "DedeUserID": true, "buvid3": true, "buvid4": true, "DedeUserID__ckMd5": true, "sid": true}

type Session struct {
	Cookies      map[string]string `json:"cookies"`
	RefreshToken string            `json:"refresh_token,omitempty"`
}

func ParseCookieHeader(value string) (Session, error) {
	session := Session{Cookies: map[string]string{}}
	for _, part := range strings.Split(value, ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || key == "" {
			continue
		}
		if allowedCookies[key] {
			session.Cookies[key] = val
		}
	}
	if err := validateSession(session); err != nil {
		return Session{}, err
	}
	return session, nil
}
func SessionFromLoginURL(value, refreshToken string, setCookies []*http.Cookie) (Session, error) {
	u, err := url.Parse(value)
	if err != nil {
		return Session{}, fmt.Errorf("parse login callback: %w", err)
	}
	cookies := map[string]string{}
	for key, values := range u.Query() {
		for allowed := range allowedCookies {
			if strings.EqualFold(key, allowed) && len(values) > 0 {
				cookies[allowed] = values[0]
			}
		}
	}
	for _, cookie := range setCookies {
		if allowedCookies[cookie.Name] {
			cookies[cookie.Name] = cookie.Value
		}
	}
	session := Session{Cookies: cookies, RefreshToken: refreshToken}
	if err := validateSession(session); err != nil {
		return Session{}, fmt.Errorf("扫码登录响应无效: %w", err)
	}
	return session, nil
}
func (s Session) CookieHeader() string {
	keys := make([]string, 0, len(s.Cookies))
	for key := range s.Cookies {
		if allowedCookies[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+s.Cookies[key])
	}
	return strings.Join(parts, "; ")
}
func (s Session) CSRF() string             { return s.Cookies["bili_jct"] }
func (s Session) Marshal() ([]byte, error) { return json.Marshal(s) }
func UnmarshalSession(data []byte) (Session, error) {
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("decode account session: %w", err)
	}
	if session.Cookies == nil {
		return session, errors.New("account session has no cookies")
	}
	if err := validateSession(session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func validateSession(session Session) error {
	if session.Cookies["SESSDATA"] == "" || session.Cookies["bili_jct"] == "" || session.Cookies["DedeUserID"] == "" {
		return errors.New("账号 Cookie 缺少 SESSDATA、bili_jct 或 DedeUserID")
	}
	for key, value := range session.Cookies {
		if !allowedCookies[key] {
			return fmt.Errorf("账号 Cookie 包含未允许字段 %q", key)
		}
		if len(value) > 8192 || strings.ContainsAny(value, "\r\n;") {
			return fmt.Errorf("账号 Cookie 字段 %q 包含非法字符或过长", key)
		}
	}
	mid, err := strconv.ParseInt(session.Cookies["DedeUserID"], 10, 64)
	if err != nil || mid <= 0 {
		return errors.New("账号 Cookie 的 DedeUserID 无效")
	}
	return nil
}
