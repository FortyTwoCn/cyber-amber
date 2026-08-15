package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type Int64 int64

func (i *Int64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte(`""`)) {
		*i = 0
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid numeric string %q", value)
		}
		*i = Int64(parsed)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	parsed, err := number.Int64()
	if err != nil {
		return err
	}
	*i = Int64(parsed)
	return nil
}

// ScalarString preserves Bilibili enum-like values while accepting the two
// representations seen in message-feed responses: JSON strings and numbers.
// Unlike Int64, it intentionally does not interpret the value because names
// such as "reply" are valid business types.
type ScalarString string

func (s *ScalarString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = ScalarString(value)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("expected string or number: %w", err)
	}
	*s = ScalarString(number.String())
	return nil
}

type response struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	TTL     int       `json:"ttl"`
	Data    *feedData `json:"data"`
}
type feedData struct {
	Cursor feedCursor     `json:"cursor"`
	Items  []notification `json:"items"`
}
type feedCursor struct {
	IsEnd bool  `json:"is_end"`
	ID    Int64 `json:"id"`
	Time  Int64 `json:"time"`
}
type notification struct {
	ID     Int64 `json:"id"`
	AtTime Int64 `json:"at_time"`
	User   struct {
		MID      Int64  `json:"mid"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	} `json:"user"`
	Item notificationItem `json:"item"`
}
type notificationItem struct {
	Type          ScalarString `json:"type"`
	Business      string       `json:"business"`
	BusinessID    Int64        `json:"business_id"`
	SubjectID     Int64        `json:"subject_id"`
	RootID        Int64        `json:"root_id"`
	SourceID      Int64        `json:"source_id"`
	TargetID      Int64        `json:"target_id"`
	URI           string       `json:"uri"`
	Content       string       `json:"content"`
	SourceContent string       `json:"source_content"`
	Title         string       `json:"title"`
	AtDetails     []struct {
		MID   Int64  `json:"mid"`
		Uname string `json:"uname"`
	} `json:"at_details"`
}

type Cursor struct {
	ID, Time    int64
	Initialized bool
}
type Event struct {
	NotificationID                        string
	At                                    int64
	SenderMID                             int64
	SenderName, SenderAvatar, Message     string
	SubjectID, RootID, SourceID, TargetID int64
	Business                              string
	BusinessType                          string
	URI                                   string
	AID                                   int64
	BVID                                  string
	RPID, RootRPID                        int64
	Page                                  int
	RawJSON                               string
	MentionedMIDs                         []int64
}
type Page struct {
	Events []Event
	Next   Cursor
	End    bool
}
