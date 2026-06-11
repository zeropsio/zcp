package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
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
//   - ProductionProjectName / Region / KeepNonHA — scope
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

// Core-package tier literals (single owner — goconst).
const (
	corePackageSerious = "SERIOUS"
	corePackageLight   = "LIGHT"
)

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
	schemaCache *schema.Cache,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
	sshDeployer ops.SSHDeployer,
	apiHost string,
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

	// Region + core-package validation against the LIVE schema (the
	// region menu derives from the import schema's project.location enum
	// — the single source of truth; the embedded copy went stale at two
	// regions while the platform offered three). Empty inputs are fine
	// (composer defaults SERIOUS / eu-central); invalid values bounce
	// here with the actual menu, before any gate/SSH work.
	regions := launchAvailableRegions(ctx, schemaCache)
	if input.Region != "" && len(regions) > 0 && !slices.Contains(regions, input.Region) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("region %q is not offered by the platform — available: %s", input.Region, strings.Join(regions, ", ")),
			"Pass one of the listed region codes (the menu derives from the live import schema).",
		), WithRecoveryStatus()), nil, nil
	}
	if cp := strings.TrimSpace(input.CorePackage); cp != "" && cp != corePackageSerious && cp != corePackageLight {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("corePackage %q is not a valid core tier — valid: SERIOUS (production default), LIGHT (cheaper shared core)", input.CorePackage),
			"Pass corePackage=\"SERIOUS\" (recommended for production) or corePackage=\"LIGHT\".",
		), WithRecoveryStatus()), nil, nil
	}

	// Source discovery (project name + service list) feeds the
	// scope-prompt SourceContext hint and the classify-prompt env table.
	// Best-effort: errors return nil and the response surfaces the
	// missing fields via blockers without the discovery hint.
	// stateDir + rt enable pair-keyed collapse and ZCP self-filter.
	sourceContext := gatherLaunchSourceContext(ctx, client, projectID, stateDir, rt)

	// Status 1 — scope-prompt: required scope fields incomplete.
	if missing := missingScopeFields(input, sourceContext); len(missing) > 0 {
		return launchScopePromptResponse(corpus, input, missing, sourceContext, regions), nil, nil
	}
	// Multi-runtime contract (F6): a Promotables-only call is valid scope
	// (missingScopeFields accepts it), but several downstream reads
	// (readAndValidateSourceState, state-file echoes) still key on
	// TargetService — normalize it to the FIRST promotable so the
	// multi-runtime path no longer passes scope+classify and then
	// hard-fails on a missing TargetService at publish.
	if input.TargetService == "" && len(input.Promotables) > 0 {
		input.TargetService = strings.TrimSpace(input.Promotables[0].Hostname)
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
	// launch-audit-log.json. Runs over EVERY promoted runtime (LAUNCH-3):
	// a multi-runtime launch must surface source-control-required for
	// runtime B here, before the one-shot launchKey is minted — not at
	// the publish-side gate after the key is spent.
	readSideRuntimes := resolveLaunchRuntimes(stateDir, input)
	gateChecks, gateBlockers, gateErr := runReadSideSourceControlGate(
		ctx, client, sshDeployer, rt, stateDir, projectID,
		readSideRuntimes, input.SkipBuildIntegration,
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
	_ = gateChecks

	// Read source project envs (needed for both classify-prompt and
	// publish-time bundle composition). Layer-2 entry point via
	// inventory.FetchProjectEnvs keeps the SDK shape (Type/Sensitive/
	// Editable) so envclass (Layer 3) can drop SYSTEM-scoped envs at
	// the handler before they reach the prompt or the composer.
	sourceEnvs, err := inventory.FetchProjectEnvs(ctx, client, projectID)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	// Reject typo'd classification buckets at the boundary before they reach
	// the composer's verbatim-on-unknown default (B12).
	if err := validateEnvClassifications(input.EnvClassifications); err != nil {
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
					`Clean up + retry: action="reset" workflow="launch-production" productionProjectName="`+existing.TargetProjectName+`" — the launch token resolves from the staged `+ops.LaunchTokenEnvKey+` secret (no launchKey re-send), and the diagnose-before-destruct refusal lists exactly what gets deleted: the orphan production project AND the state file. Then re-run start with the SAME productionProjectName. (When the staged secret is gone, pass launchKey=<the launch token> explicitly; without any token, reset only clears local state and leaves the billable orphan for manual dashboard deletion.)`,
				), WithRecoveryStatus()), nil, nil
			}
			// launched / launching-with-project / configuring-pipeline —
			// current idempotent resume. The pipeline re-check resolves
			// the launch-window token from the staged secret when the
			// call carries no launchKey (single-token lifecycle T2) —
			// the agent never re-sends the value.
			if existing.Status == topology.LaunchStatusLaunched &&
				(pendingPipelineConfigurations(existing) || (input.SkipPipelineSetup.Bool() && !pipelineSkipRecorded(existing))) {
				resumeKey := input.LaunchKey
				if resumeKey == "" {
					staged, stageErr := launchKeyFromStage(ctx, client, projectID, existing)
					if stageErr == nil {
						resumeKey = staged
					}
				}
				if resumeKey != "" {
					return executeLaunchPipelineResume(ctx, resumeKey, input, corpus, stateDir, existing, apiHost)
				}
			}
			return launchResumeResponse(corpus, existing, stateDir), nil, nil
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
	var readyBundle *ops.LaunchBundle
	var readyBundleInputs ops.LaunchBundleInputs
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
				ck, ckBlockers, ckErr := validateLaunchSourceControl(ctx, client, sshDeployer, rt, stateDir, projectID, r.ChoiceHostname, input.SkipBuildIntegration)
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
					managedDepsExclusions(input.ManagedDeps),
					input.RuntimeScaling,
					bundle.VariantLaunchNew,
				)
				bundleInputs.CorePackage = input.CorePackage
				bundleInputs.Location = input.Region
				if composeErr == nil {
					if b, bundleErr := ops.BuildLaunchBundle(bundleInputs, classifications); bundleErr == nil {
						current = b.SourceSnapshot
						haveCurrent = true
						readyBundle = b
						readyBundleInputs = bundleInputs
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
		evidenceSources := make([]readinessEvidenceSource, 0, len(resolvedForBaseline))
		for _, r := range resolvedForBaseline {
			evidenceSources = append(evidenceSources, readinessEvidenceSource{
				PushHostname: r.PushHostname,
				SetupName:    r.SetupName,
			})
		}
		return launchReadyToLaunchResponse(corpus, input, sourceEnvs, sourceContext,
			runReadinessRubric(readyBundle, readyBundleInputs, readinessEvidenceInput{StateDir: stateDir, Sources: evidenceSources}),
			launchBundlePreviewFrom(readyBundle, readyBundleInputs)), nil, nil
	}

	// Existing-project mutation path takes priority — the user has
	// explicitly identified the target project via ExistingProjectID.
	if hasExistingPath {
		return executeExistingProjectMutation(ctx, projectID, client, sshDeployer, rt, input, sourceEnvs, classifications, corpus, stateDir, launchID, apiHost)
	}

	// Mutation pipeline (new-project path) — LaunchKey supplied,
	// no existing target, baseline matches current.
	return executeLaunchMutation(ctx, projectID, client, sshDeployer, rt, input, sourceEnvs, classifications, corpus, stateDir, launchID, apiHost)
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
			"or abandon this launch via zerops_workflow action=\"reset\" "+
			"workflow=\"launch-production\" productionProjectName=\"<name>\" "+
			"(the supported reset path — it clears the state file after the "+
			"diagnose-before-destruct ack) and restart the workflow to "+
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
// resolved launch-window token (staged secret or explicit launchKey —
// the caller resolves; in-request only), runs executeLaunchPipelineCheck,
// writes the updated state, and returns the launched response with
// refreshed blockers. ZCP never PUTs (Path B); this is GetStatus-only.
func executeLaunchPipelineResume(
	ctx context.Context,
	launchKey string,
	input WorkflowInput,
	corpus []workflow.KnowledgeAtom,
	stateDir string,
	state *launchState,
	apiHost string,
) (*mcp.CallToolResult, any, error) {
	admin, err := projectAdminClientFactory(launchKey, apiHost)
	if err != nil {
		return launchFailedAuthResponse(corpus, err), nil, nil
	}
	defer admin.Close()

	checkInputs := pipelineCheckInputs{
		SkipPipelineSetup: input.SkipPipelineSetup.Bool(),
		TagRegexOverride:  input.PipelineTagRegex,
		Runtimes:          pipelineRuntimesForState(stateDir, input, state),
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
	return launchLaunchedResponse(corpus, state, stateDir), nil, nil
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
//nolint:maintidx // linear state-machine mutation pipeline (gate → compose → create-import → finalize → audit → respond); the maintainability index is dominated by Halstead volume (sequential steps + audit field plumbing), not nested control flow. The post-import tail is already extracted (finalizeImportedRuntimes).
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
	apiHost string,
) (*mcp.CallToolResult, any, error) {
	admin, err := projectAdminClientFactory(input.LaunchKey, apiHost)
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
		managedDepsExclusions(input.ManagedDeps),
		input.RuntimeScaling,
		bundle.VariantLaunchNew,
	)
	bundleInputs.CorePackage = input.CorePackage
	bundleInputs.Location = input.Region
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

	// Stage the launch token as the single working copy BEFORE the
	// irreversible create (single-token lifecycle T1): every later
	// launch-window operation reads the staged secret instead of
	// re-asking for the value, and the prodCD conveyance reads it over
	// ssh. A staging failure aborts here — no project, no state, safe
	// retry with the same launchKey.
	primaryRuntime := firstResolvedRuntime(resolved)
	if stageErr := stageLaunchToken(ctx, client, sourceProjectID, primaryRuntime.PushHostname, input.LaunchKey); stageErr != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "stage launch token: " + stageErr.Error(),
		})
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"launch-token-stage-failed",
			launchTokenStageFailedMessage(stageErr, primaryRuntime.PushHostname)), nil, nil
	}

	// Persist initial state pre-mutation — if CreateAndImport panics or
	// the process dies before completion, the state file shows the
	// attempt and the source-snapshot for forensics. Records the first
	// promoted runtime's push hostname + gate-validated RepoURL for
	// forensic correlation; multi-runtime SourceRepoURL is captured in
	// SourceSnapshot via the composer's digest.
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
		// Persist the prod-side runtime identities (one per promoted
		// runtime) so the pipeline check matches imported services by
		// prod hostname, and a resume can re-run the check (LAUNCH-1).
		RuntimeProds: runtimeProdsFromBundleInputs(bundleInputs),
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		// Non-fatal — proceed with the mutation, but warn.
		launchBundle.Warnings = append(launchBundle.Warnings,
			fmt.Sprintf("write launch state: %v (proceeding; resume after restart may not work)", err))
	}

	// Mutation: CreateAndImportProject. This is the irreversible step.
	result, err := admin.CreateAndImportProject(ctx, launchBundle.ImportYAML)
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
	// Persist the accumulated bundle warnings (compose notes, unreferenced
	// managed deps, grant-role/state-write fallbacks) so the launched and
	// resume responses surface them instead of dropping them.
	state.Warnings = launchBundle.Warnings
	state.ImportedServices = make([]importedServiceEntry, 0, len(result.ServiceStacks))
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
		}
		state.ImportedServices = append(state.ImportedServices, entry)
	}
	_ = writeLaunchState(stateDir, state) // pre-finalize snapshot for resume

	// Shared post-import tail: per-service error detect → poll → pipeline
	// check. Same helper the existing-project path uses (LAUNCH-2 parity).
	outcome := finalizeImportedRuntimes(ctx, admin, admin, state, pipelineCheckInputs{
		SkipPipelineSetup: input.SkipPipelineSetup.Bool(),
		TagRegexOverride:  input.PipelineTagRegex,
		Runtimes:          state.RuntimeProds,
	})
	_ = writeLaunchState(stateDir, state)

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
		Result:            boolStr(outcome == launchFinalizeLaunched, "success", "failure"),
		ErrorMessage:      stringIf(outcome != launchFinalizeLaunched, state.LastError),
	})

	switch outcome {
	case launchFinalizeImportError:
		return launchOrphanProjectResponse(state, result.ProjectID), nil, nil
	case launchFinalizePollFailed:
		_ = corpus // launchFirstDeployFailedResponse is corpus-independent
		return launchFirstDeployFailedResponse(state, result.ProjectID), nil, nil
	case launchFinalizeLaunched:
		recordProdLaunchBackRefs(stateDir, state, resolved)
		return launchLaunchedResponse(corpus, state, stateDir), nil, nil
	}
	// Unreachable: launchFinalizeOutcome is exhaustively handled above.
	return launchLaunchedResponse(corpus, state, stateDir), nil, nil
}

