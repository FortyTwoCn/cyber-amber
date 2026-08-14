package images

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
	biliinput "github.com/FortyTwoCn/cyber-amber/internal/bili/input"
)

type UploadedImage struct {
	URL    string  `json:"image_url"`
	Width  int     `json:"image_width"`
	Height int     `json:"image_height"`
	SizeKB float64 `json:"size"`
}

func (i *UploadedImage) UnmarshalJSON(data []byte) error {
	var raw struct {
		URL       string          `json:"image_url"`
		Width     json.RawMessage `json:"image_width"`
		Height    json.RawMessage `json:"image_height"`
		Size      json.RawMessage `json:"size"`
		ImageSize json.RawMessage `json:"image_size"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	width, err := parseNumber(raw.Width)
	if err != nil {
		return fmt.Errorf("image_width: %w", err)
	}
	height, err := parseNumber(raw.Height)
	if err != nil {
		return fmt.Errorf("image_height: %w", err)
	}
	sizeRaw := raw.Size
	if len(sizeRaw) == 0 || string(sizeRaw) == "null" {
		sizeRaw = raw.ImageSize
	}
	size, err := parseNumber(sizeRaw)
	if err != nil {
		return fmt.Errorf("image size: %w", err)
	}
	if math.IsNaN(width) || math.IsInf(width, 0) || math.Trunc(width) != width || math.IsNaN(height) || math.IsInf(height, 0) || math.Trunc(height) != height || width > 16384 || height > 16384 || math.IsNaN(size) || math.IsInf(size, 0) {
		return errors.New("image dimensions or size are not finite safe values")
	}
	i.URL, i.Width, i.Height, i.SizeKB = raw.URL, int(width), int(height), size
	return nil
}

func parseNumber(data json.RawMessage) (float64, error) {
	value := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if value == "" || value == "null" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

type Uploader interface {
	Upload(context.Context, string, string) (*UploadedImage, error)
}
type HTTPUploader struct {
	client      *client.Client
	maxAttempts int
}

func New(client *client.Client) *HTTPUploader { return &HTTPUploader{client: client, maxAttempts: 3} }

func (u *HTTPUploader) Upload(ctx context.Context, path, csrf string) (*UploadedImage, error) {
	if csrf == "" {
		return nil, errors.New("BILI_AUTH_INVALID: 缺少 CSRF")
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat GIF upload: %w", err)
	}
	if stat.Size() < 6 {
		return nil, errors.New("GIF artifact is empty")
	}
	header := make([]byte, 6)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open GIF upload: %w", err)
	}
	_, readErr := io.ReadFull(file, header)
	_ = file.Close()
	if readErr != nil || string(header[:3]) != "GIF" {
		return nil, errors.New("upload artifact is not a GIF")
	}
	var lastErr error
	attemptsUsed := 0
	for attempt := 1; attempt <= u.maxAttempts; attempt++ {
		attemptsUsed = attempt
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		var result client.Envelope[UploadedImage]
		err = u.client.DoMultipart(ctx, endpoints.ImageUpload, map[string]string{"biz": "draw", "category": "daily", "csrf": csrf, "csrf_token": csrf}, "file_up", "cyber-amber.gif", "image/gif", file, &result)
		_ = file.Close()
		if err == nil {
			err = client.Check(result.Code, result.Message)
		}
		if err == nil {
			if strings.HasPrefix(result.Data.URL, "//") {
				result.Data.URL = "https:" + result.Data.URL
			} else if strings.HasPrefix(result.Data.URL, "http://") {
				result.Data.URL = "https://" + strings.TrimPrefix(result.Data.URL, "http://")
			}
			if result.Data.URL == "" || result.Data.Width < 1 || result.Data.Height < 1 || !biliinput.IsAllowedImageURL(result.Data.URL) {
				return nil, errors.New("IMAGE_UPLOAD_STRUCTURE_CHANGED: 上传响应缺少图片信息")
			}
			if result.Data.SizeKB <= 0 {
				result.Data.SizeKB = float64(stat.Size()) / 1024
			}
			return &result.Data, nil
		}
		lastErr = err
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && (apiErr.Permanent || apiErr.Risk) {
			break
		}
		if attempt < u.maxAttempts {
			delay := time.Duration(1<<attempt) * time.Second
			delay += time.Duration(rand.Int64N(max(int64(delay/5), 1)))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, fmt.Errorf("upload GIF after %s bytes and %d attempts: %w", strconv.FormatInt(stat.Size(), 10), attemptsUsed, lastErr)
}
