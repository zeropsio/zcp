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
