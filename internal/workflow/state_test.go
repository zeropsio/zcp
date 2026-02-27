// Tests for: workflow state types, phase validation, and transitions.
package workflow

import (
	"testing"
)

func TestValidNextPhase_FullMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  Phase
		expected []Phase
	}{
		{"init", PhaseInit, []Phase{PhaseDiscover}},
		{"discover", PhaseDiscover, []Phase{PhaseDevelop}},
		{"develop", PhaseDevelop, []Phase{PhaseDeploy}},
		{"deploy", PhaseDeploy, []Phase{PhaseVerify}},
		{"verify", PhaseVerify, []Phase{PhaseDone}},
		{"done_terminal", PhaseDone, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidNextPhase(tt.current, ModeFull)
			assertPhaseSlice(t, tt.expected, got)
		})
	}
}

func TestValidNextPhase_DevOnlyMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  Phase
		expected []Phase
	}{
		{"init", PhaseInit, []Phase{PhaseDiscover}},
		{"discover", PhaseDiscover, []Phase{PhaseDevelop}},
		{"develop_goes_to_done", PhaseDevelop, []Phase{PhaseDone}},
		{"done_terminal", PhaseDone, nil},
		{"deploy_not_in_sequence", PhaseDeploy, nil},
		{"verify_not_in_sequence", PhaseVerify, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidNextPhase(tt.current, ModeDevOnly)
			assertPhaseSlice(t, tt.expected, got)
		})
	}
}

func TestValidNextPhase_HotfixMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  Phase
		expected []Phase
	}{
		{"init", PhaseInit, []Phase{PhaseDevelop}},
		{"develop", PhaseDevelop, []Phase{PhaseDeploy}},
		{"deploy", PhaseDeploy, []Phase{PhaseVerify}},
		{"verify", PhaseVerify, []Phase{PhaseDone}},
		{"done_terminal", PhaseDone, nil},
		{"discover_not_in_sequence", PhaseDiscover, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidNextPhase(tt.current, ModeHotfix)
			assertPhaseSlice(t, tt.expected, got)
		})
	}
}

func TestValidNextPhase_QuickMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  Phase
		expected []Phase
	}{
		{"init_to_develop", PhaseInit, []Phase{PhaseDevelop}},
		{"develop_to_deploy", PhaseDevelop, []Phase{PhaseDeploy}},
		{"deploy_to_verify", PhaseDeploy, []Phase{PhaseVerify}},
		{"verify_to_done", PhaseVerify, []Phase{PhaseDone}},
		{"done_terminal", PhaseDone, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidNextPhase(tt.current, ModeQuick)
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
		mode Mode
	}{
		{"full_init_to_discover", PhaseInit, PhaseDiscover, ModeFull},
		{"full_discover_to_develop", PhaseDiscover, PhaseDevelop, ModeFull},
		{"full_develop_to_deploy", PhaseDevelop, PhaseDeploy, ModeFull},
		{"full_deploy_to_verify", PhaseDeploy, PhaseVerify, ModeFull},
		{"full_verify_to_done", PhaseVerify, PhaseDone, ModeFull},
		{"devonly_develop_to_done", PhaseDevelop, PhaseDone, ModeDevOnly},
		{"hotfix_init_to_develop", PhaseInit, PhaseDevelop, ModeHotfix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !IsValidTransition(tt.from, tt.to, tt.mode) {
				t.Errorf("expected %s → %s to be valid in mode %s", tt.from, tt.to, tt.mode)
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
		mode Mode
	}{
		{"full_skip_phase", PhaseInit, PhaseDevelop, ModeFull},
		{"full_backward", PhaseDeploy, PhaseDiscover, ModeFull},
		{"devonly_has_no_deploy", PhaseDevelop, PhaseDeploy, ModeDevOnly},
		{"hotfix_has_no_discover", PhaseInit, PhaseDiscover, ModeHotfix},
		{"quick_skip_phase", PhaseInit, PhaseDeploy, ModeQuick},
		{"same_phase", PhaseInit, PhaseInit, ModeFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsValidTransition(tt.from, tt.to, tt.mode) {
				t.Errorf("expected %s → %s to be invalid in mode %s", tt.from, tt.to, tt.mode)
			}
		})
	}
}

func TestPhaseSequence_ReturnsCorrectSequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     Mode
		expected []Phase
	}{
		{"full", ModeFull, []Phase{PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone}},
		{"dev_only", ModeDevOnly, []Phase{PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDone}},
		{"hotfix", ModeHotfix, []Phase{PhaseInit, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone}},
		{"quick", ModeQuick, []Phase{PhaseInit, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := PhaseSequence(tt.mode)
			assertPhaseSlice(t, tt.expected, got)
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
