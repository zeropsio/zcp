package console

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
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
		"/", "/?kind=all&sort=cost&dir=asc", "/b/ui-current", "/b/ui-states", "/b/ui-states?verdict=blocked",
		"/r/ui-current-deploy", "/r/ui-current-deploy?steps=cited", "/r/ui-current-deploy?steps=errors",
		"/findings", "/terms", "/login", "/missing",
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

	// Give the principal run-detail fixture enough real evidence to exercise
	// long transcripts, filtered canonical links, JSON formatting and history.
	deployDir := "runs/ui-current-deploy/results/" + testResultsTS + "/deploy"
	transcript := make([]string, 0, 23)
	transcript = append(transcript,
		`{"type":"user","message":{"content":[{"type":"text","text":"Deploy the application and verify the first release."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"I will inspect the project and service state before deploying."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"fixture-discover","name":"zerops_discover","input":{"project":"p1","duplicate":1,"duplicate":2,"large":9007199254740993}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"fixture-discover","content":[{"type":"text","text":"discovered ok"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":""}]}}`,
	)
	for i := range 18 {
		transcript = append(transcript, fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"Evidence note %02d: verified the next deployment prerequisite and retained the original sequence number."}]}}`, i+1))
	}
	store.putText(t, deployDir+"/transcript.jsonl", strings.Join(transcript, "\n")+"\n")
	deployAt := now.Add(-2 * time.Hour)
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-current-deploy",
		ObsID: deployAt.Add(-10*time.Minute).Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: deployAt.Add(-10 * time.Minute), Status: "error",
		ErrorKind: observer.ErrorKindModel, Error: "fixture provider response ended before an assessment was stored",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-current-deploy",
		ObsID: deployAt.Add(20*time.Minute).Format("20060102T150405000Z") + "-claude-opus-5",
		Model: "claude-opus-5", CreatedAt: deployAt.Add(20 * time.Minute), Status: "error",
		ErrorKind: observer.ErrorKindModel, Error: "fixture model process exited before returning a complete assessment",
	})

	statesAt := now.Add(-time.Hour)
	stateRuns := []runFixture{
		{runID: "ui-states-failed", scenario: "failed-check", startedAt: statesAt, durationS: "22s", costUsd: 0.11, done: true, taskResult: farm.VerdictFailed, checks: [][5]string{{"deploy/status", "failed", "READY", "FAILED", "service detail"}}},
		{runID: "ui-states-not-finished", scenario: "not-finished", startedAt: statesAt, done: false},
		{runID: "ui-states-not-assessed", scenario: "not-assessed", startedAt: statesAt, durationS: "31s", costUsd: 0.09, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-problem", scenario: "passed-with-problem", startedAt: statesAt, durationS: "48s", costUsd: 0.14, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-clean", scenario: "clean", startedAt: statesAt, durationS: "16s", costUsd: 0.06, done: true, taskResult: farm.VerdictPassed},
	}
	seedBatchAt(t, store, "ui-states", "claude-sonnet-5", statesAt, stateRuns, true, map[string]string{
		"ui-states-failed": farm.VerdictFailed, "ui-states-not-finished": farm.VerdictNotRun,
		"ui-states-not-assessed": farm.VerdictPassed, "ui-states-problem": farm.VerdictPassed, "ui-states-clean": farm.VerdictPassed,
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-problem", ObsID: statesAt.Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(time.Minute), Status: "ok", Outcome: observer.OutcomeProblem, Headline: "The run passed, but the recovery path stayed unclear",
		Goal: observer.Goal{Reached: "partly"}, Findings: []observer.Finding{{Severity: observer.SeverityMedium, Owner: "zcp-tool", Surface: "tool:zerops_deploy", Title: "Recovery guidance is incomplete"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-clean", ObsID: statesAt.Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(time.Minute), Status: "ok", Outcome: observer.OutcomeOK, Headline: "The run completed without a recorded problem", Goal: observer.Goal{Reached: "yes"},
	})
	// Keep real transcript evidence but remove usage, producing a partially
	// known batch cost while the run remains meaningful and assessable.
	store.putJSON(t, "runs/ui-states-problem/results/"+testResultsTS+"/passed-with-problem/meta.json", map[string]any{
		"scenarioId": "passed-with-problem", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": statesAt.Format(time.RFC3339Nano), "duration": "48s", "evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": farm.VerdictPassed, "frozenAt": statesAt.Format(time.RFC3339Nano)},
	})

	unknownAt := now.Add(-3 * time.Hour)
	seedBatchAt(t, store, "ui-unknown-cost", "claude-sonnet-5", unknownAt, []runFixture{{
		runID: "ui-unknown-cost-a", scenario: "unknown-cost", startedAt: unknownAt, durationS: "25s", done: true, taskResult: farm.VerdictPassed,
	}}, true, map[string]string{"ui-unknown-cost-a": farm.VerdictPassed})
	store.putJSON(t, "runs/ui-unknown-cost-a/results/"+testResultsTS+"/unknown-cost/meta.json", map[string]any{
		"scenarioId": "unknown-cost", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": unknownAt.Format(time.RFC3339Nano), "duration": "25s", "evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": farm.VerdictPassed, "frozenAt": unknownAt.Format(time.RFC3339Nano)},
	})

	emptyAt := now.Add(-4 * time.Hour)
	seedBatchAt(t, store, "ui-empty", "off", emptyAt, []runFixture{{runID: "ui-empty-a", scenario: "never-started", startedAt: emptyAt, done: true, neverStarted: true}}, true, map[string]string{"ui-empty-a": farm.VerdictNotRun})
}
