package danmaku

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/danmaku/pb"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
	"google.golang.org/protobuf/proto"
)

const SegmentDuration = 6 * time.Minute

type Item struct {
	ID       int64
	At       time.Duration
	Mode     int32
	FontSize int32
	Color    uint32
	MidHash  string
	Content  string
	Created  int64
	Weight   int32
	Pool     int32
}

type Provider interface {
	FetchRange(context.Context, int64, int64, time.Duration, time.Duration) ([]Item, error)
}

type HTTPProvider struct {
	client          *client.Client
	maxSegmentBytes int64
}

func New(client *client.Client) *HTTPProvider {
	return &HTTPProvider{client: client, maxSegmentBytes: 8 << 20}
}

func SegmentIndexes(start, end time.Duration) []int {
	if start < 0 || end <= start {
		return nil
	}
	first := int(start/SegmentDuration) + 1
	last := int((end-time.Nanosecond)/SegmentDuration) + 1
	result := make([]int, 0, last-first+1)
	for i := first; i <= last; i++ {
		result = append(result, i)
	}
	return result
}

func (p *HTTPProvider) FetchRange(ctx context.Context, aid, cid int64, start, end time.Duration) ([]Item, error) {
	if aid <= 0 || cid <= 0 {
		return nil, fmt.Errorf("aid and cid must be positive")
	}
	indexes := SegmentIndexes(start, end)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("invalid danmaku time range")
	}
	items := make([]Item, 0)
	for _, index := range indexes {
		query := url.Values{"type": {"1"}, "oid": {strconv.FormatInt(cid, 10)}, "pid": {strconv.FormatInt(aid, 10)}, "segment_index": {strconv.Itoa(index)}}
		data, err := p.client.GetBytes(ctx, endpoints.DanmakuSegment, query, p.maxSegmentBytes)
		if err != nil {
			return nil, fmt.Errorf("fetch danmaku segment %d: %w", index, err)
		}
		var reply pb.DmSegMobileReply
		if err := proto.Unmarshal(data, &reply); err != nil {
			return nil, fmt.Errorf("decode danmaku segment %d: %w", index, err)
		}
		for _, elem := range reply.GetElems() {
			absolute := time.Duration(elem.GetProgress()) * time.Millisecond
			if absolute < start || absolute >= end {
				continue
			}
			items = append(items, Item{ID: elem.GetId(), At: absolute - start, Mode: elem.GetMode(), FontSize: elem.GetFontsize(), Color: elem.GetColor(), MidHash: elem.GetMidHash(), Content: elem.GetContent(), Created: elem.GetCtime(), Weight: elem.GetWeight(), Pool: elem.GetPool()})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].At == items[j].At {
			return items[i].ID < items[j].ID
		}
		return items[i].At < items[j].At
	})
	return items, nil
}
