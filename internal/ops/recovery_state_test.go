// Tests for: ops/recovery_state.go — ComputeRecoveryState, the single
// classification every recovery reader consumes (docs/spec-workflows.md §8
// "Recovery classification" R1).
package ops

import (
	"context"
	"strings"
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

// TestComputeRecoveryState_ArtifactAndContainerFacts_Table pins R2's
// artifact/container facts and the derived Then string (docs/spec-
// workflows.md §8 R2, live-verified 2026-09-14): a never-activated
// buildFromGit service (failed-init, built artifact, no container) points
// at the in-place appVersion redeploy; a never-activated git-provisioned
// build failure points at re-import with override; any failed shape that
// still has a container falls back to the plain non-gated redeploy; a
// healthy service carries no Then at all.
func TestComputeRecoveryState_ArtifactAndContainerFacts_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		status             string
		service            platform.ServiceStack
		serviceAppVersions []platform.AppVersionEvent
		wantShape          topology.RecoveryShape
		wantArtifactBuilt  bool
		wantHasContainer   bool
		wantGitProvisioned bool
		wantThenContains   string
		wantThenEmpty      bool
	}{
		{
			name:    "failed_init_deploy_failed_no_container",
			status:  platform.ServiceStatusReadyToDeploy,
			service: platform.ServiceStack{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
			serviceAppVersions: []platform.AppVersionEvent{
				{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2, Created: "2026-09-14T10:00:00Z"},
			},
			wantShape:          topology.RecoveryFailedInit,
			wantArtifactBuilt:  true,
			wantHasContainer:   false,
			wantGitProvisioned: true,
			wantThenContains:   "appVersion=latest",
		},
		{
			name:    "failed_build_git_no_container",
			status:  platform.ServiceStatusReadyToDeploy,
			service: platform.ServiceStack{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
			serviceAppVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Source: "GIT", Sequence: 1, Created: "2026-09-14T10:00:00Z"},
			},
			wantShape:          topology.RecoveryFailedBuild,
			wantArtifactBuilt:  false,
			wantHasContainer:   false,
			wantGitProvisioned: true,
			wantThenContains:   "override=true",
		},
		{
			name:   "failed_init_with_running_container",
			status: platform.ServiceStatusFailed,
			service: platform.ServiceStack{
				ID: "s1", Name: "api", Status: platform.ServiceStatusFailed,
				ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-0"},
			},
			serviceAppVersions: []platform.AppVersionEvent{
				{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2, Created: "2026-09-14T10:00:00Z"},
			},
			wantShape:          topology.RecoveryFailedInit,
			wantArtifactBuilt:  true,
			wantHasContainer:   true,
			wantGitProvisioned: true,
			wantThenContains:   "zerops_deploy targetService=api (never gated",
		},
		{
			name:          "healthy",
			status:        platform.ServiceStatusRunning,
			service:       platform.ServiceStack{ID: "s1", Name: "api", Status: platform.ServiceStatusRunning},
			wantShape:     topology.RecoveryHealthy,
			wantThenEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{tt.service}).
				// Seeds BOTH the ES-backed shape classifier (SearchAppVersions,
				// via WithAppVersionEvents) and the DIRECT R2 fact reader
				// (ListServiceAppVersions, via WithServiceAppVersions) with the
				// same settled history — the scenario this fixture models is a
				// service whose history has already settled, not one caught
				// mid-ES-lag.
				WithAppVersionEvents(tt.serviceAppVersions).
				WithServiceAppVersions("s1", tt.serviceAppVersions)

			state, err := ComputeRecoveryState(context.Background(), client, nil, "p-1", "api", tt.status)
			if err != nil {
				t.Fatalf("ComputeRecoveryState: %v", err)
			}
			if state.Shape != tt.wantShape {
				t.Fatalf("Shape = %q, want %q", state.Shape, tt.wantShape)
			}
			if state.ArtifactBuilt != tt.wantArtifactBuilt {
				t.Errorf("ArtifactBuilt = %v, want %v", state.ArtifactBuilt, tt.wantArtifactBuilt)
			}
			if state.HasContainer != tt.wantHasContainer {
				t.Errorf("HasContainer = %v, want %v", state.HasContainer, tt.wantHasContainer)
			}
			if state.GitProvisioned != tt.wantGitProvisioned {
				t.Errorf("GitProvisioned = %v, want %v", state.GitProvisioned, tt.wantGitProvisioned)
			}
			if tt.wantThenEmpty {
				if state.Then != "" {
					t.Errorf("Then = %q, want empty", state.Then)
				}
				return
			}
			if state.Then == "" || !strings.Contains(state.Then, tt.wantThenContains) {
				t.Errorf("Then = %q, want containing %q", state.Then, tt.wantThenContains)
			}
		})
	}
}
