package input

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	bvidRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(BV1[1-9A-HJ-NP-Za-km-z]{9})(?:$|[^A-Za-z0-9])`)
	avidRE = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])av([1-9][0-9]*)(?:$|[^A-Za-z0-9])`)
	urlRE  = regexp.MustCompile(`https?://[^\s<>"']+`)
)

type Result struct {
	BVID         string
	AID          int64
	Page         int
	CanonicalURL string
}

type Parser struct{ client *http.Client }

func New(client *http.Client) *Parser {
	if client == nil {
		client = NewSafeHTTPClient(10 * time.Second)
	}
	return &Parser{client: client}
}

func (p *Parser) Parse(ctx context.Context, text string) (Result, error) {
	text = strings.TrimSpace(text)
	if match := bvidRE.FindStringSubmatch(text); len(match) == 2 && !strings.Contains(text, "://") {
		return Result{BVID: match[1], Page: 1, CanonicalURL: "https://www.bilibili.com/video/" + match[1]}, nil
	}
	if match := avidRE.FindStringSubmatch(text); len(match) == 2 && !strings.Contains(text, "://") {
		aid, _ := strconv.ParseInt(match[1], 10, 64)
		return Result{AID: aid, Page: 1, CanonicalURL: fmt.Sprintf("https://www.bilibili.com/video/av%d", aid)}, nil
	}
	urls := urlRE.FindAllString(text, -1)
	if len(urls) == 0 {
		return Result{}, errors.New("未找到合法的 BV 号或 B站视频链接")
	}
	var lastErr error
	for _, raw := range urls {
		raw = strings.TrimRight(raw, ".,，。!！?？)）]】")
		result, err := p.parseURL(ctx, raw)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return Result{}, fmt.Errorf("无法解析 B站视频输入: %w", lastErr)
}

func (p *Parser) parseURL(ctx context.Context, raw string) (Result, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Result{}, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return Result{}, errors.New("仅允许 http/https 链接")
	}
	if u.User != nil {
		return Result{}, errors.New("B站链接不允许包含用户信息")
	}
	if !isStandardWebPort(u) {
		return Result{}, errors.New("仅允许使用标准 HTTP/HTTPS 端口的 B站链接")
	}
	host := canonicalHost(u.Hostname())
	if !isInputHost(host) {
		return Result{}, fmt.Errorf("不允许的输入域名 %q", host)
	}
	if host == "b23.tv" {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
		if err != nil {
			return Result{}, err
		}
		resp, err := p.client.Do(req)
		if err != nil {
			return Result{}, fmt.Errorf("解析短链接: %w", err)
		}
		_ = resp.Body.Close()
		u = resp.Request.URL
		host = canonicalHost(u.Hostname())
		if u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !isStandardWebPort(u) || !isBilibiliPageHost(host) {
			return Result{}, errors.New("短链接重定向到了非 B站域名")
		}
	}
	if !isBilibiliPageHost(host) {
		return Result{}, errors.New("链接不是 B站视频页")
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) < 2 || parts[0] != "video" {
		return Result{}, errors.New("仅支持普通 UGC 视频链接")
	}
	id, err := url.PathUnescape(parts[1])
	if err != nil {
		return Result{}, errors.New("视频路径编码无效")
	}
	page := 1
	if value := u.Query().Get("p"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			return Result{}, errors.New("分P参数必须是正整数")
		}
	}
	result := Result{Page: page}
	if match := bvidRE.FindStringSubmatch(id); len(match) == 2 && match[1] == id {
		result.BVID = id
	} else if match := avidRE.FindStringSubmatch(id); len(match) == 2 {
		result.AID, _ = strconv.ParseInt(match[1], 10, 64)
	} else {
		return Result{}, errors.New("视频 ID 格式无效")
	}
	if result.BVID != "" {
		result.CanonicalURL = "https://www.bilibili.com/video/" + result.BVID
	} else {
		result.CanonicalURL = fmt.Sprintf("https://www.bilibili.com/video/av%d", result.AID)
	}
	if page > 1 {
		result.CanonicalURL += fmt.Sprintf("?p=%d", page)
	}
	return result, nil
}

func IsAllowedAPIHost(host string) bool {
	host = canonicalHost(host)
	return host == "api.bilibili.com" || host == "passport.bilibili.com"
}
func IsAllowedCDNHost(host string) bool {
	host = canonicalHost(host)
	return host == "bilivideo.com" || strings.HasSuffix(host, ".bilivideo.com") || host == "hdslb.com" || strings.HasSuffix(host, ".hdslb.com")
}
func IsAllowedCDNURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !isStandardWebPort(u) {
		return false
	}
	return IsAllowedCDNHost(u.Hostname())
}
func IsAllowedImageURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !isStandardWebPort(u) {
		return false
	}
	host := canonicalHost(u.Hostname())
	return host == "hdslb.com" || strings.HasSuffix(host, ".hdslb.com")
}
func isInputHost(host string) bool { return host == "b23.tv" || isBilibiliPageHost(host) }
func isBilibiliPageHost(host string) bool {
	return host == "bilibili.com" || host == "www.bilibili.com" || host == "m.bilibili.com"
}
func canonicalHost(host string) string { return strings.TrimSuffix(strings.ToLower(host), ".") }

func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, MaxIdleConns: 20, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if !isInputHost(canonicalHost(host)) {
				return nil, errors.New("blocked outbound host")
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isForbiddenIP(ip) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			return nil, errors.New("host resolves only to forbidden addresses")
		},
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("too many redirects")
		}
		if !isInputHost(canonicalHost(req.URL.Hostname())) {
			return errors.New("redirect target is not an allowed B站 host")
		}
		if !isStandardWebPort(req.URL) {
			return errors.New("redirect target uses a non-standard port")
		}
		return nil
	}
	return client
}

func isStandardWebPort(u *url.URL) bool {
	port := u.Port()
	return port == "" || (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")
}

func isForbiddenIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("168.63.129.16")) {
		return true
	}
	for _, value := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, network, _ := net.ParseCIDR(value)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
