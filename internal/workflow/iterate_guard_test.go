package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Cx-ITERATE-GUARD regression tests (HANDOFF-to-I6, defect-class-registry
// §16.3 `v35-iterate-fake-pass`). v35: after `action=iterate` the main agent
// walked all 12 deploy substeps in ~84 seconds with zero non-workflow tool
// calls between attestations. The engine accepted every attestation because
// substep completions post-iterate had no evidence-of-work requirement; the
// only safety net was the step-level check at the end.
//
// The guard: on iterate, set state.Recipe.AwaitingEvidenceAfterIterate=true.
// Any subsequent substep complete call returns an error carrying the
// platform.ErrMissingEvidence code until the gate is cleared. The gate is
// cleared by a `zerops_record_fact` call (which is the agent's canonical
// "I learned something" touchpoint during deploy — the facts log is the
// writer-subagent's structured input).

// iterateGuardFixture returns an engine + state on the deploy step with all
// declared deploy substeps marked complete, ready to exercise the
// post-iterate evidence gate. The fixture persists state to disk so the
// Iterate() path's loadState/saveSessionState round-trip is exercised.
func iterateGuardFixture(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)
	if _, err := eng.Start("proj-iterate", WorkflowRecipe, "iterate-guard test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, err := eng.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	rs := NewRecipeState()
	rs.Tier = RecipeTierShowcase
	rs.Plan = &RecipePlan{Slug: "iterate-test", Tier: RecipeTierShowcase, Framework: "nest"}
	for i := range 3 {
		rs.Steps[i].Status = stepComplete
		rs.Steps[i].Attestation = "advanced through to deploy"
	}
	rs.CurrentStep = 3
	rs.Steps[3].Status = stepInProgress
	rs.Steps[3].SubSteps = deploySubSteps(rs.Plan)
	for i := range rs.Steps[3].SubSteps {
		rs.Steps[3].SubSteps[i].Status = stepComplete
		rs.Steps[3].SubSteps[i].Attestation = "pre-iterate complete"
	}
	rs.Steps[3].CurrentSubStep = len(rs.Steps[3].SubSteps)
	state.Recipe = rs
	if err := saveSessionState(dir, eng.sessionID, state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}
	return eng
}

// TestIterateGuard_ResetsSubstepCompletionState verifies the HANDOFF-to-I6
// invariant that iterate walks `recipe.Steps[currentStep].Substeps` and
// flips any `status=complete` markers back to pending. Post-iterate, the
// step cursor lands on the first reset step (generate) and its SubSteps
// field is cleared.
func TestIterateGuard_ResetsSubstepCompletionState(t *testing.T) {
	t.Parallel()
	eng := iterateGuardFixture(t)

	if _, err := eng.Iterate(); err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	state, err := eng.loadState()
	if err != nil {
		t.Fatalf("loadState after iterate: %v", err)
	}
	if state.Iteration != 1 {
		t.Errorf("expected Iteration=1, got %d", state.Iteration)
	}
	// Deploy's SubSteps must be cleared (ResetForIteration replaces the
	// step wholesale); no lingering complete markers from the prior pass.
	for _, ss := range state.Recipe.Steps[3].SubSteps {
		if ss.Status == stepComplete {
			t.Errorf("deploy substep %q still marked complete post-iterate", ss.Name)
		}
	}
	if !state.Recipe.AwaitingEvidenceAfterIterate {
		t.Error("expected AwaitingEvidenceAfterIterate=true post-iterate")
	}
}

// TestIterateGuard_SubstepCompleteAfterIterate_NoEvidence_Rejected is the
// v35 fake-pass scenario in test form: iterate, then immediately call
// complete on a substep with an attestation but no intervening evidence.
// The engine must reject with platform.ErrMissingEvidence.
func TestIterateGuard_SubstepCompleteAfterIterate_NoEvidence_Rejected(t *testing.T) {
	t.Parallel()
	eng := iterateGuardFixture(t)

	if _, err := eng.Iterate(); err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	state, err := eng.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	// Post-iterate CurrentStep is generate (index 2) per ResetForIteration.
	currentName := state.Recipe.CurrentStepName()
	if currentName != RecipeStepGenerate {
		t.Fatalf("expected CurrentStep %q after iterate, got %q", RecipeStepGenerate, currentName)
	}
	_, err = eng.recipeCompleteSubStep(context.Background(), state, RecipeStepGenerate, SubStepScaffold, "attest without work")
	if err == nil {
		t.Fatal("expected substep complete post-iterate to be rejected without evidence; got nil")
	}
	if !strings.Contains(err.Error(), platform.ErrMissingEvidence) {
		t.Errorf("expected error code %q in message; got: %s", platform.ErrMissingEvidence, err.Error())
	}
}

// TestIterateGuard_SubstepCompleteAfterIterate_WithRecordedFact_Passes
// exercises the gate-clear path: iterate, record a fact (the canonical
// "new evidence" touchpoint), then complete — should succeed.
func TestIterateGuard_SubstepCompleteAfterIterate_WithRecordedFact_Passes(t *testing.T) {
	t.Parallel()
	eng := iterateGuardFixture(t)

	if _, err := eng.Iterate(); err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if err := eng.ClearAwaitingEvidenceAfterIterate(); err != nil {
		t.Fatalf("ClearAwaitingEvidenceAfterIterate: %v", err)
	}
	state, err := eng.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.Recipe.AwaitingEvidenceAfterIterate {
		t.Fatal("expected AwaitingEvidenceAfterIterate=false after ClearAwaitingEvidenceAfterIterate")
	}
	if _, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepGenerate, SubStepScaffold, "attest after evidence recorded"); err != nil {
		t.Fatalf("expected substep complete to pass with gate cleared, got: %v", err)
	}
}
