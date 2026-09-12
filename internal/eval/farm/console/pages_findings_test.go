// Package console: RED/GREEN tests for GET /findings (docs/spec-eval-
// farm.md §8.3 FM-51's findings page, over findings_model.go's FindingRow/
// findingEngine — already implemented). These tests pin the page's own
// wiring (route, query parsing via the §8.7 engine, rendering), not
// BuildFindingRows itself, which findings_model_test.go already covers.
//
// This replaces TestPages_FindingsGroupedAndLinked (formerly in
// console_test.go/pages_test.go): the page no longer groups by raw owner
// via an "owner=" parameter (that predates the §8.7 list engine) — it
// filters/sorts through findingListSpec's `cause` (class), `severity`,
// `surface`, `scenario`, `batch`, `build` and `since`, default sort
// `severity`.
package console

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// TestPages_FindingsPageListsFindingsWithCauseAndSeverity pins §8.3's
// /findings page: every finding of every run in the window, one
// collapsible line each — severity tag, cause label, title, run link —
// opening to what/evidence (with its step link)/where-to-look/fix.
// Independent oracle: fixtureObservation's own two findings (api_test.go)
// — owner zcp-tool (high, step 3) and owner agent (medium, step 99).
func TestPages_FindingsPageListsFindingsWithCauseAndSeverity(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fd1", "claude-sonnet-5", []runFixture{
		{runID: "fd1-scn", scenario: "scn", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fd1-scn": "passed"})
	seedObservation(t, store, fixtureObservation("fd1-scn"))

	rr := doGET(t, h, "/findings?since=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /findings: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	for _, want := range []string{
		"Tool returned stale data", "Agent skipped a sanity check",
		"ZCP tool", "Agent mistake", // cause labels, not raw owner strings
		`href="/r/fd1-scn#f1"`,
		`href="/r/fd1-scn#s3"`,  // the zcp-tool finding's verified evidence
		"poll before returning", // fix
		"<details",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}

	// Default sort severity desc: high (zcp-tool) before medium (agent).
	if i, j := strings.Index(body, "Tool returned stale data"), strings.Index(body, "Agent skipped a sanity check"); i < 0 || j < 0 || i > j {
		t.Errorf("findings not sorted severity-desc (high before medium): high@%d, medium@%d\n%s", i, j, body)
	}
}

// TestPages_FindingsFilterByCauseNarrowsRows pins §8.7: the `cause` filter
// (class-level, superseding the old owner param) narrows the rendered
// findings.
func TestPages_FindingsFilterByCauseNarrowsRows(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fd2", "claude-sonnet-5", []runFixture{
		{runID: "fd2-scn", scenario: "scn", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fd2-scn": "passed"})
	seedObservation(t, store, fixtureObservation("fd2-scn"))

	rr := doGET(t, h, "/findings?since=24h&cause=agent")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /findings?cause=agent: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Agent skipped a sanity check") {
		t.Errorf("cause=agent body missing the agent finding:\n%s", body)
	}
	if strings.Contains(body, "Tool returned stale data") {
		t.Errorf("cause=agent body still shows the zcp finding:\n%s", body)
	}
}

// TestPages_FindingsUnknownParameterRendersBadQueryPage pins §8.7: a
// parameter the list does not take (including the retired `owner=`) is
// refused with the 400 page.
func TestPages_FindingsUnknownParameterRendersBadQueryPage(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := doGET(t, h, "/findings?owner=agent")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET /findings?owner=agent: got %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "owner") {
		t.Errorf("400 body does not name the refused parameter:\n%s", body)
	}
}

