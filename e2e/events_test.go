//go:build e2e

// Tests for: e2e — zerops_events tool against live Zerops API.
//
// Lean e2e: proves the live platform emits the contracts our unit tests pin.
// Specifically: response unmarshals into ops.EventsResult, timestamps are
// RFC3339, and summary counts cohere with len(events). Action-name mapping,
// service filtering, limit handling, and internal-action filtering are
// pinned at the unit layer (internal/ops/events_format_test.go +
// internal/ops/events_timeline_test.go) — no need to duplicate them here.
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Events -v -timeout 60s

package e2e_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

var rfc3339Re = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

func TestE2E_Events_LivePlatformContract(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	text := s.mustCallSuccess("zerops_events", map[string]any{"limit": 10})

	var result ops.EventsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse events result: %v", err)
	}

	// Structural invariants that depend on live platform data shape.
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

	// RFC3339 format: pinned at parse level in events_format_test.go, but the
	// platform-side emission is what this test guards against (catches API
	// drift to a different timestamp format).
	for i, e := range result.Events {
		if !rfc3339Re.MatchString(e.Timestamp) {
			t.Errorf("event[%d].timestamp=%q is not RFC3339", i, e.Timestamp)
		}
	}

	t.Logf("  Events: %d total (%d process, %d deploy)", result.Summary.Total, result.Summary.Processes, result.Summary.Deploys)
}
