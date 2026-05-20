// Package tools — handleLaunchReset closes the launch-production
// state-stuck loop surfaced by 5+ eval BLOCKED runs in suite 20260517 /
// 20260518.
//
// Failure mode the action closes:
//   - Launch reaches terminal `failed` (schema rejection, mutation error)
//     OR stays at `launching` past completion. State file persists at
//     .zcp/state/launch-production/<launchID>.json. launchID is
//     deterministic (sha256(sourceProjectID + "::" + targetProjectName))
//     so a blind retry with a fresh launchKey hits resume_response()
//     which echoes the cached state — new key burned without mutation.
//   - Operator workaround pre-FIX 1 PR 2: manually delete the state file.
//     FIX 1 PR 2 ships the supported action with diagnose-before-destruct
//     semantics matching the import-override gate.
//
// First call without `confirmDestructive`: returns ErrDiagnosisRequired
// + DiagnosedDestruction listing the state-file path, source/target
// project IDs, and current status. Second call with matching ack:
// deletes the state file and returns a structured report.
//
// P-LP-1 / P-LP-2 preserved — reset is pure file-system mutation, no
// ProjectAdminClient construction, no launchKey echo.
package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// launchResetOperation names the destructive operation for the reset
// gate. Echoed in DiagnosedDestruction.Operation on first-call refusal
// and required to match in ConfirmDestructive.Operation on the second.
const launchResetOperation = "launch-production-reset"

// launchResetReport is the structured response after a successful reset.
type launchResetReport struct {
	Operation         string                          `json:"operation"`       // launch-production-reset
	LaunchID          string                          `json:"launchId"`        // deleted state's launchID
	SourceProjectID   string                          `json:"sourceProjectId"` // source whose state was cleared
	TargetProjectName string                          `json:"targetProjectName"`
	PriorStatus       topology.LaunchProductionStatus `json:"priorStatus"`
	DeletedStateFile  string                          `json:"deletedStateFile"` // absolute path for audit
	Note              string                          `json:"note,omitempty"`   // operator-facing follow-up
}

// handleLaunchReset is the dispatch target for `action="reset"` +
// `workflow="launch-production"`. Routed from workflow.go:300 alongside
// the generic handleReset (which stays session-scoped for bootstrap /
// develop / recipe). Launch-production state lives in its own
// directory; the generic handler would never touch it.
//
// Parameters:
//   - stateDir: ZCP state root (.zcp/state).
//   - sourceProjectID: current MCP-session project (the launch's source).
//     Used to find the state file via launchID derivation.
//   - input: WorkflowInput; reads ProductionProjectName + ConfirmDestructive.
func handleLaunchReset(stateDir, sourceProjectID string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.ProductionProjectName == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"launch-production reset requires productionProjectName",
			`Re-call with productionProjectName=<the same target name used on the original launch>. The state file is keyed on sha256(sourceProjectId+"::"+targetProjectName) so the name uniquely identifies which launch to clear.`,
		), WithRecoveryStatus()), nil, nil
	}

	launchID := generateLaunchID(sourceProjectID, input.ProductionProjectName)
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		// Missing state file = nothing to reset. Idempotent no-op rather
		// than error — agent calling reset on a launchID that was already
		// cleared (or never created) is a benign case.
		if errors.Is(readErr, ErrLaunchStateMissing) {
			return jsonResult(launchResetReport{
				Operation:         launchResetOperation,
				LaunchID:          launchID,
				SourceProjectID:   sourceProjectID,
				TargetProjectName: input.ProductionProjectName,
				Note:              "No launch state file exists for this launchID. Already clean — nothing to reset.",
			}), nil, nil
		}
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("read launch state for reset: %v", readErr),
			"Inspect "+filepath.Join(stateDir, launchStateDir, launchID+".json")+" manually.",
		), WithRecoveryStatus()), nil, nil
	}

	// Build the destructive-ack expectation. Targets carries the target
	// project name (single-item set per launchID).
	expected := DiagnosedDestruction{
		Operation: launchResetOperation,
		Targets:   []string{state.TargetProjectName},
		Loss: DestructionLoss{
			LocalFiles: []string{filepath.Join(stateDir, launchStateDir, launchID+".json")},
		},
	}

	if validateErr := ValidateDestructiveAck(input.ConfirmDestructive, expected); validateErr != nil {
		return convertError(validateErr, WithWouldDestroy(&expected), WithRecoveryStatus()), nil, nil
	}

	// Second call with valid ack — delete the state file.
	statePath := filepath.Join(stateDir, launchStateDir, launchID+".json")
	if rmErr := os.Remove(statePath); rmErr != nil && !os.IsNotExist(rmErr) {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("delete launch state file: %v", rmErr),
			"Manual cleanup required: rm "+statePath,
		), WithRecoveryStatus()), nil, nil
	}

	note := "State file deleted. Next action=\"start\" workflow=\"launch-production\" will start fresh."
	if state.TargetProjectID != "" {
		// Orphan project may exist in Zerops account (created before
		// failure). Surface as operator follow-up since reset is
		// state-file scoped per P-LP-2; cross-project mutation requires
		// the original launch-window launchKey (project-creation
		// permission) which is one-shot already consumed.
		note += " WARNING: state recorded targetProjectId=" + state.TargetProjectID + " — the production project may exist in your Zerops account. Inspect via dashboard and delete manually if unwanted (ZCP cannot reach the prod project without a fresh launchKey)."
	}

	return jsonResult(launchResetReport{
		Operation:         launchResetOperation,
		LaunchID:          launchID,
		SourceProjectID:   sourceProjectID,
		TargetProjectName: state.TargetProjectName,
		PriorStatus:       state.Status,
		DeletedStateFile:  statePath,
		Note:              note,
	}), nil, nil
}
