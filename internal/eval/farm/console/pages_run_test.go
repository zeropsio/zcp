// Package console: tests for pages_run.go, GET /r/<runId> (§8.3 FM-51's
// seven-part run page). Relocated here from pages_test.go (which stays the
// home for cross-cutting/shared-route tests) so this slice's tests live
// next to the file they pin, per plans/farm-console-clarity-2026-09-11-
// briefs/WAVE2.md's RUN write-set.
package console

import (
	"context"
	"errors"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// hrefFragmentPattern extracts every in-page "#<id>" href target, for
// TestPages_RunInPageLinksResolve's "every link lands on an element" check.
var hrefFragmentPattern = regexp.MustCompile(`href="#([^"]+)"`)

// TestPages_RunShowsQuoteFoundMarksAndUnverifiedCount pins §8.3's observer
// block: findings with an evidence link to "#s<n>" and a "quote found"/
// "quote not found" mark (§8.8 glossary's own term, not "verified"/
// "unverified"), plus the unverified-quote count (§7.6 FM-46). Independent
// oracle: fixtureObservation's own hand-authored evidence (api_test.go) —
// step 3 found, step 99 not found.
func TestPages_RunShowsQuoteFoundMarksAndUnverifiedCount(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "rb1", "claude-sonnet-5", []runFixture{
		{runID: "rb1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"rb1-scn": "passed"})
	seedObservation(t, store, fixtureObservation("rb1-scn"))

	rr := doGET(t, h, "/r/rb1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/rb1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, `href="#s3"`) {
		t.Errorf("body missing evidence link to #s3:\n%s", body)
	}
	if !strings.Contains(body, "quote found") {
		t.Errorf("body missing a 'quote found' mark:\n%s", body)
	}
	if !strings.Contains(body, "quote not found") {
		t.Errorf("body missing a 'quote not found' mark:\n%s", body)
	}
	if !strings.Contains(body, "1 of 2 quotes unverified") {
		t.Errorf("body missing the unverified-quote count (FM-46):\n%s", body)
	}
	if !strings.Contains(body, "Tool returned stale data") || !strings.Contains(body, "Agent skipped a sanity check") {
		t.Errorf("body missing finding titles:\n%s", body)
	}
	if !strings.Contains(body, `id="f1"`) || !strings.Contains(body, `id="f2"`) {
		t.Errorf("body missing F1/F2 finding anchors:\n%s", body)
	}
}

// TestPages_RunWithoutDoneNoAssessForm pins §8.5: the run page never
// renders the Assess form for a run without done.json — there is nothing
// yet to observe. It also carries no Record/Steps section (§8.3: "a run
// without done.json shows no Record").
func TestPages_RunWithoutDoneNoAssessForm(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "nd4", "claude-sonnet-5", []runFixture{
		{runID: "nd4-a", scenario: "a", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
	}, false, nil)

	rr := doGET(t, h, "/r/nd4-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/nd4-a: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, `action="/r/nd4-a/observe"`) {
		t.Errorf("run page rendered the Assess form for a run without done.json:\n%s", body)
	}
	if strings.Contains(body, `id="record"`) {
		t.Errorf("run page rendered Record for a run without done.json:\n%s", body)
	}
}

// TestPages_RunObserverFailureShowsCanonicalReason pins the review fix: a
// failed current observation reads the §8.8 vocabulary's own wording
// ("assessment failed — <reason>", RunRow.ObserverStateText), not an
// ad-hoc string — with no goal/checks/self-review pills and no "No
// findings" line for that failed observation.
func TestPages_RunObserverFailureShowsCanonicalReason(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ef1", "claude-sonnet-5", []runFixture{
		{runID: "ef1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ef1-scn": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ef1-scn", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "error", Error: "claude exited 1: boom",
	})

	rr := doGET(t, h, "/r/ef1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/ef1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "assessment failed — claude exited 1: boom") {
		t.Errorf("body missing the canonical failure wording:\n%s", body)
	}
	if strings.Contains(body, "pill-yes") || strings.Contains(body, "pill-no") || strings.Contains(body, "Goal reached") {
		t.Errorf("body still shows goal/checks/self-review pills for a failed observation:\n%s", body)
	}
	if strings.Contains(body, "No findings") {
		t.Errorf("body shows \"No findings\" for a failed observation:\n%s", body)
	}
}

// TestPages_RunUnparsedObservationShowsRawPreview pins an observation with
// status "unparsed": the canonical failure wording plus the first 2,000
// chars of raw in a collapsed block.
func TestPages_RunUnparsedObservationShowsRawPreview(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	rawAnswer := strings.Repeat("x", 2500)
	seedBatch(t, store, "up1", "claude-sonnet-5", []runFixture{
		{runID: "up1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"up1-scn": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "up1-scn", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "unparsed", Raw: rawAnswer,
	})

	rr := doGET(t, h, "/r/up1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/up1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := html.UnescapeString(rr.Body.String())

	if !strings.Contains(body, "assessment failed — the model's answer did not parse") {
		t.Errorf("body missing the canonical unparsed wording:\n%s", body)
	}
	if !strings.Contains(body, strings.Repeat("x", 2000)) {
		t.Errorf("body missing the first 2000 chars of raw:\n%s", body)
	}
	if strings.Contains(body, strings.Repeat("x", 2001)) {
		t.Errorf("body shows more than 2000 chars of raw:\n%s", body)
	}
	if !strings.Contains(body, "<details") {
		t.Errorf("raw preview is not inside a collapsed <details> block:\n%s", body)
	}
}

// TestPages_RunListsOlderObservationVersions pins §8.3's "earlier
// assessments" section: every obsId older than the current one is shown,
// and the current obsId is never printed a second time there.
func TestPages_RunListsOlderObservationVersions(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ov1", "claude-sonnet-5", []runFixture{
		{runID: "ov1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ov1-scn": "passed"})

	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ov1-scn", ObsID: "20260910T090000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), Status: "ok", Headline: "first pass, stale",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ov1-scn", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "current headline",
	})

	rr := doGET(t, h, "/r/ov1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/ov1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "current headline") {
		t.Errorf("body missing the current observation's headline:\n%s", body)
	}
	if !strings.Contains(body, "20260910T090000000Z-claude-sonnet-5") {
		t.Errorf("body missing the older obsId:\n%s", body)
	}
	if !strings.Contains(body, "first pass, stale") {
		t.Errorf("body missing the older observation's own headline:\n%s", body)
	}
	if n := strings.Count(body, "20260911T120000000Z-claude-sonnet-5"); n != 1 {
		t.Errorf("current obsId appears %d times, want exactly 1 (not also under older versions):\n%s", n, body)
	}
}

