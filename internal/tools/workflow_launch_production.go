package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchMutationStaleAfter is the threshold beyond which a "launching"
// state-file entry with empty TargetProjectID is considered abandoned
// (the original mutation crashed). Below this threshold, a concurrent
// retry is refused to prevent silent-double-mutation (FIX 1 PR 2).
//
// 10 minutes covers normal mutation latency (CreateAndImportProject +
// async process polling typically settles in 2-3 min). Stale recovery
// after that window allows the user to retry without an explicit reset
// when the prior call genuinely crashed.
const launchMutationStaleAfter = 10 * time.Minute

// handleLaunchProduction orchestrates the launch-production workflow per
// plans/production-lifecycle-2026-05-11.md §8.1. Stateless multi-call
// narrowing via per-request WorkflowInput fields:
//   - ProductionProjectName / Region / CustomDomain / KeepNonHA — scope
//   - EnvClassifications — classify-prompt outputs
//   - LaunchKey — one-shot launch-window token with project-creation
//     permission (mutation pipeline, Phase D.2)
//
// Six top-level statuses:
//
//	scope-prompt    → ProductionProjectName empty
//	classify-prompt → source envs present, classifications incomplete
//	ready-to-launch → scope + classifications complete, awaiting LaunchKey
//	launching       → LaunchKey supplied; mutation pipeline in flight (D.2)
//	failed          → mutation step failed (D.2)
//	launched        → terminal success (D.2)
//
// P-LP-1 is enforced at the OUTPUT boundary: no field on
// launchProductionResponse, launchState, or launchAuditEntry carries
// the LaunchKey value. The auth-failure error wrapper never echoes it.
// The input field accepts the value (json:"launchKey") so the mcp-go
// schema exposes the property — without this, every call returns
// "unexpected additional properties [launchKey]". Handler must still
// not log it, structured-log it (reflect-based loggers would expose
// it), or include it in error messages we author.
//
// P-LP-2: this is the ONLY file in internal/tools/ that may construct
// platform.ProjectAdminClient. Pinned by
// internal/platform/project_admin_imports_test.go.
//
// projectAdminClientFactory constructs a ProjectAdminClient from a
// launch-window key. Indirected so unit tests can inject a mock without
// hitting the real Zerops API. Package-level var so the launch handler
// can call it; tests override via setProjectAdminClientFactory.
//
// Production default: platform.NewProjectAdminClient.
//
//nolint:gochecknoglobals // test-injection point for the cross-project surface
var projectAdminClientFactory = platform.NewProjectAdminClient

// processStatusFinished is the canonical platform.Process success
// terminal status. Pulled to a constant to satisfy strict-lint
// (goconst) and centralize the magic string.
const processStatusFinished = "FINISHED"

// setProjectAdminClientFactory swaps the factory for tests. Restore with
// the returned cleanup func via defer.
func setProjectAdminClientFactory(f func(launchKey, apiHost string) (platform.ProjectAdminClient, error)) func() {
	prev := projectAdminClientFactory
	projectAdminClientFactory = f
	return func() { projectAdminClientFactory = prev }
}

