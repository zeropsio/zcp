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

	state, err := InitSession(dir, "proj-1", "bootstrap", "deploy my app")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if state.ProjectID != "proj-1" {
		t.Errorf("ProjectID: want proj-1, got %s", state.ProjectID)
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

	// Legacy file should NOT exist.
	legacyPath := filepath.Join(dir, legacyStateFile)
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("legacy zcp_state.json should not exist")
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

	original, err := InitSession(dir, "proj-2", "deploy", "develop feature")
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
	if loaded.Workflow != "deploy" {
		t.Errorf("Workflow: want deploy, got %s", loaded.Workflow)
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
	evidenceDir := filepath.Join(dir, "evidence")

	state, err := InitSession(dir, "proj-4", "bootstrap", "iterate test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	// Save some evidence.
	ev := &Evidence{
		SessionID: state.SessionID, Type: "dev_verify", VerificationType: "attestation",
	}
	if err := SaveEvidence(evidenceDir, state.SessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	iterated, err := IterateSession(dir, evidenceDir, state.SessionID)
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

	state, err := InitSession(dir, "proj-5", "bootstrap", "archive test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	ev := &Evidence{
		SessionID: state.SessionID, Type: "discovery", VerificationType: "attestation",
	}
	if err := SaveEvidence(evidenceDir, state.SessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	if _, err := IterateSession(dir, evidenceDir, state.SessionID); err != nil {
		t.Fatalf("IterateSession: %v", err)
	}

	// Evidence should be archived under iterations/1/.
	archivePath := filepath.Join(evidenceDir, state.SessionID, "iterations", "1", "discovery.json")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archived evidence at %s: %v", archivePath, err)
	}
}

func TestIterateSession_HistoryRecordsCorrectSourcePhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")

	state, err := InitSession(dir, "proj-6", "bootstrap", "history test")
	if err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	// Manually set the phase to VERIFY (simulating a session that advanced).
	state.Phase = PhaseVerify
	if err := saveSessionState(dir, state.SessionID, state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}

	// Save evidence so iterate doesn't fail.
	ev := &Evidence{
		SessionID: state.SessionID, Type: "stage_verify", VerificationType: "attestation",
	}
	if err := SaveEvidence(evidenceDir, state.SessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	iterated, err := IterateSession(dir, evidenceDir, state.SessionID)
	if err != nil {
		t.Fatalf("IterateSession: %v", err)
	}

	// The history entry should record From=VERIFY (the phase before iterate), not DEVELOP.
	if len(iterated.History) == 0 {
		t.Fatal("expected at least one history entry")
	}
	lastEntry := iterated.History[len(iterated.History)-1]
	if lastEntry.From != PhaseVerify {
		t.Errorf("History.From = %s, want VERIFY", lastEntry.From)
	}
	if lastEntry.To != PhaseDevelop {
		t.Errorf("History.To = %s, want DEVELOP", lastEntry.To)
	}
}

func TestIterateSession_NoExistingState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")

	_, err := IterateSession(dir, evidenceDir, "nonexistent")
	if err == nil {
		t.Fatal("expected error iterating non-existent session")
	}
}

func TestInitSession_CleansUpLegacyState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a legacy state file.
	legacyPath := filepath.Join(dir, legacyStateFile)
	if err := os.WriteFile(legacyPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("create legacy file: %v", err)
	}

	if _, err := InitSession(dir, "proj-1", "bootstrap", "test"); err != nil {
		t.Fatalf("InitSession: %v", err)
	}

	// Legacy file should be gone.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("legacy zcp_state.json should be removed")
	}
}
