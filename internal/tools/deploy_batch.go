package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeployBatchInput is the MCP input for zerops_deploy_batch — v8.94 §5.9.
// One MCP call kicks off N parallel builds server-side, closing the STDIO
// serialization penalty that causes "Not connected" errors when an agent
// calls zerops_deploy three times in parallel from the client side.
//
// Use for every 3-deploy cluster in a recipe run: initial dev, snapshot-dev,
// stage cross-deploy, close redeploys. Single-target redeploys (e.g. a
// worker redeploy after a fix) still use zerops_deploy directly — batch is
// only worth the overhead at two-plus targets.
type DeployBatchInput struct {
	Targets []ops.DeployBatchTarget `json:"targets" jsonschema:"required,Array of deploy targets. Each target is one sourceService+targetService+setup combination. Minimum 1; recommend 2-3 for parallelism gains. Values beyond 5 may hit build-queue limits and fall back to serial scheduling on the platform side."`
}

// RegisterDeployBatch registers the zerops_deploy_batch MCP tool for SSH
// (container) mode. Not registered in local mode — local deploys don't face
// the STDIO serialization problem and the batch-level goroutine orchestration
// is SSH-specific.
// httpClient drives the post-success subdomain auto-enable hook applied to
// each successful entry — on first deploy for eligible modes the handler
// calls ops.Subdomain and waits for L7 readiness before returning.
func RegisterDeployBatch(
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
	recipeProbe RecipeSessionProbe,
) {
	desc := "Deploy multiple services in parallel — single MCP call kicks off N builds server-side. " +
		"Closes the MCP STDIO serialization penalty (calling zerops_deploy multiple times in parallel returns 'Not connected'). " +
		"Use for every 3-deploy cluster: initial dev, snapshot-dev, cross-stage, close redeploy. " +
		"Per-target failures do NOT cancel siblings — each target runs to completion independently. " +
		"Response aggregates per-target results so you can apply targeted fixes without rolling back the cluster."

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy_batch",
		Description: desc,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy batch — parallel deploys",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployBatchInput) (*mcp.CallToolResult, any, error) {
		if len(input.Targets) == 0 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"zerops_deploy_batch requires at least one target",
				"Pass targets=[{targetService:...}, ...] with one entry per service to deploy."),
				WithRecoveryStatus()), nil, nil
		}

		// Gate: each target (and its source, when set) must be adopted by
		// ZCP. Recipe-authoring sessions whose Plan owns the host bypass
		// adoption — see requireAdoption for the exemption rationale.
		// Batch deploy is container-only (registered when sshDeployer != nil),
		// so requireAdoption is called with InContainer=true to surface the
		// bootstrap recovery hint.
		containerRT := runtime.Info{InContainer: true}
		for _, t := range input.Targets {
			if blocked := requireAdoption(stateDir, containerRT, recipeProbe, t.TargetService, t.SourceService); blocked != nil {
				return blocked, nil, nil
			}
		}

		// Pre-flight each target (matches zerops_deploy behavior); any
		// failure aborts the whole batch so the agent sees the config issue
		// before any build burns time. Pre-flight failures echo resolved
		// setup names back into the targets — v8.85 semantics carried
		// through to batch.
		for i := range input.Targets {
			t := input.Targets[i]
			// Source defaults to target for self-deploy (mirrors
			// ops.DeploySSH auto-infer). Pre-flight reads yaml from the
			// source service's mount.
			sourceForPreflight := t.SourceService
			if sourceForPreflight == "" {
				sourceForPreflight = t.TargetService
			}
			// Batch entries are container-env SSH deploys; workingDir is "".
			// See deploy_ssh.go for the same threading rationale.
			resolvedSetup, pfResult, pfErr := deployPreFlight(ctx, client, projectID, stateDir, sourceForPreflight, t.TargetService, t.Setup, "", rtInfo.InContainer)
			if pfErr != nil {
				var blocker *workflow.ErrRequiresSetupInput
				if errors.As(pfErr, &blocker) {
					return jsonResult(buildRequiresSetupInputResponse(t.TargetService, blocker)), nil, nil
				}
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Pre-flight validation error for %s: %v", t.TargetService, pfErr),
					"Check zerops.yaml and service configuration"),
					WithRecoveryStatus()), nil, nil
			}
			if pfResult != nil && !pfResult.Passed {
				return convertError(
					platform.NewPlatformError(
						platform.ErrPreflightFailed,
						fmt.Sprintf("Preflight failed for %s: %s", t.TargetService, pfResult.Summary),
						""),
					WithChecks("preflight", pfResult.Checks),
					WithRecoveryStatus(),
				), nil, nil
			}
			if resolvedSetup != "" {
				input.Targets[i].Setup = resolvedSetup
			}
		}

		onProgress := buildProgressCallback(ctx, req)
		pollFn := func(c context.Context, r *ops.DeployResult, cb ops.ProgressCallback, lf platform.LogFetcher, d ops.SSHDeployer) {
			pollDeployBuild(c, client, projectID, r, cb, lf, d, stateDir)
		}

		authVal := auth.Info{}
		if authInfo != nil {
			authVal = *authInfo
		}
		result := ops.DeployBatchSSH(
			ctx, client, projectID, sshDeployer, authVal,
			input.Targets, logFetcher, onProgress, pollFn,
		)
		_ = engine // engine is unused; per-entry DeployAttempt recording below uses stateDir directly.

		// Record one DeployAttempt per entry (parity with every other deploy
		// path) AND auto-enable subdomain for successes. Without the attempt
		// recording the auto-close gate, FirstDeployedAt, and needsDeploy were
		// all blind to batch deploys (P0-5). Best-effort, per-target.
		for i := range result.Entries {
			entry := &result.Entries[i]
			attempt := workflow.DeployAttempt{
				AttemptedAt: entry.StartedAt,
				Setup:       entry.Target.Setup,
				Strategy:    deployStrategyZCLILabel,
			}
			switch {
			case entry.Error != "":
				// Kickoff/transport failure (build never started).
				attempt.Error = entry.Error
				classification := classifyTransportError(errors.New(entry.Error), deployStrategyZCLILabel)
				if classification != nil {
					attempt.FailureClass = classification.Category
				} else {
					attempt.FailureClass = topology.FailureClassNetwork
				}
			case entry.Result != nil && entry.Result.Status == statusDeployed:
				attempt.SucceededAt = entry.EndedAt
				maybeAutoEnableSubdomain(ctx, client, httpClient, projectID, stateDir, entry.Result.TargetService, entry.Result)
			case entry.Result != nil && entry.Result.TimedOut:
				// In-flight (B23): the build is still running — record the
				// attempt without a FailureClass so the gate doesn't read it
				// as failed and direct a redeploy on top of an in-flight build.
				attempt.Error = deployBuildInFlightMsg
			case entry.Result != nil:
				attempt.Error = fmt.Sprintf("deploy status %s", entry.Result.Status)
				attempt.FailureClass = classifyDeployStatus(entry.Result.Status)
			}
			_ = workflow.RecordDeployAttempt(stateDir, entry.Target.TargetService, attempt)
		}

		return jsonResult(deployBatchResponse{
			DeployBatchResult: result,
			WorkSessionState:  sessionAnnotations(stateDir),
		}), nil, nil
	})
}

// deployBatchResponse wraps ops.DeployBatchResult with the same
// structured WorkSessionState lifecycle signal as deployLocalResponse
// (F5 closure). Batch deploys cover multiple targets (self + promote,
// or multi-service); the session-state field reads from the current-PID
// work session so it reflects the scope as a whole.
type deployBatchResponse struct {
	*ops.DeployBatchResult
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
}
