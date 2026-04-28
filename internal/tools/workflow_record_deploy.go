package tools

import (
	"context"

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
// CI/CD, custom platform calls) to MCP-tracked state by stamping the
// service meta's FirstDeployedAt. Once stamped, develop atoms gated on
// `deployStates: [deployed]` start firing for the service in subsequent
// envelope renders.
//
// Workflow-less by design: external deployers happen outside any session
// life-cycle. Idempotent — re-stamping is a no-op that returns the
// existing timestamp. Missing meta returns a non-error response noting
// the service is not yet bootstrapped/adopted (nothing to stamp).
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

	// Phase 7: auto-enable subdomain on a fresh stamp (matches the in-tree
	// deploy paths). maybeAutoEnableSubdomain mutates a *ops.DeployResult,
	// so build a synthetic and copy fields back. Eligibility, idempotency,
	// and platform-side check-before-enable all live inside the helper.
	if stamped && client != nil && httpClient != nil {
		dr := &ops.DeployResult{}
		maybeAutoEnableSubdomain(ctx, client, httpClient, projectID, stateDir, input.TargetService, dr)
		resp.SubdomainAccessEnabled = dr.SubdomainAccessEnabled
		resp.SubdomainURL = dr.SubdomainURL
		resp.Warnings = dr.Warnings
	}

	return jsonResult(resp), resp, nil
}
