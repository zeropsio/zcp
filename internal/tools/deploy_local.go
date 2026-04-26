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
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeployLocalInput is the input type for zerops_deploy in local mode.
// No sourceService — code lives locally, not on a remote service.
//
// Strategy / RemoteURL / Branch carry the same meaning as in container
// (DeploySSHInput), so the LLM uses a single set of params regardless
// of where ZCP is running. The local git-push dispatch uses the user's
// own git config — no GIT_TOKEN, no .netrc, no cross-boundary
// credential juggling.
//
// includeGit is not user-facing: local zcli push always runs with
// --no-git. Recipes that need committed history go through
// strategy=git-push, which drives the user's own git CLI.
type DeployLocalInput struct {
	TargetService string `json:"targetService"`
	Setup         string `json:"setup,omitempty"`
	WorkingDir    string `json:"workingDir,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	RemoteURL     string `json:"remoteUrl,omitempty"`
	Branch        string `json:"branch,omitempty"`
}

func deployLocalInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"targetService": {Type: "string", Description: "Hostname of the Zerops service to deploy to."},
		"setup":         {Type: "string", Description: "zerops.yaml setup block name — matches a `setup:` key in the file's `zerops:` array. Setup names are user-defined identifiers; recipes conventionally use `dev`/`prod` (and `worker` for shared-codebase worker recipes that pack the host service's dev/prod plus the worker setup into one zerops.yaml). Required whenever zerops.yaml declares more than one setup — the tool cannot guess which block to build. Recipes always ship multiple setups, so `setup` is effectively required in recipe workflows: `targetService=apidev setup=dev`, `targetService=apistage setup=prod` (a cross-deploy from apidev→apistage uses `setup=prod` because `setup` names the zerops.yaml block, not the deploy source). Omit only when zerops.yaml has a single setup AND its name matches the target hostname (bootstrap workflows only)."},
		"workingDir":    {Type: "string", Description: "Local path to push from. Default: current directory."},
		"strategy":      {Type: "string", Description: "Deploy strategy. Omit for default push (zerops build from the working directory). Set to 'git-push' to push committed code from your local git repo to the configured origin remote — ZCP invokes your own git, no GIT_TOKEN needed."},
		"remoteUrl":     {Type: "string", Description: "Git remote URL (HTTPS). Optional for strategy=git-push — used only when origin isn't already configured in the local repo; otherwise the existing origin is reused."},
		"branch":        {Type: "string", Description: "Git branch for strategy=git-push. Default: current HEAD branch."},
	}, "targetService")
}

// RegisterDeployLocal registers the zerops_deploy tool for local mode.
// Uses zcli push instead of SSH to deploy code from the user's machine.
// httpClient drives the post-success subdomain auto-enable hook — on first
// deploy for eligible modes (dev/stage/simple/standard/local-stage) the
// handler calls ops.Subdomain and waits for L7 readiness via
// ops.WaitHTTPReady before returning.
func RegisterDeployLocal(
	srv *mcp.Server,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID string,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
	stateDir string,
	engine *workflow.Engine,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_deploy",
		Description: "Push local code to Zerops — blocks until build completes. " +
			"Requires zerops.yaml and zcli installed. " +
			"Set targetService to the Zerops service hostname. " +
			"Channel-blocking: this call holds the MCP STDIO channel for the duration of the build " +
			"(typically 60–120s). Do NOT issue other zerops_* calls in the same response — they will " +
			"return `Not connected` (an MCP transport error, not a platform rejection). Serialize all deploys.",
		InputSchema: deployLocalInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployLocalInput) (*mcp.CallToolResult, any, error) {
		// Strategy validation. "manual" is a ServiceMeta declaration only —
		// calling zerops_deploy on a manual-strategy service is a contradiction
		// ZCP refuses to resolve silently.
		if err := validateDeployStrategyParam(input.Strategy); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		// Gate: target must be adopted by ZCP.
		if blocked := requireAdoption(stateDir, input.TargetService); blocked != nil {
			return blocked, nil, nil
		}

		// Local-only projects have no Zerops-side deploy target — reject
		// push-dev (which needs a service to zcli-push into) and point the
		// user at either linking a stage or using git-push.
		if err := checkLocalOnlyGate(stateDir, input.TargetService, input.Strategy); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		// Route: git-push dispatches to the user's own local git; no Zerops
		// build is triggered from our side.
		if input.Strategy == deployStrategyGitPush {
			return handleLocalGitPush(ctx, client, projectID, *authInfo, input, stateDir)
		}

		// Pre-flight validation (harness). v8.85 — pre-flight echoes the
		// effective setup so zcli always invokes with --setup=<resolved>.
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

		// Strategy on the local deploy path is push-dev (zcli push from the
		// developer's machine). The local-env git-push branch lives in
		// deploy_local_git.go and records its own attempt with
		// Strategy=push-git.
		attempt := workflow.DeployAttempt{
			AttemptedAt: time.Now().UTC().Format(time.RFC3339),
			Setup:       input.Setup,
			Strategy:    string(topology.StrategyPushDev),
		}

		result, err := ops.DeployLocal(ctx, client, projectID, *authInfo,
			input.TargetService, input.Setup, input.WorkingDir)
		if err != nil {
			attempt.Error = err.Error()
			// Local push failed before reaching the platform — transport-
			// layer error (e.g. zcli auth, connection).
			attempt.FailureClass = workflow.FailureClassNetwork
			_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, nil)

		if result != nil && result.Status == statusDeployed {
			attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
			maybeAutoEnableSubdomain(ctx, client, httpClient, projectID, stateDir, input.TargetService, result)
		} else if result != nil {
			attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
			attempt.FailureClass = classifyDeployStatus(result.Status)
		}
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

		note, progress := sessionAnnotations(stateDir)
		return jsonResult(deployLocalResponse{
			DeployResult:      result,
			WorkSessionNote:   note,
			AutoCloseProgress: progress,
		}), nil, nil
	})
}

// deployLocalResponse wraps the local-mode deploy result with session
// annotations: a warning when no active work session is tracking the
// deploy, and the auto-close progress snapshot when one is. Both fields
// are omitted when empty/nil so the response shape stays compatible
// with non-session callers.
type deployLocalResponse struct {
	*ops.DeployResult
	WorkSessionNote   string                      `json:"workSessionNote,omitempty"`
	AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
}

// sessionAnnotations loads the current-PID work session once and derives
// both the "no active session" warning and the auto-close progress
// snapshot. A single disk read serves all response annotations, where
// previously each deploy/verify handler made two independent
// CurrentWorkSession calls.
//
// Either return value may be empty/nil depending on session state; the
// response wrappers use `omitempty` so missing values drop out cleanly.
func sessionAnnotations(stateDir string) (string, *workflow.AutoCloseProgress) {
	ws, err := workflow.CurrentWorkSession(stateDir)
	if err != nil || ws == nil || ws.ClosedAt != "" {
		return noActiveSessionWarning, nil
	}
	return "", workflow.AutoCloseProgressOf(ws)
}

const noActiveSessionWarning = "No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."
