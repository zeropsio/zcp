// Tests for: ops/deploy_preflight_gate.go — pre-flight diagnose-before-
// destruct gate (plan v4 §2.2).
//
// Pinned cases:
//   - target FAILED → gate fires with Recovery → events
//   - target READY_TO_DEPLOY + failed appVersion → gate fires with Recovery → import
//   - target READY_TO_DEPLOY + no failed history → gate passes (first-deploy)
//   - target RUNNING / ACTIVE → gate passes
//   - target STOPPED → gate passes (intentional state, not our concern)
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestGateNonRunningOnDeploy_FailedReturnsRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusFailed},
		})

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusFailed}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected gate to fire, got nil Recovery")
	}
	if rec.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want zerops_events", rec.Tool)
	}
}

func TestGateNonRunningOnDeploy_ReadyToDeployFreshLetsThrough(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents(nil)

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusReadyToDeploy}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec != nil {
		t.Fatalf("first-deploy READY_TO_DEPLOY should pass; got Recovery %+v", rec)
	}
}

func TestGateNonRunningOnDeploy_ReadyToDeployAfterFailureRefuses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusReadyToDeploy}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected gate to fire on READY_TO_DEPLOY+failed history")
	}
	if rec.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want zerops_events (read-first, never auto-destructive override — Wave-1 fix)", rec.Tool)
	}
}

func TestGateNonRunningOnDeploy_RunningPasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusRunning},
		})

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusRunning}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec != nil {
		t.Fatalf("RUNNING should pass; got Recovery %+v", rec)
	}
}

func TestGateNonRunningOnDeploy_ActivePasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusActive},
		})

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusActive}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec != nil {
		t.Fatalf("ACTIVE should pass; got Recovery %+v", rec)
	}
}

func TestGateNonRunningOnDeploy_StoppedPasses(t *testing.T) {
	t.Parallel()
	// STOPPED is intentional state — deploy will fail downstream at the
	// zcli/SSH layer with a clearer signal, the gate doesn't intercept.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: platform.ServiceStatusStopped},
		})

	target := &platform.ServiceStack{ID: "svc-1", Name: "app", Status: platform.ServiceStatusStopped}
	rec, err := GateNonRunningOnDeploy(context.Background(), mock, nil, "p-1", target)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rec != nil {
		t.Fatalf("STOPPED is intentional; gate must not fire. Got %+v", rec)
	}
}
