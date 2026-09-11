// Package console: tests for the §8.7 query engine
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 5) —
// mechanics proven against a small synthetic item type first (closed OR/
// AND, severity minimum, open exact/prefix, unknown parameter/value
// refused, notice/n/obs passthrough, counts, sort default direction +
// tie-break + unknown-last), then TestLists_* against each real list's
// wiring (batches.go, problems.go, findings_model.go, view.go).
package console

import (
	"net/url"
	"testing"
	"time"
)

// --- synthetic item + engine for mechanics tests --------------------------

type widget struct {
	name     string
	color    string // closed, multi
	severity string // closed, min
	tag      string // open
	weight   int    // sort key "weight", unknown when negative
	at       time.Time
}

func widgetSpec() ListSpec {
	return ListSpec{
		Closed: []ClosedFilter{
			{Name: "color", Allowed: []string{"red", "green", "blue"}},
			{Name: "severity", Allowed: []string{"high", "medium", "low"}, Min: true},
		},
		Open:        []string{"tag"},
		Sorts:       []SortKey{{Name: "weight", DefaultDir: "desc"}, {Name: "name", DefaultDir: "asc"}},
		DefaultSort: "weight",
		Defaults:    map[string]string{"color": "red,green"},
		HasSince:    true, DefaultSince: "24h",
	}
}

func widgetEngine() Engine[widget] {
	return Engine[widget]{
		Spec: widgetSpec(),
		Match: func(w widget, name, value string) bool {
			switch name {
			case "color":
				return w.color == value
			case "tag":
				return OpenMatch(w.tag, value)
			default:
				return false
			}
		},
		Severity: func(w widget) string { return w.severity },
		Time:     func(w widget) time.Time { return w.at },
		Sorts: map[string]SortSpec[widget]{
			"weight": {
				Primary:  func(a, b widget) int { return cmpInt(a.weight, b.weight) },
				Tiebreak: func(a, b widget) int { return cmpString(a.name, b.name) },
				Unknown:  func(w widget) bool { return w.weight < 0 },
			},
			"name": {
				Primary: func(a, b widget) int { return cmpString(a.name, b.name) },
			},
		},
	}
}

func TestParse_UnknownParameterRefused(t *testing.T) {
	_, err := Parse(widgetSpec(), url.Values{"bogus": {"x"}})
	qerr, ok := err.(*QueryError)
	if !ok {
		t.Fatalf("err = %v (%T), want *QueryError", err, err)
	}
	if qerr.Param != "bogus" {
		t.Errorf("Param = %q, want bogus", qerr.Param)
	}
	if len(qerr.Allowed) == 0 {
		t.Error("Allowed is empty, want the list's parameter names")
	}
}

func TestParse_UnknownClosedValueRefused(t *testing.T) {
	_, err := Parse(widgetSpec(), url.Values{"color": {"purple"}})
	qerr, ok := err.(*QueryError)
	if !ok {
		t.Fatalf("err = %v, want *QueryError", err)
	}
	if qerr.Param != "color" {
		t.Errorf("Param = %q, want color", qerr.Param)
	}
	wantAllowed := []string{"red", "green", "blue"}
	if len(qerr.Allowed) != len(wantAllowed) {
		t.Errorf("Allowed = %v, want %v", qerr.Allowed, wantAllowed)
	}
}

