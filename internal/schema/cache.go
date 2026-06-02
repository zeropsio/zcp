package schema

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultCacheTTL is the default time-to-live for cached schemas. Kept short
// (15 min) so recipe pre-checks pick up a brand-new platform base/type without
// a ZCP release; every refresh is single-flight-coalesced, bounded, and
// poison-guarded, so the higher frequency vs a long TTL is off any
// latency-critical path (deploy/import use the platform API directly).
const DefaultCacheTTL = 15 * time.Minute

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
	apiHost   string

	// fetchCh is non-nil when a fetch is in progress. Concurrent callers
	// wait on this channel instead of firing duplicate HTTP requests.
	fetchCh chan struct{}
}

// NewCache creates a new schema cache with the given TTL, seeded with the
// embedded schemas so Get is never nil. The seed has fetchedAt == zero, so the
// FIRST Get still performs a live fetch (the seed is the value-to-return-on-
// failure, not a fresh entry that suppresses fetching). Without the seed, a
// cold-start or first-fetch failure returned nil and recipe pre-checks
// silently skipped.
// apiHost is the resolved Zerops host (ZCP_API_HOST) the live fetch targets;
// empty defaults to CanonicalAPIHost via URLs, so the runtime validates
// against the instance the user actually deploys to rather than a hardcoded
// region.
func NewCache(ttl time.Duration, apiHost string) *Cache {
	return &Cache{ttl: ttl, apiHost: apiHost, schemas: embeddedSchemas()}
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
	schemas, err := FetchSchemas(ctx, c.apiHost)

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

// FetchSchemas fetches both schemas from the public API of the given host
// (empty → CanonicalAPIHost via URLs).
func FetchSchemas(ctx context.Context, apiHost string) (*Schemas, error) {
	zeropsURL, importURL := URLs(apiHost)
	zeropsData, err := fetchURL(ctx, zeropsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch zerops.yaml schema: %w", err)
	}
	importData, err := fetchURL(ctx, importURL)
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

	if err := rejectEmptyEnums(zeropsYml, importYml); err != nil {
		return nil, err
	}

	return &Schemas{
		ZeropsYml: zeropsYml,
		ImportYml: importYml,
	}, nil
}

// rejectEmptyEnums is the poison guard. A HTTP-200 body that is not actually a
// schema (observed in production: {"error":{"code":"502"}}) JSON-parses cleanly
// but extracts to EMPTY enums. Returning an error here makes Cache.Get keep its
// last-good value (or the embedded seed) instead of overwriting it with garbage,
// and makes `zcp catalog sync` refuse to write a poisoned version catalog.
func rejectEmptyEnums(zeropsYml *ZeropsYmlSchema, importYml *ImportYmlSchema) error {
	switch {
	case zeropsYml == nil || importYml == nil:
		return fmt.Errorf("schema fetch produced nil schema")
	case len(zeropsYml.BuildBases) == 0, len(zeropsYml.RunBases) == 0, len(importYml.ServiceTypes) == 0:
		return fmt.Errorf("schema fetch returned empty enums (build=%d run=%d types=%d) — likely a non-schema body",
			len(zeropsYml.BuildBases), len(zeropsYml.RunBases), len(importYml.ServiceTypes))
	}
	return nil
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
