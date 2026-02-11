package content

import (
	"testing"
)

func TestGetWorkflow_AllWorkflows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{"bootstrap"},
		{"deploy"},
		{"debug"},
		{"scale"},
		{"configure"},
		{"monitor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := GetWorkflow(tt.name)
			if err != nil {
				t.Fatalf("GetWorkflow(%q): %v", tt.name, err)
			}
			if content == "" {
				t.Fatalf("GetWorkflow(%q) returned empty content", tt.name)
			}
			if len(content) < 100 {
				t.Errorf("GetWorkflow(%q) content too short: %d chars", tt.name, len(content))
			}
		})
	}
}

func TestGetWorkflow_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetWorkflow("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown workflow")
	}
}

func TestGetTemplate_AllTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{"claude.md"},
		{"mcp-config.json"},
		{"ssh-config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := GetTemplate(tt.name)
			if err != nil {
				t.Fatalf("GetTemplate(%q): %v", tt.name, err)
			}
			if content == "" {
				t.Fatalf("GetTemplate(%q) returned empty content", tt.name)
			}
		})
	}
}

func TestGetTemplate_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetTemplate("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestListWorkflows_Complete(t *testing.T) {
	t.Parallel()

	workflows := ListWorkflows()

	expected := []string{"bootstrap", "configure", "debug", "deploy", "monitor", "scale"}
	if len(workflows) != len(expected) {
		t.Fatalf("expected %d workflows, got %d: %v", len(expected), len(workflows), workflows)
	}
	for i, name := range expected {
		if workflows[i] != name {
			t.Errorf("workflow[%d] = %q, want %q", i, workflows[i], name)
		}
	}
}
