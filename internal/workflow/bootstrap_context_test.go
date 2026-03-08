// Tests for: UpdateContextDelivery — persists knowledge tracking to bootstrap state.
package workflow

import (
	"testing"
)

func TestUpdateContextDelivery_ScopeLoaded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
	}{
		{"scope_flag_persisted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := NewEngine(dir)
			if _, err := engine.BootstrapStart("proj-1", "test intent"); err != nil {
				t.Fatalf("bootstrap start: %v", err)
			}

			err := engine.UpdateContextDelivery(func(cd *ContextDelivery) {
				cd.ScopeLoaded = true
			})
			if err != nil {
				t.Fatalf("UpdateContextDelivery: %v", err)
			}

			// Reload and verify persistence.
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("get state: %v", err)
			}
			if state.Bootstrap == nil || state.Bootstrap.Context == nil {
				t.Fatal("bootstrap context should exist after update")
			}
			if !state.Bootstrap.Context.ScopeLoaded {
				t.Error("ScopeLoaded should be true after update")
			}
		})
	}
}

func TestUpdateContextDelivery_BriefingFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		briefingFor string
	}{
		{"single_runtime", "nodejs@22"},
		{"runtime_plus_services", "nodejs@22+postgresql@16"},
		{"services_only", "postgresql@16+valkey@7.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := NewEngine(dir)
			if _, err := engine.BootstrapStart("proj-1", "test"); err != nil {
				t.Fatalf("bootstrap start: %v", err)
			}

			err := engine.UpdateContextDelivery(func(cd *ContextDelivery) {
				cd.BriefingFor = tt.briefingFor
			})
			if err != nil {
				t.Fatalf("UpdateContextDelivery: %v", err)
			}

			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("get state: %v", err)
			}
			if state.Bootstrap.Context.BriefingFor != tt.briefingFor {
				t.Errorf("BriefingFor: want %q, got %q", tt.briefingFor, state.Bootstrap.Context.BriefingFor)
			}
		})
	}
}

func TestUpdateContextDelivery_NoBootstrap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
	}{
		{"graceful_skip_no_bootstrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := NewEngine(dir)
			// Start a non-bootstrap session (no Bootstrap field).
			if _, err := engine.Start("proj-1", "deploy", "test"); err != nil {
				t.Fatalf("start: %v", err)
			}

			err := engine.UpdateContextDelivery(func(cd *ContextDelivery) {
				cd.ScopeLoaded = true
			})
			if err != nil {
				t.Errorf("should not error when no bootstrap state, got: %v", err)
			}
		})
	}
}