// TestPages_RunStepsHaveAnchorsAndFullText pins the step list: every step
// with anchor "s<n>", collapsible, full input and result — no truncation
// (unlike the observer's own digest, §7.3).
func TestPages_RunStepsHaveAnchorsAndFullText(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "st1", "off", []runFixture{
		{runID: "st1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"st1-scn": "passed"})

	rr := doGET(t, h, "/r/st1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/st1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	for _, want := range []string{`id="s1"`, `id="s2"`, `id="s3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing step anchor %s:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "do the thing for scn") {
		t.Errorf("body missing step 1's full task-prompt text:\n%s", body)
	}
	if !strings.Contains(body, "looking into it") {
		t.Errorf("body missing step 2's agent text:\n%s", body)
	}
	if !strings.Contains(body, "zerops_discover") || !strings.Contains(body, `{&#34;project&#34;:&#34;p1&#34;}`) {
		t.Errorf("body missing step 3's full tool input:\n%s", body)
	}
	if !strings.Contains(body, "discovered ok") {
		t.Errorf("body missing step 3's full tool result:\n%s", body)
	}
	if !strings.Contains(body, "<details") {
		t.Errorf("body has no <details> — steps must be collapsible:\n%s", body)
	}
}

// TestPages_RunShowsForensicCommand pins the exact local forensic command:
// "zcp eval farm pull <runId> --out <dir>" then
// "zcp capture ui <dir>/<runId>/capture".
func TestPages_RunShowsForensicCommand(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "fc1", "off", []runFixture{
		{runID: "fc1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fc1-scn": "passed"})

	rr := doGET(t, h, "/r/fc1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/fc1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "zcp eval farm pull fc1-scn --out") {
		t.Errorf("body missing the pull command:\n%s", body)
	}
	if !strings.Contains(body, "zcp capture ui") || !strings.Contains(body, "fc1-scn/capture") {
		t.Errorf("body missing the capture-ui command:\n%s", body)
	}
}

// TestPages_RunInPageLinksResolve pins §8.3 + FM-46: every in-page link on
// the run page lands on an element — evidence cites step 0 for a
// deterministic check row, so the failed-checks section must answer it,
// and a check id embedding "/" must still round-trip through href/id.
func TestPages_RunInPageLinksResolve(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "il1", "claude-sonnet-5", []runFixture{
		{runID: "il1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"decision/x", "failed", "never called", "called", "mcpstream"}}},
	}, true, map[string]string{"il1-scn": "failed"})
	obs := fixtureObservation("il1-scn")
	obs.Findings[0].Evidence = append(obs.Findings[0].Evidence, observer.Evidence{Step: 0, Quote: "decision/x failed", Verified: true})
	store.putJSON(t, "runs/il1-scn/observer/"+obs.ObsID+".json", obs)

	page := doGET(t, h, "/r/il1-scn")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /r/il1-scn: got %d, want 200", page.Code)
	}
	body := page.Body.String()
	hrefs := hrefFragmentPattern.FindAllStringSubmatch(body, -1)
	if len(hrefs) == 0 {
		t.Fatal("run page has no in-page links")
	}
	for _, m := range hrefs {
		if !strings.Contains(body, `id="`+m[1]+`"`) {
			t.Errorf("in-page link #%s has no element with that id", m[1])
		}
	}
}

// TestPages_RunHeaderShowsStalledLabel pins the review fix: a running run
// past its budget's 30-minute grace shows the "stalled" label and its
// vocabulary reason, never bare "running" (§8.1/§8.8).
func TestPages_RunHeaderShowsStalledLabel(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	old := fixedNow(t)().Add(-2 * time.Hour)
	seedBatch(t, store, "stl1", "claude-sonnet-5", []runFixture{
		{runID: "stl1-a", scenario: "a", startedAt: old, done: false},
	}, false, nil)
	store.putJSON(t, "batches/stl1/manifest.json", farm.BatchManifest{
		Batch: "stl1", CreatedAt: old.UTC().Format(time.RFC3339), StartedAt: old.UTC().Format(time.RFC3339),
		Set: "gate", CandidateSha256: "cand-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", RunBudgetSec: 60,
		Runs: []farm.ManifestRun{{RunID: "stl1-a", Scenario: "a", ProjectName: "zcp-farm-stl1-a"}},
	})

	body := html.UnescapeString(doGET(t, h, "/r/stl1-a").Body.String())
	if !strings.Contains(body, "stalled") {
		t.Errorf("body missing the stalled label:\n%s", body)
	}
	if !strings.Contains(body, "no result after the batch's deadline") {
		t.Errorf("body missing the stalled tooltip reason:\n%s", body)
	}
}

// TestPages_RunHeaderShowsNotStartedNotRawEnum pins §8.3: a not-started run
// (farm.VerdictNotRun = raw "not-run") shows the vocabulary label "not
// started", never the raw enum string "not-run" (§8.3: "never shows a raw
// enum").
func TestPages_RunHeaderShowsNotStartedNotRawEnum(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "nr1", "off", []runFixture{
		{runID: "nr1-a", scenario: "a", startedAt: fixedNow(t)(), done: false},
	}, true, map[string]string{"nr1-a": farm.VerdictNotRun})

	body := doGET(t, h, "/r/nr1-a").Body.String()
	if !strings.Contains(body, "not started") {
		t.Errorf("body missing the \"not started\" label:\n%s", body)
	}
	if strings.Contains(body, ">not-run<") {
		t.Errorf("body shows the raw enum \"not-run\":\n%s", body)
	}
}

