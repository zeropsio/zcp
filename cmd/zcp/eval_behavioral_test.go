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

// TestExecutionBinding_ParseFlags_AllOrNothing pins
// docs/spec-testing-architecture.md §10.4 "Binding": the four core binding
// flags are all-or-nothing, `behavioral all` refuses any binding flag, and
// --work-dir/--results-dir are extracted regardless of whether a binding is
// present.
func TestExecutionBinding_ParseFlags_AllOrNothing(t *testing.T) {
	t.Parallel()

	t.Run("no binding flags at all", func(t *testing.T) {
		t.Parallel()
		clean, flags, err := parseExecutionBindingFlags([]string{"--file", "scenario.md"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if flags.any {
			t.Fatalf("flags.any = true, want false")
		}
		if len(clean) != 2 || clean[0] != "--file" || clean[1] != "scenario.md" {
			t.Fatalf("clean = %v, want passthrough of non-binding args", clean)
		}
	})

	t.Run("partial binding is a usage error", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseExecutionBindingFlags([]string{"--candidate", "/bin/zcp"})
		if err == nil {
			t.Fatal("expected an error for a partial binding, got nil")
		}
	})

	t.Run("complete binding parses clean", func(t *testing.T) {
		t.Parallel()
		clean, flags, err := parseExecutionBindingFlags([]string{
			"--file", "scenario.md",
			"--candidate", "/bin/zcp",
			"--candidate-sha256", "deadbeef",
			"--project-id", "proj-1",
			"--ack-disposable-project", "yes",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !flags.any {
			t.Fatal("flags.any = false, want true")
		}
		if flags.candidate != "/bin/zcp" || flags.sha256 != "deadbeef" || flags.projectID != "proj-1" || flags.ack != "yes" {
			t.Fatalf("flags = %+v, want the four core binding values", flags)
		}
		if len(clean) != 2 || clean[0] != "--file" || clean[1] != "scenario.md" {
			t.Fatalf("clean = %v, want binding flags stripped", clean)
		}
	})

	t.Run("work and results dir override the env value", func(t *testing.T) {
		t.Parallel()
		_, flags, err := parseExecutionBindingFlags([]string{"--work-dir", "/tmp/w", "--results-dir", "/tmp/r"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if flags.workDir != "/tmp/w" || flags.resultsDir != "/tmp/r" {
			t.Fatalf("flags = %+v, want workDir=/tmp/w resultsDir=/tmp/r", flags)
		}
	})

	t.Run("all rejects any binding flag", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseExecutionBindingFlags([]string{
			"--candidate", "/bin/zcp",
			"--candidate-sha256", "deadbeef",
			"--project-id", "proj-1",
			"--ack-disposable-project", "yes",
		})
		if err != nil {
			t.Fatalf("unexpected error building flags: %v", err)
		}
		if err := rejectBindingFlagsForAll([]string{
			"--candidate", "/bin/zcp",
			"--candidate-sha256", "deadbeef",
			"--project-id", "proj-1",
			"--ack-disposable-project", "yes",
		}); err == nil {
			t.Fatal("rejectBindingFlagsForAll: expected an error, got nil")
		}
		if err := rejectBindingFlagsForAll([]string{"--scenarios-dir", "dir"}); err != nil {
			t.Fatalf("rejectBindingFlagsForAll: unexpected error for a non-binding arg set: %v", err)
		}
	})
}

// TestExecutionBinding_AllRefusesDirAndRunIDFlags pins that `behavioral all`
// refuses --work-dir/--results-dir/--run-id as it refuses the binding flags,
// instead of accepting and silently ignoring them.
func TestExecutionBinding_AllRefusesDirAndRunIDFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--scenarios-dir", "x", "--work-dir", "/tmp/w"},
		{"--scenarios-dir", "x", "--results-dir", "/tmp/r"},
		{"--scenarios-dir", "x", "--run-id", "r1"},
	} {
		if err := rejectBindingFlagsForAll(args); err == nil {
			t.Errorf("rejectBindingFlagsForAll(%v) = nil, want an error", args)
		}
	}
}
