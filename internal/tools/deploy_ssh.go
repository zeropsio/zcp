package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
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

// deployStrategyManualLabel is the per-service close-mode value that names
// a service as user-orchestrated. NOT a zerops_deploy strategy parameter
// (that's gated separately) — it lives on ServiceMeta.CloseDeployMode and
// reaches gate error/suggestion text when an agent confuses the two.
const deployStrategyManualLabel = "manual"

// deployStrategyZCLILabel is the internal Strategy LABEL written into
// DeployAttempt records by the default zcli-push branch (deploy_ssh.go +
// deploy_local.go). NOT a valid zerops_deploy strategy parameter — agents
// who guess it from attempt-history surfaces are redirected to "omit the
// strategy parameter" by validateDeployStrategyParam. Constant so the
// label has one source of truth across handlers + the gate's rejection
// message + classifyTransportError calls.
const deployStrategyZCLILabel = "zcli"

// deployBuildInFlightMsg is the DeployAttempt.Error recorded when the build
// is still running at poll timeout (not failed) — shared by the ssh, local,
// and batch deploy paths so the agent gets the same in-flight guidance.
const deployBuildInFlightMsg = workflow.DeployBuildInFlightError

// deployStrategyAppVersionLabel is the DeployAttempt.Strategy label for the
// R2 in-place appVersion-redeploy recovery path (docs/spec-workflows.md §8
// R2) — distinct from deployStrategyZCLILabel so attempt history tells the
// two mechanisms apart.
const deployStrategyAppVersionLabel = "appversion"

// appVersionLatest is the only supported value for the appVersion input —
// see validateAppVersionParam.
const appVersionLatest = "latest"

// validateAppVersionParam accepts "latest" (R2 in-place recovery of the
// newest appVersion, runAppVersionRedeploy) or a specific appVersion id —
// any non-empty value with no whitespace (rollback to that recorded
// appVersion, docs/spec-workflows.md §12.6 GF-8, runAppVersionRollback).
// Everything else (whitespace, e.g. a hostname pasted by mistake) is
// rejected before either op runs.
func validateAppVersionParam(appVersion string) *mcp.CallToolResult {
	if appVersion == appVersionLatest {
		return nil
	}
	if appVersion != "" && !strings.ContainsAny(appVersion, " \t\n") {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("appVersion=%q is not a valid appVersion id", appVersion),
		"Pass appVersion=\"latest\" to re-deploy the target's already-built artifact in place, or an appVersion id (read candidates from zerops_events or the status envelope's rollback block — never a probed/fake id) to roll back to it",
	), WithRecoveryStatus())
}

// runAppVersionRedeploy is the R2 in-place-recovery path for zerops_deploy
// appVersion="latest" (docs/spec-workflows.md §8 R2): re-deploys the
// target's already-built appVersion via ops.RedeployLastAppVersion — no
// rebuild, no re-import, no source resolution. Shared by both SSH
// (container) and local deploy handlers; the redeploy call is a pure
// platform API operation, env-agnostic. Records the deploy attempt and
// runs ensurePublicAccess on success (the import's own subdomain-enable
// process is CANCELED on a never-activated service — live-verified — so
// this is the first chance subdomain access gets switched on).
//
// Returns (result, nil) on a completed (success or classified-failure)
// attempt for the caller to wrap into its own response envelope; (nil,
// blocked) when RedeployLastAppVersion itself errored (the caller returns
// blocked verbatim).
func runAppVersionRedeploy(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID, stateDir, targetService, setup, mode string,
) (*ops.DeployResult, *mcp.CallToolResult) {
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       setup,
		Strategy:    deployStrategyAppVersionLabel,
	}

	result, err := ops.RedeployLastAppVersion(ctx, client, projectID, targetService, setup)
	if err != nil {
		attempt.Error = err.Error()
		_ = workflow.RecordDeployAttempt(stateDir, targetService, attempt)
		return nil, convertError(err, WithRecoveryStatus())
	}
	result.Mode = mode

	switch {
	case result.Status == statusDeployed:
		attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
		ensurePublicAccess(ctx, client, httpClient, projectID, stateDir, targetService, result)
	case result.TimedOut:
		attempt.Error = deployBuildInFlightMsg
	default:
		attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
		attempt.FailureClass = classifyDeployStatus(result.Status)
	}
	_ = workflow.RecordDeployAttempt(stateDir, targetService, attempt)
	return result, nil
}

