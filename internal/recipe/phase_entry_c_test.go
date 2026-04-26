// Tests for: Cluster C (run-14 §2.C, R-13-4 / R-13-18) — session-state
// survival across defensive sub-agent re-dispatch. C.1 (start
// attach=true) is deferred per plan §7 open question 2 — Store is
// in-memory only, no on-disk Plan/Fragments persistence exists, and
// landing C.1 would scope-creep beyond the plan's ~50 LoC bound.
//
// C.2 + C.3 land unconditionally:
//
//   - C.2 — buildSubagentPrompt's BriefFeature footer carries the
//     current phase. A defensive re-dispatch sub-agent reads "the
//     session is at phase=feature; do not re-walk research/provision/
//     scaffold" rather than firing 7+ phase-realignment calls.
//   - C.3 — phase_entry/feature.md teaches main "after complete-phase
//     phase=feature returns ok:true, enter-phase phase=finalize, do
//     NOT re-dispatch the feature sub-agent."

package recipe

import (
	"strings"
	"testing"
)

// TestSubagentPrompt_FeatureCarriesCurrentPhase pins Cluster C.2.
// buildSubagentPrompt for BriefFeature includes the resolved current
// phase in the closing footer so a re-dispatched sub-agent reads
// "session at phase=feature; do not re-walk."
func TestSubagentPrompt_FeatureCarriesCurrentPhase(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	prompt, err := buildSubagentPromptForPhase(plan, nil, RecipeInput{BriefKind: "feature"}, PhaseFeature)
	if err != nil {
		t.Fatalf("buildSubagentPromptForPhase: %v", err)
	}
	if !strings.Contains(prompt, "phase=feature") {
		t.Errorf("feature dispatch prompt missing phase=feature hint:\n%s", prompt)
	}
	if !strings.Contains(prompt, "do NOT re-walk") {
		t.Errorf("feature dispatch prompt missing do-not-re-walk teaching")
	}
}

// TestSubagentPrompt_ScaffoldOmitsCurrentPhaseHint pins the scope of
// C.2: scaffold dispatches happen during phase=scaffold and the
// re-dispatch trap doesn't manifest there (sub-agent expected to walk
// up from research). Only feature dispatches carry the hint to
// minimize wrapper bytes per criterion 27.
func TestSubagentPrompt_ScaffoldOmitsCurrentPhaseHint(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	prompt, err := buildSubagentPromptForPhase(plan, nil, RecipeInput{BriefKind: "scaffold", Codebase: "api"}, PhaseScaffold)
	if err != nil {
		t.Fatalf("buildSubagentPromptForPhase: %v", err)
	}
	if strings.Contains(prompt, "do NOT re-walk") {
		t.Errorf("scaffold prompt should not carry the re-walk hint (only feature does)")
	}
}

// TestPhaseEntry_FeatureCarriesAfterCompletePhaseTeaching pins C.3:
// the feature phase-entry atom tells main not to re-dispatch the
// feature sub-agent after complete-phase phase=feature returns
// ok:true. Closes R-13-4's defensive re-dispatch class.
func TestPhaseEntry_FeatureCarriesAfterCompletePhaseTeaching(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFeature)
	if body == "" {
		t.Fatal("feature phase-entry atom unavailable")
	}
	if !strings.Contains(body, "## After complete-phase phase=feature") {
		t.Error("feature phase-entry missing 'After complete-phase phase=feature' section")
	}
	if !strings.Contains(body, "do NOT re-dispatch") {
		t.Error("feature phase-entry missing do-not-re-dispatch teaching")
	}
}
