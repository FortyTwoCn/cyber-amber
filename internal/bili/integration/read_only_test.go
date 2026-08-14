package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku"
	biliinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/video"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/wbi"
)

// This test performs only GET/HEAD requests. It never calls a Bilibili write API.
func TestLiveReadOnlyUGC(t *testing.T) {
	if os.Getenv("BILI_READONLY_E2E") != "true" {
		t.Skip("set BILI_READONLY_E2E=true for live read-only verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	userAgent := "Mozilla/5.0 (X11; Linux x86_64) Chrome/127 CyberAmberReadOnlyTest/1.0"
	biliClient := client.New(nil, "https://api.bilibili.com", "https://passport.bilibili.com", userAgent)
	if err := biliClient.EnsureBuvid(ctx); err != nil {
		t.Fatal(err)
	}
	parser := biliinput.New(nil)
	short, err := parser.Parse(ctx, "分享 https://b23.tv/BV17x411w7KC")
	if err != nil {
		// Some CI/sandbox networks deliberately synthesize RFC 2544 addresses
		// for outbound hosts. The production parser must keep rejecting those
		// addresses; continue the live API checks with a direct BVID while the
		// deterministic short-link redirect tests cover that parser branch.
		if !strings.Contains(err.Error(), "host resolves only to forbidden addresses") {
			t.Fatal(err)
		}
		t.Logf("short-link live check unavailable in this network: %v", err)
	} else if short.BVID != "BV17x411w7KC" {
		t.Fatalf("short link got %#v", short)
	}
	signer := wbi.New(wbi.NavSource{Client: biliClient})
	resolver := video.NewResolver(parser, biliClient, signer)
	resolved, err := resolver.Resolve(ctx, "BV17x411w7KC", 1)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AID != 170001 || len(resolved.Pages) < 2 || resolved.SelectedCID == 0 {
		t.Fatalf("unexpected video %#v", resolved)
	}
	stream, err := video.NewStreamResolver(biliClient, signer).Resolve(ctx, resolved.BVID, resolved.AID, resolved.SelectedCID, 640, map[int]bool{7: true, 12: true, 13: true})
	if err != nil {
		t.Fatal(err)
	}
	if stream.Selected.BaseURL == "" {
		t.Fatal("no selected stream")
	}
	if _, err := danmaku.New(biliClient).FetchRange(ctx, resolved.AID, resolved.SelectedCID, 0, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}
