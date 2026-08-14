package danmaku

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku/pb"
	"google.golang.org/protobuf/proto"
)

func TestSegmentIndexes(t *testing.T) {
	tests := []struct {
		start, end time.Duration
		want       []int
	}{{0, 10 * time.Second, []int{1}}, {359 * time.Second, 361 * time.Second, []int{1, 2}}, {360 * time.Second, 720 * time.Second, []int{2}}}
	for _, tc := range tests {
		got := SegmentIndexes(tc.start, tc.end)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Fatalf("%s-%s got %v want %v", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestFetchRangeOffsetsAndFilters(t *testing.T) {
	reply := &pb.DmSegMobileReply{Elems: []*pb.DanmakuElem{{Id: 1, Progress: 9000, Mode: 1, Content: "before"}, {Id: 2, Progress: 10000, Mode: 5, Content: "start"}, {Id: 3, Progress: 19999, Mode: 4, Content: "inside"}, {Id: 4, Progress: 20000, Mode: 1, Content: "end"}}}
	data, err := proto.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("segment_index") != "1" {
			t.Errorf("bad segment")
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	provider := New(client.New(server.Client(), server.URL, server.URL, "test"))
	items, err := provider.FetchRange(context.Background(), 1, 2, 10*time.Second, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].At != 0 || items[1].At != 9999*time.Millisecond {
		t.Fatalf("unexpected %#v", items)
	}
}
