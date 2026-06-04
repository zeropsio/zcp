// Tests for: ops/verify_checks.go::checkServiceRunning Recovery wiring
// (plan v4 §2.1). Pinned by these tests:
//   - READY_TO_DEPLOY with prior deploy/build history → events Recovery
//     (READ-FIRST: never an auto-destructive override — Wave-1 data-loss fix)
//   - FAILED status → events Recovery
//   - STOPPED status → no Recovery (intentional state, plan v4 out-of-scope)
//   - subdomain Recovery preserved when service_running fails (side fix)
package ops

import (
	"context"
	"net/http"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestCheckServiceRunning_ReadyToDeployAttachesRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "svc-1", Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				Status:               platform.ServiceStatusReadyToDeploy,
				SubdomainAccess:      true,
				Ports:                []platform.Port{{Port: 3000}},
			},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	running := findVerifyCheck(t, result.Checks, "service_running")
	if running.Status != CheckFail {
		t.Fatalf("service_running status = %q, want fail", running.Status)
	}
	if running.Recovery == nil {
		t.Fatalf("service_running Recovery missing on READY_TO_DEPLOY+failed history")
	}
	if running.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want zerops_events (read-first, never auto-destructive override — Wave-1 fix)", running.Recovery.Tool)
	}
}

func TestCheckServiceRunning_FailedAttachesRecovery(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "svc-1", Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				Status:               platform.ServiceStatusFailed,
				SubdomainAccess:      true,
				Ports:                []platform.Port{{Port: 3000}},
			},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	running := findVerifyCheck(t, result.Checks, "service_running")
	if running.Status != CheckFail {
		t.Fatalf("service_running status = %q, want fail", running.Status)
	}
	if running.Recovery == nil {
		t.Fatalf("service_running Recovery missing on FAILED status")
	}
	if running.Recovery.Tool != "zerops_events" {
		t.Errorf("Recovery.Tool = %q, want zerops_events", running.Recovery.Tool)
	}
	if running.Recovery.Args["serviceHostname"] != "app" {
		t.Errorf("Recovery.Args[serviceHostname] = %q", running.Recovery.Args["serviceHostname"])
	}
}

func TestCheckServiceRunning_StoppedNoRecovery(t *testing.T) {
	t.Parallel()
	// STOPPED is intentional state per v4 plan out-of-scope decision —
	// no Recovery hint (the agent restarts via zerops_manage if desired,
	// guidance for that lives in workflow status not in verify).
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "svc-1", Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				Status:               platform.ServiceStatusStopped,
				SubdomainAccess:      true,
				Ports:                []platform.Port{{Port: 3000}},
			},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	running := findVerifyCheck(t, result.Checks, "service_running")
	if running.Status != CheckFail {
		t.Fatalf("service_running status = %q, want fail", running.Status)
	}
	if running.Recovery != nil {
		t.Errorf("STOPPED should produce no Recovery, got %+v", running.Recovery)
	}
}

func TestVerify_PreservesSubdomainRecoveryWhenServiceNotRunning(t *testing.T) {
	t.Parallel()
	// Side fix per plan v4 §2.1: when service_running fails AND subdomain
	// access is also missing, surface the subdomain Recovery alongside the
	// service_running failure so the agent can address both in parallel.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "svc-1", Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				Status:               platform.ServiceStatusFailed,
				SubdomainAccess:      false,
				Ports:                []platform.Port{{Port: 3000}},
			},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	running := findVerifyCheck(t, result.Checks, "service_running")
	if running.Recovery == nil {
		t.Fatalf("service_running Recovery missing on FAILED")
	}

	httpRoot := findVerifyCheck(t, result.Checks, "http_root")
	if httpRoot.Status != CheckFail {
		t.Fatalf("http_root status = %q, want fail (so subdomain Recovery emits); got: %+v", httpRoot.Status, httpRoot)
	}
	if httpRoot.Recovery == nil {
		t.Fatalf("http_root Recovery missing — subdomain Recovery must surface even when service_running fails")
	}
	if httpRoot.Recovery.Tool != "zerops_subdomain" {
		t.Errorf("http_root Recovery.Tool = %q, want zerops_subdomain", httpRoot.Recovery.Tool)
	}
}

// findVerifyCheck returns a copy of the named CheckResult; t.Fatal on miss.
// Distinct from the verify_test.go helper that takes a *VerifyResult — keeps
// the recovery-test fixtures independent of that file's signature.
func findVerifyCheck(t *testing.T, checks []CheckResult, name string) CheckResult {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	got := make([]string, len(checks))
	for i, c := range checks {
		got[i] = c.Name
	}
	t.Fatalf("check %q not found in %v", name, got)
	return CheckResult{}
}
