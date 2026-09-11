package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

func doGET(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(rr, req)
	return rr
}

// fixtureObservation builds a stored observation whose one high/zcp-tool
// finding cites step 3 (fixtureTranscript's tool step) with a verified
// quote and step-0 CHECKS citation, plus a medium/agent finding — enough
// to exercise ordering, evidenceSteps, and the unverified-quote count.
func fixtureObservation(runID string) observer.Observation {
	return observer.Observation{
		FormatVersion: observer.ObservationFormat1,
		RunID:         runID,
		ObsID:         "20260911T120000000Z-claude-sonnet-5",
		Model:         "claude-sonnet-5",
		CreatedAt:     time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
		Status:        "ok",
		Headline:      "Agent completed the task cleanly",
		Goal:          observer.Goal{Reached: "yes", Why: "it worked"},
		Checks:        observer.Checks{Verdict: "passed", Agree: true},
		Findings: []observer.Finding{
			{
				Severity: "high", Owner: "zcp-tool", Title: "Tool returned stale data",
				What: "the discover call raced the import",
				Evidence: []observer.Evidence{
					{Step: 3, Quote: "discovered ok", Verified: true},
				},
				LookAt: "internal/ops/discover.go", Fix: "poll before returning",
			},
			{
				Severity: "medium", Owner: "agent", Title: "Agent skipped a sanity check",
				What: "should have re-verified",
				Evidence: []observer.Evidence{
					{Step: 99, Quote: "not really there", Verified: false},
				},
				LookAt: "step 99", Fix: "n/a",
			},
		},
		SelfReview: observer.SelfReview{Accurate: "yes"},
	}
}

// TestAPI_RunsSinceWindow pins §8.4 FM-52's window filtering: 24h, 7d, and
// a bad window string (400). Independent oracle: fixed "now" +
// hand-chosen offsets, checked against the spec's own window semantics.
func TestAPI_RunsSinceWindow(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "wb1", "off", []runFixture{
		{runID: "wb1-recent", scenario: "recent", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "wb1-midweek", scenario: "midweek", startedAt: now.Add(-72 * time.Hour), durationS: "5s", costUsd: 0.2, taskResult: "passed", done: true},
		{runID: "wb1-old", scenario: "old", startedAt: now.Add(-240 * time.Hour), durationS: "5s", costUsd: 0.3, taskResult: "passed", done: true},
	}, false, nil)

	cases := []struct {
		name   string
		window string
		status int
		want   []string
		absent []string
	}{
		{"24h", "24h", http.StatusOK, []string{"wb1-recent"}, []string{"wb1-midweek", "wb1-old"}},
		{"7d", "7d", http.StatusOK, []string{"wb1-recent", "wb1-midweek"}, []string{"wb1-old"}},
		{"bad window", "not-a-window", http.StatusBadRequest, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doGET(t, h, "/api/runs.md?since="+tc.window)
			if rr.Code != tc.status {
				t.Fatalf("got %d, want %d, body=%s", rr.Code, tc.status, rr.Body.String())
			}
			if tc.status != http.StatusOK {
				return
			}
			body := rr.Body.String()
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("body missing %q:\n%s", w, body)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(body, a) {
					t.Errorf("body unexpectedly contains %q:\n%s", a, body)
				}
			}
		})
	}
}

// TestAPI_RunsListFiltersSortAndGrouping pins §8.4/§8.7 for GET
// /api/runs.md|.json: the same verdict/outcome/cause/batch filters and
// sort=newest as the run lists' query engine (never hand-parsed), rows
// grouped under "## <batch>" markdown headers, a leading legend line, the
// enriched per-row fields (verdictReason/outcome/causeCounts/
// failedCheckIds), and 400 {error, allowed} for an unknown parameter.
func TestAPI_RunsListFiltersSortAndGrouping(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "rl1", "off", []runFixture{
		{runID: "rl1-a", scenario: "a", startedAt: now.Add(-3 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, false, nil)
	seedBatch(t, store, "rl2", "off", []runFixture{
		{
			runID: "rl2-b", scenario: "b", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.2,
			taskResult: "failed", done: true,
			checks: [][5]string{{"chk/1", "failed", "1", "2", "results/verification.json"}},
		},
	}, true, map[string]string{"rl2-b": "failed"})

	// verdict=failed narrows to rl2-b only.
	rr := doGET(t, h, "/api/runs.json?verdict=failed")
	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].RunID != "rl2-b" {
		t.Fatalf("verdict=failed: got %+v, want exactly rl2-b", out.Runs)
	}
	if len(out.Runs[0].FailedCheckIDs) != 1 || out.Runs[0].FailedCheckIDs[0] != "chk/1" {
		t.Errorf("failedCheckIds = %+v, want [chk/1]", out.Runs[0].FailedCheckIDs)
	}

	// No filter: newest first (rl2-b, 1h ago) before rl1-a (3h ago), grouped
	// under batch headers in markdown.
	mdBody := doGET(t, h, "/api/runs.md").Body.String()
	if !strings.HasPrefix(mdBody, "Legend: ") {
		t.Errorf("runs.md missing its leading legend line:\n%s", mdBody)
	}
	if !strings.Contains(mdBody, "## rl2") || !strings.Contains(mdBody, "## rl1") {
		t.Errorf("runs.md missing per-batch headers:\n%s", mdBody)
	}
	if strings.Index(mdBody, "rl2-b") > strings.Index(mdBody, "rl1-a") {
		t.Errorf("runs.md not newest-first:\n%s", mdBody)
	}

	rrBad := doGET(t, h, "/api/runs.json?verdict=bogus")
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("bad verdict value: got %d, want 400", rrBad.Code)
	}
}

// TestAPI_RunsByBatch pins GET /api/runs.md?batch=<id>: every run of the
// batch, regardless of window.
func TestAPI_RunsByBatch(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "bb1", "off", []runFixture{
		{runID: "bb1-a", scenario: "a", startedAt: now.Add(-500 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "bb1-b", scenario: "b", startedAt: now.Add(-500 * time.Hour), durationS: "5s", costUsd: 0.2, taskResult: "failed", done: true},
	}, false, nil)

	rr := doGET(t, h, "/api/runs.json?batch=bb1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("got %d runs, want 2: %+v", len(out.Runs), out.Runs)
	}
}

// TestAPI_RunMarkdownHasObservationFailedChecksAndStepRanges pins the
// run-detail markdown: current observation rendering (S1's render.go),
// failed/blocked checks, and the evidence step ranges.
func TestAPI_RunMarkdownHasObservationFailedChecksAndStepRanges(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "rb1", "claude-sonnet-5", []runFixture{
		{
			runID: "rb1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.5,
			taskResult: "failed", done: true,
			checks: [][5]string{{"liveness/api/marker", "failed", "200", "500", "results/verification.json"}},
		},
	}, true, map[string]string{"rb1-scena": "failed"})
	seedObservation(t, store, fixtureObservation("rb1-scena"))

	rr := doGET(t, h, "/api/runs/rb1-scena.md")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"Agent completed the task cleanly", // observation headline
		"liveness/api/marker",              // failed check id
		"3",                                // evidence step range
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run detail markdown missing %q:\n%s", want, body)
		}
	}
}

