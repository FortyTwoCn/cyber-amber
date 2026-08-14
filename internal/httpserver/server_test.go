package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/auth"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	"github.com/FortyTwoCn/cyber-amber/internal/config"
	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/internal/httpserver/security"
	"github.com/FortyTwoCn/cyber-amber/internal/observability"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

type fakeResolver struct{}

func (fakeResolver) Resolve(_ context.Context, _ string, page int) (*video.ResolvedVideo, error) {
	if page < 1 {
		page = 1
	}
	return &video.ResolvedVideo{AID: 170001, BVID: "BV17x411w7KC", Title: "fixture", OwnerMID: 1, OwnerName: "up", DurationSeconds: 20, Pages: []video.Page{{CID: 2, Number: 1, Title: "P1", DurationSeconds: 20, Width: 640, Height: 360}}, SelectedPage: page, SelectedCID: 2}, nil
}

func testServer(t *testing.T, admin bool) (*Server, *store.Store) {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Path = filepath.Join(t.TempDir(), "server.db")
	cfg.Paths.DataDir = t.TempDir()
	cfg.Paths.CacheDir = t.TempDir()
	cfg.Paths.TempDir = t.TempDir()
	if admin {
		hash, err := security.HashPassword("a-long-admin-password")
		if err != nil {
			t.Fatal(err)
		}
		cfg.Security.AdminPasswordHash = hash
		cfg.Security.SessionSecret = bytes.Repeat([]byte{1}, 32)
	}
	database, err := store.Open(context.Background(), cfg.Database.Path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	biliClient := client.New(&http.Client{Timeout: time.Second}, "http://invalid", "http://invalid", "test")
	accounts := auth.NewService(biliClient, database, nil)
	metrics := observability.NewMetrics(database)
	server, err := New(cfg, database, fakeResolver{}, accounts, metrics, NewReadiness(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server, database
}

func TestResolveAndCreateJobAPI(t *testing.T) {
	server, database := testServer(t, false)
	handler := server.Handler()
	resolve := httptest.NewRequest(http.MethodPost, "/api/v1/videos/resolve", strings.NewReader(`{"input":"BV17x411w7KC","page":1}`))
	resolve.RemoteAddr = "127.0.0.1:1234"
	resolveResult := httptest.NewRecorder()
	handler.ServeHTTP(resolveResult, resolve)
	if resolveResult.Code != http.StatusOK {
		t.Fatalf("resolve %d %s", resolveResult.Code, resolveResult.Body.String())
	}
	createBody := `{"input":"BV17x411w7KC","page":1,"start":"00:00","end":"00:10","fps":10,"width":640,"resolution":"360p","danmaku":false,"danmaku_opacity":0.8,"danmaku_font_scale":1,"danmaku_density":1}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(createBody))
	request.RemoteAddr = "127.0.0.1:1234"
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusAccepted {
		t.Fatalf("create %d %s", result.Code, result.Body.String())
	}
	var body struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	job, err := database.GetJob(context.Background(), body.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.BVID != "BV17x411w7KC" || job.CID != 2 {
		t.Fatalf("unexpected %#v", job)
	}
}

func TestUniformJSONErrorAndSecurityHeaders(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/videos/resolve", strings.NewReader(`{"unknown":1}`))
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), `"request_id"`) {
		t.Fatalf("unexpected %d %s", result.Code, result.Body.String())
	}
	if result.Header().Get("Content-Security-Policy") == "" || result.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
}

func TestPublicJobUsesConfiguredClipLimit(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"BV17x411w7KC","page":1,"start":"00:00","end":"00:16","fps":10,"width":640,"danmaku":false}`))
	request.RemoteAddr = "127.0.0.1:1234"
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "CLIP_TOO_LONG") {
		t.Fatalf("unexpected %d %s", result.Code, result.Body.String())
	}
}

