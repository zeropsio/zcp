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
// Thread-safe: uses double-checked locking to minimize lock contention.
type StackTypeCache struct {
	mu        sync.RWMutex
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
func (c *StackTypeCache) Get(ctx context.Context, client platform.Client) []platform.ServiceStackType {
	c.mu.RLock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < c.ttl {
		types := c.types
		c.mu.RUnlock()
		return types
	}
	stale := c.types
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: another goroutine may have refreshed while we waited for the write lock.
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < c.ttl {
		return c.types
	}

	types, err := client.ListServiceStackTypes(ctx)
	if err != nil {
		return stale
	}

	c.types = types
	c.fetchedAt = time.Now()
	return c.types
}
