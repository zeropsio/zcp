// Package console: RED/GREEN tests for GET /problems (docs/spec-eval-
// farm.md §8.3 FM-51's problems page, over §8.6's problem clustering and
// §8.7's list engine — both already implemented in problems.go). These
// tests pin the page's own wiring (route, query parsing, rendering), not
// BuildProblems' clustering rules, which problems_test.go already covers
// exhaustively.
package console

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// seedFormat2Finding stores a format-2 observation for runID with one
// finding built from the given fields — the shape §8.6 clustering needs
// (Surface/Anchor), which fixtureObservation (api_test.go, format 1) never
// carries.
func seedFormat2Finding(t *testing.T, store *fakeStore, runID string, now time.Time, severity, surface, anchor, title, fix string, step int, quote string) {
	t.Helper()
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: runID,
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: title,
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: "passed", Agree: true},
		Findings: []observer.Finding{{
			Severity: severity, Owner: "zcp-tool", Surface: surface, Anchor: anchor,
			Title: title, What: "it went wrong", Fix: fix, LookAt: "internal/x.go",
			Evidence: []observer.Evidence{{Step: step, Quote: quote, Verified: true}},
		}},
		SelfReview: observer.SelfReview{Accurate: "yes"},
	})
}

// TestPages_ProblemsPageRendersClusteredRow pins §8.3's /problems page: a
// format-2 finding renders as one problem row (severity, cause, surface
// prefix, title, anchor, status) that expands via <details> to its member
// (run link, batch, build, severity, cause, title, first found quote with
// its step link).
func TestPages_ProblemsPageRendersClusteredRow(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "pr1", "claude-sonnet-5", []runFixture{
		{runID: "pr1-scn", scenario: "scn", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pr1-scn": "passed"})
	seedFormat2Finding(t, store, "pr1-scn", now, observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "ZCP_LAUNCH_TOKEN", "Deploy leaks the launch token", "redact it",
		3, "token leaked here")

	rr := doGET(t, h, "/problems")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /problems: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	for _, want := range []string{
		"Deploy leaks the launch token", // title
		"redact it",                     // fix
		"tool:zerops_deploy",            // surface prefix
		"ZCP_LAUNCH_TOKEN",              // anchor
		"Members (1)",
		`href="/r/pr1-scn"`,
		`href="/r/pr1-scn#s3"`,
		"token leaked here",
		"<details",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// seedTwoDistinctProblems seeds one batch with two runs, each producing its
// own single-member problem (distinct anchors so they cluster separately):
// "High problem" (high, started 2h before now) and "Medium problem"
// (medium, started 1h before now) — the shared fixture for the filter/sort/
// summary tests below.
func seedTwoDistinctProblems(t *testing.T, store *fakeStore, now time.Time) {
	t.Helper()
	seedBatch(t, store, "pr2", "claude-sonnet-5", []runFixture{
		{runID: "pr2-a", scenario: "a", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "pr2-b", scenario: "b", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pr2-a": "passed", "pr2-b": "passed"})
	seedFormat2Finding(t, store, "pr2-a", now.Add(-2*time.Hour), observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "ANCHOR_A", "High problem", "fix a", 1, "quote a")
	seedFormat2Finding(t, store, "pr2-b", now.Add(-1*time.Hour), observer.SeverityMedium,
		"tool:zerops_import/import", "ANCHOR_B", "Medium problem", "fix b", 1, "quote b")
}

// TestPages_ProblemsFilterNarrowsRowsAndShowsCounts pins §8.7: a filter
// link narrows the rendered rows, and each filter option shows its count.
func TestPages_ProblemsFilterNarrowsRowsAndShowsCounts(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	all := doGET(t, h, "/problems")
	if all.Code != http.StatusOK {
		t.Fatalf("GET /problems: got %d, want 200, body=%s", all.Code, all.Body.String())
	}
	if body := all.Body.String(); !strings.Contains(body, "High problem") || !strings.Contains(body, "Medium problem") {
		t.Fatalf("unfiltered body missing one of the two problems:\n%s", body)
	}
	// The Cause filter's "ZCP" option (both problems are ZCP-caused) shows
	// its count under the OTHER active filters (§8.7): 2 unfiltered.
	if !strings.Contains(all.Body.String(), `>ZCP <span class="filter-count">2</span>`) {
		t.Errorf("body missing the unfiltered Cause=ZCP count of 2:\n%s", all.Body.String())
	}

	high := doGET(t, h, "/problems?severity=high")
	if high.Code != http.StatusOK {
		t.Fatalf("GET /problems?severity=high: got %d, want 200", high.Code)
	}
	hb := high.Body.String()
	if !strings.Contains(hb, "High problem") {
		t.Errorf("severity=high body missing the high problem:\n%s", hb)
	}
	if strings.Contains(hb, "Medium problem") {
		t.Errorf("severity=high body still shows the medium problem:\n%s", hb)
	}
	// The Cause filter's own count narrows to 1 under severity=high, the
	// other active filter (§8.7: "each filter option shows its count under
	// the other active filters").
	if !strings.Contains(hb, `>ZCP <span class="filter-count">1</span>`) {
		t.Errorf("severity=high body's Cause=ZCP count did not narrow to 1:\n%s", hb)
	}
}

// TestPages_ProblemsSortReordersRows pins §8.7: a sort link reorders the
// rows, and every link on the page keeps the other parameters (severity
// stays set while sort/dir change).
func TestPages_ProblemsSortReordersRows(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	asc := doGET(t, h, "/problems?sort=first&dir=asc")
	if asc.Code != http.StatusOK {
		t.Fatalf("GET ?sort=first&dir=asc: got %d, want 200", asc.Code)
	}
	ab := asc.Body.String()
	iHighAsc, iMedAsc := strings.Index(ab, "High problem"), strings.Index(ab, "Medium problem")
	if iHighAsc < 0 || iMedAsc < 0 || iHighAsc > iMedAsc {
		t.Errorf("sort=first&dir=asc: want High (older, first-seen earlier) before Medium; High@%d Medium@%d\n%s", iHighAsc, iMedAsc, ab)
	}

	desc := doGET(t, h, "/problems?sort=first&dir=desc")
	db := desc.Body.String()
	iHighDesc, iMedDesc := strings.Index(db, "High problem"), strings.Index(db, "Medium problem")
	if iHighDesc < 0 || iMedDesc < 0 || iMedDesc > iHighDesc {
		t.Errorf("sort=first&dir=desc: want Medium before High; High@%d Medium@%d\n%s", iHighDesc, iMedDesc, db)
	}

	// Every link on the page keeps the other parameters: with severity=high
	// active, the sort header's own link must still carry severity=high.
	withFilter := doGET(t, h, "/problems?severity=high&sort=first&dir=asc").Body.String()
	if !strings.Contains(withFilter, "severity=high") {
		t.Errorf("a link on the filtered page dropped severity=high:\n%s", withFilter)
	}
}

// TestPages_ProblemsUnknownParameterRendersBadQueryPage pins §8.7: a
// parameter the list does not take is refused with a 400 page naming it
// and a link that drops it.
func TestPages_ProblemsUnknownParameterRendersBadQueryPage(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := doGET(t, h, "/problems?bogus=1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET /problems?bogus=1: got %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "bogus") {
		t.Errorf("400 body does not name the refused parameter:\n%s", body)
	}
	if strings.Contains(body, "bogus=1") {
		t.Errorf("400 body's drop-link still carries the refused parameter:\n%s", body)
	}
}

// TestPages_ProblemsSummaryLineCountsLiveAndHigh pins the page's one-line
// summary: live problem count and high-severity count among them.
func TestPages_ProblemsSummaryLineCountsLiveAndHigh(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, "2 live problems") {
		t.Errorf("summary missing \"2 live problems\":\n%s", body)
	}
	if !strings.Contains(body, "1 high") {
		t.Errorf("summary missing \"1 high\":\n%s", body)
	}
}
