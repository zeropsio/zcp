package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func adoptReconcileFixture(t *testing.T) string {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.ModeStandard,
		BootstrappedAt: "2026-06-17T00:00:00Z", // IsComplete()=true
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	return stateDir
}

var adoptReconcileServices = []platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}

// TestReconcileAdoptedGitPush_StampsConfiguredWhenLiveOriginAndToken pins the
// #3 fix: an adopted service that already has a live origin AND a GIT_TOKEN
// secret is reflected as GitPushState=configured (so launch won't force a
// token-destroying git-push-setup re-run), reading the live truth via the same
// readers the launch gate uses.
func TestReconcileAdoptedGitPush_StampsConfiguredWhenLiveOriginAndToken(t *testing.T) {
	t.Parallel()
	stateDir := adoptReconcileFixture(t)
	client := platform.NewMock().
		WithServices(adoptReconcileServices).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "super-secret-token-value"}})
	ssh := &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}

	got := reconcileAdoptedGitPush(context.Background(), client, ssh, runtime.Info{InContainer: true}, stateDir, adoptReconcileServices)
	if len(got) != 1 || got[0] != "appdev" {
		t.Fatalf("reconciled = %v, want [appdev]", got)
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState = %q, want configured", meta.GitPushState)
	}
	if meta.RemoteURL != "https://github.com/me/app" {
		t.Errorf("RemoteURL = %q, want the live origin verbatim", meta.RemoteURL)
	}
	// Credential safety: the GIT_TOKEN value is presence-only — it must never
	// reach the meta.
	if meta.RemoteURL == "super-secret-token-value" || meta.BuildIntegration == "super-secret-token-value" {
		t.Error("token value leaked into meta")
	}
}

// TestReconcileAdoptedGitPush_NoRemote_LeavesUnconfigured: empty live origin →
// no stamp (reflect-and-report never fabricates).
func TestReconcileAdoptedGitPush_NoRemote_LeavesUnconfigured(t *testing.T) {
	t.Parallel()
	stateDir := adoptReconcileFixture(t)
	client := platform.NewMock().
		WithServices(adoptReconcileServices).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "x"}})
	ssh := &routedSSH{responses: map[string]string{"git remote get-url": ""}}

	if got := reconcileAdoptedGitPush(context.Background(), client, ssh, runtime.Info{InContainer: true}, stateDir, adoptReconcileServices); len(got) != 0 {
		t.Fatalf("reconciled = %v, want none (no remote)", got)
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta.GitPushState == topology.GitPushConfigured {
		t.Error("must not stamp configured without a live remote")
	}
}

// TestReconcileAdoptedGitPush_RemoteButNoToken_LeavesUnconfigured: origin
// present but no GIT_TOKEN secret → not git-push-configured outside ZCP.
func TestReconcileAdoptedGitPush_RemoteButNoToken_LeavesUnconfigured(t *testing.T) {
	t.Parallel()
	stateDir := adoptReconcileFixture(t)
	client := platform.NewMock().WithServices(adoptReconcileServices) // no GIT_TOKEN env
	ssh := &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}

	if got := reconcileAdoptedGitPush(context.Background(), client, ssh, runtime.Info{InContainer: true}, stateDir, adoptReconcileServices); len(got) != 0 {
		t.Fatalf("reconciled = %v, want none (no token)", got)
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta.GitPushState == topology.GitPushConfigured {
		t.Error("must not stamp configured without a GIT_TOKEN secret")
	}
}

// TestReconcileAdoptedGitPush_LocalMode_NoOp: local mode has no service-secret
// concept — the reconcile is a no-op (container-only).
func TestReconcileAdoptedGitPush_LocalMode_NoOp(t *testing.T) {
	t.Parallel()
	stateDir := adoptReconcileFixture(t)
	client := platform.NewMock().
		WithServices(adoptReconcileServices).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "x"}})
	ssh := &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}

	if got := reconcileAdoptedGitPush(context.Background(), client, ssh, runtime.Info{InContainer: false}, stateDir, adoptReconcileServices); got != nil {
		t.Fatalf("local mode must be a no-op, got %v", got)
	}
}