// handleLaunchProduction is the dispatch entry for action="start" workflow="launch-production".
// State machine has 7 statuses + 4-branch resume; the orchestrator
// necessarily reads linearly through them. Splitting into per-status
// sub-handlers was tried in plan v3 and produced 8 small functions that
// each had to re-extract the same input fields — net negative for
// readability. Cyclomatic complexity = state-machine size, not nested
// conditionals. Phase-aware resume (FIX 1 PR 2 of 2026-05-19 review)
// adds 4 branches but each is a single-condition early return.
//
//nolint:maintidx // see doc-comment above
func handleLaunchProduction(
	ctx context.Context,
	projectID string,
	client platform.Client,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
	sshDeployer ops.SSHDeployer,
) (*mcp.CallToolResult, any, error) {
	if client == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Platform client unavailable — launch-production requires API access for source-project discovery",
			"Ensure ZCP is configured with a Zerops API key (ZCP_API_KEY) before invoking launch-production.",
		), WithRecoveryStatus()), nil, nil
	}
	if projectID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Project ID unavailable — launch-production requires a configured source-project context",
			"Ensure ZCP is bound to a Zerops project (ZCP_PROJECT_ID or zcp config).",
		), WithRecoveryStatus()), nil, nil
	}

	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	// Source discovery (project name + service list) feeds the
	// scope-prompt SourceContext hint and the classify-prompt env table.
	// Best-effort: errors return nil and the response surfaces the
	// missing fields via blockers without the discovery hint.
	// stateDir + rt enable pair-keyed collapse and ZCP self-filter.
	sourceContext := gatherLaunchSourceContext(ctx, client, projectID, stateDir, rt)

	// Status 1 — scope-prompt: required scope fields incomplete.
	if missing := missingScopeFields(input, sourceContext); len(missing) > 0 {
		return launchScopePromptResponse(corpus, input, missing, sourceContext), nil, nil
	}
	// Accept either dev-half or stage-half of a standard pair as
	// targetService; normalize to the canonical dev-half (ServiceMeta
	// primary key) for downstream meta lookup + bundle composition.
	// Both halves share the same git source and setup blocks, so the
	// distinction is presentational — stage is the validated headline,
	// dev is the build key.
	input.TargetService = normalizeTargetServiceForLaunch(stateDir, input.TargetService)

	// Status 1.5 — source-control gate (P-LP-10). Fires after scope is
	// complete but before classify-prompt: refuse to advance the
	// workflow until every promoted runtime has a user-owned git
	// remote wired via ServiceMeta.GitPushState=configured AND the
	// live origin in /var/www matches the recorded RemoteURL.
	// Read-side gate — never audit-logs (writeAudit=false at this
	// site); the publish-side mutation pipeline re-runs the gate with
	// audit enabled so drift between the two surfaces appears in
	// launch-audit-log.json.
	gateCheck, gateBlockers, gateErr := validateLaunchSourceControl(
		ctx, client, sshDeployer, rt, stateDir,
		input.TargetService, input.SkipBuildIntegration,
	)
	if gateErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("source-control gate: %v", gateErr),
			"Inspect ZCP state directory + ServiceMeta files for the source project; re-run after fixing.",
		), WithRecoveryStatus()), nil, nil
	}
	if gateHasBlockingFailure(gateBlockers) {
		return launchSourceControlRequiredResponse(corpus, input, sourceContext, gateBlockers), nil, nil
	}
	// Warn-only blockers (build-integration-recommended) attached to the
	// classify-prompt response so the agent still sees them when scope is
	// otherwise green. Cleared once SkipBuildIntegration ack lists the
	// hostname.
	_ = gateCheck

	// Read source project envs (needed for both classify-prompt and
	// publish-time bundle composition). Layer-2 entry point via
	// inventory.FetchProjectEnvs keeps the SDK shape (Type/Sensitive/
	// Editable) so envclass (Layer 3) can drop SYSTEM-scoped envs at
	// the handler before they reach the prompt or the composer.
	sourceEnvs, err := inventory.FetchProjectEnvs(ctx, client, projectID)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	// Status 2 — classify-prompt: source envs present, not all
	// user-classified. SYSTEM-scoped envs (zeropsSubdomain*, CDN URLs,
	// envIsolation/sshIsolation) are envclass-Drop and excluded
	// upstream — the prompt only surfaces USER-scoped envs.
	if needsClassifyPrompt(input.EnvClassifications, sourceEnvs) {
		classifications := convertClassificationsInput(input.EnvClassifications)
		return launchClassifyPromptResponse(corpus, sourceEnvs, classifications, sourceContext, gateBlockers), nil, nil
	}

	// Effective classifications for bundle composition: user-supplied
	// classifications only. envclass-Drop envs never reach the
	// composer (filtered at bundleProjectEnvsFromSource boundary), so no
	// per-key auto-bucketing is needed here.
	classifications := convertClassificationsInput(input.EnvClassifications)

	// Status 3+ — ready-to-launch / mutation pipeline.
	// Check for existing launch state — if a prior publish already created
	// the target project, idempotent resume returns the current state
	// instead of re-importing.
	launchID := generateLaunchID(projectID, input.ProductionProjectName)
	existing, err := readLaunchState(stateDir, launchID)
	if err != nil && !errors.Is(err, ErrLaunchStateMissing) {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("read launch state: %v", err),
			"Inspect .zcp/state/launch-production/ and clean up corrupted state files if needed.",
		), WithRecoveryStatus()), nil, nil
	}

	// Phase-aware resume (FIX 1 PR 2 — eval root-cause review 2026-05-19).
	// Four resume branches based on (Status, TargetProjectID):
	//
	//   1. launching + TargetProjectID=="" — silent-double-mutation P0
	//      lock. Mutation crashed between state-file persist and
	//      CreateAndImportProject success; a blind retry would create
	//      a SECOND project under the same name. Refuse with a
	//      timeout-gated retry hint (allow only when state is stale
	//      i.e. >launchMutationStaleAfter ago).
	//
	//   2. failed + TargetProjectID=="" — safe to retry with fresh
	//      launchKey. No prod project exists; re-enter mutation phase
	//      after wiping the failed state-file entry by ALLOWING fall-
	//      through to the normal mutation gate. Stale launchKey
	//      protection: handled by ZP API rejection on bad token.
	//
	//   3. failed + TargetProjectID!="" — destructive retry refused.
	//      Project + partial services exist; blind re-import would
	//      create duplicate services or hit projectEnvDuplicateKey.
	//      Surface a Recovery hint pointing at action="reset" so the
	//      agent first cleans the state file (and optionally the orphan
	//      project via Zerops dashboard), then re-attempts cleanly.
	//
	//   4. launched / launching-with-project — current idempotent resume
	//      (pipeline check rerun when launchKey supplied; else state-as-is).
	if existing != nil {
		if existing.TargetProjectID == "" {
			//exhaustive:ignore — only Launching and Failed need
			// resume-time treatment when TargetProjectID is empty; the
			// remaining statuses (Unset/ScopePrompt/ClassifyPrompt/
			// ReadyToLaunch/ConfiguringPipeline/Launched) flow through
			// the normal status machine below the resume gate.
			switch existing.Status {
			case topology.LaunchStatusLaunching:
				// P0 silent-double-mutation lock. Fresh `launching` state
				// without a target project ID = mutation in progress
				// elsewhere (or just crashed). Refuse blindly retrying.
				if time.Since(existing.LastUpdate) < launchMutationStaleAfter {
					return convertError(platform.NewPlatformError(
						platform.ErrAPIError,
						fmt.Sprintf(
							"launch-production already in progress (status=launching, lastUpdate=%s); refusing concurrent mutation",
							existing.LastUpdate.UTC().Format("2006-01-02T15:04:05Z"),
						),
						`Another zerops_workflow invocation is mid-mutation. Wait for it to finish (action="status" surfaces progress) or run action="reset" workflow="launch-production" if the prior call is genuinely dead and you want to retry from scratch.`,
					), WithRecoveryStatus()), nil, nil
				}
				// Stale (>launchMutationStaleAfter): the original call
				// crashed long ago; allow fall-through so the user can
				// retry. The state file gets overwritten as part of
				// executeLaunchMutation.
			case topology.LaunchStatusFailed:
				// Failed before target project was created → safe to
				// retry. Fall through to mutation gate.
			default:
				// scope-prompt / classify-prompt / ready-to-launch with
				// empty TargetProjectID are normal flow — handled by the
				// status machine below, not the resume gate.
			}
		} else {
			// TargetProjectID populated → project exists in Zerops.
			if existing.Status == topology.LaunchStatusFailed {
				// Refuse destructive retry; point at reset.
				return convertError(platform.NewPlatformError(
					platform.ErrAPIError,
					fmt.Sprintf(
						"launch-production for %q is in terminal failed state with targetProjectId=%s; cannot blindly retry — orphan project may exist",
						existing.TargetProjectName, existing.TargetProjectID,
					),
					`Run action="reset" workflow="launch-production" productionProjectName="`+existing.TargetProjectName+`" to clear the state file. The orphan production project in your Zerops account is NOT deleted by reset (state-file scoped only); inspect via dashboard and delete manually if unwanted, then retry the launch with a fresh launchKey and a different productionProjectName.`,
				), WithRecoveryStatus()), nil, nil
			}
			// launched / launching-with-project / configuring-pipeline —
			// current idempotent resume.
			if existing.Status == topology.LaunchStatusLaunched && input.LaunchKey != "" {
				if pendingPipelineConfigurations(existing) || (input.SkipPipelineSetup.Bool() && !pipelineSkipRecorded(existing)) {
					return executeLaunchPipelineResume(ctx, input, corpus, stateDir, existing)
				}
			}
			return launchResumeResponse(corpus, existing), nil, nil
		}
	}

	// P-LP-3 active compare gate: try to compute the current
	// SourceSnapshot from live source state so it can be persisted at
	// ready-to-launch (baseline) AND compared at launching (drift). The
	// snapshot is the immutability anchor; without it, the workflow has
	// no signal to detect mid-flight source mutations or state tampering.
	//
	// Soft-read at ready-to-launch: if the source can't be read or
	// validated yet (missing setup:prod, no remote, no zerops.yaml),
	// the workflow still emits ready-to-launch — the user gets to see
	// scope + classification summary before tackling source-control
	// fixes. Baseline persistence is skipped in that branch; drift
	// detection only engages when a valid baseline has been captured.
	//
	// Hard-read at launching: the existing executeLaunchMutation gate
	// re-runs readAndValidateSourceState and surfaces source-control
	// blockers; nothing here changes that — current still calls it.
	var current ops.SourceSnapshot
	var haveCurrent bool
	resolvedForBaseline := resolveLaunchRuntimes(stateDir, input)
	if len(resolvedForBaseline) > 0 {
		if _, sourceBlocker := readAndValidateSourceState(ctx, client, sshDeployer, rt, corpus, input, projectID, stateDir, launchID, false); sourceBlocker == nil {
			// Compose per-runtime inputs using the gate-validated RepoURL
			// (gateCheck already populated). For the baseline soft-read
			// path the gate ran above (gateCheck != nil only on read-side
			// pass); re-derive a fresh per-runtime gate result here so the
			// composer's per-runtime RepoURL discipline holds.
			gateChecks := make([]*LaunchSourceControlCheck, 0, len(resolvedForBaseline))
			gateClean := true
			for _, r := range resolvedForBaseline {
				ck, ckBlockers, ckErr := validateLaunchSourceControl(ctx, client, sshDeployer, rt, stateDir, r.ChoiceHostname, input.SkipBuildIntegration)
				if ckErr != nil || gateHasBlockingFailure(ckBlockers) || ck == nil {
					gateClean = false
					break
				}
				gateChecks = append(gateChecks, ck)
			}
			if gateClean {
				bundleInputs, _, composeErr := composeLaunchBundleInputs(
					ctx, client, sshDeployer, rt,
					projectID, input.ProductionProjectName,
					resolvedForBaseline, gateChecks,
					bundleProjectEnvsFromSource(sourceEnvs),
					input.KeepNonHA,
					bundle.VariantLaunchNew,
				)
				if composeErr == nil {
					if b, bundleErr := ops.BuildLaunchBundle(bundleInputs, classifications); bundleErr == nil {
						current = b.SourceSnapshot
						haveCurrent = true
					}
				}
			}
		}
	}

	hasExistingPath := input.ExistingProjectID != "" && input.ExistingProdToken != ""
	publishing := input.LaunchKey != "" || hasExistingPath

	// Reject ambiguous publish input: caller MUST pick one mutation
	// path (new-project via LaunchKey, or existing-project via
	// ExistingProjectID+ExistingProdToken). Both supplied means the
	// agent is misclassifying the user's intent — fail closed.
	if input.LaunchKey != "" && hasExistingPath {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Mutually exclusive credentials: launchKey (new-project path) AND existingProjectId+existingProdToken (existing-project path) cannot both be supplied",
			"Pick one path: either launchKey for a fresh production project, or existingProjectId+existingProdToken to import services into a pre-existing project.",
		), WithRecoveryStatus()), nil, nil
	}

	// Drift gate: when a prior ready-to-launch transition persisted a
	// baseline and the user is now publishing (either path), recompute
	// current and refuse on mismatch. The existing state file is
	// preserved on refusal so operators can inspect the drift.
	var zeroSnapshot ops.SourceSnapshot
	if haveCurrent && publishing && existing != nil && existing.SourceSnapshot != zeroSnapshot && existing.SourceSnapshot != current {
		return launchSourceDriftResponse(corpus, existing.SourceSnapshot, current), nil, nil
	}

	if !publishing {
		// Persist the ready-to-launch baseline on first transition when
		// the source state was readable. Idempotent: subsequent calls
		// without publish credentials reuse the existing baseline rather
		// than refreshing it (baseline is fixed at the moment of
		// classification completion; user must abandon + re-start to
		// refresh).
		if haveCurrent && existing == nil {
			_ = writeLaunchState(stateDir, &launchState{
				LaunchID:              launchID,
				SourceProjectID:       projectID,
				TargetProjectName:     input.ProductionProjectName,
				TargetServiceHostname: input.TargetService,
				SourceSnapshot:        current,
				Classifications:       classifications,
				Status:                topology.LaunchStatusReadyToLaunch,
			})
		}
		return launchReadyToLaunchResponse(corpus, input, sourceEnvs, sourceContext), nil, nil
	}

	// Existing-project mutation path takes priority — the user has
	// explicitly identified the target project via ExistingProjectID.
	if hasExistingPath {
		return executeExistingProjectMutation(ctx, projectID, client, sshDeployer, rt, input, sourceEnvs, classifications, corpus, stateDir, launchID)
	}

	// Mutation pipeline (new-project path) — LaunchKey supplied,
	// no existing target, baseline matches current.
	return executeLaunchMutation(ctx, projectID, client, sshDeployer, rt, input, sourceEnvs, classifications, corpus, stateDir, launchID)
}

