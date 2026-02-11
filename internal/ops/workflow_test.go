// Tests for: plans/analysis/ops.md § ops/workflow.go
package ops

import (
	"strings"
	"testing"
)

func TestGetWorkflowCatalog_NonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contains []string
	}{
		{
			name:     "contains_all_workflows",
			contains: []string{"bootstrap", "deploy", "debug", "scale", "configure", "monitor"},
		},
	}

	catalog := GetWorkflowCatalog()
	if catalog == "" {
		t.Fatal("GetWorkflowCatalog() returned empty string")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, wf := range tt.contains {
				if !strings.Contains(catalog, wf) {
					t.Errorf("catalog does not contain workflow %q", wf)
				}
			}
		})
	}
}

func TestGetWorkflow_KnownWorkflows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workflow string
	}{
		{"bootstrap", "bootstrap"},
		{"deploy", "deploy"},
		{"debug", "debug"},
		{"scale", "scale"},
		{"configure", "configure"},
		{"monitor", "monitor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := GetWorkflow(tt.workflow)
			if err != nil {
				t.Fatalf("GetWorkflow(%q): %v", tt.workflow, err)
			}
			if content == "" {
				t.Fatalf("GetWorkflow(%q) returned empty content", tt.workflow)
			}
		})
	}
}

func TestGetWorkflow_Unknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workflow string
	}{
		{"nonexistent", "nonexistent"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := GetWorkflow(tt.workflow)
			if err == nil {
				t.Fatalf("expected error for unknown workflow %q", tt.workflow)
			}
		})
	}
}
