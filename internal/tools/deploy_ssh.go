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
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// classifyDeployStatus maps a non-DEPLOYED platform status into the
// topology.FailureClass that best names the failure shape:
//
//   - BUILD_FAILED            — build pipeline failed (compile, install,
//     buildCommands). FailureClassBuild.
//   - PREPARING_RUNTIME_FAILED — runtime prep failed (initCommands,
//     prepareCommands, runtime image). The
//     container never reached the start phase.
//     FailureClassStart, because the recovery
//     action is the same as a start failure
//     (review prepareCommands / initCommands,
//     not the build pipeline).
//   - READY_TO_DEPLOY         — deploy ran but the container didn't
//     transition past READY (no start command,
//     port binding wrong, healthCheck flapping).
//     FailureClassStart.
//   - DEPLOY_FAILED           — generic deploy-stage failure (runtime
//     init / startup). FailureClassStart so
//     the LLM gets actionable wording in
//     BuildPlan rationale rather than the
//     content-free "Last attempt failed".
//
// Unknown statuses fall through to FailureClassOther — Reason still
// carries the raw status string so the LLM has the diagnostic content.
func classifyDeployStatus(status string) topology.FailureClass {
	switch status {
	case statusBuildFailed:
		return topology.FailureClassBuild
	case statusPreparingRuntimeFailed, serviceStatusReadyToDeploy, statusDeployFailed:
		return topology.FailureClassStart
	default:
		return topology.FailureClassOther
	}
}

// deployStrategyGitPush is the deploy tool strategy for git-push deploys.
const deployStrategyGitPush = "git-push"

// DeploySSHInput is the input type for zerops_deploy in SSH (container) mode.
//
// includeGit is not user-facing: ZCP enables -g on self-deploys (so a
// service deploying its own code preserves .git and any history scripts
// depend on) and leaves it off on cross-deploys (dev→stage would otherwise
// carry the dev container's .git across).
type DeploySSHInput struct {
	SourceService string `json:"sourceService,omitempty"`
	TargetService string `json:"targetService"`
	Setup         string `json:"setup,omitempty"`
	WorkingDir    string `json:"workingDir,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	RemoteURL     string `json:"remoteUrl,omitempty"`
	Branch        string `json:"branch,omitempty"`
}

func deploySSHInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"sourceService": {Type: "string", Description: "Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."},
		"targetService": {Type: "string", Description: "Hostname of the service to deploy to."},
		"setup":         {Type: "string", Description: "zerops.yaml setup block name — matches a `setup:` key in the file's `zerops:` array. Setup names are user-defined identifiers; recipes conventionally use `dev`/`prod` (and sometimes `worker`) but any name is valid. Required whenever zerops.yaml declares more than one setup — the tool cannot guess which block to build. Recipes always ship multiple setups, so `setup` is effectively required in recipe workflows: `targetService=apidev setup=dev`, `targetService=apistage setup=prod` (a cross-deploy from apidev→apistage uses `setup=prod` because `setup` names the zerops.yaml block, not the deploy source). Omit only when zerops.yaml has a single setup AND its name matches the target hostname (bootstrap workflows only)."},
		"workingDir":    {Type: "string", Description: "Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."},
		"strategy":      {Type: "string", Description: "Deploy strategy. Omit for default push (direct deploy to the Zerops service). Set to 'git-push' to push committed code to an external git remote (requires GIT_TOKEN project env var). BEFORE using git-push: ask the user if they want push-only or full CI/CD. LLM should commit changes via SSH BEFORE calling git-push."},
		"remoteUrl":     {Type: "string", Description: "Git remote URL (HTTPS). Required for strategy=git-push on first push. Omit on subsequent pushes if remote already configured."},
		"branch":        {Type: "string", Description: "Git branch name for git-push. Default: main."},
	}, "targetService")
}

// RegisterDeploySSH registers the zerops_deploy tool for SSH (container) mode.
// httpClient is used by the post-success subdomain auto-enable hook to probe
// L7 readiness before returning — empirically the L7 route takes 440ms-1.3s
// to propagate after EnableSubdomainAccess finishes
// (plans/archive/subdomain-robustness.md §1.3). Tests inject a stub.
func RegisterDeploySSH(
	srv *mcp.Server,
	client platform.Client,
	httpClient ops.HTTPDoer,
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
		// Strategy validation. "manual" is a ServiceMeta declaration only —
		// calling zerops_deploy on a manual-strategy service is a contradiction
		// ZCP refuses to resolve silently.
		if err := validateDeployStrategyParam(input.Strategy); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

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
					"Check zerops.yaml and service configuration"),
					WithRecoveryStatus()), nil, nil
			}
			if pfResult != nil && !pfResult.Passed {
				return convertError(
					platform.NewPlatformError(platform.ErrPreflightFailed, pfResult.Summary, ""),
					WithChecks("preflight", pfResult.Checks),
					WithRecoveryStatus(),
				), nil, nil
			}
			if resolvedSetup != "" {
				input.Setup = resolvedSetup
			}
		}

		// Route: git-push strategy pushes to external git remote, no Zerops build.
		if input.Strategy == deployStrategyGitPush {
			return handleGitPush(ctx, client, projectID, sshDeployer, input, stateDir)
		}

		// Record attempt up front so a crash still leaves a trace.
		// Strategy is "push-dev" for the default zcli-push path; the
		// git-push branch above takes its own handler before we reach
		// here, so this site is exclusively the push-dev case.
		attemptedAt := time.Now().UTC().Format(time.RFC3339)
		attempt := workflow.DeployAttempt{
			AttemptedAt: attemptedAt,
			Setup:       input.Setup,
			Strategy:    "push-dev",
		}

		// Default: zcli push to Zerops.
		result, err := ops.DeploySSH(ctx, client, projectID, sshDeployer, *authInfo,
			input.SourceService, input.TargetService, input.Setup, input.WorkingDir)
		if err != nil {
			attempt.Error = err.Error()
			// SSH/transport-layer failure — we never reached the build.
			classification := classifyTransportError(err, "push-dev")
			if classification != nil {
				attempt.FailureClass = classification.Category
			} else {
				attempt.FailureClass = topology.FailureClassNetwork
			}
			_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
			return convertError(err, WithRecoveryStatus(), WithFailureClassification(classification)), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer)

		if result != nil && result.Status == statusDeployed {
			attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
			// Plan 2: activate L7 subdomain for dev/stage/simple/standard/
			// local-stage modes on first deploy (idempotent via ops.Subdomain's
			// check-before-enable). Runs before RecordDeployAttempt so the
			// result payload surfaces SubdomainAccessEnabled + SubdomainURL
			// alongside the deploy outcome.
			maybeAutoEnableSubdomain(ctx, client, httpClient, projectID, stateDir, input.TargetService, result)
		} else if result != nil {
			attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
			attempt.FailureClass = classifyDeployStatus(result.Status)
		}
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

		return jsonResult(result), nil, nil
	})
}