// launchSourceDriftResponse builds the structured refusal response
// for the P-LP-3 active-compare gate. The response carries:
//   - status="failed"
//   - blocker carrying the structured "source-drift" identifier
//   - both baseline + current snapshots so the agent can diff and
//     decide whether to abandon the ready-to-launch (re-classify
//     against new source) or revert source to match baseline.
//
// State file is NOT modified by this response — caller preserves the
// existing baseline so operators can inspect the drift after the fact.
func launchSourceDriftResponse(corpus []workflow.KnowledgeAtom, baseline, current ops.SourceSnapshot) *mcp.CallToolResult {
	msg := fmt.Sprintf(
		"Source state changed since the ready-to-launch baseline was captured. "+
			"Refusing publish to protect the immutability gate.\n"+
			"baseline.commitSha=%s current.commitSha=%s\n"+
			"baseline.zeropsYamlSha256=%s current.zeropsYamlSha256=%s\n"+
			"baseline.projectEnvsDigest=%s current.projectEnvsDigest=%s\n"+
			"baseline.serviceListDigest=%s current.serviceListDigest=%s\n"+
			"To proceed: either revert source to baseline (git checkout) "+
			"or abandon this launch (delete state file under "+
			".zcp/state/launch-production/) and restart the workflow to "+
			"capture a fresh baseline against the current source.",
		baseline.GitCommitSHA, current.GitCommitSHA,
		baseline.ZeropsYAMLSHA256, current.ZeropsYAMLSHA256,
		baseline.ProjectEnvsDigest, current.ProjectEnvsDigest,
		baseline.ServiceListDigest, current.ServiceListDigest,
	)
	return launchFailedResponse(corpus, topology.BlockerCategoryOther, "source-drift", msg)
}

// pipelineSkipRecorded returns true when state.PipelineConfigurations
// already has entries with SkipReason=pipelineSkipReasonOptedOut — so we
// don't re-run the skip on every resume.
func pipelineSkipRecorded(state *launchState) bool {
	for _, entry := range state.PipelineConfigurations {
		if entry.SkipReason == pipelineSkipReasonOptedOut {
			return true
		}
	}
	return false
}