// recordProdLaunchBackRefs writes the post-launch back-reference onto
// every promoted runtime's source meta (F4 ledger completion): the prod
// project ID/name + prod hostname + launch time, append-if-new keyed on
// (ProdProjectID, ProdHostname), plus the ProdSetupName identity the
// promotion used. Develop-side surfaces read it to say "this stage feeds
// production X"; a later session (or another operator) at least knows
// the production project exists. Non-fatal best-effort — the launch
// already succeeded; a meta-write failure must not fail the response.
func recordProdLaunchBackRefs(stateDir string, state *launchState, runtimes []resolvedLaunchRuntime) {
	if state == nil || state.TargetProjectID == "" {
		return
	}
	launchedAt := time.Now().UTC().Format(time.RFC3339)
	for _, r := range runtimes {
		ref := workflow.ProdLaunchRef{
			ProdProjectID:   state.TargetProjectID,
			ProdProjectName: state.TargetProjectName,
			ProdHostname:    r.ProdHostname,
			LaunchedAt:      launchedAt,
		}
		setup := r.SetupName
		_ = workflow.UpdateServiceMeta(stateDir, r.PushHostname, func(m *workflow.ServiceMeta) error {
			for _, existing := range m.ProdLaunches {
				if existing.ProdProjectID == ref.ProdProjectID && existing.ProdHostname == ref.ProdHostname {
					return nil // already recorded — idempotent resume
				}
			}
			m.ProdLaunches = append(m.ProdLaunches, ref)
			if setup != "" {
				m.ProdSetupName = setup
			}
			return nil
		})
	}
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
func pollImportedServices(ctx context.Context, admin ops.ProcessGetter, state *launchState) error {
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

// launchFinalizeOutcome classifies the result of the shared post-import
// tail so each mutation path renders the right response + audit.
type launchFinalizeOutcome int

const (
	launchFinalizeLaunched    launchFinalizeOutcome = iota // all services imported + deployed
	launchFinalizeImportError                              // a per-service ImportError → orphaned project
	launchFinalizePollFailed                               // import OK but a build/start process failed
)

// finalizeImportedRuntimes runs the post-import tail shared by the new-
// and existing-project mutation paths. LAUNCH-2: the existing-project
// path used to skip this entirely — set Launched + audit success even on
// a per-service ImportError, and never poll the build/start processes.
// It detects per-service import errors, polls every imported process to
// terminal, and (on success) runs the per-runtime pipeline-config check.
// Mutates state.Status / state.LastError / state.PipelineConfigurations;
// the caller persists state, writes the audit entry (Result derived from
// the outcome), and selects the response.
func finalizeImportedRuntimes(
	ctx context.Context,
	prober ops.ProcessGetter,
	integ pipelineIntegrationReader,
	state *launchState,
	pipelineInputs pipelineCheckInputs,
) launchFinalizeOutcome {
	for _, e := range state.ImportedServices {
		if e.ImportError != "" {
			state.Status = topology.LaunchStatusFailed
			state.LastError = "one or more service stacks reported import errors"
			return launchFinalizeImportError
		}
	}
	state.Status = topology.LaunchStatusLaunching
	if err := pollImportedServices(ctx, prober, state); err != nil {
		state.Status = topology.LaunchStatusFailed
		state.LastError = formatPlatformErrorForAudit(err)
		return launchFinalizePollFailed
	}
	state.Status = topology.LaunchStatusConfiguringPipeline
	executeLaunchPipelineCheck(ctx, integ, state, pipelineInputs)
	state.Status = topology.LaunchStatusLaunched
	return launchFinalizeLaunched
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
			"Source zerops.yaml is missing — write it (with the production setup block), commit, push, then re-call the launch workflow.",
		)
	}
	wantSetup := launchTargetSetupName(stateDir, input.TargetService, input)
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
			"Source git remote `origin` is empty — configure git remote (see zerops_workflow action=\"git-push-setup\"), then re-call the launch workflow.",
		)
	}
	return source, nil
}

