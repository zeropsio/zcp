// Package console: RED/GREEN tests for the slice MODEL run facts
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 1,
// docs/spec-eval-farm.md §8.3/§8.5/§8.8): BuildInfo's label, the run-level
// derived facts (VerdictReason, Stalled, Outcome, Disputed, cause-class
// counts, ObserverStateText) and the NeedsAssessment predicate.
package console

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// --- BuildInfo.Label (§8.8: "ZCP build") ---------------------------------

func TestBuildInfo_Label(t *testing.T) {
	tests := []struct {
		name string
		b    BuildInfo
		want string
	}{
		{"revision known, clean", BuildInfo{Sha256: "deadbeef" + "00000000000000000000000000000000000000000000000000000000", Revision: "abcdef0123456789", Modified: false}, "abcdef012345"},
		{"revision known, modified", BuildInfo{Sha256: "cafebabe", Revision: "abcdef0123456789", Modified: true}, "abcdef012345 + modified"},
		{"no revision: sha fallback", BuildInfo{Sha256: "0123456789abcdef0123456789abcdef"}, "build 0123456789ab"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.b.Label(); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- isStalled (§8.1: "a manifest without runBudgetSec never shows
// stalled" — the spec's own text, which the brief's "default 45 min when
// absent" contradicts; the spec wins, per _common.md) --------------------

func TestIsStalled(t *testing.T) {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		now          time.Time
		runBudgetSec int
		want         bool
	}{
		{"no budget: never stalled even far past", created.Add(999 * time.Hour), 0, false},
		{"within budget", created.Add(10 * time.Minute), 1800, false},
		{"past budget but within 30m grace", created.Add(35 * time.Minute), 1800, false},
		{"past budget and past 30m grace", created.Add(61 * time.Minute), 1800, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStalled(tc.now, created, tc.runBudgetSec); got != tc.want {
				t.Errorf("isStalled = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- observerStateText (§8.8 assessment-state vocabulary) ---------------

func TestObserverStateText(t *testing.T) {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	okObs := &observer.Observation{Status: "ok"}
	errObs := &observer.Observation{Status: "error", Error: "boom"}
	unparsedObs := &observer.Observation{Status: "unparsed"}

	tests := []struct {
		name          string
		now           time.Time
		disabled      bool
		manifestOb    string
		doneExists    bool
		obs           *observer.Observation
		queued        bool
		settled       bool
		settledReason string
		want          string
	}{
		{"queued outranks everything", created, false, "claude-sonnet-5", true, errObs, true, false, "", "assessing…"},
		{"not finished, still running", created, false, "claude-sonnet-5", false, nil, false, false, "", "not assessed — run not finished"},
		{"assessed", created.Add(time.Hour), false, "claude-sonnet-5", true, okObs, false, false, "", "assessed"},
		{"assessment failed with error message", created.Add(time.Hour), false, "claude-sonnet-5", true, errObs, false, false, "", "assessment failed — boom"},
		{"assessment failed unparsed", created.Add(time.Hour), false, "claude-sonnet-5", true, unparsedObs, false, false, "", "assessment failed — the model's answer did not parse"},
		{"batch ran without observer", created.Add(time.Hour), false, "", true, nil, false, false, "", "not assessed — batch ran without observer"},
		{"batch observer off", created.Add(time.Hour), false, ObserverOff, true, nil, false, false, "", "not assessed — batch ran without observer"},
		{"console disabled", created.Add(time.Hour), true, "claude-sonnet-5", true, nil, false, false, "", "not assessed — automatic assessment is off on this console"},
		{"older than 14 days", created.Add(15 * 24 * time.Hour), false, "claude-sonnet-5", true, nil, false, false, "", "not assessed — older than 14 days (assess by hand)"},
		{"fresh, named observer, will be picked up soon", created.Add(time.Hour), false, "claude-sonnet-5", true, nil, false, false, "", "not assessed"},
		// Item 3 (FIX2): a settled run (its batch summary already resolved
		// it blocked/not-run/etc.) that never got a bundle at all needs no
		// "waiting for the worker" wording — there is nothing to assess,
		// ever, for this run.
		{"settled, no bundle, no reason", created, false, "claude-sonnet-5", false, nil, false, true, "", "nothing to assess — the run left no record"},
		{"settled, no bundle, with the summary's own reason", created, false, "claude-sonnet-5", false, nil, false, true, "aborted: setup failed", "nothing to assess — the run left no record: aborted: setup failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := observerStateText(tc.now, created, tc.disabled, tc.manifestOb, tc.doneExists, tc.obs, tc.queued, tc.settled, tc.settledReason)
			if got != tc.want {
				t.Errorf("observerStateText = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveObserverState_NoRecordDistinctFromNotObserved pins item 8
// (FIX3): a settled run with no bundle (its batch already resolved it
// blocked/not-run/etc. without done.json ever landing) reads "no record",
// distinct from "not observed" — which (before this) also covered a run
// still genuinely running and waiting its turn. The API and the
// farm-triage skill read RunRow.ObserverState directly, so this is a
// state distinction, not merely a text one (§8.3's own text fix is
// TestObserverStateText's "settled, no bundle" cases, above).
func TestResolveObserverState_NoRecordDistinctFromNotObserved(t *testing.T) {
	tests := []struct {
		name       string
		doneExists bool
		settled    bool
		want       string
	}{
		{"still running: not observed", false, false, observerStateNotObserved},
		{"settled, no bundle: no record", false, true, observerStateNoRecord},
		{"done: settled is irrelevant", true, true, observerStateNotObserved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveObserverState(false, "claude-sonnet-5", tc.doneExists, false, false, tc.settled)
			if got != tc.want {
				t.Errorf("resolveObserverState = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- NeedsAssessment (§8.5) ----------------------------------------------

func TestNeedsAssessment(t *testing.T) {
	tests := []struct {
		name     string
		row      RunRow
		inFlight bool
		want     bool
	}{
		{"not finished", RunRow{DoneExists: false}, false, false},
		{"in flight", RunRow{DoneExists: true}, true, false},
		{"no observation", RunRow{DoneExists: true}, false, true},
		{"current observation ok", RunRow{DoneExists: true, Observation: &observer.Observation{Status: "ok"}}, false, false},
		{"current observation error", RunRow{DoneExists: true, Observation: &observer.Observation{Status: "error"}}, false, true},
		{"current observation unparsed", RunRow{DoneExists: true, Observation: &observer.Observation{Status: "unparsed"}}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsAssessment(tc.row, tc.inFlight); got != tc.want {
				t.Errorf("NeedsAssessment = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- serviceHostnames (§8.6: platform-snapshot.json services + every
// verification.json row's scope) ------------------------------------------

func TestServiceHostnames(t *testing.T) {
	snap := &eval.PlatformSnapshot{Services: []eval.PlatformSnapshotService{{Hostname: "api"}, {Hostname: "web"}}}
	verification := eval.VerificationDocument{Checks: []eval.RequiredCheck{
		{Scope: "web"}, // duplicate of a snapshot hostname
		{Scope: "db1"}, // scope-only hostname
		{Scope: ""},    // empty scope: never added
	}}
	got := serviceHostnames(snap, verification)
	want := []string{"api", "db1", "web"}
	if len(got) != len(want) {
		t.Fatalf("serviceHostnames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("serviceHostnames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- End-to-end: buildRunRow resolves every new fact on a realistic
// bundle (via the fakeStore fixtures in console_test.go) ------------------

func TestBuildRunRow_ResolvesNewFacts(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)()

	seedBatch(t, store, "mf1", "claude-sonnet-5", []runFixture{
		{
			runID: "mf1-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1,
			taskResult: "failed", done: true,
			checks: [][5]string{{"c1", "failed", "x", "y", "verify"}},
		},
	}, true, map[string]string{"mf1-a": "failed"})

	obs := observer.Observation{
		RunID: "mf1-a", ObsID: "20260911T110000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		FormatVersion: observer.ObservationFormat2, Status: "ok", Outcome: observer.OutcomeProblem,
		Checks: observer.Checks{Verdict: "failed", Agree: false, Judged: []observer.JudgedCheck{{ID: "c1", Correct: false}}},
		Findings: []observer.Finding{
			{Severity: observer.SeverityHigh, Owner: "zcp-tool", Title: "t1", Evidence: []observer.Evidence{{Step: 1}}},
			{Severity: observer.SeverityMedium, Owner: "agent", Title: "t2", Evidence: []observer.Evidence{{Step: 1}}},
		},
	}
	seedObservation(t, store, obs)

	row, err := loadRunRow(context.Background(), store, false, "mf1-a", nil, nil, nil)
	if err != nil {
		t.Fatalf("loadRunRow: %v", err)
	}

	if !row.CostKnown {
		t.Error("CostKnown = false, want true (meta.json carried usage)")
	}
	if row.Outcome != observer.OutcomeProblem {
		t.Errorf("Outcome = %q, want %q", row.Outcome, observer.OutcomeProblem)
	}
	if !row.Disputed {
		t.Error("Disputed = false, want true (checks.agree is false)")
	}
	if row.Build.Sha256 != "cand-sha" {
		t.Errorf("Build.Sha256 = %q, want cand-sha", row.Build.Sha256)
	}
	if row.StepCount == 0 {
		t.Error("StepCount = 0, want > 0 (fixtureTranscript has steps)")
	}
	foundZCP, foundAgent := false, false
	for _, c := range row.CauseCounts {
		if c.Class == CauseClassZCP && c.High == 1 {
			foundZCP = true
		}
		if c.Class == CauseClassAgent && c.Medium == 1 {
			foundAgent = true
		}
	}
	if !foundZCP {
		t.Errorf("CauseCounts missing ZCP high=1: %+v", row.CauseCounts)
	}
	if !foundAgent {
		t.Errorf("CauseCounts missing Agent medium=1: %+v", row.CauseCounts)
	}
	if row.VerdictReason != "" {
		t.Errorf("VerdictReason = %q, want empty (verdict is failed, not blocked/not-started — §8.8 gives failed no \"reason\")", row.VerdictReason)
	}
	if row.ObserverStateText != "assessed" {
		t.Errorf("ObserverStateText = %q, want %q", row.ObserverStateText, "assessed")
	}
}

// TestBuildRunRow_VerdictReasonBlockedFromChecks pins VerdictReason's
// blocked-check-ids fallback (§1.4 FM-9's own detail computation, mirrored
// here for a bundle whose done.json lands before its batch's summary.json
// exists to read a detail from).
func TestBuildRunRow_VerdictReasonBlockedFromChecks(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)()
	seedBatch(t, store, "mf3", "claude-sonnet-5", []runFixture{
		{
			runID: "mf3-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1,
			taskResult: "blocked", done: true,
			checks: [][5]string{{"z1", "blocked", "x", "y", "verify"}, {"a1", "blocked", "x", "y", "verify"}},
		},
	}, false, nil) // no summary yet: this run's done.json landed before its batch settled

	row, err := loadRunRow(context.Background(), store, false, "mf3-a", nil, nil, nil)
	if err != nil {
		t.Fatalf("loadRunRow: %v", err)
	}
	if row.Verdict != "blocked" {
		t.Fatalf("Verdict = %q, want blocked", row.Verdict)
	}
	if row.VerdictReason != "a1, z1" {
		t.Errorf("VerdictReason = %q, want %q (sorted blocked check ids)", row.VerdictReason, "a1, z1")
	}
}

// TestBuildRunRow_NotDoneRunHasNoNewFacts pins that a not-yet-done run
// carries only the not-done-appropriate facts: no Build/Outcome derived
// from a bundle that doesn't exist yet, but ObserverStateText still says
// why (§8.8).
func TestBuildRunRow_NotDoneRunNotFinishedText(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)()
	seedBatch(t, store, "mf2", "claude-sonnet-5", []runFixture{
		{runID: "mf2-a", scenario: "a", startedAt: now, done: false},
	}, false, nil)

	row, err := loadRunRow(context.Background(), store, false, "mf2-a", nil, nil, nil)
	if err != nil {
		t.Fatalf("loadRunRow: %v", err)
	}
	if row.ObserverStateText != "not assessed — run not finished" {
		t.Errorf("ObserverStateText = %q, want %q", row.ObserverStateText, "not assessed — run not finished")
	}
	if row.Outcome != "" {
		t.Errorf("Outcome = %q, want empty for a not-done run", row.Outcome)
	}
}

// --- TestLists_BatchRuns / TestLists_APIRuns / TestLists_RunSteps
// (item 5, §8.7) -----------------------------------------------------------

func TestLists_BatchRuns(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	runs := []RunRow{
		{RunID: "r-failed", Scenario: "a", Verdict: farm.VerdictFailed, DoneExists: true, DurationSec: 5, CostUsd: 0.1, CostKnown: true},
		{RunID: "r-passed", Scenario: "b", Verdict: farm.VerdictPassed, DoneExists: true, DurationSec: 10, CostUsd: 0.2, CostKnown: true},
		{RunID: "r-running", Scenario: "c", Verdict: verdictRunning, DoneExists: false},
	}
	eng := batchRunsEngine()

	t.Run("verdict filter, not-started/stalled vocabulary", func(t *testing.T) {
		q, err := Parse(batchRunsListSpec(), url.Values{"verdict": {"failed"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if len(got) != 1 || got[0].RunID != "r-failed" {
			t.Fatalf("got %+v, want only r-failed", got)
		}
	})

	t.Run("sort=problem desc puts failed first", func(t *testing.T) {
		q, err := Parse(batchRunsListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if got[0].RunID != "r-failed" {
			t.Errorf("got[0] = %s, want r-failed (most concerning first)", got[0].RunID)
		}
	})

	t.Run("sort=duration unknown-last for a run with no done.json", func(t *testing.T) {
		q, err := Parse(batchRunsListSpec(), url.Values{"sort": {"duration"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if got[len(got)-1].RunID != "r-running" {
			t.Errorf("last = %s, want r-running (no done.json => unknown duration)", got[len(got)-1].RunID)
		}
	})

	t.Run("unknown verdict value refused", func(t *testing.T) {
		if _, err := Parse(batchRunsListSpec(), url.Values{"verdict": {"bogus"}}); err == nil {
			t.Error("want an error for an unknown verdict value")
		}
	})

	t.Run("since not accepted on batch runs", func(t *testing.T) {
		if _, err := Parse(batchRunsListSpec(), url.Values{"since": {"7d"}}); err == nil {
			t.Error("want an error: batch runs list does not take since (it's already scoped to one batch)")
		}
	})
}

func TestLists_APIRuns(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	runs := []RunRow{
		{RunID: "r1", Batch: "b1", StartedAt: now.Add(-time.Hour)},
		{RunID: "r2", Batch: "b2", StartedAt: now.Add(-2 * time.Hour)},
	}
	eng := apiRunsEngine()

	t.Run("batch open filter", func(t *testing.T) {
		q, err := Parse(apiRunsListSpec(), url.Values{"batch": {"b1"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if len(got) != 1 || got[0].RunID != "r1" {
			t.Fatalf("got %+v, want only r1", got)
		}
	})

	t.Run("no default since: both runs visible with no since param", func(t *testing.T) {
		q, err := Parse(apiRunsListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if len(got) != 2 {
			t.Fatalf("got %d, want 2 (no default since window)", len(got))
		}
	})

	t.Run("sort=newest desc, tie-break run id", func(t *testing.T) {
		q, err := Parse(apiRunsListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(runs, q, now)
		if got[0].RunID != "r1" {
			t.Errorf("got[0] = %s, want r1 (newest)", got[0].RunID)
		}
	})
}

func TestLists_RunSteps(t *testing.T) {
	steps := []observer.Step{
		{N: 1, Kind: observer.StepUser},
		{N: 2, Kind: observer.StepTool, ToolIsError: true},
		{N: 3, Kind: observer.StepTool, ToolIsError: false},
	}
	cited := map[int]bool{1: true}

	t.Run("all keeps every step", func(t *testing.T) {
		got := FilterSteps(steps, "all", cited)
		if len(got) != 3 {
			t.Errorf("got %d, want 3", len(got))
		}
	})
	t.Run("cited keeps only cited steps", func(t *testing.T) {
		got := FilterSteps(steps, "cited", cited)
		if len(got) != 1 || got[0].N != 1 {
			t.Errorf("got %+v, want only step 1", got)
		}
	})
	t.Run("errors keeps only error tool steps", func(t *testing.T) {
		got := FilterSteps(steps, "errors", cited)
		if len(got) != 1 || got[0].N != 2 {
			t.Errorf("got %+v, want only step 2", got)
		}
	})
	t.Run("unknown steps value refused", func(t *testing.T) {
		if _, err := Parse(runStepsListSpec(), url.Values{"steps": {"bogus"}}); err == nil {
			t.Error("want an error for an unknown steps value")
		}
	})
	t.Run("sort not accepted: this list has no sort keys", func(t *testing.T) {
		if _, err := Parse(runStepsListSpec(), url.Values{"sort": {"anything"}}); err == nil {
			t.Error("want an error: run steps has no sort keys (record order)")
		}
	})
	t.Run("obs accepted (AllowObs)", func(t *testing.T) {
		q, err := Parse(runStepsListSpec(), url.Values{"obs": {"x"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if q.Obs != "x" {
			t.Errorf("Obs = %q, want x", q.Obs)
		}
	})
}