// executeLaunchPipelineResume re-runs the pipeline check on an existing
// launched state. Constructs a fresh ProjectAdminClient from the
// supplied launchKey, runs executeLaunchPipelineCheck, writes the
// updated state, and returns the launched response with refreshed
// blockers. ZCP never PUTs (Path B); this is GetStatus-only.
func executeLaunchPipelineResume(
	ctx context.Context,
	input WorkflowInput,
	corpus []workflow.KnowledgeAtom,
	stateDir string,
	state *launchState,
) (*mcp.CallToolResult, any, error) {
	admin, err := projectAdminClientFactory(input.LaunchKey, "")
	if err != nil {
		return launchFailedAuthResponse(corpus, err), nil, nil
	}
	defer admin.Close()

	checkInputs := pipelineCheckInputs{
		SkipPipelineSetup: input.SkipPipelineSetup.Bool(),
		TagRegexOverride:  input.PipelineTagRegex,
		RuntimeHostname:   state.TargetServiceHostname,
		RepoURL:           state.SourceRepoURL,
	}
	executeLaunchPipelineCheck(ctx, admin, state, checkInputs)
	_ = writeLaunchState(stateDir, state)
	_ = appendAuditLog(stateDir, launchAuditEntry{
		LaunchID:          state.LaunchID,
		Action:            "pipeline-recheck",
		SourceProjectID:   state.SourceProjectID,
		TargetProjectID:   state.TargetProjectID,
		TargetProjectName: state.TargetProjectName,
		Result:            "success",
	})
	return launchLaunchedResponse(corpus, state), nil, nil
}

// executeLaunchMutation runs the read-modify-write mutation pipeline:
//  1. Construct ProjectAdminClient from launchKey (validates key).
//  2. Build LaunchBundle from source state.
//  3. Call CreateAndImportProject.
//  4. Write state file with results.
//  5. Append audit log entry.
//  6. Return launching/launched/failed status.
//
// The launchKey lives inside admin's SDK handler — never copied to local
// vars. defer admin.Close() zeros it before return.
//
// P-LP-1: no field on the response or state file carries the key.
//
//nolint:maintidx // state-machine size, not nested conditionals; P2 multi-runtime composer reshape pushed cyclomatic complexity up by 2 (composeLaunchBundleInputs error branch + bundleResult RepoURL plumbing) but the function is still linear top-to-bottom
func executeLaunchMutation(
	ctx context.Context,
	sourceProjectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	input WorkflowInput,
	sourceEnvs []platform.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	corpus []workflow.KnowledgeAtom,
	stateDir string,
	launchID string,
) (*mcp.CallToolResult, any, error) {
	admin, err := projectAdminClientFactory(input.LaunchKey, "")
	if err != nil {
		// Don't leak the key value in the error — wrap via the typed error.
		return launchFailedAuthResponse(corpus, err), nil, nil
	}
	defer admin.Close()

	// Source-state validation + read. Returns a blocker response when
	// any check fails (target service missing, zerops.yaml missing,
	// setup: prod block missing, git remote missing). Otherwise returns
	// the fully-populated source state for bundle composition. This is
	// the new-project mutation path's hard-read — every failure is a
	// real publish attempt and SHOULD persist a publish-rejected audit
	// entry (writeAudit=true).
	source, blocker := readAndValidateSourceState(ctx, client, sshDeployer, rt, corpus, input, sourceProjectID, stateDir, launchID, true)
	if blocker != nil {
		return blocker, nil, nil
	}
	_ = source // kept for the legacy single-runtime readAndValidate flow's auditFail side-effects; per-runtime sources are read inside composeLaunchBundleInputs.

	resolved := resolveLaunchRuntimes(stateDir, input)

	// Publish-side source-control gate (P-LP-10 hard re-check). Shared
	// helper with executeExistingProjectMutation — drift between read-
	// side OK and publish-side fail is a real publish refusal operators
	// want logged (writeAudit semantics handled inside the helper).
	// Returns one LaunchSourceControlCheck per resolved runtime; the
	// composer reads the gate-validated MetaRemoteURL from each.
	gateResult := runPublishSideSourceControlGate(
		ctx, corpus, client, sshDeployer, rt, input,
		sourceProjectID, stateDir, launchID, resolved,
	)
	if gateResult.Response != nil {
		return gateResult.Response, nil, nil
	}

	bundleInputs, composeWarnings, composeErr := composeLaunchBundleInputs(
		ctx, client, sshDeployer, rt,
		sourceProjectID, input.ProductionProjectName,
		resolved, gateResult.Checks,
		bundleProjectEnvsFromSource(sourceEnvs),
		input.KeepNonHA,
		bundle.VariantLaunchNew,
	)
	if composeErr != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "compose bundle inputs: " + composeErr.Error(),
		})
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"compose-inputs-failed",
			"Launch bundle input composition failed: "+composeErr.Error()), nil, nil
	}

	// Bundle composition — uses ops.BuildLaunchBundle (Phase C).
	launchBundle, err := ops.BuildLaunchBundle(bundleInputs, classifications)
	if err != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "bundle compose: " + err.Error(),
		})
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"bundle-compose-failed",
			"Launch bundle composition failed: "+err.Error()), nil, nil
	}
	launchBundle.Warnings = append(launchBundle.Warnings, composeWarnings...)
	if len(launchBundle.Errors) > 0 {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "schema validation failed",
		})
		return launchFailedResponse(corpus, topology.BlockerCategorySchema,
			"schema-validation-failed",
			fmt.Sprintf("Import yaml schema validation failed: %v", launchBundle.Errors)), nil, nil
	}

	// Persist initial state pre-mutation — if CreateAndImport panics or
	// the process dies before completion, the state file shows the
	// attempt and the source-snapshot for forensics. Records the first
	// promoted runtime's push hostname + gate-validated RepoURL for
	// forensic correlation; multi-runtime SourceRepoURL is captured in
	// SourceSnapshot via the composer's digest.
	primaryRuntime := firstResolvedRuntime(resolved)
	primaryRepoURL := ""
	if len(gateResult.Checks) > 0 && gateResult.Checks[0] != nil {
		primaryRepoURL = gateResult.Checks[0].MetaRemoteURL
	}
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       sourceProjectID,
		SourceRepoURL:         primaryRepoURL,
		TargetProjectName:     input.ProductionProjectName,
		TargetServiceHostname: primaryRuntime.PushHostname,
		SourceSnapshot:        launchBundle.SourceSnapshot,
		Classifications:       classifications,
		Status:                topology.LaunchStatusLaunching,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		// Non-fatal — proceed with the mutation, but warn.
		launchBundle.Warnings = append(launchBundle.Warnings,
			fmt.Sprintf("write launch state: %v (proceeding; resume after restart may not work)", err))
	}

	// Mutation: CreateAndImportProject. This is the irreversible step.
	result, err := admin.CreateAndImportProject(ctx, launchBundle.ImportYAML, platform.CreateOpts{
		Location: input.Region,
		Tags:     []string{"env:prod", "managed-by:zcp-launch"},
	})
	if err != nil {
		state.Status = topology.LaunchStatusFailed
		state.LastError = formatPlatformErrorForAudit(err)
		_ = writeLaunchState(stateDir, state)
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "create-and-import",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			SourceCommitSHA:   launchBundle.SourceSnapshot.GitCommitSHA,
			SourceYAMLSHA256:  launchBundle.SourceSnapshot.ZeropsYAMLSHA256,
			Classifications:   classifications,
			HAOptOut:          input.KeepNonHA,
			Result:            "failure",
			ErrorMessage:      formatPlatformErrorForAudit(err),
		})
		// Fallback category=Other so the blocker doesn't mislead the
		// agent into a token-regeneration loop when the actual cause is
		// schema validation, project-name collision, or rate limiting.
		// launchFailedFromPlatformError will override with a derived
		// category when pe.Code is in the typed-mapping table.
		return launchFailedFromPlatformError(corpus, err,
			topology.BlockerCategoryOther,
			"create-import-failed",
			"CreateAndImportProject failed: %v"), nil, nil
	}

	// Success — record imported services in state.
	state.TargetProjectID = result.ProjectID

	// A.10: grant launching clientUser ADMIN on the new project so the
	// workflow's subsequent env-presence reads authenticate. Failure
	// here is non-fatal — the project IS created, env-read fallbacks
	// to manual UI verification.
	if err := admin.GrantSelfRole(ctx, result.ProjectID, "ADMIN"); err != nil {
		launchBundle.Warnings = append(launchBundle.Warnings,
			fmt.Sprintf("grant self ADMIN role on %s: %v (env-presence verification disabled; user can read via UI)", result.ProjectID, err))
	}
	state.ImportedServices = make([]importedServiceEntry, 0, len(result.ServiceStacks))
	hasPerServiceError := false
	for _, s := range result.ServiceStacks {
		entry := importedServiceEntry{
			ID:   s.ID,
			Name: s.Name,
		}
		for _, p := range s.Processes {
			entry.ProcessIDs = append(entry.ProcessIDs, p.ID)
		}
		if s.Error != nil {
			entry.ImportError = s.Error.Code + ": " + s.Error.Message
			hasPerServiceError = true
		}
		state.ImportedServices = append(state.ImportedServices, entry)
	}
	if hasPerServiceError {
		state.Status = topology.LaunchStatusFailed
		state.LastError = "one or more service stacks reported import errors"
		_ = writeLaunchState(stateDir, state)
	} else {
		// Poll per-service async processes (build + start) until
		// terminal. Aggregates result into launched / failed.
		state.Status = topology.LaunchStatusLaunching
		_ = writeLaunchState(stateDir, state)

		pollErr := pollImportedServices(ctx, admin, state)
		if pollErr != nil {
			state.Status = topology.LaunchStatusFailed
			state.LastError = formatPlatformErrorForAudit(pollErr)
		} else {
			// Transition to configuring-pipeline before the GetStatus
			// loop so the state file reflects the in-progress phase
			// observable to action="status" resume calls.
			state.Status = topology.LaunchStatusConfiguringPipeline
			_ = writeLaunchState(stateDir, state)
			checkInputs := pipelineCheckInputs{
				SkipPipelineSetup: input.SkipPipelineSetup.Bool(),
				TagRegexOverride:  input.PipelineTagRegex,
				RuntimeHostname:   input.TargetService,
				RepoURL:           source.RepoURL,
			}
			executeLaunchPipelineCheck(ctx, admin, state, checkInputs)
			state.Status = topology.LaunchStatusLaunched
		}
		_ = writeLaunchState(stateDir, state)
	}

	_ = appendAuditLog(stateDir, launchAuditEntry{
		LaunchID:          launchID,
		Action:            "create-and-import",
		SourceProjectID:   sourceProjectID,
		TargetProjectID:   result.ProjectID,
		TargetProjectName: result.ProjectName,
		SourceCommitSHA:   launchBundle.SourceSnapshot.GitCommitSHA,
		SourceYAMLSHA256:  launchBundle.SourceSnapshot.ZeropsYAMLSHA256,
		Classifications:   classifications,
		HAOptOut:          input.KeepNonHA,
		Result:            boolStr(!hasPerServiceError, "success", "failure"),
		ErrorMessage:      stringIf(hasPerServiceError, "one or more service stacks reported import errors"),
	})

	if hasPerServiceError {
		return launchOrphanProjectResponse(state, result.ProjectID), nil, nil
	}
	if state.Status == topology.LaunchStatusFailed {
		_ = corpus // launchFirstDeployFailedResponse is corpus-independent
		return launchFirstDeployFailedResponse(state, result.ProjectID), nil, nil
	}

	return launchLaunchedResponse(corpus, state), nil, nil
}