// launchTargetSetupName runs the same source-meta cascade used by the
// source-control gate (override → meta.StageSetupName →
// meta.PrimarySetupName) and falls back to the legacy "prod" default
// when both are absent. Shared by the pre-launch source gate and the
// pipelineCheckInputs construction sites so the integration
// recommendation announced to the user matches what the launch
// composer actually wrote into the production import yaml.
//
// Plan §P5 deferred: the "prod" tail keeps existing tests + flow-eval
// scenarios working until test fixtures seed PrimarySetupName /
// StageSetupName on source metas.
func launchTargetSetupName(stateDir, targetHostname string, input WorkflowInput) string {
	if v := strings.TrimSpace(input.ProdSetupNameOverride); v != "" {
		return v
	}
	if meta, _ := workflow.FindServiceMeta(stateDir, targetHostname); meta != nil {
		switch {
		case meta.ProdSetupName != "":
			return meta.ProdSetupName
		case meta.StageSetupName != "":
			return meta.StageSetupName
		case meta.PrimarySetupName != "":
			return meta.PrimarySetupName
		}
	}
	return setupNameProd
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
	// AvailableRegions is the live region menu (import schema
	// project.location enum) — attached on scope-prompt so the agent
	// offers the user the REAL choice set instead of a hardcoded subset.
	AvailableRegions []string `json:"availableRegions,omitempty"`
	// ReadinessChecks carries the prod-readiness rubric on ready-to-launch
	// (informed consent: the user sees what will be created BEFORE being
	// asked to mint the irreversible launchKey).
	ReadinessChecks []readinessCheck `json:"readinessChecks,omitempty"`
	// BundlePreview is the compact what-will-be-created summary on
	// ready-to-launch. Deliberately NOT the full import yaml (response-size
	// discipline) — services + tiers + env counts + warnings.
	BundlePreview *launchBundlePreview `json:"bundlePreview,omitempty"`
	// ProdCD carries the tag→prod Actions track when the source declared
	// integration=actions (spec-git-delivery-target FP-5).
	ProdCD map[string]any `json:"prodCd,omitempty"`
	// FirstRelease is the pipeline-first truth block on the launched
	// response: runtimes are ACTIVE with EMPTY containers
	// (startWithoutCode); the app arrives with the first release.
	FirstRelease *launchFirstReleaseBlock `json:"firstRelease,omitempty"`
	// StageRecommendation is the no-stage consent block on scope-prompt
	// (gap plan P1.2): recommend-create-stage-first with the prefilled
	// expansion call + the proceed-with-ack alternative.
	StageRecommendation map[string]any `json:"stageRecommendation,omitempty"`
	// CredentialsRequired is the typed wait-for-user ask block attached
	// when a blocker chains into a credential-bearing call (LP-2).
	CredentialsRequired []launchCredentialAsk `json:"credentialsRequired,omitempty"`
	// PipelineSummary is the per-runtime CD observation on the launched/
	// resume responses (F43/J3): configured | not-configured | skipped,
	// derived from state.PipelineConfigurations. Sorted by hostname.
	PipelineSummary []launchPipelineSummaryEntry `json:"pipelineSummary,omitempty"`
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
	// `promotionHeadline` rather than ask the user when populated.
	SourceContext *launchSourceContext `json:"sourceContext,omitempty"`
	// ImportedServices surfaces the per-service create+import outcomes
	// when CreateAndImportProject succeeded at the project level but
	// one or more services reported a per-service error. Without this,
	// the orphan-project blocker is actionable only via "inspect state
	// file" guidance — agents can't read state files directly. Each
	// entry's ImportError captures the API code + message + per-field
	// detail so the agent sees exactly which service rejected and why.
	ImportedServices []importedServiceEntry `json:"importedServices,omitempty"`
	// Warnings surfaces non-fatal launch-time advisories from bundle
	// composition (e.g. a promoted managed dep no runtime references via
	// ${host_*}, which is unreachable under the default service isolation).
	// Carried from launchState.Warnings so the success/resume responses
	// don't silently drop them.
	Warnings []string `json:"warnings,omitempty"`
}

