package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleLaunchProduction orchestrates the launch-production workflow per
// plans/production-lifecycle-2026-05-11.md §8.1. Stateless multi-call
// narrowing via per-request WorkflowInput fields:
//   - ProductionProjectName / Region / CustomDomain / KeepNonHA — scope
//   - EnvClassifications — classify-prompt outputs
//   - LaunchKey — one-shot account-wide token (mutation pipeline, Phase D.2)
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

	// Status 1 — scope-prompt: required scope fields incomplete OR
	// targetService names the stage-half of a dev/stage pair (which
	// the launch flow refuses — promotion always uses the dev-half).
	if missing := missingScopeFields(input, sourceContext); len(missing) > 0 {
		return launchScopePromptResponse(corpus, input, missing, sourceContext), nil, nil
	}
	if devHost, isStageHalf := stageHalfForTarget(stateDir, input.TargetService); isStageHalf {
		return launchStageHalfBlockedResponse(corpus, input, devHost, sourceContext), nil, nil
	}

	// Read source project envs (needed for both classify-prompt and
	// publish-time bundle composition).
	sourceEnvs, err := readProjectEnvs(ctx, client, projectID)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	// Status 2 — classify-prompt: source envs present, not all bucketed.
	if needsClassifyPrompt(input.EnvClassifications, sourceEnvs) {
		classifications := convertClassificationsInput(input.EnvClassifications)
		return launchClassifyPromptResponse(corpus, sourceEnvs, classifications, sourceContext), nil, nil
	}

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

	// If we already created the target project on a prior call, two
	// resume sub-cases:
	//   - launchKey supplied AND pipeline check is pending OR
	//     SkipPipelineSetup=true and not yet recorded: re-run pipeline
	//     check with a fresh ProjectAdminClient (post-dashboard-config
	//     refresh).
	//   - otherwise: return the current launched/failed view as-is.
	// This is the recovery primitive (action="status" semantics).
	if existing != nil && existing.TargetProjectID != "" {
		if existing.Status == topology.LaunchStatusLaunched && input.LaunchKey != "" {
			if pendingPipelineConfigurations(existing) || (input.SkipPipelineSetup.Bool() && !pipelineSkipRecorded(existing)) {
				return executeLaunchPipelineResume(ctx, input, corpus, stateDir, existing)
			}
		}
		return launchResumeResponse(corpus, existing), nil, nil
	}

	if input.LaunchKey == "" {
		return launchReadyToLaunchResponse(corpus, input, sourceEnvs, sourceContext), nil, nil
	}

	// Mutation pipeline — LaunchKey supplied, no existing target.
	return executeLaunchMutation(ctx, projectID, client, sshDeployer, rt, input, sourceEnvs, classifications, corpus, stateDir, launchID)
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
func executeLaunchMutation(
	ctx context.Context,
	sourceProjectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	input WorkflowInput,
	sourceEnvs []ops.ProjectEnvVar,
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
	// the fully-populated source state for bundle composition.
	source, blocker := readAndValidateSourceState(ctx, client, sshDeployer, rt, corpus, input, sourceProjectID, stateDir, launchID)
	if blocker != nil {
		return blocker, nil, nil
	}

	bundleInputs := ops.LaunchBundleInputs{
		SourceProjectID:   sourceProjectID,
		TargetProjectName: input.ProductionProjectName,
		TargetHostname:    input.TargetService,
		ServiceType:       source.ServiceType,
		SetupName:         "prod",
		RepoURL:           source.RepoURL,
		ZeropsYAMLBody:    source.ZeropsYAMLBody,
		GitCommitSHA:      source.GitCommitSHA,
		ProjectEnvs:       sourceEnvs,
		ManagedServices:   source.ManagedServices,
		KeepNonHA:         input.KeepNonHA,
	}

	// Bundle composition — uses ops.BuildLaunchBundle (Phase C).
	bundle, err := ops.BuildLaunchBundle(bundleInputs, classifications)
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
	if len(bundle.Errors) > 0 {
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
			fmt.Sprintf("Import yaml schema validation failed: %v", bundle.Errors)), nil, nil
	}

	// Persist initial state pre-mutation — if CreateAndImport panics or
	// the process dies before completion, the state file shows the
	// attempt and the source-snapshot for forensics.
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       sourceProjectID,
		SourceRepoURL:         source.RepoURL,
		TargetProjectName:     input.ProductionProjectName,
		TargetServiceHostname: input.TargetService,
		SourceSnapshot:        bundle.SourceSnapshot,
		Classifications:       classifications,
		Status:                topology.LaunchStatusLaunching,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		// Non-fatal — proceed with the mutation, but warn.
		bundle.Warnings = append(bundle.Warnings,
			fmt.Sprintf("write launch state: %v (proceeding; resume after restart may not work)", err))
	}

	// Mutation: CreateAndImportProject. This is the irreversible step.
	result, err := admin.CreateAndImportProject(ctx, bundle.ImportYAML, platform.CreateOpts{
		Location: input.Region,
		Tags:     []string{"env:prod", "managed-by:zcp-launch"},
	})
	if err != nil {
		state.Status = topology.LaunchStatusFailed
		state.LastError = err.Error()
		_ = writeLaunchState(stateDir, state)
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "create-and-import",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			SourceCommitSHA:   bundle.SourceSnapshot.GitCommitSHA,
			SourceYAMLSHA256:  bundle.SourceSnapshot.ZeropsYAMLSHA256,
			Classifications:   classifications,
			HAOptOut:          input.KeepNonHA,
			Result:            "failure",
			ErrorMessage:      err.Error(),
		})
		return launchFailedResponse(corpus, topology.BlockerCategoryAuth,
			"create-import-failed",
			"CreateAndImportProject failed: "+err.Error()), nil, nil
	}

	// Success — record imported services in state.
	state.TargetProjectID = result.ProjectID

	// A.10: grant launching clientUser ADMIN on the new project so the
	// workflow's subsequent env-presence reads authenticate. Failure
	// here is non-fatal — the project IS created, env-read fallbacks
	// to manual UI verification.
	if err := admin.GrantSelfRole(ctx, result.ProjectID, "ADMIN"); err != nil {
		bundle.Warnings = append(bundle.Warnings,
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
			state.LastError = pollErr.Error()
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
		SourceCommitSHA:   bundle.SourceSnapshot.GitCommitSHA,
		SourceYAMLSHA256:  bundle.SourceSnapshot.ZeropsYAMLSHA256,
		Classifications:   classifications,
		HAOptOut:          input.KeepNonHA,
		Result:            boolStr(!hasPerServiceError, "success", "failure"),
		ErrorMessage:      stringIf(hasPerServiceError, "one or more service stacks reported import errors"),
	})

	if hasPerServiceError {
		return launchFailedResponse(corpus, topology.BlockerCategoryOrphan,
			"orphan-project",
			fmt.Sprintf("Target project %s created but one or more services had import errors. Inspect imported-services in state file; delete via Zerops dashboard or retry with corrected inputs.", result.ProjectID),
		), nil, nil
	}
	if state.Status == topology.LaunchStatusFailed {
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"first-deploy-failed",
			fmt.Sprintf("Target project %s created but first deploy did not complete cleanly: %s", result.ProjectID, state.LastError),
		), nil, nil
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
) (*LaunchSourceState, *mcp.CallToolResult) {
	auditFail := func(reason string) {
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
	if !hasSetupProd(source.ZeropsYAMLBody) {
		auditFail("source zerops.yaml lacks `setup: prod` block")
		// Item #6: derive a concrete proposed block from the source's
		// existing dev/stage setup. Agent applies + tweaks instead of
		// guessing from a generic template. Falls back to the generic
		// message if derivation fails (malformed yaml, no template).
		proposed, derr := deriveProdSetupBlock(source.ZeropsYAMLBody)
		if derr != nil {
			return nil, launchSourceControlBlockerResponse(corpus,
				"Source zerops.yaml lacks a `setup: prod` block — write it (see launch-write-prod-setup atom), commit, push, then re-call publish.",
			)
		}
		return nil, launchSourceControlBlockerResponse(corpus,
			prodSetupGuidanceWithBlock(proposed),
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
type launchClassifyRow struct {
	Key           string                        `json:"key"`
	CurrentBucket topology.SecretClassification `json:"currentBucket"`
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

// launchStageHalfBlockedResponse fires when the agent supplied
// `targetService` as the stage-half of a dev/stage pair (e.g.
// `appstage` when the meta records {Hostname: appdev, StageHostname:
// appstage}). Promotion always uses the dev-half — silently
// rewriting could publish from the wrong source tree (the stage-half
// is a deploy target, not a source). A scope-prompt blocker tells the
// agent to re-call with the dev-half hostname, keeping the user in
// the loop on which half ships.
func launchStageHalfBlockedResponse(corpus []workflow.KnowledgeAtom, input WorkflowInput, devHost string, sourceCtx *launchSourceContext) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-scope-prompt")
	if guidance == "" {
		guidance = "targetService names a stage-half. Re-call with the dev-half hostname (the platform's authoritative source of truth)."
	}
	blockers := []topology.Blocker{{
		ID:       "scope-stage-half-not-promotable",
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategoryScope,
		Message:  fmt.Sprintf("targetService %q is the stage-half of dev/stage pair (dev-half=%q). Promotion uses the dev-half — re-call with targetService=%q.", input.TargetService, devHost, devHost),
	}}
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
func launchClassifyPromptResponse(
	corpus []workflow.KnowledgeAtom,
	sourceEnvs []ops.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	sourceCtx *launchSourceContext,
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

	rows := make([]launchClassifyRow, 0, len(sourceEnvs))
	for _, env := range sourceEnvs {
		rows = append(rows, launchClassifyRow{
			Key:           env.Key,
			CurrentBucket: classifications[env.Key],
		})
	}

	return jsonResult(launchProductionResponse{
		Workflow:        workflowLaunchProduction,
		Status:          topology.LaunchStatusClassifyPrompt,
		Phase:           workflow.PhaseLaunchProductionActive,
		Guidance:        guidance,
		Classifications: rows,
		SourceContext:   sourceCtx,
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
	sourceEnvs []ops.ProjectEnvVar,
	sourceCtx *launchSourceContext,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-mutation-key-required")
	if guidance == "" {
		guidance = "Scope and classifications complete. Generate a one-shot Zerops API key (account-wide) and re-call with launchKey set to advance to publish."
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
		topology.LaunchStatusClassifyPrompt,
		topology.LaunchStatusReadyToLaunch,
		topology.LaunchStatusLaunching,
		topology.LaunchStatusConfiguringPipeline:
		// Resume found state with a pre-terminal status — surface
		// in-progress guidance; agent re-polls via action="status".
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
	_ = corpus
	return jsonResult(launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusFailed,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: "Launch-window key validation failed. Generate a new account-wide token in Zerops dashboard and retry.",
		Blockers: []topology.Blocker{{
			ID:       "launch-key-invalid",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryAuth,
			Message:  "ProjectAdminClient construction failed: " + err.Error(),
		}},
	})
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
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusLaunched,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: guidance,
		Blockers: pipelineBlockers(state),
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
