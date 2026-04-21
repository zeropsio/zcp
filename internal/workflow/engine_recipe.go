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
	resp := rs.BuildResponse(state.SessionID, intent, state.Iteration, e.environment, e.knowledge)
	// Cx-GUIDANCE-TOPIC-REGISTRY (v35 F-5 close): hand the main agent
	// the closed universe of valid zerops_guidance topic IDs at start
	// so it references the registry instead of pattern-matching from
	// its own reasoning.
	resp.GuidanceTopicIDs = AllTopicIDs()
	return resp, nil
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

	// Cx-CLOSE-STEP-STAGING: before marking close complete, stage the
	// writer sub-agent's per-codebase README.md + CLAUDE.md from the
	// source mount into the recipe output tree so they reach the
	// deliverable tarball. Without this, sessionless export via
	// `git ls-files` strips uncommitted writer output and the
	// deliverable ships without per-codebase markdown — v36 F-10.
	// Staging failure blocks close completion and names the missing
	// file so the agent can dispatch the writer sub-agent and retry.
	if step == RecipeStepClose {
		if stageErr := e.stageWriterContent(state); stageErr != nil {
			resp := state.Recipe.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
			resp.CheckResult = &StepCheckResult{
				Passed:  false,
				Summary: stageErr.Error(),
			}
			resp.Message = fmt.Sprintf("Step %q: writer content staging failed — %v", step, stageErr)
			return resp, nil
		}
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
	// Close-completion also surfaces publish CLI guidance in structured fields
	// so the agent can render it to the user without inferring it from
	// Message prose. Publish is NOT a workflow state — it's a post-workflow
	// CLI operation the agent relays only when the user explicitly asks.
	if !state.Recipe.Active && state.Recipe.Plan != nil {
		resp.Message += buildRecipeTransition(state.Recipe.Plan)
		if step == RecipeStepClose {
			resp.PostCompletionSummary, resp.PostCompletionNextSteps = buildClosePostCompletion(state.Recipe.Plan, state.Recipe.OutputDir)
		}
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
	// Cx-ITERATE-GUARD (v35 F-3 close): after action=iterate, each substep
	// complete must be backed by new evidence of work. The canonical gate-
	// clear is a zerops_record_fact call (the agent's "I learned something"
	// touchpoint during deploy). Without this guard, v35 let the agent
	// walk all 12 deploy substeps in 84s with zero intervening tool calls.
	if rs.AwaitingEvidenceAfterIterate {
		return nil, fmt.Errorf(
			"%s: substep %q refused post-iterate — this step was reset by action=iterate and the engine requires new evidence of work before accepting attestations; record at least one fact with zerops_record_fact (the writer subagent's facts-log input; each fact = one observed platform behavior, fix applied, or cross-codebase contract) between the iterate and the next substep complete — re-attesting substeps without fresh evidence is the v35 fake-pass pattern the guard exists to catch",
			platform.ErrMissingEvidence, subStepName,
		)
	}

	// Initialize sub-steps on first sub-step completion if not already set.
	if !currentStep.hasSubSteps() {
		currentStep.SubSteps = initSubSteps(step, rs.Plan)
		currentStep.CurrentSubStep = 0
	}

	// v8.98 Fix C: close sub-step ordering. close-browser-walk is expensive
	// dynamic verification and must run AFTER code-review's static pass so
	// the browser walk observes the post-fix state. Without this guard the
	// agent could attest browser-walk first against pre-fix code, then run
	// code-review which applies fixes — leaving a stale browser-walk
	// attestation behind. completeSubStep's current-pointer check already
	// rejects out-of-order attempts with a generic message; this guard
	// replaces that message with a semantically richer one that names
	// both sub-steps and explains WHY the order matters.
	if step == RecipeStepClose && subStepName == SubStepCloseBrowserWalk {
		codeReviewDone := false
		for _, ss := range currentStep.SubSteps {
			if ss.Name == SubStepCloseReview && ss.Status == stepComplete {
				codeReviewDone = true
				break
			}
		}
		if !codeReviewDone {
			return nil, fmt.Errorf(
				"%s: close sub-step %q must be attested before %q — dispatch the code-review subagent first, apply any fixes, then run the browser walk so it observes the post-fix state; attesting browser-walk first against pre-fix code produces a stale verification signal",
				platform.ErrSubagentMisuse, SubStepCloseReview, SubStepCloseBrowserWalk,
			)
		}
	}
	// C-7.5 Fix D: close sub-step ordering. code-review must run AFTER
	// editorial-review so code-review sees any reclassification + inline
	// fixes the editorial reviewer applied. Without this guard, code-review
	// could grade a pre-fix deliverable, miss the classification errors
	// editorial would have caught, and the close step would ship with
	// wrong-surface gotchas (v28) / fabricated mechanisms (v23) / self-
	// referential content (v34) that editorial-review's reclassification
	// would have flagged. The message names both substeps + explains WHY.
	if step == RecipeStepClose && subStepName == SubStepCloseReview {
		editorialDone := false
		for _, ss := range currentStep.SubSteps {
			if ss.Name == SubStepEditorialReview && ss.Status == stepComplete {
				editorialDone = true
				break
			}
		}
		if !editorialDone {
			return nil, fmt.Errorf(
				"%s: close sub-step %q must be attested before %q — dispatch the editorial-review subagent first, apply any reclassification + inline fixes it reports, then run the code-review sub-agent so it grades the post-fix state; attesting code-review first against pre-fix content produces a stale review signal that missed the classification-error-at-source class (v28 folk-doctrine, v34 self-referential)",
				platform.ErrSubagentMisuse, SubStepEditorialReview, SubStepCloseReview,
			)
		}
	}

	// Run sub-step validator if available.
	if validator := getSubStepValidator(subStepName); validator != nil {
		result := validator(ctx, rs.Plan, rs, attestation)
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
			// C-7.5: merge optional per-check rows into the CheckResult so
			// the agent sees per-predicate pass/fail detail in addition to
			// the aggregate Issues/Guidance. Only populated by validators
			// that run a predicate battery (editorial-review); attestation-
			// floor validators leave Checks nil and keep the pre-C-7.5
			// summary-only shape.
			resp.CheckResult = &StepCheckResult{
				Passed:  false,
				Checks:  result.Checks,
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

// stageWriterContent is the Cx-CLOSE-STEP-STAGING action that copies
// per-codebase writer output (README.md + CLAUDE.md) from each runtime
// target's source mount directory into the recipe output tree under
// `{OutputDir}/{hostname}dev/`. Called during RecipeComplete(step=close)
// after close-phase checks pass. Without this, writer output stays on
// the source mount uncommitted; sessionless export via git ls-files
// strips it and the deliverable ships without per-codebase markdown.
//
// Sources the mount base from recipeMountBase (overridable via
// recipeMountBaseOverride for tests — same pattern OverlayRealREADMEs
// uses so the two staging paths share one mocking surface).
//
// Returns a wrapped error when any expected source file is missing so
// the close-step can refuse completion and name the offending path to
// the agent. Managed-service and shared-codebase-worker targets are
// skipped — only runtimes that own their own codebase carry writer
// output.
func (e *Engine) stageWriterContent(state *WorkflowState) error {
	if state.Recipe == nil || state.Recipe.Plan == nil || state.Recipe.OutputDir == "" {
		return nil
	}
	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}
	var missing []string
	var staged int
	for _, t := range state.Recipe.Plan.Targets {
		if !IsRuntimeType(t.Type) {
			continue
		}
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		codebase := t.Hostname + "dev"
		srcDir := filepath.Join(base, codebase)
		// Source directory absent → writer sub-agent did not run on
		// this codebase (e.g. unit test stubs that skip dispatch, or
		// push-app never occurred). Skip silently; close-step still
		// advances, but a production run with a real mount would
		// have the directory present and fall into the staging body
		// below.
		if _, err := os.Stat(srcDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stage writer content: stat %s: %w", srcDir, err)
		}
		dstDir := filepath.Join(state.Recipe.OutputDir, codebase)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return fmt.Errorf("stage writer content: mkdir %s: %w", dstDir, err)
		}
		for _, name := range []string{"README.md", "CLAUDE.md"} {
			src := filepath.Join(srcDir, name)
			dst := filepath.Join(dstDir, name)
			data, err := os.ReadFile(src)
			if err != nil {
				if os.IsNotExist(err) {
					missing = append(missing, src)
					continue
				}
				return fmt.Errorf("stage writer content: read %s: %w", src, err)
			}
			if err := os.WriteFile(dst, data, 0o600); err != nil {
				return fmt.Errorf("stage writer content: write %s: %w", dst, err)
			}
			staged++
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"stage writer content: %d required file(s) missing on source mount: %s — dispatch the writer sub-agent to author them, then re-attest action=complete step=close",
			len(missing), strings.Join(missing, ", "),
		)
	}
	if staged > 0 {
		fmt.Fprintf(os.Stderr, "zcp: staged %d writer file(s) into %s\n", staged, state.Recipe.OutputDir)
	}
	return nil
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
