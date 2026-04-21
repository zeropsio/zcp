package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// closeOrderingFixture returns an engine and a WorkflowState with the
// recipe state advanced to step=close (showcase). Sub-steps for close are
// NOT initialized yet — the engine initializes them on first substep call.
// Caller can pre-populate state.Recipe.Steps[5].SubSteps when a specific
// ordering scenario requires it.
func closeOrderingFixture(t *testing.T) (*Engine, *WorkflowState) {
	t.Helper()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)
	eng.setSessionID("fix-c-test")

	rs := NewRecipeState()
	rs.Tier = RecipeTierShowcase
	rs.Plan = &RecipePlan{Slug: "test-showcase", Tier: RecipeTierShowcase, Framework: "nest"}
	// Advance through research..finalize so close is the current step.
	for i := range 5 {
		rs.Steps[i].Status = stepComplete
		rs.Steps[i].Attestation = "test attestation filler"
	}
	rs.CurrentStep = 5
	rs.Steps[5].Status = stepInProgress

	state := &WorkflowState{
		Version:   "1",
		SessionID: "fix-c-test",
		Workflow:  WorkflowRecipe,
		Recipe:    rs,
	}
	return eng, state
}

// TestCloseSubStepOrder_BrowserWalkBeforeReviewRejected — v8.98 Fix C.
// Attempting close-browser-walk without a prior code-review attestation
// returns an error whose code is SUBAGENT_MISUSE and whose message names
// both sub-steps by their literal names plus the "before" sequencing cue.
func TestCloseSubStepOrder_BrowserWalkBeforeReviewRejected(t *testing.T) {
	t.Parallel()

	eng, state := closeOrderingFixture(t)

	_, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepCloseBrowserWalk, "walk attestation filler content here")
	if err == nil {
		t.Fatal("expected Fix C guard to reject browser-walk-first; got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, platform.ErrSubagentMisuse) {
		t.Errorf("error must carry SUBAGENT_MISUSE code; got: %s", msg)
	}
	if !strings.Contains(msg, SubStepCloseReview) {
		t.Errorf("error must name %q; got: %s", SubStepCloseReview, msg)
	}
	if !strings.Contains(msg, SubStepCloseBrowserWalk) {
		t.Errorf("error must name %q; got: %s", SubStepCloseBrowserWalk, msg)
	}
}

// TestCloseSubStepOrder_ReviewBeforeBrowserWalkAccepted — v8.98 Fix C
// + C-7.5. Canonical ordering (editorial-review → code-review →
// close-browser-walk) passes both guards and all three attestations
// succeed end-to-end.
func TestCloseSubStepOrder_ReviewBeforeBrowserWalkAccepted(t *testing.T) {
	t.Parallel()

	eng, state := closeOrderingFixture(t)

	if _, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepEditorialReview, validEditorialReviewPayload()); err != nil {
		t.Fatalf("editorial-review first call must succeed; got: %v", err)
	}
	if _, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepCloseReview, "review attestation filler content here"); err != nil {
		t.Fatalf("code-review after editorial-review must succeed; got: %v", err)
	}
	if _, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepCloseBrowserWalk, "walk attestation filler content here"); err != nil {
		t.Fatalf("close-browser-walk after code-review must succeed; got: %v", err)
	}
}

// TestCloseSubStepOrder_FixCGuardDoesNotFireOnCodeReview — v8.98 Fix C.
// The Fix C guard is scoped to subStepName == close-browser-walk; any
// code-review attestation (first-time or otherwise) bypasses Fix C
// entirely. C-7.5 Fix D governs editorial-review → code-review ordering
// and does fire on code-review when editorial-review is absent, so the
// fixture first completes editorial-review; the Fix C guard is asserted
// not to fire on the subsequent code-review call.
func TestCloseSubStepOrder_FixCGuardDoesNotFireOnCodeReview(t *testing.T) {
	t.Parallel()

	eng, state := closeOrderingFixture(t)

	if _, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepEditorialReview, validEditorialReviewPayload()); err != nil {
		t.Fatalf("editorial-review setup call must succeed; got: %v", err)
	}
	_, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepCloseReview, "review attestation filler content here")
	if err != nil {
		if strings.Contains(err.Error(), platform.ErrSubagentMisuse) {
			t.Errorf("Fix C guard must not fire on code-review substep; got SUBAGENT_MISUSE error: %s", err.Error())
		}
	}
}

// TestCloseSubStepOrder_BrowserWalkErrorIsActionable — v8.98 Fix C.
// The error message from Fix C's guard contains the substrings the agent
// needs to recover in one step: both sub-step names, the "before"
// sequencing cue, and the "post-fix state" reason phrase.
func TestCloseSubStepOrder_BrowserWalkErrorIsActionable(t *testing.T) {
	t.Parallel()

	eng, state := closeOrderingFixture(t)

	_, err := eng.recipeCompleteSubStep(context.Background(), state, RecipeStepClose, SubStepCloseBrowserWalk, "walk attestation filler content here")
	if err == nil {
		t.Fatal("expected Fix C guard to reject; got nil")
	}
	msg := err.Error()
	for _, want := range []string{"code-review", "close-browser-walk", "before", "post-fix state"} {
		if !strings.Contains(msg, want) {
			t.Errorf("actionable error must contain %q; got: %s", want, msg)
		}
	}
}
