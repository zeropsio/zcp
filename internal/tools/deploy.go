package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeployInput is the input type for zerops_deploy.
type DeployInput struct {
	SourceService string `json:"sourceService,omitempty" jsonschema:"SSH mode only: hostname of the container to execute deploy from (e.g. a builder service). Omit for local deploy."`
	TargetService string `json:"targetService,omitempty" jsonschema:"Hostname of the service to deploy code to. Required for both SSH and local modes."`
	Setup         string `json:"setup,omitempty"         jsonschema:"SSH mode only: custom shell command to run before push (e.g. npm install or cp config)."`
	WorkingDir    string `json:"workingDir,omitempty"    jsonschema:"Directory containing the code to deploy. SSH mode default: /var/www. In local mode: path on your machine."`
	IncludeGit    bool   `json:"includeGit,omitempty"    jsonschema:"Include .git directory in the push (zcli -g flag). Rarely needed."`
	FreshGit      bool   `json:"freshGit,omitempty"      jsonschema:"Remove existing .git and reinitialize before push. Use this for first deploys or when the directory has no valid git repo — avoids ownership and identity errors. Recommended for most SSH deploys."`
}

// RegisterDeploy registers the zerops_deploy tool.
func RegisterDeploy(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	sshDeployer ops.SSHDeployer,
	localDeployer ops.LocalDeployer,
	authInfo *auth.Info,
	engine *workflow.Engine,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: "REQUIRES active workflow session — call zerops_workflow action=\"start\" first. Deploy code to a Zerops service via SSH (cross-service) or local zcli push. Blocks until build pipeline completes — returns final status (DEPLOYED or BUILD_FAILED) with build duration. Automatically handles git initialization — use freshGit=true when deploying to a directory without a proper git repo (common for first deploys or shared storage). SSH mode: set sourceService (container to run from) + targetService. Local mode: set only targetService. SSH mode requires zerops.yml in workingDir. After deploy, /var/www only contains deployFiles artifacts — dev services must use deployFiles: [.] so zerops.yml survives for SSH cross-service deploys.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployInput) (*mcp.CallToolResult, any, error) {
		if blocked := requireWorkflow(engine); blocked != nil {
			return blocked, nil, nil
		}
		result, err := ops.Deploy(ctx, client, projectID, sshDeployer, localDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit, input.FreshGit)
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
		result.NextActions = nextActionDeploySuccess
	case statusBuildFailed:
		result.Status = statusBuildFailed
		result.Suggestion = "Check build logs with zerops_logs for details"
		result.NextActions = nextActionDeployBuildFail
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
