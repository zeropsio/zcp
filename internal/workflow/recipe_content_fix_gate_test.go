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
		{"apidev_claude_md_no_burn_trap_folk", true},
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

func TestBriefBugAckRe_RecognizesOperatorAck(t *testing.T) {
	t.Parallel()
	// v8.86 §3.3 — the gate no longer accepts "dispatched content-fix
	// subagent" style attestations (that escape hatch is removed). Only
	// the explicit writer-brief-bug acknowledgment passes, for operator-
	// controlled retry while the brief is being patched.
	positive := []string{
		"writer-brief-bug acknowledged — brief patched in-flight",
		"Writer brief bug acknowledged. Re-ran end-to-end after patch.",
		"writer brief-bug acknowledged",
	}
	for _, att := range positive {
		if !briefBugAckRe.MatchString(att) {
			t.Errorf("expected match on %q", att)
		}
	}
	negative := []string{
		// v8.81's "content-fix subagent" phrasings MUST NOT pass anymore.
		"dispatched content-fix sub-agent; rewrote workerdev gotchas",
		"Content-fix subagent handled the 3 failing checks.",
		"dispatched a fix subagent to fix the apidev README",
		"inline-fix acknowledged — single checkbox flip",
		"All content checks now pass.",
		"",
	}
	for _, att := range negative {
		if briefBugAckRe.MatchString(att) {
			t.Errorf("attestation must not pass gate: %q", att)
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

func TestContentFixDispatchGate_PriorFails_FailsAsBriefBug(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality", "workerdev_gotcha_causal_anchor"},
		},
	}
	result := contentFixDispatchGate(rs, RecipeStepDeploy, "Rewrote content inline in main.")
	if result == nil {
		t.Fatal("gate should fail (prior fails)")
	}
	if result.Passed {
		t.Error("result.Passed should be false")
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(result.Checks))
	}
	if result.Checks[0].Name != "writer_brief_bug" {
		t.Errorf("unexpected check name: %s", result.Checks[0].Name)
	}
	for _, needle := range []string{
		"WRITER BRIEF BUG",
		"workerdev_content_reality",
		"workerdev_gotcha_causal_anchor",
		"self-verify",
		"DO NOT dispatch",
	} {
		if !strings.Contains(result.Checks[0].Detail, needle) {
			t.Errorf("detail missing %q: %s", needle, result.Checks[0].Detail)
		}
	}
	// Critical: the new detail MUST NOT offer a dispatch-fix escape hatch.
	if strings.Contains(result.Checks[0].Detail, "content-fix-subagent-brief") {
		t.Error("v8.86 gate must not reference content-fix-subagent-brief topic")
	}
}

func TestContentFixDispatchGate_OldDispatchAttestation_StillFails(t *testing.T) {
	t.Parallel()
	// v8.81 attestations are no longer accepted; old callers must now
	// either fix the writer brief or acknowledge the brief bug.
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality"},
		},
	}
	att := "Dispatched content-fix sub-agent for workerdev README gotchas; sub-agent rewrote them."
	if got := contentFixDispatchGate(rs, RecipeStepDeploy, att); got == nil {
		t.Error("gate must not accept v8.81 dispatch-fix attestations")
	}
}

func TestContentFixDispatchGate_WriterBriefBugAck_Passes(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepDeploy: {"workerdev_content_reality"},
		},
	}
	att := "writer-brief-bug acknowledged — patched brief in-flight; re-ran end-to-end."
	if got := contentFixDispatchGate(rs, RecipeStepDeploy, att); got != nil {
		t.Errorf("gate should pass (explicit brief-bug ack), got %+v", got)
	}
}

func TestContentFixDispatchGate_NonDeployStep_SkipsGate(t *testing.T) {
	t.Parallel()
	rs := &RecipeState{
		PriorStepCheckFails: map[string][]string{
			RecipeStepGenerate: {"apidev_content_reality"},
		},
	}
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
