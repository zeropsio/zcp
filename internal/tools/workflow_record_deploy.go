package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recordDeployResult is the structured response of action="record-deploy".
type recordDeployResult struct {
	Hostname        string `json:"hostname"`
	Stamped         bool   `json:"stamped"`
	FirstDeployedAt string `json:"firstDeployedAt"`
	Note            string `json:"note,omitempty"`
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
// Required input: TargetService (the runtime hostname whose meta to stamp).
func handleRecordDeploy(stateDir string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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

	return jsonResult(resp), resp, nil
}
