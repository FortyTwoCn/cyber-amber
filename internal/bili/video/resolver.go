package video

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
	biliinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/wbi"
)

type Resolver struct {
	input  *biliinput.Parser
	client *client.Client
	signer *wbi.Signer
}

func NewResolver(input *biliinput.Parser, client *client.Client, signer *wbi.Signer) *Resolver {
	return &Resolver{input: input, client: client, signer: signer}
}

type viewData struct {
	BVID        string `json:"bvid"`
	AID         int64  `json:"aid"`
	Videos      int    `json:"videos"`
	Title       string `json:"title"`
	Pic         string `json:"pic"`
	Duration    int64  `json:"duration"`
	State       int    `json:"state"`
	RedirectURL string `json:"redirect_url"`
	Owner       struct {
		MID  int64  `json:"mid"`
		Name string `json:"name"`
	} `json:"owner"`
	Rights struct {
		Pay       int `json:"pay"`
		UGCPay    int `json:"ugc_pay"`
		IsStein   int `json:"is_stein_gate"`
		ArcPay    int `json:"arc_pay"`
		FreeWatch int `json:"free_watch"`
	} `json:"rights"`
	Pages []struct {
		CID       int64  `json:"cid"`
		Number    int    `json:"page"`
		Title     string `json:"part"`
		Duration  int64  `json:"duration"`
		Dimension struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"dimension"`
	} `json:"pages"`
}

func (r *Resolver) Resolve(ctx context.Context, input string, pageOverride int) (*ResolvedVideo, error) {
	parsed, err := r.input.Parse(ctx, input)
	if err != nil {
		return nil, err
	}
	if pageOverride > 0 {
		parsed.Page = pageOverride
	}
	query := url.Values{}
	if parsed.BVID != "" {
		query.Set("bvid", parsed.BVID)
	} else {
		query.Set("aid", strconv.FormatInt(parsed.AID, 10))
	}
	signed, err := r.signer.Sign(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sign video view: %w", err)
	}
	var result client.Envelope[viewData]
	if err := r.client.GetJSON(ctx, endpoints.VideoViewWBI, signed, &result); err != nil {
		return nil, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return nil, err
	}
	data := result.Data
	if data.AID <= 0 || data.BVID == "" || data.Title == "" || data.Owner.MID <= 0 || data.Owner.Name == "" || data.Duration <= 0 {
		return nil, errors.New("VIDEO_STRUCTURE_CHANGED: 视频详情缺少 aid、bvid、标题、UP主或时长")
	}
	if decodedAID, err := biliinput.BVToAV(data.BVID); err != nil || decodedAID != data.AID {
		return nil, errors.New("VIDEO_STRUCTURE_CHANGED: API 返回的 aid/bvid 不一致")
	}
	if data.State != 0 {
		return nil, fmt.Errorf("UNSUPPORTED_RESOURCE: 视频状态不可用 (%d)", data.State)
	}
	if data.Rights.Pay != 0 || data.Rights.UGCPay != 0 || data.Rights.ArcPay != 0 || data.Rights.FreeWatch != 0 {
		return nil, errors.New("UNSUPPORTED_RESOURCE: MVP 不处理付费或受限资源")
	}
	if data.Rights.IsStein != 0 {
		return nil, errors.New("UNSUPPORTED_RESOURCE: MVP 不处理互动视频分支")
	}
	if data.RedirectURL != "" && strings.Contains(data.RedirectURL, "/bangumi/") {
		return nil, errors.New("UNSUPPORTED_RESOURCE: MVP 仅支持普通 UGC 视频")
	}
	coverURL := normalizeHTTPS(data.Pic)
	if coverURL != "" && !biliinput.IsAllowedImageURL(coverURL) {
		return nil, errors.New("VIDEO_STRUCTURE_CHANGED: API 返回了不安全的封面地址")
	}
	video := &ResolvedVideo{AID: data.AID, BVID: data.BVID, Title: data.Title, OwnerMID: data.Owner.MID, OwnerName: data.Owner.Name, CoverURL: coverURL, DurationSeconds: data.Duration, SelectedPage: parsed.Page}
	for _, item := range data.Pages {
		video.Pages = append(video.Pages, Page{CID: item.CID, Number: item.Number, Title: item.Title, DurationSeconds: item.Duration, Width: item.Dimension.Width, Height: item.Dimension.Height})
	}
	if len(video.Pages) == 0 {
		if err := r.fetchPages(ctx, video); err != nil {
			return nil, err
		}
	}
	sort.Slice(video.Pages, func(i, j int) bool { return video.Pages[i].Number < video.Pages[j].Number })
	for index, item := range video.Pages {
		if item.Number != index+1 || item.CID <= 0 || item.DurationSeconds <= 0 {
			return nil, errors.New("VIDEO_STRUCTURE_CHANGED: 分P编号、cid 或时长无效")
		}
	}
	if parsed.Page < 1 || parsed.Page > len(video.Pages) {
		return nil, fmt.Errorf("PAGE_NOT_FOUND: 分P %d 不存在，共 %d P", parsed.Page, len(video.Pages))
	}
	video.SelectedCID = video.Pages[parsed.Page-1].CID
	return video, nil
}

func (r *Resolver) fetchPages(ctx context.Context, video *ResolvedVideo) error {
	query := url.Values{"bvid": {video.BVID}}
	var result client.Envelope[[]struct {
		CID       int64  `json:"cid"`
		Number    int    `json:"page"`
		Title     string `json:"part"`
		Duration  int64  `json:"duration"`
		Dimension struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"dimension"`
	}]
	if err := r.client.GetJSON(ctx, endpoints.VideoPageList, query, &result); err != nil {
		return err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return err
	}
	for _, item := range result.Data {
		video.Pages = append(video.Pages, Page{CID: item.CID, Number: item.Number, Title: item.Title, DurationSeconds: item.Duration, Width: item.Dimension.Width, Height: item.Dimension.Height})
	}
	if len(video.Pages) == 0 {
		return errors.New("VIDEO_STRUCTURE_CHANGED: API 未返回分P")
	}
	return nil
}

