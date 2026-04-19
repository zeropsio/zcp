package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Package workflow uses global state (os.Getpid, file-based work sessions),
// so test files here do NOT use t.Parallel(). See CLAUDE.md.

func TestWorkSession_SaveLoad_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ws   *WorkSession
	}{
		{
			name: "fresh session",
			ws:   NewWorkSession("proj-1", "container", "add login form", []string{"web", "api"}),
		},
		{
			name: "session with history",
			ws: &WorkSession{
				PID:            os.Getpid(),
				ProjectID:      "proj-1",
				Environment:    "container",
				Intent:         "add login",
				Services:       []string{"web"},
				CreatedAt:      time.Now().UTC().Format(time.RFC3339),
				LastActivityAt: time.Now().UTC().Format(time.RFC3339),
				Deploys: map[string][]DeployAttempt{
					"web": {{AttemptedAt: "2026-04-17T10:00:00Z", SucceededAt: "2026-04-17T10:01:00Z", Setup: "dev", Strategy: "push-dev"}},
				},
				Verifies: map[string][]VerifyAttempt{
					"web": {{AttemptedAt: "2026-04-17T10:02:00Z", PassedAt: "2026-04-17T10:02:30Z", Summary: "HTTP 200", Passed: true}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := SaveWorkSession(dir, tt.ws); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, err := LoadWorkSession(dir, tt.ws.PID)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got == nil {
				t.Fatal("load returned nil")
			}
			if got.Version != workSessionVersion {
				t.Errorf("version = %q, want %q", got.Version, workSessionVersion)
			}
			if got.Intent != tt.ws.Intent {
				t.Errorf("intent = %q, want %q", got.Intent, tt.ws.Intent)
			}
			if len(got.Services) != len(tt.ws.Services) {
				t.Errorf("services len = %d, want %d", len(got.Services), len(tt.ws.Services))
			}
		})
	}
}

func TestWorkSession_LoadMissing_ReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	ws, err := LoadWorkSession(dir, 99999)
	if err != nil {
		t.Fatalf("load missing: unexpected error: %v", err)
	}
	if ws != nil {
		t.Fatalf("load missing: want nil, got %+v", ws)
	}
}

