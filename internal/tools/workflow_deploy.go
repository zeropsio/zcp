package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func handleDeployComplete(_ context.Context, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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

	resp, err := engine.DeployComplete(input.Step, input.Attestation)
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
