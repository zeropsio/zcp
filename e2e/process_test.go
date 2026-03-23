//go:build e2e

// Tests for: e2e — zerops_process tool against live Zerops API.
//
// Verifies status lookup, response structure, timestamps, error handling.
// Uses real process IDs obtained from zerops_events.
//
// Prerequisites:
//   - ZCP_API_KEY set
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Process -v -timeout 120s

package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestE2E_Process_StatusLookup(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// Get a real process ID from events.
	eventsText := s.mustCallSuccess("zerops_events", map[string]any{"limit": 5})
	var events ops.EventsResult
	if err := json.Unmarshal([]byte(eventsText), &events); err != nil {
		t.Fatalf("parse events: %v", err)
	}

	var processID string
	for _, e := range events.Events {
		if e.ProcessID != "" {
			processID = e.ProcessID
			break
		}
	}
	if processID == "" {
		t.Skip("no process events found — cannot test process lookup")
	}

	// Look up the process.
	text := s.mustCallSuccess("zerops_process", map[string]any{
		"processId": processID,
	})

	var result ops.ProcessStatusResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse process result: %v", err)
	}

	// Verify structure.
	if result.ProcessID != processID {
		t.Errorf("processId=%q, want %q", result.ProcessID, processID)
	}
	if result.Status == "" {
		t.Error("status is empty")
	}
	if result.Action == "" {
		t.Error("actionName is empty")
	}

	// Verify timestamp is RFC3339.
	if !rfc3339Re.MatchString(result.Created) {
		t.Errorf("created=%q is not RFC3339", result.Created)
	}
	if result.Started != nil && !rfc3339Re.MatchString(*result.Started) {
		t.Errorf("started=%q is not RFC3339", *result.Started)
	}

	t.Logf("  Process %s: action=%s status=%s", result.ProcessID, result.Action, result.Status)
}

func TestE2E_Process_NotFound(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	result := s.callTool("zerops_process", map[string]any{
		"processId": "00000000-0000-0000-0000-000000000000",
	})

	if !result.IsError {
		t.Error("expected error for non-existent process ID")
	}

	text := getE2ETextContent(t, result)
	t.Logf("  Error response: %.200s", text)
}

func TestE2E_Process_EmptyID(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	result := s.callTool("zerops_process", map[string]any{
		"processId": "",
	})

	if !result.IsError {
		t.Error("expected error for empty process ID")
	}

	text := getE2ETextContent(t, result)
	var errResult struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(text), &errResult); err == nil {
		if errResult.Code != "INVALID_PARAMETER" {
			t.Errorf("error code=%q, want INVALID_PARAMETER", errResult.Code)
		}
	}
}

func TestE2E_Process_CancelTerminal(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// Get a finished process from events.
	eventsText := s.mustCallSuccess("zerops_events", map[string]any{"limit": 10})
	var events ops.EventsResult
	if err := json.Unmarshal([]byte(eventsText), &events); err != nil {
		t.Fatalf("parse events: %v", err)
	}

	var finishedID string
	for _, e := range events.Events {
		if e.ProcessID != "" && (e.Status == "FINISHED" || e.Status == "FAILED") {
			finishedID = e.ProcessID
			break
		}
	}
	if finishedID == "" {
		t.Skip("no terminal process found — cannot test cancel")
	}

	result := s.callTool("zerops_process", map[string]any{
		"processId": finishedID,
		"action":    "cancel",
	})

	if !result.IsError {
		t.Error("expected error when canceling terminal process")
	}

	text := getE2ETextContent(t, result)
	var errResult struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(text), &errResult); err == nil {
		if errResult.Code != "PROCESS_ALREADY_TERMINAL" {
			t.Errorf("error code=%q, want PROCESS_ALREADY_TERMINAL", errResult.Code)
		}
	}

	t.Logf("  Cancel terminal process: %s", text[:min(len(text), 200)])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
