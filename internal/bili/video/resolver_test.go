package video

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	bilinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/wbi"
)

type fixedKeys struct{}

func (fixedKeys) Keys(context.Context) (wbi.Keys, error) {
	return wbi.Keys{Img: "7cd084941338484aae1ad9425b84077c", Sub: "4932caff0ff746eab6f01bf08b70ac45", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestResolveVideoAndPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/wbi/view" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"message":"OK","data":{"bvid":"BV17x411w7KC","aid":170001,"title":"test","duration":20,"state":0,"owner":{"mid":1,"name":"up"},"rights":{},"pages":[{"cid":2,"page":1,"part":"one","duration":20,"dimension":{"width":640,"height":360}}]}}`)
	}))
	defer server.Close()
	c := client.New(server.Client(), server.URL, server.URL, "test")
	r := NewResolver(bilinput.New(nil), c, wbi.New(fixedKeys{}))
	got, err := r.Resolve(context.Background(), "BV17x411w7KC", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.AID != 170001 || got.SelectedCID != 2 || len(got.Pages) != 1 {
		t.Fatalf("unexpected %#v", got)
	}
}

func TestTrackUnmarshalVariants(t *testing.T) {
	data := `{"id":64,"base_url":"https://x.bilivideo.com/v","backupUrl":["https://y.bilivideo.com/v"],"codecid":7,"width":1280,"height":720,"frame_rate":"30"}`
	var track Track
	if err := json.Unmarshal([]byte(data), &track); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(track.BaseURL, "x.bilivideo.com") || track.FrameRate != "30" || len(track.BackupURLs) != 1 {
		t.Fatalf("unexpected %#v", track)
	}
}

func TestSelectTrackAvoidsNeedlessUltraHD(t *testing.T) {
	tracks := []Track{{Width: 3840, CodecID: 7}, {Width: 640, CodecID: 7}, {Width: 1280, CodecID: 7}}
	if got := selectTrack(tracks, 640); got.Width != 640 {
		t.Fatalf("got %d", got.Width)
	}
	if got := selectTrack(tracks, 854); got.Width != 1280 {
		t.Fatalf("got %d", got.Width)
	}
}
