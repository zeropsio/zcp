// Package console: RED/GREEN tests for GET /problems (docs/spec-eval-
// farm.md §8.3 FM-51's problems page, over §8.6's problem clustering and
// §8.7's list engine — both already implemented in problems.go). These
// tests pin the page's own wiring (route, query parsing, rendering), not
// BuildProblems' clustering rules, which problems_test.go already covers
// exhaustively.
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

// TestPages_ProblemsPageStatusUsesFullHistoryNotSinceWindow pins item 1
// (FIX3): /problems computes status over the FULL farm history, not just
// its own since window (default 30d) — a problem hit on a build far older
// than 30 days must still read status=recurring, never first-seen.
func TestPages_ProblemsPageStatusUsesFullHistoryNotSinceWindow(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedFullHistoryProblemFixture(t, store, "fh3", now)

	body := doGET(t, h, "/problems").Body.String()
	// Inspect the row badge, not the filter menu which lists every status.
	if !strings.Contains(body, `class="state-badge state-recurring"`) {
		t.Errorf("row's own status chip must read recurring:\n%s", body)
	}
	if strings.Contains(body, `class="state-badge state-first-seen"`) {
		t.Errorf("status computed over the since window only, not the full farm history:\n%s", body)
	}
}

// TestPages_ProblemsPageStillEmittedWhenAnchorStillInNewestBuildSteps pins
// item 2 (FIX3): before settling on "gone"/"unconfirmed", a problem's own
// anchor is searched in the newest build's own candidate runs' raw step
// text (StepTextFinder, wired end to end here through the real run cache) —
// found there flips the status to "still-emitted" even though no CURRENT
// observation happens to flag it on that build.
func TestPages_ProblemsPageStillEmittedWhenAnchorStillInNewestBuildSteps(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	// se-old: an older build whose run hits the anchor as a real finding.
	seedBatch(t, store, "se-old", "claude-sonnet-5", []runFixture{
		{runID: "se-old-x", scenario: "se-scn", startedAt: now.Add(-100 * 24 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"se-old-x": "passed"})
	store.putJSON(t, "batches/se-old/manifest.json", farm.BatchManifest{
		Batch: "se-old", CreatedAt: "2026-06-01T00:00:00Z", StartedAt: "2026-06-01T00:00:00Z",
		Set: "gate", CandidateSha256: "se-old-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", Runs: []farm.ManifestRun{{RunID: "se-old-x", Scenario: "se-scn", ProjectName: "zcp-farm-se-old-x"}},
	})
	seedFormat2Finding(t, store, "se-old-x", now, observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "STILL_HERE_TOKEN", "Deploy still prints a stale token", "rotate it", 3, "quote")

	// se-new: the newest build. Its own run is assessed clean (no current
	// finding for this anchor) — but its transcript still emits the exact
	// text, unlike a genuinely fixed bug.
	seedBatch(t, store, "se-new", "claude-sonnet-5", []runFixture{
		{runID: "se-new-x", scenario: "se-scn", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"se-new-x": "passed"})
	store.putJSON(t, "batches/se-new/manifest.json", farm.BatchManifest{
		Batch: "se-new", CreatedAt: "2026-09-11T12:00:00Z", StartedAt: "2026-09-11T12:00:00Z",
		Set: "gate", CandidateSha256: "se-new-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", Runs: []farm.ManifestRun{{RunID: "se-new-x", Scenario: "se-scn", ProjectName: "zcp-farm-se-new-x"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "se-new-x", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeOK, Headline: "clean",
	})
	// Overwrite se-new-x's transcript (seedRun's own fixtureTranscript
	// carries no anchor text) with one whose tool result still emits the
	// exact anchor text a real ZCP run would print.
	resultsDir := "runs/se-new-x/results/" + testResultsTS + "/se-scn"
	customTranscript := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"deploying"},{"type":"tool_use","id":"tu9","name":"zerops_deploy","input":{"project":"p1"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu9","content":[{"type":"text","text":"error: STILL_HERE_TOKEN appears in the output"}]}]}}`,
	}, "\n") + "\n"
	store.putText(t, resultsDir+"/transcript.jsonl", customTranscript)

	// since must be wide enough to keep se-old-x's own finding in scope
	// (BuildProblemsScoped only lists a problem with >=1 in-scope member;
	// se-new-x itself is never a member, since it hits no current finding).
	body := doGET(t, h, "/problems?status=all&since=200d").Body.String()
	if !strings.Contains(body, "Deploy still prints a stale token") {
		t.Fatalf("body missing the problem's title:\n%s", body)
	}
	if !strings.Contains(body, "still emitted (no longer reported)") {
		t.Errorf("status must be still-emitted, not gone/unconfirmed, since the anchor is still in the newest build's own steps:\n%s", body)
	}
}

// TestPages_ProblemsRunsHitCellShowsInScopeBesideNewestBuildHit pins item 3
// (FIX3): the Runs-hit cell keeps §8.6's own "hit a/b runs on <newest
// build>" figure (spec-mandated) and, beside it, the caller-scope figure
// (InScopeHit/InScopeAssessed) — the two differ whenever the newest
// build's own runs aren't exactly the caller's scope, as in
// seedInScopeVsNewestBuildFixture's own 0/1 vs 1/2 split.
func TestPages_ProblemsRunsHitCellShowsInScopeBesideNewestBuildHit(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedInScopeVsNewestBuildFixture(t, store, now)

	body := doGET(t, h, "/problems?status=all").Body.String()
	// The exact two incidence measures remain distinct in expanded evidence.
	for _, want := range []string{"hit 0/1 runs", "1/2 in scope", "Newest build coverage", "Current time window"} {
		if !strings.Contains(body, want) {
			t.Errorf("problem evidence missing %q", want)
		}
	}
}

// TestPages_ProblemsRowIsCompact verifies the title-first summary and sibling
// evidence structure; actual row density is verified in the browser.
func TestPages_ProblemsRowIsCompact(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "cpt1", "claude-sonnet-5", []runFixture{
		{runID: "cpt1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cpt1-a": "passed"})
	seedFormat2Finding(t, store, "cpt1-a", now, observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "COMPACT_ANCHOR", "Compact row problem", "fix it", 1, "quote here")
	body := doGET(t, srv.Handler(), "/problems").Body.String()
	if !strings.Contains(body, fmtTimeShort(now)) {
		t.Errorf("missing short-form date %q", fmtTimeShort(now))
	}
	if !regexp.MustCompile(`(?s)<details class="problem-row"[^>]*>\s*<summary[^>]*>.*?Compact row problem.*?</summary>\s*<section class="member-content"`).MatchString(body) {
		t.Error("problem evidence is not a full-width sibling of its title-first summary")
	}
	for _, want := range []string{"Members (1)", `href="/r/cpt1-a"`} {
		if !strings.Contains(body, want) {
			t.Errorf("problem dropped %q", want)
		}
	}
}

// TestPages_ProblemsBannersFailedAssessmentRuns pins item 9 (FIX3): a run
// whose current observation failed to parse/errored is silently absent
// from every /problems aggregate (BuildProblems only sees status "ok"
// observations) — a banner names it, so a reader knows the list is
// incomplete rather than trusting an empty or short one.
func TestPages_ProblemsBannersFailedAssessmentRuns(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fa1", "claude-sonnet-5", []runFixture{
		{runID: "fa1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fa1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "fa1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "unparsed", Raw: "not json",
	})

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, "1 run") || !strings.Contains(body, "could not be parsed") {
		t.Errorf("body missing the failed-assessment banner:\n%s", body)
	}
	if !strings.Contains(body, "missing from this list") {
		t.Errorf("body missing the \"missing from this list\" warning:\n%s", body)
	}
	if !strings.Contains(body, `href="/r/fa1-a"`) {
		t.Errorf("body missing the link to the affected run:\n%s", body)
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

// TestPages_ProblemsSeverityFilterTooltipNamesThreshold pins item 11
// (FIX2): the severity filter's own options carry a tooltip that says what
// selecting them narrows the list to ("medium or higher", "any severity")
// rather than the plain severity-level definition, which describes the
// severity itself, not the minimum-filter's effect.
func TestPages_ProblemsSeverityFilterTooltipNamesThreshold(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedTwoDistinctProblems(t, store, fixedNow(t)())

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, `title="medium or higher"`) {
		t.Errorf("body missing the Medium option's \"medium or higher\" tooltip:\n%s", body)
	}
	if !strings.Contains(body, `title="any severity"`) {
		t.Errorf("body missing the Low option's \"any severity\" tooltip:\n%s", body)
	}
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
	if !strings.Contains(body, "2 matching problems · 2 live") {
		t.Errorf("summary missing matching/live counts:\n%s", body)
	}
	if !strings.Contains(body, "1 high") {
		t.Errorf("summary missing \"1 high\":\n%s", body)
	}
}

// TestPages_ProblemsOmitsSurfaceChipWhenEmpty pins item 11 (round-1
// follow-up): a problem with no surface (a format-2 finding whose surface
// is "agent"/"platform"/empty, or a format-1 finding) renders no empty
// surface chip on /problems.
func TestPages_ProblemsOmitsSurfaceChipWhenEmpty(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ns3", "claude-sonnet-5", []runFixture{
		{runID: "ns3-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ns3-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ns3-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "format-1 finding, no surface"}},
	})

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, "format-1 finding, no surface") {
		t.Fatalf("body missing the problem's title:\n%s", body)
	}
	if strings.Contains(body, `<code class="chip"></code>`) {
		t.Errorf("body renders an empty surface chip:\n%s", body)
	}
}

// TestPages_ProblemsSortDisclosureKeepsEveryKey verifies the native sorting
// control remains available at every width after replacing the table header.
func TestPages_ProblemsSortDisclosureKeepsEveryKey(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedTwoDistinctProblems(t, store, fixedNow(t)())

	body := doGET(t, h, "/problems").Body.String()
	for _, key := range []string{"rank", "severity", "runs", "last", "first"} {
		if !strings.Contains(body, "sort="+key) {
			t.Errorf("sort control omits %s", key)
		}
	}
}

// TestAppCSS_SortRowHiddenAboveNarrowMarkerReadableProblemsNoWrap pins item
// 10 (FIX3)'s three CSS-only fixes, read straight off the served
// stylesheet (there is no browser here to render a media query against):
// the sort-chip row is hidden above 640px (the sortable column headers
// already cover that width), a sort marker inside an active (accent-filled)
// filter chip is readable (color: inherit, not the low-contrast --fg-dim
// it would otherwise inherit), and a /problems row's dates/totals line
// never wraps at that same width.
func TestAppCSS_SortRowHiddenAboveNarrowMarkerReadableProblemsNoWrap(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	body := doGET(t, h, "/static/app.css").Body.String()
	if !strings.Contains(body, "@media (min-width: 641px)") {
		t.Fatalf("app.css missing a min-width:641px rule:\n%s", body)
	}
	if !strings.Contains(body, ".sort-row { display: none; }") {
		t.Errorf("app.css does not hide .sort-row above 640px:\n%s", body)
	}
	if !strings.Contains(body, ".filter.on .sort-marker { color: inherit; }") {
		t.Errorf("app.css does not make an active chip's sort-marker readable:\n%s", body)
	}
	for _, label := range []string{"Last seen", "First seen", "Runs hit"} {
		want := `table.stack td[data-label="` + label + `"]`
		if !strings.Contains(body, want) {
			t.Errorf("app.css missing a no-wrap rule for %q:\n%s", want, body)
		}
	}
}

// TestPages_ProblemsSortBeforeRegister keeps sorting available even when no
// matching rows exist; it does not depend on a desktop-only table header.
func TestPages_ProblemsSortBeforeRegister(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "psc1", "off", []runFixture{
		{runID: "psc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"psc1-a": "passed"})

	body := doGET(t, h, "/problems").Body.String()
	sortAt, registerAt := strings.Index(body, `class="list-sort"`), strings.Index(body, `class="card problem-table"`)
	if sortAt < 0 || registerAt < 0 || sortAt >= registerAt {
		t.Error("sort control is not before the problem register")
	}
}

// TestPages_ProblemsSurfaceLinkNarrowsList pins the filter-tester's finding:
// a problem row's surface chip is a link (listURL, keeping the other
// parameters) that narrows /problems to that surface.
func TestPages_ProblemsSurfaceLinkNarrowsList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sl2", "claude-sonnet-5", []runFixture{
		{runID: "sl2-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "sl2-b", scenario: "b", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sl2-a": "passed", "sl2-b": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl2-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "deploy surface problem"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl2-b", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "y",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_scale/scale", Title: "scale surface problem"}},
	})

	body := doGET(t, h, "/problems").Body.String()
	// Problem.Surface is already the clustering key's own truncated prefix
	// (up to the first "/") — the finding's raw "tool:zerops_deploy/deploy"
	// surface becomes the row's "tool:zerops_deploy".
	linkRE := regexp.MustCompile(`href="(/problems\?[^"]*surface=tool%3Azerops_deploy[^"]*)"`)
	m := linkRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("body missing a surface filter link for tool:zerops_deploy:\n%s", body)
	}

	narrowed := doGET(t, h, html.UnescapeString(m[1])).Body.String()
	if !strings.Contains(narrowed, "deploy surface problem") {
		t.Errorf("surface link did not keep the matching problem:\n%s", narrowed)
	}
	if strings.Contains(narrowed, "scale surface problem") {
		t.Errorf("surface link did not narrow out the other problem:\n%s", narrowed)
	}
}