func TestWorkSession_DeleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteWorkSession(dir, 12345); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	ws := NewWorkSession("p", "container", "test", []string{"w"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := DeleteWorkSession(dir, ws.PID); err != nil {
		t.Fatalf("delete present: %v", err)
	}
	if err := DeleteWorkSession(dir, ws.PID); err != nil {
		t.Fatalf("delete again: %v", err)
	}
}

func TestRecordDeployAttempt_AppendsAndCaps(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkSession("p", "container", "test", []string{"web"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	tests := []struct {
		name        string
		n           int
		wantHistory int
	}{
		{"one attempt", 1, 1},
		{"five attempts", 5, 5},
		{"at cap", workSessionMaxHist, workSessionMaxHist},
		{"over cap", workSessionMaxHist + 5, workSessionMaxHist},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset.
			ws := NewWorkSession("p", "container", "test", []string{"web"})
			if err := SaveWorkSession(dir, ws); err != nil {
				t.Fatalf("save: %v", err)
			}
			for i := 0; i < tt.n; i++ {
				err := RecordDeployAttempt(dir, "web", DeployAttempt{
					AttemptedAt: time.Now().UTC().Format(time.RFC3339),
					Error:       "build failed",
				})
				if err != nil {
					t.Fatalf("record: %v", err)
				}
			}
			loaded, err := LoadWorkSession(dir, os.Getpid())
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			got := len(loaded.Deploys["web"])
			if got != tt.wantHistory {
				t.Errorf("history len = %d, want %d", got, tt.wantHistory)
			}
		})
	}
}

func TestRecordDeployAttempt_NoSessionNoError(t *testing.T) {
	dir := t.TempDir()
	err := RecordDeployAttempt(dir, "web", DeployAttempt{AttemptedAt: "2026-04-17T10:00:00Z"})
	if err != nil {
		t.Fatalf("no session: want no error, got %v", err)
	}
}

// Recording for a hostname outside ws.Services must not pollute ws.Deploys —
// spec-work-session.md §7.5 single-source-of-truth.
func TestRecordDeployAttempt_OutOfScope_RejectedAndNotPolluted(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkSession("p", "container", "test", []string{"web"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	err := RecordDeployAttempt(dir, "api", DeployAttempt{AttemptedAt: "t"})
	if err == nil {
		t.Fatal("expected error for out-of-scope hostname, got nil")
	}

	loaded, _ := LoadWorkSession(dir, os.Getpid())
	if _, ok := loaded.Deploys["api"]; ok {
		t.Errorf("ws.Deploys must not contain out-of-scope hostname, got: %v", loaded.Deploys)
	}
}

func TestRecordVerifyAttempt_OutOfScope_RejectedAndNotPolluted(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkSession("p", "container", "test", []string{"web"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	err := RecordVerifyAttempt(dir, "api", VerifyAttempt{AttemptedAt: "t", Passed: true})
	if err == nil {
		t.Fatal("expected error for out-of-scope hostname, got nil")
	}

	loaded, _ := LoadWorkSession(dir, os.Getpid())
	if _, ok := loaded.Verifies["api"]; ok {
		t.Errorf("ws.Verifies must not contain out-of-scope hostname, got: %v", loaded.Verifies)
	}
}

// Negative control: in-scope hostname still records successfully.
func TestRecordDeployAttempt_InScope_Accepted(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkSession("p", "container", "test", []string{"web", "api"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := RecordDeployAttempt(dir, "api", DeployAttempt{AttemptedAt: "t"}); err != nil {
		t.Fatalf("in-scope deploy: %v", err)
	}
	loaded, _ := LoadWorkSession(dir, os.Getpid())
	if len(loaded.Deploys["api"]) != 1 {
		t.Errorf("want 1 deploy for api, got %d", len(loaded.Deploys["api"]))
	}
}

func TestEvaluateAutoClose(t *testing.T) {
	tests := []struct {
		name string
		ws   *WorkSession
		want bool
	}{
		{"nil", nil, false},
		{"no services", &WorkSession{Services: nil}, false},
		{
			"no deploys",
			&WorkSession{Services: []string{"web"}},
			false,
		},
		{
			"deploy failed",
			&WorkSession{
				Services: []string{"web"},
				Deploys:  map[string][]DeployAttempt{"web": {{AttemptedAt: "t", Error: "x"}}},
			},
			false,
		},
		{
			"deployed not verified",
			&WorkSession{
				Services: []string{"web"},
				Deploys:  map[string][]DeployAttempt{"web": {{AttemptedAt: "t", SucceededAt: "t"}}},
			},
			false,
		},
		{
			"deployed + verified",
			&WorkSession{
				Services: []string{"web"},
				Deploys:  map[string][]DeployAttempt{"web": {{AttemptedAt: "t", SucceededAt: "t"}}},
				Verifies: map[string][]VerifyAttempt{"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}}},
			},
			true,
		},
		{
			"one service done, another not",
			&WorkSession{
				Services: []string{"web", "api"},
				Deploys: map[string][]DeployAttempt{
					"web": {{AttemptedAt: "t", SucceededAt: "t"}},
					"api": {{AttemptedAt: "t", SucceededAt: "t"}},
				},
				Verifies: map[string][]VerifyAttempt{
					"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
				},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateAutoClose(tt.ws)
			if got != tt.want {
				t.Errorf("EvaluateAutoClose = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordDeployAttempt_TriggersAutoClose(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkSession("p", "container", "test", []string{"web"})
	// Pre-seed a passed verify so the final deploy trips auto-close.
	ws.Verifies = map[string][]VerifyAttempt{
		"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
	}
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	err := RecordDeployAttempt(dir, "web", DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		SucceededAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       "dev",
		Strategy:    "push-dev",
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	loaded, err := LoadWorkSession(dir, os.Getpid())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.CloseReason != CloseReasonAutoComplete {
		t.Errorf("closeReason = %q, want %q", loaded.CloseReason, CloseReasonAutoComplete)
	}
}

func TestMigrateRemoveLegacyWorkState(t *testing.T) {
	dir := t.TempDir()

	// Seed legacy artifacts.
	if err := os.WriteFile(filepath.Join(dir, "active_session"), []byte("oldid"), 0o600); err != nil {
		t.Fatalf("seed active_session: %v", err)
	}
	developDir := filepath.Join(dir, "develop")
	if err := os.MkdirAll(developDir, 0o755); err != nil {
		t.Fatalf("seed develop dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(developDir, "123.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed develop marker: %v", err)
	}

	MigrateRemoveLegacyWorkState(dir)

	if _, err := os.Stat(filepath.Join(dir, "active_session")); !os.IsNotExist(err) {
		t.Errorf("active_session not removed")
	}
	if _, err := os.Stat(developDir); !os.IsNotExist(err) {
		t.Errorf("develop dir not removed")
	}
}

func TestCleanStaleWorkSessions(t *testing.T) {
	dir := t.TempDir()

	// Seed current-PID session.
	live := NewWorkSession("p", "container", "alive", []string{"web"})
	if err := SaveWorkSession(dir, live); err != nil {
		t.Fatalf("save live: %v", err)
	}

	// Seed a dead-PID session by crafting a file directly.
	deadPID := 99999
	deadWS := &WorkSession{
		PID: deadPID, ProjectID: "p", Intent: "dead",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveWorkSession(dir, deadWS); err != nil {
		t.Fatalf("save dead: %v", err)
	}
	// Also add an entry to registry so cleanup can remove it.
	_ = RegisterSession(dir, SessionEntry{SessionID: WorkSessionID(deadPID), PID: deadPID, Workflow: WorkflowWork})

	CleanStaleWorkSessions(dir)

	if _, err := os.Stat(workSessionPath(dir, deadPID)); !os.IsNotExist(err) {
		t.Errorf("dead PID file not removed")
	}
	if _, err := os.Stat(workSessionPath(dir, os.Getpid())); err != nil {
		t.Errorf("live PID file removed: %v", err)
	}
}