// TestPages_FindingsSummaryLine pins the page's one-line summary: total
// finding count and the window in words.
func TestPages_FindingsFilteredCount_MatchesCards(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fd3", "claude-sonnet-5", []runFixture{
		{runID: "fd3-scn", scenario: "scn", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fd3-scn": "passed"})
	seedObservation(t, store, fixtureObservation("fd3-scn"))

	filtered := doGET(t, h, "/findings?since=7d&severity=high").Body.String()
	if !strings.Contains(filtered, "1 matching finding in 7 days") || strings.Count(filtered, `<details class="finding `) != 1 {
		t.Errorf("filtered summary and rendered-card count diverge:\n%s", filtered)
	}
	summaryStart := strings.Index(filtered, `<summary class="finding-head">`)
	summaryEnd := strings.Index(filtered[summaryStart:], `</summary>`)
	if summaryStart < 0 || summaryEnd < 0 || strings.Contains(filtered[summaryStart:summaryStart+summaryEnd], "<a ") {
		t.Errorf("finding disclosure summary contains a nested interactive link:\n%s", filtered)
	}

	noMatch := doGET(t, h, "/findings?since=7d&cause=platform").Body.String()
	if !strings.Contains(noMatch, "0 matching findings in 7 days") ||
		!strings.Contains(noMatch, "No findings match these filters") ||
		!strings.Contains(noMatch, `href="/findings"`) || strings.Contains(noMatch, "No findings in this window") {
		t.Errorf("filter no-match state is not distinct and resettable:\n%s", noMatch)
	}

	emptySrv, _, _ := testServer(t)
	noData := doGET(t, emptySrv.Handler(), "/findings?since=7d").Body.String()
	if !strings.Contains(noData, "0 matching findings in 7 days") ||
		!strings.Contains(noData, "No source runs in this window") || strings.Contains(noData, "No findings match these filters") {
		t.Errorf("source-empty state is not distinct from a filter miss:\n%s", noData)
	}
}

// TestPages_FindingsEmpty_NoSourceExplainsProvenance pins the first empty
// state: an empty store means there are no source runs in the selected
// window. It must not imply that assessments ran cleanly.
func TestPages_FindingsEmpty_NoSourceExplainsProvenance(t *testing.T) {
	srv, _, _ := testServer(t)
	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()

	for _, want := range []string{
		"0 source runs",
		"0 successfully assessed",
		"0 findings",
		"0 runs with unavailable evidence",
		"No source runs in this window",
		"No run records are available in this time window.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "reported no findings") {
		t.Errorf("source-empty page implies that assessments were clean:\n%s", body)
	}
}

// TestPages_FindingsEmpty_NoSuccessfulAssessmentExplainsProvenance pins a
// run-backed empty state where source evidence exists but no assessment
// completed successfully.
func TestPages_FindingsEmpty_NoSuccessfulAssessmentExplainsProvenance(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "empty-unassessed", "claude-sonnet-5", []runFixture{{
		runID: "empty-unassessed-a", scenario: "a", startedAt: now.Add(-time.Hour),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, true, map[string]string{"empty-unassessed-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "empty-unassessed-a",
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: observationStatusError, Error: "model call failed",
	})

	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()
	for _, want := range []string{
		"1 source run", "0 successfully assessed", "0 findings", "0 runs with unavailable evidence",
		"No successfully assessed runs", "The source run has no successful assessment to report findings from.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "reported no findings") {
		t.Errorf("unassessed page implies that an assessment was clean:\n%s", body)
	}
}

// TestPages_FindingsEmpty_AssessedCleanExplainsProvenance pins the only
// empty state that can make a positive claim: available, successful
// assessment evidence exists and it reported no findings.
func TestPages_FindingsEmpty_AssessedCleanExplainsProvenance(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "empty-clean", "claude-sonnet-5", []runFixture{{
		runID: "empty-clean-a", scenario: "a", startedAt: now.Add(-time.Hour),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, true, map[string]string{"empty-clean-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "empty-clean-a",
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: observationStatusOK, Outcome: observer.OutcomeOK,
	})

	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()
	for _, want := range []string{
		"1 source run", "1 successfully assessed", "0 findings", "0 runs with unavailable evidence",
		"No findings reported", "The successfully assessed run reported no findings.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// TestPages_FindingsEmpty_UnavailableEvidenceDoesNotClaimClean pins the
// conservative mixed state: a clean assessment beside an unreadable run
// is partial evidence, not an all-clear.
func TestPages_FindingsEmpty_UnavailableEvidenceDoesNotClaimClean(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "empty-partial", "claude-sonnet-5", []runFixture{
		{runID: "empty-partial-clean", scenario: "clean", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "empty-partial-broken", scenario: "broken", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"empty-partial-clean": "passed", "empty-partial-broken": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "empty-partial-clean",
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: observationStatusOK, Outcome: observer.OutcomeOK,
	})
	store.putText(t, "runs/empty-partial-broken/results/"+testResultsTS+"/broken/meta.json", "{malformed")

	body := doGET(t, srv.Handler(), "/findings?since=30d").Body.String()
	for _, want := range []string{
		"2 source runs", "1 successfully assessed", "0 findings", "1 run with unavailable evidence",
		"Evidence is incomplete", "One source run has unavailable evidence, so the findings view may be incomplete.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "The successfully assessed run reported no findings.") {
		t.Errorf("partial-evidence page claims the assessed-clean state:\n%s", body)
	}
}

// TestPages_FindingsEvidenceUsesCanonicalQuoteVocabulary keeps the
// Findings page aligned with the glossary and the run page.
func TestPages_FindingsEvidenceUsesCanonicalQuoteVocabulary(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "quote-vocab", "claude-sonnet-5", []runFixture{{
		runID: "quote-vocab-a", scenario: "a", startedAt: now.Add(-time.Hour),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, true, map[string]string{"quote-vocab-a": "passed"})
	seedObservation(t, store, fixtureObservation("quote-vocab-a"))

	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()
	for _, want := range []string{"quote found", "quote not found"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing canonical wording %q:\n%s", want, body)
		}
	}
	for _, stale := range []string{">verified<", ">unverified<"} {
		if strings.Contains(body, stale) {
			t.Errorf("body contains stale evidence wording %q:\n%s", stale, body)
		}
	}
}

// TestPages_FindingsRunLinksTargetTheirFinding pins the return path from
// the aggregate list to the exact finding on the run page.
func TestPages_FindingsRunLinksTargetTheirFinding(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "finding-target", "claude-sonnet-5", []runFixture{{
		runID: "finding-target-a", scenario: "a", startedAt: now.Add(-time.Hour),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, true, map[string]string{"finding-target-a": "passed"})
	seedObservation(t, store, fixtureObservation("finding-target-a"))

	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()
	for _, want := range []string{
		`class="run-link" href="/r/finding-target-a#f1"`,
		`class="run-link" href="/r/finding-target-a#f2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing exact finding target %q:\n%s", want, body)
		}
	}
}

// TestPages_FindingsStepLinksRequireExactVisibleTarget pins conservative
// navigation. A positive in-range number is still unavailable when that
// recorded step is intentionally not rendered; only an exact visible step
// gets a link.
func TestPages_FindingsStepLinksRequireExactVisibleTarget(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "step-target", "claude-sonnet-5", []runFixture{{
		runID: "step-target-a", scenario: "a", startedAt: now.Add(-time.Hour),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, true, map[string]string{"step-target-a": "passed"})
	store.putText(t, "runs/step-target-a/results/"+testResultsTS+"/a/transcript.jsonl", strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":""},{"type":"tool_use","id":"tu1","name":"zerops_discover","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"discovered ok"}]}]}}`,
	}, "\n")+"\n")
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "step-target-a",
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: observationStatusOK, Outcome: observer.OutcomeProblem,
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Title: "step targets", Evidence: []observer.Evidence{
			{Step: 2, Quote: "blank thinking", Verified: false},
			{Step: 3, Quote: "discovered ok", Verified: true},
			{Step: 99, Quote: "outside the record", Verified: false},
		}}},
	})

	body := doGET(t, srv.Handler(), "/findings?since=7d").Body.String()
	if !strings.Contains(body, `href="/r/step-target-a#s3">step 3</a>`) {
		t.Errorf("body missing exact valid step link:\n%s", body)
	}
	for _, unavailable := range []string{"step 2 unavailable", "step 99 unavailable"} {
		if !strings.Contains(body, unavailable) {
			t.Errorf("body missing %q:\n%s", unavailable, body)
		}
	}
	for _, invalidLink := range []string{`href="/r/step-target-a#s2"`, `href="/r/step-target-a#s99"`} {
		if strings.Contains(body, invalidLink) {
			t.Errorf("body links an unavailable target %q:\n%s", invalidLink, body)
		}
	}
}

