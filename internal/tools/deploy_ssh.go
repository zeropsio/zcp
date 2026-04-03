package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// DeploySSHInput is the input type for zerops_deploy in SSH (container) mode.
type DeploySSHInput struct {
	SourceService string `json:"sourceService,omitempty" jsonschema:"Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."`
	TargetService string `json:"targetService"           jsonschema:"Hostname of the service to deploy to."`
	WorkingDir    string `json:"workingDir,omitempty"    jsonschema:"Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."`
	IncludeGit    bool   `json:"includeGit,omitempty"    jsonschema:"Include .git directory in the push (-g flag). Auto-forced for self-deploy."`
}

// RegisterDeploySSH registers the zerops_deploy tool for SSH (container) mode.
func RegisterDeploySSH(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	sshDeployer ops.SSHDeployer,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
	rtInfo runtime.Info,
) {
	desc := "Deploy code via SSH — blocks until build completes. "
	if rtInfo.InContainer {
		desc += "Omit workingDir — container path is always /var/www. "
	} else {
		desc += "workingDir defaults to /var/www. "
	}
	desc += "Requires zerops.yaml. Self-deploy: set targetService only. Cross-deploy: set sourceService + targetService. " +
		"Self-deploying services MUST use deployFiles: [.] — otherwise source files are destroyed."

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: desc,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeploySSHInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.DeploySSH(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.WorkingDir, input.IncludeGit)
		if err != nil {
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer)

		return jsonResult(result), nil, nil
	})
}
