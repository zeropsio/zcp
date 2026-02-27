// Tests for: knowledge call tracking — briefing+scope recording, IsLoaded, Summary.
package ops

import (
	"strings"
	"sync"
	"testing"
)

func TestKnowledgeTracker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(kt *KnowledgeTracker)
		wantLoaded    bool
		wantSummary   string // substring expected in Summary()
		noWantSummary string // substring NOT expected in Summary()
	}{
		{
			name:       "empty_tracker",
			setup:      func(_ *KnowledgeTracker) {},
			wantLoaded: false,
		},
		{
			name: "briefing_only",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordBriefing("php-nginx@8.4", []string{"postgresql@16", "valkey@7.2"})
			},
			wantLoaded:  false,
			wantSummary: "php-nginx@8.4",
		},
		{
			name: "scope_only",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordScope()
			},
			wantLoaded:  false,
			wantSummary: "infrastructure",
		},
		{
			name: "both_loaded",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordBriefing("bun@1.2", []string{"postgresql@16"})
				kt.RecordScope()
			},
			wantLoaded:  true,
			wantSummary: "bun@1.2",
		},
		{
			name: "summary_format",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordBriefing("php-nginx@8.4", []string{"postgresql@16", "valkey@7.2"})
				kt.RecordScope()
			},
			wantLoaded:  true,
			wantSummary: "infrastructure",
		},
		{
			name: "briefing_no_services",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordBriefing("go@1", nil)
				kt.RecordScope()
			},
			wantLoaded:  true,
			wantSummary: "go@1",
		},
		{
			name: "multiple_briefings",
			setup: func(kt *KnowledgeTracker) {
				kt.RecordBriefing("bun@1.2", []string{"postgresql@16"})
				kt.RecordBriefing("go@1", nil)
				kt.RecordScope()
			},
			wantLoaded:  true,
			wantSummary: "go@1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kt := NewKnowledgeTracker()
			tt.setup(kt)

			if got := kt.IsLoaded(); got != tt.wantLoaded {
				t.Errorf("IsLoaded(): want %v, got %v", tt.wantLoaded, got)
			}

			if tt.wantSummary != "" {
				summary := kt.Summary()
				if !strings.Contains(summary, tt.wantSummary) {
					t.Errorf("Summary() = %q, want substring %q", summary, tt.wantSummary)
				}
			}
			if tt.noWantSummary != "" {
				summary := kt.Summary()
				if strings.Contains(summary, tt.noWantSummary) {
					t.Errorf("Summary() = %q, should NOT contain %q", summary, tt.noWantSummary)
				}
			}
		})
	}
}

func TestKnowledgeTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kt := NewKnowledgeTracker()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			kt.RecordBriefing("bun@1.2", []string{"postgresql@16"})
		}()
		go func() {
			defer wg.Done()
			kt.RecordScope()
		}()
		go func() {
			defer wg.Done()
			_ = kt.IsLoaded()
			_ = kt.Summary()
		}()
	}
	wg.Wait()

	if !kt.IsLoaded() {
		t.Error("expected IsLoaded() = true after concurrent writes")
	}
}
