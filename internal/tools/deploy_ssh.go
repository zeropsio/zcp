package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeploySSHInput is the input type for zerops_deploy in SSH (container) mode.
type DeploySSHInput struct {
	SourceService string `json:"sourceService,omitempty" jsonschema:"Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."`
	TargetService string `json:"targetService"           jsonschema:"Hostname of the service to deploy to."`
	Setup         string `json:"setup,omitempty"         jsonschema:"zerops.yaml setup name to use. Required when setup name differs from hostname (e.g. setup=prod for hostname=appstage). Omit when setup name matches hostname."`
	WorkingDir    string `json:"workingDir,omitempty"    jsonschema:"Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."`
	IncludeGit    bool   `json:"includeGit,omitempty"    jsonschema:"Include .git directory in the push (-g flag). Auto-forced for self-deploy."`
	Strategy      string `json:"strategy,omitempty"      jsonschema:"Deploy strategy. Omit for default (zcli push to Zerops). Set to 'git-push' to push committed code to an external git remote. LLM should commit changes via SSH BEFORE calling git-push."`
	RemoteURL     string `json:"remoteUrl,omitempty"     jsonschema:"Git remote URL (HTTPS). Required for strategy=git-push on first push. Omit on subsequent pushes if remote already configured."`
	Branch        string `json:"branch,omitempty"        jsonschema:"Git branch name for git-push. Default: main."`
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
	stateDir string,
	engine *workflow.Engine,
) {
	desc := "Deploy code via SSH — blocks until build completes. "
	if rtInfo.InContainer {
		desc += "Omit workingDir — container path is always /var/www. "
	} else {
		desc += "workingDir defaults to /var/www. "
	}
	desc += "Requires zerops.yaml. Self-deploy: set targetService only. Cross-deploy: set sourceService + targetService. " +
		"Self-deploying services MUST use deployFiles: [.] — otherwise source files are destroyed. " +
		"strategy=git-push: push committed code to external git remote (GitHub/GitLab). LLM commits first, tool pushes."

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: desc,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeploySSHInput) (*mcp.CallToolResult, any, error) {
		// Gate: deploy requires an active workflow session.
		if blocked := requireWorkflow(engine); blocked != nil {
			return blocked, nil, nil
		}
		// Gate: target (and source) must be adopted by ZCP.
		if blocked := requireAdoption(stateDir, input.TargetService, input.SourceService); blocked != nil {
			return blocked, nil, nil
		}

		// Validate strategy parameter.
		if input.Strategy != "" && input.Strategy != "git-push" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q", input.Strategy),
				"Valid values: omit (default zcli push) or 'git-push'",
			)), nil, nil
		}

		// Route: git-push strategy pushes to external git remote, no Zerops build.
		if input.Strategy == "git-push" {
			return handleGitPush(ctx, sshDeployer, *authInfo, input)
		}

		// Default: zcli push to Zerops.
		result, err := ops.DeploySSH(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit)
		if err != nil {
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer)

		return jsonResult(result), nil, nil
	})
}