// pollImportedServices polls every recorded service-stack process to
// terminal state. Aggregates: any FAILED process surfaces as an error
// with the offending process ID + reason; success returns nil. Timeouts
// also surface as errors (caller treats as failed for now; future
// follow-up may distinguish launching-timeout from outright failure).
//
// Uses ops.PollProcess via the ProcessGetter interface — same poll
// machinery as deploy_ssh / deploy_local / scale / manage, including
// the "no progress notification before response" race-avoidance pattern.
// Launch passes nil onProgress because the launch workflow returns a
// summary response per call, not interactive progress.
func pollImportedServices(ctx context.Context, admin platform.ProjectAdminClient, state *launchState) error {
	for _, svc := range state.ImportedServices {
		for _, pid := range svc.ProcessIDs {
			proc, err := ops.PollProcess(ctx, admin, pid, nil)
			if err != nil {
				return fmt.Errorf("poll process %s (service %s): %w", pid, svc.Name, err)
			}
			if proc == nil {
				continue
			}
			// Treat non-FINISHED terminal statuses as failure. Reuse
			// the export workflow's terminal-status semantics: FAILED,
			// CANCELED, etc. all mean "did not succeed".
			if !isProcessSuccess(proc) {
				reason := proc.Status
				if proc.FailReason != nil {
					reason = *proc.FailReason
				}
				return fmt.Errorf("service %s: process %s terminal status %s (%s)", svc.Name, pid, proc.Status, reason)
			}
		}
	}
	return nil
}

// isProcessSuccess returns true for the platform's success-terminal
// process status. FINISHED is the canonical success state.
func isProcessSuccess(proc *platform.Process) bool {
	return proc.Status == processStatusFinished
}