// launchInputsEcho echoes the scope inputs the workflow saw on the call,
// for agent forensics. Excludes LaunchKey unconditionally.
type launchInputsEcho struct {
	ProductionProjectName string   `json:"productionProjectName,omitempty"`
	Region                string   `json:"region,omitempty"`
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
// pass. sourceContext is read-only: when it carries a PromotionHeadline
// the agent can fill TargetService on the next call without prompting
// the user (single-runtime case); the field is still listed missing so
// the agent acts on the suggestion rather than silently defaulting.
func missingScopeFields(input WorkflowInput, _ *launchSourceContext) []string {
	var missing []string
	if input.ProductionProjectName == "" {
		missing = append(missing, "productionProjectName")
	}
	// Accept either a single targetService OR a Promotables[] list
	// (multi-runtime launch). Only flag the field missing when BOTH are
	// absent (LAUNCH-3/4: a Promotables-only call is a valid scope).
	if input.TargetService == "" && len(input.Promotables) == 0 {
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
func launchScopePromptResponse(corpus []workflow.KnowledgeAtom, input WorkflowInput, missing []string, sourceCtx *launchSourceContext, availableRegions []string) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-scope-prompt")
	if guidance == "" {
		// Fallback when corpus load left the atom out — shouldn't happen
		// in practice, but better than a silent empty response.
		guidance = "Provide productionProjectName + targetService (runtime hostname). Region defaults to eu-central. Use sourceContext.suggestedTargetName + sourceContext.promotionHeadline when populated."
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
		Workflow:            workflowLaunchProduction,
		Status:              topology.LaunchStatusScopePrompt,
		Phase:               workflow.PhaseLaunchProductionActive,
		Guidance:            guidance,
		Blockers:            blockers,
		Inputs:              echoInputs(input),
		SourceContext:       sourceCtx,
		AvailableRegions:    availableRegions,
		StageRecommendation: stageRecommendationBlock(input, sourceCtx),
	})
}

// stageRecommendationBlock surfaces the no-stage consent question
// (spec-git-delivery-target / gap plan P1.2, Karel's area 1): when the
// chosen (or only) runtime has no stage half, recommend creating one —
// the stage half is the structurally safe topology (its push source is
// never CI-replaced, and its verified setup becomes the promotion
// basis). Dismissible per the no-new-gates ledger: choices carry the
// prefilled expansion call AND the proceed-with-ack re-call; the ack
// (skipStageRecommendation) self-extinguishes the block.
func stageRecommendationBlock(input WorkflowInput, sourceCtx *launchSourceContext) map[string]any {
	if input.SkipStageRecommendation.Bool() || sourceCtx == nil {
		return nil
	}
	// Resolve the runtime the launch is about: explicit targetService, or
	// the single available runtime.
	var choice *runtimeChoice
	for i := range sourceCtx.AvailableRuntimes {
		rt := &sourceCtx.AvailableRuntimes[i]
		if input.TargetService != "" && (rt.Hostname == input.TargetService || rt.DevHostname == input.TargetService) {
			choice = rt
			break
		}
	}
	if choice == nil && len(sourceCtx.AvailableRuntimes) == 1 {
		choice = &sourceCtx.AvailableRuntimes[0]
	}
	if choice == nil {
		return nil
	}
	// Pairs already have the verified-stage basis; only no-stage
	// push-capable shapes get the recommendation.
	if choice.Mode != string(topology.PlanModeSimple) && choice.Mode != string(topology.PlanModeLocalOnly) {
		return nil
	}
	return map[string]any{
		"recommendation": fmt.Sprintf("Runtime %q has NO stage half — production would promote the live development service directly. Recommended: create a stage half first, verify the app there, and promote from that verified basis. Ask the user.", choice.Hostname),
		"why":            "A stage half is the structurally safe promotion basis: its push source is never replaced by CI builds, the stage setup is the validated last-known-good the production setup derives from, and verify evidence accumulates on a runtime that mirrors production.",
		"choices": []map[string]any{
			{
				"label": "create stage first (recommended)",
				"call": map[string]any{
					"tool": "zerops_workflow",
					"args": map[string]string{"action": "start", "workflow": "bootstrap", "route": "adopt"},
				},
				"note": fmt.Sprintf("Plan target: isExisting=true + bootstrapMode=\"standard\" + hostname %q + an explicit stageHostname (see the develop-mode-expansion atom). Then develop → push → verify on stage, and re-enter launch.", choice.Hostname),
			},
			{
				"label": "proceed with direct promotion",
				"call": map[string]any{
					"tool": "zerops_workflow",
					"args": map[string]string{"action": "start", "workflow": "launch-production", "skipStageRecommendation": "true"},
				},
				"note": "Carry ALL previously-accepted inputs forward plus skipStageRecommendation=true; the recommendation will not re-surface.",
			},
		},
	}
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
		guidance = "Classify each source env into infrastructure / auto-secret / external-secret / plain-config / exclude buckets."
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
	resp := launchProductionResponse{
		Workflow:      workflowLaunchProduction,
		Status:        topology.LaunchStatusSourceControlRequired,
		Phase:         workflow.PhaseLaunchProductionActive,
		Guidance:      guidance,
		Blockers:      blockers,
		Inputs:        echoInputs(input),
		SourceContext: sourceCtx,
	}
	// LP-2 proactive credential discipline: when any blocker chains into
	// git-push-setup, the repo URL + PAT are USER-OWNED inputs. The typed
	// block is the wait-for-user contract (parity with the launchKey ask
	// and the error-side credential contract in errwire) — agents
	// fabricated tokens after generic instructions in 4 observed runs.
	for _, b := range blockers {
		if b.Recovery != nil && b.Recovery.Action == actionGitPushSetup {
			resp.CredentialsRequired = []launchCredentialAsk{
				{
					Name:        "remoteUrl",
					Label:       "Git remote URL (HTTPS)",
					FromUser:    true,
					Description: "The repository production will clone from. " + credentialUserOwnedAskContract,
				},
				{
					Name:        "gitToken",
					Label:       "Fine-grained PAT for the repo",
					Secret:      true,
					FromUser:    true,
					Description: "Contents: Read and write on the single target repo (add Secrets+Workflows for integration=actions). " + credentialUserOwnedAskContract,
				},
			}
			break
		}
	}
	return jsonResult(resp)
}

// credentialUserOwnedAskContract is the PROACTIVE wait-for-user
// discipline on credential asks (blocker side) — sibling of errwire's
// credentialUserOwnedContract (error side). One sentence, same intent:
// the value comes FROM THE USER, never from the model.
const credentialUserOwnedAskContract = "This value is user-owned: ask the user (AskUserQuestion) and WAIT for their answer — NEVER invent, guess, or reuse a value from another repo."

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
	checks []readinessCheck,
	preview *launchBundlePreview,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-mutation-key-required")
	if guidance == "" {
		guidance = "Scope and classifications complete. Have the user generate the launch integration token (Custom access per project + 'Allow creating projects' toggle ON) and re-call with launchKey set to advance to publish — the value is passed ONCE; later launch-window calls read the staged secret."
	}
	_ = sourceEnvs // env summary folded into BundlePreview

	return jsonResult(launchProductionResponse{
		Workflow:        workflowLaunchProduction,
		Status:          topology.LaunchStatusReadyToLaunch,
		Phase:           workflow.PhaseLaunchProductionActive,
		Guidance:        guidance,
		Inputs:          echoInputs(input),
		SourceContext:   sourceCtx,
		ReadinessChecks: checks,
		BundlePreview:   preview,
	})
}

