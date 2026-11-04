// Tests for the launch-production publish gate, reset's staged-secret
// cleanup and the shared launch-test fixtures.
package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// sentinelMintedToken is a value that, if it ever appears in a response,
// state file, audit record, or stderr log, reveals a P-LP-1/D-2
// violation. sentinelMintedTokenPrefix is a stable leading substring —
// scans check both the full value and the prefix so a truncated/partial
// leak (e.g. a log line cut mid-token) still trips the guard.
const (
	sentinelMintedToken       = "zcp-mint-leak-canary-9f3e7a1c8d2b5610"
	sentinelMintedTokenPrefix = "zcp-mint-leak-canary"
)

// launchKeyPublishInput mirrors completeLaunchInput/pLP3CompleteInput and
// publishes with an explicit LaunchKey carrying the sentinel value.
func launchKeyPublishInput() WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		LaunchKey:             sentinelMintedToken,
	}
}

// happyMockAdmin returns a MockProjectAdminClient wired for a successful
// CreateAndImportProject + poll, matching the fixture used across the
// existing launch-mutation happy-path tests.
func happyMockAdmin() *platform.MockProjectAdminClient {
	return platform.NewMockProjectAdminClient().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "new-prod-id",
			ProjectName: "myapp-prod",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
		WithClientUserID("client-user-abc")
}

// scanLaunchStateDirForSentinels walks every file under
// stateDir/launch-production (state files + audit log) and fails the
// test if any needle appears.
func scanLaunchStateDirForSentinels(t *testing.T, stateDir string, needles ...string) {
	t.Helper()
	dir := filepath.Join(stateDir, launchStateDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read state dir %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		body := string(data)
		for _, needle := range needles {
			if strings.Contains(body, needle) {
				t.Errorf("state dir file %s contains sentinel %q", path, needle)
			}
		}
	}
}

// TestPublishGate_NoLaunchKey_StaysReadOnly pins that omitting LaunchKey
// keeps the workflow on read-only statuses.
func TestPublishGate_NoLaunchKey_StaysReadOnly(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	input := pLP3CompleteInput()
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch", resp.Status)
	}
}

