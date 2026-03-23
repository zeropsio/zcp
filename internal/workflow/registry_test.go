package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

func TestPruneDeadSessions(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Hour).Format(time.RFC3339)
	borderKeep := now.Add(-23*time.Hour - 59*time.Minute).Format(time.RFC3339)
	borderPrune := now.Add(-24*time.Hour - 1*time.Minute).Format(time.RFC3339)
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)

	livePID := os.Getpid()
	deadPID := 9999999

	tests := []struct {
		name    string
		input   []SessionEntry
		wantIDs []string
	}{
		{
			name:    "dead PID removed regardless of age",
			input:   []SessionEntry{{SessionID: "a", PID: deadPID, CreatedAt: fresh}},
			wantIDs: nil,
		},
		{
			name:    "live PID fresh session kept",
			input:   []SessionEntry{{SessionID: "a", PID: livePID, CreatedAt: fresh}},
			wantIDs: []string{"a"},
		},
		{
			name:    "live PID at 23h59m kept (TTL boundary)",
			input:   []SessionEntry{{SessionID: "a", PID: livePID, CreatedAt: borderKeep}},
			wantIDs: []string{"a"},
		},
		{
			name:    "live PID at 24h01m pruned (TTL boundary)",
			input:   []SessionEntry{{SessionID: "a", PID: livePID, CreatedAt: borderPrune}},
			wantIDs: nil,
		},
		{
			name: "mixed: dead PID + old + young",
			input: []SessionEntry{
				{SessionID: "dead", PID: deadPID, CreatedAt: fresh},
				{SessionID: "old", PID: livePID, CreatedAt: old},
				{SessionID: "young", PID: livePID, CreatedAt: fresh},
			},
			wantIDs: []string{"young"},
		},
		{
			name:    "malformed CreatedAt kept (parse error = keep)",
			input:   []SessionEntry{{SessionID: "a", PID: livePID, CreatedAt: "not-a-date"}},
			wantIDs: []string{"a"},
		},
		{
			name:    "empty CreatedAt kept",
			input:   []SessionEntry{{SessionID: "a", PID: livePID, CreatedAt: ""}},
			wantIDs: []string{"a"},
		},
		{
			name:    "empty input",
			input:   nil,
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Copy input to avoid slice aliasing from alive := sessions[:0].
			input := make([]SessionEntry, len(tt.input))
			copy(input, tt.input)

			got := pruneDeadSessions(input)
			gotIDs := make([]string, 0, len(got))
			for _, s := range got {
				gotIDs = append(gotIDs, s.SessionID)
			}
			if len(gotIDs) != len(tt.wantIDs) {
				t.Fatalf("pruneDeadSessions: got %v, want %v", gotIDs, tt.wantIDs)
			}
			for i, id := range gotIDs {
				if id != tt.wantIDs[i] {
					t.Errorf("pruneDeadSessions[%d]: got %s, want %s", i, id, tt.wantIDs[i])
				}
			}
		})
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

func TestListSessions_ReadOnly_NoSideEffects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a dead PID session directly.
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

	// ListSessions should return the dead session (no pruning).
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session (read-only, no pruning), got %d", len(sessions))
	}
	if sessions[0].SessionID != "dead-sess" {
		t.Errorf("SessionID: want dead-sess, got %s", sessions[0].SessionID)
	}

	// Registry file should still contain the dead session (no write-back).
	sessions2, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions second call: %v", err)
	}
	if len(sessions2) != 1 {
		t.Errorf("dead session should persist across calls (no pruning), got %d", len(sessions2))
	}
}

func TestClassifySessions_AliveAndDead(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{SessionID: "alive-1", PID: os.Getpid()},
		{SessionID: "dead-1", PID: 9999999},
		{SessionID: "alive-2", PID: os.Getpid()},
	}
	alive, dead := ClassifySessions(sessions)
	if len(alive) != 2 {
		t.Errorf("alive: want 2, got %d", len(alive))
	}
	if len(dead) != 1 {
		t.Errorf("dead: want 1, got %d", len(dead))
	}
	if dead[0].SessionID != "dead-1" {
		t.Errorf("dead[0].SessionID: want dead-1, got %s", dead[0].SessionID)
	}
}

func TestClassifySessions_AllAlive(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{SessionID: "a", PID: os.Getpid()},
		{SessionID: "b", PID: os.Getpid()},
	}
	alive, dead := ClassifySessions(sessions)
	if len(alive) != 2 {
		t.Errorf("alive: want 2, got %d", len(alive))
	}
	if len(dead) != 0 {
		t.Errorf("dead: want 0, got %d", len(dead))
	}
}

func TestClassifySessions_AllDead(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{SessionID: "x", PID: 9999999},
		{SessionID: "y", PID: 9999998},
	}
	alive, dead := ClassifySessions(sessions)
	if len(alive) != 0 {
		t.Errorf("alive: want 0, got %d", len(alive))
	}
	if len(dead) != 2 {
		t.Errorf("dead: want 2, got %d", len(dead))
	}
}

func TestClassifySessions_Empty(t *testing.T) {
	t.Parallel()
	alive, dead := ClassifySessions(nil)
	if len(alive) != 0 {
		t.Errorf("alive: want 0, got %d", len(alive))
	}
	if len(dead) != 0 {
		t.Errorf("dead: want 0, got %d", len(dead))
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

func TestLockFileExclusive_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lock timeout test in short mode")
	}
	t.Parallel()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".test.lock")

	// Hold an exclusive lock in a goroutine.
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer holder.Close()

	if err := lockFileExclusive(holder); err != nil {
		t.Fatalf("initial lock: %v", err)
	}
	defer unlockFile(holder)

	// Try to acquire from a second file descriptor — should timeout.
	contender, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open contender: %v", err)
	}
	defer contender.Close()

	start := time.Now()
	lockErr := lockFileExclusive(contender)
	elapsed := time.Since(start)

	if lockErr == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed < 3*time.Second {
		t.Errorf("expected timeout after ~5s, returned in %v", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}
