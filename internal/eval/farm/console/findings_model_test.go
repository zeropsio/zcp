// Package console: RED/GREEN tests for the findings model
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 3,
// docs/spec-eval-farm.md §8.3 item 3, §8.8): cause vocabulary and
// BuildFindingRows.
package console

import (
	"net/url"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

func TestCauseLabelAndClass(t *testing.T) {
	tests := []struct {
		owner     string
		wantLabel string
		wantClass string
	}{
		{"zcp-guidance", "ZCP guidance", CauseClassZCP},
		{"zcp-tool", "ZCP tool", CauseClassZCP},
		{"platform", "Zerops platform", CauseClassPlatform},
		{"agent", "Agent mistake", CauseClassAgent},
		{"scenario", "Test scenario", CauseClassTest},
		{"evaluator", "Test check", CauseClassTest},
	}
	for _, tc := range tests {
		if got := CauseLabel(tc.owner); got != tc.wantLabel {
			t.Errorf("CauseLabel(%q) = %q, want %q", tc.owner, got, tc.wantLabel)
		}
		if got := CauseClass(tc.owner); got != tc.wantClass {
			t.Errorf("CauseClass(%q) = %q, want %q", tc.owner, got, tc.wantClass)
		}
	}
}

// TestBuildFindingRows_SkipsNonOkObservations pins §8.6's "over the
// current observations (status ok)": a failed/unparsed current
// observation contributes no FindingRow even if it happens to carry
// findings data.
func TestBuildFindingRows_SkipsNonOkObservations(t *testing.T) {
	rows := []RunRow{
		{RunID: "r1", Observation: &observer.Observation{
			Status: "ok", FormatVersion: observer.ObservationFormat2,
			Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Title: "t1"}},
		}},
		{RunID: "r2", Observation: &observer.Observation{
			Status: "error", FormatVersion: observer.ObservationFormat2,
			Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Title: "should not appear"}},
		}},
		{RunID: "r3", Observation: nil},
	}
	got := BuildFindingRows(rows)
	if len(got) != 1 {
		t.Fatalf("BuildFindingRows returned %d rows, want 1: %+v", len(got), got)
	}
	if got[0].RunID != "r1" || got[0].Title != "t1" {
		t.Errorf("got[0] = %+v, want RunID r1, Title t1", got[0])
	}
	if got[0].CauseLabel != "ZCP tool" || got[0].CauseClass != CauseClassZCP {
		t.Errorf("got[0].CauseLabel/Class = %q/%q, want ZCP tool/%q", got[0].CauseLabel, got[0].CauseClass, CauseClassZCP)
	}
	if got[0].FormatVersion != 2 {
		t.Errorf("got[0].FormatVersion = %d, want 2", got[0].FormatVersion)
	}
}

// TestBuildFindingRows_IndexAndDedupeEvidence pins item 3's finding index
// (for #f<n>) and evidence step de-duplication.
func TestBuildFindingRows_IndexAndDedupeEvidence(t *testing.T) {
	rows := []RunRow{
		{RunID: "r1", Batch: "b1", Scenario: "s1", Observation: &observer.Observation{
			Status: "ok", FormatVersion: observer.ObservationFormat1,
			Findings: []observer.Finding{
				{Severity: "high", Owner: "agent", Title: "f0", Evidence: []observer.Evidence{{Step: 3}, {Step: 3}, {Step: 5}}},
				{Severity: "low", Owner: "scenario", Title: "f1"},
			},
		}},
	}
	got := BuildFindingRows(rows)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0].Index != 0 || got[1].Index != 1 {
		t.Errorf("indices = %d, %d, want 0, 1", got[0].Index, got[1].Index)
	}
	if len(got[0].Evidence) != 2 {
		t.Errorf("Evidence = %+v, want 2 entries (step 3 deduped)", got[0].Evidence)
	}
	if got[0].FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", got[0].FormatVersion)
	}
}

// --- TestLists_Findings (item 5, §8.7) -------------------------------------

func TestLists_Findings(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	findings := []FindingRow{
		{RunID: "r1", Severity: observer.SeverityHigh, CauseClass: CauseClassZCP, Surface: "tool:zerops_deploy", StartedAt: now.Add(-time.Hour), Index: 0},
		{RunID: "r2", Severity: observer.SeverityLow, CauseClass: CauseClassAgent, Surface: "agent", StartedAt: now.Add(-2 * time.Hour), Index: 0},
		{RunID: "r3", Severity: observer.SeverityMedium, CauseClass: CauseClassTest, Surface: "scenario", StartedAt: now.Add(-96 * time.Hour), Index: 0}, // outside 7d? no, 96h < 7d — keep inside window
	}
	eng := findingEngine()

	t.Run("since default 7d", func(t *testing.T) {
		q, err := Parse(findingListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if q.Since != 7*24*time.Hour {
			t.Errorf("Since = %v, want 7d", q.Since)
		}
		got, _ := eng.Apply(findings, q, now)
		if len(got) != 3 {
			t.Fatalf("got %d, want 3 (all within 7d)", len(got))
		}
	})

	t.Run("since narrows out an old finding", func(t *testing.T) {
		q, err := Parse(findingListSpec(), url.Values{"since": {"3h"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(findings, q, now)
		if len(got) != 2 {
			t.Fatalf("got %d, want 2", len(got))
		}
	})

	t.Run("surface prefix filter", func(t *testing.T) {
		q, err := Parse(findingListSpec(), url.Values{"surface": {"tool:*"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(findings, q, now)
		if len(got) != 1 || got[0].RunID != "r1" {
			t.Fatalf("got %+v, want only r1", got)
		}
	})

	t.Run("sort=severity default desc", func(t *testing.T) {
		q, err := Parse(findingListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(findings, q, now)
		if got[0].Severity != observer.SeverityHigh {
			t.Errorf("got[0].Severity = %q, want high", got[0].Severity)
		}
	})

	t.Run("sort=cause is ZCP-first", func(t *testing.T) {
		q, err := Parse(findingListSpec(), url.Values{"sort": {"cause"}, "dir": {"asc"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(findings, q, now)
		if got[0].CauseClass != CauseClassZCP {
			t.Errorf("got[0].CauseClass = %q, want zcp (ZCP-first)", got[0].CauseClass)
		}
	})

	t.Run("unknown parameter refused", func(t *testing.T) {
		if _, err := Parse(findingListSpec(), url.Values{"bogus": {"x"}}); err == nil {
			t.Error("want an error for an unknown parameter")
		}
	})
}
