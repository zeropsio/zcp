package main

import (
	"testing"

	"github.com/zeropsio/zcp/internal/eval"
)

// TestSelectForAll_FiltersExcludeFromAll pins the `behavioral all` /
// flow-eval `all` selection guard: a scenario with ExcludeFromAll=true is
// dropped from the all-run set, everything else passes through unchanged
// and in order. Direct execution by id (runBehavioralRun) never calls this
// filter, so excluded scenarios stay reachable that way.
func TestSelectForAll_FiltersExcludeFromAll(t *testing.T) {
	t.Parallel()
	in := []*eval.Scenario{
		{ID: "a"},
		{ID: "b", ExcludeFromAll: true},
		{ID: "c"},
		{ID: "d", ExcludeFromAll: true},
		{ID: "e"},
	}
	got := selectForAll(in)

	gotIDs := make([]string, 0, len(got))
	for _, sc := range got {
		gotIDs = append(gotIDs, sc.ID)
	}
	wantIDs := []string{"a", "c", "e"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("selectForAll ids: got %v, want %v", gotIDs, wantIDs)
	}
	for i, id := range wantIDs {
		if gotIDs[i] != id {
			t.Errorf("selectForAll ids: got %v, want %v", gotIDs, wantIDs)
			break
		}
	}
}

// TestSelectForAll_NoExclusions_PassesAllThrough asserts the common case
// (no scenario opts out) is a no-op filter.
func TestSelectForAll_NoExclusions_PassesAllThrough(t *testing.T) {
	t.Parallel()
	in := []*eval.Scenario{{ID: "a"}, {ID: "b"}}
	got := selectForAll(in)
	if len(got) != 2 {
		t.Fatalf("selectForAll: got %d scenarios, want 2", len(got))
	}
}

// TestSelectForAll_RealScenarios_ExcludesLaunchDelegated is the wiring
// proof against the ACTUAL scenario directory: launch-production-delegated
// consumes a live one-time platform delegation on a real run, so it must
// never be selectable by `behavioral all` — loadBehavioralScenarios must
// still find it (list / direct-run stay unaffected), but selectForAll must
// drop it.
func TestSelectForAll_RealScenarios_ExcludesLaunchDelegated(t *testing.T) {
	t.Parallel()
	scenarios, err := loadBehavioralScenarios("../../eval/behavioral/scenarios")
	if err != nil {
		t.Fatalf("loadBehavioralScenarios: %v", err)
	}
	foundLoaded := false
	for _, sc := range scenarios {
		if sc.ID == "launch-production-delegated" {
			foundLoaded = true
			if !sc.ExcludeFromAll {
				t.Fatalf("launch-production-delegated: ExcludeFromAll = false, want true")
			}
		}
	}
	if !foundLoaded {
		t.Fatal("launch-production-delegated not found by loadBehavioralScenarios — list/direct-run would break too")
	}

	selected := selectForAll(scenarios)
	for _, sc := range selected {
		if sc.ID == "launch-production-delegated" {
			t.Fatal("launch-production-delegated must be excluded from selectForAll, but it was selected")
		}
	}
}