// readAndValidateSourceState runs source-control gate before the
// mutation pipeline:
//  1. Require targetService input (runtime hostname).
//  2. Call readSourceState (SSH/local FS env-aware).
//  3. Validate source zerops.yaml present + contains `setup: prod` block.
//  4. Validate git remote configured.
//
// Each failure path appends an audit log entry and returns a blocker
// response. On success returns the populated LaunchSourceState + nil
// blocker.
//
// Pulled out of executeLaunchMutation to keep that function under
// maintainability-index threshold; the call sites are otherwise
// straight-line.
//
// writeAudit selects whether failure paths persist a `publish-rejected`
// entry to launch-audit-log.json. Mutation callers (executeLaunchMutation,
// executeExistingProjectMutation) pass true — every refusal is a real
// publish attempt that operators want logged. The ready-to-launch
// baseline probe (workflow_launch_production.go:193) passes false; it
// runs on every poll regardless of publish intent, and persisting an
// audit entry per poll would spam the log with refusals the user never
// authored (bug_008).
func readAndValidateSourceState(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	sourceProjectID string,
	stateDir string,
	launchID string,
	writeAudit bool,
) (*LaunchSourceState, *mcp.CallToolResult) {
	auditFail := func(reason string) {
		if !writeAudit {
			return
		}
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      reason,
		})
	}

	if input.TargetService == "" {
		auditFail("TargetService (runtime hostname) required for launch publish")
		return nil, launchSourceControlBlockerResponse(corpus,
			"TargetService input required — launch needs the source-runtime hostname. Pass targetService=<hostname> from the source project.",
		)
	}

	source, err := readSourceState(ctx, client, sshDeployer, rt, sourceProjectID, input.TargetService, "")
	if err != nil {
		auditFail("read source state: " + err.Error())
		return nil, convertError(err, WithRecoveryStatus())
	}
	if strings.TrimSpace(source.ZeropsYAMLBody) == "" {
		auditFail("source zerops.yaml missing")
		return nil, launchSourceControlBlockerResponse(corpus,
			"Source zerops.yaml is missing — write it (with `setup: prod` block), commit, push, then re-call publish.",
		)
	}
	wantSetup := input.ProdSetupNameOverride
	if wantSetup == "" {
		wantSetup = defaultPipelineZeropsYamlSetup
	}
	if !hasSetupNamed(source.ZeropsYAMLBody, wantSetup) {
		availableNames, _ := listSetupNames(source.ZeropsYAMLBody)
		auditFail(fmt.Sprintf("source zerops.yaml lacks `setup: %s` block (found: %s)", wantSetup, strings.Join(availableNames, ", ")))
		// Item #6: derive a concrete proposed block from the source's
		// existing dev/stage setup. Agent applies + tweaks instead of
		// guessing from a generic template. Falls back to the generic
		// message if derivation fails (malformed yaml, no template).
		proposed, derr := deriveProdSetupBlock(source.ZeropsYAMLBody)
		if derr != nil {
			return nil, launchSourceControlBlockerResponse(corpus,
				prodSetupMissingGenericMessage(wantSetup, availableNames),
			)
		}
		return nil, launchSourceControlBlockerResponse(corpus,
			prodSetupGuidanceWithBlock(wantSetup, availableNames, proposed),
		)
	}
	if source.RepoURL == "" {
		auditFail("source git remote not configured")
		return nil, launchSourceControlBlockerResponse(corpus,
			"Source git remote `origin` is empty — configure git remote (see zerops_workflow action=\"git-push-setup\"), then re-call publish.",
		)
	}
	return source, nil
}

// effectiveProdSetupName resolves the setup name the launch composer
// should target in the source zerops.yaml. Honors
// WorkflowInput.ProdSetupNameOverride; falls back to the canonical
// "prod" when unset. Empty input ⇒ canonical default.
func effectiveProdSetupName(input WorkflowInput) string {
	if override := strings.TrimSpace(input.ProdSetupNameOverride); override != "" {
		return override
	}
	return defaultPipelineZeropsYamlSetup
}

// boolStr returns t when cond, f otherwise.
func boolStr(cond bool, t, f string) string {
	if cond {
		return t
	}
	return f
}

// stringIf returns s when cond, "" otherwise.
func stringIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

// launchProductionResponse is the wire shape returned by every status of
// the launch-production workflow.
//
// Top-level status drives agent dispatch; blockers[] + checks[] carry
// structured per-status detail per plan v2 §8.1 design.
//
// LaunchKey intentionally absent — P-LP-1 invariant. No field on this
// struct or its members carries the key.
type launchProductionResponse struct {
	Workflow string                          `json:"workflow"`
	Status   topology.LaunchProductionStatus `json:"status"`
	Phase    workflow.Phase                  `json:"phase"`
	Guidance string                          `json:"guidance"`
	Blockers []topology.Blocker              `json:"blockers,omitempty"`
	Inputs   *launchInputsEcho               `json:"inputs,omitempty"`
	// ProductionProjectID is the new prod project's UUID — surfaced at
	// top level on launched / failed responses (whenever a target project
	// actually got created). Scenario #9 retro flagged this as buried:
	// the only carrier was a deep-link URL inside a pipeline-blocker
	// message, which agents had to regex out for post-launch cleanup
	// (`zcli project delete <id>`). Top-level surface gives every agent
	// the project ID without parsing.
	ProductionProjectID string `json:"productionProjectId,omitempty"`
	// Classifications is the classify-prompt review table — emitted when
	// status is classify-prompt. Per-env rows omit values per P-LP-5.
	Classifications []launchClassifyRow `json:"classifications,omitempty"`
	// SourceContext carries discovery hints about the source dev/stage
	// project: derived production-project name, available runtimes,
	// suggested runtime when source has exactly one. Best-effort —
	// populated on scope/classify/ready responses when source discovery
	// succeeded. The agent SHOULD apply `suggestedTargetName` and
	// `suggestedRuntime` rather than ask the user when populated.
	SourceContext *launchSourceContext `json:"sourceContext,omitempty"`
	// ImportedServices surfaces the per-service create+import outcomes
	// when CreateAndImportProject succeeded at the project level but
	// one or more services reported a per-service error. Without this,
	// the orphan-project blocker is actionable only via "inspect state
	// file" guidance — agents can't read state files directly. Each
	// entry's ImportError captures the API code + message + per-field
	// detail so the agent sees exactly which service rejected and why.
	ImportedServices []importedServiceEntry `json:"importedServices,omitempty"`
}

// launchInputsEcho echoes the scope inputs the workflow saw on the call,
// for agent forensics. Excludes LaunchKey unconditionally.
type launchInputsEcho struct {
	ProductionProjectName string   `json:"productionProjectName,omitempty"`
	Region                string   `json:"region,omitempty"`
	CustomDomain          string   `json:"customDomain,omitempty"`
	KeepNonHA             []string `json:"keepNonHa,omitempty"`
}

