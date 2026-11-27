package workflow

import (
	"errors"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// witness: internal/workflow/engine.go | if !mate { → if true {
func TestBootstrapOwnership_SO1_ExplicitResumeAfterProcessExit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ids      []string
		mate     bool
		recovers bool
	}{
		{"Mate, one interrupted session", []string{"first"}, true, false},
		{"Mate, two interrupted sessions", []string{"first", "second"}, true, false},
		{"standalone, one interrupted session", []string{"first"}, false, true},
		{"standalone, two interrupted sessions", []string{"first", "second"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, id := range tt.ids {
				state := &WorkflowState{SessionID: id, ProjectID: "project", Workflow: WorkflowBootstrap, Bootstrap: &BootstrapState{Active: true, Steps: []BootstrapStep{{Name: "discover", Status: "in_progress"}}}}
				if err := SaveSessionState(dir, id, state); err != nil {
					t.Fatalf("save interrupted bootstrap: %v", err)
				}
				if err := RegisterSession(dir, SessionEntry{SessionID: id, Workflow: WorkflowBootstrap, ProjectID: "project"}); err != nil {
					t.Fatalf("register interrupted bootstrap: %v", err)
				}
			}

			var engine *Engine
			if tt.mate {
				engine = NewMateEngine(dir, EnvContainer, nil)
			} else {
				engine = NewEngine(dir, EnvContainer, nil)
			}
			if got := engine.SessionID(); (got == "first") != tt.recovers || (!tt.recovers && ResolveLifecycle(dir, nil) != FocusIdle) {
				t.Fatalf("startup session = %q, want recovery %v", got, tt.recovers)
			}
			state, err := engine.Resume("first")
			if err != nil {
				t.Fatalf("resume interrupted bootstrap: %v", err)
			}
			if state.SessionID != "first" || state.Bootstrap == nil || !state.Bootstrap.Active || state.Bootstrap.Steps[0].Name != "discover" || ResolveLifecycle(dir, nil) != FocusBootstrap {
				t.Fatalf("resumed bootstrap = %#v, focus = %v", state, ResolveLifecycle(dir, nil))
			}
		})
	}
}

// witness: internal/workflow/registry.go | if mate && s.Workflow == WorkflowBootstrap { → if !mate && s.Workflow == WorkflowBootstrap {
func TestBootstrapOwnership_SO2_NewBootstrapKeepsInterruptedWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		createdAt string
		mate      bool
		kept      bool
	}{
		{"Mate, old", "2020-01-01T00:00:00Z", true, true},
		{"Mate, newer", "2099-01-01T00:00:00Z", true, true},
		{"standalone, dead", "2099-01-01T00:00:00Z", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var other *Engine
			if tt.mate {
				other = NewMateEngine(dir, EnvContainer, nil)
			} else {
				other = NewEngine(dir, EnvContainer, nil)
			}
			state := &WorkflowState{SessionID: "first", ProjectID: "project", Workflow: WorkflowBootstrap, Bootstrap: &BootstrapState{Active: true, Steps: []BootstrapStep{{Name: "discover", Status: "in_progress"}}}}
			if err := SaveSessionState(dir, "first", state); err != nil {
				t.Fatalf("save interrupted bootstrap: %v", err)
			}
			if err := RegisterSession(dir, SessionEntry{SessionID: "first", Workflow: WorkflowBootstrap, ProjectID: "project", CreatedAt: tt.createdAt}); err != nil {
				t.Fatalf("register interrupted bootstrap: %v", err)
			}
			if _, err := other.Start("project", WorkflowBootstrap, "other conversation"); err != nil {
				t.Fatalf("start another bootstrap: %v", err)
			}
			_, err := LoadSessionByID(dir, "first")
			if !tt.kept {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("standalone interrupted session still exists: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("load interrupted session: %v", err)
			}
			original := NewMateEngine(dir, EnvContainer, nil)
			resumed, err := original.Resume("first")
			if err != nil {
				t.Fatalf("resume original bootstrap after another starts: %v", err)
			}
			if resumed.Bootstrap == nil || !resumed.Bootstrap.Active || resumed.Bootstrap.Steps[0].Name != "discover" {
				t.Fatalf("original progress = %#v", resumed.Bootstrap)
			}
		})
	}
}

// witness: internal/workflow/engine.go | if inRegistry && (e.mate || isProcessAlive(s.PID, s.StartTime)) { → if inRegistry && isProcessAlive(s.PID, s.StartTime) {
func TestBootstrapOwnership_SO3_HostnameLockSurvivesProcessExit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                        string
		mate, registered, live, own bool
		stage, locked               bool
	}{
		{"Mate, dead session", true, true, false, false, false, true},
		{"Mate, dead session's stage", true, true, false, false, true, true},
		{"Mate, live session", true, true, true, false, false, true},
		{"Mate, own session", true, true, false, true, false, false},
		{"Mate, removed session", true, false, false, false, false, false},
		{"standalone, dead session", false, true, false, false, false, false},
		{"standalone, live session", false, true, true, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var engine *Engine
			if tt.mate {
				engine = NewMateEngine(dir, EnvContainer, nil)
			} else {
				engine = NewEngine(dir, EnvContainer, nil)
			}
			if err := WriteServiceMeta(dir, &ServiceMeta{Hostname: "dev", Stages: []topology.StageRef{{Hostname: "stage"}}, BootstrapSession: "first"}); err != nil {
				t.Fatalf("write incomplete pair: %v", err)
			}
			if tt.registered {
				entry := SessionEntry{SessionID: "first", Workflow: WorkflowBootstrap}
				if tt.live {
					entry.PID, entry.StartTime = os.Getpid(), CurrentProcessStartTime()
				}
				if err := RegisterSession(dir, entry); err != nil {
					t.Fatalf("register bootstrap: %v", err)
				}
			}
			if tt.own {
				engine.setSessionID("first")
			}
			target := BootstrapTarget{Runtime: RuntimeTarget{DevHostname: "dev"}}
			if tt.stage {
				target.Runtime.DevHostname = "other"
				target.Runtime.BootstrapMode = topology.PlanModeStandard
				target.Runtime.Stages = []topology.StageRef{{Hostname: "stage"}}
			}
			err := engine.checkHostnameLocks([]BootstrapTarget{target})
			if (err != nil) != tt.locked {
				t.Fatalf("hostname lock error = %v, want locked %v", err, tt.locked)
			}
			if tt.mate && tt.locked && !tt.live {
				if err := UnregisterSession(dir, "first"); err != nil {
					t.Fatalf("remove session: %v", err)
				}
				if err := engine.checkHostnameLocks([]BootstrapTarget{target}); err != nil {
					t.Fatalf("hostname remains locked after session removal: %v", err)
				}
			}
		})
	}
}
