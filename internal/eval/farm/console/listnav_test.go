package console

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testLabeler() listLabeler {
	return listLabeler{
		Param: func(p string) string { return p },
		Value: func(_, v string) string { return v },
		Title: func(_, _ string) string { return "" },
	}
}

// TestListURL_KeepsOtherParamsDropsNotice pins §8.7: every link keeps the
// other parameters; notice/n are never carried into links.
func TestListURL_KeepsOtherParamsDropsNotice(t *testing.T) {
	t.Parallel()
	values := url.Values{"cause": {"zcp"}, "sort": {"severity"}, "notice": {"queued"}, "n": {"3"}}
	got := listURL("/findings", values, map[string]string{"severity": "high"})
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("cause") != "zcp" || q.Get("sort") != "severity" || q.Get("severity") != "high" || q.Has("notice") || q.Has("n") {
		t.Errorf("listURL = %q", got)
	}
	if got := listURL("/findings", url.Values{"cause": {"zcp"}}, map[string]string{"cause": ""}); got != "/findings" {
		t.Errorf("removing the only param = %q, want /findings", got)
	}
}

// TestBuildListNav_FilterBarAndSorts pins the shared filter bar: an OR-set
// option toggles within the set, a minimum replaces, a count-0 option is not
// a link, an explicit filter shows a removable chip, and a sort header flips
// its direction once active.
func TestBuildListNav_FilterBarAndSorts(t *testing.T) {
	t.Parallel()
	spec := ListSpec{
		Closed: []ClosedFilter{
			{Name: "cause", Allowed: []string{"zcp", "test", "agent", "platform"}},
			{Name: "severity", Allowed: []string{"high", "medium", "low"}, Min: true},
		},
		Open:        []string{"scenario"},
		Sorts:       []SortKey{{Name: "severity", DefaultDir: "desc"}, {Name: "newest", DefaultDir: "desc"}},
		DefaultSort: "severity",
	}
	values := url.Values{"cause": {"zcp"}, "scenario": {"s1"}, "sort": {"severity"}}
	q := Query{Closed: map[string][]string{"cause": {"zcp"}}, Open: map[string]string{"scenario": "s1"}, Sort: "severity", Dir: "desc"}
	counts := map[string]OptionCounts{"cause": {"zcp": 4, "test": 2, "agent": 0}, "severity": {"high": 1, "medium": 3}}

	nav := buildListNav("/findings", spec, q, values, counts, map[string]string{"severity": "Severity"}, testLabeler())

	byValue := map[string]FilterOptionView{}
	for _, g := range nav.Filters.Groups {
		for _, o := range g.Options {
			byValue[g.Name+"="+o.Value] = o
		}
	}
	if o := byValue["cause=test"]; !strings.Contains(o.URL, "cause=zcp%2Ctest") || !strings.Contains(o.URL, "scenario=s1") {
		t.Errorf("cause=test option URL = %q, want it to add test to the set and keep scenario", o.URL)
	}
	if o := byValue["cause=zcp"]; !o.Active || strings.Contains(o.URL, "cause=") {
		t.Errorf("active cause=zcp option = %+v, want active with a URL removing it", o)
	}
	if o := byValue["cause=agent"]; o.URL != "" || o.Count != 0 {
		t.Errorf("count-0 option = %+v, want no link", o)
	}
	if o := byValue["severity=medium"]; !strings.Contains(o.URL, "severity=medium") || o.Label != "medium+" {
		t.Errorf("severity option = %+v, want a link setting the minimum, labelled medium+", o)
	}
	if o := byValue["severity=high"]; o.Label != "high" {
		t.Errorf("top severity option label = %q, want no + (nothing is above it)", o.Label)
	}
	if len(nav.Filters.Active) != 2 || nav.Filters.ClearURL == "" {
		t.Errorf("active chips = %+v, clear = %q; want cause and scenario chips and a clear-all link", nav.Filters.Active, nav.Filters.ClearURL)
	}
	if h := nav.Sorts[0]; !h.Active || h.AriaSort != "descending" || !strings.Contains(h.URL, "dir=asc") {
		t.Errorf("active sort header = %+v, want descending and a link flipping to asc", h)
	}
	if h := nav.Sorts[1]; h.Active || !strings.Contains(h.URL, "sort=newest") || !strings.Contains(h.URL, "dir=desc") {
		t.Errorf("inactive sort header = %+v, want a link to newest in its default direction", h)
	}
}

// TestRenderBadQuery_Names400AndDropLink pins §8.7's refusal page.
func TestRenderBadQuery_Names400AndDropLink(t *testing.T) {
	t.Parallel()
	srv := NewServer(Config{Store: newFakeStore(), Token: testToken})
	r := httptest.NewRequest(http.MethodGet, "/findings?cause=zcp&bogus=1", nil)
	w := httptest.NewRecorder()
	srv.renderBadQuery(w, r, navFindings, &QueryError{Param: "bogus"})
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<code>bogus</code>") || !strings.Contains(body, `href="/findings?cause=zcp"`) {
		t.Errorf("400 page must name the parameter and link to the list without it:\n%s", body)
	}
}

// TestBuildListNav_SingleFilterReplaces pins §8.7's switch filters (kind,
// status): an option link replaces the value instead of adding it to the
// default, so the count on the option is the number of rows the click
// shows. (Live: "Empty 8" linked to kind=evaluation,empty and showed 27.)
func TestBuildListNav_SingleFilterReplaces(t *testing.T) {
	t.Parallel()
	spec := ListSpec{Closed: []ClosedFilter{{Name: "kind", Allowed: []string{"evaluation", "empty", "all"}, Single: true}},
		Defaults: map[string]string{"kind": "evaluation"}}
	q := Query{Closed: map[string][]string{"kind": {"evaluation"}}}
	nav := buildListNav("/", spec, q, url.Values{}, map[string]OptionCounts{"kind": {"evaluation": 19, "empty": 8, "all": 27}}, nil, testLabeler())
	for _, o := range nav.Filters.Groups[0].Options {
		switch o.Value {
		case "empty":
			if o.URL != "/?kind=empty" {
				t.Errorf("empty option URL = %q, want /?kind=empty (replace, not add)", o.URL)
			}
		case "evaluation":
			if !o.Active {
				t.Errorf("default option not marked active")
			}
		}
	}
	if len(nav.Filters.Active) != 0 {
		t.Errorf("a defaulted switch shows %d removable chips, want none", len(nav.Filters.Active))
	}
}

// TestParse_SingleFilterTakesOneValue pins that a switch filter refuses a
// comma list instead of OR-ing it.
func TestParse_SingleFilterTakesOneValue(t *testing.T) {
	t.Parallel()
	spec := ListSpec{Closed: []ClosedFilter{{Name: "kind", Allowed: []string{"evaluation", "empty", "all"}, Single: true}}}
	if _, err := Parse(spec, url.Values{"kind": {"evaluation,empty"}}); err == nil {
		t.Error("Parse accepted two values for a single-value filter")
	}
	if _, err := Parse(spec, url.Values{"kind": {"empty"}}); err != nil {
		t.Errorf("Parse refused one value: %v", err)
	}
}
