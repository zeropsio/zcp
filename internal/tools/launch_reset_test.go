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
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestHandleLaunchReset_MissingProductionProjectName_Error(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{})
	body := launchResetTextBody(t, result)
	if !strings.Contains(body, "productionProjectName") {
		t.Errorf("error message must reference productionProjectName, got %q", body)
	}
}

func TestHandleLaunchReset_NoStateFile_IdempotentNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{ProductionProjectName: "myapp-prod"})

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

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{ProductionProjectName: "myapp-prod"})

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
	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
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

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
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

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
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

// TestHandleLaunchReset_WithLaunchKey_FirstCall_ListsProjectInWouldDestroy
// pins that when launchKey is supplied, the first-call refusal's wouldDestroy
// payload lists the orphan PROJECT (not just the state file) so the agent
// acks an actual project deletion.
func TestHandleLaunchReset_WithLaunchKey_FirstCall_ListsProjectInWouldDestroy(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
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

	mock := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, mock)()

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		LaunchKey:             "lk-123",
	})
	body := launchResetTextBody(t, result)
	if !strings.Contains(body, "DIAGNOSIS_REQUIRED") {
		t.Fatalf("first call without ack must refuse, got %q", body)
	}
	if !strings.Contains(body, `"projects":["tgt-existing"]`) {
		t.Errorf("wouldDestroy must list the orphan project when launchKey supplied, got %q", body)
	}
	if mock.CapturedDeleteProject != "" {
		t.Errorf("no deletion may happen on the refusal call, got CapturedDeleteProject=%q", mock.CapturedDeleteProject)
	}
}

// TestHandleLaunchReset_WithLaunchKey_DeletesOrphanProject pins D4: a reset
// with launchKey + valid ack deletes the orphan production project via the
// launch token AND clears state. The token stays valid until the user
// revokes it, so ZCP can still reach the project.
func TestHandleLaunchReset_WithLaunchKey_DeletesOrphanProject(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
	dir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "tgt-existing",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	mock := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-proc-1", Status: platform.ProcessStatusRunning})
	defer installMockAdminFactory(t, mock)()

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		LaunchKey:             "lk-123",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	})

	report := unmarshalResetReport(t, result)
	if mock.CapturedDeleteProject != "tgt-existing" {
		t.Errorf("DeleteProject must be called with the orphan project ID, got %q", mock.CapturedDeleteProject)
	}
	if !mock.Closed {
		t.Errorf("admin client must be Closed after use (launch token must not linger)")
	}
	if report.DeletedProjectID != "tgt-existing" {
		t.Errorf("report.DeletedProjectID = %q, want tgt-existing", report.DeletedProjectID)
	}
	if report.DeleteProcessID != "del-proc-1" {
		t.Errorf("report.DeleteProcessID = %q, want del-proc-1", report.DeleteProcessID)
	}
	if !strings.Contains(report.Note, "deletion initiated") {
		t.Errorf("Note must confirm project deletion, got %q", report.Note)
	}
	statePath := filepath.Join(dir, launchStateDir, launchID+".json")
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Errorf("state file must be deleted; stat err = %v", statErr)
	}
}

// TestHandleLaunchReset_LaunchKeyDeleteFails_KeepsState pins that a failed
// project delete leaves the state file intact (orphan stays tracked for a
// retry rather than stranded with its ID lost).
func TestHandleLaunchReset_LaunchKeyDeleteFails_KeepsState(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
	dir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "tgt-existing",
		Status:            topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	mock := platform.NewMockProjectAdminClient().WithDeleteError(errors.New("boom"))
	defer installMockAdminFactory(t, mock)()

	result, _, _ := handleLaunchReset(context.Background(), dir, "src", WorkflowInput{
		ProductionProjectName: "myapp-prod",
		LaunchKey:             "lk-123",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	})

	body := launchResetTextBody(t, result)
	if !strings.Contains(body, "delete production project") {
		t.Errorf("expected a delete-failed error, got %q", body)
	}
	statePath := filepath.Join(dir, launchStateDir, launchID+".json")
	if _, statErr := os.Stat(statePath); os.IsNotExist(statErr) {
		t.Errorf("state file must be KEPT when project delete fails (orphan stays tracked)")
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
