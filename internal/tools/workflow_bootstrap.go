package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func handleBootstrapComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, liveTypes []platform.ServiceStackType, tracker *ops.KnowledgeTracker) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for complete action",
			"Specify step name (e.g., step=\"detect\")")), nil, nil
	}

	// Structured plan routing for "plan" step.
	if input.Step == "plan" && len(input.Plan) > 0 {
		resp, err := engine.BootstrapCompletePlan(input.Plan, liveTypes)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Plan validation failed: %v", err),
				"Provide valid plan: [{hostname, type, mode?}]. Hostnames: lowercase a-z0-9, max 25 chars. Managed services default to mode: NON_HA. Specify HA explicitly for production.")), nil, nil
		}
		injectKnowledgeHint(resp, tracker)
		populateStacks(ctx, resp, client, cache)
		return jsonResult(resp), nil, nil
	}

	// Default: free-text attestation.
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for complete action",
			"Describe what was accomplished in this step")), nil, nil
	}

	resp, err := engine.BootstrapComplete(input.Step, input.Attestation)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Complete step failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap")), nil, nil
	}
	injectKnowledgeHint(resp, tracker)
	populateStacks(ctx, resp, client, cache)
	return jsonResult(resp), nil, nil
}

func handleBootstrapSkip(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for skip action",
			"Specify step name (e.g., step=\"mount-dev\")")), nil, nil
	}

	reason := input.Reason
	if reason == "" {
		reason = "skipped by user"
	}

	resp, err := engine.BootstrapSkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Skip step failed: %v", err),
			"Only skippable steps (mount-dev, discover-envs, deploy) can be skipped")), nil, nil
	}
	populateStacks(ctx, resp, client, cache)
	return jsonResult(resp), nil, nil
}

func handleBootstrapStatus(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, tracker *ops.KnowledgeTracker) (*mcp.CallToolResult, any, error) {
	resp, err := engine.BootstrapStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Bootstrap status failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap")), nil, nil
	}
	injectKnowledgeHint(resp, tracker)
	populateStacks(ctx, resp, client, cache)
	return jsonResult(resp), nil, nil
}

// injectKnowledgeHint adds a hint to the load-knowledge step guidance when
// knowledge has already been loaded via prior zerops_knowledge calls.
func injectKnowledgeHint(resp *workflow.BootstrapResponse, tracker *ops.KnowledgeTracker) {
	if resp.Current == nil || resp.Current.Name != "load-knowledge" {
		return
	}
	if tracker == nil || !tracker.IsLoaded() {
		return
	}
	resp.Current.Guidance = fmt.Sprintf(
		"Knowledge already loaded (%s).\nComplete this step with: zerops_workflow action=\"complete\" step=\"load-knowledge\" attestation=\"Already loaded\"",
		tracker.Summary(),
	)
}
