//go:build e2e

// Tests for: e2e — zerops_events tool against live Zerops API.
//
// Verifies response structure, timestamp format, action name mapping,
// service filtering, and summary counts.
//
// Prerequisites:
//   - ZCP_API_KEY set
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Events -v -timeout 120s

package e2e_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

var rfc3339Re = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

func TestE2E_Events_ProjectWide(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	text := s.mustCallSuccess("zerops_events", map[string]any{"limit": 10})

	var result ops.EventsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse events result: %v", err)
	}

	// Verify structure.
	if result.ProjectID == "" {
		t.Error("projectId is empty")
	}
	if result.Summary.Total != len(result.Events) {
		t.Errorf("summary.total=%d != len(events)=%d", result.Summary.Total, len(result.Events))
	}
	if result.Summary.Processes+result.Summary.Deploys != result.Summary.Total {
		t.Errorf("processes(%d)+deploys(%d) != total(%d)",
			result.Summary.Processes, result.Summary.Deploys, result.Summary.Total)
	}

	// Verify timestamps are RFC3339 (not Go default format).
	for i, e := range result.Events {
		if !rfc3339Re.MatchString(e.Timestamp) {
			t.Errorf("event[%d].timestamp=%q is not RFC3339", i, e.Timestamp)
		}
	}

	// Verify action names are mapped (not raw API format).
	knownMapped := map[string]bool{
		"start": true, "stop": true, "restart": true,
		"scale": true, "import": true, "delete": true,
		"build": true, "env-update": true,
		"subdomain-enable": true, "subdomain-disable": true,
	}
	for _, e := range result.Events {
		if e.Type == "process" && e.Action != "" {
			if !knownMapped[e.Action] {
				// Unknown action — may be unmapped. Log but don't fail.
				t.Logf("  unmapped action: %q (service=%s)", e.Action, e.Service)
			}
		}
	}

	// Verify no internal platform actions leaked through.
	for _, e := range result.Events {
		if e.Type == "process" && len(e.Action) > 0 && e.Action[0] == 'z' {
			t.Errorf("internal action leaked: %q", e.Action)
		}
	}

	t.Logf("  Events: %d total (%d process, %d deploy)", result.Summary.Total, result.Summary.Processes, result.Summary.Deploys)
}

func TestE2E_Events_ServiceFilter(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// Fetch events for a specific service.
	text := s.mustCallSuccess("zerops_events", map[string]any{
		"serviceHostname": "docs",
		"limit":           5,
	})

	var result ops.EventsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse events result: %v", err)
	}

	// All events should be for the requested service.
	for i, e := range result.Events {
		if e.Service != "docs" {
			t.Errorf("event[%d].service=%q, want 'docs'", i, e.Service)
		}
	}

	t.Logf("  Filtered events for 'docs': %d", len(result.Events))
}

func TestE2E_Events_LimitRespected(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	text := s.mustCallSuccess("zerops_events", map[string]any{"limit": 2})

	var result ops.EventsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse events result: %v", err)
	}

	if len(result.Events) > 2 {
		t.Errorf("expected at most 2 events, got %d", len(result.Events))
	}
}
