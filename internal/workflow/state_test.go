// Tests for: workflow state types, phase validation, and transitions.
package workflow

import (
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
