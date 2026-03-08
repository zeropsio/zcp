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
			"discover_returns_content",
			"discover",
			"Detect",
			true,
		},
		{
			"provision_has_import_yml",
			"provision",
			"import.yml",
			true,
		},
		{
			"provision_has_env_discovery",
			"provision",
			"discovery protocol",
			true,
		},
		{
			"generate_returns_content",
			"generate",
			"zerops.yml",
			true,
		},
		{
			"generate_has_commit_recommendation",
			"generate",
			"committing",
			true,
		},
		{
			"generate_has_noop_start",
			"generate",
			"zsc noop --silent",
			true,
		},
		{
			"deploy_has_agent_prompt",
			"deploy",
			"Service Bootstrap Agent Prompt",
			true,
		},
		{
			"deploy_has_manual_start_cycle",
			"deploy",
			"manual start cycle",
			true,
		},
		{
			"deploy_has_sshfs_note",
			"deploy",
			"already on the dev container",
			true,
		},
		{
			"verify_has_verification",
			"verify",
			"Verification Protocol",
			true,
		},
		{
			"verify_has_next_iteration",
			"verify",
			"next iteration",
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

func TestResolveGuidance_NoDevStartCommand(t *testing.T) {
	t.Parallel()
	guide := ResolveGuidance("generate")
	if strings.Contains(guide, "devStartCommand") {
		t.Error("generate guidance still contains 'devStartCommand' — should use 'zsc noop --silent' instead")
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

// --- Item 28: ResolveProgressiveGuidance ---

func TestResolveProgressiveGuidance_NonDeployStep(t *testing.T) {
	t.Parallel()
	// Non-deploy steps should return same as ResolveGuidance.
	guide := ResolveProgressiveGuidance("discover", nil, 0)
	if guide == "" {
		t.Error("expected non-empty guidance for discover step")
	}
	expected := ResolveGuidance("discover")
	if guide != expected {
		t.Error("non-deploy step should return same as ResolveGuidance")
	}
}

func TestResolveProgressiveGuidance_DeployStandard(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 0)
	if guide == "" {
		t.Error("expected non-empty guidance for deploy step")
	}
	// Standard mode should include deploy-overview and deploy-standard.
	if !strings.Contains(guide, "deploy") {
		t.Error("deploy guidance should contain deploy content")
	}
}

func TestResolveProgressiveGuidance_DeploySimple(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "nginx@1", Simple: true}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 0)
	if guide == "" {
		t.Error("expected non-empty guidance for deploy step in simple mode")
	}
}

func TestResolveProgressiveGuidance_DeployWithRecovery(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 1)
	guideNoRecovery := ResolveProgressiveGuidance("deploy", plan, 0)
	// With failureCount > 0, should include recovery content (if section exists).
	// At minimum, should be >= the no-failure version.
	if len(guide) < len(guideNoRecovery) {
		t.Error("deploy guidance with failures should be >= guidance without")
	}
}

func TestHasNonImplicitWebserverRuntime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want bool
	}{
		{"nil_plan", nil, false},
		{"empty_targets", &ServicePlan{}, false},
		{"implicit_nginx", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "nginx@1"}},
		}}, false},
		{"implicit_phpnginx", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "php-nginx@8"}},
		}}, false},
		{"implicit_static", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "static@1"}},
		}}, false},
		{"implicit_phpapache", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "php-apache@8"}},
		}}, false},
		{"standard_bun", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "bun@1.2"}},
		}}, true},
		{"mixed_one_standard", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{Type: "nginx@1"}},
			{Runtime: RuntimeTarget{Type: "go@1"}},
		}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasNonImplicitWebserverRuntime(tt.plan)
			if got != tt.want {
				t.Errorf("hasNonImplicitWebserverRuntime: want %v, got %v", tt.want, got)
			}
		})
	}
}

// --- Item 29: BuildIterationDelta ---

func TestBuildIterationDelta_ZeroIteration(t *testing.T) {
	t.Parallel()
	result := BuildIterationDelta("deploy", 0, nil, "")
	if result != "" {
		t.Error("expected empty for iteration 0")
	}
}

func TestBuildIterationDelta_NonDeployStep(t *testing.T) {
	t.Parallel()
	result := BuildIterationDelta("verify", 1, nil, "failed")
	if result != "" {
		t.Error("expected empty for non-deploy step")
	}
}

func TestBuildIterationDelta_DeployWithIteration(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "timeout on /status")
	if result == "" {
		t.Fatal("expected non-empty delta for deploy iteration > 0")
	}
	if !strings.Contains(result, "ITERATION 1") {
		t.Error("delta should contain iteration number")
	}
	if !strings.Contains(result, "timeout on /status") {
		t.Error("delta should contain last attestation")
	}
	if !strings.Contains(result, "RECOVERY PATTERNS") {
		t.Error("delta should contain recovery patterns table")
	}
	if !strings.Contains(result, "MAX ITERATIONS REMAINING") {
		t.Error("delta should contain max iterations remaining")
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
