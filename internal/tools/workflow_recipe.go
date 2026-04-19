package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// requiredRecipeModel is the only model identifier accepted for new recipe
// workflow sessions. v13 shipped on Sonnet/200k and doubled wall time
// (40.7 → 79.8 min) while regressing close-step severity from 5 WRONG to
// 2 CRITICAL + 1 WRONG. The recipe workflow pulls ~80 KB of guidance topics,
// ~30 KB of schemas, plus the agent's own code-writing context; 200k is not
// enough to hold that plus the feature sub-agent brief plus debugging loops
// without compaction. Keep this list minimal and explicit — any alias ("opus")
// or variant without the [1m] suffix is rejected so the agent has to switch
// client configuration rather than hope the server will tolerate a near-match.
// recipeAllowedModels is the accepted self-reported client model set
// for recipe workflow start. Opus at 1M context only — either 4.6 or
// 4.7. v13 shipped on Sonnet/200k and doubled wall time; variants
// without the [1m] suffix cannot hold the ~80 KB guidance + schemas +
// feature-subagent brief + code-writing context without compaction.
var recipeAllowedModels = map[string]struct{}{
	"claude-opus-4-6[1m]": {},
	"claude-opus-4-7[1m]": {},
}

// validateRecipeModel returns nil when the self-reported client model clears
// the recipe workflow floor. Returns a platform error naming the required
// value on any failure.
func validateRecipeModel(clientModel string) error {
	model := strings.TrimSpace(clientModel)
	if model == "" {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"clientModel is required for recipe workflow start",
			"Pass clientModel=\"claude-opus-4-7[1m]\" (or \"claude-opus-4-6[1m]\") — the agent's exact model ID from its own system prompt. The recipe workflow pulls ~80 KB of guidance plus the agent's code-writing context and requires Opus at 1M tokens. Weaker models double wall time and regress close-step severity.")
	}
	if _, ok := recipeAllowedModels[model]; !ok {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe workflow requires claude-opus-4-7[1m] or claude-opus-4-6[1m] — got %q", model),
			"Switch the client to Claude Opus 4.7 (or 4.6) with the 1M-token context window. In Claude Code: /model claude-opus-4-7[1m]. In headless mode: --model claude-opus-4-7[1m]. Retry action=\"start\" after switching.")
	}
	return nil
}

// handleRecipeStart validates tier and creates a recipe session.
func handleRecipeStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if err := validateRecipeModel(input.ClientModel); err != nil {
		return convertError(err), nil, nil
	}

	tier := input.Tier
	if tier == "" {
		tier = workflow.RecipeTierMinimal
	}

	resp, err := engine.RecipeStart(projectID, input.Intent, tier)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Recipe start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}

	// Inject available stacks for the research step.
	if client != nil && cache != nil {
		if types := cache.Get(ctx, client); len(types) > 0 {
			resp.AvailableStacks = knowledge.FormatServiceStacks(types)
		}
	}

	return jsonResult(resp), nil, nil
}

// handleRecipeComplete routes research step to plan submission, others to checkers.
func handleRecipeComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, schemaCache *schema.Cache, projectID, stateDir string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for recipe complete action",
			"Specify step name (e.g., step=\"research\")")), nil, nil
	}

	// Research step: requires a recipe plan submission.
	if input.Step == workflow.RecipeStepResearch {
		return handleRecipeCompletePlan(ctx, engine, client, cache, schemaCache, input)
	}

	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for recipe complete action",
			"Describe what was accomplished")), nil, nil
	}

	// v8.95: resolve the active session's facts-log path lazily so the
	// content-manifest completeness sub-check can cross-reference recorded
	// FactRecord.Titles against manifest entries. Captured by reference —
	// the closure runs when deploy-step checks execute, by which time the
	// engine has a stable session ID.
	factsLogPathFn := func() string {
		sid := engine.SessionID()
		if sid == "" {
			return ""
		}
		return ops.FactLogPath(sid)
	}
	checker := buildRecipeStepChecker(ctx, input.Step, projectID, stateDir, schemaCache, engine.KnowledgeProvider(), factsLogPathFn)

	var resp *workflow.RecipeResponse
	var err error
	if input.SubStep != "" {
		resp, err = engine.RecipeComplete(ctx, input.Step, input.Attestation, checker, input.SubStep)
	} else {
		resp, err = engine.RecipeComplete(ctx, input.Step, input.Attestation, checker)
	}
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe complete failed: %v", err),
			"Check step name and session state")), nil, nil
	}

	return jsonResult(resp), nil, nil
}

// handleRecipeCompletePlan validates and submits the recipe plan for the research step.
func handleRecipeCompletePlan(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, schemaCache *schema.Cache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.RecipePlan == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"recipePlan is required for research step completion",
			"Submit a structured RecipePlan with framework, tier, slug, runtimeType, research fields")), nil, nil
	}

	attestation := input.Attestation
	if attestation == "" {
		attestation = fmt.Sprintf("Research completed for %s %s recipe (%s)", input.RecipePlan.Framework, input.RecipePlan.Tier, input.RecipePlan.Slug)
	}

	// Get live schemas for plan validation (build/run base enums).
	var schemas *schema.Schemas
	if schemaCache != nil {
		schemas = schemaCache.Get(ctx)
	}
	var liveTypes []platform.ServiceStackType
	if cache != nil && client != nil {
		liveTypes = cache.Get(ctx, client)
	}

	resp, err := engine.RecipeCompletePlan(*input.RecipePlan, attestation, liveTypes, schemas)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe plan submission failed: %v", err),
			"Ensure research step is current and plan is valid")), nil, nil
	}

	return jsonResult(resp), nil, nil
}

