package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDeployStart reads service metas and creates a deploy session.
func handleDeployStart(_ context.Context, engine *workflow.Engine, projectID string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(engine.StateDir())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to read service metas: %v", err),
			"Run bootstrap first to create services")), nil, nil
	}

	if len(metas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"No bootstrapped services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\"")), nil, nil
	}

	// Reject incomplete metas (bootstrap started but didn't finish).
	for _, m := range metas {
		if !m.IsComplete() {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Service %q was provisioned but bootstrap didn't complete", m.Hostname),
				"Run bootstrap first to finish setup: action=\"start\" workflow=\"bootstrap\"")), nil, nil
		}
	}

	// Filter to runtime services only (those with a type that has a mode).
	var runtimeMetas []*workflow.ServiceMeta
	for _, m := range metas {
		if m.Mode != "" || m.StageHostname != "" {
			runtimeMetas = append(runtimeMetas, m)
		}
	}
	if len(runtimeMetas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"No runtime services found in service metas",
			"Only managed services exist — nothing to deploy")), nil, nil
	}

	// Strategy check: if any runtime service has no strategy, present selection guidance.
	var needStrategy []*workflow.ServiceMeta
	for _, m := range runtimeMetas {
		if m.DeployStrategy == "" {
			needStrategy = append(needStrategy, m)
		}
	}
	if len(needStrategy) > 0 {
		return jsonResult(buildStrategySelectionResponse(needStrategy)), nil, nil
	}

	targets, mode, strategy := workflow.BuildDeployTargets(runtimeMetas)

	// Check for mixed strategies: all runtime services must have the same strategy for now.
	for i := 1; i < len(targets); i++ {
		if targets[i].Strategy != targets[0].Strategy {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Mixed strategies not supported: %q vs %q", targets[0].Strategy, targets[i].Strategy),
				"Deploy one strategy at a time. Create separate deploy sessions per strategy.")), nil, nil
		}
	}

	resp, err := engine.DeployStart(projectID, input.Intent, targets, mode, strategy)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Deploy start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleDeployComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID, stateDir string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for deploy complete action",
			"Specify step name (e.g., step=\"prepare\")")), nil, nil
	}
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for deploy complete action",
			"Describe what was accomplished")), nil, nil
	}

	checker := buildDeployStepChecker(input.Step, client, projectID, stateDir)

	resp, err := engine.DeployComplete(ctx, input.Step, input.Attestation, checker)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy complete failed: %v", err),
			"Start deploy first with action=start workflow=deploy")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleDeploySkip(_ context.Context, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for deploy skip action",
			"Specify step name")), nil, nil
	}
	reason := input.Reason
	if reason == "" {
		reason = "skipped by user"
	}

	resp, err := engine.DeploySkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy skip failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleDeployStatus(_ context.Context, engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	resp, err := engine.DeployStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy status failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(resp), nil, nil
}
