package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// deployStrategyGitPush is the deploy tool strategy for git-push deploys.
const deployStrategyGitPush = "git-push"

// DeploySSHInput is the input type for zerops_deploy in SSH (container) mode.
//
// IncludeGit is FlexBool so stringified boolean forms go through
// (same reasoning as DiscoverInput/EnvInput — see flexbool.go).
type DeploySSHInput struct {
	SourceService string   `json:"sourceService,omitempty"`
	TargetService string   `json:"targetService"`
	Setup         string   `json:"setup,omitempty"`
	WorkingDir    string   `json:"workingDir,omitempty"`
	IncludeGit    FlexBool `json:"includeGit,omitempty"`
	Strategy      string   `json:"strategy,omitempty"`
	RemoteURL     string   `json:"remoteUrl,omitempty"`
	Branch        string   `json:"branch,omitempty"`
}

func deploySSHInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"sourceService": {Type: "string", Description: "Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."},
		"targetService": {Type: "string", Description: "Hostname of the service to deploy to."},
		"setup":         {Type: "string", Description: "zerops.yaml setup name to use. Required when setup name differs from hostname (e.g. setup=prod for hostname=appstage). Omit when setup name matches hostname."},
		"workingDir":    {Type: "string", Description: "Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."},
		"includeGit":    flexBoolSchema("Include .git directory in the push (-g flag). Auto-forced for self-deploy."),
		"strategy":      {Type: "string", Description: "Deploy strategy. Omit for default (zcli push to Zerops). Set to 'git-push' to push committed code to an external git remote (requires GIT_TOKEN project env var). BEFORE using git-push: ask the user if they want push-only or full CI/CD. LLM should commit changes via SSH BEFORE calling git-push."},
		"remoteUrl":     {Type: "string", Description: "Git remote URL (HTTPS). Required for strategy=git-push on first push. Omit on subsequent pushes if remote already configured."},
		"branch":        {Type: "string", Description: "Git branch name for git-push. Default: main."},
	}, "targetService")
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
		InputSchema: deploySSHInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeploySSHInput) (*mcp.CallToolResult, any, error) {
		// Gate: target (and source) must be adopted by ZCP.
		if blocked := requireAdoption(stateDir, input.TargetService, input.SourceService); blocked != nil {
			return blocked, nil, nil
		}

		// Pre-flight validation (harness) — skip for git-push (no zerops.yaml needed).
		if input.Strategy != deployStrategyGitPush {
			if pfResult, pfErr := deployPreFlight(ctx, client, projectID, stateDir, input.TargetService, input.Setup); pfErr != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Pre-flight validation error: %v", pfErr),
					"Check zerops.yaml and service configuration")), nil, nil
			} else if pfResult != nil && !pfResult.Passed {
				return jsonResult(pfResult), nil, nil
			}
		}

		// Validate strategy parameter.
		if input.Strategy != "" && input.Strategy != deployStrategyGitPush {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q", input.Strategy),
				"Valid values: omit (default zcli push) or 'git-push'",
			)), nil, nil
		}

		// Route: git-push strategy pushes to external git remote, no Zerops build.
		if input.Strategy == deployStrategyGitPush {
			return handleGitPush(ctx, sshDeployer, *authInfo, input)
		}

		// Default: zcli push to Zerops.
		result, err := ops.DeploySSH(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit.Bool())
		if err != nil {
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer)

		return jsonResult(result), nil, nil
	})
}
