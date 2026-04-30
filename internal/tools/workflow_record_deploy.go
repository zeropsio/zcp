package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recordDeployResult is the structured response of action="record-deploy".
//
// SubdomainAccessEnabled / SubdomainURL surface the result of the
// auto-enable side-effect (deploy-strategy decomposition Phase 7): when
// stamping FirstDeployedAt also unblocks subdomain auto-enable for an
// eligible mode, the response carries the L7 route activation outcome
// alongside the stamp confirmation. Warnings carries any non-fatal
// auto-enable / probe issues so the agent can recover without a
// separate zerops_subdomain round-trip.
//
// WorkSessionState mirrors the deploy/verify response shape (F5 closure):
// record-deploy mutates the per-PID Work Session by appending a synthetic
// successful DeployAttempt, so the response surfaces the session state
// post-mutation. The agent learns immediately whether this ack auto-closed
// the session (every scope service deployed+verified) without a separate
// status round-trip. nil when no session is open or the hostname is
// out-of-scope.
type recordDeployResult struct {
	Hostname               string            `json:"hostname"`
	Stamped                bool              `json:"stamped"`
	FirstDeployedAt        string            `json:"firstDeployedAt"`
	Note                   string            `json:"note,omitempty"`
	SubdomainAccessEnabled bool              `json:"subdomainAccessEnabled,omitempty"`
	SubdomainURL           string            `json:"subdomainUrl,omitempty"`
	Warnings               []string          `json:"warnings,omitempty"`
	WorkSessionState       *WorkSessionState `json:"workSessionState,omitempty"`
}

// handleRecordDeploy bridges deploys that happened outside ZCP (zcli,
// CI/CD, custom platform calls, or async builds triggered by a prior
// zerops_deploy strategy="git-push") to MCP-tracked state by stamping
// the service meta's FirstDeployedAt. After this call the develop
// envelope reports Deployed=true for the host, develop atoms gated on
// `deployStates: [never-deployed]` (e.g. develop-record-external-deploy)
// stop firing, and the Plan moves on from "Deploy" guidance.
//
// Workflow-less by design: external deployers happen outside any session
// life-cycle. Idempotent — re-stamping is a no-op that returns the
// existing timestamp. Missing meta returns a non-error response noting
// the service is not yet bootstrapped/adopted (nothing to stamp).
//
// Work-session bridge: when a develop session is active for the
// hostname, record-deploy ALSO appends a synthetic successful
// DeployAttempt so the per-session Plan dispatch (needsDeploy /
// EvaluateAutoClose) moves on. Without this, the in-flight push attempt
// recorded by zerops_deploy strategy="git-push" stays as the LAST
// attempt and the Plan keeps emitting "Deploy" guidance even after the
// agent has acked the deploy. Out-of-scope hostnames silently no-op
// (workflow-less ack of a service outside the current scope).
//
// Phase 7 of the deploy-strategy decomposition: on successful stamp
// (stamped=true), call maybeAutoEnableSubdomain so the L7 route lands
// alongside the deploy stamp — same auto-enable behaviour the in-tree
// deploy paths get on first deploy. Eligibility and idempotency are
// checked inside maybeAutoEnableSubdomain; record-deploy stays a no-op
// at the platform layer when the subdomain is already enabled or the
// mode doesn't qualify (e.g. ModeLocalOnly).
//
// Required input: TargetService (the runtime hostname whose meta to stamp).
func handleRecordDeploy(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID, stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	if input.TargetService == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"action=\"record-deploy\" requires targetService",
			"Pass targetService=\"<hostname>\" — the runtime service whose external deploy you are acknowledging."), WithRecoveryStatus()), nil, nil
	}

	// N2 build-status pre-flight: when the meta would actually flip
	// (no FirstDeployedAt yet), gate on the latest appVersion event being
	// ACTIVE. Push transmits ≠ build lands; agents that ack too early
	// stamp Deployed=true while the webhook/actions build is still
	// running, then verify against stale code and (worst case) auto-close
	// against an unfinished deploy. The atom layer
	// (`develop-build-observe`) tells the agent to wait for ACTIVE before
	// calling record-deploy — this is the defense-in-depth.
	//
	// Skipped when the meta is already stamped (idempotent re-run — no
	// state change either way) or absent (no-op response below). Skipped
	// when client is nil (test/mock setup with no platform reach).
	if client != nil {
		meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
		if meta != nil && meta.FirstDeployedAt == "" {
			if blocked := recordDeployBuildStatusGate(ctx, client, projectID, input.TargetService); blocked != nil {
				return blocked, nil, nil
			}
		}
	}

	stamped, firstAt, recordErr := workflow.RecordExternalDeploy(stateDir, input.TargetService)
	if recordErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPermissionDenied,
			"failed to record external deploy: "+recordErr.Error(),
			"Check ZCP state directory permissions; meta is written under .zcp/state/services/<hostname>.json."),
			WithRecoveryStatus()), nil, recordErr
	}

	resp := recordDeployResult{
		Hostname:        input.TargetService,
		Stamped:         stamped,
		FirstDeployedAt: firstAt,
	}
	switch {
	case firstAt == "" && !stamped:
		resp.Note = "no service meta found — service is not yet bootstrapped or adopted; record-deploy is a no-op"
	case stamped:
		resp.Note = "FirstDeployedAt freshly stamped — ServiceSnapshot.Deployed flips to true on next envelope build"
	default:
		resp.Note = "already stamped — no change"
	}

	// Work-session bridge: append a synthetic successful DeployAttempt so
	// the per-session Plan dispatch reflects the ack. RecordDeployAttempt
	// returns ErrHostnameOutOfScope when the hostname isn't in the current
	// session's scope — that's a legitimate workflow-less ack of an
	// out-of-scope deploy; we ignore it. Other errors are best-effort
	// (record-deploy stays successful even if the work-session write
	// fails).
	if firstAt != "" {
		recordErr := workflow.RecordDeployAttempt(stateDir, input.TargetService, workflow.DeployAttempt{
			AttemptedAt: time.Now().UTC().Format(time.RFC3339),
			SucceededAt: time.Now().UTC().Format(time.RFC3339),
			Strategy:    "record-deploy",
		})
		if recordErr != nil && !errors.Is(recordErr, workflow.ErrHostnameOutOfScope) {
			resp.Warnings = append(resp.Warnings,
				"record-deploy: meta stamp ok, work-session record-keeping failed: "+recordErr.Error())
		}
	}

	// Phase 7: auto-enable subdomain on a fresh stamp (matches the in-tree
	// deploy paths). maybeAutoEnableSubdomain mutates a *ops.DeployResult,
	// so build a synthetic and copy fields back. Eligibility, idempotency,
	// and platform-side check-before-enable all live inside the helper.
	// Append (not replace) the helper's warnings so any earlier work-
	// session bridge warning above survives.
	if stamped && client != nil && httpClient != nil {
		dr := &ops.DeployResult{}
		maybeAutoEnableSubdomain(ctx, client, httpClient, projectID, stateDir, input.TargetService, dr)
		resp.SubdomainAccessEnabled = dr.SubdomainAccessEnabled
		resp.SubdomainURL = dr.SubdomainURL
		resp.Warnings = append(resp.Warnings, dr.Warnings...)
	}

	resp.WorkSessionState = sessionAnnotations(stateDir)

	return jsonResult(resp), resp, nil
}

