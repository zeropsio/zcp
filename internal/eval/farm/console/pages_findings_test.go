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
	"net/http"
	"strings"
	"testing"
	"time"
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
		`href="/r/fd1-scn"`,
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
func TestPages_FindingsSummaryLine(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fd3", "claude-sonnet-5", []runFixture{
		{runID: "fd3-scn", scenario: "scn", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fd3-scn": "passed"})
	seedObservation(t, store, fixtureObservation("fd3-scn"))

	body := doGET(t, h, "/findings?since=7d").Body.String()
	if !strings.Contains(body, "2 findings in 7 days") {
		t.Errorf("summary missing \"2 findings in 7 days\":\n%s", body)
	}
}
