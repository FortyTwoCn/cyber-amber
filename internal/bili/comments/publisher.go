package comments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/images"
	biliinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
)

const VideoReplyType = "1"
const (
	Published           = "published"
	PublishedUnverified = "published_unverified"
	PendingReview       = "pending_review"
	SelfOnlySuspected   = "self_only_suspected"
	Deleted             = "deleted"
	Failed              = "failed"
)

type PublishRequest struct {
	AID        int64
	UserMID    int64
	Username   string
	TaskID     string
	Start, End time.Duration
	Image      images.UploadedImage
	CSRF       string
}
type PublishedComment struct {
	RPID     int64
	Status   string
	Message  string
	ImageURL string
	RawJSON  string
}
type Publisher interface {
	PublishRootImageComment(context.Context, PublishRequest) (*PublishedComment, error)
	FindTaskMarker(context.Context, int64, string, int64) (*PublishedComment, error)
	VerifyRootImageComment(context.Context, int64, int64, string, int64, string) (*PublishedComment, error)
	ReplyOriginal(context.Context, int64, int64, int64, string, string) (int64, error)
}
type HTTPPublisher struct{ client *client.Client }

func New(client *client.Client) *HTTPPublisher { return &HTTPPublisher{client: client} }

func (p *HTTPPublisher) PublishRootImageComment(ctx context.Context, request PublishRequest) (*PublishedComment, error) {
	if request.AID <= 0 || request.UserMID <= 0 || request.Username == "" || len(request.Username) > 128 || strings.ContainsAny(request.Username, "\r\n") || request.TaskID == "" || request.End <= request.Start || !biliinput.IsAllowedImageURL(request.Image.URL) || request.CSRF == "" {
		return nil, errors.New("invalid root image comment request")
	}
	message := fmt.Sprintf("@%s 你的赛博琥珀已生成\n时间：%s–%s\n任务：%s", request.Username, clock(request.Start), clock(request.End), request.TaskID)
	atMap, _ := json.Marshal(map[string]string{request.Username: strconv.FormatInt(request.UserMID, 10)})
	type picturePayload struct {
		Source string  `json:"img_src"`
		Width  int     `json:"img_width"`
		Height int     `json:"img_height"`
		Size   float64 `json:"img_size"`
	}
	pictures, _ := json.Marshal([]picturePayload{{Source: request.Image.URL, Width: request.Image.Width, Height: request.Image.Height, Size: request.Image.Size}})
	form := url.Values{"oid": {strconv.FormatInt(request.AID, 10)}, "type": {VideoReplyType}, "message": {message}, "pictures": {string(pictures)}, "at_name_to_mid": {string(atMap)}, "plat": {"1"}, "csrf": {request.CSRF}, "csrf_token": {request.CSRF}, "gaia_source": {"main_web"}, "statistics": {`{"appId":100,"platform":5}`}}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RPID       int64  `json:"rpid"`
			RPIDString string `json:"rpid_str"`
			Reply      *struct {
				RPID int64 `json:"rpid"`
			} `json:"reply"`
		} `json:"data"`
	}
	if err := p.client.PostForm(ctx, endpoints.CommentAdd, form, &result); err != nil {
		return nil, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return nil, err
	}
	rpid := result.Data.RPID
	if rpid == 0 && result.Data.Reply != nil {
		rpid = result.Data.Reply.RPID
	}
	if rpid == 0 && result.Data.RPIDString != "" {
		rpid, _ = strconv.ParseInt(result.Data.RPIDString, 10, 64)
	}
	if rpid == 0 {
		return nil, errors.New("COMMENT_PUBLISH_STRUCTURE_CHANGED: code=0 但没有 rpid")
	}
	raw, _ := json.Marshal(result)
	return &PublishedComment{RPID: rpid, Status: PublishedUnverified, Message: message, ImageURL: request.Image.URL, RawJSON: string(raw)}, nil
}

type numericID int64

func (n *numericID) UnmarshalJSON(data []byte) error {
	value := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if value == "" || value == "null" {
		*n = 0
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid numeric id %q: %w", value, err)
	}
	*n = numericID(parsed)
	return nil
}