// launchBundlePreview is the compact informed-consent summary attached to
// ready-to-launch: WHAT will be created when the launchKey is spent.
// Compact by design — full yaml stays out of the response (the
// 23.6KB-classify-turn lesson); the operator can inspect details in the
// dashboard after create.
type launchBundlePreview struct {
	TargetProjectName string                 `json:"targetProjectName"`
	CorePackage       string                 `json:"corePackage"`
	Location          string                 `json:"location"`
	Services          []launchPreviewService `json:"services"`
	ProjectEnvCount   int                    `json:"projectEnvCount"`
	// ManagedDepHint names the exact exclusion re-call when any managed
	// dep shows referenced=false — the decision belongs BEFORE the
	// launchKey is spent (the prod.txt session learned about an
	// unwanted dep from the invoice).
	ManagedDepHint string `json:"managedDepHint,omitempty"`
	// SetupProvenanceHint raises the confirm question when a runtime's
	// production setup resolved from the dev iteration setup or the
	// legacy default — the operator must see WHICH zerops.yaml recipe
	// production will build with before spending the launchKey.
	SetupProvenanceHint string   `json:"setupProvenanceHint,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

type launchPreviewService struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type"`
	Role     string `json:"role"` // runtime | managed
	Mode     string `json:"mode,omitempty"`
	Setup    string `json:"setup,omitempty"`
	// SetupProvenance (runtime role only) names which cascade source
	// produced Setup: override | recorded-prod | stage-setup |
	// dev-setup-promoted | default-prod.
	SetupProvenance string `json:"setupProvenance,omitempty"`
	// Referenced (managed role only) reports whether anything in the
	// bundle references ${<host>_*} — an unreferenced dep is unreachable
	// from the promoted runtimes under the default service isolation.
	// Nil for runtimes (the concept doesn't apply).
	Referenced *bool `json:"referenced,omitempty"`
	// Containers renders the production container range ("2", "2–3",
	// "1 (consented)") — the consent screen used to hide the count
	// entirely (gap plan P2.4; the prod.txt session learned about the
	// 2-container floor from the invoice).
	Containers string `json:"containers,omitempty"`
}

