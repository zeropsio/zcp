package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeployLocalInput is the input type for zerops_deploy in local mode.
// No sourceService — code lives locally, not on a remote service.
type DeployLocalInput struct {
	TargetService string `json:"targetService"        jsonschema:"Hostname of the Zerops service to deploy to."`
	WorkingDir    string `json:"workingDir,omitempty" jsonschema:"Local path to push from. Default: current directory."`
	IncludeGit    bool   `json:"includeGit,omitempty" jsonschema:"Include .git directory in the push (-g flag)."`
}

// RegisterDeployLocal registers the zerops_deploy tool for local mode.
// Uses zcli push instead of SSH to deploy code from the user's machine.
func RegisterDeployLocal(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_deploy",
		Description: "Push local code to Zerops — blocks until build completes. " +
			"Requires zerops.yaml and zcli installed. " +
			"Set targetService to the Zerops service hostname.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployLocalInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.DeployLocal(ctx, client, projectID, *authInfo,
			input.TargetService, input.WorkingDir, input.IncludeGit)
		if err != nil {
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, nil)

		return jsonResult(result), nil, nil
	})
}