// recordDeployBuildStatusGate fetches the most recent appVersion event
// for the target service and refuses the record-deploy when the build
// has not landed (status != ACTIVE). Returns nil to let the caller
// proceed with the stamp; returns a CallToolResult ready for verbatim
// return otherwise.
//
// Status mapping:
//   - `ACTIVE`                                 → proceed (build landed)
//   - `BUILDING` / `DEPLOYING` / `PREPARING_*` → refuse, agent must wait
//   - `BUILD_FAILED` / `DEPLOY_FAILED` / `*_FAILED` / `CANCELED` → refuse
//   - no recent deploy/build event             → refuse (nothing to record)
//
// Events fetch errors fail-closed (refuse with the error). Cheap (one API
// round-trip per record-deploy), small (limit=10 because the most recent
// deploy/build event is invariably in the top entries — events are
// sorted descending by timestamp at `ops.Events`).
func recordDeployBuildStatusGate(
	ctx context.Context,
	client platform.Client,
	projectID, targetService string,
) *mcp.CallToolResult {
	// fetcher=nil: this gate only needs status, not failure classification —
	// keep the call lean (no log round-trip) on the happy path.
	events, err := ops.Events(ctx, client, nil, projectID, targetService, 10)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"record-deploy: failed to fetch zerops_events to verify build status: "+err.Error(),
			"Retry record-deploy after the events fetch recovers, or call zerops_events directly to inspect the latest appVersion status."),
			WithRecoveryStatus())
	}
	var latest *ops.TimelineEvent
	for i := range events.Events {
		ev := &events.Events[i]
		if ev.Type == "deploy" || ev.Type == "build" {
			latest = ev
			break
		}
	}
	if latest == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("record-deploy: no recent deploy/build event found for service %q — there is nothing to record yet", targetService),
			"Push first via zerops_deploy strategy=\"git-push\" (or wait for the external deployer to run), then poll zerops_events until appVersion shows Status=ACTIVE before calling record-deploy."),
			WithRecoveryStatus())
	}
	switch latest.Status {
	case statusActive:
		return nil
	case "BUILDING", "DEPLOYING", "PREPARING_RUNTIME", "WAITING_TO_BUILD", "WAITING_TO_DEPLOY", "UPLOADING":
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("record-deploy: latest appVersion is still in flight (status=%s) — wait for ACTIVE before recording the deploy", latest.Status),
			"Poll zerops_events serviceHostname=\""+targetService+"\" until the topmost appVersion event reports Status=ACTIVE, then re-run record-deploy. Pushes transmit instantly; webhook/actions builds run for tens of seconds."),
			WithRecoveryStatus())
	case statusBuildFailed, statusDeployFailed, statusPreparingRuntimeFailed, statusCanceled:
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("record-deploy: latest appVersion did not land successfully (status=%s) — there is no successful deploy to record", latest.Status),
			"Read zerops_logs serviceHostname=\""+targetService+"\" facility=application since=5m for the failure cause, fix the issue, push again, and call record-deploy after the next build reaches Status=ACTIVE."),
			WithRecoveryStatus())
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("record-deploy: unexpected appVersion status %q for service %q", latest.Status, targetService),
			"Inspect via zerops_events serviceHostname=\""+targetService+"\" — record-deploy only proceeds when the latest appVersion shows Status=ACTIVE."),
			WithRecoveryStatus())
	}
}
