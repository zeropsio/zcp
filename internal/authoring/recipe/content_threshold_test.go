package recipe

import (
	"strings"
	"testing"
)

// TestRefinementSynthesisWorkflow_ThresholdLanguage — run-23 F-27 +
// run-34 Fix A. The threshold language used to live in the embedded
// rubric; after the rubric was retired (run-33 fix #2) and the atom
// deleted (run-34 Fix A), the synthesis_workflow.md atom carries the
// canonical threshold teaching. The language is rule-shaped now (cite
// the violated rule, not the violated rubric criterion) — the
// derived_rules.md substrate is what refinement walks.
func TestRefinementRubric_ThresholdSaysCiteCriterionFragmentEdit(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow: %v", err)
	}
	for _, anchor := range []string{
		"cite the rule id + the exact phrase + the preserving edit",
		"Bias toward ACT",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("synthesis_workflow.md missing threshold anchor %q", anchor)
		}
	}
}

// TestPhaseEntryRefinement_NewThresholdLanguage — F-27 second edit
// site, updated for run-34 Fix A. The phase-entry atom carries the
// rule-walk threshold language (formerly rubric-criterion-based) so
// the agent encounters it at brief read-order step 1.
func TestPhaseEntryRefinement_NewThresholdLanguage(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/refinement.md")
	if err != nil {
		t.Fatalf("read phase entry: %v", err)
	}
	for _, anchor := range []string{
		"refinement edit threshold",
		"cite the violated rule",
		"Bias toward ACT",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("phase_entry/refinement.md missing F-27 anchor %q", anchor)
		}
	}
	// Old "100%-sure" framing must be gone except for the explicit
	// historical reference noting why the framing changed.
	if strings.Contains(body, "100%-sure threshold") {
		t.Error("phase entry still carries the old 100-percent-sure threshold header — F-27 replaces it")
	}
	// Run-34 Fix A — the rubric-criterion framing must be retired.
	// Refinement walks derived_rules.md, not rubric criteria.
	if strings.Contains(body, "the rubric (5 criteria") {
		t.Error("phase entry still teaches rubric-criterion scoring — run-34 Fix A flipped to rule-walk")
	}
	if strings.Contains(body, "Score against each rubric criterion") {
		t.Error("phase entry still teaches per-criterion scoring — run-34 Fix A flipped to rule-walk")
	}
}
