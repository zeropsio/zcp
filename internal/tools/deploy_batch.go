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
// Use whenever two or more deploys go together — every stage of a pair
// (stages fed from one dev service build in parallel), several dev services,
// close redeploys. Single-target redeploys (e.g. a worker redeploy after a
// fix) still use zerops_deploy directly — batch is only worth the overhead at
// two-plus targets.
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
	devsup DevsupSource,
	opts ...DeployOption,
) {
	vouch := deployConfigOf(opts).leaseVouch(client, projectID, stateDir)
	desc := "Deploy multiple services in parallel — single MCP call kicks off N builds server-side. " +
		"Closes the MCP STDIO serialization penalty (calling zerops_deploy multiple times in parallel returns 'Not connected'). " +
		"Use whenever two or more deploys go together: every stage of a pair in one call (stages fed from one dev service build in parallel), several dev services, a close redeploy. " +
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
		d := batchDeployer{
			client: client, httpClient: httpClient, projectID: projectID, sshDeployer: sshDeployer,
			authInfo: authInfo, logFetcher: logFetcher, rtInfo: rtInfo, stateDir: stateDir, recipeProbe: recipeProbe, devsup: devsup,
			vouch: vouch,
		}
		if blocked := d.gate(ctx, input.Targets); blocked != nil {
			return blocked, nil, nil
		}

		// Pre-flight each target (matches zerops_deploy behavior); any
		// failure aborts the whole batch so the agent sees the config issue
		// before any build burns time. Pre-flight failures echo resolved
		// setup names back into the targets — v8.85 semantics carried
		// through to batch.
		for i := range input.Targets {
			resolved, refusal := d.preflight(ctx, input.Targets[i])
			if refusal != nil {
				return refusal.result(), nil, nil
			}
			input.Targets[i] = resolved
		}

		result := d.deploy(ctx, buildProgressCallback(ctx, req), input.Targets)
		return jsonResult(deployBatchResponse{
			DeployBatchResult: result,
			WorkSessionState:  sessionAnnotations(ctx, httpClient, stateDir),
		}), nil, nil
	})
}

// batchDeployer is everything a batch of SSH deploys needs. zerops_deploy_batch
// and a Mate's stand-up (standup.go) deploy through it, so both apply the same
// gates, the same pre-flight and the same post-deploy steps to every entry.
type batchDeployer struct {
	client      platform.Client
	httpClient  ops.HTTPDoer
	projectID   string
	sshDeployer ops.SSHDeployer
	authInfo    *auth.Info
	logFetcher  platform.LogFetcher
	rtInfo      runtime.Info
	stateDir    string
	recipeProbe RecipeSessionProbe
	// devsup finds the zcp-devsup a deploy putting a dev setup under the
	// supervisor installs.
	devsup DevsupSource
	// vouch asks a lease source's orchestrator whose its container is
	// before anything reads it (DP-2).
	vouch leaseVouch
}

// gate applies the adoption gate and the push-delivery redirect to every
// target; the first refusal is the answer for the whole batch.
func (d batchDeployer) gate(ctx context.Context, targets []ops.DeployBatchTarget) *mcp.CallToolResult {
	// Gate: each target (and its source, when set) must be adopted by
	// ZCP. Recipe-authoring sessions whose Plan owns the host bypass
	// adoption — see requireAdoption for the exemption rationale. A pool
	// target is never adopted, so it is not asked to be (PS-5, DP-7); a
	// lease source is no service to adopt (DP-1).
	// Batch deploy is container-only (registered when sshDeployer != nil),
	// so requireAdoption is called with InContainer=true to surface the
	// bootstrap recovery hint.
	containerRT := runtime.Info{InContainer: true}
	for _, t := range targets {
		gated := append(withoutPoolHosts(ctx, d.client, d.projectID, t.TargetService), t.SourceService)
		if blocked := requireAdoption(d.stateDir, containerRT, d.recipeProbe, gated...); blocked != nil {
			return blocked
		}
	}
	// L1 terminal-act rule, same gate the single deploy applies
	// (repoDeliveryRedirect): a pair whose pushes are consumed by a
	// ZCP-managed integration delivers via PUSH. Batch used to skip
	// this entirely.
	return batchRepoDeliveryRedirect(d.stateDir, targets)
}