// TestPages_ProblemsMemberLinksNarrowList pins the filter-tester's finding:
// a problem member's scenario/batch/build values are links (each via
// listURL, keeping the other parameters) that narrow /problems.
func TestPages_ProblemsMemberLinksNarrowList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	// Both members share scenario "scen-a" (seeded below).
	shared := func(runID string) observer.Observation {
		return observer.Observation{
			FormatVersion: observer.ObservationFormat2, RunID: runID,
			ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: now,
			Status: "ok", Outcome: observer.OutcomeProblem, Headline: "same tool problem",
			Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "always fails this way"}},
		}
	}

	seedBatch(t, store, "ml1", "claude-sonnet-5", []runFixture{
		{runID: "ml1-a", scenario: "scen-a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml1-a": "passed"})
	seedObservation(t, store, shared("ml1-a"))

	seedBatch(t, store, "ml2", "claude-sonnet-5", []runFixture{
		{runID: "ml2-a", scenario: "scen-a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml2-a": "passed"})
	seedObservation(t, store, shared("ml2-a"))

	// A second, unrelated problem in a batch with a different scenario,
	// batch id AND build (overwritten below — seedBatch always writes
	// "cand-sha"), so batch/scenario/build filters each have something
	// real to narrow out.
	seedBatch(t, store, "ml3", "claude-sonnet-5", []runFixture{
		{runID: "ml3-x", scenario: "scen-x", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml3-x": "passed"})
	// Older than ml1/ml2's shared CreatedAt (seedBatch's own fixed
	// timestamp) so it never becomes "the newest build" (§8.6) itself —
	// that would reclassify the ml1/ml2 problem as unconfirmed (not hit
	// on the newest build) and drop it out of status=live, the default
	// this test's own /problems fetch relies on.
	store.putJSON(t, "batches/ml3/manifest.json", farm.BatchManifest{
		Batch: "ml3", CreatedAt: "2026-08-01T00:00:00Z", StartedAt: "2026-08-01T00:00:00Z",
		Set: "gate", CandidateSha256: "other-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", Runs: []farm.ManifestRun{{RunID: "ml3-x", Scenario: "scen-x", ProjectName: "zcp-farm-ml3-x"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ml3-x", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "z",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "unrelated problem"}},
	})

	body := doGET(t, h, "/problems").Body.String()

	scenarioLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*scenario=scen-a[^"]*)"`)
	batchLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*batch=ml1[^"]*)"`)
	buildLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*build=cand-sha[^"]*)"`)

	for name, re := range map[string]*regexp.Regexp{"scenario": scenarioLinkRE, "batch": batchLinkRE, "build": buildLinkRE} {
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("body missing a %s filter link on a problem member:\n%s", name, body)
		}
		narrowed := doGET(t, h, html.UnescapeString(m[1])).Body.String()
		if !strings.Contains(narrowed, "always fails this way") {
			t.Errorf("%s link did not keep the matching problem:\n%s", name, narrowed)
		}
		if strings.Contains(narrowed, "unrelated problem") {
			t.Errorf("%s link did not narrow out the unrelated problem:\n%s", name, narrowed)
		}
	}
}

// FM-55: the list summary describes only rows matching every active filter.
func TestPages_ProblemsFilteredCount_MatchesVisibleRows(t *testing.T) {
	t.Parallel()
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "filtered", "claude-sonnet-5", []runFixture{
		{runID: "filtered-a", scenario: "alpha", startedAt: now, costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "filtered-b", scenario: "beta", startedAt: now, costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"filtered-a": "passed", "filtered-b": "passed"})
	seedFormat2Finding(t, store, "filtered-a", now, observer.SeverityHigh, "tool:zerops_deploy", "ALPHA", "Alpha problem", "Fix alpha", 2, "alpha")
	seedFormat2Finding(t, store, "filtered-b", now, observer.SeverityLow, "tool:zerops_import", "BETA", "Beta problem", "Fix beta", 2, "beta")
	for _, tc := range []struct{ path, summary string }{
		{"/problems", "2 matching problems · 2 live · 1 high"},
		{"/problems?scenario=alpha", "1 matching problem · 1 live · 1 high"},
		{"/problems?scenario=beta", "1 matching problem · 1 live · 0 high"},
		{"/problems?cause=platform", "0 matching problems · 0 live · 0 high"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			rr := doGET(t, srv.Handler(), tc.path)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), tc.summary) {
				t.Errorf("missing filtered summary %q", tc.summary)
			}
		})
	}
}

// FM-51/FM-55: an empty match set offers recovery without claiming no source data.
func TestPages_ProblemsNoMatches_ResetRecovery(t *testing.T) {
	t.Parallel()
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "recover", "claude-sonnet-5", []runFixture{{runID: "recover-a", scenario: "alpha", startedAt: now, costUsd: 0.1, done: true, taskResult: "passed"}}, true, map[string]string{"recover-a": "passed"})
	seedFormat2Finding(t, store, "recover-a", now, observer.SeverityHigh, "tool:zerops_deploy", "ALPHA", "Recoverable problem", "Fix alpha", 2, "alpha")
	body := doGET(t, srv.Handler(), "/problems?scenario=missing&sort=last&dir=asc").Body.String()
	for _, want := range []string{"No problems match these filters", "Reset filters", `href="/problems?dir=asc&amp;sort=last"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no-match result missing %q", want)
		}
	}
	if strings.Contains(body, "No problems in this window") {
		t.Error("no-match result falsely claims the window has no problems")
	}
	reset := doGET(t, srv.Handler(), "/problems?dir=asc&sort=last").Body.String()
	if !strings.Contains(reset, "Recoverable problem") {
		t.Error("reset did not restore the problem")
	}
}

// FM-51: the problem title opens its canonical finding, not just a run header.
func TestPages_ProblemTitle_OpensRepresentativeFinding(t *testing.T) {
	t.Parallel()
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	seedBatch(t, store, "finding-link", "claude-sonnet-5", []runFixture{{runID: "finding-link-a", scenario: "alpha", startedAt: now, costUsd: 0.1, done: true, taskResult: "passed"}}, true, map[string]string{"finding-link-a": "passed"})
	seedFormat2Finding(t, store, "finding-link-a", now, observer.SeverityHigh, "tool:zerops_deploy", "ALPHA", "Trace this problem", "Fix alpha", 2, "alpha")
	body := doGET(t, srv.Handler(), "/problems").Body.String()
	if !regexp.MustCompile(`<a[^>]*href="/r/finding-link-a#f1"[^>]*>Trace this problem</a>`).MatchString(body) {
		t.Error("problem title does not link to its representative finding #f1")
	}
	if !strings.Contains(doGET(t, srv.Handler(), "/r/finding-link-a").Body.String(), `id="f1"`) {
		t.Error("representative finding target #f1 is absent")
	}
}

// FM-51: a problem member quotes the first found step quote. Unverified
// citations and the step-zero CHECKS citation must not become a false #s0 link.
func TestPages_ProblemMembers_ShowFirstFoundStepQuote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		evidence     []observer.Evidence
		want, absent string
	}{
		{"first found", []observer.Evidence{{Step: 2, Quote: "unmatched quotation", Verified: false}, {Step: 3, Quote: "discovered ok", Verified: true}}, `href="/r/quote-a#s3"`, "unmatched quotation"},
		{"checks only", []observer.Evidence{{Step: 0, Quote: "check verdict", Verified: true}}, "No verified step quote recorded", "#s0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, store, _ := testServer(t)
			now := fixedNow(t)()
			seedBatch(t, store, "quote", "claude-sonnet-5", []runFixture{{runID: "quote-a", scenario: "alpha", startedAt: now, costUsd: 0.1, done: true, taskResult: "passed"}}, true, map[string]string{"quote-a": "passed"})
			seedObservation(t, store, observer.Observation{FormatVersion: observer.ObservationFormat2, RunID: "quote-a", ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "Quote finding", Evidence: tc.evidence}}})
			body := doGET(t, srv.Handler(), "/problems").Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("member missing %q", tc.want)
			}
			if strings.Contains(body, tc.absent) {
				t.Errorf("member incorrectly shows %q", tc.absent)
			}
		})
	}
}

// FM-55: severity is counted across matching rows, including gone problems.
func TestProblemRowView_UnknownTimesRenderAsDashes(t *testing.T) {
	row := newProblemRowView(Problem{Key: "unknown-time"}, "/problems", nil)
	if row.FirstSeenShort != unknownDash || row.LastSeenShort != unknownDash {
		t.Fatalf("unknown time labels = %q/%q, want dashes", row.FirstSeenShort, row.LastSeenShort)
	}
}

func TestPages_ProblemsHighSummary_CountsNonLiveMatches(t *testing.T) {
	t.Parallel()
	srv, store, _ := testServer(t)
	seedInScopeVsNewestBuildFixture(t, store, fixedNow(t)())
	for _, path := range []string{"/problems?status=gone", "/problems?status=all"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			body := doGET(t, srv.Handler(), path).Body.String()
			if !strings.Contains(body, "1 matching problem · 0 live · 1 high") {
				t.Error("summary omits the matching gone problem's high severity")
			}
		})
	}
}
