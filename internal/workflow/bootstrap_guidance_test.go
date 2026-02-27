// Tests for: bootstrap guidance resolver — extracts sections from bootstrap.md per step.
package workflow

import (
	"strings"
	"testing"
)

func TestResolveGuidance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		step         string
		wantContains string
		wantNonEmpty bool
	}{
		{
			"deploy_has_agent_prompt",
			"deploy",
			"Service Bootstrap Agent Prompt",
			true,
		},
		{
			"verify_has_verification",
			"verify",
			"Verification",
			true,
		},
		{
			"discover_envs_has_env_protocol",
			"discover-envs",
			"Env var discovery protocol",
			true,
		},
		{
			"generate_import_has_import_yml",
			"generate-import",
			"import.yml",
			true,
		},
		{
			"detect_returns_content",
			"detect",
			"Detect",
			true,
		},
		{
			"unknown_step_returns_empty",
			"nonexistent-step",
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guide := ResolveGuidance(tt.step)
			if tt.wantNonEmpty && guide == "" {
				t.Errorf("ResolveGuidance(%q) returned empty, want non-empty", tt.step)
			}
			if !tt.wantNonEmpty && guide != "" {
				t.Errorf("ResolveGuidance(%q) returned content, want empty", tt.step)
			}
			if tt.wantContains != "" && !strings.Contains(guide, tt.wantContains) {
				t.Errorf("ResolveGuidance(%q) missing %q (got %d chars)", tt.step, tt.wantContains, len(guide))
			}
		})
	}
}

func TestResolveGuidance_DeployLength(t *testing.T) {
	t.Parallel()
	guide := ResolveGuidance("deploy")
	// The agent prompt section is substantial (>100 lines).
	if len(guide) < 500 {
		t.Errorf("deploy guidance too short (%d chars), expected substantial agent prompt", len(guide))
	}
}