// TestPages_FindingsRowLinksNarrowList pins the filter-tester's finding: a
// finding card's scenario/batch/build/surface values are links (each via
// listURL, keeping the other parameters) that narrow /findings.
func TestPages_FindingsRowLinksNarrowList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fl1", "claude-sonnet-5", []runFixture{
		{runID: "fl1-scen-a", scenario: "scen-a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fl1-scen-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "fl1-scen-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "matching finding"}},
	})

	seedBatch(t, store, "fl2", "claude-sonnet-5", []runFixture{
		{runID: "fl2-scen-x", scenario: "scen-x", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fl2-scen-x": "passed"})
	// A distinct build (seedBatch always writes "cand-sha") so the build
	// filter has something real to narrow out.
	store.putJSON(t, "batches/fl2/manifest.json", farm.BatchManifest{
		Batch: "fl2", CreatedAt: "2026-09-01T00:00:00Z", StartedAt: "2026-09-01T00:00:00Z",
		Set: "gate", CandidateSha256: "other-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", Runs: []farm.ManifestRun{{RunID: "fl2-scen-x", Scenario: "scen-x", ProjectName: "zcp-farm-fl2-scen-x"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "fl2-scen-x", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "y",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "unrelated finding"}},
	})

	body := doGET(t, h, "/findings?since=24h").Body.String()

	cases := map[string]*regexp.Regexp{
		"scenario": regexp.MustCompile(`href="(/findings\?[^"]*scenario=scen-a[^"]*)"`),
		"batch":    regexp.MustCompile(`href="(/findings\?[^"]*batch=fl1[^"]*)"`),
		"build":    regexp.MustCompile(`href="(/findings\?[^"]*build=cand-sha[^"]*)"`),
		"surface":  regexp.MustCompile(`href="(/findings\?[^"]*surface=tool%3Azerops_deploy%2Fdeploy[^"]*)"`),
	}
	for name, re := range cases {
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("body missing a %s filter link:\n%s", name, body)
		}
		narrowed := doGET(t, h, html.UnescapeString(m[1])).Body.String()
		if !strings.Contains(narrowed, "matching finding") {
			t.Errorf("%s link did not keep the matching finding:\n%s", name, narrowed)
		}
		if strings.Contains(narrowed, "unrelated finding") {
			t.Errorf("%s link did not narrow out the unrelated finding:\n%s", name, narrowed)
		}
	}
}
