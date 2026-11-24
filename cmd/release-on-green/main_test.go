package main

import "testing"

// witness: cmd/release-on-green/main.go | "internal/tools/", → "internal/toolz/",
func TestShowsAgents_RGAgents_PathDecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		held bool
	}{
		{"internal/content/atoms/deploy-self.md", true},
		{"internal/content/templates/agents_container.md", true},
		{"internal/knowledge/playbooks/stand-up.md", true},
		{"internal/tools/deploy.go", true},
		{"internal/workflow/idle_guidance.go", true},
		{"internal/server/instructions.go", true},
		{"internal/skillpacks/add.go", true},
		{"internal/init/adapters/claude.go", true},
		{"internal/server/server.go", false},
		{"internal/tools/deploy_test.go", false},
		{"internal/knowledge/testdata/schema_versions.json", false},
		{"internal/platform/client.go", false},
		{"internal/contentcheck/x.go", false},
		{".github/workflows/ci.yml", false},
		{"docs/spec-mate.md", false},
	}
	for _, tt := range tests {
		got := len(showsAgents([]string{tt.path})) > 0
		if got != tt.held {
			t.Errorf("%s: held for a person's release = %v, want %v", tt.path, got, tt.held)
		}
	}
}
