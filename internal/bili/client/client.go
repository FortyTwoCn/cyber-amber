package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
)

const maxJSONBody = 16 << 20

type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
	Risk       bool
	Permanent  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bilibili api: http=%d code=%d message=%s", e.HTTPStatus, e.Code, e.Message)
}

type Client struct {
	httpClient *http.Client
	apiBase    string
	passport   string
	userAgent  string
	mu         sync.RWMutex
	cookie     string
	buvid3     string
	buvid4     string
	observer   func(string, int)
}

func New(httpClient *http.Client, apiBase, passportBase, userAgent string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, MaxIdleConns: 100, MaxIdleConnsPerHost: 20, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 8 * time.Second}}
	}
	return &Client{httpClient: httpClient, apiBase: strings.TrimRight(apiBase, "/"), passport: strings.TrimRight(passportBase, "/"), userAgent: userAgent}
}

func (c *Client) SetCookie(cookie string) {
	c.mu.Lock()
	c.cookie = strings.TrimSpace(cookie)
	c.mu.Unlock()
}
func (c *Client) SetObserver(observer func(string, int)) {
	c.mu.Lock()
	c.observer = observer
	c.mu.Unlock()
}
func (c *Client) HasCookie() bool { c.mu.RLock(); defer c.mu.RUnlock(); return c.cookie != "" }

func (c *Client) APIURL(path string, query url.Values) string {
	return buildURL(c.apiBase, path, query)
}
func (c *Client) PassportURL(path string, query url.Values) string {
	return buildURL(c.passport, path, query)
}

func buildURL(base, path string, query url.Values) string {
	value := base + path
	if len(query) > 0 {
		value += "?" + query.Encode()
	}
	return value
}

func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.doJSON(ctx, http.MethodGet, c.APIURL(path, query), nil, out, true)
}

// GetJSONWithPageContext performs an authenticated API request using the
// browser page context expected by endpoints that are scoped to a Bilibili
// sub-site, such as the message center.
func (c *Client) GetJSONWithPageContext(ctx context.Context, path string, query url.Values, referer, origin string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, c.APIURL(path, query), nil, true)
	if err != nil {
		return err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if origin == "" {
		req.Header.Del("Origin")
	} else {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	_, err = c.doJSONRequest(req, out)
	return err
}

func (c *Client) GetPassportJSON(ctx context.Context, path string, query url.Values, out any) error {
	_, err := c.GetPassportJSONWithCookies(ctx, path, query, out)
	return err
}

// GetPassportJSONWithCookies uses the request context expected by the passport
// website and returns response cookies so QR login can persist credentials even
// when a successful response does not repeat every cookie in its callback URL.
func (c *Client) GetPassportJSONWithCookies(ctx context.Context, path string, query url.Values, out any) ([]*http.Cookie, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.PassportURL(path, query), nil, false)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", c.passport+"/")
	req.Header.Set("Origin", c.passport)
	return c.doJSONRequest(req, out)
}

func (c *Client) PostForm(ctx context.Context, path string, form url.Values, out any) error {
	body := strings.NewReader(form.Encode())
	return c.doJSON(ctx, http.MethodPost, c.APIURL(path, nil), body, out, true, "application/x-www-form-urlencoded")
}

func (c *Client) GetBytes(ctx context.Context, path string, query url.Values, maxBytes int64) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.APIURL(path, query), nil, true)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.observe(req.URL.Path, 0)
		return nil, fmt.Errorf("bilibili request: %w", err)
	}
	c.observe(req.URL.Path, resp.StatusCode)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, classify(resp.StatusCode, 0, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read bilibili response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("bilibili response exceeds size limit")
	}
	return data, nil
}

func (c *Client) DoMultipart(ctx context.Context, path string, fields map[string]string, fileField, fileName, contentType string, file io.Reader, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("write multipart field: %w", err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, fileName))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create multipart file: %w", err)
	}
	if _, err = io.Copy(part, file); err != nil {
		return fmt.Errorf("copy multipart file: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("finish multipart: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, c.APIURL(path, nil), &body, out, true, writer.FormDataContentType())
}

