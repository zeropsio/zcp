package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// RecipeStart creates a new session with recipe state and returns the first step.
func (e *Engine) RecipeStart(projectID, intent, tier string) (*RecipeResponse, error) {
	if tier != RecipeTierMinimal && tier != RecipeTierShowcase {
		return nil, fmt.Errorf("recipe start: invalid tier %q (must be %q or %q)", tier, RecipeTierMinimal, RecipeTierShowcase)
	}

	state, err := e.Start(projectID, WorkflowRecipe, intent)
	if err != nil {
		return nil, fmt.Errorf("recipe start: %w", err)
	}

	rs := NewRecipeState()
	rs.Steps[0].Status = stepInProgress
	state.Recipe = rs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe start save: %w", err)
	}
	return rs.BuildResponse(state.SessionID, intent, state.Iteration, e.environment, e.knowledge), nil
}

// RecipeComplete completes the current recipe step.
// If checker is non-nil and fails, the step stays and the agent receives failure details.
func (e *Engine) RecipeComplete(ctx context.Context, step, attestation string, checker RecipeStepChecker) (*RecipeResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("recipe complete: %w", err)
	}
	if state.Recipe == nil || !state.Recipe.Active {
		return nil, fmt.Errorf("recipe complete: not active")
	}

	// Non-research steps require a plan from the research step.
	if step != RecipeStepResearch && state.Recipe.Plan == nil {
		return nil, fmt.Errorf("recipe complete: step %q requires plan from research step", step)
	}

	var checkResult *StepCheckResult
	if checker != nil {
		result, checkErr := checker(ctx, state.Recipe.Plan, state.Recipe)
		if checkErr != nil {
			return nil, fmt.Errorf("recipe step check: %w", checkErr)
		}
		if result != nil && !result.Passed {
			resp := state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
			resp.CheckResult = result
			resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", step, result.Summary)
			return resp, nil
		}
		checkResult = result
	}

	if err := state.Recipe.CompleteStep(step, attestation); err != nil {
		return nil, fmt.Errorf("recipe complete: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	var cleanupErr error
	if !state.Recipe.Active {
		e.writeRecipeOutputs(state)
		cleanupErr = ResetSessionByID(e.stateDir, state.SessionID)
		if cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed recipe session: %v\n", cleanupErr)
		}
		e.completedState = state
		e.sessionID = ""
	} else if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe complete save: %w", err)
	}

	resp := state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	resp.CheckResult = checkResult
	if cleanupErr != nil {
		resp.Message += "\n\nWarning: session cleanup failed: " + cleanupErr.Error()
	}

	// Append transition message with publish commands when recipe is done.
	if !state.Recipe.Active && state.Recipe.Plan != nil {
		resp.Message += buildRecipeTransition(state.Recipe.Plan)
	}

	return resp, nil
}

// RecipeCompletePlan validates a structured recipe plan, completes the research step,
// and stores it in state. liveTypes may be nil (validation skips type checks).
func (e *Engine) RecipeCompletePlan(plan RecipePlan, attestation string, liveTypes []platform.ServiceStackType) (*RecipeResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("recipe complete plan: %w", err)
	}
	if state.Recipe == nil || !state.Recipe.Active {
		return nil, fmt.Errorf("recipe complete plan: not active")
	}
	if state.Recipe.CurrentStepName() != RecipeStepResearch {
		return nil, fmt.Errorf("recipe complete plan: current step is %q, not %q", state.Recipe.CurrentStepName(), RecipeStepResearch)
	}

	// Validate the plan.
	if errs := ValidateRecipePlan(plan, liveTypes); len(errs) > 0 {
		return nil, fmt.Errorf("recipe complete plan: validation failed: %s", strings.Join(errs, "; "))
	}

	if err := state.Recipe.CompleteStep(RecipeStepResearch, attestation); err != nil {
		return nil, fmt.Errorf("recipe complete plan: %w", err)
	}

	plan.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	state.Recipe.Plan = &plan

	// Set output directory: {projectRoot}/zcprecipator/{slug}/ (e.g., /var/www/zcprecipator/laravel-hello-world/).
	projectRoot := filepath.Dir(filepath.Dir(e.stateDir))
	state.Recipe.OutputDir = filepath.Join(projectRoot, "zcprecipator", plan.Slug)

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe complete plan save: %w", err)
	}

	return state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// RecipeSkip skips the current recipe step (only close is skippable).
func (e *Engine) RecipeSkip(step, reason string) (*RecipeResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("recipe skip: %w", err)
	}
	if state.Recipe == nil || !state.Recipe.Active {
		return nil, fmt.Errorf("recipe skip: not active")
	}

	if err := state.Recipe.SkipStep(step, reason); err != nil {
		return nil, fmt.Errorf("recipe skip: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	var cleanupErr error
	if !state.Recipe.Active {
		// Close was skipped — still write meta and cleanup session.
		e.writeRecipeOutputs(state)
		cleanupErr = ResetSessionByID(e.stateDir, state.SessionID)
		if cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup skipped recipe session: %v\n", cleanupErr)
		}
		e.completedState = state
		e.sessionID = ""
	} else if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe skip save: %w", err)
	}

	resp := state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	if cleanupErr != nil {
		resp.Message += "\n\nWarning: session cleanup failed: " + cleanupErr.Error()
	}
	return resp, nil
}

// RecipeStatus returns the current recipe progress with fresh guidance.
func (e *Engine) RecipeStatus() (*RecipeResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("recipe status: %w", err)
	}
	if state.Recipe == nil {
		return nil, fmt.Errorf("recipe status: no recipe state")
	}
	return state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// writeRecipeOutputs writes the RecipeMeta file for the completed recipe.
// Best-effort — errors logged to stderr.
func (e *Engine) writeRecipeOutputs(state *WorkflowState) {
	if state.Recipe == nil || state.Recipe.Plan == nil {
		return
	}
	plan := state.Recipe.Plan
	meta := &RecipeMeta{
		Slug:        plan.Slug,
		Framework:   plan.Framework,
		Tier:        plan.Tier,
		RuntimeType: plan.RuntimeType,
		CreatedAt:   time.Now().UTC().Format("2006-01-02"),
		OutputDir:   state.Recipe.OutputDir,
	}
	if err := WriteRecipeMeta(e.stateDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: write recipe meta: %v\n", err)
	}
}

// buildRecipeTransition returns the post-completion transition message with
// publish commands, test instructions, and eval launch info.
func buildRecipeTransition(plan *RecipePlan) string {
	return fmt.Sprintf(`

## Recipe Complete: %s

### Publish
1. Push to GitHub:
   `+"`"+`zcp sync push recipes %s`+"`"+`
2. After merge, clear Strapi cache:
   `+"`"+`zcp sync cache-clear %s`+"`"+`
3. Pull merged version:
   `+"`"+`zcp sync pull recipes %s`+"`"+`

### Test
Run through eval to verify quality:
`+"`"+`zcp eval run --recipe %s`+"`"+`
`, plan.Slug, plan.Slug, plan.Slug, plan.Slug, plan.Slug)
}
