package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// installMockAdminFactory replaces the package factory with one returning
// the supplied mock. Returns a cleanup that restores the original.
func installMockAdminFactory(t *testing.T, mock *platform.MockProjectAdminClient) func() {
	t.Helper()
	restore := setProjectAdminClientFactory(func(launchKey, apiHost string) (platform.ProjectAdminClient, error) {
		if launchKey == "" {
			return nil, platform.ErrEmptyLaunchKey
		}
		_ = apiHost
		return mock, nil
	})
	return restore
}

// withTempState returns a state dir under t.TempDir.
func withTempState(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// completeLaunchInput returns a WorkflowInput with all source-control
// fields a real publish needs. Used by mutation-path tests.
func completeLaunchInput() WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		LaunchKey:             sentinelLaunchKey,
	}
}

// TestHandleLaunchProduction_MissingTargetService_ScopePromptEarly pins
// that missing targetService surfaces as a scope-prompt blocker (early
// fail), NOT as a late mutation failure. Part of the Part-2 ergonomics
// pass — scope-check is the right layer for must-have inputs, and the
// scope-prompt response carries the available-runtimes hint so the
// agent can re-call with the right value on the next turn.
func TestHandleLaunchProduction_MissingTargetService_ScopePromptEarly(t *testing.T) {
	stateDir := withTempState(t)
	mock := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, mock)()

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := completeLaunchInput()
	input.TargetService = "" // explicit empty

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != "scope-prompt" {
		t.Fatalf("status: got %q want scope-prompt (missing targetService surfaces in scope-check now)", resp.Status)
	}
	foundTargetService := false
	for _, b := range resp.Blockers {
		if b.ID == "scope-missing-targetService" {
			foundTargetService = true
			break
		}
	}
	if !foundTargetService {
		t.Fatalf("expected scope-missing-targetService blocker; got %+v", resp.Blockers)
	}
}

// TestLaunchState_RoundTrip verifies write+read roundtrip.
func TestLaunchState_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          "abc12345",
		SourceProjectID:   "source-id",
		TargetProjectID:   "target-id",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunched,
		Classifications: map[string]topology.SecretClassification{
			"LOG_LEVEL": topology.SecretClassPlainConfig,
		},
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readLaunchState(dir, "abc12345")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatal("nil state after write")
	}
	if got.TargetProjectID != "target-id" {
		t.Errorf("TargetProjectID round-trip: got %q", got.TargetProjectID)
	}
	if got.Status != topology.LaunchStatusLaunched {
		t.Errorf("Status round-trip: got %q", got.Status)
	}
}

// TestLaunchState_ReadMissing returns ErrLaunchStateMissing for an
// absent state file (sentinel error rather than (nil, nil) per strict
// lint discipline).
func TestLaunchState_ReadMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state, err := readLaunchState(dir, "nonexistent")
	if !errors.Is(err, ErrLaunchStateMissing) {
		t.Fatalf("read missing: expected ErrLaunchStateMissing, got %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state for missing file, got %+v", state)
	}
}

