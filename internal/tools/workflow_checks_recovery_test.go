// Tests for: tools/workflow_checks.go status-rejection Recovery wiring
// (plan v4 §1.4). Pinned by these tests:
//   - READY_TO_DEPLOY with failed appVersion history → import override Recovery
//   - READY_TO_DEPLOY with no failed history → zerops_logs Recovery
//   - FAILED status → zerops_events Recovery
//   - Recovery propagates StepCheck → CheckWire round-trip
package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckServiceStatusAny_ReadyToDeployWithFailedAppVersion_RecoveryToImport(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: serviceStatusRunning},
			{ID: "s2", Name: "appstage", Status: serviceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s2", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: "standard", ExplicitStage: "appstage",
			},
		}},
	}

	checker := checkProvision(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}

	stage := findStatusCheck(t, result.Checks, "appstage_status")
	if stage.Status != "pass" {
		t.Fatalf("READY_TO_DEPLOY should be in the pass set for stage; check fails or got %q", stage.Status)
	}

	// Now make the stage status one that's NOT in the pass set so it rejects
	// and Recovery is attached.
	mockReject := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: serviceStatusRunning},
			{ID: "s2", Name: "appstage", Status: platform.ServiceStatusFailed},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s2", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})
	checker2 := checkProvision(mockReject, nil, "proj-1", nil)
	result2, err := checker2(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	stage2 := findStatusCheck(t, result2.Checks, "appstage_status")
	if stage2.Status != "fail" {
		t.Fatalf("expected fail for FAILED status, got %q", stage2.Status)
	}
	if stage2.Recovery == nil {
		t.Fatalf("expected Recovery on FAILED status, got nil")
	}
	if stage2.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want %q", stage2.Recovery.Tool, "zerops_events")
	}
}

func TestCheckServiceStatusAny_ReadyToDeployRejectFromRunningSet_AttachesImportRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: serviceStatusReadyToDeploy},
			{ID: "s2", Name: "db", Status: serviceStatusRunning},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		}).
		WithServiceEnv("s2", []platform.ServiceEnvVar{{Key: "connectionString", Content: "pg://..."}})

	// Dev runtime expected to be RUNNING/ACTIVE — READY_TO_DEPLOY rejects.
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: "standard",
			},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	dev := findStatusCheck(t, result.Checks, "appdev_status")
	if dev.Status != "fail" {
		t.Fatalf("expected fail for READY_TO_DEPLOY in running set, got %q", dev.Status)
	}
	if dev.Recovery == nil {
		t.Fatalf("expected Recovery on rejected dev runtime, got nil")
	}
	if dev.Recovery.Tool != "zerops_import" {
		t.Errorf("Recovery.Tool = %q, want %q (failed history → import override)", dev.Recovery.Tool, "zerops_import")
	}
	if dev.Recovery.Args["override"] != "true" {
		t.Errorf("Recovery.Args[override] = %q, want %q", dev.Recovery.Args["override"], "true")
	}
}

func TestCheckServiceStatusAny_ReadyToDeployNoFailedHistory_RecoveryToLogs(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: serviceStatusReadyToDeploy},
		}).
		WithAppVersionEvents(nil)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: "standard",
			},
		}},
	}

	checker := checkProvision(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	dev := findStatusCheck(t, result.Checks, "appdev_status")
	if dev.Status != "fail" {
		t.Fatalf("expected fail for READY_TO_DEPLOY when running expected, got %q", dev.Status)
	}
	if dev.Recovery == nil {
		t.Fatalf("expected Recovery on rejection, got nil")
	}
	if dev.Recovery.Tool != "zerops_logs" {
		t.Errorf("Recovery.Tool = %q, want %q (no failed history → logs)", dev.Recovery.Tool, "zerops_logs")
	}
}

func TestCheckServiceStatusAny_FailedStatus_RecoveryToEvents(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: platform.ServiceStatusFailed},
		})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: "standard",
			},
		}},
	}

	checker := checkProvision(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	dev := findStatusCheck(t, result.Checks, "appdev_status")
	if dev.Status != "fail" {
		t.Fatalf("expected fail for FAILED status, got %q", dev.Status)
	}
	if dev.Recovery == nil {
		t.Fatalf("expected Recovery on FAILED status, got nil")
	}
	if dev.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want %q", dev.Recovery.Tool, "zerops_events")
	}
	if dev.Recovery.Args["serviceHostname"] != "appdev" {
		t.Errorf("Recovery.Args[serviceHostname] = %q", dev.Recovery.Args["serviceHostname"])
	}
}

// TestCheckWire_RecoveryRoundTrip pins the StepCheck.Recovery →
// CheckWire.Recovery copy in WithChecks. Drift here would silently drop
// Recovery hints from error responses.
func TestCheckWire_RecoveryRoundTrip(t *testing.T) {
	t.Parallel()
	checks := []workflow.StepCheck{{
		Name:   "appdev_status",
		Status: "fail",
		Detail: "expected RUNNING, got FAILED",
		Recovery: &topology.Recovery{
			Tool:   "zerops_events",
			Action: "fetch",
			Args:   map[string]string{"serviceHostname": "appdev"},
		},
	}}

	wire := ErrorWire{}
	WithChecks("preflight", checks)(&wire)

	if len(wire.Checks) != 1 {
		t.Fatalf("expected 1 CheckWire, got %d", len(wire.Checks))
	}
	got := wire.Checks[0]
	if got.Recovery == nil {
		t.Fatalf("Recovery dropped during conversion")
	}
	if got.Recovery.Tool != "zerops_events" || got.Recovery.Action != "fetch" {
		t.Errorf("Recovery shape = %+v, want zerops_events/fetch", got.Recovery)
	}
	if got.Recovery.Args["serviceHostname"] != "appdev" {
		t.Errorf("Recovery.Args = %+v", got.Recovery.Args)
	}
}

// findStatusCheck returns a copy of the named StepCheck from a slice;
// t.Fatal when not found so the calling test fails fast instead of nil-
// deref. Distinct from the recipe-only `findCheck` (returns *StepCheck for
// in-place mutation) to avoid the redeclaration collision.
func findStatusCheck(t *testing.T, checks []workflow.StepCheck, name string) workflow.StepCheck {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in %v", name, checkStatusNames(checks))
	return workflow.StepCheck{}
}

func checkStatusNames(checks []workflow.StepCheck) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}
