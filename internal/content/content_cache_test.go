package content

import (
	"sync"
	"testing"
)

// TestGetWorkflow_CachedAcrossCalls proves the sync.Once cache serves every
// call from the same in-memory map. The proof: call GetWorkflow many times,
// then swap the cache entry for a sentinel and read again — if the sentinel
// comes back, the second call hit the cache (not a fresh embed read).
//
// This test is intentionally non-parallel: it mutates workflowCacheInit and
// workflowCache directly to isolate state from other tests in this package.
// If you ever add a parallel test that also touches these vars, wrap this
// test in a helper that saves/restores global state.
func TestGetWorkflow_CachedAcrossCalls(t *testing.T) {
	// Reset cache for test isolation.
	workflowCacheMu.Lock()
	workflowCacheInit = sync.Once{}
	workflowCache = nil
	errWorkflowCacheInit = nil
	workflowCacheMu.Unlock()
	t.Cleanup(func() {
		workflowCacheMu.Lock()
		workflowCacheInit = sync.Once{}
		workflowCache = nil
		errWorkflowCacheInit = nil
		workflowCacheMu.Unlock()
	})

	// Prime the cache with many calls.
	for i := range 100 {
		s, err := GetWorkflow("recipe")
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if len(s) == 0 {
			t.Fatalf("iteration %d: empty", i)
		}
	}

	// Swap the cached entry for a sentinel. If the next GetWorkflow call
	// serves the sentinel, it's hitting the cache (not re-reading embed).
	workflowCacheMu.Lock()
	workflowCache["recipe"] = "SENTINEL"
	workflowCacheMu.Unlock()

	got, err := GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("post-swap GetWorkflow: %v", err)
	}
	if got != "SENTINEL" {
		t.Errorf("expected cache hit returning SENTINEL, got fresh read of %d bytes", len(got))
	}
}

// TestGetWorkflow_MissingReturnsError preserves the pre-cache error contract:
// unknown names still return a descriptive error, not an empty string.
func TestGetWorkflow_MissingReturnsError(t *testing.T) {
	t.Parallel()
	_, err := GetWorkflow("definitely-does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing workflow, got nil")
	}
}
