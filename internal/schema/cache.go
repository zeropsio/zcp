package schema

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultCacheTTL is the default time-to-live for cached schemas.
const DefaultCacheTTL = 24 * time.Hour

// fetchTimeout is the per-request timeout for schema fetches.
const fetchTimeout = 10 * time.Second

// maxResponseBytes caps schema response bodies to 5MB to prevent OOM from misbehaving servers.
const maxResponseBytes = 5 << 20

// Cache provides TTL-cached access to live Zerops schemas.
// Thread-safe. Coalesces concurrent fetches. On fetch error, returns stale data if available.
type Cache struct {
	mu        sync.Mutex
	schemas   *Schemas
	fetchedAt time.Time
	ttl       time.Duration

	// fetchCh is non-nil when a fetch is in progress. Concurrent callers
	// wait on this channel instead of firing duplicate HTTP requests.
	fetchCh chan struct{}
}

// NewCache creates a new schema cache with the given TTL.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl}
}

// Get returns cached schemas, refreshing from the API when expired.
// Coalesces concurrent requests: only one goroutine fetches while others wait.
// Returns nil on first-fetch failure. Returns stale data on refresh failure.
func (c *Cache) Get(ctx context.Context) *Schemas {
	c.mu.Lock()

	// Fast path: cache is fresh.
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < c.ttl {
		result := c.schemas
		c.mu.Unlock()
		return result
	}

	// Another goroutine is already fetching — wait for it.
	if c.fetchCh != nil {
		ch := c.fetchCh
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		result := c.schemas
		c.mu.Unlock()
		return result
	}

	// We are the fetcher. Signal others to wait.
	ch := make(chan struct{})
	c.fetchCh = ch
	c.mu.Unlock()

	// Fetch outside lock (no mutex held during I/O).
	schemas, err := FetchSchemas(ctx)

	c.mu.Lock()
	if err == nil {
		c.schemas = schemas
		c.fetchedAt = time.Now()
	}
	c.fetchCh = nil
	c.mu.Unlock()

	// Wake all waiters.
	close(ch)

	if err != nil {
		// Return stale data (or nil on first-fetch failure).
		c.mu.Lock()
		result := c.schemas
		c.mu.Unlock()
		return result
	}
	return schemas
}

// FetchSchemas fetches both schemas from the public API.
func FetchSchemas(ctx context.Context) (*Schemas, error) {
	zeropsData, err := fetchURL(ctx, ZeropsYmlURL)
	if err != nil {
		return nil, fmt.Errorf("fetch zerops.yaml schema: %w", err)
	}
	importData, err := fetchURL(ctx, ImportYmlURL)
	if err != nil {
		return nil, fmt.Errorf("fetch import.yaml schema: %w", err)
	}

	zeropsYml, err := ParseZeropsYmlSchema(zeropsData)
	if err != nil {
		return nil, err
	}
	importYml, err := ParseImportYmlSchema(importData)
	if err != nil {
		return nil, err
	}

	return &Schemas{
		ZeropsYml: zeropsYml,
		ImportYml: importYml,
	}, nil
}

// fetchURL performs an HTTP GET with timeout and response size limit.
func fetchURL(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
}