// appVersionRedeploySuggestion returns the R2 corrective (docs/spec-
// workflows.md §8 R2) when a SELF-deploy SSH failure (source omitted or
// source==target) is explained by the target having no container to
// source from at all — a never-activated buildFromGit service whose
// artifact already built. Consumes RecoveryState.Then verbatim (the
// single producer — nothing composes its own text here); empty string
// (no override) for a cross-deploy, a lookup/classify failure, a target
// with a container, or one whose artifact never built.
func appVersionRedeploySuggestion(ctx context.Context, client platform.Client, projectID, sourceService, targetService string) string {
	if sourceService != "" && sourceService != targetService {
		return "" // cross-deploy: SSH connects to the SOURCE, not this target
	}
	svc, err := ops.LookupService(ctx, client, projectID, targetService)
	if err != nil || svc == nil {
		return ""
	}
	state, err := ops.ComputeRecoveryState(ctx, client, nil, projectID, targetService, svc.Status)
	if err != nil || !state.ArtifactBuilt || state.HasContainer {
		return ""
	}
	return state.Then
}

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
	// BreakGlass overrides the L1 push-delivery redirect for a FUNDAMENTAL
	// reason (git host outage, recovery). See repoDeliveryRedirect.
	BreakGlass FlexBool `json:"breakGlass,omitempty"`
	// AppVersion switches this call to a source-free path against the
	// platform's PUT /app-version/{id}/deploy — no rebuild, no re-import,
	// no source resolution. "latest" is the R2 in-place recovery for a
	// never-activated buildFromGit service (docs/spec-workflows.md §8 R2):
	// the target's newest (DEPLOY_FAILED) appVersion is re-deployed. Any
	// other value is a specific appVersion id: the GF-8 rollback
	// (§12.6) — that recorded appVersion is re-activated, which only
	// succeeds when it is currently BACKUP.
	AppVersion string `json:"appVersion,omitempty"`
	// SHA, when set, deploys the EXACT git commit instead of the current
	// working tree: resolved via `git rev-parse` inside the SOURCE
	// container, its tree extracted into a temp dir there, and pushed with
	// --version-name. The deploy attempt records its source revision.
	// docs/spec-workflows.md §4.9.
	SHA string `json:"sha,omitempty"`
}

func deploySSHInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"sourceService": {Type: "string", Description: "Hostname to deploy FROM. Omit for self-deploy (auto-inferred from targetService). Set for cross-deploy (e.g. dev→stage)."},
		"targetService": {Type: "string", Description: "Hostname of the service to deploy to."},
		"setup":         {Type: "string", Description: "zerops.yaml setup block name (a `setup:` key under `zerops:`). Omit when there is one setup, or one whose name matches the target hostname/role — the deploy resolves it. Pass explicitly when multiple setups exist and none matches; on a mismatch the deploy returns the available setup names to choose from."},
		"workingDir":    {Type: "string", Description: "Container path for deploy. Default: /var/www. In container mode: omit entirely (always correct)."},
		"strategy":      {Type: "string", Description: "Deploy strategy. Omit for default push (direct deploy to the Zerops service). Set to 'git-push' to push committed code to an external git remote (requires GIT_TOKEN — a service-scope secret on the push source, written by git-push-setup). BEFORE using git-push: ask the user if they want push-only or full CI/CD. LLM should commit changes via SSH BEFORE calling git-push."},
		"remoteUrl":     {Type: "string", Description: "Git remote URL (HTTPS). Required for strategy=git-push on first push. Omit on subsequent pushes if remote already configured."},
		"branch":        {Type: "string", Description: "Git branch name for git-push. Default: main."},
		"breakGlass":    {Type: "boolean", Description: "Override for the push-delivery redirect: a pair with git-push configured delivers via push (the repo is the source of truth); a direct deploy is refused with the recommended push call unless breakGlass=true. Reserve for fundamental reasons (git host outage, recovery) — the response then flags that the container is ahead of the repo."},
		"appVersion":    {Type: "string", Description: "'latest' re-deploys the newest built artifact in place (recovery). An appVersion id of the target re-activates that BACKUP artifact without a build (rollback, ~1 min) — read candidates from zerops_events or the status rollback block, never a probed id."},
		"sha":           {Type: "string", Description: "Deploy this exact git commit instead of the working tree. Returns the resolved sha and deployed appVersionId."},
	}, "targetService")
}

