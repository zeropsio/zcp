package tools

import (
	"context"

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
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeployInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Deploy(ctx, client, projectID, sshDeployer, localDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
