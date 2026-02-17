package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeployInput is the input type for zerops_deploy.
type DeployInput struct {
	SourceService string `json:"sourceService,omitempty"`
	TargetService string `json:"targetService,omitempty"`
	Setup         string `json:"setup,omitempty"`
	WorkingDir    string `json:"workingDir,omitempty"`
	IncludeGit    bool   `json:"includeGit,omitempty"`
}

// RegisterDeploy registers the zerops_deploy tool.
func RegisterDeploy(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	sshDeployer ops.SSHDeployer,
	localDeployer ops.LocalDeployer,
	authInfo *auth.Info,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: "Deploy code to a Zerops service via SSH (cross-service) or local zcli push.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Deploy(ctx, client, projectID, sshDeployer, localDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit)
		if err != nil {
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress)

		return jsonResult(result), nil, nil
	})
}

// pollDeployBuild polls build status after deploy trigger and updates result in-place.
func pollDeployBuild(
	ctx context.Context,
	client platform.Client,
	projectID string,
	result *ops.DeployResult,
	onProgress ops.ProgressCallback,
) {
	if result.TargetServiceID == "" {
		return
	}

	event, err := ops.PollBuild(ctx, client, projectID, result.TargetServiceID, onProgress)
	if err != nil {
		// Timeout or context cancellation — keep original BUILD_TRIGGERED status.
		result.TimedOut = true
		return
	}

	result.BuildStatus = event.Status
	result.BuildDuration = calcBuildDuration(event)

	switch event.Status {
	case statusActive:
		result.Status = statusDeployed
		result.MonitorHint = ""
		result.Message = fmt.Sprintf("Successfully deployed to %s", result.TargetService)
	case statusBuildFailed:
		result.Status = statusBuildFailed
		result.Suggestion = "Check build logs with zerops_logs for details"
	}
}

// calcBuildDuration computes the build pipeline duration from event build info.
func calcBuildDuration(event *platform.AppVersionEvent) string {
	if event.Build == nil || event.Build.PipelineStart == nil {
		return ""
	}
	start, err := time.Parse(time.RFC3339, *event.Build.PipelineStart)
	if err != nil {
		return ""
	}
	var endStr string
	switch {
	case event.Build.PipelineFinish != nil:
		endStr = *event.Build.PipelineFinish
	case event.Build.PipelineFailed != nil:
		endStr = *event.Build.PipelineFailed
	default:
		return ""
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return ""
	}
	return end.Sub(start).Truncate(time.Second).String()
}
