package ingest

import (
	"strconv"
	"testing"
	"time"
)

func TestDedup_FirstSeen_NotDuplicate(t *testing.T) {
	t.Parallel()

	d := newDedup(1000, 24*time.Hour)
	now := time.Now()

	if d.seen("s1:1", now) {
		t.Error("first occurrence reported as duplicate")
	}
}

func TestDedup_SecondSeen_IsDuplicate(t *testing.T) {
	t.Parallel()

	d := newDedup(1000, 24*time.Hour)
	now := time.Now()

	d.seen("s1:1", now)
	if !d.seen("s1:1", now) {
		t.Error("second occurrence of same event_id not flagged as duplicate")
	}
}

func TestDedup_DistinctIDs_NeitherDuplicate(t *testing.T) {
	t.Parallel()

	d := newDedup(1000, 24*time.Hour)
	now := time.Now()

	if d.seen("s1:1", now) {
		t.Error("s1:1 unexpectedly duplicate")
	}
	if d.seen("s1:2", now) {
		t.Error("s1:2 unexpectedly duplicate")
	}
}

func TestDedup_TTLExpiry_ReadmitsAfterTTL(t *testing.T) {
	t.Parallel()

	d := newDedup(1000, time.Hour)
	t0 := time.Now()

	d.seen("s1:1", t0)
	// Still within TTL: duplicate.
	if !d.seen("s1:1", t0.Add(30*time.Minute)) {
		t.Error("expected duplicate within TTL window")
	}
	// Past TTL: no longer treated as a duplicate.
	if d.seen("s1:1", t0.Add(2*time.Hour)) {
		t.Error("expected TTL-expired entry to be treated as new")
	}
}

func TestDedup_SizeBound_EvictsOldest(t *testing.T) {
	t.Parallel()

	d := newDedup(3, 24*time.Hour)
	now := time.Now()

	d.seen("a", now)
	d.seen("b", now)
	d.seen("c", now)
	// Over capacity: "d" pushes "a" (least-recently-used) out.
	d.seen("d", now)

	if d.seen("a", now) {
		t.Error("evicted entry 'a' unexpectedly still flagged as duplicate")
	}
	if !d.seen("d", now) {
		t.Error("recently inserted 'd' should still be a duplicate on second sight")
	}
}

func TestDedup_ConcurrentAccess_NoRace(t *testing.T) {
	t.Parallel()

	d := newDedup(10000, 24*time.Hour)
	now := time.Now()

	done := make(chan struct{})
	for g := range 8 {
		go func() {
			for i := range 200 {
				d.seen("g"+strconv.Itoa(g)+":"+strconv.Itoa(i), now)
			}
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
}
