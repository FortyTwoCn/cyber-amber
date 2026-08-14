package danmaku

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

type CachedProvider struct {
	next  Provider
	store *store.Store
	ttl   time.Duration
}

func NewCachedProvider(next Provider, store *store.Store, ttl time.Duration) *CachedProvider {
	return &CachedProvider{next: next, store: store, ttl: ttl}
}
func (c *CachedProvider) FetchRange(ctx context.Context, aid, cid int64, start, end time.Duration) ([]Item, error) {
	rawKey := fmt.Sprintf("%d:%d:%d:%d", aid, cid, start.Milliseconds(), end.Milliseconds())
	sum := sha256.Sum256([]byte(rawKey))
	key := hex.EncodeToString(sum[:])
	if value, ok, err := c.store.CacheGet(ctx, "danmaku_cache", key); err == nil && ok {
		var items []Item
		if json.Unmarshal(value, &items) == nil {
			return items, nil
		}
	}
	items, err := c.next.FetchRange(ctx, aid, cid, start, end)
	if err != nil {
		return nil, err
	}
	if value, err := json.Marshal(items); err == nil {
		_ = c.store.CacheSet(ctx, "danmaku_cache", key, value, c.ttl)
	}
	return items, nil
}