// TestPages_RunAgentCostDashWhenUnknown pins §8.8's glossary: "Agent
// cost — … — when not recorded" — a run with no usage in meta.json shows
// "—", never a misleading "$0.00".
func TestPages_RunAgentCostDashWhenUnknown(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "cost1", "off", []runFixture{
		{runID: "cost1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"cost1-a": "passed"})
	// seedRun always writes a "usage" object (even a zero one); overwrite
	// meta.json without it to simulate a bundle that never recorded usage
	// (view.go: RunRow.CostKnown is true only when meta.json carries one).
	store.putJSON(t, "runs/cost1-a/results/"+testResultsTS+"/a/meta.json", map[string]any{
		"scenarioId": "a", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": fixedNow(t)().UTC().Format(time.RFC3339Nano), "duration": "5s",
		"evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": "passed", "frozenAt": fixedNow(t)().UTC().Format(time.RFC3339Nano)},
	})

	body := doGET(t, h, "/r/cost1-a").Body.String()
	if !strings.Contains(body, "· — ·") && !strings.Contains(body, "· —</p>") {
		t.Errorf("body missing the \"—\" unknown-cost marker:\n%s", body)
	}
	if strings.Contains(body, "$0.00") {
		t.Errorf("body shows \"$0.00\" for an unrecorded cost:\n%s", body)
	}
}

// TestPages_RunWhyThisVerdictListsFailedChecksWithLinks pins §8.3 part 2:
// "Failed because: <check id> — expected …, got …" per failed/blocked
// check, each linking to its row in the checks table.
func TestPages_RunWhyThisVerdictListsFailedChecksWithLinks(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "wv1", "off", []runFixture{
		{runID: "wv1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "failed", done: true,
			checks: [][5]string{{"c1", "failed", "5", "3", "mcp"}}},
	}, true, map[string]string{"wv1-a": "failed"})

	body := doGET(t, h, "/r/wv1-a").Body.String()
	if !strings.Contains(body, "Failed because:") {
		t.Errorf("body missing the \"Failed because\" line:\n%s", body)
	}
	if !strings.Contains(body, "expected 5, got 3") {
		t.Errorf("body missing the expected/got text:\n%s", body)
	}
	if !strings.Contains(body, `id="check-c1"`) {
		t.Errorf("body missing the checks-table row anchor:\n%s", body)
	}
	if !strings.Contains(body, `href="#check-c1"`) {
		t.Errorf("body missing the link to the checks-table row:\n%s", body)
	}
}

