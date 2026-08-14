package auth

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

func TestFailedCookieImportRestoresStoredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		loggedIn := strings.Contains(cookie, "SESSDATA=old")
		mid, name := int64(0), ""
		if loggedIn {
			mid, name = 42, "robot"
		}
		_, _ = fmt.Fprintf(w, `{"code":0,"message":"0","data":{"isLogin":%t,"mid":%d,"uname":%q}}`, loggedIn, mid, name)
	}))
	defer server.Close()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "auth.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service := NewService(client.New(server.Client(), server.URL, server.URL, "test"), database, bytes.Repeat([]byte{1}, 32))
	if err := service.Import(ctx, "SESSDATA=old; bili_jct=csrf-old; DedeUserID=42"); err != nil {
		t.Fatal(err)
	}
	if err := service.Import(ctx, "SESSDATA=invalid; bili_jct=csrf-new; DedeUserID=99"); err == nil {
		t.Fatal("invalid replacement cookie was accepted")
	}
	status, err := service.Check(ctx)
	if err != nil || !status.LoggedIn || status.MID != 42 {
		t.Fatalf("old session was not restored: %#v err=%v", status, err)
	}
}

func TestQRPollAcceptsCredentialsFromSetCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/poll":
			for name, value := range map[string]string{"SESSDATA": "session", "bili_jct": "csrf", "DedeUserID": "42"} {
				http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
			}
			_, _ = fmt.Fprint(w, `{"code":0,"message":"0","data":{"url":"https://passport.bilibili.com/callback","refresh_token":"refresh","code":0,"message":"success"}}`)
		case "/x/web-interface/nav":
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=session") {
				t.Fatalf("validated without QR response cookies: %q", r.Header.Get("Cookie"))
			}
			_, _ = fmt.Fprint(w, `{"code":0,"message":"0","data":{"isLogin":true,"mid":42,"uname":"robot"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "qr.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service := NewService(client.New(server.Client(), server.URL, server.URL, "test"), database, bytes.Repeat([]byte{2}, 32))
	result, err := service.PollQR(ctx, "qr-key")
	if err != nil || result.State != "confirmed" || result.Status == nil || result.Status.MID != 42 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	session, err := service.Session(ctx)
	if err != nil || session.Cookies["DedeUserID"] != "42" || session.RefreshToken != "refresh" {
		t.Fatalf("session=%#v err=%v", session, err)
	}
}