// batchPreflightRefusal is why one target cannot deploy: the setup cannot be
// resolved without the agent, its source is a parked lease, pre-flight could
// not run, or it failed.
type batchPreflightRefusal struct {
	target     string
	setupInput *workflow.ErrRequiresSetupInput
	refusal    error
	err        error
	checks     *workflow.StepCheckResult
}

// result is the refusal as zerops_deploy_batch answers it.
func (r *batchPreflightRefusal) result() *mcp.CallToolResult {
	switch {
	case r.refusal != nil:
		return convertError(r.refusal, WithRecoveryStatus())
	case r.setupInput != nil:
		return jsonResult(buildRequiresSetupInputResponse(r.target, r.setupInput))
	case r.err != nil:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Pre-flight validation error for %s: %v", r.target, r.err),
			"Check zerops.yaml and service configuration"),
			WithRecoveryStatus())
	default:
		return convertError(
			platform.NewPlatformError(
				platform.ErrPreflightFailed,
				fmt.Sprintf("Preflight failed for %s: %s", r.target, r.checks.Summary),
				""),
			WithChecks("preflight", r.checks.Checks),
			WithRecoveryStatus(),
		)
	}
}

// preflight checks one target before any build burns time and resolves the
// setup it deploys with and the config file it builds from.
func (d batchDeployer) preflight(ctx context.Context, t ops.DeployBatchTarget) (ops.DeployBatchTarget, *batchPreflightRefusal) {
	// Source defaults to target for self-deploy (mirrors
	// ops.DeploySSH auto-infer). Pre-flight reads yaml from the
	// source service's mount.
	sourceForPreflight := t.SourceService
	if sourceForPreflight == "" {
		sourceForPreflight = t.TargetService
	}
	// A lease source is asked about first (DP-2): one that is no longer the
	// lease's, or that the orchestrator does not answer about, is never read.
	// A parked lease answers nothing and its mount is gone (DP-28).
	switch lease, ok, err := checkedLeaseOf(ctx, d.stateDir, d.vouch, sourceForPreflight, "deployed from"); {
	case err != nil:
		return t, &batchPreflightRefusal{target: t.TargetService, refusal: err}
	case ok && lease.Parked:
		return t, &batchPreflightRefusal{target: t.TargetService, refusal: ops.LeaseParkedError(lease, "a deploy from it")}
	}
	t.WorkingDir = sourceWorkingDir(d.stateDir, sourceForPreflight, t.WorkingDir)
	resolved, pfResult, pfErr := deployPreFlight(ctx, d.client, d.projectID, d.stateDir, sourceForPreflight, t.TargetService, t.Setup, t.WorkingDir, d.rtInfo.InContainer)
	if pfErr != nil {
		var blocker *workflow.ErrRequiresSetupInput
		if errors.As(pfErr, &blocker) {
			return t, &batchPreflightRefusal{target: t.TargetService, setupInput: blocker}
		}
		return t, &batchPreflightRefusal{target: t.TargetService, err: pfErr}
	}
	if pfResult != nil && !pfResult.Passed {
		return t, &batchPreflightRefusal{target: t.TargetService, checks: pfResult}
	}
	if resolved.setup != "" {
		t.Setup = resolved.setup
	}
	t.ConfigFile = resolved.configFile
	return t, nil
}

