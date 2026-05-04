package recipe

import (
	"strings"
	"testing"
)

// TestRefinementRubric_ThresholdSaysCiteCriterionFragmentEdit — run-23
// F-27. Embedded rubric carries the new edit threshold language. The
// old "100%-sure" threshold mapped to default-HOLD on cross-surface
// duplication notices the rubric already named as violations; the new
// language re-anchors the threshold on a citable rubric criterion +
// fragment + preserving edit.
func TestRefinementRubric_ThresholdSaysCiteCriterionFragmentEdit(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/embedded_rubric.md")
	if err != nil {
		t.Fatalf("read embedded rubric: %v", err)
	}
	for _, anchor := range []string{
		"Refinement edit threshold",
		"cite the violated rubric criterion",
		"the exact fragment",
		"preserving edit",
		"Bias toward ACT",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("embedded rubric missing F-27 threshold anchor %q", anchor)
		}
	}
}

// TestPhaseEntryRefinement_NewThresholdLanguage — F-27 second edit
// site. The phase-entry atom carries the same threshold language as
// the rubric so the agent encounters it at brief read-order step 1.
func TestPhaseEntryRefinement_NewThresholdLanguage(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/refinement.md")
	if err != nil {
		t.Fatalf("read phase entry: %v", err)
	}
	for _, anchor := range []string{
		"refinement edit threshold",
		"cite the violated rubric criterion",
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
}