// DeployOption configures RegisterDeploySSH and RegisterDeployBatch.
type DeployOption func(*deployConfig)

type deployConfig struct {
	swarm ops.SwarmFactory
	// vouch replaces the orchestrator's answer (tests).
	vouch leaseVouch
}

// leaseVouch is how the deploy tools ask whose a lease source's container is.
func (c deployConfig) leaseVouch(client platform.Client, projectID, stateDir string) leaseVouch {
	if c.vouch != nil {
		return c.vouch
	}
	return orchestratorVouch(client, projectID, stateDir, c.swarm)
}

// WithPoolOrchestratorsForDeploy lets a deploy from a lease ask the pool's
// orchestrator, through swarm, whether the lease's container is still the
// lease's before anything reads it (docs/spec-workflows.md §8 DP-2). Without
// it a deploy from a lease is refused: zcp cannot vouch for the container.
func WithPoolOrchestratorsForDeploy(swarm ops.SwarmFactory) DeployOption {
	return func(c *deployConfig) { c.swarm = swarm }
}

func deployConfigOf(opts []DeployOption) deployConfig {
	var cfg deployConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// RegisterDeploySSH registers the zerops_deploy tool for SSH (container) mode.
// httpClient is used by the post-success subdomain auto-enable hook to probe
// L7 readiness before returning — empirically the L7 route takes 440ms-1.3s
// to propagate after EnableSubdomainAccess finishes
// (plans/archive/subdomain-robustness.md §1.3). Tests inject a stub.
//
// recipeProbe wires the recipe-authoring exemption into requireAdoption:
// a recipe session whose Plan owns the deploy target satisfies the
// adoption gate so cross-deploys (e.g. `apidev → apistage`) succeed
// before any bootstrap workflow runs. May be nil in tests.
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
	recipeProbe RecipeSessionProbe,
	devsup DevsupSource,
	opts ...DeployOption,
) {
	vouch := deployConfigOf(opts).leaseVouch(client, projectID, stateDir)
	desc := "Deploy code via SSH — blocks until build completes. "
	if rtInfo.InContainer {
		desc += "Omit workingDir — container path is always /var/www. "
	} else {
		desc += "workingDir defaults to /var/www. "
	}
	desc += "Requires zerops.yaml. Self-deploy: set targetService only. Cross-deploy: set sourceService + targetService. " +
		"Self-deploying services MUST use deployFiles: [.] — otherwise source files are destroyed. " +
		"strategy=git-push: pushes committed code to an external git remote. " +
		"Blocks 60–120s; for two or more deploys make one zerops_deploy_batch call."

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_deploy",
		Description: desc,
		InputSchema: deploySSHInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeploySSHInput) (*mcp.CallToolResult, any, error) {
		// R2 in-place recovery (docs/spec-workflows.md §8 R2): appVersion
		// redeploy has no source to resolve and no cross/self distinction —
		// dispatch BEFORE strategy validation, the adoption gate, and the
		// push-delivery redirect, all of which assume a source container.
		if input.AppVersion != "" {
			if blocked := validateAppVersionParam(input.AppVersion); blocked != nil {
				return blocked, nil, nil
			}
			// A non-"latest" value is an appVersion id: rollback
			// (docs/spec-workflows.md §12.6 GF-8), not the R2 newest-only
			// recovery below.
			if input.AppVersion != appVersionLatest {
				result, blocked := runAppVersionRollback(ctx, client, projectID, stateDir, input.TargetService, input.AppVersion, "ssh")
				if blocked != nil {
					return blocked, nil, nil
				}
				return jsonResult(deploySSHResponse{
					DeployResult:     result,
					WorkSessionState: sessionAnnotations(ctx, httpClient, rtInfo, stateDir),
					Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
				}), nil, nil
			}
			result, blocked := runAppVersionRedeploy(ctx, client, httpClient, projectID, stateDir, input.TargetService, input.Setup, "ssh")
			if blocked != nil {
				return blocked, nil, nil
			}
			return jsonResult(deploySSHResponse{
				DeployResult:     result,
				WorkSessionState: sessionAnnotations(ctx, httpClient, rtInfo, stateDir),
				Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
			}), nil, nil
		}

		// Strategy validation. "manual" is a ServiceMeta declaration only —
		// calling zerops_deploy on a manual-strategy service is a contradiction
		// ZCP refuses to resolve silently.
		if err := validateDeployStrategyParam(input.Strategy); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		// Gate: target (and source) must be adopted by ZCP. Recipe-
		// authoring sessions whose Plan owns the host bypass adoption —
		// see requireAdoption for the exemption rationale. A pool target
		// or source is never adopted, so it is not asked to be: a pool
		// target is deployed to (DP-7), a pool source is refused below (PS-3).
		// The project is listed only for what needs a service's type: the
		// gate's pool exemption once it refuses, a source zcp has not
		// adopted, and the push.
		listServices, listedServices := lazyDeployListing(ctx, client, projectID)
		if blocked := requireDeployAdoption(stateDir, rtInfo, recipeProbe, listServices, input.TargetService, input.SourceService); blocked != nil {
			return blocked, nil, nil
		}
		// A lease's container is asked about once, before anything reads it
		// (DP-2): one that is no longer the lease's — maybe another holder's
		// — or that the orchestrator does not answer about is never read or
		// pushed from.
		srcLease, fromLease, leaseErr := checkedLeaseOf(ctx, stateDir, vouch, cmp.Or(input.SourceService, input.TargetService), "deployed from")
		if leaseErr != nil {
			return convertError(leaseErr, WithRecoveryStatus()), nil, nil
		}
		// A lease pushes its own branch itself; git-push pushes the target's
		// tracked branch (main by default), so from a lease it would put the
		// feature branch on the stage's delivery branch (DP-6). A branch is
		// tried on its lease's preview URL and checked on a stage that then
		// runs main again (DP-8).
		if input.Strategy == deployStrategyGitPush {
			if lease, ok := leaseOf(stateDir, cmp.Or(input.SourceService, input.TargetService)); ok {
				return convertError(leaseGitPushRefusal(lease, rtInfo), WithRecoveryStatus()), nil, nil
			}
		}
		// Before any read of the source's files: a parked lease answers
		// nothing and its mount is gone (DP-28).
		if fromLease && srcLease.Parked {
			return convertError(ops.LeaseParkedError(srcLease, "a deploy from it"), WithRecoveryStatus()), nil, nil
		}
		// Before any read of the source's files: a pool source (named, or a
		// self-deploy of a pool) is refused, never pushed from (PS-3).
		if src := cmp.Or(input.SourceService, input.TargetService); !isLease(stateDir, src) && !adoptedNonPool(stateDir, src) {
			if svc, err := ops.FindService(listServices(), src); err == nil {
				if refusal := ops.RefusePoolSource(svc, input.TargetService); refusal != nil {
					return convertError(refusal, WithRecoveryStatus()), nil, nil
				}
			}
		}

		// L1 terminal-act rule (spec-git-delivery-target §2): a pair with
		// git-push configured delivers via PUSH; direct deploys redirect
		// to the push call unless break-glassed.
		if redirect := repoDeliveryRedirect(stateDir, input.TargetService, input.Strategy, input.BreakGlass.Bool()); redirect != nil {
			return redirect, nil, nil
		}

		// Pre-flight validation (harness) — skip for git-push (no zerops.yaml needed).
		// v8.85 — pre-flight echoes the effective setup back so zcli always
		// invokes with --setup=<resolved>, even when the caller passed an
		// empty string and pre-flight found the match via role/hostname
		// fallback. Closes session-log-16 L145 where setup was resolved in
		// pre-flight but zcli still got empty and failed.
		//
		// Source defaults to target for self-deploy (mirrors ops.DeploySSH
		// auto-infer). Pre-flight reads yaml from the source service's mount
		// — the canonical per-codebase location and the same path
		// `ops.deploySSH` reads from after pre-flight.
		sourceForPreflight := input.SourceService
		if sourceForPreflight == "" {
			sourceForPreflight = input.TargetService
		}
		input.WorkingDir = sourceWorkingDir(stateDir, sourceForPreflight, input.WorkingDir)
		// configFile is the config file the deploy builds from, as pre-flight
		// read it; its failure classification names that file.
		var configFile string
		if input.Strategy != deployStrategyGitPush {
			// SSH deploy reads yaml from the source SSHFS mount in container
			// env, below it for a workingDir on a code volume. In LOCAL env
			// there is no mount: sourceMountReadable=false defers yaml
			// validation to deploy time.
			resolved, pfResult, pfErr := deployPreFlight(ctx, client, projectID, stateDir, sourceForPreflight, input.TargetService, input.Setup, input.WorkingDir, rtInfo.InContainer)
			if pfErr != nil {
				var blocker *workflow.ErrRequiresSetupInput
				if errors.As(pfErr, &blocker) {
					return jsonResult(buildRequiresSetupInputResponse(input.TargetService, blocker)), nil, nil
				}
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
			if resolved.setup != "" {
				input.Setup = resolved.setup
			}
			configFile = resolved.configFile
		}

		// Route: git-push strategy pushes to the external git remote, then
		// follows the integration-triggered build to terminal (§6.1 watch).
		if input.Strategy == deployStrategyGitPush {
			return handleGitPush(ctx, client, httpClient, projectID, sshDeployer, logFetcher, buildProgressCallback(ctx, req), input, stateDir, rtInfo)
		}

		return runDeploySSHZCLIPush(ctx, req, client, httpClient, sshDeployer, authInfo, logFetcher, rtInfo, projectID, stateDir, configFile, devsup, listedServices(), input)
	})
}

