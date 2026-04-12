package workflow

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
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
	rs.Tier = tier
	rs.Steps[0].Status = stepInProgress
	state.Recipe = rs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe start save: %w", err)
	}
	return rs.BuildResponse(state.SessionID, intent, state.Iteration, e.environment, e.knowledge), nil
}

// RecipeComplete completes the current recipe step or sub-step.
// If substep is non-empty, completes that sub-step within the current step.
// If checker is non-nil and fails, the step stays and the agent receives failure details.
func (e *Engine) RecipeComplete(ctx context.Context, step, attestation string, checker RecipeStepChecker, substep ...string) (*RecipeResponse, error) {
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

	// Sub-step completion: if substep is provided, complete that sub-step
	// within the current step rather than the full step.
	subStepName := ""
	if len(substep) > 0 {
		subStepName = substep[0]
	}
	if subStepName != "" {
		return e.recipeCompleteSubStep(ctx, state, step, subStepName, attestation)
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

	// Auto-generate finalize files when deploy completes (entering finalize step).
	// Files are written to outputDir so the agent only needs to review and reconcile.
	if step == RecipeStepDeploy && state.Recipe.CurrentStepName() == RecipeStepFinalize {
		e.autoGenerateFinalizeFiles(state)
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
		e.clearSessionID()
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

// recipeCompleteSubStep completes a sub-step within the current step, validates
// the agent's output, and returns guidance for the next sub-step.
func (e *Engine) recipeCompleteSubStep(ctx context.Context, state *WorkflowState, step, subStepName, attestation string) (*RecipeResponse, error) {
	rs := state.Recipe
	if rs.CurrentStep >= len(rs.Steps) {
		return nil, fmt.Errorf("recipe substep complete: all steps done")
	}
	currentStep := &rs.Steps[rs.CurrentStep]
	if currentStep.Name != step {
		return nil, fmt.Errorf("recipe substep complete: expected step %q, got %q", currentStep.Name, step)
	}

	// Initialize sub-steps on first sub-step completion if not already set.
	if !currentStep.hasSubSteps() {
		currentStep.SubSteps = initSubSteps(step, rs.Plan)
		currentStep.CurrentSubStep = 0
	}

	// Run sub-step validator if available.
	if validator := getSubStepValidator(subStepName); validator != nil {
		result := validator(ctx, rs.Plan, rs)
		if result != nil && !result.Passed {
			// Record failure pattern for Phase C adaptive retry.
			rs.FailurePatterns = append(rs.FailurePatterns, FailurePattern{
				SubStep:   subStepName,
				Issues:    result.Issues,
				Iteration: state.Iteration,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
				return nil, fmt.Errorf("recipe substep save: %w", err)
			}
			resp := rs.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
			resp.CheckResult = &StepCheckResult{
				Passed:  false,
				Summary: fmt.Sprintf("Sub-step %q validation failed: %s", subStepName, result.Guidance),
			}
			resp.Message = fmt.Sprintf("Sub-step %q: %d issues — fix and retry", subStepName, len(result.Issues))
			return resp, nil
		}
	}

	// Complete the sub-step.
	nextSubStep, err := currentStep.completeSubStep(subStepName, attestation)
	if err != nil {
		return nil, fmt.Errorf("recipe substep complete: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("recipe substep save: %w", err)
	}

	resp := rs.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	if nextSubStep != "" {
		resp.Message = fmt.Sprintf("Sub-step %q complete. Next: %s", subStepName, nextSubStep)
	} else {
		resp.Message = fmt.Sprintf("All sub-steps for %q complete. Call complete with step=%q to finish.", step, step)
	}
	return resp, nil
}

// RecipeCompletePlan validates a structured recipe plan, completes the research step,
// and stores it in state. Prefers schema enums for validation; falls back to liveTypes.
func (e *Engine) RecipeCompletePlan(plan RecipePlan, attestation string, liveTypes []platform.ServiceStackType, schemas *schema.Schemas) (*RecipeResponse, error) {
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
	if errs := ValidateRecipePlan(plan, liveTypes, schemas); len(errs) > 0 {
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
		e.clearSessionID()
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

// RecipeSession returns the current recipe state, or nil if no active session.
func (e *Engine) RecipeSession() *RecipeState {
	state, err := e.loadState()
	if err != nil || state.Recipe == nil {
		return nil
	}
	return state.Recipe
}

// RecordGuidanceAccess logs a topic fetch for Phase C adaptive delivery.
// Best-effort — errors are silently ignored.
func (e *Engine) RecordGuidanceAccess(topicID, step string) {
	state, err := e.loadState()
	if err != nil || state.Recipe == nil {
		return
	}
	state.Recipe.GuidanceAccess = append(state.Recipe.GuidanceAccess, GuidanceAccessEntry{
		TopicID:   topicID,
		Step:      step,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	_ = saveSessionState(e.stateDir, e.sessionID, state)
}

// envVarNameRegexp validates env var names. Matches the standard
// [a-zA-Z_][a-zA-Z0-9_]* rule from POSIX / env-variables knowledge base.
var envVarNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// UpdateRecipeProjectEnvVariables merges agent-authored per-env project env
// vars into the recipe plan and persists the session. Merge semantics, per env:
//
//   - Passing a non-empty map for an env REPLACES that env's prior map
//     (deterministic: the agent owns the full per-env shape). This differs
//     from EnvComments.Service which is additive per key — project env var
//     declarations are intentionally atomic per env.
//   - Passing an empty map for an env CLEARS that env's entry entirely.
//   - Omitting an env key leaves that env untouched, so the agent can refine
//     one env at a time without restating the others.
//   - Nil input is a no-op (matches UpdateRecipeComments).
//
// Validation:
//   - Env keys must be "0".."5".
//   - Var names must match [A-Za-z_][A-Za-z0-9_]* (POSIX env var names).
//   - Empty var names are rejected.
//
// Values are not validated beyond string typing. Values often contain
// ${zeropsSubdomainHost} which is resolved by the platform preprocessor
// at project import time, not by this function.
func (e *Engine) UpdateRecipeProjectEnvVariables(projectEnvVariables map[string]map[string]string) error {
	if projectEnvVariables == nil {
		return nil
	}
	// Validate up front so we never partially-persist invalid input.
	for envKey, vars := range projectEnvVariables {
		if !isValidEnvKey(envKey) {
			return fmt.Errorf("update recipe project env variables: invalid env key %q (must be \"0\"..\"5\")", envKey)
		}
		for name := range vars {
			if name == "" {
				return fmt.Errorf("update recipe project env variables: env %q has empty variable name", envKey)
			}
			if !envVarNameRegexp.MatchString(name) {
				return fmt.Errorf("update recipe project env variables: env %q variable name %q must match [A-Za-z_][A-Za-z0-9_]*", envKey, name)
			}
		}
	}

	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("update recipe project env variables load: %w", err)
	}
	if state.Recipe == nil || state.Recipe.Plan == nil {
		return fmt.Errorf("update recipe project env variables: no active recipe plan")
	}
	plan := state.Recipe.Plan
	if plan.ProjectEnvVariables == nil {
		plan.ProjectEnvVariables = map[string]map[string]string{}
	}
	for envKey, vars := range projectEnvVariables {
		if len(vars) == 0 {
			delete(plan.ProjectEnvVariables, envKey)
			continue
		}
		// Atomic replace — copy the map so callers can't mutate behind us.
		cp := make(map[string]string, len(vars))
		maps.Copy(cp, vars)
		plan.ProjectEnvVariables[envKey] = cp
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
}

// isValidEnvKey returns true for the 6 known env tier indices as strings.
func isValidEnvKey(k string) bool {
	switch k {
	case "0", "1", "2", "3", "4", "5":
		return true
	}
	return false
}

// UpdateRecipeComments merges agent-authored per-env comments into the recipe
// plan and persists the session. Merge semantics, per env:
//   - Service entries with a non-empty value overwrite prior entries for that key.
//   - Service entries with an empty-string value delete the prior entry.
//   - A non-empty Project value overwrites; an empty Project value leaves the
//     prior value untouched (pass a single space to clear if ever needed).
//
// Envs not present in the input map are left untouched, so the agent can
// refine one env at a time without restating the others.
func (e *Engine) UpdateRecipeComments(envComments map[string]EnvComments) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("update recipe comments load: %w", err)
	}
	if state.Recipe == nil || state.Recipe.Plan == nil {
		return fmt.Errorf("update recipe comments: no active recipe plan")
	}
	plan := state.Recipe.Plan
	if envComments != nil {
		if plan.EnvComments == nil {
			plan.EnvComments = map[string]EnvComments{}
		}
		for envKey, incoming := range envComments {
			existing := plan.EnvComments[envKey]
			if incoming.Service != nil {
				if existing.Service == nil {
					existing.Service = map[string]string{}
				}
				for svcKey, text := range incoming.Service {
					if text == "" {
						delete(existing.Service, svcKey)
					} else {
						existing.Service[svcKey] = text
					}
				}
			}
			if incoming.Project != "" {
				existing.Project = incoming.Project
			}
			plan.EnvComments[envKey] = existing
		}
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
}

// autoGenerateFinalizeFiles writes all template-generated recipe files to outputDir.
// Called automatically when deploy completes and finalize step begins.
// Best-effort — errors logged to stderr, never block step transition.
func (e *Engine) autoGenerateFinalizeFiles(state *WorkflowState) {
	if state.Recipe == nil || state.Recipe.Plan == nil || state.Recipe.OutputDir == "" {
		return
	}
	files := BuildFinalizeOutput(state.Recipe.Plan)
	if overlaid := OverlayRealREADMEs(files, state.Recipe.Plan); overlaid > 0 {
		fmt.Fprintf(os.Stderr, "zcp: overlaid %d README(s) from mount (agent's real READMEs)\n", overlaid)
	}
	var count int
	for relPath, content := range files {
		fullPath := filepath.Join(state.Recipe.OutputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: auto-generate mkdir %s: %v\n", filepath.Dir(fullPath), err)
			continue
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: auto-generate write %s: %v\n", fullPath, err)
			continue
		}
		count++
	}
	if count > 0 {
		fmt.Fprintf(os.Stderr, "zcp: auto-generated %d recipe files in %s\n", count, state.Recipe.OutputDir)
	}
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
