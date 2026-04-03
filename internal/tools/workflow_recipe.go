package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleRecipeStart validates tier and creates a recipe session.
func handleRecipeStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, schemaCache *schema.Cache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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

	// Inject live schema knowledge for research.
	injectSchemaKnowledge(ctx, resp, schemaCache)

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

	// Inject schema knowledge for the next step.
	injectSchemaKnowledge(ctx, resp, schemaCache)

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

	// Fetch schemas once — used for both validation and knowledge injection.
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

	// Inject schema knowledge for the next step (provision).
	// Reuse already-fetched schemas to avoid a second cache lookup.
	injectSchemaKnowledgeWith(resp, schemas)

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
func handleRecipeStatus(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache) (*mcp.CallToolResult, any, error) {
	resp, err := engine.RecipeStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Recipe status failed: %v", err),
			"")), nil, nil
	}

	injectSchemaKnowledge(ctx, resp, schemaCache)

	return jsonResult(resp), nil, nil
}

// injectSchemaKnowledge fetches schemas from cache and injects step-appropriate
// schema info into the recipe response.
func injectSchemaKnowledge(ctx context.Context, resp *workflow.RecipeResponse, schemaCache *schema.Cache) {
	if resp == nil || schemaCache == nil || resp.Current == nil {
		return
	}
	schemas := schemaCache.Get(ctx)
	injectSchemaKnowledgeWith(resp, schemas)
}

// injectSchemaKnowledgeWith injects pre-fetched schema knowledge into a recipe response.
// Avoids redundant cache lookups when the caller already has schemas.
func injectSchemaKnowledgeWith(resp *workflow.RecipeResponse, schemas *schema.Schemas) {
	if resp == nil || schemas == nil || resp.Current == nil {
		return
	}

	switch resp.Current.Name {
	case workflow.RecipeStepResearch:
		// Research needs both schemas for informed planning.
		resp.SchemaKnowledge = schema.FormatBothForLLM(schemas)
	case workflow.RecipeStepProvision:
		// Provision needs import.yaml schema for service creation.
		resp.SchemaKnowledge = schema.FormatImportYmlForLLM(schemas.ImportYml)
	case workflow.RecipeStepGenerate:
		// Generate needs zerops.yml schema for config writing.
		resp.SchemaKnowledge = schema.FormatZeropsYmlForLLM(schemas.ZeropsYml)
	case workflow.RecipeStepFinalize:
		// Finalize needs both: import.yaml for env tiers, zerops.yml for inline config.
		resp.SchemaKnowledge = schema.FormatBothForLLM(schemas)
	case workflow.RecipeStepDeploy:
		// Deploy may need schema for troubleshooting.
		resp.SchemaKnowledge = schema.FormatZeropsYmlForLLM(schemas.ZeropsYml)
	case workflow.RecipeStepClose:
		// Close step: no schema knowledge needed.
	}
}
