package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleRecipeStart validates tier and creates a recipe session.
func handleRecipeStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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

	checker := buildRecipeStepChecker(input.Step, projectID, stateDir)

	resp, err := engine.RecipeComplete(ctx, input.Step, input.Attestation, checker)
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
func handleRecipeGenerateFinalize(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
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

	// Generate all files from the plan.
	files := workflow.BuildFinalizeOutput(plan)

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

	return jsonResult(map[string]any{
		"status":  "generated",
		"files":   written,
		"count":   len(written),
		"dir":     outputDir,
		"message": fmt.Sprintf("Generated %d recipe files with rich framework-aware comments derived from your research plan. Do NOT rewrite from scratch — reconcile against your actual build: verify framework references match what you implemented (start command, build pipeline, service types). Fix only what diverged from the plan. Review READMEs for accuracy. Then complete the finalize step.", len(written)),
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
