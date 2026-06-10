package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestLaunchLaunchedResponse_SurfacesWarnings pins the B1 fix: bundle
// composition warnings (e.g. the P5 unreferenced-managed-dep warning) are
// accumulated on launchBundle.Warnings, persisted to launchState.Warnings, and
// MUST reach the agent in the launched response. Before the fix the success
// path discarded them entirely, so a promoted-but-unreferenced managed dep
// warning silently vanished.
func TestLaunchLaunchedResponse_SurfacesWarnings(t *testing.T) {
	t.Parallel()
	state := &launchState{
		TargetProjectID:   "proj-prod-1",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunched,
		Warnings: []string{
			`managed service "db" is promoted but nothing references ${db_*} in run.envVariables or project envs`,
		},
	}

	result := launchLaunchedResponse(nil, state, "")
	text := getTextContent(t, result)
	if !strings.Contains(text, "db_") || !strings.Contains(text, "managed service") {
		t.Fatalf("launched response must surface bundle warnings; got: %s", text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse launched response: %v", err)
	}
	warns, _ := parsed["warnings"].([]any)
	if len(warns) == 0 {
		t.Fatalf("expected warnings[] in launched response, got: %v", parsed)
	}
}