type StreamResolver struct {
	client *client.Client
	signer *wbi.Signer
}

func NewStreamResolver(client *client.Client, signer *wbi.Signer) *StreamResolver {
	return &StreamResolver{client: client, signer: signer}
}

func (r *StreamResolver) Resolve(ctx context.Context, bvid string, aid, cid int64, targetWidth int, supported map[int]bool) (*Stream, error) {
	query := url.Values{"cid": {strconv.FormatInt(cid, 10)}, "qn": {"127"}, "fnver": {"0"}, "fnval": {"4048"}, "fourk": {"1"}}
	if bvid != "" {
		query.Set("bvid", bvid)
	} else {
		query.Set("avid", strconv.FormatInt(aid, 10))
	}
	for attempt := 0; attempt < 2; attempt++ {
		signed, err := r.signer.Sign(ctx, query)
		if err != nil {
			return nil, err
		}
		var result client.Envelope[struct {
			Dash struct {
				Duration int64   `json:"duration"`
				Video    []Track `json:"video"`
			} `json:"dash"`
		}]
		if err := r.client.GetJSON(ctx, endpoints.PlayURLWBI, signed, &result); err != nil {
			return nil, err
		}
		if result.Code != 0 {
			if attempt == 0 && (result.Code == -403 || result.Code == -352) {
				r.signer.Invalidate()
				continue
			}
			return nil, client.Check(result.Code, result.Message)
		}
		if len(result.Data.Dash.Video) == 0 {
			return nil, errors.New("STREAM_UNAVAILABLE: API 未返回 DASH 视频轨道")
		}
		tracks := make([]Track, 0, len(result.Data.Dash.Video))
		for _, track := range result.Data.Dash.Video {
			if track.ID <= 0 || track.BaseURL == "" || track.CodecID <= 0 || track.Width <= 0 || track.Height <= 0 || track.Width > 16384 || track.Height > 16384 {
				continue
			}
			if !biliinput.IsAllowedCDNURL(track.BaseURL) {
				continue
			}
			if len(supported) > 0 && !supported[track.CodecID] {
				continue
			}
			validBackup := track.BackupURLs[:0]
			for _, backup := range track.BackupURLs {
				if biliinput.IsAllowedCDNURL(backup) {
					validBackup = append(validBackup, backup)
				}
			}
			track.BackupURLs = validBackup
			tracks = append(tracks, track)
		}
		if len(tracks) == 0 {
			return nil, errors.New("STREAM_UNAVAILABLE: 没有受支持且域名安全的视频轨道")
		}
		selected := selectTrack(tracks, targetWidth)
		return &Stream{DurationSeconds: result.Data.Dash.Duration, Tracks: tracks, Selected: selected}, nil
	}
	return nil, errors.New("WBI signature refresh failed")
}

func selectTrack(tracks []Track, targetWidth int) Track {
	best := tracks[0]
	bestDistance := int(^uint(0) >> 1)
	for _, track := range tracks {
		distance := track.Width - targetWidth
		if distance < 0 {
			distance = 100000 + (-distance)
		}
		codecPenalty := 0
		if track.CodecID != 7 {
			codecPenalty = 1000
		}
		score := distance + codecPenalty
		if score < bestDistance {
			best, bestDistance = track, score
		}
	}
	return best
}
func normalizeHTTPS(value string) string {
	if strings.HasPrefix(value, "http://") {
		return "https://" + strings.TrimPrefix(value, "http://")
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}