func (p *HTTPPublisher) FindTaskMarker(ctx context.Context, aid int64, taskID string, expectedAuthorMID int64) (*PublishedComment, error) {
	if aid <= 0 || taskID == "" || len(taskID) > 64 || strings.ContainsAny(taskID, "\r\n\x00") || expectedAuthorMID <= 0 {
		return nil, errors.New("invalid task marker lookup")
	}
	next := int64(0)
	seen := map[int64]bool{}
	for page := 0; page < 20; page++ {
		if seen[next] {
			return nil, errors.New("COMMENT_LIST_STRUCTURE_CHANGED: 评论游标形成循环")
		}
		seen[next] = true
		query := url.Values{"type": {VideoReplyType}, "oid": {strconv.FormatInt(aid, 10)}, "mode": {"2"}, "next": {strconv.FormatInt(next, 10)}}
		var result struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Replies []struct {
					RPID   numericID `json:"rpid"`
					Member struct {
						MID numericID `json:"mid"`
					} `json:"member"`
					Content struct {
						Message  string `json:"message"`
						Pictures []struct {
							ImgSrc string `json:"img_src"`
						} `json:"pictures"`
					} `json:"content"`
				} `json:"replies"`
				Cursor struct {
					IsEnd bool      `json:"is_end"`
					Next  numericID `json:"next"`
				} `json:"cursor"`
			} `json:"data"`
		}
		if err := p.client.GetJSON(ctx, endpoints.CommentList, query, &result); err != nil {
			return nil, err
		}
		if err := client.Check(result.Code, result.Message); err != nil {
			return nil, err
		}
		for _, reply := range result.Data.Replies {
			if int64(reply.Member.MID) == expectedAuthorMID && containsTaskMarker(reply.Content.Message, taskID) {
				if reply.RPID <= 0 {
					return nil, errors.New("COMMENT_LIST_STRUCTURE_CHANGED: 匹配评论缺少 rpid")
				}
				status := PublishedUnverified
				imageURL := ""
				if len(reply.Content.Pictures) > 0 {
					imageURL = reply.Content.Pictures[0].ImgSrc
					if strings.HasPrefix(imageURL, "//") {
						imageURL = "https:" + imageURL
					} else if strings.HasPrefix(imageURL, "http://") {
						imageURL = "https://" + strings.TrimPrefix(imageURL, "http://")
					}
					if !biliinput.IsAllowedImageURL(imageURL) {
						imageURL = ""
					}
				}
				if imageURL == "" {
					status = PendingReview
				}
				return &PublishedComment{RPID: int64(reply.RPID), Status: status, Message: reply.Content.Message, ImageURL: imageURL}, nil
			}
		}
		if result.Data.Cursor.IsEnd || len(result.Data.Replies) == 0 {
			break
		}
		if result.Data.Cursor.Next == 0 {
			return nil, errors.New("COMMENT_LIST_STRUCTURE_CHANGED: 非末页缺少 next 游标")
		}
		next = int64(result.Data.Cursor.Next)
	}
	return nil, nil
}

