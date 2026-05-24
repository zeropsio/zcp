package content

import (
	"encoding/json"
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
		{"agents_shared.md"},
		{"agents_container.md"},
		{"agents_local.md"},
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

func TestGetTemplate_AgentsSharedContent(t *testing.T) {
	t.Parallel()

	body, err := GetTemplate("agents_shared.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	// Shared body must mention the routing header (the static
	// project-rule layer all envs share).
	if !strings.Contains(body, "Route every user turn") {
		t.Error("agents_shared.md should contain 'Route every user turn' header")
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

// TestMCPServerNameCanonical pins the single canonical MCP server key
// (`zerops`) across every template that writes one. Multi-agent adapter
// work (Codex, Cursor, Gemini) will register the same key in their own
// per-agent config files (~/.codex/config.toml, .cursor/mcp.json, etc.) —
// drift between the template, the permission allowlist, and atom tool
// namespace (`mcp__zerops__zerops_*`) would silently fork the agent's
// tool-call namespace per install.
//
// The corpus (atoms, recipes) hardcodes `mcp__zerops__zerops_*` already;
// this test locks the writer side to match. End-user installs land
// `zerops` everywhere by default — only this dev repo's gitignored
// .mcp.json used to drift historically (operator-edited).
func TestMCPServerNameCanonical(t *testing.T) {
	t.Parallel()

	const canonicalServerKey = "zerops"

	// mcp-config.json: the MCP server registration template.
	mcpConfig, err := GetTemplate("mcp-config.json")
	if err != nil {
		t.Fatalf("GetTemplate(mcp-config.json): %v", err)
	}
	var mcp map[string]any
	if err := json.Unmarshal([]byte(mcpConfig), &mcp); err != nil {
		t.Fatalf("parse mcp-config.json: %v", err)
	}
	servers, ok := mcp["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp-config.json missing mcpServers map; got %T", mcp["mcpServers"])
	}
	if _, ok := servers[canonicalServerKey]; !ok {
		t.Errorf("mcp-config.json mcpServers missing canonical key %q; got keys %v",
			canonicalServerKey, mapKeys(servers))
	}
	for key := range servers {
		if key != canonicalServerKey {
			t.Errorf("mcp-config.json: non-canonical mcpServers key %q (expected only %q)",
				key, canonicalServerKey)
		}
	}

	// settings-local.json: the permission allowlist must reference the
	// canonical tool namespace (mcp__<server>__*).
	settingsLocal, err := GetTemplate("settings-local.json")
	if err != nil {
		t.Fatalf("GetTemplate(settings-local.json): %v", err)
	}
	wantAllowlist := "mcp__" + canonicalServerKey + "__*"
	if !strings.Contains(settingsLocal, wantAllowlist) {
		t.Errorf("settings-local.json missing canonical permission %q", wantAllowlist)
	}

	// Negative assertion: NO template should contain a stale mcp__zcp__ namespace.
	staleNamespace := "mcp__zcp__"
	for _, tmpl := range []string{
		"mcp-config.json",
		"settings-local.json",
		"claude.json",
		"claude-settings.json",
	} {
		body, err := GetTemplate(tmpl)
		if err != nil {
			continue // optional template
		}
		if strings.Contains(body, staleNamespace) {
			t.Errorf("template %s contains stale namespace %q (expected mcp__%s__)",
				tmpl, staleNamespace, canonicalServerKey)
		}
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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