// runDeploySSHZCLIPush is the default zcli-push branch of zerops_deploy's
// SSH-mode handler — split out of RegisterDeploySSH's closure to keep both
// under the maintainability-index ceiling. Strategy is "zcli" for this
// mechanism; the git-push branch takes its own handler (`Strategy:
// "git-push"`) before the caller reaches here, so this site is exclusively
// the default-zcli case. `record-deploy` writes `Strategy: "record-deploy"`
// for externally-stamped deploys.
func runDeploySSHZCLIPush(
	ctx context.Context,
	req *mcp.CallToolRequest,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
	rtInfo runtime.Info,
	projectID, stateDir, configFile string,
	devsup DevsupSource,
	services []platform.ServiceStack,
	input DeploySSHInput,
) (*mcp.CallToolResult, any, error) {
	// Record attempt up front so a crash still leaves a trace.
	attemptedAt := time.Now().UTC().Format(time.RFC3339)
	attempt := workflow.DeployAttempt{
		AttemptedAt: attemptedAt,
		Setup:       input.Setup,
		Strategy:    deployStrategyZCLILabel,
	}

	// A lease is a pool container, not a dev service: the supervisor in it is
	// not migrated by this deploy (docs/spec-workflows.md §8 DP).
	_, fromLease := leaseOf(stateDir, input.SourceService)
	var move devSetupMove
	var superviseErr error
	if !fromLease {
		move, superviseErr = superviseDevSetup(ctx, client, projectID, sshDeployer, stateDir, devsup, input.SourceService, input.TargetService, input.Setup, input.WorkingDir)
	}

	// Default: zcli push to Zerops.
	result, err := ops.DeploySSHFromListing(ctx, client, projectID, sshDeployer, *authInfo, stateDir, services,
		input.SourceService, input.TargetService, input.Setup, input.WorkingDir, input.SHA)
	if err != nil {
		attempt.Error = err.Error()
		// SSH/transport-layer failure — we never reached the build.
		classification := classifyTransportError(err, deployStrategyZCLILabel, configFile)
		if classification != nil {
			attempt.FailureClass = classification.Category
		} else {
			attempt.FailureClass = topology.FailureClassNetwork
		}
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
		// R2 (docs/spec-workflows.md §8): a self-deploy SSH failure
		// against a never-activated buildFromGit target (no container
		// to source from at all) can never succeed on retry — override
		// the generic transport suggestion with the in-place redeploy.
		suggestion := appVersionRedeploySuggestion(ctx, client, projectID, input.SourceService, input.TargetService)
		// The dev setup was rewritten before the push, so a failed deploy
		// still says so: the diff is never left unexplained.
		toldMove := func(w *ErrorWire) {
			if told := move.told(nil); told != "" {
				w.Suggestion = strings.TrimSpace(w.Suggestion + " " + told)
			}
		}
		return convertError(err, WithRecoveryStatus(), WithFailureClassification(classification), WithSuggestion(suggestion), toldMove), nil, nil
	}

	// A deploy to a pool, whatever its source, reaches only the containers
	// the pool creates later (docs/spec-workflows.md §8 DP-7).
	toPool := result.ReachesNewContainersOnly
	onProgress := buildProgressCallback(ctx, req)
	ctx = deployFollowUpContext(ctx)
	pollTriggeredDeploy(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer, stateDir, configFile)
	if told := move.told(result); told != "" {
		result.Message = told + " " + result.Message
	}
	if superviseErr != nil {
		result.Warnings = append(result.Warnings, superviseErr.Error())
	}
	if steer := codeVolumeSteerFor(stateDir, input.TargetService); steer != "" {
		result.Warnings = append(result.Warnings, steer)
	}

	// L1 break-glass aftermath: a direct deploy on a push-delivering
	// pair that shipped what the repo lacks leaves the container ahead of
	// it — flag the standing reconcile push (spec-git-delivery-target §1.1).
	if result.Status == statusDeployed && !toPool {
		if warn := repoDeliveryDivergenceWarning(ctx, stateDir, input.TargetService, sshShipped(sshDeployer, input, result)); warn != "" {
			result.Warnings = append(result.Warnings, warn)
		}
	}

	switch {
	case result != nil && result.Status == statusDeployed && toPool:
		// The pool is no app runtime (PS-1/PS-2): no public access is decided
		// on it, no dev server asked for, no code volume filled.
		attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
	case result != nil && result.Status == statusDeployed:
		attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
		// Plan 2: activate L7 subdomain for dev/stage/simple/standard/
		// local-stage modes on first deploy (idempotent via ops.Subdomain's
		// check-before-enable). Runs before RecordDeployAttempt so the
		// result payload surfaces SubdomainAccessEnabled + SubdomainURL
		// alongside the deploy outcome. Whether something listens is the
		// dev-server supervisor's word when the target runs one.
		listening := deployListener(ctx, sshDeployer, stateDir, input.TargetService, result)
		ensurePublicAccess(ctx, client, httpClient, projectID, stateDir, input.TargetService, result, listening...)
		move.settle(stateDir, input.TargetService, listening, result)
		fillCodeVolumeAfterDeploy(ctx, sshDeployer, stateDir, input.TargetService, result)
	case result != nil && result.TimedOut:
		// In-flight (B23): the build is still running at poll timeout, not
		// failed. Record without a FailureClass so the envelope doesn't
		// say "last attempt failed" and direct a redeploy on top of it.
		attempt.Error = deployBuildInFlightMsg
	case result != nil:
		attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
		attempt.FailureClass = classifyDeployStatus(result.Status)
	}
	if result != nil {
		attempt.SHA = result.SHA
		attempt.AppVersionID = result.AppVersionID
		attempt.Dirty = result.Dirty
	}
	_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

	// F10 fix (round-3 audit): default container SSH zerops_deploy was
	// the sole deploy path returning raw *ops.DeployResult — the F5
	// `WorkSessionState` lifecycle signal was already on every other
	// deploy path (deploy_local, deploy_batch, deploy_git_push,
	// deploy_local_git, verify, record-deploy) but not here. The
	// asymmetry meant container agents had to call status after every
	// deploy to discover whether auto-close fired; local agents got the
	// signal inline. Wrap to match.
	return jsonResult(deploySSHResponse{
		DeployResult:     result,
		WorkSessionState: sessionAnnotations(ctx, httpClient, rtInfo, stateDir),
		Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
	}), nil, nil
}

