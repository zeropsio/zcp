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
// P-LP-1 / P-LP-2 preserved — the token (explicit launchKey, or the
// staged ZEROPS_TOKEN_PROD secret read in-request) flows only into the
// per-call admin client on the orphan-delete path and is never echoed
// or persisted; without a resolvable token reset is pure file-system
// mutation.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
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
	DeletedStateFile  string                          `json:"deletedStateFile"`           // absolute path for audit
	DeletedProjectID  string                          `json:"deletedProjectId,omitempty"` // orphan prod project deleted via launchKey
	DeleteProcessID   string                          `json:"deleteProcessId,omitempty"`  // platform delete process (async teardown)
	Note              string                          `json:"note,omitempty"`             // operator-facing follow-up
}

// handleLaunchReset is the dispatch target for `action="reset"` +
// `workflow="launch-production"`. Routed from workflow.go:300 alongside
// the generic handleReset (which stays session-scoped for bootstrap /
// develop / recipe). Launch-production state lives in its own
// directory; the generic handler would never touch it.
//
// Orphan-project cleanup: when a launch-window token is resolvable —
// explicit input.LaunchKey, or the staged ZEROPS_TOKEN_PROD secret on
// the source push service (single-token lifecycle T2) — AND the failed
// launch recorded a TargetProjectID, reset ALSO deletes that orphan
// production project via the token (which stays valid until the user
// revokes it — the "one-shot" model is a ZCP convention, not a Zerops
// token type). Without a resolvable token, reset stays state-file-only
// and the billable orphan is left for manual dashboard deletion.
//
// Parameters:
//   - ctx: for the cross-project DeleteProject call (orphan cleanup path).
//   - stateDir: ZCP state root (.zcp/state).
//   - sourceProjectID: current MCP-session project (the launch's source).
//     Used to find the state file via launchID derivation.
//   - client: source-project session client for the staged-secret read.
//   - input: WorkflowInput; reads ProductionProjectName + ConfirmDestructive
//   - LaunchKey.
func handleLaunchReset(ctx context.Context, stateDir, sourceProjectID string, client platform.Client, input WorkflowInput, apiHost string) (*mcp.CallToolResult, any, error) {
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

	statePath := filepath.Join(stateDir, launchStateDir, launchID+".json")

	// Orphan-delete path: a launch-window token resolved (explicit
	// launchKey, or the staged secret) + a real target project was
	// recorded. The token stays valid until the user revokes it, so ZCP
	// can still reach the orphan and delete it (not just the state file).
	launchKey := input.LaunchKey
	if launchKey == "" && state.TargetProjectID != "" {
		staged, stageErr := launchKeyFromStage(ctx, client, sourceProjectID, state)
		if stageErr == nil {
			launchKey = staged
		}
	}
	deleteProject := launchKey != "" && state.TargetProjectID != ""

	// Build the destructive-ack expectation. Targets carries the target
	// project name (single-item set per launchID); Loss lists the state
	// file and — on the orphan-delete path — the project itself.
	//
	// On the orphan-delete path the project ID is ALSO added to Targets so
	// the ack is scope-bound: ValidateDestructiveAck compares Operation +
	// acknowledgedTargets, so without this an ack minted from a state-file-
	// only refusal (Targets=[name]) would clear a later launchKey call that
	// actually deletes a real Zerops project the agent never saw in any
	// wouldDestroy.projects[]. Adding the ID forces the agent through a fresh
	// refusal whose wouldDestroy lists the project before deletion.
	expected := DiagnosedDestruction{
		Operation: launchResetOperation,
		Targets:   []string{state.TargetProjectName},
		Loss: DestructionLoss{
			LocalFiles: []string{statePath},
		},
	}
	if deleteProject {
		expected.Loss.Projects = []string{state.TargetProjectID}
		expected.Targets = append(expected.Targets, state.TargetProjectID)
	}

	if validateErr := ValidateDestructiveAck(input.ConfirmDestructive, expected); validateErr != nil {
		return convertError(validateErr, WithWouldDestroy(&expected), WithRecoveryStatus()), nil, nil
	}

	// Second call with valid ack. Delete the orphan project FIRST (before
	// clearing state) so a failed delete leaves the orphan tracked for a
	// retry rather than stranded with its ID lost.
	var deletedProjectID, deleteProcessID string
	if deleteProject {
		admin, adminErr := projectAdminClientFactory(launchKey, apiHost)
		if adminErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("launch token rejected — cannot delete production project %s: %v", state.TargetProjectID, adminErr),
				"If you already revoked the launch token, delete the project manually in the dashboard. Otherwise re-run reset with a valid launchKey. State file kept so the orphan stays tracked.",
			), WithRecoveryStatus()), nil, nil
		}
		proc, delErr := admin.DeleteProject(ctx, state.TargetProjectID)
		admin.Close()
		if delErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("delete production project %s failed: %v", state.TargetProjectID, delErr),
				"Delete the project manually in the dashboard. State file kept so the orphan stays tracked.",
			), WithRecoveryStatus()), nil, nil
		}
		deletedProjectID = state.TargetProjectID
		if proc != nil {
			deleteProcessID = proc.ID
		}
	}

	if rmErr := os.Remove(statePath); rmErr != nil && !os.IsNotExist(rmErr) {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("delete launch state file: %v", rmErr),
			"Manual cleanup required: rm "+statePath,
		), WithRecoveryStatus()), nil, nil
	}

	note := "State file deleted. Next action=\"start\" workflow=\"launch-production\" will start fresh (the same productionProjectName is free to reuse)."
	switch {
	case deleteProject:
		note += fmt.Sprintf(" Orphan production project %s deletion initiated via the launch token (process %s).", deletedProjectID, deleteProcessID)
	case state.TargetProjectID != "":
		// State cleared without a resolvable token (no launchKey and the
		// staged secret is absent), so the project ID is now gone from
		// ZCP's view. The launch token stays valid until the user revokes
		// it — but reset can only delete the orphan when a token resolves
		// IN THE SAME call (before state is cleared).
		note += " The production project " + state.TargetProjectID + " still exists in your Zerops account (billable) — delete it manually in the dashboard. Reset deletes the orphan for you when it can read the staged " + ops.LaunchTokenEnvKey + " secret on the source push service, or when the call passes launchKey=<the launch token>."
	}

	return jsonResult(launchResetReport{
		Operation:         launchResetOperation,
		LaunchID:          launchID,
		SourceProjectID:   sourceProjectID,
		TargetProjectName: state.TargetProjectName,
		PriorStatus:       state.Status,
		DeletedStateFile:  statePath,
		DeletedProjectID:  deletedProjectID,
		DeleteProcessID:   deleteProcessID,
		Note:              note,
	}), nil, nil
}
