package workflow

import "testing"

// witness: internal/workflow/engine.go | CleanStaleWorkSessions(baseDir) → CleanStaleWorkSessions(baseDir); if sessions, _ := ListSessions(baseDir); len(sessions) == 1 { _, _ = e.Resume(sessions[0].SessionID) }
func TestBootstrapOwnership_SO1_ExplicitResumeAfterProcessExit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ids  []string
	}{
		{"one interrupted session", []string{"first"}},
		{"two interrupted sessions", []string{"first", "second"}},
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

			engine := NewEngine(dir, EnvContainer, nil)
			if engine.SessionID() != "" || ResolveLifecycle(dir, nil) != FocusIdle {
				t.Fatalf("new process inherited bootstrap %q", engine.SessionID())
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

// witness: internal/workflow/registry.go | if s.Workflow == WorkflowBootstrap { → if s.Workflow != WorkflowBootstrap {
func TestBootstrapOwnership_SO2_NewBootstrapKeepsInterruptedWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		createdAt string
	}{
		{"old", "2020-01-01T00:00:00Z"},
		{"newer", "2099-01-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			state := &WorkflowState{SessionID: "first", ProjectID: "project", Workflow: WorkflowBootstrap, Bootstrap: &BootstrapState{Active: true, Steps: []BootstrapStep{{Name: "discover", Status: "in_progress"}}}}
			if err := SaveSessionState(dir, "first", state); err != nil {
				t.Fatalf("save interrupted bootstrap: %v", err)
			}
			if err := RegisterSession(dir, SessionEntry{SessionID: "first", Workflow: WorkflowBootstrap, ProjectID: "project", CreatedAt: tt.createdAt}); err != nil {
				t.Fatalf("register interrupted bootstrap: %v", err)
			}
			other := NewEngine(dir, EnvContainer, nil)
			if _, err := other.Start("project", WorkflowBootstrap, "other conversation"); err != nil {
				t.Fatalf("start another bootstrap: %v", err)
			}
			original := NewEngine(dir, EnvContainer, nil)
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
