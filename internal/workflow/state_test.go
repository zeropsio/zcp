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
		{"debug_removed", "debug", false},
		{"scale", "scale", false},
		{"configure_removed", "configure", false},
		{"deploy", "deploy", false},
		{"bootstrap", "bootstrap", false},
		{"cicd", "cicd", true},
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
