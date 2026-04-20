// Tests for: workflow session management — init, load, reset, iterate.
package workflow

import (
	"os"
	"testing"
)

func TestInitSession_CreatesState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-1", "bootstrap", "deploy my app")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if state.ProjectID != "proj-1" {
		t.Errorf("ProjectID: want proj-1, got %s", state.ProjectID)
	}
	if state.Intent != "deploy my app" {
		t.Errorf("Intent: want 'deploy my app', got %s", state.Intent)
	}
	if state.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if state.Version == "" {
		t.Error("expected non-empty Version")
	}
	if state.Iteration != 0 {
		t.Errorf("Iteration: want 0, got %d", state.Iteration)
	}
}

func TestInitSession_PerSessionFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-1", "bootstrap", "test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	// File should exist at sessions/{id}.json
	statePath := sessionFilePath(dir, state.SessionID)
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected session file at %s: %v", statePath, err)
	}
}

func TestInitSession_SetsPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-1", "bootstrap", "test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if state.PID != os.Getpid() {
		t.Errorf("PID: want %d, got %d", os.Getpid(), state.PID)
	}
}

func TestInitSession_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-1", "bootstrap", "test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session in registry, got %d", len(sessions))
	}
	if sessions[0].SessionID != state.SessionID {
		t.Errorf("registry SessionID mismatch: want %s, got %s", state.SessionID, sessions[0].SessionID)
	}
}

func TestLoadSessionByID_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original, err := InitSession(dir, "proj-2", "develop", "develop feature")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	loaded, err := LoadSessionByID(dir, original.SessionID)
	if err != nil {
		t.Fatalf("LoadSessionByID: %v", err)
	}
	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch: want %s, got %s", original.SessionID, loaded.SessionID)
	}
	if loaded.Workflow != "develop" {
		t.Errorf("Workflow: want develop, got %s", loaded.Workflow)
	}
}

func TestLoadSessionByID_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := LoadSessionByID(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error loading non-existent session")
	}
}

func TestResetSessionByID_DeletesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-3", "bootstrap", "test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	if err := ResetSessionByID(dir, state.SessionID); err != nil {
		t.Fatalf("ResetSessionByID: %v", err)
	}

	statePath := sessionFilePath(dir, state.SessionID)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Error("expected session file to be removed after reset")
	}
}

func TestResetSessionByID_Unregisters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-3", "bootstrap", "test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	if err := ResetSessionByID(dir, state.SessionID); err != nil {
		t.Fatalf("ResetSessionByID: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after reset, got %d", len(sessions))
	}
}

func TestResetSessionByID_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Reset with no state should not error.
	if err := ResetSessionByID(dir, "nonexistent"); err != nil {
		t.Fatalf("ResetSessionByID on empty dir: %v", err)
	}
}

func TestIterateSession_IncrementsCounter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-4", "bootstrap", "iterate test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	iterated, err := IterateSession(dir, state.SessionID)
	if err != nil {
		t.Fatalf("IterateSession: %v", err)
	}
	if iterated.Iteration != 1 {
		t.Errorf("Iteration: want 1, got %d", iterated.Iteration)
	}
}

func TestIterateSession_NoExistingState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := IterateSession(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error iterating non-existent session")
	}
}

// Bootstrap no longer iterates under Option A (infra-only, retry = hard stop
// escalated to the user) — only recipe sessions reset on IterateSession.

func TestIterateSession_WithoutBootstrap_StillWorks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-c03b", "develop", "no bootstrap test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	iterated, err := IterateSession(dir, state.SessionID)
	if err != nil {
		t.Fatalf("IterateSession: %v", err)
	}
	if iterated.Bootstrap != nil {
		t.Error("Bootstrap should remain nil when not set")
	}
	if iterated.Iteration != 1 {
		t.Errorf("Iteration: want 1, got %d", iterated.Iteration)
	}
}
