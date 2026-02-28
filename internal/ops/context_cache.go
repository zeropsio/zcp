package ops

import (
	"context"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// DefaultStackTypeCacheTTL is the default time-to-live for cached service stack types.
const DefaultStackTypeCacheTTL = 1 * time.Hour

// StackTypeCache caches service stack types with a TTL.
type StackTypeCache struct {
	mu        sync.Mutex
	types     []platform.ServiceStackType
	fetchedAt time.Time
	ttl       time.Duration
}

// NewStackTypeCache creates a new StackTypeCache with the given TTL.
func NewStackTypeCache(ttl time.Duration) *StackTypeCache {
	return &StackTypeCache{ttl: ttl}
}

// Get returns cached service stack types, refreshing from the API when expired.
// On API error: returns stale data if available, else nil.
// The mutex is released before network I/O to avoid blocking concurrent callers.
func (c *StackTypeCache) Get(ctx context.Context, client platform.Client) []platform.ServiceStackType {
	c.mu.Lock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < c.ttl {
		result := c.types
		c.mu.Unlock()
		return result
	}
	c.mu.Unlock()

	// Network I/O outside lock.
	types, err := client.ListServiceStackTypes(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		return c.types // stale or nil
	}
	c.types = types
	c.fetchedAt = time.Now()
	return c.types
}
