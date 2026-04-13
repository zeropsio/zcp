package workflow

import (
	"fmt"
	"strings"
	"time"
)

// Sub-step name constants for generate and deploy.
const (
	SubStepScaffold     = "scaffold"
	SubStepZeropsYAML   = "zerops-yaml"
	SubStepAppCode      = "app-code"
	SubStepReadme       = "readme"
	SubStepSmokeTest    = "smoke-test"
	SubStepDeployDev    = "deploy-dev"
	SubStepStartProcs   = "start-processes"
	SubStepVerifyDev    = "verify-dev"
	SubStepInitCommands = "init-commands"
	SubStepSubagent     = "subagent"
	SubStepBrowserWalk  = "browser-walk"
	SubStepCrossDeploy  = "cross-deploy"
	SubStepVerifyStage  = "verify-stage"
)

// initSubSteps returns the sub-step sequence for a step based on plan shape.
// Only generate and deploy have sub-steps; other steps return nil.
func initSubSteps(step string, plan *RecipePlan) []RecipeSubStep {
	switch step {
	case RecipeStepGenerate:
		return generateSubSteps()
	case RecipeStepDeploy:
		return deploySubSteps(plan)
	default:
		return nil
	}
}

func generateSubSteps() []RecipeSubStep {
	names := []string{
		SubStepScaffold,
		SubStepZeropsYAML,
		SubStepAppCode,
		SubStepReadme,
		SubStepSmokeTest,
	}
	steps := make([]RecipeSubStep, len(names))
	for i, n := range names {
		steps[i] = RecipeSubStep{Name: n, Status: stepPending}
	}
	steps[0].Status = stepInProgress
	return steps
}

func deploySubSteps(plan *RecipePlan) []RecipeSubStep {
	names := []string{
		SubStepDeployDev,
		SubStepStartProcs,
		SubStepVerifyDev,
		SubStepInitCommands,
	}
	if isShowcase(plan) {
		names = append(names, SubStepSubagent, SubStepBrowserWalk)
	}
	names = append(names, SubStepCrossDeploy, SubStepVerifyStage)

	steps := make([]RecipeSubStep, len(names))
	for i, n := range names {
		steps[i] = RecipeSubStep{Name: n, Status: stepPending}
	}
	steps[0].Status = stepInProgress
	return steps
}

// completeSubStep marks the current sub-step complete and advances to the next.
// Returns the name of the next sub-step, or "" if all sub-steps are done.
func (r *RecipeStep) completeSubStep(name, attestation string) (string, error) {
	if len(r.SubSteps) == 0 {
		return "", nil
	}
	if r.CurrentSubStep >= len(r.SubSteps) {
		return "", fmt.Errorf("all sub-steps already complete")
	}
	current := r.SubSteps[r.CurrentSubStep]
	if current.Name != name {
		return "", fmt.Errorf("expected sub-step %q (current), got %q", current.Name, name)
	}

	r.SubSteps[r.CurrentSubStep].Status = stepComplete
	r.SubSteps[r.CurrentSubStep].Attestation = attestation
	r.SubSteps[r.CurrentSubStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	r.CurrentSubStep++

	if r.CurrentSubStep < len(r.SubSteps) {
		r.SubSteps[r.CurrentSubStep].Status = stepInProgress
		return r.SubSteps[r.CurrentSubStep].Name, nil
	}
	return "", nil // all sub-steps done
}

// currentSubStepName returns the name of the current sub-step, or "" if none.
func (r *RecipeStep) currentSubStepName() string {
	if len(r.SubSteps) == 0 || r.CurrentSubStep >= len(r.SubSteps) {
		return ""
	}
	return r.SubSteps[r.CurrentSubStep].Name
}

// hasSubSteps reports whether this step has sub-step tracking.
func (r *RecipeStep) hasSubSteps() bool {
	return len(r.SubSteps) > 0
}

// allSubStepsComplete reports whether all sub-steps are done.
func (r *RecipeStep) allSubStepsComplete() bool {
	if len(r.SubSteps) == 0 {
		return true
	}
	return r.CurrentSubStep >= len(r.SubSteps)
}

// enforceSubStepsComplete returns an error when the step has expected
// sub-steps (for the given plan shape) that have not all been completed.
// When expected sub-steps exist but r.SubSteps is empty, the agent never
// called substep complete at all — the failure message names what was
// expected so the agent knows where to start. When sub-steps exist but
// are incomplete, the failure names the pending ones so retries are
// targeted.
//
// This is the backbone of the v13 feature-subagent gate: v11 and v12 both
// shipped scaffold-quality output because step 4b was a bullet in the
// deploy guide, not a precondition for step completion.
func (r *RecipeStep) enforceSubStepsComplete(stepName string, plan *RecipePlan) error {
	expected := initSubSteps(stepName, plan)
	if len(expected) == 0 {
		return nil
	}
	if !r.hasSubSteps() {
		names := make([]string, len(expected))
		for i, e := range expected {
			names[i] = e.Name
		}
		return fmt.Errorf(
			"recipe complete step: %q has %d required sub-steps (%s) — call complete with substep=... for each before completing the full step",
			stepName, len(expected), strings.Join(names, ", "),
		)
	}
	if r.allSubStepsComplete() {
		return nil
	}
	pending := make([]string, 0, len(r.SubSteps))
	for _, ss := range r.SubSteps {
		if ss.Status != stepComplete {
			pending = append(pending, ss.Name)
		}
	}
	return fmt.Errorf(
		"recipe complete step: %q has %d pending sub-step(s): %s — complete each via substep= before completing the full step",
		stepName, len(pending), strings.Join(pending, ", "),
	)
}
