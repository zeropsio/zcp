// Tests for: workflow state types and immediate workflow detection.
package workflow

import (
	"testing"
)

func TestIsImmediateWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		workflow string
		want     bool
	}{
		{"debug", "debug", true},
		{"scale", "scale", false},
		{"configure", "configure", true},
		{"deploy", "deploy", false},
		{"bootstrap", "bootstrap", false},
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