// pollTriggeredDeploy is pollDeployBuild for a triggered SSH deploy, single or
// batched. A deploy to a pool, whatever its source, reaches only the
// containers the pool creates later (docs/spec-workflows.md §8 DP-7): no
// container of the pool is "the" pool, so nothing is probed by its service
// hostname (PS-3), and the finished result says what the build changed
// (sayPoolImageOnly), naming the source when it is a lease of that pool.
func pollTriggeredDeploy(
	ctx context.Context,
	client platform.Client,
	projectID string,
	result *ops.DeployResult,
	onProgress ops.ProgressCallback,
	logFetcher platform.LogFetcher,
	sshDeployer ops.SSHDeployer,
	stateDir, configFile string,
) {
	if !result.ReachesNewContainersOnly {
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, sshDeployer, stateDir, configFile)
		return
	}
	pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, nil, stateDir, configFile)
	var ownLease string
	if lease, fromLease := leaseOf(stateDir, result.SourceService); fromLease && lease.Pool == result.TargetService {
		ownLease = lease.Alias
	}
	sayPoolImageOnly(result, result.TargetService, ownLease)
}

// sayPoolImageOnly rewrites what a finished deploy to a pool tells the agent,
// whatever its source: the build changed the pool's image, it updated no
// container, and existing ones — a lease the deploy came from included
// (lease, empty for any other source) — keep their version until the pool is
// rolled out. pollDeployBuild's own wording ("Successfully deployed", a
// verify pointer) speaks of a running service and does not apply to a pool.
// Releasing and acquiring again can hand back the same outdated container,
// so only the rollout puts free containers on this version.
func sayPoolImageOnly(result *ops.DeployResult, pool, lease string) {
	if result.Status != statusDeployed {
		return
	}
	result.Message = fmt.Sprintf("Built appVersion %s for pool %s%s.", result.AppVersionID, pool, ops.PoolDeployNote(pool, lease))
	held := "A held lease keeps its version"
	if lease != "" {
		held = fmt.Sprintf("Your lease %s keeps its version", lease)
	}
	result.NextActions = fmt.Sprintf("zerops_lease action=rollout pool=%q replaces the pool's free outdated containers with ones on this version. "+
		"%s until it is released and the pool is rolled out again. "+
		"Verify a container through its lease alias (zerops_verify serviceHostname=\"<alias>\"), never through the pool's hostname.", pool, held)
}

// deploySSHResponse mirrors deployLocalResponse — wraps the SSH-mode
// deploy result with the structured WorkSessionState lifecycle signal so
// container deploys get the same F5 surface local deploys already have.
type deploySSHResponse struct {
	*ops.DeployResult
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}