// launchBundlePreviewFrom derives the preview from the already-composed
// baseline bundle. Nil bundle (source not yet readable) yields nil — the
// response degrades to today's minimal shape.
func launchBundlePreviewFrom(b *ops.LaunchBundle, inputs ops.LaunchBundleInputs) *launchBundlePreview {
	if b == nil {
		return nil
	}
	corePackage := strings.TrimSpace(inputs.CorePackage)
	if corePackage == "" {
		corePackage = corePackageSerious
	}
	location := strings.TrimSpace(inputs.Location)
	if location == "" {
		location = "eu-central"
	}
	preview := &launchBundlePreview{
		TargetProjectName: inputs.TargetProjectName,
		CorePackage:       corePackage,
		Location:          location,
		ProjectEnvCount:   len(inputs.ProjectEnvs),
		Warnings:          b.Warnings,
	}
	keepNonHA := make(map[string]bool, len(inputs.KeepNonHA))
	for _, h := range inputs.KeepNonHA {
		keepNonHA[h] = true
	}
	var unconfirmedSetups []string
	for _, r := range inputs.Runtimes {
		preview.Services = append(preview.Services, launchPreviewService{
			Hostname:        r.ProdHostname,
			Type:            r.ServiceType,
			Role:            "runtime",
			Mode:            "NON_HA",
			Setup:           r.SetupName,
			SetupProvenance: r.SetupProvenance,
			Containers:      previewContainers(r),
		})
		if r.SetupProvenance == setupProvenanceDevPromoted || r.SetupProvenance == setupProvenanceDefault {
			unconfirmedSetups = append(unconfirmedSetups, fmt.Sprintf("%s (setup %q, %s)", r.ProdHostname, r.SetupName, r.SetupProvenance))
		}
	}
	if len(unconfirmedSetups) > 0 {
		preview.SetupProvenanceHint = fmt.Sprintf(
			"Production build recipe was NOT explicitly chosen for: %s. dev-setup-promoted means the dev iteration setup becomes the production build; default-prod means nothing was recorded and the literal \"prod\" setup name is assumed. Confirm with the user which zerops.yaml setup production should build with — to override, re-call with prodSetupNameOverride=<name> (or per-promotable promotables[].prodSetupNameOverride) before supplying the launchKey.",
			strings.Join(unconfirmedSetups, ", "))
	}
	referenced := make(map[string]bool, len(b.ManagedDeps))
	for _, d := range b.ManagedDeps {
		referenced[d.Hostname] = d.Referenced
	}
	var unreferenced []string
	for _, m := range inputs.ManagedServices {
		mode := "HA"
		if keepNonHA[m.Hostname] {
			mode = "NON_HA"
		}
		entry := launchPreviewService{
			Hostname: m.Hostname,
			Type:     m.Type,
			Role:     "managed",
			Mode:     mode,
		}
		if ref, ok := referenced[m.Hostname]; ok {
			entry.Referenced = &ref
			if !ref {
				unreferenced = append(unreferenced, m.Hostname)
			}
		}
		preview.Services = append(preview.Services, entry)
	}
	if len(unreferenced) > 0 {
		hints := make([]string, 0, len(unreferenced))
		for _, h := range unreferenced {
			hints = append(hints, fmt.Sprintf("managedDeps={%q:%q}", h, "exclude"))
		}
		preview.ManagedDepHint = fmt.Sprintf(
			"Managed deps marked referenced=false are unreachable from the promoted runtimes — confirm with the user whether to drop them. To exclude, re-call with %s before supplying the launchKey; omitted deps stay included. To keep one, wire a ${<host>_*} reference into a runtime's run.envVariables first.",
			strings.Join(hints, " "))
	}
	return preview
}

