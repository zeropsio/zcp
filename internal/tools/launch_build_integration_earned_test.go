package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// F1 — earned BuildIntegration. The declared enum (none/webhook/actions)
// records WHICH integration the user chose; BuildIntegrationVerifiedAt
// records the last EARNED verification. The launch gate's check 6 warns on
// declared-but-unverified instead of silently trusting the declaration.

// installFakeBuildIntegrationEarnProbe swaps the gate's earn probe for the
// test; returns results per push hostname.
func installFakeBuildIntegrationEarnProbe(t *testing.T, results map[string]bool) {
	t.Helper()
	prev := buildIntegrationEarnProbe
	buildIntegrationEarnProbe = func(_ context.Context, _ earnProbeDeps, pushHost string, _ *workflow.ServiceMeta) (bool, string) {
		return results[pushHost], "test-evidence"
	}
	t.Cleanup(func() { buildIntegrationEarnProbe = prev })
}

// TestValidateLaunchSourceControl_DeclaredButUnverified_WarnsNotSuppressed
// pins the core F1 invariant: BuildIntegration=actions with an empty
// VerifiedAt and a failing live earn probe DOES fire the
// build-integration-recommended warn — the declaration alone no longer
// suppresses it (the unearned-state bug class).
func TestValidateLaunchSourceControl_DeclaredButUnverified_WarnsNotSuppressed(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL,
		func(m *workflow.ServiceMeta) { m.BuildIntegrationVerifiedAt = "" })
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	installFakeBuildIntegrationEarnProbe(t, map[string]bool{"app": false})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	var warn *topology.Blocker
	for i := range blockers {
		if strings.HasPrefix(blockers[i].ID, "build-integration-recommended-") {
			warn = &blockers[i]
		}
	}
	if warn == nil {
		t.Fatalf("declared-but-unverified BuildIntegration=actions must fire the warn blocker; got %+v", blockers)
	}
	if warn.Severity != topology.BlockerSeverityWarn {
		t.Errorf("severity: got %q want warn", warn.Severity)
	}
	if !strings.Contains(warn.Message, "declared") {
		t.Errorf("declared-unverified warn must name the declared-vs-verified distinction; got %q", warn.Message)
	}
}

// TestValidateLaunchSourceControl_VerifiedAtRecorded_SuppressesWarn pins
// the recorded-evidence happy path: a prior publish-side earn stamped
// VerifiedAt → no warn, no live probe needed.
func TestValidateLaunchSourceControl_VerifiedAtRecorded_SuppressesWarn(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	// Probe would fail — must not even be consulted.
	installFakeBuildIntegrationEarnProbe(t, map[string]bool{"app": false})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	for _, b := range blockers {
		if strings.HasPrefix(b.ID, "build-integration-recommended-") {
			t.Errorf("VerifiedAt-recorded meta must suppress the warn, got %+v", b)
		}
	}
}

// TestValidateLaunchSourceControl_LiveEarnPasses_SuppressesWarn pins the
// live-earn path: declared + unstamped, but the live probe verifies →
// warn suppressed and the check carries the in-memory verified flag (the
// publish-side caller stamps from it; the read-side never mutates).
func TestValidateLaunchSourceControl_LiveEarnPasses_SuppressesWarn(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL,
		func(m *workflow.ServiceMeta) { m.BuildIntegrationVerifiedAt = "" })
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	installFakeBuildIntegrationEarnProbe(t, map[string]bool{"app": true})

	check, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	for _, b := range blockers {
		if strings.HasPrefix(b.ID, "build-integration-recommended-") {
			t.Errorf("live-earned integration must suppress the warn, got %+v", b)
		}
	}
	if check == nil || !check.BuildIntegrationVerified {
		t.Errorf("check.BuildIntegrationVerified must be true after a live earn; got %+v", check)
	}
}

