package console

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// TestUIFixture_Serve is an opt-in browser harness for the actual Handler.
// Run: ZCP_FARM_UI_FIXTURE=1 go test ./internal/eval/farm/console
// -run '^TestUIFixture_Serve$' -count=1 -timeout=0 -v
// Open http://localhost:8768/login and use testToken (printed on startup).
// This fixed-port development server is deliberately not parallel. It has no
// Worker, no credentials and no process invocation; assessment actions use a
// deterministic fake job. Normal tests skip it. Stop the command to shut down.
func TestUIFixture_Serve(t *testing.T) {
	if os.Getenv("ZCP_FARM_UI_FIXTURE") != "1" {
		t.Skip("set ZCP_FARM_UI_FIXTURE=1 to serve the local browser fixture")
	}
	store := newFakeStore()
	seedUIFixture(t, store)
	queue := NewQueue(func(context.Context, Job) error {
		return errors.New("fixture assessment deliberately stopped before storage")
	})
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: queue})
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:8768")
	if err != nil {
		t.Fatalf("listen on fixture loopback address: %v", err)
	}
	httpServer := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() {
		if err := httpServer.Close(); err != nil {
			t.Errorf("close fixture server: %v", err)
		}
	})
	t.Logf("Fixture: http://localhost:8768/login — local-only test token: %s", testToken)
	for _, route := range []string{
		"/problems", "/problems?status=all", "/problems?cause=platform", "/problems?scenario=missing", "/problems?bogus=1",
		"/", "/b/ui-current", "/r/ui-current-deploy", "/findings", "/terms", "/login", "/missing",
	} {
		t.Logf("route: http://localhost:8768%s", route)
	}
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve fixture: %v", err)
	}
}

// seedUIFixture records a small, realistic register and history. These are
// synthetic examples, not assertions about a deployed farm or platform.
func seedUIFixture(t *testing.T, store *fakeStore) {
	t.Helper()
	now := fixedNow(t)()
	cases := []struct{ scenario, severity, owner, surface, anchor, title, fix string }{
		{"deploy", "high", "zcp-guidance", "recipe:nodejs", "Start the runtime before deploying", "Deployment guidance skips the first application release", "Name the first deployment step after the runtime becomes ready."},
		{"import", "high", "zcp-tool", "tool:zerops_import", "IMPORT_SERVICE_CONFLICT", "A service-name conflict leaves the agent without a recovery path", "Return the conflicting service and the supported recovery action."},
		{"database", "high", "platform", "tool:zerops_deploy", "Waiting for service readiness", "Database readiness never settles after a successful import", "Inspect service readiness independently of the import result."},
		{"checks", "high", "evaluator", "check:readiness", "expected READY, observed PENDING", "The readiness check grades an incomplete platform response as failure", "Keep unavailable evidence separate from a proven failed check."},
		{"scope", "medium", "agent", "tool:zerops_discover", "project scope is required", "Agent repeats discovery without selecting the requested project", "Use the project returned by the first discovery call."},
		{"env", "medium", "zcp-guidance", "recipe:python", "Configure the connection string", "The connection-string example omits the managed database hostname", "Show the managed service hostname in the setup example."},
		{"port", "medium", "zcp-tool", "tool:zerops_deploy", "runtime port mismatch", "Port validation does not explain which setting to change", "Name the conflicting ports and the configuration field to edit."},
		{"build", "medium", "zcp-guidance", "recipe:go", "build artifacts are required", "The build recipe leaves the output path ambiguous", "Describe the exact build artifact path."},
		{"labels", "low", "scenario", "scenario:static-site", "Check the public page", "The scenario accepts an empty response as a working public page", "Check the expected page content as well as its status."},
		{"history", "low", "zcp-tool", "tool:zerops_discover", "activity still pending", "Completed activity remains labelled pending in the follow-up response", "Use the current activity status in follow-up output."},
	}
	for _, batch := range []struct {
		id, sha string
		age     time.Duration
		n       int
	}{
		{"ui-previous", "af00000000000000000000000000000000000000000000000000000000000001", 48 * time.Hour, 5},
		{"ui-current", "bc00000000000000000000000000000000000000000000000000000000000002", 2 * time.Hour, len(cases)},
	} {
		at := now.Add(-batch.age)
		runs := make([]runFixture, 0, batch.n)
		results := map[string]string{}
		for i, c := range cases[:batch.n] {
			id := batch.id + "-" + c.scenario
			verdict := farm.VerdictPassed
			var checks [][5]string
			if i < 4 {
				verdict = farm.VerdictFailed
				checks = [][5]string{{"readiness", "failed", "READY", "PENDING", "service snapshot"}}
			}
			runs = append(runs, runFixture{runID: id, scenario: c.scenario, startedAt: at, durationS: "6m20s", costUsd: 0.28 + float64(i)*0.08, done: true, taskResult: verdict, checks: checks})
			results[id] = verdict
		}
		seedBatchAt(t, store, batch.id, "claude-sonnet-5", at, runs, true, results)
		manifest := farm.BatchManifest{Batch: batch.id, CreatedAt: at.Format(time.RFC3339), StartedAt: at.Format(time.RFC3339), Set: "gate", CandidateSha256: batch.sha, EvaluatorSha256: "fixture-evaluator", Observer: "claude-sonnet-5"}
		for i, c := range cases[:batch.n] {
			id := batch.id + "-" + c.scenario
			manifest.Runs = append(manifest.Runs, farm.ManifestRun{RunID: id, Scenario: c.scenario, ProjectName: "zcp-farm-" + id})
			seedObservation(t, store, observer.Observation{
				FormatVersion: observer.ObservationFormat2, RunID: id, ObsID: at.Format("20060102T150405000Z") + "-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: at.Add(7 * time.Minute), Status: "ok", Outcome: observer.OutcomeProblem, Headline: c.title,
				Goal: observer.Goal{Reached: "partly", Why: "The run progressed, but the recorded issue remained."}, Checks: observer.Checks{Verdict: results[id], Agree: i != 3},
				Findings: []observer.Finding{{Severity: c.severity, Owner: c.owner, Surface: c.surface, Anchor: c.anchor, Title: c.title, What: "The recorded tool result did not give the agent enough information to finish the next step.", Fix: c.fix, Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}}}},
			})
		}
		store.putJSON(t, fmt.Sprintf("batches/%s/manifest.json", batch.id), manifest)
	}
}
