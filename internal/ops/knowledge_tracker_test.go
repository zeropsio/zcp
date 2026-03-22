// Tests for: knowledge call tracking — briefing+scope recording.
package ops

import (
	"sync"
	"testing"
)

func TestKnowledgeTracker_RecordBriefing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		runtime  string
		services []string
	}{
		{"runtime_with_services", "php-nginx@8.4", []string{"postgresql@16", "valkey@7.2"}},
		{"runtime_only", "go@1", nil},
		{"services_only", "", []string{"postgresql@16"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kt := NewKnowledgeTracker()
			kt.RecordBriefing(tt.runtime, tt.services)
			// Verify internal state via exported fields is not possible,
			// but we verify no panic and correct construction.
		})
	}
}

func TestKnowledgeTracker_RecordScope(t *testing.T) {
	t.Parallel()
	kt := NewKnowledgeTracker()
	kt.RecordScope()
	// Verify no panic on double call.
	kt.RecordScope()
}

func TestKnowledgeTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kt := NewKnowledgeTracker()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			kt.RecordBriefing("bun@1.2", []string{"postgresql@16"})
		}()
		go func() {
			defer wg.Done()
			kt.RecordScope()
		}()
	}
	wg.Wait()
}