// buildFormat2Observation builds a format-2 observation with the given
// findings/judged checks, for the disputed-line tests below.
func buildFormat2Observation(runID string, findings []observer.Finding, judged []observer.JudgedCheck, agree bool) observer.Observation {
	return observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: runID, ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC), Status: "ok",
		Outcome: "problem", Headline: "OK-looking but disputed",
		Story:      &observer.Story{Task: "do it", Expected: "works", Did: "tried", Ending: "finished"},
		Goal:       observer.Goal{Reached: "yes", Why: "close enough"},
		Checks:     observer.Checks{Verdict: "failed", Agree: agree, Judged: judged},
		Findings:   findings,
		SelfReview: observer.SelfReview{Accurate: "yes"},
	}
}

// TestPages_RunDisputedLineLinksToEvaluatorFinding pins the review finding:
// the disputed line links to the explaining (evaluator-owned) finding.
func TestPages_RunDisputedLineLinksToEvaluatorFinding(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "dp1", "claude-sonnet-5", []runFixture{
		{runID: "dp1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "failed", done: true,
			checks: [][5]string{{"c1", "failed", "5", "3", "mcp"}}},
	}, true, map[string]string{"dp1-a": "failed"})
	obs := buildFormat2Observation("dp1-a", []observer.Finding{
		{Severity: "high", Owner: "evaluator", Title: "check c1 is wrong", What: "the check compares the wrong field",
			Evidence: []observer.Evidence{{Step: 1, Quote: "do the thing for a", Verified: true}}, LookAt: "checks.go", Fix: "fix the check"},
	}, nil, false)
	seedObservation(t, store, obs)

	body := doGET(t, h, "/r/dp1-a").Body.String()
	if !strings.Contains(body, "The observer thinks a check is wrong") {
		t.Errorf("body missing the disputed line:\n%s", body)
	}
	if !strings.Contains(body, "the check compares the wrong field") {
		t.Errorf("body missing the disputed why text:\n%s", body)
	}
	if !strings.Contains(body, `href="#f1"`) {
		t.Errorf("body missing the disputed line's link to F1:\n%s", body)
	}
}

