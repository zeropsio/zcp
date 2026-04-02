package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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

	// Inject available stack types if possible.
	if client != nil && cache != nil {
		// Stack injection will be added in Phase 2 with guidance.
		_ = cache.Get(ctx, client)
	}

	return jsonResult(resp), nil, nil
}

// handleRecipeComplete routes research step to plan submission, others to checkers.
func handleRecipeComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, projectID, stateDir string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for recipe complete action",
			"Specify step name (e.g., step=\"research\")")), nil, nil
	}

	// Research step: requires a recipe plan submission.
	if input.Step == workflow.RecipeStepResearch {
		return handleRecipeCompletePlan(ctx, engine, client, cache, input)
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
func handleRecipeCompletePlan(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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

	// Get live types for plan validation (best-effort).
	var liveTypes []platform.ServiceStackType
	if cache != nil && client != nil {
		liveTypes = cache.Get(ctx, client)
	}

	resp, err := engine.RecipeCompletePlan(*input.RecipePlan, attestation, liveTypes)
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
