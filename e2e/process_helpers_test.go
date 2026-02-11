//go:build e2e

// Tests for: e2e — process polling helpers for E2E tests.

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	maxPollAttempts = 40
	pollInterval    = 3 * time.Second
)

// waitForProcess polls zerops_process via MCP until the process reaches a terminal state.
func waitForProcess(s *e2eSession, processID string) {
	s.t.Helper()
	for i := 0; i < maxPollAttempts; i++ {
		text := s.mustCallSuccess("zerops_process", map[string]any{
			"processId": processID,
		})

		var status struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(text), &status); err != nil {
			s.t.Fatalf("parse process status: %v", err)
		}

		switch status.Status {
		case "FINISHED":
			return
		case "FAILED":
			s.t.Fatalf("process %s failed: %s", processID, text)
		case "CANCELED":
			s.t.Fatalf("process %s was canceled", processID)
		}

		time.Sleep(pollInterval)
	}
	s.t.Fatalf("process %s did not reach terminal state after %d attempts", processID, maxPollAttempts)
}

// waitForProcessDirect polls a process via direct API calls (not MCP).
// Used for cleanup where MCP session may not be available.
func waitForProcessDirect(ctx context.Context, client platform.Client, processID string) {
	for i := 0; i < maxPollAttempts; i++ {
		p, err := client.GetProcess(ctx, processID)
		if err != nil {
			return // best effort
		}
		switch p.Status {
		case "FINISHED", "FAILED", "CANCELED":
			return
		}
		time.Sleep(pollInterval)
	}
}

// parseProcesses parses a JSON array of process objects from import/delete tool responses.
func parseProcesses(t *testing.T, text string) []map[string]interface{} {
	t.Helper()
	var wrapper struct {
		Processes []map[string]interface{} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
		t.Fatalf("parse processes: %v", err)
	}
	return wrapper.Processes
}

// extractProcessID extracts the processId field from a JSON response.
// Works for manage/delete results that return a process object directly.
func extractProcessID(t *testing.T, text string) string {
	t.Helper()
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	id, ok := obj["id"].(string)
	if !ok {
		// Try processId field (used in ProcessStatusResult).
		id, ok = obj["processId"].(string)
		if !ok {
			t.Fatalf("no id or processId in: %s", text)
		}
	}
	return id
}

// findServiceByHostname searches for a service hostname in a discover response.
func findServiceByHostname(t *testing.T, discoverJSON string, hostname string) bool {
	t.Helper()
	var result struct {
		Services []struct {
			Hostname string `json:"hostname"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(discoverJSON), &result); err != nil {
		t.Fatalf("parse discover: %v", err)
	}
	for _, svc := range result.Services {
		if svc.Hostname == hostname {
			return true
		}
	}
	return false
}

// waitForServiceReady polls zerops_discover until a service appears with a non-empty status.
func waitForServiceReady(s *e2eSession, hostname string) {
	s.t.Helper()
	for i := 0; i < maxPollAttempts; i++ {
		text := s.mustCallSuccess("zerops_discover", nil)
		if findServiceByHostname(s.t, text, hostname) {
			// Service exists, check its status.
			var result struct {
				Services []struct {
					Hostname string `json:"hostname"`
					Status   string `json:"status"`
				} `json:"services"`
			}
			if err := json.Unmarshal([]byte(text), &result); err == nil {
				for _, svc := range result.Services {
					if svc.Hostname == hostname && svc.Status != "" {
						return
					}
				}
			}
		}
		time.Sleep(pollInterval)
	}
	s.t.Fatalf("service %s did not become ready after %d attempts", hostname, maxPollAttempts)
}

// logStep logs a numbered step in the E2E test for readability.
func logStep(t *testing.T, step int, msg string, args ...interface{}) {
	t.Helper()
	t.Logf("Step %d: %s", step, fmt.Sprintf(msg, args...))
}