func TestParse_OpenValueNeverRefused(t *testing.T) {
	q, err := Parse(widgetSpec(), url.Values{"tag": {"anything-goes"}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if q.Open["tag"] != "anything-goes" {
		t.Errorf("Open[tag] = %q, want anything-goes", q.Open["tag"])
	}
}

func TestParse_NoticeAndNPassThroughUnvalidated(t *testing.T) {
	q, err := Parse(widgetSpec(), url.Values{"notice": {"queued"}, "n": {"3"}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if q.Notice != "queued" || q.N != "3" {
		t.Errorf("Notice/N = %q/%q, want queued/3", q.Notice, q.N)
	}
}

func TestParse_ObsRefusedUnlessAllowed(t *testing.T) {
	if _, err := Parse(widgetSpec(), url.Values{"obs": {"x"}}); err == nil {
		t.Error("obs accepted on a list that does not declare AllowObs")
	}
	spec := widgetSpec()
	spec.AllowObs = true
	q, err := Parse(spec, url.Values{"obs": {"x"}})
	if err != nil {
		t.Fatalf("Parse with AllowObs: %v", err)
	}
	if q.Obs != "x" {
		t.Errorf("Obs = %q, want x", q.Obs)
	}
}

func TestParse_DefaultsApplied(t *testing.T) {
	q, err := Parse(widgetSpec(), url.Values{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(q.Closed["color"]) != 2 || q.Closed["color"][0] != "red" || q.Closed["color"][1] != "green" {
		t.Errorf("default color = %v, want [red green]", q.Closed["color"])
	}
	if q.Sort != "weight" || q.Dir != "desc" {
		t.Errorf("default sort/dir = %s/%s, want weight/desc", q.Sort, q.Dir)
	}
	if q.Since != 24*time.Hour {
		t.Errorf("default since = %v, want 24h", q.Since)
	}
}

func TestEngine_ClosedFilterORWithinANDAcross(t *testing.T) {
	items := []widget{
		{name: "a", color: "red", tag: "x"},
		{name: "b", color: "green", tag: "x"},
		{name: "c", color: "blue", tag: "x"},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{"color": {"red", "green"}}, Open: map[string]string{}}
	got, _ := eng.Apply(items, q, time.Now())
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (red OR green)", len(got))
	}
}

func TestEngine_SeverityIsAMinimum(t *testing.T) {
	items := []widget{
		{name: "a", severity: "low"},
		{name: "b", severity: "medium"},
		{name: "c", severity: "high"},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{}, SeverityMin: "medium"}
	got, _ := eng.Apply(items, q, time.Now())
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (medium and high)", len(got))
	}
}

func TestEngine_OpenPrefixMatch(t *testing.T) {
	items := []widget{
		{name: "a", tag: "tool:zerops_deploy"},
		{name: "b", tag: "tool:zerops_import"},
		{name: "c", tag: "agent"},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{"tag": "tool:*"}}
	got, _ := eng.Apply(items, q, time.Now())
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (tool:*)", len(got))
	}
}

func TestEngine_OpenValueMatchingNothingIsEmptyNotError(t *testing.T) {
	items := []widget{{name: "a", tag: "x"}}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{"tag": "nope"}}
	got, _ := eng.Apply(items, q, time.Now())
	if len(got) != 0 {
		t.Errorf("got %d items, want 0", len(got))
	}
}

func TestEngine_CountsUnderOtherActiveFilters(t *testing.T) {
	items := []widget{
		{name: "a", color: "red", tag: "keep"},
		{name: "b", color: "green", tag: "keep"},
		{name: "c", color: "blue", tag: "drop"},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{"tag": "keep"}}
	_, counts := eng.Apply(items, q, time.Now())
	want := map[string]int{"red": 1, "green": 1, "blue": 0}
	for color, n := range want {
		if counts["color"][color] != n {
			t.Errorf("counts[color][%s] = %d, want %d", color, counts["color"][color], n)
		}
	}
}

func TestEngine_SortDefaultDirectionAndTiebreak(t *testing.T) {
	items := []widget{
		{name: "b", weight: 5},
		{name: "a", weight: 5},
		{name: "z", weight: 10},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{}, Sort: "weight", Dir: "desc"}
	got, _ := eng.Apply(items, q, time.Now())
	wantOrder := []string{"z", "a", "b"} // weight desc; tie broken by name asc
	for i, w := range wantOrder {
		if got[i].name != w {
			t.Errorf("got[%d].name = %q, want %q (order %v)", i, got[i].name, w, namesOf(got))
		}
	}
}

func TestEngine_SortAscendingFlipsOnlyPrimary(t *testing.T) {
	items := []widget{
		{name: "b", weight: 5},
		{name: "a", weight: 5},
		{name: "z", weight: 10},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{}, Sort: "weight", Dir: "asc"}
	got, _ := eng.Apply(items, q, time.Now())
	wantOrder := []string{"a", "b", "z"} // weight asc; tie still broken by name asc
	for i, w := range wantOrder {
		if got[i].name != w {
			t.Errorf("got[%d].name = %q, want %q (order %v)", i, got[i].name, w, namesOf(got))
		}
	}
}

func TestEngine_UnknownSortsLastBothDirections(t *testing.T) {
	items := []widget{
		{name: "known-hi", weight: 10},
		{name: "unknown", weight: -1},
		{name: "known-lo", weight: 1},
	}
	eng := widgetEngine()
	for _, dir := range []string{"asc", "desc"} {
		q := Query{Closed: map[string][]string{}, Open: map[string]string{}, Sort: "weight", Dir: dir}
		got, _ := eng.Apply(items, q, time.Now())
		if got[len(got)-1].name != "unknown" {
			t.Errorf("dir=%s: unknown item not last: %v", dir, namesOf(got))
		}
	}
}

func TestEngine_SinceWindowMembership(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	items := []widget{
		{name: "in", at: now.Add(-time.Hour)},
		{name: "out", at: now.Add(-48 * time.Hour)},
	}
	eng := widgetEngine()
	q := Query{Closed: map[string][]string{}, Open: map[string]string{}, HasSince: true, Since: 24 * time.Hour}
	got, _ := eng.Apply(items, q, now)
	if len(got) != 1 || got[0].name != "in" {
		t.Errorf("got %v, want only \"in\"", namesOf(got))
	}
}

func namesOf(items []widget) []string {
	out := make([]string, len(items))
	for i, w := range items {
		out[i] = w.name
	}
	return out
}

// TestEngine_SeverityMinimumHasCounts pins §8.7's filter-bar counts for the
// one minimum filter: each severity option counts the items at that
// severity or above under the other active filters, so the bar can link it.
// (Live: /findings showed "High 0 · Medium 0 · Low 0" as dead options while
// high findings were listed.)
func TestEngine_SeverityMinimumHasCounts(t *testing.T) {
	items := []widget{
		{name: "a", severity: "low", color: "red"},
		{name: "b", severity: "medium", color: "red"},
		{name: "c", severity: "high", color: "red"},
		{name: "d", severity: "high", color: "blue"},
	}
	q := Query{Closed: map[string][]string{"color": {"red"}}, Open: map[string]string{}, SeverityMin: "high"}
	_, counts := widgetEngine().Apply(items, q, time.Now())
	want := OptionCounts{"high": 1, "medium": 2, "low": 3}
	for v, n := range want {
		if counts["severity"][v] != n {
			t.Errorf("counts[severity][%s] = %d, want %d (at or above, under color=red)", v, counts["severity"][v], n)
		}
	}
}
