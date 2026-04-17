package workflow

import (
	"strings"
	"testing"
)

func TestIsContentCheck_Families(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		// v8.78/v8.79 per-host content checks (prefix + suffix shape)
		{"apidev_content_reality", true},
		{"appdev_gotcha_causal_anchor", true},
		{"workerdev_gotcha_distinct_from_guide", true},
		{"apidev_claude_readme_consistency", true},
		{"appdev_scaffold_hygiene", true},
		{"workerdev_service_coverage", true},
		{"apidev_ig_per_item_standalone", true},
		{"workerdev_knowledge_base_authenticity", true},
		// Hostnameless content checks
		{"cross_readme_gotcha_uniqueness", true},
		{"recipe_architecture_narrative", true},
		{"knowledge_base_exceeds_predecessor", true},
		// Not content checks (bootstrap / zerops.yaml / etc.)
		{"zerops_yml_exists", false},
		{"apidev_dev_start_contract", false},
		{"apidev_ports", false},
		{"apidev_run_start", false},
		{"apidev_setup", false},
		{"some_random_check", false},
	}
	for _, tt := range tests {
		if got := isContentCheck(tt.name); got != tt.want {
			t.Errorf("isContentCheck(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestContentFixAttestationRe_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		attestation string
		want        bool
	}{
		// Positive matches
		{"dispatched content-fix sub-agent; rewrote workerdev gotchas", true},
		{"Content-fix subagent handled the 3 failing checks on apidev.", true},
		{"Fix sub-agent dispatched for workerdev README content", true},
		{"dispatched a fix subagent to fix the apidev README gotchas", true},
		{"inline-fix acknowledged — single checkbox flip", true},
		// Negative (v22's actual behavior: "fixed in main, re-ran checks")
		{"Rewrote workerdev/README.md gotchas inline.", false},
		{"Fixed content-check fails by editing README.md directly.", false},
		{"All content checks now pass.", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := contentFixAttestationRe.MatchString(tt.attestation); got != tt.want {
			t.Errorf("contentFixAttestationRe(%q) = %v, want %v", tt.attestation, got, tt.want)
		}
	}
}

func TestContentFixDispatchGate_NoPriorFails_Passes(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{}
	if got := contentFixDispatchGate(rs, RecipeStepDeploy, "random attestation"); got != nil {
		t.Errorf("gate should pass (no prior fails), got %+v", got)
	}
}

func TestContentFixDispatchGate_PriorFailsWithoutDispatch_Fails(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality", "workerdev_gotcha_causal_anchor"},
		},
	}
	result := contentFixDispatchGate(rs, RecipeStepDeploy, "Rewrote content inline in main.")
	if result == nil {
		t.Fatal("gate should fail (prior fails + no dispatch reference)")
	}
	if result.Passed {
		t.Error("result.Passed should be false")
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(result.Checks))
	}
	if result.Checks[0].Name != "content_fix_dispatch_required" {
		t.Errorf("unexpected check name: %s", result.Checks[0].Name)
	}
	for _, needle := range []string{
		"content-quality check(s)",
		"workerdev_content_reality",
		"workerdev_gotcha_causal_anchor",
		"content-fix sub-agent",
		"content-fix-subagent-brief",
	} {
		if !strings.Contains(result.Checks[0].Detail, needle) {
			t.Errorf("detail missing %q: %s", needle, result.Checks[0].Detail)
		}
	}
}

func TestContentFixDispatchGate_PriorFailsWithDispatch_Passes(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality"},
		},
	}
	att := "Dispatched content-fix sub-agent for workerdev README gotchas; sub-agent rewrote them."
	if got := contentFixDispatchGate(rs, RecipeStepDeploy, att); got != nil {
		t.Errorf("gate should pass (dispatch referenced), got %+v", got)
	}
}

func TestContentFixDispatchGate_InlineAcknowledgedDeviation_Passes(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality"},
		},
	}
	att := "inline-fix acknowledged — one-line typo fix, no sub-agent needed."
	if got := contentFixDispatchGate(rs, RecipeStepDeploy, att); got != nil {
		t.Errorf("gate should pass (explicit deviation marker), got %+v", got)
	}
}

func TestContentFixDispatchGate_NonDeployStep_SkipsGate(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepGenerate: {"apidev_content_reality"},
		},
	}
	// Gate only fires on deploy step retries.
	if got := contentFixDispatchGate(rs, RecipeStepGenerate, "fixed inline"); got != nil {
		t.Errorf("gate should not fire on non-deploy step, got %+v", got)
	}
}

func TestRecordContentCheckFails_ExtractsContentFamilyOnly(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{}
	result := &StepCheckResult{
		Passed: false,
		Checks: []StepCheck{
			{Name: "apidev_content_reality", Status: "fail"},
			{Name: "apidev_ports", Status: "fail"}, // not a content check
			{Name: "workerdev_gotcha_causal_anchor", Status: "fail"},
			{Name: "apidev_setup", Status: "pass"}, // pass; ignored
			{Name: "recipe_architecture_narrative", Status: "fail"},
			{Name: "zerops_yml_exists", Status: "fail"}, // not a content check
		},
	}
	recordContentCheckFails(rs, RecipeStepDeploy, result)
	got := rs.PriorStepCheckFails[RecipeStepDeploy]
	want := []string{
		"apidev_content_reality",
		"workerdev_gotcha_causal_anchor",
		"recipe_architecture_narrative",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRecordContentCheckFails_DeduplicatesAcrossCalls(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{}
	result := &StepCheckResult{
		Checks: []StepCheck{
			{Name: "apidev_content_reality", Status: "fail"},
			{Name: "workerdev_gotcha_causal_anchor", Status: "fail"},
		},
	}
	recordContentCheckFails(rs, RecipeStepDeploy, result)
	// Second call with same fails + one new one — duplicates must not accrue.
	result2 := &StepCheckResult{
		Checks: []StepCheck{
			{Name: "apidev_content_reality", Status: "fail"},
			{Name: "workerdev_scaffold_hygiene", Status: "fail"},
		},
	}
	recordContentCheckFails(rs, RecipeStepDeploy, result2)
	got := rs.PriorStepCheckFails[RecipeStepDeploy]
	if len(got) != 3 {
		t.Errorf("expected 3 unique fails, got %d: %v", len(got), got)
	}
}

func TestInt2Str(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{42, "42"},
		{99, "99"},
	}
	for _, tt := range tests {
		if got := int2str(tt.in); got != tt.want {
			t.Errorf("int2str(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
