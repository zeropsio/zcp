// Tests for: deploy_local.go + deploy_ssh.go integration with the
// pre-flight gate (plan v4 §2.2). Exercises end-to-end behavior:
//   - deploy refuses on FAILED target with DeployGateError carrying Recovery
//   - deploy refuses on READY_TO_DEPLOY+failed-history with import Recovery
//   - deploy proceeds normally on RUNNING target (existing path)
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeployLocal_FailedServiceReturnsRecovery(t *testing.T) {
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

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err == nil {
		t.Fatal("expected DeployGateError on FAILED target, got nil")
	}
	var gateErr *DeployGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected DeployGateError, got %T: %v", err, err)
	}
	if gateErr.Recovery == nil || gateErr.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery = %+v, want zerops_events", gateErr.Recovery)
	}
}

func TestDeployLocal_ReadyToDeployFreshLetsThrough(t *testing.T) {
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
		t.Fatalf("first-deploy READY_TO_DEPLOY should pass: %v", err)
	}
	if result == nil || result.Status != "BUILD_TRIGGERED" {
		t.Errorf("result = %+v, want Status=BUILD_TRIGGERED", result)
	}
}

func TestDeployLocal_ReadyToDeployAfterFailureRefuses(t *testing.T) {
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

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err == nil {
		t.Fatal("expected gate to fire on READY_TO_DEPLOY+failed history")
	}
	var gateErr *DeployGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected DeployGateError, got %T", err)
	}
	if gateErr.Recovery == nil || gateErr.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery = %+v, want zerops_events (read-first; never an auto-destructive override on a service with code — Wave-1 fix)", gateErr.Recovery)
	}
}
