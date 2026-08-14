package wbi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
)

var mixinTable = []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52}

type Keys struct {
	Img, Sub  string
	ExpiresAt time.Time
}
type KeySource interface {
	Keys(context.Context) (Keys, error)
}

type Signer struct {
	source KeySource
	clock  func() time.Time
	mu     sync.Mutex
	cached Keys
}

func New(source KeySource) *Signer { return &Signer{source: source, clock: time.Now} }
func NewWithClock(source KeySource, clock func() time.Time) *Signer {
	return &Signer{source: source, clock: clock}
}
func (s *Signer) Invalidate() { s.mu.Lock(); s.cached = Keys{}; s.mu.Unlock() }

func (s *Signer) Sign(ctx context.Context, values url.Values) (url.Values, error) {
	keys, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	return Sign(values, keys.Img, keys.Sub, s.clock().Unix())
}

func (s *Signer) keys(ctx context.Context) (Keys, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.cached.Img != "" && now.Before(s.cached.ExpiresAt) {
		return s.cached, nil
	}
	keys, err := s.source.Keys(ctx)
	if err != nil {
		return Keys{}, err
	}
	if keys.ExpiresAt.IsZero() {
		keys.ExpiresAt = now.Add(6 * time.Hour)
	}
	s.cached = keys
	return keys, nil
}

func Sign(values url.Values, imgKey, subKey string, wts int64) (url.Values, error) {
	mixin, err := mixinKey(imgKey + subKey)
	if err != nil {
		return nil, err
	}
	result := make(url.Values, len(values)+2)
	for key, list := range values {
		for _, value := range list {
			result.Add(key, strings.Map(func(r rune) rune {
				if strings.ContainsRune("!'()*", r) {
					return -1
				}
				return r
			}, value))
		}
	}
	result.Del("w_rid")
	result.Set("wts", fmt.Sprintf("%d", wts))
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, escape(key)+"="+escape(result.Get(key)))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + mixin))
	result.Set("w_rid", hex.EncodeToString(sum[:]))
	return result, nil
}

func escape(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }

func mixinKey(origin string) (string, error) {
	if len(origin) < 64 {
		return "", errors.New("WBI key material is shorter than 64 characters")
	}
	var b strings.Builder
	for _, index := range mixinTable {
		b.WriteByte(origin[index])
	}
	return b.String()[:32], nil
}

type NavSource struct{ Client *client.Client }

func (n NavSource) Keys(ctx context.Context) (Keys, error) {
	type img struct {
		ImgURL string `json:"img_url"`
		SubURL string `json:"sub_url"`
	}
	var result client.Envelope[struct {
		WBI img `json:"wbi_img"`
	}]
	if err := n.Client.GetJSON(ctx, endpoints.Nav, nil, &result); err != nil {
		return Keys{}, err
	}
	if result.Data.WBI.ImgURL == "" || result.Data.WBI.SubURL == "" {
		if err := client.Check(result.Code, result.Message); err != nil {
			return Keys{}, err
		}
		return Keys{}, errors.New("nav response omitted wbi_img")
	}
	return Keys{Img: keyName(result.Data.WBI.ImgURL), Sub: keyName(result.Data.WBI.SubURL), ExpiresAt: time.Now().Add(6 * time.Hour)}, nil
}
func keyName(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	return strings.TrimSuffix(name, path.Ext(name))
}
