package input

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseVideoInputs(t *testing.T) {
	p := New(nil)
	ctx := context.Background()
	tests := []struct {
		input, bvid string
		aid         int64
		page        int
	}{
		{"BV17x411w7KC", "BV17x411w7KC", 0, 1},
		{"https://www.bilibili.com/video/BV17x411w7KC?p=2", "BV17x411w7KC", 0, 2},
		{"分享这个视频 https://m.bilibili.com/video/av170001?p=3 好看", "", 170001, 3},
	}
	for _, tc := range tests {
		got, err := p.Parse(ctx, tc.input)
		if err != nil {
			t.Fatalf("%q: %v", tc.input, err)
		}
		if got.BVID != tc.bvid || got.AID != tc.aid || got.Page != tc.page {
			t.Fatalf("%q: %#v", tc.input, got)
		}
	}
}

func TestShortLinkRedirect(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: &http.Request{URL: mustURL("https://www.bilibili.com/video/BV17x411w7KC?p=4")}}, nil
	})}
	got, err := New(client).Parse(context.Background(), "https://b23.tv/abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Page != 4 || got.BVID != "BV17x411w7KC" {
		t.Fatalf("unexpected %#v", got)
	}
}

func TestRejectSSRFAndHostSuffixConfusion(t *testing.T) {
	p := New(nil)
	for _, value := range []string{"http://127.0.0.1/video/BV17x411w7KC", "https://www.bilibili.com.evil.test/video/BV17x411w7KC", "https://evil.test/?u=https://www.bilibili.com/video/BV17x411w7KC", "https://b23.tv:22/abc", "http://www.bilibili.com:8080/video/BV17x411w7KC", "https://user@www.bilibili.com/video/BV17x411w7KC"} {
		if _, err := p.Parse(context.Background(), value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if IsAllowedCDNHost("bilivideo.com.evil.test") {
		t.Fatal("suffix confusion accepted")
	}
	for _, value := range []string{"ftp://x.bilivideo.com/video", "https://x.bilivideo.com:22/video", "https://x.bilivideo.com.evil.test/video", "https://user@x.bilivideo.com/video"} {
		if IsAllowedCDNURL(value) {
			t.Fatalf("unsafe CDN URL accepted: %s", value)
		}
	}
	if !IsAllowedCDNURL("https://x.bilivideo.com/video") {
		t.Fatal("valid CDN URL rejected")
	}
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "168.63.129.16", "192.0.2.1", "2001:db8::1"} {
		if !isForbiddenIP(net.ParseIP(value)) {
			t.Fatalf("SSRF address accepted: %s", value)
		}
	}
	if isForbiddenIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public unicast address was rejected")
	}
}

func TestBVCaseAndAlphabet(t *testing.T) {
	for _, value := range []string{"bv17x411w7KC", "BV17x411w7K0", "BV17x411w7K"} {
		if _, err := New(nil).Parse(context.Background(), value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func mustURL(value string) *url.URL {
	u, err := url.Parse(value)
	if err != nil {
		panic(err)
	}
	return u
}