// deploy runs the targets as one batch and applies each entry's post-deploy
// steps: its deploy attempt, its supervisor's dev server, its public
// access. An entry whose target is a pool takes none of the last two and says
// it reaches only containers the pool creates later (pollTriggeredDeploy,
// docs/spec-workflows.md §8 DP-7); one whose source is a lease alias pushes
// from that container (ops.DeployBatchSSH, DP-1).
func (d batchDeployer) deploy(ctx context.Context, onProgress ops.ProgressCallback, targets []ops.DeployBatchTarget) *ops.DeployBatchResult {
	ctx = deployFollowUpContext(ctx)
	configFiles := make(map[string]string, len(targets))
	for _, t := range targets {
		configFiles[t.TargetService] = t.ConfigFile
	}
	pollFn := func(c context.Context, r *ops.DeployResult, cb ops.ProgressCallback, lf platform.LogFetcher, s ops.SSHDeployer) {
		pollTriggeredDeploy(c, d.client, d.projectID, r, cb, lf, s, d.stateDir, configFiles[r.TargetService])
	}

	authVal := auth.Info{}
	if d.authInfo != nil {
		authVal = *d.authInfo
	}
	// Each self-deployed dev service goes under the supervisor as a single
	// zerops_deploy puts it there (superviseDevSetup). A lease is a pool
	// container, not a dev service: the supervisor in it is not migrated by
	// this deploy (docs/spec-workflows.md §8 DP).
	moves := map[string]devSetupMove{}
	warnings := map[string]string{}
	for _, t := range targets {
		if _, fromLease := leaseOf(d.stateDir, t.SourceService); fromLease {
			continue
		}
		move, err := superviseDevSetup(ctx, d.client, d.projectID, d.sshDeployer, d.stateDir, d.devsup, t.SourceService, t.TargetService, t.Setup, t.WorkingDir)
		moves[t.TargetService] = move
		if err != nil {
			warnings[t.TargetService] = err.Error()
		}
	}

	result := ops.DeployBatchSSH(
		ctx, d.client, d.projectID, d.sshDeployer, authVal, d.stateDir,
		targets, d.logFetcher, onProgress, pollFn,
	)

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
			classification := classifyTransportError(errors.New(entry.Error), deployStrategyZCLILabel, entry.Target.ConfigFile)
			if classification != nil {
				attempt.FailureClass = classification.Category
			} else {
				attempt.FailureClass = topology.FailureClassNetwork
			}
		case entry.Result != nil && entry.Result.Status == statusDeployed && entry.Result.ReachesNewContainersOnly:
			// The pool is no app runtime (PS-1/PS-2): no public access is
			// decided on it and no dev server asked for (DP-7).
			attempt.SucceededAt = entry.EndedAt
		case entry.Result != nil && entry.Result.Status == statusDeployed:
			attempt.SucceededAt = entry.EndedAt
			// Whether something listens is the dev-server supervisor's
			// word when the target runs one.
			listening := deployListener(ctx, d.sshDeployer, d.stateDir, entry.Result.TargetService, entry.Result)
			ensurePublicAccess(ctx, d.client, d.httpClient, d.projectID, d.stateDir, entry.Result.TargetService, entry.Result, listening...)
			moves[entry.Target.TargetService].settle(d.stateDir, entry.Target.TargetService, listening, entry.Result)
		case entry.Result != nil && entry.Result.TimedOut:
			// In-flight (B23): the build is still running — record the
			// attempt without a FailureClass so the gate doesn't read it
			// as failed and direct a redeploy on top of an in-flight build.
			attempt.Error = deployBuildInFlightMsg
		case entry.Result != nil:
			attempt.Error = fmt.Sprintf("deploy status %s", entry.Result.Status)
			attempt.FailureClass = classifyDeployStatus(entry.Result.Status)
		}
		if entry.Result != nil {
			if told := moves[entry.Target.TargetService].told(entry.Result); told != "" {
				entry.Result.Message = told + " " + entry.Result.Message
			}
			if w := warnings[entry.Target.TargetService]; w != "" {
				entry.Result.Warnings = append(entry.Result.Warnings, w)
			}
			attempt.SHA = entry.Result.SHA
			attempt.AppVersionID = entry.Result.AppVersionID
			attempt.Dirty = entry.Result.Dirty
		}
		_ = workflow.RecordDeployAttempt(d.stateDir, entry.Target.TargetService, attempt)
	}

	return result
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