func TestAdminLoginAndCSRFFailure(t *testing.T) {
	server, _ := testServer(t, true)
	handler := server.Handler()
	login := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{"password":"a-long-admin-password"}`))
	login.RemoteAddr = "127.0.0.1:1234"
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, login)
	if result.Code != http.StatusOK {
		t.Fatalf("login %d %s", result.Code, result.Body.String())
	}
	cookies := result.Result().Cookies()
	pause := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bot/pause", strings.NewReader(`{"reason":"test"}`))
	for _, cookie := range cookies {
		pause.AddCookie(cookie)
	}
	pauseResult := httptest.NewRecorder()
	handler.ServeHTTP(pauseResult, pause)
	if pauseResult.Code != http.StatusForbidden {
		t.Fatalf("pause without CSRF got %d", pauseResult.Code)
	}
}

func TestAdminQRFeedbackIsVisibleBeforeRequest(t *testing.T) {
	server, _ := testServer(t, true)
	handler := server.Handler()

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("admin page %d %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "Header String") || !strings.Contains(page.Body.String(), `id="qr-status"`) {
		t.Fatalf("admin page does not explain the cookie format or expose QR status feedback: %s", page.Body.String())
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/admin.js", nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("admin asset %d %s", asset.Code, asset.Body.String())
	}
	script := asset.Body.String()
	showQR := strings.Index(script, `$("qr-box").classList.remove("hidden")`)
	requestQR := strings.Index(script, `await api("/api/v1/admin/account/qrcode"`)
	if showQR < 0 || requestQR < 0 || showQR > requestQR {
		t.Fatal("QR feedback container is not shown before the network request")
	}
}

func TestTrustedProxyHandling(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := server.remoteIP(request); got != "10.0.0.2" {
		t.Fatalf("untrusted proxy spoof accepted: %s", got)
	}
	server.cfg.Web.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	if got := server.remoteIP(request); got != "203.0.113.9" {
		t.Fatalf("trusted proxy ignored: %s", got)
	}
	request.Header.Set("X-Forwarded-For", "198.51.100.66, 203.0.113.9")
	if got := server.remoteIP(request); got != "203.0.113.9" {
		t.Fatalf("client-supplied leftmost XFF bypassed trusted proxy chain: %s", got)
	}
}

func TestPublicCancellationRequiresCreatingBrowser(t *testing.T) {
	server, database := testServer(t, false)
	handler := server.Handler()
	create := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"BV17x411w7KC","page":1,"start":"00:00","end":"00:10","fps":10,"width":640,"danmaku":false}`))
	create.RemoteAddr = "127.0.0.1:1234"
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	var body struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	withoutOwner := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+body.Job.ID+"/cancel", strings.NewReader(`{}`))
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutOwner)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unauthorized cancel=%d %s", denied.Code, denied.Body.String())
	}
	withOwner := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+body.Job.ID+"/cancel", strings.NewReader(`{}`))
	for _, cookie := range created.Result().Cookies() {
		withOwner.AddCookie(cookie)
	}
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, withOwner)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("owner cancel=%d %s", accepted.Code, accepted.Body.String())
	}
	job, err := database.GetJob(context.Background(), body.Job.ID)
	if err != nil || job.Status != domain.JobCancelled {
		t.Fatalf("job=%#v err=%v", job, err)
	}
}

func TestArtifactDownloadCannotEscapeArtifactDirectory(t *testing.T) {
	server, database := testServer(t, false)
	outside := filepath.Join(t.TempDir(), "outside.gif")
	if err := os.WriteFile(outside, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	params := domain.RenderParams{FPS: 10, Width: 640, Colors: 256, Dither: "sierra2_4a"}
	job := domain.Job{ID: "01HTTPARTIFACT000000000000", Source: domain.SourceWeb, AnonymousID: "anonymous-browser-token", Page: 1, Start: 0, End: time.Second, Requested: params, Final: params, DedupeKey: "outside-artifact", Status: domain.JobSucceeded, Progress: 100, MaxAttempts: 3, ArtifactPath: outside, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := database.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID+"/artifact", nil)
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusGone {
		t.Fatalf("escaped artifact status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestHTTPSModeSendsHSTS(t *testing.T) {
	server, _ := testServer(t, false)
	server.secureCookie = true
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HTTPS deployment omitted HSTS")
	}
}

func TestAdminOperationalSettingsPersistAfterValidation(t *testing.T) {
	server, database := testServer(t, true)
	handler := server.Handler()
	login := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{"password":"a-long-admin-password"}`))
	login.RemoteAddr = "127.0.0.1:1234"
	loginResult := httptest.NewRecorder()
	handler.ServeHTTP(loginResult, login)
	if loginResult.Code != http.StatusOK {
		t.Fatalf("login %d %s", loginResult.Code, loginResult.Body.String())
	}
	var loginBody struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(loginResult.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}
	settings := server.cfg.OperationalSettings()
	settings.DefaultFPS = 12
	payload, _ := json.Marshal(settings)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(payload))
	request.Header.Set("X-CSRF-Token", loginBody.CSRF)
	for _, cookie := range loginResult.Result().Cookies() {
		request.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"restart_required":true`) {
		t.Fatalf("save settings %d %s", result.Code, result.Body.String())
	}
	raw, found, err := database.LoadSetting(context.Background(), config.OperationalSettingsKey)
	if err != nil || !found || !strings.Contains(string(raw), `"default_fps":12`) {
		t.Fatalf("setting=%s found=%v err=%v", raw, found, err)
	}

	settings.DefaultFPS = settings.MaxOutputFPS + 1
	payload, _ = json.Marshal(settings)
	invalid := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(payload))
	invalid.Header.Set("X-CSRF-Token", loginBody.CSRF)
	for _, cookie := range loginResult.Result().Cookies() {
		invalid.AddCookie(cookie)
	}
	invalidResult := httptest.NewRecorder()
	handler.ServeHTTP(invalidResult, invalid)
	if invalidResult.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings %d %s", invalidResult.Code, invalidResult.Body.String())
	}
}
