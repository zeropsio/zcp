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
			"Verification Protocol",
			true,
		},
		{
			"discover_envs_has_env_protocol",
			"discover-envs",
			"discovery protocol",
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
			"plan_returns_content",
			"plan",
			"Identify stack",
			true,
		},
		{
			"load_knowledge_returns_content",
			"load-knowledge",
			"zerops_knowledge",
			true,
		},
		{
			"report_returns_content",
			"report",
			"completion",
			true,
		},
		{
			"generate_code_returns_content",
			"generate-code",
			"zerops.yml",
			true,
		},
		{
			"generate_code_has_commit_recommendation",
			"generate-code",
			"committing",
			true,
		},
		{
			"deploy_has_sshfs_note",
			"deploy",
			"already on the dev container",
			true,
		},
		{
			"import_services_returns_empty",
			"import-services",
			"",
			false,
		},
		{
			"mount_dev_returns_empty",
			"mount-dev",
			"",
			false,
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
	// The deploy section contains the full agent prompt (~170 lines) plus deploy flows.
	// Must be >5000 chars to confirm the agent prompt wasn't truncated.
	if len(guide) < 5000 {
		t.Errorf("deploy guidance too short (%d chars), expected >5000 with full agent prompt", len(guide))
	}
}

func TestResolveGuidance_DeployContainsCodeBlocksWithHashes(t *testing.T) {
	t.Parallel()
	guide := ResolveGuidance("deploy")
	// The agent prompt template contains ## headings inside fenced code blocks.
	// Old heading-based extraction would truncate here. Verify they survive.
	if !strings.Contains(guide, "## Environment") {
		t.Error("deploy guidance missing '## Environment' from agent prompt template")
	}
	if !strings.Contains(guide, "## Tasks") {
		t.Error("deploy guidance missing '## Tasks' from agent prompt template")
	}
}

func TestExtractSection_Basic(t *testing.T) {
	t.Parallel()
	md := `some preamble
<section name="foo">
Content of foo section.
</section>
<section name="bar">
Content of bar section.
</section>`

	tests := []struct {
		name      string
		section   string
		wantSub   string
		wantEmpty bool
	}{
		{"foo_found", "foo", "Content of foo section.", false},
		{"bar_found", "bar", "Content of bar section.", false},
		{"missing_returns_empty", "baz", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractSection(md, tt.section)
			if tt.wantEmpty && got != "" {
				t.Errorf("extractSection(%q) = %q, want empty", tt.section, got)
			}
			if !tt.wantEmpty && !strings.Contains(got, tt.wantSub) {
				t.Errorf("extractSection(%q) missing %q, got %q", tt.section, tt.wantSub, got)
			}
		})
	}
}

func TestExtractSection_WithHashesInCodeBlocks(t *testing.T) {
	t.Parallel()
	md := `preamble
<section name="test">
Some intro text.

` + "````" + `
## This is a heading inside a code block
### Another heading
` + "````" + `

More text after code block.
</section>
trailing`

	got := extractSection(md, "test")
	if !strings.Contains(got, "## This is a heading inside a code block") {
		t.Error("extractSection lost content with # inside code blocks")
	}
	if !strings.Contains(got, "More text after code block.") {
		t.Error("extractSection truncated content after code block")
	}
}
