package content

import (
	"strings"
	"testing"
)

// Recipe is the only workflow still backed by a static workflows/*.md file —
// bootstrap, develop, cicd, and export are synthesized at runtime from the
// atom corpus. Recipe stays in the workflow file because its AUTHORING
// pipeline (recipe_guidance.go, recipe_section_parser.go, recipe_topic_
// registry.go) consumes it through an independent section parser.
func TestGetWorkflow_AllWorkflows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{"recipe"},
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

// TestGetWorkflow_OrchestratedNotStatic asserts that no workflow except
// recipe is loadable through GetWorkflow. Bootstrap, develop, and export are
// atom-synthesized; re-adding a workflows/*.md for any of them would let the
// static file drift from the atom corpus. cicd is retired — its setup flow
// now lives in action=strategy (strategy-setup phase atoms).
func TestGetWorkflow_OrchestratedNotStatic(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"bootstrap", "develop", "export"} {
		if _, err := GetWorkflow(name); err == nil {
			t.Errorf("GetWorkflow(%q) should fail — atom-synthesized workflows must not have a static .md file", name)
		}
	}
}

func TestGetTemplate_AllTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{"claude_shared.md"},
		{"claude_container.md"},
		{"claude_local.md"},
		{"mcp-config.json"},
		{"ssh-config"},
		{"settings-local.json"},
		{"vscode-settings.json"},
		{"vscode-bootstrap-package.json"},
		{"vscode-bootstrap-extension.js"},
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

func TestGetTemplate_ClaudeSharedContent(t *testing.T) {
	t.Parallel()

	body, err := GetTemplate("claude_shared.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	// Shared body must mention the routing header (the static
	// project-rule layer all envs share).
	if !strings.Contains(body, "Route every user turn") {
		t.Error("claude_shared.md should contain 'Route every user turn' header")
	}
}

func TestGetTemplate_SettingsLocalJSON(t *testing.T) {
	t.Parallel()

	content, err := GetTemplate("settings-local.json")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	required := []string{
		"permissions",
		"mcp__zerops__*",
	}

	for _, keyword := range required {
		if !strings.Contains(content, keyword) {
			t.Errorf("settings-local.json template should contain %q", keyword)
		}
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

	expected := []string{"recipe"}
	if len(workflows) != len(expected) {
		t.Fatalf("expected %d workflows, got %d: %v", len(expected), len(workflows), workflows)
	}
	for i, name := range expected {
		if workflows[i] != name {
			t.Errorf("workflow[%d] = %q, want %q", i, workflows[i], name)
		}
	}
}