// TestAPI_RunDetailHasStepCountAndLinks pins §8.4's run-detail header:
// "(with step count) ... links to the task prompt and self-review."
func TestAPI_RunDetailHasStepCountAndLinks(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "rd1", "off", []runFixture{
		{runID: "rd1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true, selfReview: "all good"},
	}, false, nil)

	rr := doGET(t, h, "/api/runs/rd1-scena.md")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	// fixtureTranscript's steps: #1 user, #2 agent, #3 tool.
	if !strings.Contains(body, "steps: 3") {
		t.Errorf("run detail missing step count:\n%s", body)
	}
	if !strings.Contains(body, "/api/runs/rd1-scena/steps.md?n=1") {
		t.Errorf("run detail missing a task-prompt link:\n%s", body)
	}
	if !strings.Contains(body, "/api/runs/rd1-scena/self-review.md") {
		t.Errorf("run detail missing a self-review link:\n%s", body)
	}

	var detail RunDetail
	if err := json.Unmarshal(doGET(t, h, "/api/runs/rd1-scena.json").Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.StepCount != 3 {
		t.Errorf("StepCount = %d, want 3", detail.StepCount)
	}
	if detail.TaskPromptURL != "/api/runs/rd1-scena/steps.md?n=1" || detail.SelfReviewURL != "/api/runs/rd1-scena/self-review.md" {
		t.Errorf("detail URLs = %+v", detail)
	}
}

// TestAPI_StepsFromToUntruncated pins GET
// /api/runs/<runId>/steps.md?from=<n>&to=<m>: the run's steps, verbatim —
// no digest-style truncation.
func TestAPI_StepsFromToUntruncated(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sb1", "off", []runFixture{
		{runID: "sb1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)

	rr := doGET(t, h, "/api/runs/sb1-scena/steps.md?from=1&to=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"#1 user", "do the thing for scena",
		"#2 agent", "looking into it",
		"#3 tool zerops_discover", `{"project":"p1"}`, "discovered ok",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("steps.md missing %q:\n%s", want, body)
		}
	}

	rrJSON := doGET(t, h, "/api/runs/sb1-scena/steps.json?from=3&to=3")
	var steps []StepJSON
	if err := json.Unmarshal(rrJSON.Body.Bytes(), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(steps) != 1 || steps[0].Tool != "zerops_discover" || steps[0].Input != `{"project":"p1"}` || steps[0].Result != "discovered ok" {
		t.Errorf("steps.json[0] = %+v, want the full untruncated tool step", steps)
	}
}

// TestAPI_StepsSingleNTruncationAndEmptyThinking pins §8.4's remaining
// steps.md rules: `?n=<step>` as shorthand for `from=n&to=n`, a tool
// result over 2,000 chars cut (said) unless `full=1`, and an empty
// thinking block left out.
func TestAPI_StepsSingleNTruncationAndEmptyThinking(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sn1", "off", []runFixture{
		{runID: "sn1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)

	// ?n=3 is shorthand for from=3&to=3 — the tool step alone.
	rr := doGET(t, h, "/api/runs/sn1-scena/steps.md?n=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "zerops_discover") || strings.Contains(body, "#1 user") {
		t.Errorf("?n=3 should return only step 3:\n%s", body)
	}

	longResult := strings.Repeat("x", 2500)
	store.putText(t, "runs/sn1-scena/results/"+testResultsTS+"/scena/transcript.jsonl", strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"   "},{"type":"tool_use","id":"tu1","name":"zerops_discover","input":{}}]}}`,
		fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":%q}]}]}}`, longResult),
	}, "\n")+"\n")

	rrCut := doGET(t, h, "/api/runs/sn1-scena/steps.md?from=1&to=10")
	cutBody := rrCut.Body.String()
	if strings.Contains(cutBody, "#2 thinking") {
		t.Errorf("empty thinking block should be left out:\n%s", cutBody)
	}
	if strings.Contains(cutBody, longResult) {
		t.Errorf("2000-char result should be cut without full=1:\n%s", cutBody)
	}
	if !strings.Contains(cutBody, "truncated") {
		t.Errorf("truncation should be said:\n%s", cutBody)
	}

	rrFull := doGET(t, h, "/api/runs/sn1-scena/steps.md?from=1&to=10&full=1")
	if !strings.Contains(rrFull.Body.String(), longResult) {
		t.Errorf("full=1 should return the untruncated result:\n%s", rrFull.Body.String())
	}
}

// TestAPI_StepsCentersOnCitedQuoteWhenTruncated pins the verification
// round's item 3: a step's tool result cut at 2,000 chars must still
// surface the exact quote an evidence citation points at, even when that
// quote sits past the cut — recover run step 16's own bug, where the agent
// fetched the step to check an anchor quote and never saw it.
func TestAPI_StepsCentersOnCitedQuoteWhenTruncated(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sq1", "claude-sonnet-5", []runFixture{
		{runID: "sq1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	// fixtureObservation cites step 3 with the quote "discovered ok" —
	// overwrite that step's tool result so the quote sits well past the
	// 2,000-char cut, like the real recover-run bug.
	longResult := strings.Repeat("x", 2500) + "discovered ok"
	store.putText(t, "runs/sq1-scena/results/"+testResultsTS+"/scena/transcript.jsonl", strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"looking into it"},{"type":"tool_use","id":"tu1","name":"zerops_discover","input":{"project":"p1"}}]}}`,
		fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":%q}]}]}}`, longResult),
	}, "\n")+"\n")
	seedObservation(t, store, fixtureObservation("sq1-scena"))

	rr := doGET(t, h, "/api/runs/sq1-scena/steps.md?n=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "discovered ok") {
		t.Errorf("steps.md?n=3 cut the cited quote out entirely:\n%s", body)
	}
	if !strings.Contains(body, "truncated") {
		t.Errorf("truncation should still be said:\n%s", body)
	}

	var out []StepJSON
	if err := json.Unmarshal(doGET(t, h, "/api/runs/sq1-scena/steps.json?n=3").Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || !out[0].Truncated {
		t.Fatalf("steps.json?n=3 = %+v, want one truncated step", out)
	}
	if !strings.Contains(out[0].Result, "discovered ok") && out[0].CutNote == "" {
		t.Errorf("steps.json?n=3 step lost the cited quote with no cutNote either: %+v", out[0])
	}
}

