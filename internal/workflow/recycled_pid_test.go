package workflow

import (
	"os"
	"testing"
)

// TestClassifySessions_RecycledPID_BucketsAsDead pins the registry-level effect
// of the two-state liveness check: an entry whose PID is alive (our own) but
// whose recorded StartTime is foreign is classified DEAD, so auto-recovery can
// reclaim the session ID instead of colliding with a wedged ghost.
func TestClassifySessions_RecycledPID_BucketsAsDead(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{SessionID: "live", PID: os.Getpid(), StartTime: CurrentProcessStartTime()},
		{SessionID: "recycled", PID: os.Getpid(), StartTime: "0.0-foreign-start"},
		{SessionID: "legacy-bare", PID: os.Getpid(), StartTime: ""},
	}
	alive, dead := ClassifySessions(sessions)

	aliveIDs := map[string]bool{}
	for _, s := range alive {
		aliveIDs[s.SessionID] = true
	}
	if !aliveIDs["live"] || !aliveIDs["legacy-bare"] {
		t.Errorf("matched-start and legacy-bare entries must be alive; alive=%v", aliveIDs)
	}
	if aliveIDs["recycled"] {
		t.Error("recycled-PID entry (foreign StartTime) must NOT be classified alive")
	}
	deadIDs := map[string]bool{}
	for _, s := range dead {
		deadIDs[s.SessionID] = true
	}
	if !deadIDs["recycled"] {
		t.Errorf("recycled-PID entry must be classified dead; dead=%v", deadIDs)
	}
}

// TestCurrentWorkSession_RecycledPID_TreatedAsAbsent pins that a work-session
// file keyed by our PID but stamped with a foreign StartTime is not returned as
// "our" session — a recycled PID must not inherit a dead predecessor's work.
func TestCurrentWorkSession_RecycledPID_TreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()

	// A work file for our PID with a foreign start-time (the predecessor's).
	ghost := NewWorkSession("proj", "container", "ghost intent", []string{"api"})
	ghost.StartTime = "0.0-foreign-start"
	if err := SaveWorkSession(dir, ghost); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}

	got, err := CurrentWorkSession(dir)
	if err != nil {
		t.Fatalf("CurrentWorkSession: %v", err)
	}
	if got != nil {
		t.Errorf("recycled-PID work file must be treated as absent; got intent=%q", got.Intent)
	}

	// A matched start-time IS our session.
	mine := NewWorkSession("proj", "container", "real intent", []string{"api"})
	if err := SaveWorkSession(dir, mine); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	got, err = CurrentWorkSession(dir)
	if err != nil {
		t.Fatalf("CurrentWorkSession: %v", err)
	}
	if got == nil || got.Intent != "real intent" {
		t.Errorf("matched-start work file must be returned; got=%v", got)
	}
}
