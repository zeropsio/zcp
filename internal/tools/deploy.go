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
	SourceService string `json:"sourceService,omitempty" jsonschema:"Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."`
	TargetService string `json:"targetService"           jsonschema:"Hostname of the service to deploy to."`
	WorkingDir    string `json:"workingDir,omitempty"    jsonschema:"Directory containing the code to deploy. Default: /var/www."`
	IncludeGit    bool   `json:"includeGit,omitempty"    jsonschema:"Include .git directory in the push (-g flag). Auto-forced for self-deploy."`
}

// RegisterDeploy registers the zerops_deploy tool.
func RegisterDeploy(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	sshDeployer ops.SSHDeployer,
	authInfo *auth.Info,
	engine *workflow.Engine,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: "REQUIRES active workflow session — call zerops_workflow action=\"start\" first. Deploy code to a Zerops service via SSH. Blocks until build pipeline completes — returns final status (DEPLOYED or BUILD_FAILED) with build duration. Automatically handles git initialization — git is initialized if no .git directory exists. Self-deploy: set targetService only (sourceService auto-inferred, includeGit auto-forced). Cross-deploy (dev→stage): set sourceService + targetService. Requires zerops.yml in workingDir (/var/www default). After deploy, /var/www only contains deployFiles artifacts. Self-deploying services MUST use deployFiles: [.] — otherwise source files and zerops.yml are destroyed, breaking further iteration. Cross-deploy targets (e.g. stage) can use specific deployFiles for compiled output.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployInput) (*mcp.CallToolResult, any, error) {
		if blocked := requireWorkflow(engine); blocked != nil {
			return blocked, nil, nil
		}
		result, err := ops.Deploy(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.WorkingDir, input.IncludeGit)
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
