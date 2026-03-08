// Tests for: workflow state types, phase validation, and transitions.
package workflow

import (
	"encoding/json"
	"testing"
)

func TestPhaseSequence_ReturnsOrchestratedPhases(t *testing.T) {
	t.Parallel()
	expected := []Phase{PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone}
	got := PhaseSequence()
	assertPhaseSlice(t, expected, got)
}

func TestValidNextPhase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  Phase
		expected []Phase
	}{
		{"init_to_discover", PhaseInit, []Phase{PhaseDiscover}},
		{"discover_to_develop", PhaseDiscover, []Phase{PhaseDevelop}},
		{"develop_to_deploy", PhaseDevelop, []Phase{PhaseDeploy}},
		{"deploy_to_verify", PhaseDeploy, []Phase{PhaseVerify}},
		{"verify_to_done", PhaseVerify, []Phase{PhaseDone}},
		{"done_terminal", PhaseDone, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidNextPhase(tt.current)
			assertPhaseSlice(t, tt.expected, got)
		})
	}
}

func TestIsValidTransition_ValidCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from Phase
		to   Phase
	}{
		{"init_to_discover", PhaseInit, PhaseDiscover},
		{"discover_to_develop", PhaseDiscover, PhaseDevelop},
		{"develop_to_deploy", PhaseDevelop, PhaseDeploy},
		{"deploy_to_verify", PhaseDeploy, PhaseVerify},
		{"verify_to_done", PhaseVerify, PhaseDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !IsValidTransition(tt.from, tt.to) {
				t.Errorf("expected %s → %s to be valid", tt.from, tt.to)
			}
		})
	}
}

func TestIsValidTransition_InvalidCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from Phase
		to   Phase
	}{
		{"skip_phase", PhaseInit, PhaseDevelop},
		{"backward", PhaseDeploy, PhaseDiscover},
		{"skip_to_deploy", PhaseInit, PhaseDeploy},
		{"same_phase", PhaseInit, PhaseInit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsValidTransition(tt.from, tt.to) {
				t.Errorf("expected %s → %s to be invalid", tt.from, tt.to)
			}
		})
	}
}

func TestIsImmediateWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		workflow string
		want     bool
	}{
		{"debug", "debug", true},
		{"scale", "scale", true},
		{"configure", "configure", true},
		{"bootstrap", "bootstrap", false},
		{"deploy", "deploy", false},
		{"unknown", "nonexistent", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsImmediateWorkflow(tt.workflow); got != tt.want {
				t.Errorf("IsImmediateWorkflow(%q) = %v, want %v", tt.workflow, got, tt.want)
			}
		})
	}
}

func TestStateUnknown_IsValidProjectState(t *testing.T) {
	t.Parallel()
	if StateUnknown != ProjectState("UNKNOWN") {
		t.Errorf("StateUnknown: want UNKNOWN, got %s", StateUnknown)
	}
	// Verify it's distinct from the other states.
	states := []ProjectState{StateFresh, StateConformant, StateNonConformant, StateUnknown}
	seen := make(map[ProjectState]bool, len(states))
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate ProjectState: %s", s)
		}
		seen[s] = true
	}
}

// --- Item 22: ContextDelivery tests ---

func TestContextDelivery_Serialization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cd   ContextDelivery
	}{
		{
			name: "empty",
			cd:   ContextDelivery{GuideSentFor: make(map[string]int)},
		},
		{
			name: "populated",
			cd: ContextDelivery{
				GuideSentFor: map[string]int{"discover": 0, "provision": 1},
				StacksSentAt: "2026-03-08T10:00:00Z",
				ScopeLoaded:  true,
				BriefingFor:  "nodejs@22+postgresql@16",
			},
		},
		{
			name: "scope_only",
			cd: ContextDelivery{
				GuideSentFor: make(map[string]int),
				ScopeLoaded:  true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.cd)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got ContextDelivery
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.ScopeLoaded != tt.cd.ScopeLoaded {
				t.Errorf("ScopeLoaded: want %v, got %v", tt.cd.ScopeLoaded, got.ScopeLoaded)
			}
			if got.StacksSentAt != tt.cd.StacksSentAt {
				t.Errorf("StacksSentAt: want %q, got %q", tt.cd.StacksSentAt, got.StacksSentAt)
			}
			if got.BriefingFor != tt.cd.BriefingFor {
				t.Errorf("BriefingFor: want %q, got %q", tt.cd.BriefingFor, got.BriefingFor)
			}
			if len(got.GuideSentFor) != len(tt.cd.GuideSentFor) {
				t.Errorf("GuideSentFor length: want %d, got %d", len(tt.cd.GuideSentFor), len(got.GuideSentFor))
			}
			for k, v := range tt.cd.GuideSentFor {
				if got.GuideSentFor[k] != v {
					t.Errorf("GuideSentFor[%q]: want %d, got %d", k, v, got.GuideSentFor[k])
				}
			}
		})
	}
}

func TestContextDelivery_GuideSentFor(t *testing.T) {
	t.Parallel()
	cd := ContextDelivery{GuideSentFor: make(map[string]int)}

	// Record guide delivery for discover at iteration 0.
	cd.GuideSentFor["discover"] = 0
	if cd.GuideSentFor["discover"] != 0 {
		t.Errorf("GuideSentFor[discover]: want 0, got %d", cd.GuideSentFor["discover"])
	}

	// Update for iteration 1.
	cd.GuideSentFor["discover"] = 1
	if cd.GuideSentFor["discover"] != 1 {
		t.Errorf("GuideSentFor[discover]: want 1, got %d", cd.GuideSentFor["discover"])
	}

	// Different step at different iteration.
	cd.GuideSentFor["provision"] = 0
	if len(cd.GuideSentFor) != 2 {
		t.Errorf("GuideSentFor length: want 2, got %d", len(cd.GuideSentFor))
	}
}

// assertPhaseSlice compares two phase slices for equality.
func assertPhaseSlice(t *testing.T, want, got []Phase) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("phase slice length: want %d, got %d\nwant: %v\ngot:  %v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("phase[%d]: want %s, got %s", i, want[i], got[i])
		}
	}
}
