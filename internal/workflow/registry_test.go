package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRegisterSession_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	entry := SessionEntry{
		SessionID: "sess-1",
		PID:       os.Getpid(),
		Workflow:  "bootstrap",
		ProjectID: "proj-1",
		Intent:    "deploy my app",
	}

	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %s", sessions[0].SessionID)
	}
	if sessions[0].CreatedAt == "" {
		t.Error("expected non-empty CreatedAt")
	}
}

func TestRegisterSession_Multiple(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, id := range []string{"sess-1", "sess-2", "sess-3"} {
		entry := SessionEntry{
			SessionID: id,
			PID:       os.Getpid(),
			Workflow:  "deploy",
			ProjectID: "proj-1",
		}
		if err := RegisterSession(dir, entry); err != nil {
			t.Fatalf("RegisterSession(%s): %v", id, err)
		}
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("want 3 sessions, got %d", len(sessions))
	}
}

func TestUnregisterSession_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	entry := SessionEntry{
		SessionID: "sess-1",
		PID:       os.Getpid(),
		Workflow:  "deploy",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	if err := UnregisterSession(dir, "sess-1"); err != nil {
		t.Fatalf("UnregisterSession: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after unregister, got %d", len(sessions))
	}
}

func TestUnregisterSession_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := UnregisterSession(dir, "nonexistent")
	if err != nil {
		t.Fatalf("UnregisterSession of nonexistent should not error: %v", err)
	}
}

func TestRefreshRegistry_PrunesDeadPIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register with a PID that certainly doesn't exist.
	entry := SessionEntry{
		SessionID: "dead-sess",
		PID:       9999999,
		Workflow:  "deploy",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	if err := RefreshRegistry(dir); err != nil {
		t.Fatalf("RefreshRegistry: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after pruning dead PID, got %d", len(sessions))
	}
}

func TestRefreshRegistry_KeepsLivePIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	entry := SessionEntry{
		SessionID: "live-sess",
		PID:       os.Getpid(),
		Workflow:  "deploy",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	if err := RefreshRegistry(dir); err != nil {
		t.Fatalf("RefreshRegistry: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("want 1 session (live PID), got %d", len(sessions))
	}
}

func TestListSessions_EmptyRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(sessions))
	}
}

func TestListSessions_AutoRefreshes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a dead PID session directly via withRegistryLock.
	entry := SessionEntry{
		SessionID: "dead-sess",
		PID:       9999999,
		Workflow:  "deploy",
		ProjectID: "proj-1",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	// ListSessions should auto-refresh and prune dead PID.
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after auto-refresh, got %d", len(sessions))
	}
}

func TestWithRegistryLock_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Run 10 goroutines concurrently registering sessions.
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entry := SessionEntry{
				SessionID: fmt.Sprintf("sess-%d", idx),
				PID:       os.Getpid(),
				Workflow:  "deploy",
				ProjectID: "proj-1",
			}
			if err := RegisterSession(dir, entry); err != nil {
				t.Errorf("RegisterSession(sess-%d): %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 10 {
		t.Errorf("want 10 sessions, got %d", len(sessions))
	}
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	t.Parallel()
	if !isProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	t.Parallel()
	if isProcessAlive(9999999) {
		t.Error("PID 9999999 should not be alive")
	}
}

func TestIsProcessAlive_ZeroPID(t *testing.T) {
	t.Parallel()
	if isProcessAlive(0) {
		t.Error("PID 0 should not be considered alive")
	}
}

func TestRefreshRegistry_NoRegistryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// RefreshRegistry on empty dir should not error.
	if err := RefreshRegistry(dir); err != nil {
		t.Fatalf("RefreshRegistry on empty dir: %v", err)
	}
}

func TestRefreshRegistry_CleansOrphanedSessionFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a session file without a registry entry.
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	orphanPath := filepath.Join(sessDir, "orphan123.json")
	if err := os.WriteFile(orphanPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RefreshRegistry(dir); err != nil {
		t.Fatalf("RefreshRegistry: %v", err)
	}

	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("orphaned session file should be removed, but still exists")
	}
}

func TestRefreshRegistry_KeepsLiveSessionFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a live session.
	entry := SessionEntry{
		SessionID: "live-sess",
		PID:       os.Getpid(),
		Workflow:  "deploy",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	// Create matching session file.
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sessFile := filepath.Join(sessDir, "live-sess.json")
	if err := os.WriteFile(sessFile, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RefreshRegistry(dir); err != nil {
		t.Fatalf("RefreshRegistry: %v", err)
	}

	// Session file should still exist.
	if _, err := os.Stat(sessFile); err != nil {
		t.Errorf("live session file should survive: %v", err)
	}
}

func TestRegistryFile_CreatedOnFirstRegister(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	entry := SessionEntry{
		SessionID: "sess-1",
		PID:       os.Getpid(),
		Workflow:  "deploy",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	registryPath := filepath.Join(dir, registryFileName)
	if _, err := os.Stat(registryPath); err != nil {
		t.Fatalf("expected registry file at %s: %v", registryPath, err)
	}
}
