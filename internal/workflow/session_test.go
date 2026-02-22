// Tests for: workflow session management — init, load, reset, iterate.
package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitSession_CreatesState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	state, err := InitSession(dir, "proj-1", ModeFull, "deploy my app")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if state.ProjectID != "proj-1" {
		t.Errorf("ProjectID: want proj-1, got %s", state.ProjectID)
	}
	if state.Mode != ModeFull {
		t.Errorf("Mode: want full, got %s", state.Mode)
	}
	if state.Phase != PhaseInit {
		t.Errorf("Phase: want INIT, got %s", state.Phase)
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

	// File should exist on disk.
	statePath := filepath.Join(dir, "zcp_state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file at %s: %v", statePath, err)
	}
}

func TestInitSession_ExistingSessionBlocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// First init should succeed.
	if _, err := InitSession(dir, "proj-1", ModeFull, "first"); err != nil {
		t.Fatalf("first InitSession: %v", err)
	}

	// Second init should fail.
	_, err := InitSession(dir, "proj-1", ModeFull, "second")
	if err == nil {
		t.Fatal("expected error for second InitSession with existing session")
	}
}

func TestLoadSession_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original, err := InitSession(dir, "proj-2", ModeDevOnly, "develop feature")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	loaded, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch: want %s, got %s", original.SessionID, loaded.SessionID)
	}
	if loaded.Mode != ModeDevOnly {
		t.Errorf("Mode: want dev_only, got %s", loaded.Mode)
	}
}

func TestLoadSession_NoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := LoadSession(dir)
	if err == nil {
		t.Fatal("expected error loading non-existent session")
	}
}

func TestResetSession_DeletesState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if _, err := InitSession(dir, "proj-3", ModeFull, "test"); err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	if err := ResetSession(dir); err != nil {
		t.Fatalf("ResetSession: %v", err)
	}

	statePath := filepath.Join(dir, "zcp_state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Error("expected state file to be removed after reset")
	}
}

func TestResetSession_NoExistingState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Reset with no state should not error.
	if err := ResetSession(dir); err != nil {
		t.Fatalf("ResetSession on empty dir: %v", err)
	}
}

func TestIterateSession_IncrementsCounter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")

	if _, err := InitSession(dir, "proj-4", ModeFull, "iterate test"); err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	// Save some evidence.
	ev := &Evidence{
		SessionID: "", Type: "dev_verify", VerificationType: "attestation",
	}
	// For iterate, we need to know the session ID.
	state, _ := LoadSession(dir)
	ev.SessionID = state.SessionID
	if err := SaveEvidence(evidenceDir, state.SessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	iterated, err := IterateSession(dir, evidenceDir)
	if err != nil {
		t.Fatalf("IterateSession: %v", err)
	}
	if iterated.Iteration != 1 {
		t.Errorf("Iteration: want 1, got %d", iterated.Iteration)
	}
	if iterated.Phase != PhaseDevelop {
		t.Errorf("Phase: want DEVELOP, got %s", iterated.Phase)
	}
}

func TestIterateSession_ArchivesEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")

	if _, err := InitSession(dir, "proj-5", ModeFull, "archive test"); err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	state, _ := LoadSession(dir)
	ev := &Evidence{
		SessionID: state.SessionID, Type: "discovery", VerificationType: "attestation",
	}
	if err := SaveEvidence(evidenceDir, state.SessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	if _, err := IterateSession(dir, evidenceDir); err != nil {
		t.Fatalf("IterateSession: %v", err)
	}

	// Evidence should be archived under iterations/1/.
	archivePath := filepath.Join(evidenceDir, state.SessionID, "iterations", "1", "discovery.json")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archived evidence at %s: %v", archivePath, err)
	}
}

func TestIterateSession_NoExistingState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")

	_, err := IterateSession(dir, evidenceDir)
	if err == nil {
		t.Fatal("expected error iterating non-existent session")
	}
}
