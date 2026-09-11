package console

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// List navigation (docs/spec-eval-farm.md §8.7 FM-55): the filter bar, the
// sortable column headers and the 400 page every list page renders, built
// once from a list's ListSpec, its parsed Query, the request's own query
// values and the engine's option counts — so "every link on the page keeps
// the other parameters" holds the same way on every page.

// sinceOptions are the windows a list's `since` filter offers as links.
var sinceOptions = []string{"24h", "7d", "30d", "90d"}

// FilterOptionView is one option of one filter group: its link toggles the
// option (a closed OR-set filter), replaces the value (a minimum or a
// window), or is empty when the option has no match under the other active
// filters and is not active — shown, not a link (§8.7).
type FilterOptionView struct {
	Value  string
	Label  string
	Title  string
	Count  int
	Counts bool // false for options without a count (since windows)
	Active bool
	URL    string
}

// FilterGroupView is one filter's row in the bar.
type FilterGroupView struct {
	Name    string
	Label   string
	Options []FilterOptionView
}

// ActiveChipView is one explicitly set filter value, removable.
type ActiveChipView struct {
	Label     string
	RemoveURL string
}

// FilterBarView is a list's whole filter bar.
type FilterBarView struct {
	Groups   []FilterGroupView
	Active   []ActiveChipView
	ClearURL string
}

// SortHeaderView is one sortable column header: its link sorts by Key, in
// the key's default direction, or flips the direction when already active.
type SortHeaderView struct {
	Key    string
	Label  string
	URL    string
	Active bool
	Dir    string // "asc" | "desc" when Active
	// AriaSort is the header's aria-sort value: ascending/descending/none.
	AriaSort string
}

// listNav bundles what a list page's template needs for its navigation.
type listNav struct {
	Filters FilterBarView
	Sorts   []SortHeaderView
}

// listParamLabels are the display names of every §8.7 list parameter, the
// same on every page's filter bar and active-filter chips.
var listParamLabels = map[string]string{
	paramCause: "Cause", paramSeverity: "Severity", paramStatus: "Status", paramSurface: "Surface",
	paramScenario: "Scenario", paramBatch: "Batch", paramBuild: "Build", paramVerdict: "Verdict",
	paramOutcome: "Outcome", paramKind: "Kind", "steps": "Steps", "since": "Window",
}

// listParamLabel names one list parameter for display.
func listParamLabel(param string) string {
	if l, ok := listParamLabels[param]; ok {
		return l
	}
	return param
}

// listLabeler names a filter parameter and one of its values for display.
type listLabeler struct {
	Param func(param string) string
	Value func(param, value string) string
	Title func(param, value string) string
}

