package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
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

// TestReconcileAdoptedGitPush_IncompleteMeta_SkippedThenReconciledOnCompletion
// pins A4. On a FIRST adoption the reconcile runs at the discover step,
// against metas the plan has only just written — partial, no BootstrappedAt.
// IsComplete() is false for every one of them, so the pass reconciles nothing
// and the launch source-control gate later forces a git-push-setup re-run on
// a service that was already push-capable: the one normal path into the
// credential-rotation branch that can destroy a working token.
//
// The skip itself is right (a partial meta is not a runtime ZCP has finished
// adopting). What was missing is the second pass — at the point those metas
// become complete, the same reflect-and-report must run again and stamp them.
func TestReconcileAdoptedGitPush_IncompleteMeta_SkippedThenReconciledOnCompletion(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	// The shape writeProvisionMetas leaves behind: no BootstrappedAt.
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:      "appdev",
		StageHostname: "appstage",
		Mode:          topology.ModeStandard,
	}); err != nil {
		t.Fatalf("seed partial meta: %v", err)
	}
	client := platform.NewMock().
		WithServices(adoptReconcileServices).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "x"}})
	ssh := &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}
	rt := runtime.Info{InContainer: true}

	if got := reconcileAdoptedGitPush(context.Background(), client, ssh, rt, stateDir, adoptReconcileServices); len(got) != 0 {
		t.Fatalf("a partial meta must be skipped, got %v", got)
	}

	// Bootstrap finishes: writeBootstrapOutputs stamps BootstrappedAt.
	if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.BootstrappedAt = "2026-09-16T00:00:00Z"
		return nil
	}); err != nil {
		t.Fatalf("complete meta: %v", err)
	}

	got := reconcileAdoptedGitPush(context.Background(), client, ssh, rt, stateDir, adoptReconcileServices)
	if len(got) != 1 || got[0] != "appdev" {
		t.Fatalf("reconciled = %v, want [appdev] once the meta is complete", got)
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState = %q, want configured", meta.GitPushState)
	}
}

// TestReconcileGitPushOnBootstrapFinish is A4's call site: the pass that runs
// when the metas actually satisfy the reconcile's precondition. It fires only
// at the terminal step — a mid-bootstrap response would reconcile the same
// partial metas the discover pass already skipped — and it never fails a
// bootstrap.
func TestReconcileGitPushOnBootstrapFinish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    *workflow.BootstrapStepInfo
		rt         runtime.Info
		noSSH      bool
		wantStamp  bool
		wantReport bool
	}{
		{
			name:       "terminal, in container",
			rt:         runtime.Info{InContainer: true},
			wantStamp:  true,
			wantReport: true,
		},
		{
			name:    "mid-bootstrap does nothing",
			current: &workflow.BootstrapStepInfo{Name: "provision"},
			rt:      runtime.Info{InContainer: true},
		},
		{name: "local mode does nothing", rt: runtime.Info{}},
		{name: "no ssh does nothing", rt: runtime.Info{InContainer: true}, noSSH: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := adoptReconcileFixture(t)
			client := platform.NewMock().
				WithServices(adoptReconcileServices).
				WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "x"}})
			var ssh ops.SSHDeployer
			if !tt.noSSH {
				ssh = &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}
			}
			resp := &workflow.BootstrapResponse{Current: tt.current, Message: "done."}

			reconcileGitPushOnBootstrapFinish(context.Background(), client, ssh, tt.rt, "p1", stateDir, resp)

			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if got := meta.GitPushState == topology.GitPushConfigured; got != tt.wantStamp {
				t.Errorf("stamped = %v, want %v", got, tt.wantStamp)
			}
			if got := strings.Contains(resp.Message, "reconciled from live"); got != tt.wantReport {
				t.Errorf("reported = %v, want %v (message: %q)", got, tt.wantReport, resp.Message)
			}
		})
	}
}

// A nil response is the shape a failed BootstrapComplete leaves behind — the
// reconcile must not be what turns that into a panic.
func TestReconcileGitPushOnBootstrapFinish_NilResponse(t *testing.T) {
	t.Parallel()
	reconcileGitPushOnBootstrapFinish(
		context.Background(), platform.NewMock(), &routedSSH{},
		runtime.Info{InContainer: true}, "p1", t.TempDir(), nil,
	)
}