// launchClassifyRow is one row of the classify-prompt review table.
// Mirrors export's classify-prompt row shape but explicitly omits raw
// values — agent fetches them separately via zerops_discover and
// re-calls with the populated EnvClassifications.
//
// SuggestedBucket + Rationale carry server-computed hints derived from
// the env key NAME only (never the value, per the no-leak invariant):
// envclass.ClassifyProjectEnv supplies the credentialPattern-vs-default
// bias; topology.IsClassifyInfrastructure overrides for ZCP control-
// plane keys that always bucket as infrastructure. The agent treats
// these as a starting point — the four-bucket atom guidance applies
// when the agent overrides.
type launchClassifyRow struct {
	Key             string                        `json:"key"`
	CurrentBucket   topology.SecretClassification `json:"currentBucket"`
	SuggestedBucket topology.SecretClassification `json:"suggestedBucket,omitempty"`
	Rationale       string                        `json:"rationale,omitempty"`
}

// missingScopeFields returns the names of scope fields that are still
// missing. Empty result = scope complete enough to advance.
//
// Surfaces TargetService here (not late in executeLaunchMutation) so
// the scope-prompt response carries the agent the full picture in one
// pass. sourceContext is read-only: when it carries a SuggestedRuntime
// the agent can fill TargetService on the next call without prompting
// the user (single-runtime case); the field is still listed missing so
// the agent acts on the suggestion rather than silently defaulting.
func missingScopeFields(input WorkflowInput, _ *launchSourceContext) []string {
	var missing []string
	if input.ProductionProjectName == "" {
		missing = append(missing, "productionProjectName")
	}
	if input.TargetService == "" {
		missing = append(missing, "targetService")
	}
	// Region defaults to eu-central at compose-time if empty (per spec-
	// launch-production-platform-spike A.4) — don't require it from
	// the agent.
	return missing
}

// launchScopePromptResponse builds the scope-prompt response.
// SourceContext (when populated) gives the agent the suggested
// productionProjectName + suggested runtime — agent applies without
// asking the user when single-runtime, asks the user when multi-runtime.
func launchScopePromptResponse(corpus []workflow.KnowledgeAtom, input WorkflowInput, missing []string, sourceCtx *launchSourceContext) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-scope-prompt")
	if guidance == "" {
		// Fallback when corpus load left the atom out — shouldn't happen
		// in practice, but better than a silent empty response.
		guidance = "Provide productionProjectName + targetService (runtime hostname). Region defaults to eu-central. Use sourceContext.suggestedTargetName + sourceContext.suggestedRuntime when populated."
	}

	blockers := make([]topology.Blocker, 0, len(missing))
	for _, name := range missing {
		blockers = append(blockers, topology.Blocker{
			ID:       "scope-missing-" + name,
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryScope,
			Message:  fmt.Sprintf("workflow input %q required to advance to classify-prompt", name),
		})
	}

	return jsonResult(launchProductionResponse{
		Workflow:      workflowLaunchProduction,
		Status:        topology.LaunchStatusScopePrompt,
		Phase:         workflow.PhaseLaunchProductionActive,
		Guidance:      guidance,
		Blockers:      blockers,
		Inputs:        echoInputs(input),
		SourceContext: sourceCtx,
	})
}

// launchClassifyPromptResponse builds the classify-prompt response.
// warnBlockers carry warn-only source-control gate failures (e.g.
// build-integration-recommended) so the agent surfaces them alongside
// the classify-prompt without losing visibility — the gate advanced
// past block-severity failures but still wants the agent to ask the
// user whether to set up the warn-level prerequisite.
func launchClassifyPromptResponse(
	corpus []workflow.KnowledgeAtom,
	sourceEnvs []platform.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	sourceCtx *launchSourceContext,
	warnBlockers []topology.Blocker,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-classify-prompt")
	if guidance == "" {
		// Reuse export's classify-prompt guidance shape — same protocol,
		// same buckets. Atom alias planned for Phase E.
		guidance = atomBody(corpus, "export-classify-envs")
	}
	if guidance == "" {
		guidance = "Classify each source env into infrastructure / auto-secret / external-secret / plain-config buckets."
	}

	// Hide envclass-Drop envs (project SYSTEM scope: zeropsSubdomain*,
	// CDN URLs, envIsolation, sshIsolation) from the prompt rows. The
	// agent only sees USER-scoped envs that need its judgment; the
	// target project regenerates SYSTEM envs on import.
	userEnvs := envsForClassifyPrompt(sourceEnvs)
	rows := make([]launchClassifyRow, 0, len(userEnvs))
	for _, env := range userEnvs {
		suggested, rationale := suggestBucketForKey(env)
		rows = append(rows, launchClassifyRow{
			Key:             env.Key,
			CurrentBucket:   classifications[env.Key],
			SuggestedBucket: suggested,
			Rationale:       rationale,
		})
	}

	return jsonResult(launchProductionResponse{
		Workflow:        workflowLaunchProduction,
		Status:          topology.LaunchStatusClassifyPrompt,
		Phase:           workflow.PhaseLaunchProductionActive,
		Guidance:        guidance,
		Classifications: rows,
		SourceContext:   sourceCtx,
		Blockers:        warnBlockers,
	})
}

// launchSourceControlRequiredResponse builds the response for the new
// source-control gate failure status. Carries one or more blockers
// (one per failing check, top-down order) with structured Recovery
// hints pointing the agent at the existing workflow actions that
// resolve each blocker. Stateless — no state file written.
//
// Atom `launch-source-control-required` carries the per-blocker-id
// user-facing guidance; the response includes the rendered atom body
// in `guidance` so the agent has the disambiguation table available
// without an extra lookup.
func launchSourceControlRequiredResponse(
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	sourceCtx *launchSourceContext,
	blockers []topology.Blocker,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-source-control-required")
	if guidance == "" {
		guidance = "Source-side prerequisites for production promotion are not all in place. Resolve each blocker shown below (top-down — agent runs the Recovery call, then re-calls launch-production between each)."
	}
	return jsonResult(launchProductionResponse{
		Workflow:      workflowLaunchProduction,
		Status:        topology.LaunchStatusSourceControlRequired,
		Phase:         workflow.PhaseLaunchProductionActive,
		Guidance:      guidance,
		Blockers:      blockers,
		Inputs:        echoInputs(input),
		SourceContext: sourceCtx,
	})
}

// launchReadyToLaunchResponse builds the ready-to-launch preview. Phase D.1
// emits a minimal preview that echoes inputs + classified-env summary +
// directs the agent to obtain the one-shot launch key. Phase D.2 will
// extend this with the LaunchBundle preview, source-snapshot hashes, and
// cost estimate.
func launchReadyToLaunchResponse(
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	sourceEnvs []platform.ProjectEnvVar,
	sourceCtx *launchSourceContext,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-mutation-key-required")
	if guidance == "" {
		guidance = "Scope and classifications complete. Generate a one-shot Zerops API key (Custom access per project + 'Allow creating projects' toggle ON) and re-call with launchKey set to advance to publish."
	}
	_ = sourceEnvs // Phase D.2 will surface classified-env summary

	return jsonResult(launchProductionResponse{
		Workflow:      workflowLaunchProduction,
		Status:        topology.LaunchStatusReadyToLaunch,
		Phase:         workflow.PhaseLaunchProductionActive,
		Guidance:      guidance,
		Inputs:        echoInputs(input),
		SourceContext: sourceCtx,
	})
}

