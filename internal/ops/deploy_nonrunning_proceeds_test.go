// Tests for: deploy_local.go + deploy_ssh.go — a corrective redeploy of a
// non-running target PROCEEDS (no refusal). This replaces the deleted
// deploy_preflight_gate + deploy_gate_integration tests after the
// category-error verdict (plans/deploy-gate-category-error-2026-06-04.md):
// a failed deploy is non-destructive (prior appVersion keeps serving), so
// blocking the corrective redeploy deadlocked recovery and pushed the agent
// into the destructive zerops_import override escape. These pin the FIX —
// FAILED and READY_TO_DEPLOY-with-failed-history both deploy, not refuse.
package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeployLocal_FailedTargetProceeds(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:                   "svc-1",
				Name:                 "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				Status:               platform.ServiceStatusFailed,
			},
		})

	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n"), 0o644)

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err != nil {
		t.Fatalf("FAILED target must redeploy (corrective, non-destructive), got refusal: %v", err)
	}
	if result == nil || result.Status != "BUILD_TRIGGERED" {
		t.Errorf("result = %+v, want Status=BUILD_TRIGGERED", result)
	}
}

func TestDeployLocal_ReadyToDeployFreshProceeds(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:                   "svc-1",
				Name:                 "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				Status:               platform.ServiceStatusReadyToDeploy,
			},
		})

	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n"), 0o644)

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err != nil {
		t.Fatalf("first-deploy READY_TO_DEPLOY should proceed: %v", err)
	}
	if result == nil || result.Status != "BUILD_TRIGGERED" {
		t.Errorf("result = %+v, want Status=BUILD_TRIGGERED", result)
	}
}

// TestDeployLocal_ReadyToDeployAfterFailureProceeds is the deadlock-fix
// regression guard: the post-BUILD_FAILED redeploy (the exact state the old
// gate refused byte-identically with no diagnosis exit) now PROCEEDS. If a
// future change re-introduces a non-running deploy refusal, this fails.
func TestDeployLocal_ReadyToDeployAfterFailureProceeds(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:                   "svc-1",
				Name:                 "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				Status:               platform.ServiceStatusReadyToDeploy,
			},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n"), 0o644)

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err != nil {
		t.Fatalf("corrective redeploy after BUILD_FAILED must proceed (no diagnose-gate deadlock), got: %v", err)
	}
	if result == nil || result.Status != "BUILD_TRIGGERED" {
		t.Errorf("result = %+v, want Status=BUILD_TRIGGERED", result)
	}
}
