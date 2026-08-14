package video

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

type CachedResolver struct {
	next  VideoResolver
	store *store.Store
	ttl   time.Duration
}

func NewCachedResolver(next VideoResolver, store *store.Store, ttl time.Duration) *CachedResolver {
	return &CachedResolver{next: next, store: store, ttl: ttl}
}
func (c *CachedResolver) Resolve(ctx context.Context, input string, page int) (*ResolvedVideo, error) {
	sum := sha256.Sum256([]byte(input + "\x00" + strconv.Itoa(page)))
	key := hex.EncodeToString(sum[:])
	if value, ok, err := c.store.CacheGet(ctx, "video_cache", key); err == nil && ok {
		var resolved ResolvedVideo
		if json.Unmarshal(value, &resolved) == nil {
			return &resolved, nil
		}
	}
	resolved, err := c.next.Resolve(ctx, input, page)
	if err != nil {
		return nil, err
	}
	if value, err := json.Marshal(resolved); err == nil {
		_ = c.store.CacheSet(ctx, "video_cache", key, value, c.ttl)
	}
	return resolved, nil
}