// TestHandleLaunchReset_NoTargetProject_DeletesStagedSecret pins the
// §4.5 reset fix: a no-target reset (state never reached
// CreateAndImportProject) must still delete the staged secret — the
// pre-fix code only resolved/deleted credentials on the orphan-delete
// (TargetProjectID-set) path, orphaning the secret otherwise.
func TestHandleLaunchReset_NoTargetProject_DeletesStagedSecret(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()

	// First call — arm the ack.
	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("first handleLaunchReset call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "myapp-prod") {
		t.Fatalf("first call must arm the diagnose-before-destruct ack: %s", getTextContent(t, firstCall))
	}

	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"stagedSecretDeleted":true`) {
		t.Errorf("reset result must report stagedSecretDeleted=true:\n%s", body)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got != "" {
		t.Errorf("staged secret must be deleted; still reads %q", got)
	}
	if _, readErr := readLaunchState(stateDir, launchID); !errors.Is(readErr, ErrLaunchStateMissing) {
		t.Errorf("state file must be cleared after a successful reset; read err = %v", readErr)
	}
}

// TestHandleLaunchReset_ProjectDeletedSecretDeleteFails_RetrySkipsProjectDelete
// pins the 2026-07-12 audit #3 fix (reset idempotence after partial
// success): when the orphan-project delete SUCCEEDS and the staged-
// secret delete then FAILS, the persisted state must checkpoint the
// project as gone (TargetProjectID cleared) — otherwise every retry
// re-runs DeleteProject on the already-deleted project, errors, and
// never reaches the secret again (permanent wedge).
func TestHandleLaunchReset_ProjectDeletedSecretDeleteFails_RetrySkipsProjectDelete(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Attempt 1: project delete succeeds, staged-secret delete fails.
	failingClient := stagedSourceClient().WithError("DeleteUserData", errors.New("simulated delete failure"))
	adminA := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-1", Status: platform.ProcessStatusRunning}).
		WithProcess(&platform.Process{ID: "del-1", Status: platform.ProcessStatusFinished})
	restoreA := installMockAdminFactory(t, adminA)

	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", failingClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("arm call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "orphan-pid") {
		t.Fatalf("arm call must diagnose the orphan project in wouldDestroy: %s", getTextContent(t, firstCall))
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", failingClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset attempt 1: %v", err)
	}
	text := extractText(result)
	if adminA.CapturedDeleteProject != "orphan-pid" {
		t.Fatalf("attempt 1 must delete the orphan project; captured %q", adminA.CapturedDeleteProject)
	}
	if !strings.Contains(text, "NOT cleared") {
		t.Errorf("attempt 1 must refuse completion on the failed secret delete; got:\n%s", text)
	}
	if !strings.Contains(text, "orphan-pid") {
		t.Errorf("the refusal must report the project WAS already deleted (partial progress); got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state after attempt 1: %v", readErr)
	}
	if state.TargetProjectID != "" {
		t.Fatalf("checkpoint missing: TargetProjectID must be cleared after the successful project delete; got %q", state.TargetProjectID)
	}
	restoreA()

	// Attempt 2 (secret delete now succeeds): must NOT re-run
	// DeleteProject, must delete the secret and clear the state.
	healthyClient := stagedSourceClient()
	adminB := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, adminB)()

	if _, _, armErr := handleLaunchReset(context.Background(), stateDir, "src", healthyClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); armErr != nil {
		t.Fatalf("arm call attempt 2: %v", armErr)
	}
	result2, _, err2 := handleLaunchReset(context.Background(), stateDir, "src", healthyClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err2 != nil {
		t.Fatalf("handleLaunchReset attempt 2: %v", err2)
	}
	if adminB.CapturedDeleteProject != "" {
		t.Errorf("attempt 2 must not re-delete the already-deleted project; captured %q", adminB.CapturedDeleteProject)
	}
	if got := stagedTokenValue(t, healthyClient, "svc-dev"); got != "" {
		t.Errorf("attempt 2 must delete the staged secret; still reads %q", got)
	}
	if !strings.Contains(getTextContent(t, result2), `"stagedSecretDeleted":true`) {
		t.Errorf("attempt 2 must report stagedSecretDeleted=true:\n%s", getTextContent(t, result2))
	}
	if _, readErr2 := readLaunchState(stateDir, launchID); !errors.Is(readErr2, ErrLaunchStateMissing) {
		t.Errorf("state must be cleared after attempt 2; read err = %v", readErr2)
	}
}

// TestHandleLaunchReset_DeleteProcessFails_NoCheckpoint pins the async
// truth of the P4 checkpoint (Codex review, 2026-07-12): DeleteProject
// call-success is only INITIATION — when the platform-side deletion
// process ends FAILED, the project may still exist, so the checkpoint
// must NOT clear TargetProjectID and the reset must refuse with the
// orphan still tracked for a re-delete.
func TestHandleLaunchReset_DeleteProcessFails_NoCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()
	failReason := "quota teardown rejected"
	admin := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-1", Status: platform.ProcessStatusRunning}).
		WithProcess(&platform.Process{ID: "del-1", Status: platform.ProcessStatusFailed, FailReason: &failReason})
	defer installMockAdminFactory(t, admin)()

	if _, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); err != nil {
		t.Fatalf("arm call: %v", err)
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "did not succeed") || !strings.Contains(text, failReason) {
		t.Errorf("refusal must report the failed deletion process with its reason; got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if state.TargetProjectID != "orphan-pid" {
		t.Errorf("a FAILED deletion process must NOT checkpoint the project as gone; TargetProjectID = %q", state.TargetProjectID)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got == "" {
		t.Error("the staged secret must be untouched when the reset refused before secret cleanup")
	}
}

// TestHandleLaunchReset_DeleteReturnsNoProcess_NoCheckpoint pins the
// defensive branch (Codex confirmation round): a DeleteProject call that
// returns no trackable process cannot be CONFIRMED — reset must refuse
// without checkpointing (live DeleteProject always returns the process;
// nil would mean an unobservable outcome).
func TestHandleLaunchReset_DeleteReturnsNoProcess_NoCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()
	admin := platform.NewMockProjectAdminClient() // DeleteProject returns (nil, nil)
	defer installMockAdminFactory(t, admin)()

	if _, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); err != nil {
		t.Fatalf("arm call: %v", err)
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "no trackable process") {
		t.Errorf("refusal must name the unconfirmable deletion; got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if state.TargetProjectID != "orphan-pid" {
		t.Errorf("an unconfirmable deletion must NOT checkpoint; TargetProjectID = %q", state.TargetProjectID)
	}
}

// TestHandleLaunchReset_StagedSecretDeleteFails_PreservesState pins the
// delete-first discipline: when the staged-secret delete fails, the
// state file is preserved and completion is refused (mirror of
// confirm-production's delete-first rule).
func TestHandleLaunchReset_StagedSecretDeleteFails_PreservesState(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient().WithError("DeleteUserData", errors.New("simulated delete failure"))

	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("first handleLaunchReset call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "myapp-prod") {
		t.Fatalf("first call must arm the diagnose-before-destruct ack: %s", getTextContent(t, firstCall))
	}

	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "NOT cleared") {
		t.Errorf("a failed staged-secret delete must refuse completion honestly; got:\n%s", text)
	}
	if _, readErr := readLaunchState(stateDir, launchID); readErr != nil {
		t.Errorf("state must be PRESERVED after a failed staged-secret delete; read err = %v", readErr)
	}
}

// TestLaunchTokenStageFailedMessage_OriginalWording pins that the staging
// failure message keeps its original wording: nothing was created and the
// same launchKey can be re-supplied.
func TestLaunchTokenStageFailedMessage_OriginalWording(t *testing.T) {
	t.Parallel()
	msg := launchTokenStageFailedMessage(errors.New("boom"), "appdev")
	for _, forbidden := range []string{"delegation", "regenerate", "one-time"} {
		if strings.Contains(strings.ToLower(msg), forbidden) {
			t.Errorf("staging message must carry no delegation narrative; found %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "re-call with the same launchKey") {
		t.Errorf("original wording must be preserved; got %q", msg)
	}
}