// TestValidateLaunchSourceControl_EarnSkippedWhenPushProofFailed pins the
// sequencing dependency: the working-tree earn signal is trustworthy ONLY
// after the push-proof (clean tree + HEAD pushed). When earlier checks
// fail, the probe must NOT be consulted and the integration stays
// declared-unverified.
func TestValidateLaunchSourceControl_EarnSkippedWhenPushProofFailed(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL,
		func(m *workflow.ServiceMeta) { m.BuildIntegrationVerifiedAt = "" })
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	installFakePushProofReader(t, map[string]LaunchPushProofResult{
		"app": {DirtyTree: true, LocalHead: canonicalLaunchTestHeadSHA, RemoteHead: canonicalLaunchTestHeadSHA},
	})
	probeCalled := false
	prev := buildIntegrationEarnProbe
	buildIntegrationEarnProbe = func(_ context.Context, _ earnProbeDeps, _ string, _ *workflow.ServiceMeta) (bool, string) {
		probeCalled = true
		return true, "must-not-be-trusted"
	}
	t.Cleanup(func() { buildIntegrationEarnProbe = prev })

	check, _, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if probeCalled {
		t.Error("earn probe must NOT run when push-proof failed (file-stat unverifiable without pushed HEAD)")
	}
	if check != nil && check.BuildIntegrationVerified {
		t.Error("BuildIntegrationVerified must stay false when push-proof failed")
	}
}

// TestTrackTriggerMissingWarning_DeclaredUnverified pins the deploy-path
// parity: close-mode=git-push + declared-but-unverified integration keeps
// warning (the one diagnostic telling the user their push may trigger
// nothing); a VerifiedAt-stamped meta clears it.
func TestTrackTriggerMissingWarning_DeclaredUnverified(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL,
		func(m *workflow.ServiceMeta) {
			m.CloseDeployMode = topology.CloseModeGitPush
			m.BuildIntegrationVerifiedAt = ""
		})

	warn := trackTriggerMissingWarning(stateDir, "app")
	if warn == "" {
		t.Fatal("declared-but-unverified integration on close-mode=git-push must keep the trigger-missing warning")
	}
	if !strings.Contains(warn, "declared") {
		t.Errorf("warning must name the declared-vs-verified distinction; got %q", warn)
	}
}

func TestTrackTriggerMissingWarning_VerifiedClears(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL,
		func(m *workflow.ServiceMeta) { m.CloseDeployMode = topology.CloseModeGitPush })

	if warn := trackTriggerMissingWarning(stateDir, "app"); warn != "" {
		t.Errorf("VerifiedAt-stamped integration must clear the warning; got %q", warn)
	}
}

// TestHandleBuildIntegration_RemoteDriftWarning pins the live-origin
// drift check: when the push source's live origin differs (repo
// identity) from the recorded meta.RemoteURL, the actions confirm
// carries repoDriftWarning so the agent realigns via git-push-setup
// before running the owner/repo-derived gh commands.
func TestHandleBuildIntegration_RemoteDriftWarning(t *testing.T) {
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	installFakeLiveRemoteReader(t, map[string]string{"appdev": "https://github.com/example/OTHER.git"})

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: "actions",
	}, stateDir, runtime.Info{})
	body := getTextContent(t, result)
	if !strings.Contains(body, "repoDriftWarning") {
		t.Errorf("drifted live origin must surface repoDriftWarning: %s", body)
	}
}

// TestValidateLaunchSourceControl_ReadFailure_IsNotStateFailure pins the
// F2 read-vs-state split: an SSH/exec failure on the live-origin read
// surfaces as source-read-failed ("could not VERIFY ... transport"),
// never as remote-mismatch — a network outage must not hand the agent
// "fix your remote" instructions.
func TestValidateLaunchSourceControl_ReadFailure_IsNotStateFailure(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	prev := launchLiveRemoteReader
	launchLiveRemoteReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string) (string, error) {
		return "", errors.New("ssh app: connect: no route to host")
	}
	t.Cleanup(func() { launchLiveRemoteReader = prev })

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	var found *topology.Blocker
	for i := range blockers {
		if strings.HasPrefix(blockers[i].ID, "remote-mismatch-") {
			t.Errorf("read failure must NOT render as remote-mismatch: %+v", blockers[i])
		}
		if strings.HasPrefix(blockers[i].ID, "source-read-failed-") {
			found = &blockers[i]
		}
	}
	if found == nil {
		t.Fatalf("expected source-read-failed blocker, got %+v", blockers)
	}
	if !strings.Contains(found.Message, "no route to host") {
		t.Errorf("blocker must embed the read error; got %q", found.Message)
	}
	if !strings.Contains(found.Message, "NOT a confirmed remote mismatch") {
		t.Errorf("blocker must name the unverified-read-vs-mismatch distinction; got %q", found.Message)
	}
}