// launchResumeResponse returns the current state of a launch that has
// already created the target project (idempotent resume). For launched
// state, delegates to launchLaunchedResponse so pipeline blockers
// surface consistently with first-call responses.
func launchResumeResponse(corpus []workflow.KnowledgeAtom, state *launchState, stateDir string) *mcp.CallToolResult {
	if state.Status == topology.LaunchStatusLaunched {
		return launchLaunchedResponse(corpus, state, stateDir)
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
		topology.LaunchStatusExistingProjectConflictPrompt,
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
// Attaches pipeline blockers when any runtime is unconfigured (P-LP-8)
// and the firstRelease truth block (pipeline-first: runtimes are ACTIVE
// with empty containers until the first release).
func launchLaunchedResponse(corpus []workflow.KnowledgeAtom, state *launchState, stateDir string) *mcp.CallToolResult {
	family := launchDeliveryFamily(stateDir, state)
	// Concatenate the mandatory delete-key atom + post-checklist for the
	// composite "what you do next" surface. Append the pipeline atom
	// when applicable (configured / configure-dashboard / skipped).
	deleteAtom := atomBody(corpus, "launch-delete-key")
	checklistAtom := atomBody(corpus, "launch-post-checklist")
	pipelineAtomID := pickPipelineAtomID(state, family)
	pipelineAtom := atomBody(corpus, pipelineAtomID)
	guidance := deleteAtom
	// J5: the delete-key step and the keep-the-key pipeline resume used
	// to contradict each other in one payload. The key-deletion atom
	// stays mandatory (P-LP-4), but with delivery wiring pending the
	// response leads with the explicit ordering: wire delivery + ship the
	// first release first, THEN revoke.
	switch {
	case family == topology.BuildIntegrationActions:
		guidance = "ORDER OF OPERATIONS: production is EMPTY until the first release — wire the actions track (prodCd block: staged-secret conveyance + workflow file), push the first release tag, and watch it reach the production runtimes (prod-ops) BEFORE closing the launch window. The confirm-production step below applies AFTER production is verified working.\n\n" + guidance
	case pendingPipelineConfigurations(state):
		guidance = "ORDER OF OPERATIONS: production is EMPTY until the first release — configure the pipeline (or explicitly skip it), push the first release tag, and watch it reach the production runtimes (prod-ops) BEFORE closing the launch window. The confirm-production step below applies AFTER production is verified working.\n\n" + guidance
	}
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
		guidance = "Production project " + state.TargetProjectID + " launched. Once production is verified fully functional, close the launch window: zerops_workflow action=\"confirm-production\" productionProjectName=\"" + state.TargetProjectName + "\" confirmFunctional=true — this deletes the staged " + ops.LaunchTokenEnvKey + " secret."
	}
	return jsonResult(launchProductionResponse{
		Workflow:            workflowLaunchProduction,
		Status:              topology.LaunchStatusLaunched,
		Phase:               workflow.PhaseLaunchProductionActive,
		Guidance:            guidance,
		Blockers:            pipelineBlockers(state, family),
		ProductionProjectID: state.TargetProjectID,
		Warnings:            state.Warnings,
		PipelineSummary:     pipelineSummaryFrom(state),
		ImportedServices:    state.ImportedServices,
		ProdCD:              prodCDActionsBlock(stateDir, state),
		FirstRelease:        composeFirstReleaseBlock(family, state),
		Inputs: &launchInputsEcho{
			ProductionProjectName: state.TargetProjectName,
		},
	})
}

// launchDeliveryFamily resolves the production delivery family from the
// source pair's declared BuildIntegration — the SINGLE owner of that
// read on the launched surface (prodCD block, pipeline atom pick,
// pipeline blockers, and the firstRelease block all key on it).
// Multi-runtime launches use the target service's pair (the same
// simplification prodCDActionsBlock always had). Empty/unknown → none.
func launchDeliveryFamily(stateDir string, state *launchState) topology.BuildIntegration {
	if stateDir == "" || state == nil || state.TargetServiceHostname == "" {
		return topology.BuildIntegrationNone
	}
	meta, err := workflow.FindServiceMeta(stateDir, state.TargetServiceHostname)
	if err != nil || meta == nil || meta.BuildIntegration == "" {
		return topology.BuildIntegrationNone
	}
	return meta.BuildIntegration
}

// launchFirstReleaseBlock is the structured pipeline-first truth on every
// launched response: the production runtimes are ACTIVE with EMPTY
// containers (startWithoutCode) and the application arrives with the
// FIRST RELEASE through the production pipeline.
type launchFirstReleaseBlock struct {
	Truth          string   `json:"truth"`
	DeliveryFamily string   `json:"deliveryFamily"` // actions | webhook | none
	NextSteps      []string `json:"nextSteps"`
	Watch          string   `json:"watch"`
}

// composeFirstReleaseBlock renders the per-family first-release steps.
// none family NEVER picks silently — the choice belongs to the user.
func composeFirstReleaseBlock(family topology.BuildIntegration, state *launchState) *launchFirstReleaseBlock {
	if state == nil {
		return nil
	}
	block := &launchFirstReleaseBlock{
		Truth:          "Production runtimes are ACTIVE with EMPTY containers (startWithoutCode) — the application is NOT running yet. The FIRST production build arrives through the production pipeline when the first release tag is pushed.",
		DeliveryFamily: string(family),
		Watch:          "Watch via prod-ops (action=\"prod-ops\") — the launch-window token is read from the staged " + ops.LaunchTokenEnvKey + " secret, no launchKey re-send needed. Verify HTTP exposure afterwards, then close the launch window per the post-launch checklist.",
	}
	releaseStep := "Release: zerops_workflow action=\"release\" service=\"" + state.TargetServiceHostname + "\" — tags + pushes; the pipeline builds production."
	switch family {
	case topology.BuildIntegrationActions:
		block.NextSteps = []string{
			"Wire the actions track NOW — prodCd block: run secret.command (it conveys the staged " + ops.LaunchTokenEnvKey + " secret to the GitHub repo secret; no value is pasted), write .github/workflows/zerops-prod.yml, commit + push.",
			releaseStep,
		}
	case topology.BuildIntegrationWebhook:
		block.NextSteps = []string{
			"Configure the dashboard TAG integration on each production runtime (see blockers: deep-link + recommended repositoryFullName/eventType/tagRegex).",
			releaseStep,
		}
	case topology.BuildIntegrationNone:
		fallthrough
	default:
		block.NextSteps = []string{
			"No delivery family is declared (the build-integration recommendation was skipped) — ASK THE USER which production delivery to wire: GitHub Actions (agent-drivable: repo secret + tag-triggered workflow file) or the dashboard TAG integration (GUI, no repo secret). Then wire it and release.",
			releaseStep,
		}
	}
	return block
}

// prodCDActionsBlock composes the tag→prod GitHub Actions track for
// sources whose declared BuildIntegration is `actions`
// (spec-git-delivery-target FP-5 — the F7 item that was silently cut):
// a complete second workflow file (`on: push: tags`) with CONCRETE prod
// service IDs from the import result, plus the two repo-secret commands.
// The dashboard TAG webhook remains the fully-supported alternative
// (Path B, P-LP-7 untouched) — the block names both, recommending the
// track that matches the declared integration. Nil when the source pair
// declared webhook/none (the pipeline atoms own that story).
func prodCDActionsBlock(stateDir string, state *launchState) map[string]any {
	if stateDir == "" || state == nil || state.TargetServiceHostname == "" {
		return nil
	}
	meta, err := workflow.FindServiceMeta(stateDir, state.TargetServiceHostname)
	if err != nil || meta == nil || meta.BuildIntegration != topology.BuildIntegrationActions {
		return nil
	}
	idByName := make(map[string]string, len(state.ImportedServices))
	for _, svc := range state.ImportedServices {
		idByName[svc.Name] = svc.ID
	}
	type prodJob struct{ Hostname, ServiceID, Setup string }
	var jobs []prodJob
	for _, rt := range state.RuntimeProds {
		id := idByName[rt.ProdHostname]
		if id == "" {
			continue
		}
		setup := rt.SetupName
		if setup == "" {
			setup = setupNameProd
		}
		jobs = append(jobs, prodJob{Hostname: rt.ProdHostname, ServiceID: id, Setup: setup})
	}
	if len(jobs) == 0 {
		return nil
	}
	var stepsB strings.Builder
	for _, j := range jobs {
		fmt.Fprintf(&stepsB, `      - name: Deploy %s to production
        run: |
          zcli login "$%[4]s"
          zcli push --service-id %[2]q --setup %[3]q
        env:
          %[4]s: ${{ secrets.%[4]s }}
`, j.Hostname, j.ServiceID, j.Setup, ops.LaunchTokenEnvKey)
	}
	steps := stepsB.String()
	workflowYAML := `name: Zerops production release
on:
  push:
    tags: ['v*.*.*']
jobs:
  deploy-production:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
` + steps
	owner, repo, repoOK := ops.ParseGitRemoteOwnerRepo(meta.RemoteURL)
	ownerRepo := "<owner>/<repo>"
	if repoOK {
		ownerRepo = owner + "/" + repo
	}
	sshFlags := "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	secretCmd := fmt.Sprintf(
		"GH_TOKEN=$(ssh %s %s 'printf %%s \"$GIT_TOKEN\"') gh secret set %s -b \"$(ssh %s %s 'printf %%s \"$%s\"')\" -R %s",
		sshFlags, meta.Hostname, ops.LaunchTokenEnvKey, sshFlags, meta.Hostname, ops.LaunchTokenEnvKey, ownerRepo,
	)
	return map[string]any{
		"track": "actions-tag",
		"why":   "Your source pair declared integration=actions, so production delivery is a TAG-triggered workflow in the SAME repo: `git push --tags` (or zerops_workflow action=\"release\") deploys production. This track delivers the FIRST production build too — the launched runtimes are empty (startWithoutCode) until the first release. The dashboard TAG integration remains a fully-supported alternative — see the pipeline guidance.",
		"workflowFile": map[string]any{
			"path":    ".github/workflows/zerops-prod.yml",
			"content": workflowYAML,
		},
		"secret": map[string]any{
			"name":    ops.LaunchTokenEnvKey,
			"source":  "STAGED single token: the same integration token that created the production project is staged as the " + ops.LaunchTokenEnvKey + " service secret on the source push service. The command below conveys it secret-to-secret — both values are read over ssh, so neither re-enters the conversation. Do NOT ask the user to paste the token again and NEVER fabricate one. The token stays valid for GitHub Actions after the launch window closes (confirm-production deletes only the staged service env); recommend regenerating it in the dashboard later for maximum hygiene.",
			"command": secretCmd,
		},
		"steps": []string{
			"1. Run secret.command — it reads the staged " + ops.LaunchTokenEnvKey + " secret from " + meta.Hostname + " and sets it as the GitHub repo secret (no value passes through the conversation).",
			"2. Write workflowFile.content at .github/workflows/zerops-prod.yml in the source repo, commit, push.",
			"3. From then on: zerops_workflow action=\"release\" service=\"" + meta.Hostname + "\" tags + pushes — the workflow deploys production.",
		},
		"hardening":    "Recommend to the user: a plain repo secret is effectively readable by any write-access collaborator (a workflow edit can exfiltrate it). Where the GitHub plan allows, move " + ops.LaunchTokenEnvKey + " to a `production` ENVIRONMENT secret with required reviewers and pin the deploy job with `environment: production` (environments on private repos need Pro/Team; required reviewers on private need Enterprise; public repos get both on any plan).",
		"verification": "A launch resume earns the actions track once the workflow file is present at the pushed HEAD; prod-ops status reflects it in the done boundary.",
	}
}