// listURL returns path with values, every key in set applied (an empty
// value deletes the key), and notice/n always dropped — they are never
// carried into links (§8.7).
func listURL(path string, values url.Values, set map[string]string) string {
	v := url.Values{}
	for k, vs := range values {
		if k == "notice" || k == "n" {
			continue
		}
		v[k] = append([]string(nil), vs...)
	}
	for k, val := range set {
		if val == "" {
			v.Del(k)
			continue
		}
		v.Set(k, val)
	}
	if enc := v.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// toggled returns set with v added when absent or removed when present,
// in allowed's order, comma-joined.
func toggled(allowed, set []string, v string) string {
	next := make([]string, 0, len(set)+1)
	has := slices.Contains(set, v)
	for _, a := range allowed {
		if a == v {
			if !has {
				next = append(next, a)
			}
			continue
		}
		if slices.Contains(set, a) {
			next = append(next, a)
		}
	}
	return strings.Join(next, ",")
}

// buildListNav builds a list's filter bar and sort headers.
func buildListNav(path string, spec ListSpec, q Query, values url.Values, counts map[string]OptionCounts, sortLabels map[string]string, lab listLabeler) listNav {
	var bar FilterBarView
	for _, cf := range spec.Closed {
		g := FilterGroupView{Name: cf.Name, Label: lab.Param(cf.Name)}
		active := q.Closed[cf.Name]
		if cf.Min {
			active = nil
			if q.SeverityMin != "" {
				active = []string{q.SeverityMin}
			}
		}
		for _, v := range cf.Allowed {
			o := FilterOptionView{Value: v, Label: lab.Value(cf.Name, v), Title: lab.Title(cf.Name, v), Counts: true,
				Count: counts[cf.Name][v], Active: slices.Contains(active, v)}
			if cf.Min && v != cf.Allowed[0] {
				o.Label += "+" // a minimum: this value or above
			}
			switch {
			case cf.Min && o.Active:
				o.URL = listURL(path, values, map[string]string{cf.Name: ""})
			case cf.Min:
				o.URL = listURL(path, values, map[string]string{cf.Name: v})
			default:
				o.URL = listURL(path, values, map[string]string{cf.Name: toggled(cf.Allowed, active, v)})
			}
			if o.Count == 0 && !o.Active {
				o.URL = ""
			}
			g.Options = append(g.Options, o)
		}
		bar.Groups = append(bar.Groups, g)
		if values.Get(cf.Name) != "" {
			for _, v := range active {
				bar.Active = append(bar.Active, ActiveChipView{
					Label:     lab.Param(cf.Name) + ": " + lab.Value(cf.Name, v),
					RemoveURL: removeValueURL(path, values, cf, active, v),
				})
			}
		}
	}
	if spec.HasSince {
		g := FilterGroupView{Name: "since", Label: lab.Param("since")}
		current := values.Get("since")
		if current == "" {
			current = spec.DefaultSince
		}
		for _, w := range sinceOptions {
			g.Options = append(g.Options, FilterOptionView{Value: w, Label: w, Active: w == current,
				URL: listURL(path, values, map[string]string{"since": w})})
		}
		bar.Groups = append(bar.Groups, g)
		if values.Get("since") != "" {
			bar.Active = append(bar.Active, ActiveChipView{Label: lab.Param("since") + ": " + values.Get("since"),
				RemoveURL: listURL(path, values, map[string]string{"since": ""})})
		}
	}
	for _, name := range spec.Open {
		if v := values.Get(name); v != "" {
			bar.Active = append(bar.Active, ActiveChipView{Label: lab.Param(name) + ": " + v,
				RemoveURL: listURL(path, values, map[string]string{name: ""})})
		}
	}
	if len(bar.Active) > 0 {
		cleared := map[string]string{"since": ""}
		for _, cf := range spec.Closed {
			cleared[cf.Name] = ""
		}
		for _, name := range spec.Open {
			cleared[name] = ""
		}
		bar.ClearURL = listURL(path, values, cleared)
	}

	sorts := make([]SortHeaderView, 0, len(spec.Sorts))
	for _, k := range spec.Sorts {
		h := SortHeaderView{Key: k.Name, Label: sortLabels[k.Name], AriaSort: "none"}
		if h.Label == "" {
			h.Label = k.Name
		}
		dir := k.DefaultDir
		if q.Sort == k.Name {
			h.Active = true
			h.Dir = q.Dir
			if h.Dir == "" {
				h.Dir = k.DefaultDir
			}
			h.AriaSort = map[string]string{"asc": "ascending", "desc": "descending"}[h.Dir]
			dir = map[string]string{"asc": "desc", "desc": "asc"}[h.Dir]
		}
		h.URL = listURL(path, values, map[string]string{"sort": k.Name, "dir": dir})
		sorts = append(sorts, h)
	}
	return listNav{Filters: bar, Sorts: sorts}
}

// removeValueURL drops v from an explicitly set closed filter: a minimum
// filter is removed; an OR-set keeps its other values.
func removeValueURL(path string, values url.Values, cf ClosedFilter, active []string, v string) string {
	if cf.Min {
		return listURL(path, values, map[string]string{cf.Name: ""})
	}
	return listURL(path, values, map[string]string{cf.Name: toggled(cf.Allowed, active, v)})
}

// badQueryPage is the 400 page of §8.7: it names the refused parameter and
// its allowed values, with a link that drops it.
type badQueryPage struct {
	Meta    pageMeta
	Param   string
	Allowed []string
	DropURL string
}

// renderBadQuery answers a list page's refused parameter with the §8.7 400
// page.
func (s *Server) renderBadQuery(w http.ResponseWriter, r *http.Request, nav string, qerr *QueryError) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	renderPage(w, "badquery", badQueryPage{
		Meta:    s.pageMeta(r, "Unknown filter", nav, false),
		Param:   qerr.Param,
		Allowed: qerr.Allowed,
		DropURL: listURL(r.URL.Path, r.URL.Query(), map[string]string{qerr.Param: ""}),
	})
}