// handleRecipeSkip validates skip rules (only close is skippable).
func handleRecipeSkip(_ context.Context, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for recipe skip action",
			"Specify step name")), nil, nil
	}
	reason := input.Reason
	if reason == "" {
		reason = defaultSkipReason
	}

	resp, err := engine.RecipeSkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe skip failed: %v", err),
			"Only the close step can be skipped in recipe workflow")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

// handleRecipeGenerateFinalize generates all recipe repo files using BuildFinalizeOutput.
// Writes files to the recipe output directory and returns the list of files written.
// When envComments or projectEnvVariables is provided, each is merged into the
// plan and baked into the generated import.yaml files — no per-file hand-editing
// required. Idempotent: calling with identical inputs produces byte-identical
// output across runs (guarded by TestGenerateFinalize_Idempotent).
func handleRecipeGenerateFinalize(engine *workflow.Engine, envComments map[string]workflow.EnvComments, projectEnvVariables map[string]map[string]string) (*mcp.CallToolResult, any, error) {
	session := engine.RecipeSession()
	if session == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"No active recipe session",
			"")), nil, nil
	}

	plan := session.Plan
	if plan == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Recipe plan not set — complete the research step first",
			"")), nil, nil
	}

	outputDir := session.OutputDir
	if outputDir == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Output directory not set in recipe state",
			"")), nil, nil
	}

	// Persist per-env comment inputs into the plan (provided env keys merge;
	// empty service values delete; nil map leaves existing untouched). Then
	// reload the session so BuildFinalizeOutput sees the merged plan.
	if envComments != nil {
		if err := engine.UpdateRecipeComments(envComments); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("persist comment inputs: %v", err),
				"")), nil, nil
		}
		session = engine.RecipeSession()
		plan = session.Plan
	}

	// Persist per-env project env var inputs. Atomic per-env replace: passing
	// a non-empty map for an env replaces that env's prior map; empty map
	// clears; omitted env untouched. This is how the agent declares URL
	// constants (DEV_*, STAGE_*) once per recipe instead of hand-editing
	// every re-generated import.yaml.
	if projectEnvVariables != nil {
		if err := engine.UpdateRecipeProjectEnvVariables(projectEnvVariables); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("persist project env var inputs: %v", err),
				"")), nil, nil
		}
		session = engine.RecipeSession()
		plan = session.Plan
	}

	// Generate all files from the plan.
	files := workflow.BuildFinalizeOutput(plan)

	// Overlay real per-codebase READMEs from the SSHFS mounts if the agent
	// wrote them during the generate step. Prevents TODO scaffolds from
	// landing in the deliverable folder for any codebase whose README is
	// already in place.
	readmeOverlayCount := workflow.OverlayRealREADMEs(files, plan)

	// Write files to disk.
	var written []string
	for relPath, content := range files {
		fullPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("mkdir %s: %v", filepath.Dir(fullPath), err),
				"")), nil, nil
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("write %s: %v", fullPath, err),
				"")), nil, nil
		}
		written = append(written, relPath)
	}

	hasComments := len(plan.EnvComments) > 0
	var message string
	var readmeNote string
	if readmeOverlayCount > 0 {
		readmeNote = fmt.Sprintf(" %d README(s) overlaid from /var/www mounts (real content, not scaffold).", readmeOverlayCount)
	} else {
		readmeNote = " App README uses the TODO scaffold — write /var/www/{hostname}dev/README.md for each codebase during generate step and re-run generate-finalize to overlay it."
	}
	if hasComments {
		message = fmt.Sprintf("Regenerated %d recipe files with your per-env comments baked in. Review the output — do NOT edit these files by hand. To refine one env, call generate-finalize again with just that env's updated entry under envComments (merge semantics, rest left untouched).%s", len(written), readmeNote)
	} else {
		message = fmt.Sprintf("Regenerated %d recipe files — no agent comments yet. The 30%% comment ratio check will fail until you provide them. Call `zerops_workflow action=\"generate-finalize\" envComments={\"0\":{\"service\":{\"appdev\":\"...\",\"appstage\":\"...\",\"db\":\"...\"},\"project\":\"...\"}, \"1\":{...}, ..., \"5\":{...}}` with one entry per env (0..5). Service keys match hostnames in that file — envs 0-1 carry appdev+appstage, envs 2-5 carry app. Each env's commentary should reflect what makes THAT env distinct. Do NOT edit import.yaml files by hand (rewriting drops auto-generated zeropsSetup/buildFromGit fields).%s", len(written), readmeNote)
	}
	return jsonResult(map[string]any{
		"status":  "generated",
		"files":   written,
		"count":   len(written),
		"dir":     outputDir,
		"message": message,
	}), nil, nil
}

// handleRecipeStatus returns current recipe state.
func handleRecipeStatus(_ context.Context, engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	resp, err := engine.RecipeStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe status failed: %v", err),
			"")), nil, nil
	}

	return jsonResult(resp), nil, nil
}
