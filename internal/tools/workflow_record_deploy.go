package tools

import (
	"context"
	"errors"
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
type recordDeployResult struct {
	Hostname               string   `json:"hostname"`
	Stamped                bool     `json:"stamped"`
	FirstDeployedAt        string   `json:"firstDeployedAt"`
	Note                   string   `json:"note,omitempty"`
	SubdomainAccessEnabled bool     `json:"subdomainAccessEnabled,omitempty"`
	SubdomainURL           string   `json:"subdomainUrl,omitempty"`
	Warnings               []string `json:"warnings,omitempty"`
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

	return jsonResult(resp), resp, nil
}
