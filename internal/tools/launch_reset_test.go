// Tests for: tools/launch_reset.go::handleLaunchReset.
//
// Pinned cases:
//   - Missing productionProjectName → invalid-parameter error
//   - No state file → idempotent no-op response
//   - State exists, no ack → ErrDiagnosisRequired + wouldDestroy payload
//   - State exists, mismatched ack → ErrDiagnosisRequired
//   - State exists, valid ack → file deleted, structured report returned
//   - State with TargetProjectID → note warns about orphan prod project
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestHandleLaunchReset_MissingProductionProjectName_Error(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{})
	body := launchResetTextBody(t, result)
	if !strings.Contains(body, "productionProjectName") {
		t.Errorf("error message must reference productionProjectName, got %q", body)
	}
}

func TestHandleLaunchReset_NoStateFile_IdempotentNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{ProductionProjectName: "myapp-prod"})

	report := unmarshalResetReport(t, result)
	if report.Operation != launchResetOperation {
		t.Errorf("Operation = %q, want %q", report.Operation, launchResetOperation)
	}
	if !strings.Contains(report.Note, "Already clean") {
		t.Errorf("Note must indicate no-op for missing state, got %q", report.Note)
	}
}

func TestHandleLaunchReset_FirstCallNoAck_ReturnsWouldDestroy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          generateLaunchID("src", "myapp-prod"),
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{ProductionProjectName: "myapp-prod"})

	body := launchResetTextBody(t, result)
	if !strings.Contains(body, `"code":"DIAGNOSIS_REQUIRED"`) {
		t.Errorf("first call without ack must return DIAGNOSIS_REQUIRED, got %q", body)
	}
	if !strings.Contains(body, `"wouldDestroy"`) {
		t.Errorf("response must carry wouldDestroy block, got %q", body)
	}
	if !strings.Contains(body, launchResetOperation) {
		t.Errorf("wouldDestroy.operation must be %q, got %q", launchResetOperation, body)
	}
	if !strings.Contains(body, "myapp-prod") {
		t.Errorf("wouldDestroy.targets must contain target project name, got %q", body)
	}

	// Smoking gun: state file MUST still exist after the first-call refusal.
	statePath := filepath.Join(dir, launchStateDir, state.LaunchID+".json")
	if _, statErr := os.Stat(statePath); os.IsNotExist(statErr) {
		t.Errorf("first call must NOT delete state file (it returned a refusal)")
	}
}

func TestHandleLaunchReset_MismatchedAck_StillRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          generateLaunchID("src", "myapp-prod"),
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// Ack with wrong operation.
	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           "import-override", // wrong!
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	})
	body := launchResetTextBody(t, result)
	if !strings.Contains(body, "DIAGNOSIS_REQUIRED") {
		t.Errorf("mismatched ack must STILL return DIAGNOSIS_REQUIRED, got %q", body)
	}
}

func TestHandleLaunchReset_ValidAck_DeletesAndReports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	})

	report := unmarshalResetReport(t, result)
	if report.LaunchID != launchID {
		t.Errorf("LaunchID = %q, want %q", report.LaunchID, launchID)
	}
	if report.PriorStatus != topology.LaunchStatusFailed {
		t.Errorf("PriorStatus = %q, want failed", report.PriorStatus)
	}
	if report.TargetProjectName != "myapp-prod" {
		t.Errorf("TargetProjectName = %q, want myapp-prod", report.TargetProjectName)
	}

	// State file MUST be gone.
	statePath := filepath.Join(dir, launchStateDir, launchID+".json")
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Errorf("state file must be deleted; stat err = %v", statErr)
	}
}

func TestHandleLaunchReset_WithTargetProjectID_WarnsOrphan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          generateLaunchID("src", "myapp-prod"),
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "tgt-existing",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	result, _, _ := handleLaunchReset(dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	})

	report := unmarshalResetReport(t, result)
	if !strings.Contains(report.Note, "tgt-existing") {
		t.Errorf("Note must warn about orphan project (TargetProjectID set), got %q", report.Note)
	}
}

// --- helpers --------------------------------------------------------------

func launchResetTextBody(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("nil / empty result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] not TextContent: %T", result.Content[0])
	}
	return text.Text
}

func unmarshalResetReport(t *testing.T, result *mcp.CallToolResult) launchResetReport {
	t.Helper()
	body := launchResetTextBody(t, result)
	var rep launchResetReport
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("unmarshal report: %v\nbody: %s", err, body)
	}
	return rep
}
