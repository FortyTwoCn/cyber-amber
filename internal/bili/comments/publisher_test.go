package comments

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/images"
)

func TestRootImagePayloadHasNoRootOrParentAndRealAt(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		received = r.PostForm
		fmt.Fprint(w, `{"code":0,"message":"0","data":{"rpid":123}}`)
	}))
	defer server.Close()
	publisher := New(client.New(server.Client(), server.URL, server.URL, "test"))
	got, err := publisher.PublishRootImageComment(context.Background(), PublishRequest{AID: 170001, UserMID: 42, Username: "用户", TaskID: "CA01TEST", Start: 10 * time.Second, End: 20 * time.Second, Image: images.UploadedImage{URL: "https://i0.hdslb.com/a.gif", Width: 640, Height: 360, SizeKB: 123}, CSRF: "csrf"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RPID != 123 {
		t.Fatal(got)
	}
	if received.Has("root") || received.Has("parent") {
		t.Fatalf("root/parent present: %v", received)
	}
	if received.Get("type") != "1" || received.Get("oid") != "170001" || !strings.Contains(received.Get("pictures"), "a.gif") || !strings.Contains(received.Get("at_name_to_mid"), "42") || !strings.Contains(received.Get("message"), "@用户") {
		t.Fatalf("bad form: %v", received)
	}
}

func TestFindExistingTaskMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"replies":[{"rpid":"55","member":{"mid":"42"},"content":{"message":"任务：CA01TEST","pictures":[{"img_src":"https://i0.hdslb.com/a.gif"}]}}],"cursor":{"is_end":true}}}`)
	}))
	defer server.Close()
	publisher := New(client.New(server.Client(), server.URL, server.URL, "test"))
	got, err := publisher.FindTaskMarker(context.Background(), 170001, "CA01TEST", 42)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RPID != 55 || got.Status != PublishedUnverified {
		t.Fatalf("unexpected %#v", got)
	}
}

func TestFindTaskMarkerUsesReturnedCursorAndBotAuthor(t *testing.T) {
	var nextValues []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := r.URL.Query().Get("next")
		nextValues = append(nextValues, next)
		if next == "0" {
			fmt.Fprint(w, `{"code":0,"data":{"replies":[{"rpid":1,"member":{"mid":99},"content":{"message":"任务：CA01TEST","pictures":[{"img_src":"https://i0.hdslb.com/forged.gif"}]}}],"cursor":{"is_end":false,"next":"42"}}}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"replies":[{"rpid":2,"member":{"mid":42},"content":{"message":"任务：CA01TEST","pictures":[{"img_src":"https://i0.hdslb.com/real.gif"}]}}],"cursor":{"is_end":true}}}`)
	}))
	defer server.Close()
	publisher := New(client.New(server.Client(), server.URL, server.URL, "test"))
	got, err := publisher.FindTaskMarker(context.Background(), 170001, "CA01TEST", 42)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RPID != 2 || got.ImageURL != "https://i0.hdslb.com/real.gif" || strings.Join(nextValues, ",") != "0,42" {
		t.Fatalf("got=%#v cursors=%v", got, nextValues)
	}
}

func TestOriginalReplyIsTextOnly(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		fmt.Fprint(w, `{"code":0,"data":{"rpid":66}}`)
	}))
	defer server.Close()
	publisher := New(client.New(server.Client(), server.URL, server.URL, "test"))
	if _, err := publisher.ReplyOriginal(context.Background(), 170001, 10, 11, "请查看新主楼", "csrf"); err != nil {
		t.Fatal(err)
	}
	if form.Get("root") != "10" || form.Get("parent") != "11" || form.Has("pictures") {
		t.Fatalf("bad reply form %v", form)
	}
}

func TestTaskMarkerRequiresExactMarkerLine(t *testing.T) {
	if !containsTaskMarker("完成\n任务：CA01TEST\n", "CA01TEST") {
		t.Fatal("exact marker line was rejected")
	}
	for _, message := range []string{"任务：CA01TEST-FORGED", "前缀 CA01TEST 后缀", "任务: CA01TEST"} {
		if containsTaskMarker(message, "CA01TEST") {
			t.Fatalf("inexact marker accepted: %q", message)
		}
	}
}
