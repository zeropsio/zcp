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
		{"serviceStackStart", "start"},
		{"serviceStackStop", "stop"},
		{"serviceStackRestart", "restart"},
		{"serviceStackAutoscaling", "scale"},
		{"serviceStackImport", "import"},
		{"serviceStackDelete", "delete"},
		{"serviceStackUserDataFile", "env-update"},
		{"serviceStackEnableSubdomainAccess", "subdomain-enable"},
		{"serviceStackDisableSubdomainAccess", "subdomain-disable"},
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
				ID: "av1", ServiceStackID: "svc-1", Status: "ACTIVE",
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
				ID: "av3", ServiceStackID: "svc-1", Status: "BUILDING",
				Created: "2024-01-01T00:03:00Z",
			},
			wantHint: "IN_PROGRESS: Build is running. Continue polling.",
		},
		{
			name: "appVersion BUILD_FAILED",
			appVer: &platform.AppVersionEvent{
				ID: "av4", ServiceStackID: "svc-1", Status: "BUILD_FAILED",
				Created: "2024-01-01T00:04:00Z",
			},
			wantHint: "FAILED: Build failed. Check build logs with zerops_logs severity=error.",
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
				ID: "p1", ActionName: "serviceStackStart", Status: "FINISHED",
				Created:       "2024-01-01T00:06:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "COMPLETE: Process finished successfully.",
		},
		{
			name: "process RUNNING",
			process: &platform.ProcessEvent{
				ID: "p2", ActionName: "serviceStackStart", Status: "RUNNING",
				Created:       "2024-01-01T00:07:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "IN_PROGRESS: Process still running.",
		},
		{
			name: "process FAILED",
			process: &platform.ProcessEvent{
				ID: "p3", ActionName: "serviceStackStart", Status: "FAILED",
				Created:       "2024-01-01T00:08:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
			},
			wantHint: "FAILED: Process failed.",
		},
		{
			name: "process PENDING",
			process: &platform.ProcessEvent{
				ID: "p4", ActionName: "serviceStackStart", Status: "PENDING",
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

func strPtr(s string) *string {
	return &s
}
