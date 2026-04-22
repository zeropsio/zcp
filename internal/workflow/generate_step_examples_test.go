package workflow

import (
	"strings"
	"testing"
)

// TestGenerateStepGuidance_IncludesExamples — v39 Commit 3a.
// When the agent enters the zerops-yaml substep (where per-codebase
// zerops.yaml comments are authored), the engine substep guidance
// MUST include annotated pass/fail zerops.yaml-comment examples so
// the main agent pattern-matches against concrete shapes before
// writing its own comments. Pattern-match beats prose-rule-parsing
// for content-quality failure modes (field-narration, invented
// mechanism, journal voice).
func TestGenerateStepGuidance_IncludesExamples(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlanForBrief()
	state := &RecipeState{
		Active:      true,
		Tier:        RecipeTierShowcase,
		Plan:        plan,
		CurrentStep: 0,
		Steps:       make([]RecipeStep, len(recipeStepDetails)),
	}
	for i, d := range recipeStepDetails {
		state.Steps[i] = RecipeStep{Name: d.Name, Status: stepPending}
	}

	guide := state.buildSubStepGuide(RecipeStepGenerate, SubStepZeropsYAML, "")
	if guide == "" {
		t.Fatal("buildSubStepGuide returned empty for generate.zerops-yaml")
	}

	// Example block header marks surface injection.
	if !strings.Contains(guide, "annotated zerops-yaml-comment examples") {
		t.Errorf("generate.zerops-yaml guidance missing example-section header\nguide:\n%s", guide)
	}

	// Both verdict tags appear — sampler mixes pass + fail.
	if !strings.Contains(guide, "[FAIL]") {
		t.Error("example block missing [FAIL] samples — sampler didn't produce a fail example")
	}
	if !strings.Contains(guide, "[PASS]") {
		t.Error("example block missing [PASS] samples — sampler didn't produce a pass example")
	}
}

// TestGenerateStepGuidance_ExamplesOnlyOnZeropsYAML — other generate
// substeps (scaffold, app-code, smoke-test) do NOT get example
// injection because they don't author example-bank-covered content.
// Pins the narrow scope of v39 Commit 3a's generate-step injection.
func TestGenerateStepGuidance_ExamplesOnlyOnZeropsYAML(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlanForBrief()
	state := &RecipeState{Active: true, Tier: RecipeTierShowcase, Plan: plan, Steps: []RecipeStep{{Name: RecipeStepGenerate}}}

	for _, otherSubStep := range []string{SubStepScaffold, SubStepAppCode, SubStepSmokeTest} {
		guide := state.buildSubStepGuide(RecipeStepGenerate, otherSubStep, "")
		if strings.Contains(guide, "annotated zerops-yaml-comment examples") {
			t.Errorf("substep %s unexpectedly received zerops-yaml-comment examples — injection should be narrow to SubStepZeropsYAML only",
				otherSubStep)
		}
	}
}
