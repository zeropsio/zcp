// Tests for: ops/dev_server.go pre-spawn gate (plan v4 §2.3). Pinned by:
//   - dev_server start refuses on FAILED target with Recovery → events
//   - dev_server start refuses on READY_TO_DEPLOY+prior-history with
//     Recovery → events (READ-FIRST, never auto-destructive — Wave-1 fix)
//   - dev_server start passes on RUNNING target (existing path)
package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDevServer_FailedRefusesWithRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusFailed},
		})

	err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev")
	if err == nil {
		t.Fatal("expected DeployGateError on FAILED target")
	}
	var gateErr *DeployGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected DeployGateError, got %T: %v", err, err)
	}
	if gateErr.Recovery == nil || gateErr.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery = %+v, want zerops_events", gateErr.Recovery)
	}
}

func TestDevServer_ReadyToDeployWithFailedAppVersionRefusesWithRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev")
	if err == nil {
		t.Fatal("expected gate on READY_TO_DEPLOY+failed history")
	}
	var gateErr *DeployGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected DeployGateError, got %T", err)
	}
	if gateErr.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want zerops_events (read-first, never auto-destructive override — Wave-1 fix)", gateErr.Recovery.Tool)
	}
}

func TestDevServer_RunningPasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusRunning},
		})

	err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev")
	if err != nil {
		t.Fatalf("RUNNING target should pass: %v", err)
	}
}