// TestLaunchState_NoLaunchKeyFieldExists is a compile-time-adjacent pin:
// launchState struct must never grow a LaunchKey field. P-LP-1.
func TestLaunchState_NoLaunchKeyFieldExists(t *testing.T) {
	t.Parallel()
	// Marshal a populated state; assert no "launchKey" / "key" / "token"
	// field name appears.
	state := launchState{
		LaunchID:          "x",
		SourceProjectID:   "y",
		TargetProjectID:   "z",
		TargetProjectName: "w",
		Status:            topology.LaunchStatusLaunched,
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := strings.ToLower(string(b))
	for _, banned := range []string{"launchkey", "\"key\":", "\"token\":", "apikey"} {
		if strings.Contains(body, banned) {
			t.Errorf("launchState marshals a banned secret-like field name %q: %s", banned, body)
		}
	}
}

// TestReadyToLaunchSoftRead_NoAuditEntries pins bug_008: the active-
// compare baseline read at ready-to-launch is a probe — it runs on
// every poll BEFORE the user commits to publishing — so its failure
// paths MUST NOT persist publish-rejected entries to the audit log.
// Mutation callers (executeLaunchMutation, executeExistingProjectMutation)
// continue to write the audit log on hard-read failures; only the
// soft-read branch (workflow_launch_production.go:193) suppresses.
//
// Pre-fix: every poll against a source missing `setup: prod` appended
// a publish-rejected entry to launch-audit-log.json, polluting forensic
// reads with refusals the user never authored.
func TestReadyToLaunchSoftRead_NoAuditEntries(t *testing.T) {
	stateDir := t.TempDir()

	// Source zerops.yaml lacks a `setup: prod` block — the active-compare
	// gate's hasSetupNamed check returns false. Pre-fix this lands in
	// auditFail and writes a publish-rejected entry; post-fix it returns
	// the blocker without persisting state.
	sshNoProdBlock := &stubSSHDeployer{
		responses: map[string][]byte{
			"git rev-parse HEAD": []byte("frozen-baseline-sha\n"),
			"git remote get-url": []byte("https://github.com/example/myapp\n"),
			"/var/www/zerops.yaml": []byte("zerops:\n" +
				"  - setup: dev\n" +
				"    build:\n      base: nodejs@22\n" +
				"    run:\n      base: nodejs@22\n      start: node dev.js\n"),
		},
	}

	sourceClient := pLP3MockClient()
	// pLP3CompleteInput has no LaunchKey + no ExistingProjectID — the
	// active-compare baseline path runs, publishing=false.
	input := pLP3CompleteInput()

	_, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, input,
		stateDir,
		pLP3ContainerRuntime(),
		sshNoProdBlock,
		"",
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}

	auditPath := filepath.Join(stateDir, launchStateDir, launchAuditLogName)
	if _, statErr := os.Stat(auditPath); statErr == nil {
		body, _ := os.ReadFile(auditPath)
		t.Errorf("ready-to-launch soft-read probe MUST NOT write audit entries; found %d bytes:\n%s",
			len(body), string(body))
	}
}

// TestAuditLog_AppendOnlyMode verifies audit log writes append, never
// truncate. P-LP-6.
func TestAuditLog_AppendOnlyMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 3 {
		entry := launchAuditEntry{
			LaunchID:        "abc",
			Action:          "test-action",
			SourceProjectID: "src",
			Result:          "success",
		}
		if err := appendAuditLog(dir, entry); err != nil {
			t.Fatalf("append #%d: %v", i, err)
		}
	}
	path := filepath.Join(dir, launchStateDir, launchAuditLogName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Count(string(data), "\n")
	if lines != 3 {
		t.Errorf("expected 3 audit lines, got %d (body:\n%s)", lines, string(data))
	}
}

// TestAuditLog_NeverContainsLaunchKey pins P-LP-1 on the audit log path.
// Even if a future handler change accidentally tries to write the key
// into an audit entry, the entry struct has no field for it.
func TestAuditLog_NeverContainsLaunchKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := launchAuditEntry{
		LaunchID:        "x",
		Action:          "publish",
		SourceProjectID: "src",
		Result:          "success",
	}
	if err := appendAuditLog(dir, entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	path := filepath.Join(dir, launchStateDir, launchAuditLogName)
	data, _ := os.ReadFile(path)
	body := strings.ToLower(string(data))
	for _, banned := range []string{"launchkey", "\"key\":", "\"token\":", "apikey", sentinelLaunchKey} {
		if strings.Contains(body, strings.ToLower(banned)) {
			t.Errorf("audit log contains banned %q: %s", banned, body)
		}
	}
}