// launchResumeResponse returns the current state of a launch that has
// already created the target project (idempotent resume). For launched
// state, delegates to launchLaunchedResponse so pipeline blockers
// surface consistently with first-call responses.
func launchResumeResponse(corpus []workflow.KnowledgeAtom, state *launchState) *mcp.CallToolResult {
	if state.Status == topology.LaunchStatusLaunched {
		return launchLaunchedResponse(corpus, state)
	}
	resp := launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   state.Status,
		Phase:    workflow.PhaseLaunchProductionActive,
	}
	switch state.Status {
	case topology.LaunchStatusFailed:
		resp.Guidance = "Prior launch reached failed status. Inspect lastError in state file; retry by clearing the state file and re-calling publish."
		resp.Blockers = []topology.Blocker{{
			ID:       "prior-launch-failed",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryOther,
			Message:  "previous launch failed: " + state.LastError,
		}}
	case topology.LaunchStatusUnset,
		topology.LaunchStatusScopePrompt,
		topology.LaunchStatusSourceControlRequired,
		topology.LaunchStatusClassifyPrompt,
		topology.LaunchStatusReadyToLaunch,
		topology.LaunchStatusLaunching,
		topology.LaunchStatusConfiguringPipeline:
		// Resume found state with a pre-terminal status — surface
		// in-progress guidance; agent re-polls via action="status".
		// source-control-required never persists state files (gate is
		// stateless), so this branch is defensive: any future writer
		// of the status to a state file lands here.
		resp.Guidance = "Launch in progress. State file shows targetProjectID " + state.TargetProjectID + "."
	case topology.LaunchStatusLaunched:
		// Handled by the early-return above; case kept for exhaustiveness.
	}
	return jsonResult(resp)
}

// launchFailedAuthResponse handles the case where NewProjectAdminClient
// fails to authenticate. The error from the constructor never contains
// the key value (per P-LP-1) — we just wrap its message.
func launchFailedAuthResponse(corpus []workflow.KnowledgeAtom, err error) *mcp.CallToolResult {
	return launchFailedFromPlatformError(corpus, err,
		topology.BlockerCategoryAuth,
		"launch-key-invalid",
		"ProjectAdminClient construction failed: %v")
}

// launchSourceControlBlockerResponse fires when source-control fields
// (zerops.yaml body, setup name, target hostname) are not supplied to
// the handler.
func launchSourceControlBlockerResponse(corpus []workflow.KnowledgeAtom, msg string) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-write-prod-setup")
	if guidance == "" {
		guidance = "Append setup: prod block to source zerops.yaml, commit, and push before publish."
	}
	return jsonResult(launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusFailed,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: guidance,
		Blockers: []topology.Blocker{{
			ID:       "source-control-required",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message:  msg,
		}},
	})
}

// launchFailedResponse builds a generic failed-status response with a
// single blocker.
func launchFailedResponse(corpus []workflow.KnowledgeAtom, category topology.BlockerCategory, id, msg string) *mcp.CallToolResult {
	_ = corpus
	return jsonResult(launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusFailed,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: msg,
		Blockers: []topology.Blocker{{
			ID:       id,
			Severity: topology.BlockerSeverityBlock,
			Category: category,
			Message:  msg,
		}},
	})
}

// launchLaunchedResponse builds the terminal-success response with the
// mandatory delete-key atom (P-LP-4 invariant) + post-launch checklist.
// Attaches pipeline blockers when any runtime is unconfigured (P-LP-8).
func launchLaunchedResponse(corpus []workflow.KnowledgeAtom, state *launchState) *mcp.CallToolResult {
	// Concatenate the mandatory delete-key atom + post-checklist for the
	// composite "what you do next" surface. Append the pipeline atom
	// when applicable (configured / configure-dashboard / skipped).
	deleteAtom := atomBody(corpus, "launch-delete-key")
	checklistAtom := atomBody(corpus, "launch-post-checklist")
	pipelineAtomID := pickPipelineAtomID(state)
	pipelineAtom := atomBody(corpus, pipelineAtomID)
	guidance := deleteAtom
	if pipelineAtom != "" {
		if guidance != "" {
			guidance += "\n\n"
		}
		guidance += pipelineAtom
	}
	if checklistAtom != "" {
		if guidance != "" {
			guidance += "\n\n"
		}
		guidance += checklistAtom
	}
	if guidance == "" {
		// Fallback so the mandatory step is never silently dropped.
		guidance = "Production project " + state.TargetProjectID + " launched. DELETE THE LAUNCH-WINDOW KEY NOW in Zerops dashboard → Access Tokens Management."
	}
	return jsonResult(launchProductionResponse{
		Workflow:            workflowLaunchProduction,
		Status:              topology.LaunchStatusLaunched,
		Phase:               workflow.PhaseLaunchProductionActive,
		Guidance:            guidance,
		Blockers:            pipelineBlockers(state),
		ProductionProjectID: state.TargetProjectID,
		Inputs: &launchInputsEcho{
			ProductionProjectName: state.TargetProjectName,
		},
	})
}

// pickPipelineAtomID selects which pipeline-related atom to render in
// the launched response based on the observed pipeline state. Empty
// string when no pipeline check has run yet (mutation pipeline came
// from a pre-Part2 state file).
func pickPipelineAtomID(state *launchState) string {
	if state.PipelineCheckedAt.IsZero() {
		return ""
	}
	if pendingPipelineConfigurations(state) {
		return "launch-pipeline-configure-dashboard"
	}
	if pipelineSkipRecorded(state) {
		return "launch-pipeline-skipped"
	}
	return "launch-pipeline-configured"
}

// echoInputs returns a sanitized snapshot of scope inputs — never
// includes LaunchKey.
func echoInputs(input WorkflowInput) *launchInputsEcho {
	echo := &launchInputsEcho{
		ProductionProjectName: input.ProductionProjectName,
		Region:                input.Region,
		CustomDomain:          input.CustomDomain,
	}
	if len(input.KeepNonHA) > 0 {
		echo.KeepNonHA = append(echo.KeepNonHA, input.KeepNonHA...)
	}
	return echo
}

// atomBody returns the body string for atom with the given ID. Empty
// when not found (caller falls back to inline guidance). Thin wrapper
// over workflow.LookupAtomBody so the discipline test
// (TestNoProductionAtomBodyReads) keeps the direct Body access at the
// parser boundary.
func atomBody(corpus []workflow.KnowledgeAtom, id string) string {
	return workflow.LookupAtomBody(corpus, id)
}
