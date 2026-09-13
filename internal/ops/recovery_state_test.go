// Tests for: ops/recovery_state.go — ComputeRecoveryState, the single
// classification every recovery reader consumes (docs/spec-workflows.md §8
// "Recovery classification" R1).
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestComputeRecoveryState_Shapes_Table pins R1's shape set against the
// inputs that produce each one. Expected Shape/Next values are literals
// from R1/R2 — the oracle is the spec, not the implementation.
func TestComputeRecoveryState_Shapes_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		status         string
		appVersions    []platform.AppVersionEvent
		processes      []platform.Process
		wantShape      topology.RecoveryShape
		wantNextTool   string
		wantNextNil    bool
		wantLiveProcID string
	}{
		{
			name:        "healthy_running",
			status:      platform.ServiceStatusRunning,
			wantShape:   topology.RecoveryHealthy,
			wantNextNil: true,
		},
		{
			name:         "fresh_misconfigured_no_history",
			status:       platform.ServiceStatusReadyToDeploy,
			wantShape:    topology.RecoveryFreshMisconfigured,
			wantNextTool: "zerops_logs",
		},
		{
			name:   "failed_build",
			status: platform.ServiceStatusReadyToDeploy,
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			},
			wantShape:    topology.RecoveryFailedBuild,
			wantNextTool: "zerops_events",
		},
		{
			name:   "failed_init",
			status: platform.ServiceStatusReadyToDeploy,
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Created: "2026-05-05T10:00:00Z"},
			},
			wantShape:    topology.RecoveryFailedInit,
			wantNextTool: "zerops_events",
		},
		{
			name:   "stuck_building",
			status: platform.ServiceStatusReadyToDeploy,
			appVersions: []platform.AppVersionEvent{
				{ID: "av-q", ServiceStackID: "s1", Status: "WAITING_TO_BUILD", Source: "GIT_PUSH", Created: "2026-05-18T14:00:00Z"},
			},
			processes: []platform.Process{
				{ID: "p-failed", ActionName: actionStackBuild, Status: platform.ProcessStatusFailed, ServiceStacks: []platform.ServiceStackRef{{ID: "s1"}}},
			},
			wantShape:    topology.RecoveryStuckBuilding,
			wantNextTool: "zerops_events",
		},
		{
			name:   "live_build_not_failed",
			status: platform.ServiceStatusReadyToDeploy,
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			},
			processes: []platform.Process{
				{ID: "p-live", ActionName: actionStackBuild, Status: platform.ProcessStatusRunning, ServiceStacks: []platform.ServiceStackRef{{ID: "s1"}}},
			},
			wantShape:      topology.RecoveryHealthy,
			wantNextNil:    true,
			wantLiveProcID: "p-live",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: tt.status}}).
				WithAppVersionEvents(tt.appVersions).
				WithProjectProcesses(tt.processes)

			state, err := ComputeRecoveryState(context.Background(), client, nil, "p-1", "api", tt.status)
			if err != nil {
				t.Fatalf("ComputeRecoveryState: %v", err)
			}
			if state.Shape != tt.wantShape {
				t.Errorf("Shape = %q, want %q", state.Shape, tt.wantShape)
			}
			if tt.wantNextNil {
				if state.Next != nil {
					t.Errorf("Next = %+v, want nil", state.Next)
				}
			} else {
				if state.Next == nil {
					t.Fatalf("Next = nil, want Tool=%q", tt.wantNextTool)
				}
				if state.Next.Tool != tt.wantNextTool {
					t.Errorf("Next.Tool = %q, want %q", state.Next.Tool, tt.wantNextTool)
				}
			}
			if tt.wantLiveProcID != "" && state.LiveProcess != tt.wantLiveProcID {
				t.Errorf("LiveProcess = %q, want %q", state.LiveProcess, tt.wantLiveProcID)
			}
		})
	}
}