// TestPages_RunDisputedLineLinksToJudgedCheckRow pins the review finding's
// other branch: with no evaluator finding, the disputed line links to the
// judged check's own row.
func TestPages_RunDisputedLineLinksToJudgedCheckRow(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "dp2", "claude-sonnet-5", []runFixture{
		{runID: "dp2-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "failed", done: true,
			checks: [][5]string{{"c1", "failed", "5", "3", "mcp"}}},
	}, true, map[string]string{"dp2-a": "failed"})
	obs := buildFormat2Observation("dp2-a", nil, []observer.JudgedCheck{
		{ID: "c1", Correct: false, Why: "the check's expected value is stale"},
	}, false)
	seedObservation(t, store, obs)

	body := html.UnescapeString(doGET(t, h, "/r/dp2-a").Body.String())
	if !strings.Contains(body, "the check's expected value is stale") {
		t.Errorf("body missing the disputed why text:\n%s", body)
	}
	if !strings.Contains(body, `href="#check-c1"`) {
		t.Errorf("body missing the disputed line's link to the check row:\n%s", body)
	}
}

// TestPages_RunStoryRendersWithStuckStepLinks pins §8.3 part 3's story
// block: task/expected/did/ending prose plus a stuck range linking to its
// steps.
func TestPages_RunStoryRendersWithStuckStepLinks(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "sy1", "claude-sonnet-5", []runFixture{
		{runID: "sy1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"sy1-a": "passed"})
	obs := buildFormat2Observation("sy1-a", nil, nil, true)
	obs.Story.Stuck = &observer.Stuck{From: 2, To: 3, What: "got confused about the tool result"}
	seedObservation(t, store, obs)

	body := doGET(t, h, "/r/sy1-a").Body.String()
	for _, want := range []string{"do it", "works", "tried", "got confused about the tool result", `href="#s2"`, `href="#s3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing story text/link %q:\n%s", want, body)
		}
	}
}

// TestPages_RunFindingsHaveFBadgeIdsAndCitedByCallout pins the review
// finding: F1…Fn ids, and every step a finding cites renders open with a
// "Cited by F<n>" callout carrying the quote and a backlink.
func TestPages_RunFindingsHaveFBadgeIdsAndCitedByCallout(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "cb1", "claude-sonnet-5", []runFixture{
		{runID: "cb1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"cb1-a": "passed"})
	obs := buildFormat2Observation("cb1-a", []observer.Finding{
		{Severity: "medium", Owner: "zcp-tool", Title: "stale result", What: "the tool result was stale",
			Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}}, LookAt: "tool.go", Fix: "poll first"},
	}, nil, true)
	seedObservation(t, store, obs)

	body := doGET(t, h, "/r/cb1-a").Body.String()
	if !strings.Contains(body, `id="f1"`) {
		t.Errorf("body missing finding anchor id=f1:\n%s", body)
	}
	if !strings.Contains(body, `<details id="s3" class="step step-tool" open>`) {
		t.Errorf("step 3 does not render open even though a finding cites it:\n%s", body)
	}
	if !strings.Contains(body, "Cited by") || !strings.Contains(body, "F1") || !strings.Contains(body, "discovered ok") {
		t.Errorf("body missing the \"Cited by F1\" callout with its quote:\n%s", body)
	}
	if !strings.Contains(body, `↩ F1`) {
		t.Errorf("body missing the callout's backlink to F1:\n%s", body)
	}
}

// TestPages_RunModelPickerLabelsAndPreselection pins the review finding:
// the model picker's labels and its preselection to the current
// observation's model, and the button's "Re-assess with <label>" text.
func TestPages_RunModelPickerLabelsAndPreselection(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "mp1", "claude-opus-5", []runFixture{
		{runID: "mp1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"mp1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "mp1-a", ObsID: "20260911T120000000Z-claude-opus-5",
		Model: "claude-opus-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "OK — fine",
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: "passed", Agree: true}, SelfReview: observer.SelfReview{Accurate: "yes"},
	})

	body := doGET(t, h, "/r/mp1-a").Body.String()
	for _, want := range []string{"Sonnet 5 (default)", "Opus 5 (stronger)", "Fable 5.1 (strongest)"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing model label %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `<option value="claude-opus-5" selected>Opus 5 (stronger)</option>`) {
		t.Errorf("body does not preselect the current observation's model:\n%s", body)
	}
	if !strings.Contains(body, "Re-assess with Opus 5 (stronger)") {
		t.Errorf("body missing the button's \"Re-assess with <label>\" text:\n%s", body)
	}
}

// TestPages_RunLiveStatusWhileAssessing pins §8.5: a queued/running job
// shows "Assessing with <model> — started …" and hides the Assess form.
func TestPages_RunLiveStatusWhileAssessing(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	q := NewQueue(func(ctx context.Context, job Job) error {
		<-release
		return nil
	})
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: q})
	seedBatch(t, store, "ls1", "off", []runFixture{
		{runID: "ls1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"ls1-a": "passed"})

	if err := q.Enqueue(context.Background(), Job{RunID: "ls1-a", Batch: "ls1", Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { return q.State("ls1-a") != "" }) {
		t.Fatal("job never showed as queued/running")
	}

	body := doGET(t, srv.Handler(), "/r/ls1-a").Body.String()
	if !strings.Contains(body, "Assessing with claude-sonnet-5") {
		t.Errorf("body missing the live status text:\n%s", body)
	}
	if strings.Contains(body, `action="/r/ls1-a/observe"`) {
		t.Errorf("body still shows the Assess form while a job is in flight:\n%s", body)
	}
}

// TestPages_RunPreStoreFailureShown pins §8.5: the last pre-store failure
// (a job that errored before anything was stored) shows its own line, and
// the Assess form stays available so the operator can retry.
func TestPages_RunPreStoreFailureShown(t *testing.T) {
	q := NewQueue(func(ctx context.Context, job Job) error {
		return errors.New("boom")
	})
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: q})
	seedBatch(t, store, "pf1", "off", []runFixture{
		{runID: "pf1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"pf1-a": "passed"})

	if err := q.Enqueue(context.Background(), Job{RunID: "pf1-a", Batch: "pf1", Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { _, ok := q.LastFailure("pf1-a"); return ok }) {
		t.Fatal("job never recorded a pre-store failure")
	}

	body := doGET(t, srv.Handler(), "/r/pf1-a").Body.String()
	if !strings.Contains(body, "failed before anything was stored: boom") {
		t.Errorf("body missing the pre-store failure line:\n%s", body)
	}
	if !strings.Contains(body, "Re-assess to retry") {
		t.Errorf("body missing the retry instruction:\n%s", body)
	}
	if !strings.Contains(body, `action="/r/pf1-a/observe"`) {
		t.Errorf("body hides the Assess form after a pre-store failure:\n%s", body)
	}
}

// TestPages_RunObsParamRendersOlderVersion pins §8.3/§8.7: ?obs=<obsId>
// renders that stored version in the card, with a note and a way back to
// the current one.
func TestPages_RunObsParamRendersOlderVersion(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ob1", "claude-sonnet-5", []runFixture{
		{runID: "ob1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"ob1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ob1-a", ObsID: "20260910T090000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), Status: "ok", Headline: "older headline",
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: "passed", Agree: true}, SelfReview: observer.SelfReview{Accurate: "yes"},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ob1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "current headline",
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: "passed", Agree: true}, SelfReview: observer.SelfReview{Accurate: "yes"},
	})

	body := doGET(t, h, "/r/ob1-a?obs=20260910T090000000Z-claude-sonnet-5").Body.String()
	if !strings.Contains(body, "older headline") {
		t.Errorf("body does not render the requested older observation:\n%s", body)
	}
	if strings.Contains(body, "current headline") {
		t.Errorf("body still shows the current headline while viewing an older obs:\n%s", body)
	}
	if !strings.Contains(body, "Showing an earlier assessment") {
		t.Errorf("body missing the \"showing an earlier assessment\" note:\n%s", body)
	}
	if !strings.Contains(body, `href="/r/ob1-a"`) {
		t.Errorf("body missing a way back to the current observation:\n%s", body)
	}
}

// TestPages_RunObsParamUnknownIs404 pins §8.3: a missing or foreign obsId
// answers 404, never a rendered (wrong) card.
func TestPages_RunObsParamUnknownIs404(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ob2", "off", []runFixture{
		{runID: "ob2-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"ob2-a": "passed"})

	rr := doGET(t, h, "/r/ob2-a?obs=20000101T000000000Z-nobody")
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET with an unknown obs id: got %d, want 404", rr.Code)
	}
}

// TestPages_RunStepsFilterNarrowsRowsAndShowsCounts pins §8.7: steps=cited
// keeps only the steps a finding's evidence cites, and the filter bar
// shows every option's count.
func TestPages_RunStepsFilterNarrowsRowsAndShowsCounts(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "sf1", "claude-sonnet-5", []runFixture{
		{runID: "sf1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"sf1-a": "passed"})
	obs := buildFormat2Observation("sf1-a", []observer.Finding{
		{Severity: "low", Owner: "zcp-tool", Title: "t", What: "w",
			Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}}, LookAt: "x", Fix: "y"},
	}, nil, true)
	seedObservation(t, store, obs)

	all := doGET(t, h, "/r/sf1-a").Body.String()
	if !strings.Contains(all, `id="s1"`) || !strings.Contains(all, `id="s3"`) {
		t.Fatalf("default steps=all does not render every step:\n%s", all)
	}
	if !strings.Contains(all, `Cited <span class="filter-count">1</span>`) {
		t.Errorf("filter bar missing the cited option's count:\n%s", all)
	}

	cited := doGET(t, h, "/r/sf1-a?steps=cited").Body.String()
	if strings.Contains(cited, `id="s1"`) {
		t.Errorf("steps=cited still renders step 1 (not cited):\n%s", cited)
	}
	if !strings.Contains(cited, `id="s3"`) {
		t.Errorf("steps=cited drops step 3 (cited by F1):\n%s", cited)
	}
}

// TestPages_RunUnknownQueryParamIs400 pins §8.7: a parameter this page does
// not take is refused with the 400 page naming it, and a bad `steps=`
// value is refused the same way.
func TestPages_RunUnknownQueryParamIs400(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "bq1", "off", []runFixture{
		{runID: "bq1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"bq1-a": "passed"})

	rr := doGET(t, h, "/r/bq1-a?bogus=1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET with an unknown parameter: got %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "bogus") {
		t.Errorf("400 page does not name the refused parameter:\n%s", body)
	}

	rr2 := doGET(t, h, "/r/bq1-a?steps=nonsense")
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("GET with a bad steps= value: got %d, want 400, body=%s", rr2.Code, rr2.Body.String())
	}
	if body := rr2.Body.String(); !strings.Contains(body, "steps") {
		t.Errorf("400 page does not name the steps parameter:\n%s", body)
	}
}

// TestPages_RunDegradesWithoutTaskPromptNever502 pins the review finding
// (live bug: /r/gate5-resume-after-compaction): a done run whose bundle is
// missing task-prompt.txt renders 200 with the header and a reason, never
// a 502.
func TestPages_RunDegradesWithoutTaskPromptNever502(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "gt5", "off", []runFixture{
		{runID: "gt5-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"gt5-a": "passed"})

	resultsDir := "runs/gt5-a/results/" + testResultsTS + "/a"
	store.mu.Lock()
	delete(store.objects, resultsDir+"/task-prompt.txt")
	store.mu.Unlock()

	rr := doGET(t, h, "/r/gt5-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("run with no task prompt: got %d, want 200 (never 502), body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<h1>a</h1>") {
		t.Errorf("body missing the header for a degraded run:\n%s", body)
	}
	if !strings.Contains(body, "steps unavailable") {
		t.Errorf("body missing the steps-unavailable reason:\n%s", body)
	}
	if !strings.Contains(body, "record unavailable") {
		t.Errorf("body missing the record-unavailable reason:\n%s", body)
	}
}

// TestPages_RunAssessFormHiddenWhenObserverUnavailable pins §8.3: when the
// observer is unavailable (credential missing), every Assess form is
// hidden — even for a finished run.
func TestPages_RunAssessFormHiddenWhenObserverUnavailable(t *testing.T) {
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), ObserverCredentialMissing: true})
	seedBatch(t, store, "un1", "off", []runFixture{
		{runID: "un1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true},
	}, true, map[string]string{"un1-a": "passed"})

	body := doGET(t, srv.Handler(), "/r/un1-a").Body.String()
	if strings.Contains(body, `action="/r/un1-a/observe"`) {
		t.Errorf("Assess form rendered while the observer is unavailable:\n%s", body)
	}
}
