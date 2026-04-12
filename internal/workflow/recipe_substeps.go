package workflow

import (
	"fmt"
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
