package video

import (
	"context"
	"encoding/json"
	"time"
)

type Page struct {
	CID             int64  `json:"cid"`
	Number          int    `json:"page"`
	Title           string `json:"part"`
	DurationSeconds int64  `json:"duration"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
}

func (p Page) Duration() time.Duration { return time.Duration(p.DurationSeconds) * time.Second }

type ResolvedVideo struct {
	AID             int64  `json:"aid"`
	BVID            string `json:"bvid"`
	Title           string `json:"title"`
	OwnerMID        int64  `json:"owner_mid"`
	OwnerName       string `json:"owner_name"`
	CoverURL        string `json:"cover_url"`
	DurationSeconds int64  `json:"duration"`
	Pages           []Page `json:"pages"`
	SelectedPage    int    `json:"selected_page"`
	SelectedCID     int64  `json:"selected_cid"`
}

type VideoResolver interface {
	Resolve(context.Context, string, int) (*ResolvedVideo, error)
}

type Track struct {
	ID         int
	BaseURL    string
	BackupURLs []string
	Codecs     string
	CodecID    int
	Width      int
	Height     int
	FrameRate  string
	Bandwidth  int64
}

func (t *Track) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID             int      `json:"id"`
		BaseURL        string   `json:"baseUrl"`
		BaseURLSnake   string   `json:"base_url"`
		BackupURLs     []string `json:"backupUrl"`
		BackupSnake    []string `json:"backup_url"`
		Codecs         string   `json:"codecs"`
		CodecID        int      `json:"codecid"`
		Width          int      `json:"width"`
		Height         int      `json:"height"`
		FrameRate      string   `json:"frameRate"`
		FrameRateSnake string   `json:"frame_rate"`
		Bandwidth      int64    `json:"bandwidth"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.ID, t.Codecs, t.CodecID, t.Width, t.Height, t.Bandwidth = raw.ID, raw.Codecs, raw.CodecID, raw.Width, raw.Height, raw.Bandwidth
	t.BaseURL = raw.BaseURL
	if t.BaseURL == "" {
		t.BaseURL = raw.BaseURLSnake
	}
	t.BackupURLs = raw.BackupURLs
	if len(t.BackupURLs) == 0 {
		t.BackupURLs = raw.BackupSnake
	}
	t.FrameRate = raw.FrameRate
	if t.FrameRate == "" {
		t.FrameRate = raw.FrameRateSnake
	}
	return nil
}

type Stream struct {
	DurationSeconds int64
	Tracks          []Track
	Selected        Track
}
