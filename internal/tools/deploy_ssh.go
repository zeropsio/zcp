package tools

import (
	"context"
	"fmt"
	"time"

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
		"setup":         {Type: "string", Description: "zerops.yaml setup block name — matches a `setup:` key in the file's `zerops:` array. Setup names are user-defined identifiers; recipes conventionally use `dev`/`prod` (and sometimes `worker`) but any name is valid. Required whenever zerops.yaml declares more than one setup — the tool cannot guess which block to build. Recipes always ship multiple setups, so `setup` is effectively required in recipe workflows: `targetService=apidev setup=dev`, `targetService=apistage setup=prod` (a cross-deploy from apidev→apistage uses `setup=prod` because `setup` names the zerops.yaml block, not the deploy source). Omit only when zerops.yaml has a single setup AND its name matches the target hostname (bootstrap workflows only)."},
		"workingDir":    {Type: "string", Description: "Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."},
		"includeGit":    flexBoolSchema("Include .git directory in the push (-g flag). Auto-forced for self-deploy."),
		"strategy":      {Type: "string", Description: "Deploy strategy. Omit for default push (direct deploy to the Zerops service). Set to 'git-push' to push committed code to an external git remote (requires GIT_TOKEN project env var). BEFORE using git-push: ask the user if they want push-only or full CI/CD. LLM should commit changes via SSH BEFORE calling git-push."},
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
		"strategy=git-push: pushes committed code to an external git remote. " +
		"Channel-blocking 60–120s — serialize deploys, no parallel zerops_* calls (returns Not connected)."

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
		// v8.85 — pre-flight echoes the effective setup back so zcli always
		// invokes with --setup=<resolved>, even when the caller passed an
		// empty string and pre-flight found the match via role/hostname
		// fallback. Closes session-log-16 L145 where setup was resolved in
		// pre-flight but zcli still got empty and failed.
		if input.Strategy != deployStrategyGitPush {
			resolvedSetup, pfResult, pfErr := deployPreFlight(ctx, client, projectID, stateDir, input.TargetService, input.Setup)
			if pfErr != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Pre-flight validation error: %v", pfErr),
					"Check zerops.yaml and service configuration")), nil, nil
			}
			if pfResult != nil && !pfResult.Passed {
				return jsonResult(pfResult), nil, nil
			}
			if resolvedSetup != "" {
				input.Setup = resolvedSetup
			}
		}

		// Validate strategy parameter.
		if input.Strategy != "" && input.Strategy != deployStrategyGitPush {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q", input.Strategy),
				"Valid values: omit (default push) or 'git-push'",
			)), nil, nil
		}

		// Route: git-push strategy pushes to external git remote, no Zerops build.
		if input.Strategy == deployStrategyGitPush {
			return handleGitPush(ctx, sshDeployer, *authInfo, input, stateDir)
		}

		// Record attempt up front so a crash still leaves a trace.
		attemptedAt := time.Now().UTC().Format(time.RFC3339)
		attempt := workflow.DeployAttempt{
			AttemptedAt: attemptedAt,
			Setup:       input.Setup,
			Strategy:    "",
		}

		// Default: zcli push to Zerops.
		result, err := ops.DeploySSH(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit.Bool())
		if err != nil {
			attempt.Error = err.Error()
			_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer)

		if result != nil && result.Status == statusDeployed {
			attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
		} else if result != nil {
			attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
		}
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

		return jsonResult(result), nil, nil
	})
}