// TestAPI_RunsListFindingsFormatIsSpelledOut pins the verification round's
// item 4: a runs-list line's per-cause-class finding counts read "zcp 1
// high 1 med", not the old "ZCP:1/1" shorthand a reader has to already
// know the meaning of.
func TestAPI_RunsListFindingsFormatIsSpelledOut(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ff1", "claude-sonnet-5", []runFixture{
		{runID: "ff1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, false, nil)
	seedObservation(t, store, fixtureObservation("ff1-a")) // 1 zcp-tool high, 1 agent medium

	body := doGET(t, h, "/api/runs.md?batch=ff1").Body.String()
	if !strings.Contains(body, "zcp 1 high 0 med") {
		t.Errorf("runs.md findings should spell out the count, not \"ZCP:1/0\":\n%s", body)
	}
	if strings.Contains(body, "ZCP:1") || strings.Contains(body, "Agent:0") {
		t.Errorf("runs.md still uses the old class:high/med shorthand:\n%s", body)
	}
}

// TestAPI_BatchesMDSaysItDefaultedKind pins the verification round's item
// 5: batches.md silently applies kind=evaluation — an omitted kind must say
// so in one line, and an explicit kind (even kind=evaluation itself) must
// not.
func TestAPI_BatchesMDSaysItDefaultedKind(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "bd1", "off", []runFixture{
		{runID: "bd1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)

	defaulted := doGET(t, h, "/api/batches.md").Body.String()
	if !strings.Contains(defaulted, "kind=evaluation") || !strings.Contains(defaulted, "default") {
		t.Errorf("batches.md must say it defaulted to kind=evaluation:\n%s", defaulted)
	}

	explicit := doGET(t, h, "/api/batches.md?kind=evaluation").Body.String()
	if strings.Contains(explicit, "default") {
		t.Errorf("batches.md must not claim a default when kind was given explicitly:\n%s", explicit)
	}

	all := doGET(t, h, "/api/batches.md?kind=all").Body.String()
	if strings.Contains(all, "default") {
		t.Errorf("batches.md must not claim a default when kind=all was given:\n%s", all)
	}
}

// TestAPI_RunDetailMDUsesBuildLabelsForCandidateAndEvaluator pins the
// verification round's item 5: run-detail markdown must show the §8.8
// build label for both candidate and evaluator, not two indistinguishable
// raw 64-hex shas.
func TestAPI_RunDetailMDUsesBuildLabelsForCandidateAndEvaluator(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "bl1", "off", []runFixture{
		{runID: "bl1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)

	body := doGET(t, h, "/api/runs/bl1-a.md").Body.String()
	if strings.Contains(body, "ZCP build: cand-sha") {
		t.Errorf("run detail md should show the build label, not the bare raw candidate sha:\n%s", body)
	}
	if !strings.Contains(body, "ZCP build: build cand-sha") {
		t.Errorf("run detail md missing the candidate's §8.8 build label:\n%s", body)
	}
	if !strings.Contains(body, "evaluator build: build eval-sha") {
		t.Errorf("run detail md missing the evaluator's §8.8 build label:\n%s", body)
	}
}

// TestAPI_DigestDefaultsWindowTo30dAndPrintsIt pins part of the
// verification round's item 2: digest.md with neither batch= nor since=
// must use a 30d window (matching /problems' own default), not
// ParseWindow's bare 24h fallback, and must print the window it used.
func TestAPI_DigestDefaultsWindowTo30dAndPrintsIt(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "dw1", "off", []runFixture{
		{runID: "dw1-old", scenario: "a", startedAt: now.Add(-25 * 24 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"dw1-old": "passed"})

	body := doGET(t, h, "/api/digest.md").Body.String()
	if !strings.Contains(body, "window: 30d") {
		t.Errorf("digest.md must print the window it used:\n%s", body)
	}
	if !strings.Contains(body, "dw1") {
		t.Errorf("digest.md's default window must be 30d (a 25-day-old batch must be in scope):\n%s", body)
	}

	batchScoped := doGET(t, h, "/api/digest.md?batch=dw1").Body.String()
	if !strings.Contains(batchScoped, "window: batch dw1") {
		t.Errorf("digest.md?batch= must print its scope as the window:\n%s", batchScoped)
	}
}

// TestAPI_ProblemLinesSayHitInScopeAndSeenTotals pins the rest of item 2:
// a problem line reads "hit a/b in scope — seen N runs / M batches / K
// builds — status X", in both problems.md and digest.md, so the same
// figures a batch-scoped digest.md call already carries (naturally 1/1,
// "first seen") are never confused with the problem's own across-history
// totals.
func TestAPI_ProblemLinesSayHitInScopeAndSeenTotals(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ps1", "claude-sonnet-5", []runFixture{
		{runID: "ps1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, true, map[string]string{"ps1-a": "passed"})
	seedObservation(t, store, fixtureObservation("ps1-a"))

	for _, body := range []string{
		doGET(t, h, "/api/problems.md").Body.String(),
		doGET(t, h, "/api/digest.md?batch=ps1").Body.String(),
	} {
		if !strings.Contains(body, "hit 1/1 in scope") {
			t.Errorf("problem line missing \"hit a/b in scope\":\n%s", body)
		}
		if !strings.Contains(body, "seen 1 runs / 1 batches / 1 builds") {
			t.Errorf("problem line missing the seen-totals phrase:\n%s", body)
		}
		if strings.Contains(body, "runs on newest build") {
			t.Errorf("problem line still uses the old \"on newest build\" wording:\n%s", body)
		}
	}
}

// TestAPI_ObservationByID pins GET
// /api/runs/<runId>/observations/<obsId>.md (§8.4): one stored observation,
// with its JSON twin, and 404 for a missing/foreign obsId.
func TestAPI_ObservationByID(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ob1", "claude-sonnet-5", []runFixture{
		{runID: "ob1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	obs := fixtureObservation("ob1-scena")
	seedObservation(t, store, obs)

	rr := doGET(t, h, "/api/runs/ob1-scena/observations/"+obs.ObsID+".md")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), obs.Headline) {
		t.Errorf("observation markdown missing headline:\n%s", rr.Body.String())
	}

	var gotObs observer.Observation
	if err := json.Unmarshal(doGET(t, h, "/api/runs/ob1-scena/observations/"+obs.ObsID+".json").Body.Bytes(), &gotObs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gotObs.ObsID != obs.ObsID || gotObs.Headline != obs.Headline {
		t.Errorf("observation json = %+v", gotObs)
	}

	rrMissing := doGET(t, h, "/api/runs/ob1-scena/observations/nonexistent-obs-id.md")
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("missing obsId: got %d, want 404", rrMissing.Code)
	}
}

// TestAPI_SelfReview pins GET /api/runs/<runId>/self-review.md|.json.
func TestAPI_SelfReview(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "rv1", "off", []runFixture{
		{runID: "rv1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true, selfReview: "I did great, no issues."},
	}, false, nil)

	rrMD := doGET(t, h, "/api/runs/rv1-scena/self-review.md")
	if rrMD.Code != http.StatusOK || rrMD.Body.String() != "I did great, no issues." {
		t.Fatalf("self-review.md: got %d %q", rrMD.Code, rrMD.Body.String())
	}

	rrJSON := doGET(t, h, "/api/runs/rv1-scena/self-review.json")
	var out struct {
		RunID      string `json:"runId"`
		SelfReview string `json:"selfReview"`
	}
	if err := json.Unmarshal(rrJSON.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SelfReview != "I did great, no issues." || out.RunID != "rv1-scena" {
		t.Errorf("self-review.json = %+v", out)
	}
}

// TestAPI_FindingsDefaultSortIsSeverity pins §8.7's /findings row: the
// default sort key is `severity` desc (findingListSpec, findings_model.go)
// — superseding an earlier "grouped by owner" reading of §8.4 that predates
// the query-engine-driven list surface.
func TestAPI_FindingsDefaultSortIsSeverity(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fb1", "claude-sonnet-5", []runFixture{
		{runID: "fb1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	// fixtureObservation carries one high/zcp-tool finding then one
	// medium/agent finding — severity desc puts the high one first.
	seedObservation(t, store, fixtureObservation("fb1-scena"))

	rr := doGET(t, h, "/api/findings.json?since=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Findings []FindingItem `json:"findings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Findings) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(out.Findings), out.Findings)
	}
	if out.Findings[0].Severity != "high" || out.Findings[1].Severity != "medium" {
		t.Errorf("findings not severity-desc: %+v", out.Findings)
	}
}

// TestAPI_FindingsListFiltersSortAndFields pins §8.4/§8.7 for GET
// /api/findings.md|.json: batch/scenario/build/started/severity/cause/
// surface/anchor fields (§8.4: "every finding in scope: batch, scenario,
// build, run id, started, severity, cause, surface, anchor, title, what,
// steps, quotes found n/m, where to look, fix"), the same query engine as
// /findings (§8.7: cause/severity/surface/scenario/batch/build/since,
// sort=severity default), the leading legend line, and 400 for an unknown
// parameter or a closed value outside its set.
func TestAPI_FindingsListFiltersSortAndFields(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fl1", "claude-sonnet-5", []runFixture{
		{runID: "fl1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	seedObservation(t, store, fixtureObservation("fl1-scena"))

	rr := doGET(t, h, "/api/findings.json?cause=zcp")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Findings []FindingItem `json:"findings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Findings) != 1 {
		t.Fatalf("cause=zcp: got %d findings, want 1 (only the zcp-tool one): %+v", len(out.Findings), out.Findings)
	}
	f := out.Findings[0]
	if f.Batch != "fl1" || f.Scenario != "scena" || f.RunID != "fl1-scena" || f.Severity != "high" || f.Cause != "ZCP tool" {
		t.Errorf("finding fields = %+v, want batch/scenario/runId/severity/cause populated", f)
	}
	if f.StartedAt.IsZero() {
		t.Error("finding StartedAt is zero, want the run's own started time")
	}

	mdBody := doGET(t, h, "/api/findings.md").Body.String()
	if !strings.HasPrefix(mdBody, "Legend: ") {
		t.Errorf("findings.md missing its leading legend line:\n%s", mdBody)
	}
	if !strings.Contains(mdBody, "fl1-scena") || !strings.Contains(mdBody, "Tool returned stale data") {
		t.Errorf("findings.md missing expected content:\n%s", mdBody)
	}

	rrBad := doGET(t, h, "/api/findings.json?cause=bogus")
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("bad cause value: got %d, want 400", rrBad.Code)
	}
}

// TestAPI_FilesOnlyUnderResults pins GET
// /api/runs/<runId>/files/<path>: only results/ files, as text/plain.
func TestAPI_FilesOnlyUnderResults(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "flb1", "off", []runFixture{
		{runID: "flb1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	store.putText(t, "runs/flb1-scena/results/a/b/meta.json", `{"ok":true}`)

	cases := []struct {
		path   string
		status int
	}{
		{"results/a/b/meta.json", http.StatusOK},
		{"done.json", http.StatusNotFound},
		{"capture/x", http.StatusNotFound},
		{"results/../done.json", http.StatusNotFound},
		{"results%2F..%2Fdone.json", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rr := doGET(t, h, "/api/runs/flb1-scena/files/"+tc.path)
			if rr.Code != tc.status {
				t.Fatalf("got %d, want %d, body=%s", rr.Code, tc.status, rr.Body.String())
			}
			if tc.status == http.StatusOK {
				if ct := rr.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
					t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
				}
				if rr.Body.String() != `{"ok":true}` {
					t.Errorf("body = %q", rr.Body.String())
				}
			}
		})
	}
}

// TestAPI_InvalidRunOrBatchIDRejected pins FM-47: an id failing the run-id
// or batch-id grammar is rejected with NO store call, at every run-scoped
// and batch-scoped route — not just one representative route. A route that
// falls through to a deeper check (e.g. observer.NewSinkBundle's own
// ValidRunID) can still answer 404 while having already made a Head/List
// call; call-count is the load-bearing assertion here, status is
// secondary. Every route answers 404 for an invalid id (including the
// batch-scoped ones — handleRunsList checks farm.ValidBatchID itself and
// never falls through to ParseWindow's 400 path for a bad id).
func TestAPI_InvalidRunOrBatchIDRejected(t *testing.T) {
	invalidIDs := []string{"A_B", "-x", "x..y"}

	runRouteTemplates := []string{
		"/api/runs/%s.md",
		"/api/runs/%s.json",
		"/api/runs/%s/steps.md",
		"/api/runs/%s/self-review.md",
		"/api/runs/%s/files/results/a/b/meta.json",
		"/api/runs/%s/observations/some-obs-id.md",
	}
	batchRouteTemplates := []string{
		"/api/runs.md?batch=%s",
		"/api/runs.json?batch=%s",
	}

	assertRejected := func(t *testing.T, path string) {
		t.Helper()
		counting := &countingStore{}
		srv := NewServer(Config{Store: counting, Token: testToken, Now: fixedNow(t)})
		rr := doGET(t, srv.Handler(), path)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404, body=%s", path, rr.Code, rr.Body.String())
		}
		if counting.calls != 0 {
			t.Errorf("%s: %d store calls made for an invalid id, want 0", path, counting.calls)
		}
	}

	for _, id := range invalidIDs {
		for _, tmpl := range runRouteTemplates {
			path := fmt.Sprintf(tmpl, id)
			t.Run(path, func(t *testing.T) { assertRejected(t, path) })
		}
		for _, tmpl := range batchRouteTemplates {
			path := fmt.Sprintf(tmpl, id)
			t.Run(path, func(t *testing.T) { assertRejected(t, path) })
		}
	}

	// A percent-encoded slash inside the run-id path segment: net/http
	// decodes %2F to '/' in r.URL.Path before this package's own routing
	// ever runs, so "x%2Fresults.md" arrives as the two-segment path
	// "x/results.md" — routed as run "x", tail "results.md", which matches
	// no known sub-resource and 404s without ever reaching farm.ValidRunID.
	t.Run("/api/runs/x%2Fresults.md", func(t *testing.T) {
		assertRejected(t, "/api/runs/x%2Fresults.md")
	})
}

// TestAPI_Digest pins GET /api/digest.md?batch=<id> (§8.4): the scope
// header (batches, builds, verdict counts, cost), ranked problems (§8.6),
// failed/blocked runs with their failed checks and headline, the finished-
// not-yet-assessed count, a leading legend line, and the 8 KB cap with
// truncation said (never silent) once the content overflows it.
func TestAPI_Digest(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "dg1", "claude-sonnet-5", []runFixture{
		{
			runID: "dg1-failing", scenario: "failing", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 1.0,
			taskResult: "failed", done: true,
			checks: [][5]string{{"chk/1", "failed", "1", "2", "results/verification.json"}},
		},
		{runID: "dg1-unassessed", scenario: "unassessed", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.5, taskResult: "passed", done: true},
	}, true, map[string]string{"dg1-failing": "failed", "dg1-unassessed": "passed"})
	seedObservation(t, store, fixtureObservation("dg1-failing"))

	rr := doGET(t, h, "/api/digest.md?batch=dg1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, "Legend: ") {
		t.Errorf("digest.md missing its leading legend line:\n%s", body)
	}
	if rr.Body.Len() > 8192 {
		t.Errorf("digest.md exceeds 8 KB: %d bytes", rr.Body.Len())
	}
	for _, want := range []string{"dg1", "dg1-failing", "Tool returned stale data", "Agent completed the task cleanly", "Unassessed: 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest.md missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "truncated") {
		t.Errorf("small digest should not report truncation:\n%s", body)
	}

	// Truncation: many runs, each with its own (format-1, so "solo") high
	// finding, overflow the 8 KB cap — truncation must be said, and the
	// body must still fit the cap.
	runs := make([]runFixture, 0, 60)
	summaryResults := map[string]string{}
	for i := range 60 {
		runID := fmt.Sprintf("dg2-r%02d", i)
		runs = append(runs, runFixture{runID: runID, scenario: fmt.Sprintf("s%02d", i), startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true})
		summaryResults[runID] = "passed"
	}
	seedBatch(t, store, "dg2", "claude-sonnet-5", runs, true, summaryResults)
	for _, rf := range runs {
		obs := fixtureObservation(rf.runID)
		obs.ObsID = "20260911T120000000Z-" + rf.runID
		seedObservation(t, store, obs)
	}

	rrTrunc := doGET(t, h, "/api/digest.md?batch=dg2")
	if rrTrunc.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rrTrunc.Code, rrTrunc.Body.String())
	}
	if rrTrunc.Body.Len() > 8192 {
		t.Errorf("truncated digest.md still exceeds 8 KB: %d bytes", rrTrunc.Body.Len())
	}
	if !strings.Contains(rrTrunc.Body.String(), "truncated") {
		t.Errorf("overflowing digest.md must say it was truncated:\n%s", rrTrunc.Body.String())
	}
}

// TestAPI_BatchesList pins GET /api/batches.md|.json (§8.4): the Overview's
// batches table, filtered/sorted through the same query engine as the page
// (§8.7's Overview-batches row), carrying a one-line legend (§8.8 FM-56) and
// answering an unknown parameter with 400 {error, allowed} (JSON) or a
// one-line text (markdown).
func TestAPI_BatchesList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ab1", "off", []runFixture{
		{runID: "ab1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	seedBatch(t, store, "ab2-empty", "off", []runFixture{
		{runID: "ab2-empty-a", scenario: "a", startedAt: now.Add(-time.Hour), done: false},
	}, false, nil)

	rr := doGET(t, h, "/api/batches.md")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, "Legend: ") {
		t.Errorf("batches.md missing its leading legend line:\n%s", body)
	}
	if !strings.Contains(body, "ab1") {
		t.Errorf("batches.md missing evaluation batch ab1:\n%s", body)
	}
	if strings.Contains(body, "ab2-empty") {
		t.Errorf("batches.md default kind=evaluation must exclude the empty batch:\n%s", body)
	}

	rrAll := doGET(t, h, "/api/batches.json?kind=all")
	var out struct {
		Batches []BatchListItem `json:"batches"`
	}
	if err := json.Unmarshal(rrAll.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Batches) != 2 {
		t.Fatalf("kind=all: got %d batches, want 2: %+v", len(out.Batches), out.Batches)
	}

	rrBad := doGET(t, h, "/api/batches.json?bogus=1")
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("bad param: got %d, want 400", rrBad.Code)
	}
	var qerr queryErrorJSON
	if err := json.Unmarshal(rrBad.Body.Bytes(), &qerr); err != nil {
		t.Fatalf("unmarshal query error: %v", err)
	}
	if qerr.Error == "" || len(qerr.Allowed) == 0 {
		t.Errorf("query error = %+v, want a non-empty error and allowed list", qerr)
	}

	rrBadMD := doGET(t, h, "/api/batches.md?bogus=1")
	if rrBadMD.Code != http.StatusBadRequest {
		t.Fatalf("bad param (md): got %d, want 400", rrBadMD.Code)
	}
	if lines := strings.Split(strings.TrimRight(rrBadMD.Body.String(), "\n"), "\n"); len(lines) != 1 {
		t.Errorf("bad param (md) body should be one line: %q", rrBadMD.Body.String())
	}
}

// TestAPI_ProblemsList pins GET /api/problems.md|.json (§8.4/§8.6): the
// clustered problems, with their members, through the same query engine and
// surface as /problems (§8.7), with the same legend/400 contract as the
// other list endpoints.
func TestAPI_ProblemsList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "pb1", "claude-sonnet-5", []runFixture{
		{runID: "pb1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, true, map[string]string{"pb1-scena": "passed"})
	seedObservation(t, store, fixtureObservation("pb1-scena"))

	rr := doGET(t, h, "/api/problems.md")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, "Legend: ") {
		t.Errorf("problems.md missing its leading legend line:\n%s", body)
	}
	for _, want := range []string{"Tool returned stale data", "pb1-scena"} {
		if !strings.Contains(body, want) {
			t.Errorf("problems.md missing %q:\n%s", want, body)
		}
	}

	rrJSON := doGET(t, h, "/api/problems.json")
	var out struct {
		Problems []ProblemItem `json:"problems"`
	}
	if err := json.Unmarshal(rrJSON.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Problems) != 2 {
		t.Fatalf("got %d problems, want 2 (one per finding, no anchor => solo): %+v", len(out.Problems), out.Problems)
	}
	found := false
	for _, p := range out.Problems {
		if len(p.Members) == 1 && p.Members[0].RunID == "pb1-scena" && p.Members[0].Title == "Tool returned stale data" {
			found = true
		}
	}
	if !found {
		t.Errorf("no problem carries the expected member: %+v", out.Problems)
	}

	rrBad := doGET(t, h, "/api/problems.json?bogus=1")
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("bad param: got %d, want 400", rrBad.Code)
	}
}

// countingStore is an ObjectStore that always reports "not found" and
// counts every call, so a test can prove a handler validated an id before
// ever touching the store.
type countingStore struct{ calls int }

func (c *countingStore) Get(context.Context, string) ([]byte, error) {
	c.calls++
	return nil, errFakeStoreNotFound
}
func (c *countingStore) Put(context.Context, string, []byte) error { c.calls++; return nil }
func (c *countingStore) Head(context.Context, string) (bool, int64, error) {
	c.calls++
	return false, 0, nil
}
func (c *countingStore) List(context.Context, string) ([]string, error) { c.calls++; return nil, nil }

// TestAPI_WindowUsesMetaStartedAt pins FM-52: the window is measured
// against meta.json.startedAt, falling back to the batch manifest's
// createdAt only when meta.json is absent (a run not yet done).
func TestAPI_WindowUsesMetaStartedAt(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	// Batch mw1: manifest.createdAt is OLD (outside the 24h window), but
	// the done run's meta.json.startedAt is recent — it must be found via
	// meta.json, proving meta wins over a stale manifest date.
	oldCreatedAt := now.Add(-500 * time.Hour).UTC().Format(time.RFC3339)
	manifestOld := farm.BatchManifest{
		Batch: "mw1", CreatedAt: oldCreatedAt, StartedAt: oldCreatedAt,
		Set: "gate", CandidateSha256: "c", EvaluatorSha256: "e", ScenariosDigest: "s", Observer: "off",
		Runs: []farm.ManifestRun{{RunID: "mw1-done", Scenario: "done", ProjectName: "p"}},
	}
	store.putJSON(t, "batches/mw1/manifest.json", manifestOld)
	seedRun(t, store, runFixture{runID: "mw1-done", scenario: "done", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1, taskResult: "passed", done: true})

	// Batch mw2: manifest.createdAt is RECENT (inside the window); its run
	// has no meta.json (not done yet) — it must be found via the
	// manifest's own createdAt fallback.
	recentCreatedAt := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	manifestRecent := farm.BatchManifest{
		Batch: "mw2", CreatedAt: recentCreatedAt, StartedAt: recentCreatedAt,
		Set: "gate", CandidateSha256: "c", EvaluatorSha256: "e", ScenariosDigest: "s", Observer: "off",
		Runs: []farm.ManifestRun{{RunID: "mw2-running", Scenario: "running", ProjectName: "p"}},
	}
	store.putJSON(t, "batches/mw2/manifest.json", manifestRecent)
	seedRun(t, store, runFixture{runID: "mw2-running", scenario: "running", startedAt: now.Add(-time.Hour), done: false})

	rr := doGET(t, h, "/api/runs.md?since=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "mw1-done") {
		t.Errorf("done run (recent meta.startedAt) missing despite old manifest.createdAt:\n%s", body)
	}
	if !strings.Contains(body, "mw2-running") {
		t.Errorf("running run (no meta.json, recent manifest.createdAt fallback) missing:\n%s", body)
	}
}

// TestAPI_JSONTwinsMatchMarkdownContent pins FM-52: "JSON twins carry
// exactly the markdown's content as fields."
func TestAPI_JSONTwinsMatchMarkdownContent(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "jt1", "claude-sonnet-5", []runFixture{
		{runID: "jt1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 2.5, taskResult: "passed", done: true, selfReview: "all good"},
	}, true, map[string]string{"jt1-scena": "passed"})
	seedObservation(t, store, fixtureObservation("jt1-scena"))

	mdBody := doGET(t, h, "/api/runs/jt1-scena.md").Body.String()
	var detail RunDetail
	if err := json.Unmarshal(doGET(t, h, "/api/runs/jt1-scena.json").Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Observation == nil {
		t.Fatal("json twin carries no observation")
	}
	if !strings.Contains(mdBody, detail.Observation.Headline) {
		t.Errorf("markdown missing JSON's headline %q:\n%s", detail.Observation.Headline, mdBody)
	}
	if !strings.Contains(mdBody, detail.Verdict) {
		t.Errorf("markdown missing JSON's verdict %q:\n%s", detail.Verdict, mdBody)
	}

	selfReviewMD := doGET(t, h, "/api/runs/jt1-scena/self-review.md").Body.String()
	var selfReviewJSON struct {
		SelfReview string `json:"selfReview"`
	}
	if err := json.Unmarshal(doGET(t, h, "/api/runs/jt1-scena/self-review.json").Body.Bytes(), &selfReviewJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if selfReviewMD != selfReviewJSON.SelfReview {
		t.Errorf("self-review twins differ: md=%q json=%q", selfReviewMD, selfReviewJSON.SelfReview)
	}
}

// TestAPI_ObserverStateAssessmentFailed pins §8.4's JSON observerState:
// "stays one of observed, observing, not observed, observer off, observer
// disabled ... plus assessment failed for a run whose current observation's
// status is error or unparsed", with observerStateText carrying the §8.8
// wording alongside it.
func TestAPI_ObserverStateAssessmentFailed(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "of1", "claude-sonnet-5", []runFixture{
		{runID: "of1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, false, nil)
	obs := fixtureObservation("of1-scena")
	obs.Status = observationStatusError
	obs.Error = "model call timed out"
	seedObservation(t, store, obs)

	rr := doGET(t, h, "/api/runs.json?batch=of1")
	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(out.Runs))
	}
	if out.Runs[0].ObserverState != observerStateAssessmentFailed {
		t.Errorf("observerState = %q, want %q", out.Runs[0].ObserverState, observerStateAssessmentFailed)
	}
	if !strings.Contains(out.Runs[0].ObserverStateText, "model call timed out") {
		t.Errorf("observerStateText = %q, want it to carry the failure reason", out.Runs[0].ObserverStateText)
	}

	var detail RunDetail
	if err := json.Unmarshal(doGET(t, h, "/api/runs/of1-scena.json").Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.ObserverState != observerStateAssessmentFailed {
		t.Errorf("run detail observerState = %q, want %q", detail.ObserverState, observerStateAssessmentFailed)
	}
}

// TestAPI_RunningRunWithoutDoneShowsRunning pins §7.5/§8.4: a run with no
// done.json shows verdict "running" and observerState "not observed".
func TestAPI_RunningRunWithoutDoneShowsRunning(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "rr1", "claude-sonnet-5", []runFixture{
		{runID: "rr1-scena", scenario: "scena", startedAt: now.Add(-time.Minute), done: false},
	}, false, nil)

	rr := doGET(t, h, "/api/runs.json?batch=rr1")
	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(out.Runs))
	}
	if out.Runs[0].Verdict != "running" {
		t.Errorf("verdict = %q, want running", out.Runs[0].Verdict)
	}
	if out.Runs[0].ObserverState != "not observed" {
		t.Errorf("observerState = %q, want %q", out.Runs[0].ObserverState, "not observed")
	}
}

// TestView_ObservedOutranksOffAndDisabled pins §8.4: observerState describes
// the run's data first — a run that has an observation reads "observed" even
// in a batch whose manifest predates the observer field (or says off) and
// even under the kill switch, because operator actions work regardless of
// both (§8.5 FM-53).
func TestView_ObservedOutranksOffAndDisabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		disabled       bool
		manifest       string
		hasObservation bool
		want           string
	}{
		{"old batch with an observation", false, "", true, observerStateObserved},
		{"manifest off with an observation", false, ObserverOff, true, observerStateObserved},
		{"kill switch with an observation", true, "claude-sonnet-5", true, observerStateObserved},
		{"old batch without an observation", false, "", false, observerStateOff},
		{"kill switch without an observation", true, "claude-sonnet-5", false, observerStateDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveObserverState(tc.disabled, tc.manifest, true, tc.hasObservation, false)
			if got != tc.want {
				t.Errorf("resolveObserverState = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAPI_SettledRunWithoutDoneUsesSummaryResult pins §8.1/§8.4: a run with
// no done.json is "running" only while its batch has not settled it; once
// batches/<batch>/summary.json carries the run (blocked: no bundle,
// interrupted, a creation error), that result is the run's verdict — a run
// the controller gave up on weeks ago must not read as still running.
func TestAPI_SettledRunWithoutDoneUsesSummaryResult(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sr1", "claude-sonnet-5", []runFixture{
		{runID: "sr1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), done: false},
	}, true, map[string]string{"sr1-scena": "blocked"})

	rr := doGET(t, h, "/api/runs.json?batch=sr1")
	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(out.Runs))
	}
	if out.Runs[0].Verdict != "blocked" {
		t.Errorf("verdict = %q, want blocked (the batch summary settled the run)", out.Runs[0].Verdict)
	}
}

// TestAPI_FilterTestFindings pins three API defects a functional filter
// test found (§8.4/§8.7): the JSON twin of runs.md says outcome "none" as
// the markdown does; digest.md refuses a parameter it does not take; a
// problem member names its scenario and a finding its cause class (the
// value the cause= filter takes) beside its label.
func TestAPI_FilterTestFindings(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedBatch(t, store, "ft1", "claude-sonnet-5", []runFixture{
		{runID: "ft1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.5, taskResult: "passed", done: true},
		{runID: "ft1-b", scenario: "b", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.5, taskResult: "failed", done: true},
	}, true, map[string]string{"ft1-a": "passed", "ft1-b": "failed"})
	seedObservation(t, store, fixtureObservation("ft1-b"))

	rr := doGET(t, h, "/api/runs.json?batch=ft1")
	if !strings.Contains(rr.Body.String(), `"outcome":"none"`) {
		t.Errorf("runs.json: an unassessed run's outcome must read \"none\" like runs.md:\n%s", rr.Body.String())
	}
	if rr := doGET(t, h, "/api/digest.md?batch=ft1&bogus=1"); rr.Code != http.StatusBadRequest {
		t.Errorf("digest.md?bogus=1: got %d, want 400", rr.Code)
	}
	body := doGET(t, h, "/api/problems.json?status=all").Body.String()
	if !strings.Contains(body, `"scenario":"b"`) {
		t.Errorf("problems.json members must carry their scenario:\n%s", body)
	}
	body = doGET(t, h, "/api/findings.json?since=30d").Body.String()
	if !strings.Contains(body, `"causeClass":"`) {
		t.Errorf("findings.json must carry causeClass beside the cause label:\n%s", body)
	}
}

// TestAPI_RunDetailJSON_VerdictReasonAndCostKnown pins FIX2-API item 1: a
// run's verdictReason and costKnown — missing from GET
// /api/runs/<runId>.json today — through the same facts the runs list
// already carries: verdictReason from the blocked-check-ids fallback
// (view_test.go's TestBuildRunRow_VerdictReasonBlockedFromChecks), costKnown
// false for a bundle whose meta.json never recorded usage.
func TestAPI_RunDetailJSON_VerdictReasonAndCostKnown(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "vc1", "off", []runFixture{
		{
			runID: "vc1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1,
			taskResult: "blocked", done: true,
			checks: [][5]string{{"z1", "blocked", "x", "y", "verify"}, {"a1", "blocked", "x", "y", "verify"}},
		},
	}, false, nil) // no summary: done.json landed before the batch settled
	// seedRun always writes a "usage" object (even a zero one); overwrite
	// meta.json without it to simulate a bundle that never recorded usage
	// (view.go: RunRow.CostKnown is true only when meta.json carries one).
	store.putJSON(t, "runs/vc1-a/results/"+testResultsTS+"/a/meta.json", map[string]any{
		"scenarioId": "a", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano), "duration": "5s",
		"evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": "blocked", "frozenAt": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)},
	})

	var detail RunDetail
	if err := json.Unmarshal(doGET(t, h, "/api/runs/vc1-a.json").Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.VerdictReason != "a1, z1" {
		t.Errorf("VerdictReason = %q, want %q", detail.VerdictReason, "a1, z1")
	}
	if detail.CostKnown {
		t.Error("CostKnown = true, want false (meta.json carried no usage)")
	}

	var out struct {
		Runs []RunsListItem `json:"runs"`
	}
	if err := json.Unmarshal(doGET(t, h, "/api/runs.json?batch=vc1").Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal runs list: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(out.Runs))
	}
	if out.Runs[0].CostKnown {
		t.Error("runs list CostKnown = true, want false")
	}
}

// TestAPI_RunDetailMD_Wording pins FIX2-API item 2's §8.8 wording for GET
// /api/runs/<runId>.md: "agent cost:" (with "— (not recorded)" when
// unknown, never a misleading "$0.00"), "ZCP build:", "evaluator build:",
// "assessment: <state text>" (never a raw "observerState: <enum>"), and
// "assessment: none" (never "Observer: (not recorded)") for a run that
// carries no observation at all.
func TestAPI_RunDetailMD_Wording(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "wd1", "off", []runFixture{
		{runID: "wd1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", taskResult: "passed", done: true},
	}, false, nil)
	// seedRun always writes a "usage" object (even a zero one); overwrite
	// meta.json without it so this run's cost is genuinely unrecorded.
	store.putJSON(t, "runs/wd1-a/results/"+testResultsTS+"/a/meta.json", map[string]any{
		"scenarioId": "a", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano), "duration": "5s",
		"evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": "passed", "frozenAt": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)},
	})

	body := doGET(t, h, "/api/runs/wd1-a.md").Body.String()
	for _, want := range []string{
		"agent cost: — (not recorded)",
		"ZCP build: build cand-sha",
		"evaluator build: build eval-sha",
		"assessment: not assessed — batch ran without observer",
		"assessment: none",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run detail md missing %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"\ncost: $", "\ncandidate: ", "\nevaluator: eval-sha", "observerState:", "Observer: (not recorded)", "$0.00"} {
		if strings.Contains(body, bad) {
			t.Errorf("run detail md still contains old wording %q:\n%s", bad, body)
		}
	}

	seedBatch(t, store, "wd2", "off", []runFixture{
		{runID: "wd2-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 2.5, taskResult: "passed", done: true},
	}, false, nil)
	body2 := doGET(t, h, "/api/runs/wd2-a.md").Body.String()
	if !strings.Contains(body2, "agent cost: $2.5000") {
		t.Errorf("run detail md missing known agent cost:\n%s", body2)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// TestAPI_LegendsInlineVerdictAndDropUnprintedTerms pins FIX2-API item 3:
// no legend says "see the table below" (no markdown endpoint prints that
// table — it's a /terms-only page fixture); runs.md's legend drops
// "Disputed"/"Agent cost" (its line never prints either) and defines
// "Outcome"/"Findings" (which it does); problems.md defines "Hit" and
// "Status" (both printed on every problem line). findings.md's own
// Surface/Anchor legend coverage is pinned separately, by
// TestAPI_FindingsMDRowsCarryEverySpecField, once its row grew to print
// them (a follow-up to this test's original round-1 premise that it
// didn't).
func TestAPI_LegendsInlineVerdictAndDropUnprintedTerms(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "lg1", "claude-sonnet-5", []runFixture{
		{runID: "lg1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 1.0, taskResult: "passed", done: true},
	}, true, map[string]string{"lg1-a": "passed"})
	seedObservation(t, store, fixtureObservation("lg1-a"))

	for _, path := range []string{"/api/runs.md", "/api/problems.md", "/api/batches.md", "/api/digest.md?batch=lg1", "/api/findings.md"} {
		body := doGET(t, h, path).Body.String()
		if strings.Contains(body, "table below") {
			t.Errorf("%s legend still says \"see the table below\":\n%s", path, firstLine(body))
		}
	}

	verdictLegend := firstLine(doGET(t, h, "/api/batches.md").Body.String())
	if !strings.Contains(verdictLegend, "every check held") || !strings.Contains(verdictLegend, "a check proved the run wrong") {
		t.Errorf("batches.md legend does not inline the verdict meanings:\n%s", verdictLegend)
	}

	runsLegend := firstLine(doGET(t, h, "/api/runs.md").Body.String())
	if strings.Contains(runsLegend, "Disputed = ") {
		t.Errorf("runs.md legend still defines \"Disputed\", which its line never prints:\n%s", runsLegend)
	}
	if !strings.Contains(runsLegend, "Outcome = ") || !strings.Contains(runsLegend, "Findings = ") {
		t.Errorf("runs.md legend must define Outcome and Findings, which its line prints:\n%s", runsLegend)
	}

	problemsLegend := firstLine(doGET(t, h, "/api/problems.md").Body.String())
	if !strings.Contains(problemsLegend, "Hit = ") || !strings.Contains(problemsLegend, "Status = ") {
		t.Errorf("problems.md legend must define Hit and Status, which its line prints:\n%s", problemsLegend)
	}
}

// TestAPI_DigestCountsFailedAssessmentAsUnassessed pins the verification
// round's item 1: a run whose current observation failed to parse
// (status unparsed, so Headline is "") is NOT "assessed" — the digest's
// unassessed count must include it (via NeedsAssessment, not a bare
// Observation == nil check) and say how many of those failed to parse; the
// runs-list line for that run must fall back to "(<observerState>)"
// instead of a blank headline.
func TestAPI_DigestCountsFailedAssessmentAsUnassessed(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ua1", "claude-sonnet-5", []runFixture{
		{runID: "ua1-unparsed", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "ua1-never", scenario: "b", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ua1-unparsed": "failed", "ua1-never": "passed"})
	obs := fixtureObservation("ua1-unparsed")
	obs.Status = observationStatusUnparsed
	obs.Headline = ""
	obs.Raw = "the model did not answer in the expected shape"
	seedObservation(t, store, obs)

	body := doGET(t, h, "/api/digest.md?batch=ua1").Body.String()
	if !strings.Contains(body, "Unassessed: 2 finished run(s) not yet assessed (1 assessment failed)") {
		t.Errorf("digest.md unassessed line wrong:\n%s", body)
	}

	rr := doGET(t, h, "/api/runs.md?batch=ua1")
	runsBody := rr.Body.String()
	for line := range strings.SplitSeq(runsBody, "\n") {
		if strings.HasPrefix(line, "- ua1-unparsed") {
			if !strings.Contains(line, "(assessment failed)") {
				t.Errorf("runs.md line for the unparsed run must fall back to its observer state, not a blank headline:\n%s", line)
			}
		}
	}
}

// TestAPI_FindingsMDRowsCarryEverySpecField pins the follow-up to round 1's
// legend trim: §8.4 lists "batch, scenario, build, run id, started,
// severity, cause, surface, anchor, title, what, steps, quotes found n/m,
// where to look, fix" for GET /api/findings.md, but the row printed only
// severity/cause/title/batch/runId/started/steps/quotes/what — scenario,
// build, surface, anchor, look-at and fix never reached the page even
// though FindingItem already carried them. Compact: one line plus
// indented "look at:"/"fix:" lines. The legend gets Surface/Anchor back
// now that the row prints them.
func TestAPI_FindingsMDRowsCarryEverySpecField(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fs1", "claude-sonnet-5", []runFixture{
		{runID: "fs1-scena", scenario: "scena", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, false, nil)
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2,
		RunID:         "fs1-scena",
		ObsID:         "20260911T120000000Z-claude-sonnet-5",
		Model:         "claude-sonnet-5",
		CreatedAt:     now,
		Status:        "ok",
		Outcome:       "problem",
		Headline:      "found something",
		Story:         &observer.Story{Task: "t", Expected: "e", Did: "d", Ending: "finished"},
		Goal:          observer.Goal{Reached: "yes", Why: "why"},
		Checks:        observer.Checks{Verdict: "passed", Agree: true},
		Findings: []observer.Finding{{
			Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy",
			Anchor: "source mount missing", Title: "Deploy preflight missing mount",
			What:     "the preflight check never ran",
			Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}},
			LookAt:   "internal/ops/deploy.go", Fix: "run the preflight before the upload step",
		}},
		SelfReview: observer.SelfReview{Accurate: "yes"},
	})

	body := doGET(t, h, "/api/findings.md").Body.String()
	for _, want := range []string{
		"scena",                                         // scenario
		"build cand-sha",                                // build label
		"surface tool:zerops_deploy/deploy",             // surface
		`anchor "source mount missing"`,                 // anchor
		"look at: internal/ops/deploy.go",               // where to look, own indented line
		"fix: run the preflight before the upload step", // fix, own indented line
	} {
		if !strings.Contains(body, want) {
			t.Errorf("findings.md missing %q:\n%s", want, body)
		}
	}

	legend := firstLine(body)
	if !strings.Contains(legend, "Surface = ") || !strings.Contains(legend, "Anchor = ") {
		t.Errorf("findings.md legend must define Surface and Anchor again, now that the row prints them:\n%s", legend)
	}
}
