package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// TestFinalizeStepGuidance_IncludesRenderedYaml — v39 Commit 3b,
// F-21 closure. When the agent enters the finalize step, the engine's
// guidance response MUST include the rendered import.yaml for each
// env tier (schema-only, no comments) BEFORE the agent authors
// envComments. v38's "2 GB quota" fabrication across all 6 tiers
// happened because the main agent wrote envComment claims from
// memory without seeing the yaml first; yaml-visibility-before-
// authoring turns "what does this block say" from recall into
// context inspection — the agent cannot invent a number that isn't
// in the visible yaml.
func TestFinalizeStepGuidance_IncludesRenderedYaml(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlanForBrief()
	guide := resolveRecipeGuidance(RecipeStepFinalize, RecipeTierShowcase, plan)

	// Every env's folder label appears in the rendered block.
	for i := 0; i < EnvTierCount(); i++ {
		folder := EnvFolder(i)
		wantMarker := fmt.Sprintf("env %d — `%s/import.yaml`", i, folder)
		if !strings.Contains(guide, wantMarker) {
			t.Errorf("finalize guidance missing yaml preview for env %d (%s)\nexpected marker: %q",
				i, folder, wantMarker)
		}
	}

	// The schema-only preamble prose names the factuality gate.
	if !strings.Contains(guide, "Author envComments against THIS yaml") {
		t.Error("finalize guidance missing factuality prose — the preamble must tell the agent to ground claims in the visible yaml")
	}

	// Rendered yaml carries concrete field values the factuality check
	// walks — spot-check that a known showcase field is present.
	if !strings.Contains(guide, "project:") {
		t.Error("rendered yaml block missing project: header — schema render produced empty yaml")
	}

	// Env 5 always has corePackage: SERIOUS (emitted by writeProjectSection).
	if !strings.Contains(guide, "corePackage: SERIOUS") {
		t.Error("rendered env 5 yaml missing corePackage: SERIOUS — confirms the preview is schema-only but still complete")
	}
}

// TestFinalizeStepGuidance_PreservesEnvCommentsAcrossRender — the
// yaml render intentionally stashes plan.EnvComments to produce a
// schema-only preview. This test pins that the render does NOT
// leave EnvComments cleared afterward (the defer restores the map).
// Regression here would silently wipe agent-authored comments on
// every step-transition after finalize guidance is built.
func TestFinalizeStepGuidance_PreservesEnvCommentsAcrossRender(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlanForBrief()
	plan.EnvComments = map[string]EnvComments{
		"0": {
			Project: "AI-agent tier",
			Service: map[string]string{"appdev": "dev workspace"},
		},
	}
	before := plan.EnvComments

	_ = resolveRecipeGuidance(RecipeStepFinalize, RecipeTierShowcase, plan)

	if plan.EnvComments == nil {
		t.Fatal("finalize render cleared plan.EnvComments — the defer restore failed")
	}
	if len(plan.EnvComments) != len(before) {
		t.Errorf("EnvComments length drift after render: before=%d, after=%d", len(before), len(plan.EnvComments))
	}
	if plan.EnvComments["0"].Project != "AI-agent tier" {
		t.Errorf("EnvComments[0].Project corrupted: got %q", plan.EnvComments["0"].Project)
	}
}

// TestFinalizeStepGuidance_NilPlanNoPanic — defensive. The step may
// be entered in test or error paths with a nil plan; the render
// must return the base guidance without the yaml block, not panic.
func TestFinalizeStepGuidance_NilPlanNoPanic(t *testing.T) {
	t.Parallel()

	got := resolveRecipeGuidance(RecipeStepFinalize, RecipeTierShowcase, nil)
	if got == "" {
		t.Error("finalize guidance empty for nil plan — base guidance should still render")
	}
	if strings.Contains(got, "Pre-loaded input — rendered `import.yaml`") {
		t.Error("finalize guidance injected yaml block despite nil plan — renderFinalizeYAMLInput should return empty")
	}
}
