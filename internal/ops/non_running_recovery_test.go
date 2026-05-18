// Tests for: ops/non_running_recovery.go — NonRunningRecovery helper.
// Discriminates between READY_TO_DEPLOY-with-failed-history (override),
// READY_TO_DEPLOY-clean (logs), FAILED (events), and intentional states
// (STOPPED/NEW → nil).
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestNonRunningRecovery_ReadyToDeployWithFailed_PointsAtImport(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusReadyToDeploy)
	if rec == nil {
		t.Fatalf("expected Recovery for READY_TO_DEPLOY+failed history, got nil")
	}
	if rec.Tool != "zerops_import" {
		t.Errorf("Tool = %q, want %q", rec.Tool, "zerops_import")
	}
	if rec.Args["override"] != "true" {
		t.Errorf("Args[override] = %q, want %q", rec.Args["override"], "true")
	}
	if rec.Args["startWithoutCode"] != "true" {
		t.Errorf("Args[startWithoutCode] = %q, want %q", rec.Args["startWithoutCode"], "true")
	}
}

func TestNonRunningRecovery_ReadyToDeployNoFailedHistory_PointsAtLogs(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}}).
		WithAppVersionEvents(nil)

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusReadyToDeploy)
	if rec == nil {
		t.Fatalf("expected Recovery for never-deployed READY_TO_DEPLOY, got nil")
	}
	if rec.Tool != "zerops_logs" {
		t.Errorf("Tool = %q, want %q", rec.Tool, "zerops_logs")
	}
	if rec.Args["serviceHostname"] != "api" {
		t.Errorf("Args[serviceHostname] = %q", rec.Args["serviceHostname"])
	}
	if rec.Args["facility"] != "application" {
		t.Errorf("Args[facility] = %q, want %q", rec.Args["facility"], "application")
	}
}

func TestNonRunningRecovery_FailedStatus_PointsAtEvents(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusFailed)
	if rec == nil {
		t.Fatalf("expected Recovery for FAILED status, got nil")
	}
	if rec.Tool != "zerops_events" {
		t.Errorf("Tool = %q, want %q", rec.Tool, "zerops_events")
	}
	if rec.Args["serviceHostname"] != "api" {
		t.Errorf("Args[serviceHostname] = %q", rec.Args["serviceHostname"])
	}
}

func TestNonRunningRecovery_StoppedReturnsNil(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusStopped)
	if rec != nil {
		t.Fatalf("STOPPED is intentional state — expected nil Recovery, got %+v", rec)
	}
}

func TestNonRunningRecovery_NewReturnsNil(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusNew)
	if rec != nil {
		t.Fatalf("NEW is pre-deploy state — expected nil Recovery, got %+v", rec)
	}
}

func TestNonRunningRecovery_RunningReturnsNil(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusRunning)
	if rec != nil {
		t.Fatalf("RUNNING is healthy — expected nil Recovery, got %+v", rec)
	}
}

// TestNonRunningRecovery_ReadyToDeployWithQueuedBuild_PointsAtImport pins the
// Phase 2.2 discriminator broadening — services in READY_TO_DEPLOY with a
// queued/stalled appVersion (WAITING_TO_BUILD with no FailurePhaseFromStatus
// mapping) now correctly point at zerops_import. Previously the develop-adopt
// path fell through to zerops_logs because LatestFailedAppVersionContext
// filtered out the stalled state. Karel's 2026-05-16 launch reproducer hit
// exactly this case (WAITING_TO_BUILD with null pipelineStart). Symmetric to
// 33fb9358 (launch-production-side fix).
func TestNonRunningRecovery_ReadyToDeployWithQueuedBuild_PointsAtImport(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			// WAITING_TO_BUILD is a real lifecycle state but NOT a recognized
			// failure phase per FailurePhaseFromStatus. Before Phase 2.2 this
			// appVersion was silently filtered out and Recovery fell through to
			// zerops_logs. After: any non-startWithoutCode appVersion counts as
			// a prior deploy attempt → import is the right recovery.
			{ID: "av-queued", ServiceStackID: "s1", Status: "WAITING_TO_BUILD", Source: "GIT_PUSH", Created: "2026-05-18T14:00:00Z"},
		})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusReadyToDeploy)
	if rec == nil {
		t.Fatalf("expected Recovery for READY_TO_DEPLOY+queued-deploy history, got nil")
	}
	if rec.Tool != "zerops_import" {
		t.Errorf("Tool = %q, want %q — discriminator must point at import for any prior deploy attempt, including queued/stalled states", rec.Tool, "zerops_import")
	}
	if rec.Args["override"] != "true" {
		t.Errorf("Args[override] = %q, want %q", rec.Args["override"], "true")
	}
}

// TestHasPriorDeployAttempt_TableDriven pins the discriminator predicate
// across the relevant appVersion shapes. Phase 2.2 of fix-plan.
func TestHasPriorDeployAttempt_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		appVersions []platform.AppVersionEvent
		want        bool
	}{
		{
			name:        "no_history_returns_false",
			appVersions: nil,
			want:        false,
		},
		{
			name: "failed_build_returns_true",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Source: "GIT_PUSH"},
			},
			want: true,
		},
		{
			name: "queued_build_returns_true",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: "WAITING_TO_BUILD", Source: "GIT_PUSH"},
			},
			want: true,
		},
		{
			name: "active_build_returns_true",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: "ACTIVE", Source: "GIT_PUSH"},
			},
			want: true,
		},
		{
			name: "start_without_code_only_returns_false",
			appVersions: []platform.AppVersionEvent{
				// Source=NONE + Build=nil marks bootstrap startWithoutCode
				// stamps; these are not deploy attempts and must NOT trigger
				// import recovery.
				{ID: "av-bootstrap", ServiceStackID: "s1", Status: "ACTIVE", Source: "NONE"},
			},
			want: false,
		},
		{
			name: "different_service_returns_false",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "other-service", Status: platform.BuildStatusBuildFailed, Source: "GIT_PUSH"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}}).
				WithAppVersionEvents(tt.appVersions)
			got, err := HasPriorDeployAttempt(context.Background(), client, "p-1", "api")
			if err != nil {
				t.Fatalf("HasPriorDeployAttempt: %v", err)
			}
			if got != tt.want {
				t.Errorf("HasPriorDeployAttempt = %v, want %v", got, tt.want)
			}
		})
	}
}