func containsTaskMarker(message, taskID string) bool {
	want := "任务：" + taskID
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// VerifyRootImageComment checks the assigned rpid without the account Cookie,
// so a successful result is evidence of visitor-side visibility. Bilibili may
// accept a reply/add request and then delete the comment asynchronously; the
// reply endpoint's 12022 response distinguishes that outcome from an ordinary
// propagation/review delay.
func (p *HTTPPublisher) VerifyRootImageComment(ctx context.Context, aid, rpid int64, taskID string, expectedAuthorMID int64, expectedImageURL string) (*PublishedComment, error) {
	if aid <= 0 || rpid <= 0 || taskID == "" || expectedAuthorMID <= 0 || !biliinput.IsAllowedImageURL(expectedImageURL) {
		return nil, errors.New("invalid root image comment verification")
	}
	query := url.Values{"type": {VideoReplyType}, "oid": {strconv.FormatInt(aid, 10)}, "root": {strconv.FormatInt(rpid, 10)}}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Root *struct {
				RPID   numericID `json:"rpid"`
				Member struct {
					MID numericID `json:"mid"`
				} `json:"member"`
				Content struct {
					Message  string `json:"message"`
					Pictures []struct {
						ImgSrc string `json:"img_src"`
					} `json:"pictures"`
				} `json:"content"`
			} `json:"root"`
		} `json:"data"`
	}
	if err := p.client.GetPublicJSON(ctx, endpoints.CommentDetail, query, &result); err != nil {
		return nil, err
	}
	if result.Code == 12006 || result.Code == 12022 {
		deleted, err := p.confirmDeleted(ctx, aid, rpid)
		if err != nil {
			return nil, err
		}
		status := PendingReview
		if deleted || result.Code == 12022 {
			status = Deleted
		}
		return &PublishedComment{RPID: rpid, Status: status, ImageURL: expectedImageURL}, nil
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return nil, err
	}
	if result.Data == nil || result.Data.Root == nil {
		return &PublishedComment{RPID: rpid, Status: PendingReview, ImageURL: expectedImageURL}, nil
	}
	root := result.Data.Root
	if int64(root.RPID) != rpid || int64(root.Member.MID) != expectedAuthorMID || !containsTaskMarker(root.Content.Message, taskID) {
		return nil, errors.New("COMMENT_DETAIL_STRUCTURE_CHANGED: 指定 rpid 的作者或任务标记不匹配")
	}
	imageURL := ""
	if len(root.Content.Pictures) > 0 {
		imageURL = normalizeImageURL(root.Content.Pictures[0].ImgSrc)
	}
	status := Published
	if imageURL == "" || imageURL != normalizeImageURL(expectedImageURL) {
		status = PendingReview
	}
	return &PublishedComment{RPID: rpid, Status: status, Message: root.Content.Message, ImageURL: imageURL}, nil
}

func (p *HTTPPublisher) confirmDeleted(ctx context.Context, aid, rpid int64) (bool, error) {
	query := url.Values{"type": {VideoReplyType}, "oid": {strconv.FormatInt(aid, 10)}, "root": {strconv.FormatInt(rpid, 10)}, "pn": {"1"}, "ps": {"1"}}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := p.client.GetPublicJSON(ctx, endpoints.CommentReply, query, &result); err != nil {
		return false, err
	}
	if result.Code == 12022 {
		return true, nil
	}
	if result.Code == 12006 {
		return false, nil
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return false, err
	}
	return false, nil
}

func normalizeImageURL(value string) string {
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	} else if strings.HasPrefix(value, "http://") {
		value = "https://" + strings.TrimPrefix(value, "http://")
	}
	if !biliinput.IsAllowedImageURL(value) {
		return ""
	}
	return value
}

func (p *HTTPPublisher) ReplyOriginal(ctx context.Context, aid, rootRPID, parentRPID int64, message, csrf string) (int64, error) {
	if aid <= 0 || rootRPID <= 0 || parentRPID <= 0 || message == "" || csrf == "" {
		return 0, errors.New("invalid original comment reply")
	}
	form := url.Values{"oid": {strconv.FormatInt(aid, 10)}, "type": {VideoReplyType}, "root": {strconv.FormatInt(rootRPID, 10)}, "parent": {strconv.FormatInt(parentRPID, 10)}, "message": {message}, "plat": {"1"}, "csrf": {csrf}, "csrf_token": {csrf}}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RPID  int64 `json:"rpid"`
			Reply *struct {
				RPID int64 `json:"rpid"`
			} `json:"reply"`
		} `json:"data"`
	}
	if err := p.client.PostForm(ctx, endpoints.CommentAdd, form, &result); err != nil {
		return 0, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return 0, err
	}
	if result.Data.RPID != 0 {
		return result.Data.RPID, nil
	}
	if result.Data.Reply != nil {
		return result.Data.Reply.RPID, nil
	}
	return 0, errors.New("text reply response omitted rpid")
}

func clock(value time.Duration) string {
	seconds := int64(value / time.Second)
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
