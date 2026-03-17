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
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "nginx@1", BootstrapMode: "simple"}},
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

func TestDistinctModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want map[string]bool
	}{
		{"nil_plan", nil, nil},
		{"empty_targets", &ServicePlan{}, map[string]bool{}},
		{"default_mode_is_standard", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
		}}, map[string]bool{"standard": true}},
		{"explicit_dev", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "app", Type: "bun@1.2", BootstrapMode: "dev"}},
		}}, map[string]bool{"dev": true}},
		{"explicit_simple", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "app", Type: "nginx@1", BootstrapMode: "simple"}},
		}}, map[string]bool{"simple": true}},
		{"mixed_standard_and_simple", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
			{Runtime: RuntimeTarget{DevHostname: "frontend", Type: "nginx@1", BootstrapMode: "simple"}},
		}}, map[string]bool{"standard": true, "simple": true}},
		{"mixed_all_three", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
			{Runtime: RuntimeTarget{DevHostname: "worker", Type: "bun@1.2", BootstrapMode: "dev"}},
			{Runtime: RuntimeTarget{DevHostname: "static", Type: "nginx@1", BootstrapMode: "simple"}},
		}}, map[string]bool{"standard": true, "dev": true, "simple": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := distinctModes(tt.plan)
			if tt.want == nil {
				if got != nil {
					t.Errorf("distinctModes: want nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("distinctModes: want %v, got %v", tt.want, got)
				return
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("distinctModes: missing mode %q in %v", k, got)
				}
			}
		})
	}
}

func TestResolveProgressiveGuidance_DevMode(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "bun@1.2", BootstrapMode: "dev"}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 0)
	if guide == "" {
		t.Fatal("expected non-empty guidance for deploy step in dev mode")
	}
	// Should contain dev-only specific content.
	if !strings.Contains(guide, "Dev-only mode") {
		t.Error("dev mode guidance should contain 'Dev-only mode' from deploy-dev section")
	}
	// Should NOT include deploy-standard section content.
	if strings.Contains(guide, "Standard mode (dev+stage)") {
		t.Error("dev mode guidance should not contain deploy-standard section")
	}
}

func TestResolveProgressiveGuidance_DevMode_HasDeployDevContent(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "bun@1.2", BootstrapMode: "dev"}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 0)
	if guide == "" {
		t.Fatal("expected non-empty guidance for deploy step in dev mode")
	}
	// deploy-dev section must contain actionable content.
	if !strings.Contains(guide, "no stage pair") {
		t.Error("deploy-dev section should mention 'no stage pair'")
	}
	if !strings.Contains(guide, "zerops_deploy") {
		t.Error("deploy-dev section should reference zerops_deploy")
	}
}

func TestResolveProgressiveGuidance_MixedStandardDev(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
		{Runtime: RuntimeTarget{DevHostname: "worker", Type: "bun@1.2", BootstrapMode: "dev"}},
	}}
	guide := ResolveProgressiveGuidance("deploy", plan, 0)
	if guide == "" {
		t.Fatal("expected non-empty guidance for mixed mode deploy")
	}
	// Both standard and dev sections should be present.
	standardOnly := ResolveProgressiveGuidance("deploy", &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}, 0)
	if len(guide) <= len(standardOnly) {
		t.Error("mixed mode guidance should be longer than standard-only guidance")
	}
	// deploy-iteration heading should appear exactly once (no duplication).
	iterCount := strings.Count(guide, "### Dev iteration: manual start cycle")
	if iterCount != 1 {
		t.Errorf("deploy-iteration section should appear exactly once, got %d", iterCount)
	}
}

func TestBuildIterationDelta_RemainingUsesMaxIterations(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "some failure")
	// maxIterations() defaults to 10, so remaining should be 9 at iteration 1.
	if !strings.Contains(result, "9") {
		t.Errorf("expected remaining=9 (maxIterations()-1), got: %s", result)
	}
	// Must NOT show 4 (which would be old maxBootstrapIterations=5 minus 1).
	if strings.Contains(result, "REMAINING: 4") {
		t.Error("remaining should use maxIterations() (default 10), not old constant 5")
	}
}

func TestBuildIterationDelta_NoForceGuide(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "some failure")
	if strings.Contains(result, "forceGuide") {
		t.Error("output should not contain 'forceGuide'")
	}
	if !strings.Contains(result, `action="iterate"`) {
		t.Errorf("output should contain action=\"iterate\", got: %s", result)
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
