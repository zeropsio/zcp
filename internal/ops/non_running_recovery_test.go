// Tests for: ops/non_running_recovery.go — NonRunningRecovery helper.
// Discriminates between READY_TO_DEPLOY-with-history (READ-FIRST via events —
// never a destructive override, Wave-1 data-loss fix), READY_TO_DEPLOY-clean
// (logs), FAILED (events), and intentional states (STOPPED/NEW → nil).
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestNonRunningRecovery_ReadyToDeployWithFailed_PointsAtEvents pins the Wave-1
// data-loss fix: a READY_TO_DEPLOY service whose build FAILED has buildFromGit
// code/config worth diagnosing — the recovery MUST read-first (zerops_events),
// NOT a destructive zerops_import override that would wipe the source under
// diagnosis. Override remains available only as a gated, explicit choice.
func TestNonRunningRecovery_ReadyToDeployWithFailed_PointsAtEvents(t *testing.T) {
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
	if rec.Tool != "zerops_events" {
		t.Errorf("Tool = %q, want %q — read-first, never a destructive override on a service with code", rec.Tool, "zerops_events")
	}
	if rec.Args["serviceHostname"] != "api" {
		t.Errorf("Args[serviceHostname] = %q, want %q", rec.Args["serviceHostname"], "api")
	}
	if rec.Args["override"] != "" {
		t.Errorf("recovery must NOT carry a destructive override arg, got override=%q", rec.Args["override"])
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
	// B7/NF3: no `facility` arg — zerops_logs has no such param (SDK rejects it).
	if _, ok := rec.Args["facility"]; ok {
		t.Errorf("Args must not carry the phantom `facility` key: %v", rec.Args)
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

// TestNonRunningRecovery_ReadyToDeployWithQueuedBuild_PointsAtEvents pins that a
// READY_TO_DEPLOY service with a queued/stalled appVersion (WAITING_TO_BUILD,
// no FailurePhaseFromStatus mapping) is treated as prior history and routed
// READ-FIRST to zerops_events. Before the Wave-1 fix this returned a
// destructive zerops_import override; reading the timeline first lets the agent
// see WHY it stalled before any reset (reset stays available, gated). The
// HasPriorDeployAttempt discriminator (any non-startWithoutCode appVersion,
// incl. queued — Karel's 2026-05-16 reproducer) still fires; only the chosen
// recovery action changed from destructive-reset to diagnose-first.
func TestNonRunningRecovery_ReadyToDeployWithQueuedBuild_PointsAtEvents(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-queued", ServiceStackID: "s1", Status: "WAITING_TO_BUILD", Source: "GIT_PUSH", Created: "2026-05-18T14:00:00Z"},
		})

	rec := NonRunningRecovery(context.Background(), client, nil, "p-1", "api", platform.ServiceStatusReadyToDeploy)
	if rec == nil {
		t.Fatalf("expected Recovery for READY_TO_DEPLOY+queued-deploy history, got nil")
	}
	if rec.Tool != "zerops_events" {
		t.Errorf("Tool = %q, want %q — read-first for any prior attempt, never auto-destructive", rec.Tool, "zerops_events")
	}
	if rec.Args["override"] != "" {
		t.Errorf("recovery must NOT carry a destructive override arg, got override=%q", rec.Args["override"])
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