// TestHandleLaunchProduction_Mutation_AuthFailureWrappedSafely verifies
// the auth failure error wrapper never leaks the key value.
func TestHandleLaunchProduction_Mutation_AuthFailureWrappedSafely(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	// Factory that always errors (e.g., key invalid)
	restore := setProjectAdminClientFactory(func(launchKey, apiHost string) (platform.ProjectAdminClient, error) {
		_ = launchKey
		_ = apiHost
		return nil, errors.New("simulated invalid key")
	})
	defer restore()

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := completeLaunchInput()
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	// Key sentinel should not appear in the wrapped error
	if strings.Contains(text, sentinelLaunchKey) {
		t.Errorf("auth failure response leaks launchKey value: %s", text)
	}
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != "failed" {
		t.Fatalf("status: got %q want failed", resp.Status)
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Category != "auth" {
		t.Fatalf("expected auth blocker, got %+v", resp.Blockers)
	}
}

// TestHandleLaunchProduction_IdempotentResume verifies that a second
// call with an existing target-project state returns the launched
// status without re-importing.
func TestHandleLaunchProduction_IdempotentResume(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	launchID := generateLaunchID("source-project-id", "myapp-prod")
	// Pre-populate state file as if a prior publish already ran.
	priorState := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "source-project-id",
		TargetProjectID:   "prior-target-id",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunched,
	}
	if err := writeLaunchState(stateDir, priorState); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Mock admin that should NOT be called on resume.
	mock := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, mock)()

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := completeLaunchInput()
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	if resp.Status != "launched" {
		t.Fatalf("status: got %q want launched", resp.Status)
	}
	// Idempotency: the mock admin was never asked to create+import.
	if mock.CapturedImportYAML != "" {
		t.Errorf("admin.CreateAndImportProject called on resume; should have been skipped")
	}
}

// TestHandleLaunchProduction_LaunchedResponseIncludesDeleteKey pins
// P-LP-4: launched response always carries the launch-delete-key atom.
func TestHandleLaunchProduction_LaunchedResponseIncludesDeleteKey(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	launchID := generateLaunchID("source-project-id", "myapp-prod")
	state := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "source-project-id",
		TargetProjectID:   "target-id",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunched,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mock := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, mock)()
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := completeLaunchInput()
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	bodyLower := strings.ToLower(text)
	// The launch-post-checklist atom mentions delete-the-key prominently.
	// Resume case returns either launch-post-checklist OR the resume
	// guidance; both must include a key-deletion phrase.
	if !strings.Contains(bodyLower, "delete") {
		t.Errorf("launched response does not mention key deletion: %s", text)
	}
}

// TestPollImportedServices_AllSuccess verifies that when every recorded
// process returns FINISHED, the poll completes without error.
func TestPollImportedServices_AllSuccess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient().
		WithProcess(&platform.Process{ID: "p1", Status: "FINISHED"})

	state := &launchState{
		ImportedServices: []importedServiceEntry{
			{ID: "svc1", Name: "app", ProcessIDs: []string{"p1"}},
		},
	}
	if err := pollImportedServices(context.Background(), mock, state); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestPollImportedServices_FailedProcess surfaces the failure with
// service + process + reason in the error message.
func TestPollImportedServices_FailedProcess(t *testing.T) {
	t.Parallel()
	reason := "build failed: tsc compilation error"
	mock := platform.NewMockProjectAdminClient().
		WithProcess(&platform.Process{ID: "p1", Status: "FAILED", FailReason: &reason})

	state := &launchState{
		ImportedServices: []importedServiceEntry{
			{ID: "svc1", Name: "app", ProcessIDs: []string{"p1"}},
		},
	}
	err := pollImportedServices(context.Background(), mock, state)
	if err == nil {
		t.Fatal("expected error for FAILED process")
	}
	msg := err.Error()
	for _, substr := range []string{"app", "p1", "FAILED"} {
		if !strings.Contains(msg, substr) {
			t.Errorf("error message missing %q: %s", substr, msg)
		}
	}
}

// TestPollImportedServices_NoProcessesNoOp pins behavior when a
// service has no recorded ProcessIDs (e.g., import returned 0 procs
// for managed deps).
func TestPollImportedServices_NoProcessesNoOp(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := &launchState{
		ImportedServices: []importedServiceEntry{
			{ID: "svc1", Name: "db"}, // ProcessIDs empty
		},
	}
	if err := pollImportedServices(context.Background(), mock, state); err != nil {
		t.Fatalf("expected nil for empty process list, got %v", err)
	}
}
