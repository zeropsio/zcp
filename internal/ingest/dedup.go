package ingest

import (
	"container/list"
	"sync"
	"time"
)

// dedupMaxEntries / dedupTTL are the spec §6 item 3 defaults: 24 h TTL,
// size-bounded 1M entries.
const (
	dedupMaxEntries = 1_000_000
	dedupTTL        = 24 * time.Hour
)

type dedupEntry struct {
	key       string
	expiresAt time.Time
}

// dedup is an in-memory LRU-TTL cache on event_id (spec §6 item 3): a
// duplicate event_id within TTL is counted, never re-inserted. Size-bounded
// via LRU eviction so a burst of unique IDs can't grow memory unbounded.
// Instance-owned (no package-level state) — one per ingest process.
type dedup struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	order      *list.List // front = most recently touched
	items      map[string]*list.Element
}

func newDedup(maxEntries int, ttl time.Duration) *dedup {
	return &dedup{
		maxEntries: maxEntries,
		ttl:        ttl,
		order:      list.New(),
		items:      make(map[string]*list.Element, 1024),
	}
}

// seen reports whether id was already recorded within the TTL window (a
// duplicate), and otherwise records it. An entry whose TTL has expired is
// treated as new (readmitted) and its expiry is refreshed.
func (d *dedup) seen(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if el, ok := d.items[id]; ok {
		entry := el.Value.(*dedupEntry) //nolint:forcetypeassert // internal invariant: items always holds *dedupEntry
		if now.Before(entry.expiresAt) {
			d.order.MoveToFront(el)
			return true
		}
		d.order.Remove(el)
		delete(d.items, id)
	}

	entry := &dedupEntry{key: id, expiresAt: now.Add(d.ttl)}
	el := d.order.PushFront(entry)
	d.items[id] = el

	if d.order.Len() > d.maxEntries {
		oldest := d.order.Back()
		if oldest != nil {
			d.order.Remove(oldest)
			delete(d.items, oldest.Value.(*dedupEntry).key) //nolint:forcetypeassert // see above
		}
	}
	return false
}
