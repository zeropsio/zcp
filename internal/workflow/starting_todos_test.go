package workflow

import (
	"strings"
	"testing"
)

// TestBuildStartingTodos_IncludesCanonicalSubsteps — v39 Commit 5b.
// Asserts the starter-todos list published on action=start covers every
// step AND every sub-step (where applicable), so the main agent doesn't
// need to re-derive substep sequences after a context compaction.
func TestBuildStartingTodos_IncludesCanonicalSubsteps(t *testing.T) {
	t.Parallel()

	showcase := BuildStartingTodos(RecipeTierShowcase)
	if len(showcase) == 0 {
		t.Fatal("showcase starter-todos empty")
	}

	joined := strings.Join(showcase, "\n")

	// Every recipe step appears at top-level.
	for _, d := range recipeStepDetails {
		if !strings.Contains(joined, "Recipe step: "+d.Name) {
			t.Errorf("showcase starter-todos missing top-level step %q", d.Name)
		}
	}

	// Every generate sub-step appears.
	for _, ss := range generateSubSteps() {
		if !strings.Contains(joined, "substep "+RecipeStepGenerate+"."+ss.Name) {
			t.Errorf("showcase starter-todos missing generate sub-step %q", ss.Name)
		}
	}

	// Every deploy sub-step appears (showcase includes subagent + snapshot
	// + browser-walk).
	for _, wantSubstep := range []string{
		SubStepDeployDev,
		SubStepSubagent,
		SubStepSnapshotDev,
		SubStepFeatureSweepDev,
		SubStepBrowserWalk,
		SubStepCrossDeploy,
		SubStepReadmes,
	} {
		if !strings.Contains(joined, "substep "+RecipeStepDeploy+"."+wantSubstep) {
			t.Errorf("showcase starter-todos missing deploy sub-step %q", wantSubstep)
		}
	}

	// Every close sub-step appears (showcase has editorial + code review +
	// browser walk).
	for _, wantSubstep := range []string{
		SubStepEditorialReview,
		SubStepCloseReview,
		SubStepCloseBrowserWalk,
	} {
		if !strings.Contains(joined, "substep "+RecipeStepClose+"."+wantSubstep) {
			t.Errorf("showcase starter-todos missing close sub-step %q", wantSubstep)
		}
	}
}

// TestBuildStartingTodos_MinimalHasNoShowcaseSubsteps — pins that the
// feature sub-agent / snapshot-dev / browser-walk / close sub-steps do
// NOT appear in the minimal-tier starter list. Wrong-tier sub-steps in
// the todos would cause the minimal-tier agent to block on work that
// never gets dispatched.
func TestBuildStartingTodos_MinimalHasNoShowcaseSubsteps(t *testing.T) {
	t.Parallel()

	minimal := BuildStartingTodos(RecipeTierMinimal)
	joined := strings.Join(minimal, "\n")

	for _, showcaseOnly := range []string{
		"substep " + RecipeStepDeploy + "." + SubStepSubagent,
		"substep " + RecipeStepDeploy + "." + SubStepSnapshotDev,
		"substep " + RecipeStepDeploy + "." + SubStepBrowserWalk,
		"substep " + RecipeStepClose + "." + SubStepEditorialReview,
		"substep " + RecipeStepClose + "." + SubStepCloseReview,
		"substep " + RecipeStepClose + "." + SubStepCloseBrowserWalk,
	} {
		if strings.Contains(joined, showcaseOnly) {
			t.Errorf("minimal starter-todos contains showcase-only sub-step %q", showcaseOnly)
		}
	}

	// Core sub-steps still appear.
	for _, wantSubstep := range []string{
		"substep " + RecipeStepDeploy + "." + SubStepDeployDev,
		"substep " + RecipeStepDeploy + "." + SubStepFeatureSweepDev,
		"substep " + RecipeStepDeploy + "." + SubStepReadmes,
	} {
		if !strings.Contains(joined, wantSubstep) {
			t.Errorf("minimal starter-todos missing core sub-step %q", wantSubstep)
		}
	}
}

// TestBuildWriterBrief_UnderSizeLimit — v39 Commit 5a tripwire. Pins
// the writer dispatch brief below a size ceiling so the slim survives
// future atom edits. The aspirational target from plans/v39-fix-stack.md
// is 25KB (v38 was ~60KB); this bar sits at 65KB to pin the post-slim
// landing (~60KB with headroom). A future dedicated trim round should
// lower this cap as atoms shrink further.
func TestBuildWriterBrief_UnderSizeLimit(t *testing.T) {
	t.Parallel()
	const maxBytes = 65 * 1024 // 65KB — current landing is ~60KB.

	plan := testShowcasePlanForBrief()
	res, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/x.jsonl", "")
	if err != nil {
		t.Fatalf("BuildSubagentBrief: %v", err)
	}
	if len(res.Prompt) > maxBytes {
		t.Errorf("writer brief size = %d bytes (> %d) — a recent atom edit re-inflated the brief. "+
			"v39 Commit 5 dropped 4 atoms (classification-taxonomy, routing-matrix, fact-recording-discipline principle, trimmed content-surface-contracts). "+
			"If the regression is intentional, raise the cap with the reasoning documented; otherwise slim the atom that grew.",
			len(res.Prompt), maxBytes)
	}
	// Aspirational floor — logged but not enforced. The plan targets 25KB;
	// getting there requires trimming citation-map / manifest-contract /
	// self-review-per-surface in a followup commit.
	t.Logf("writer brief size = %d bytes (%.1fKB) — aspirational target 25KB per plans/v39-fix-stack.md",
		len(res.Prompt), float64(len(res.Prompt))/1024)
}