func (c *Client) doJSON(ctx context.Context, method, target string, body io.Reader, out any, authenticated bool, contentType ...string) error {
	req, err := c.newRequest(ctx, method, target, body, authenticated)
	if err != nil {
		return err
	}
	if len(contentType) > 0 {
		req.Header.Set("Content-Type", contentType[0])
	}
	_, err = c.doJSONRequest(req, out)
	return err
}

func (c *Client) doJSONRequest(req *http.Request, out any) ([]*http.Cookie, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.observe(req.URL.Path, 0)
		return nil, fmt.Errorf("bilibili request: %w", err)
	}
	c.observe(req.URL.Path, resp.StatusCode)
	defer resp.Body.Close()
	cookies := resp.Cookies()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return nil, fmt.Errorf("read bilibili response: %w", err)
	}
	if len(data) > maxJSONBody {
		return nil, errors.New("bilibili JSON response exceeds size limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, classify(resp.StatusCode, 0, truncate(string(data), 256))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Code: -1, Message: "响应结构不兼容: " + err.Error(), Permanent: true}
	}
	return cookies, nil
}

func (c *Client) observe(operation string, status int) {
	c.mu.RLock()
	observer := c.observer
	c.mu.RUnlock()
	if observer != nil {
		observer(operation, status)
	}
}

func (c *Client) newRequest(ctx context.Context, method, target string, body io.Reader, authenticated bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("create bilibili request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if authenticated {
		c.mu.RLock()
		cookie := c.cookie
		buvid3, buvid4 := c.buvid3, c.buvid4
		c.mu.RUnlock()
		parts := make([]string, 0, 3)
		if cookie != "" {
			parts = append(parts, cookie)
		}
		if buvid3 != "" && !strings.Contains(cookie, "buvid3=") {
			parts = append(parts, "buvid3="+buvid3)
		}
		if buvid4 != "" && !strings.Contains(cookie, "buvid4=") {
			parts = append(parts, "buvid4="+buvid4)
		}
		if len(parts) > 0 {
			req.Header.Set("Cookie", strings.Join(parts, "; "))
		}
	}
	return req, nil
}

type Envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TTL     int    `json:"ttl"`
	Data    T      `json:"data"`
}

func Check(code int, message string) error {
	if code == 0 {
		return nil
	}
	return classify(http.StatusOK, code, message)
}

func classify(status, code int, message string) *APIError {
	err := &APIError{HTTPStatus: status, Code: code, Message: message}
	if status == http.StatusPreconditionFailed || code == -352 || code == -105 {
		err.Risk = true
		err.Permanent = true
	}
	// 12002 is a resource-level permanent failure (for example, a closed
	// comment area), not evidence that the whole account is under risk control.
	if code == 12002 {
		err.Permanent = true
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || code == -101 || code == -111 {
		err.Permanent = true
	}
	if status == http.StatusTooManyRequests || code == -509 || code == 12015 {
		err.Permanent = false
	}
	if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		err.Permanent = true
	}
	return err
}

func (c *Client) EnsureBuvid(ctx context.Context) error {
	var result Envelope[struct {
		Buvid3 string `json:"b_3"`
		Buvid4 string `json:"b_4"`
	}]
	if err := c.GetJSON(ctx, endpoints.FingerSPI, nil, &result); err != nil {
		return err
	}
	if err := Check(result.Code, result.Message); err != nil {
		return err
	}
	if result.Data.Buvid3 == "" || result.Data.Buvid4 == "" {
		return errors.New("finger SPI response omitted buvid values")
	}
	c.mu.Lock()
	c.buvid3 = result.Data.Buvid3
	c.buvid4 = result.Data.Buvid4
	c.mu.Unlock()
	return nil
}

func truncate(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}
