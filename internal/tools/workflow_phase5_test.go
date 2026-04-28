package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleCloseMode_Update pins the happy path: writing meta.CloseDeployMode +
// CloseDeployModeConfirmed=true, returning status=updated. Single-service
// case; handler accepts a per-service map even for one entry.
func TestHandleCloseMode_Update(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": string(topology.CloseModeAuto)},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), `"status":"updated"`) {
		t.Errorf("response missing status=updated: %s", getTextContent(t, result))
	}

	// Re-read and verify persistence.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("CloseDeployMode not persisted: %+v", meta)
	}
	if !meta.CloseDeployModeConfirmed {
		t.Error("CloseDeployModeConfirmed not flipped on update")
	}
}

// TestHandleCloseMode_GitPushChainsSetup pins §3.4 Scenario B: switching
// to git-push close-mode while GitPushState != configured succeeds the
// write but surfaces a chained pointer at action=git-push-setup.
func TestHandleCloseMode_GitPushChainsSetup(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
		// GitPushState left unconfigured — migrate at parseMeta would land
		// at GitPushUnconfigured anyway; explicit for readability.
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": string(topology.CloseModeGitPush)},
	}, stateDir)
	if err != nil || result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"nextSteps"`) {
		t.Errorf("response missing nextSteps pointer: %s", body)
	}
	if !strings.Contains(body, `git-push-setup`) {
		t.Errorf("nextSteps should mention git-push-setup: %s", body)
	}
}

// TestHandleCloseMode_InvalidValue pins the value-validation gate:
// closeMode values outside the closed enum set are rejected with
// ErrInvalidParameter and the valid-set listing.
func TestHandleCloseMode_InvalidValue(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	result, _, _ := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": "auto-close"},
	}, stateDir)
	if !result.IsError {
		t.Fatal("expected error for invalid closeMode value")
	}
	body := getTextContent(t, result)
	for _, want := range []string{"Invalid closeMode", "auto-close", "auto, git-push, manual"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q: %s", want, body)
		}
	}
}

// TestHandleGitPushSetup_Confirm pins the confirm-mode happy path:
// passing service + remoteUrl writes GitPushState=configured + RemoteURL.
func TestHandleGitPushSetup_Confirm(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(WorkflowInput{
		Service:   "appdev",
		RemoteURL: "https://github.com/example/app.git",
	}, stateDir, runtime.Info{})
	if err != nil || result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState = %q, want configured", meta.GitPushState)
	}
	if meta.RemoteURL != "https://github.com/example/app.git" {
		t.Errorf("RemoteURL not persisted: %q", meta.RemoteURL)
	}
}

// TestHandleGitPushSetup_RejectsStageHostname pins the source-of-push
// gate: a stage-hostname target resolves to the dev-keyed meta but is
// not a push source — rejected with remediation pointing at the dev half.
func TestHandleGitPushSetup_RejectsStageHostname(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleGitPushSetup(WorkflowInput{
		Service:   "appstage",
		RemoteURL: "https://github.com/example/app.git",
	}, stateDir, runtime.Info{})
	if !result.IsError {
		t.Fatal("expected error for stage-hostname target")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "not a source-of-push") {
		t.Errorf("response should call out the stage-hostname rejection: %s", body)
	}
	if !strings.Contains(body, "appdev") {
		t.Errorf("response should redirect to dev hostname: %s", body)
	}
}

// TestHandleBuildIntegration_NeedsGitPushSetup pins the prereq-chain
// pre-check: setting integration=webhook on a service with
// GitPushState != configured returns the chained guidance pointer.
func TestHandleBuildIntegration_NeedsGitPushSetup(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationWebhook),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected guidance response, not error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"status":"needsGitPushSetup"`) {
		t.Errorf("response missing needsGitPushSetup: %s", body)
	}
	if !strings.Contains(body, "git-push-setup") {
		t.Errorf("response should chain to git-push-setup: %s", body)
	}
}

// TestHandleBuildIntegration_Configures pins the happy path: with
// GitPushState=configured, setting integration=webhook writes
// meta.BuildIntegration.
func TestHandleBuildIntegration_Configures(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), `"status":"configured"`) {
		t.Errorf("response missing status=configured: %s", getTextContent(t, result))
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.BuildIntegration != topology.BuildIntegrationActions {
		t.Errorf("BuildIntegration = %q, want actions", meta.BuildIntegration)
	}
}

// TestHandleGitPush_RejectsUnconfiguredState pins the missing R-state
// rejection unit test flagged by the Phase 4 Codex POST-WORK review:
// a service with GitPushState=unconfigured triggers gitPushMetaPreflight's
// ErrPrerequisiteMissing branch with a setup-pointer remediation.
func TestHandleGitPush_RejectsUnconfiguredState(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
		// GitPushState left unset — migrate at parseMeta lands at
		// GitPushUnconfigured given DeployStrategy is also empty.
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	captured := make([]string, 0, 1)
	recordAttempt := func(err string, _ topology.FailureClass) {
		captured = append(captured, err)
	}
	result := gitPushMetaPreflight(stateDir, "appdev", recordAttempt)
	if result == nil {
		t.Fatal("expected pre-flight rejection on unconfigured GitPushState")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "git-push not configured") {
		t.Errorf("error should call out unconfigured state: %s", body)
	}
	if !strings.Contains(body, "git-push-setup") {
		t.Errorf("error should redirect to git-push-setup: %s", body)
	}
	if len(captured) != 1 {
		t.Errorf("recordAttempt called %d times, want 1", len(captured))
	}
}

var _ = context.Background // keep import alive for future test additions
