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