// launchPipelineSummaryEntry is one runtime's CD observation row.
type launchPipelineSummaryEntry struct {
	Hostname   string `json:"hostname"`
	State      string `json:"state"` // configured | not-configured | skipped | check-failed
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// pipelineSummaryFrom derives the sorted per-runtime CD summary from
// state.PipelineConfigurations (F43). Key-free by construction —
// the entries carry only observation data.
func pipelineSummaryFrom(state *launchState) []launchPipelineSummaryEntry {
	if state == nil || len(state.PipelineConfigurations) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(state.PipelineConfigurations))
	for h := range state.PipelineConfigurations {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	out := make([]launchPipelineSummaryEntry, 0, len(hosts))
	for _, h := range hosts {
		entry := state.PipelineConfigurations[h]
		row := launchPipelineSummaryEntry{Hostname: h}
		switch {
		case entry.SkipReason == pipelineSkipReasonOptedOut:
			row.State = "skipped"
			row.Detail = "user opted out (skipPipelineSetup)"
		case entry.SkipReason != "":
			row.State = "check-failed"
			row.Detail = entry.SkipReason
		case entry.Configured:
			row.State = "configured"
			if entry.CurrentConfig != nil {
				row.Provider = entry.CurrentConfig.Provider
				row.Repository = entry.CurrentConfig.RepositoryFullName
			}
		default:
			row.State = "not-configured"
			row.Detail = "wire CD in the dashboard (see the pipeline blocker recommendation)"
		}
		out = append(out, row)
	}
	return out
}

// launchPipelineConfigureDashboardAtom is the atom rendered when one or
// more promoted runtimes have no CD pipeline integration (the agent guides
// the user through dashboard setup).
const launchPipelineConfigureDashboardAtom = "launch-pipeline-configure-dashboard"

// pickPipelineAtomID selects which pipeline-related atom to render in
// the launched response based on the observed pipeline state. Empty
// string when no pipeline check has run yet (mutation pipeline came
// from a pre-Part2 state file).
func pickPipelineAtomID(state *launchState, family topology.BuildIntegration) string {
	if state.PipelineCheckedAt.IsZero() {
		return ""
	}
	// Actions family: the platform integration-status is EXPECTED
	// not-configured (GitHub Actions registers no Zerops webhook
	// integration) — the prodCD actions block owns the delivery story;
	// rendering the dashboard walkthrough next to it contradicted it.
	if family == topology.BuildIntegrationActions {
		return ""
	}
	if pendingPipelineConfigurations(state) {
		return launchPipelineConfigureDashboardAtom
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

// launchAvailableRegions returns the live region menu — the import
// schema's project.location enum. Single source of truth for both the
// scope-prompt offer and the input validation; empty when the schema
// surface is unavailable (validation then defers to the platform).
func launchAvailableRegions(ctx context.Context, schemaCache *schema.Cache) []string {
	if schemaCache == nil {
		return nil
	}
	schemas := schemaCache.Get(ctx)
	if schemas == nil || schemas.ImportYml == nil {
		return nil
	}
	return schemas.ImportYml.Locations
}

// launchCredentialAsk is one user-owned input the agent must collect
// (and WAIT for) before the chained recovery call can run.
type launchCredentialAsk struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret,omitempty"`
	FromUser    bool   `json:"fromUser"`
	Description string `json:"description"`
}

// previewContainers renders the runtime's production container range for
// the consent screen: the consented value when given, else the HA-floor
// default applied over the live source scaling.
func previewContainers(r bundle.LaunchRuntimeInput) string {
	minC := r.MinContainers
	maxC := r.MaxContainers
	consented := minC > 0
	if minC == 0 {
		minC = 2 // production HA floor default
		if r.Scaling != nil && r.Scaling.MinContainers > minC {
			minC = r.Scaling.MinContainers
		}
	}
	if maxC == 0 {
		if r.Scaling != nil && r.Scaling.MaxContainers > 0 {
			maxC = r.Scaling.MaxContainers
		}
		if maxC < minC {
			maxC = minC
		}
	}
	out := fmt.Sprintf("%d", minC)
	if maxC > minC {
		out = fmt.Sprintf("%d–%d", minC, maxC)
	}
	if consented {
		out += " (consented)"
	}
	return out
}
