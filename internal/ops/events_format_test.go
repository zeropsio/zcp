// Tests for: plans/analysis/ops.md § events — formatting and mapping
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestEvents_ActionNameMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		// Real API format (stack.* prefix, verified 2026-03-23).
		{"stack.start", "start"},
		{"stack.stop", "stop"},
		{"stack.restart", "restart"},
		{"stack.autoscaling", "scale"},
		{"stack.import", "import"},
		{"stack.delete", "delete"},
		{"stack.build", "build"},
		{"stack.userDataFile", "env-update"},
		{"stack.enableSubdomainAccess", "subdomain-enable"},
		{"stack.disableSubdomainAccess", "subdomain-disable"},
		{"unknownAction", "unknownAction"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := mapActionName(tt.input)
			if got != tt.want {
				t.Errorf("mapActionName(%s) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestEvents_DurationCalculation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		started  *string
		finished *string
		want     string
	}{
		{"nil_started", nil, strPtr("2024-01-01T00:01:00Z"), ""},
		{"nil_finished", strPtr("2024-01-01T00:00:00Z"), nil, ""},
		{"5_seconds", strPtr("2024-01-01T00:00:00Z"), strPtr("2024-01-01T00:00:05Z"), "5s"},
		{"2m30s", strPtr("2024-01-01T00:00:00Z"), strPtr("2024-01-01T00:02:30Z"), "2m30s"},
		{"1h15m", strPtr("2024-01-01T00:00:00Z"), strPtr("2024-01-01T01:15:00Z"), "1h15m"},
		// RFC3339Nano format (from fixed mappers).
		{"nano_5s", strPtr("2024-01-01T00:00:00.123456789Z"), strPtr("2024-01-01T00:00:05.987654321Z"), "5s"},
		{"nano_2m", strPtr("2024-01-01T00:00:00.5Z"), strPtr("2024-01-01T00:02:30.5Z"), "2m30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calcDuration(tt.started, tt.finished)
			if got != tt.want {
				t.Errorf("calcDuration() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEvents_StatusHints(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
	}

	tests := []struct {
		name     string
		process  *platform.ProcessEvent
		appVer   *platform.AppVersionEvent
		wantHint string
	}{
		{
			name: "appVersion ACTIVE uppercase",
			appVer: &platform.AppVersionEvent{
				ID: "av1", ServiceStackID: "svc-1", Status: statusActive,
				Created: "2024-01-01T00:01:00Z",
			},
			wantHint: "DEPLOYED: App version is deployed and running. Build pipeline complete. No further polling needed.",
		},
		{
			name: "appVersion active lowercase",
			appVer: &platform.AppVersionEvent{
				ID: "av2", ServiceStackID: "svc-1", Status: "active",
				Created: "2024-01-01T00:02:00Z",
			},
			wantHint: "DEPLOYED: App version is deployed and running. Build pipeline complete. No further polling needed.",
		},
		{
			name: "appVersion BUILDING",
			appVer: &platform.AppVersionEvent{
				ID: "av3", ServiceStackID: "svc-1", Status: statusBuilding,
				Created: "2024-01-01T00:03:00Z",
			},
			wantHint: "IN_PROGRESS: Build is running. Continue polling.",
		},
		{
			name: "appVersion BUILD_FAILED",
			appVer: &platform.AppVersionEvent{
				ID: "av4", ServiceStackID: "svc-1", Status: statusBuildFailed,
				Created: "2024-01-01T00:04:00Z",
			},
			wantHint: "FAILED: Build failed. Read failureClass + description on this event and use zerops_logs serviceHostname={service} facility=application since=5m for the build container output. Don't re-call zerops_deploy until the cause is identified — re-running without a fix loops the failure.",
		},
		{
			name: "appVersion DEPLOYING",
			appVer: &platform.AppVersionEvent{
				ID: "av5", ServiceStackID: "svc-1", Status: "DEPLOYING",
				Created: "2024-01-01T00:05:00Z",
			},
			wantHint: "IN_PROGRESS: Deploy is running. Continue polling.",
		},
		{
			name: "process FINISHED",
			process: &platform.ProcessEvent{
				ID: "p1", ActionName: "stack.start", Status: statusFinished,
				Created:       "2024-01-01T00:06:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "COMPLETE: Process finished successfully.",
		},
		{
			name: "process RUNNING",
			process: &platform.ProcessEvent{
				ID: "p2", ActionName: "stack.start", Status: "RUNNING",
				Created:       "2024-01-01T00:07:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "IN_PROGRESS: Process still running.",
		},
		{
			name: "process FAILED",
			process: &platform.ProcessEvent{
				ID: "p3", ActionName: "stack.start", Status: statusFailed,
				Created:       "2024-01-01T00:08:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "FAILED: Process failed.",
		},
		{
			name: "process PENDING",
			process: &platform.ProcessEvent{
				ID: "p4", ActionName: "stack.start", Status: "PENDING",
				Created:       "2024-01-01T00:09:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "IN_PROGRESS: Process queued.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var processes []platform.ProcessEvent
			var appVersions []platform.AppVersionEvent
			if tt.process != nil {
				processes = []platform.ProcessEvent{*tt.process}
			}
			if tt.appVer != nil {
				appVersions = []platform.AppVersionEvent{*tt.appVer}
			}

			mock := platform.NewMock().
				WithServices(services).
				WithProcessEvents(processes).
				WithAppVersionEvents(appVersions)

			result, err := Events(context.Background(), mock, "proj-1", "", 50)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result.Events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(result.Events))
			}

			if result.Events[0].Hint != tt.wantHint {
				t.Errorf("hint = %q, want %q", result.Events[0].Hint, tt.wantHint)
			}
		})
	}
}

func TestEvents_InternalActionsFiltered(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{{ID: "svc-1", Name: "api"}}

	processes := []platform.ProcessEvent{
		{
			ID: "p1", ActionName: "stack.start", Status: statusFinished,
			Created:       "2024-01-01T00:01:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
		{
			ID: "p2", ActionName: "zL7Master.instanceL7BalancerConfigUpdate", Status: statusFinished,
			Created:       "2024-01-01T00:02:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, "proj-1", "", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event (internal filtered), got %d", len(result.Events))
	}
	if result.Events[0].Action != "start" {
		t.Errorf("expected action=start, got %s", result.Events[0].Action)
	}
}

func TestEvents_FailReasonPropagated(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{{ID: "svc-1", Name: "api"}}
	reason := "out of memory"

	processes := []platform.ProcessEvent{
		{
			ID: "p1", ActionName: "stack.start", Status: statusFailed,
			Created:       "2024-01-01T00:01:00Z",
			FailReason:    &reason,
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, "proj-1", "", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	if result.Events[0].FailReason != "out of memory" {
		t.Errorf("failReason = %q, want %q", result.Events[0].FailReason, "out of memory")
	}
}

func strPtr(s string) *string {
	return &s
}
