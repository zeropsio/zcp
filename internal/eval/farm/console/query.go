// Package console: §8.7 lists — one query engine shared by every list
// (Overview batches, /problems, /findings, a batch's runs, /api/runs.md,
// a run's steps) — plans/farm-console-clarity-2026-09-11-briefs/MODEL.md
// item 5. Parameter validation, defaults, filtering (closed OR-within/
// AND-across + severity minimum + open exact/prefix), per-option counts
// under the other active filters, and sorting (default direction, a
// stable tie-break, unknown-last in both directions) all live here, once;
// each list's own file (batches.go, problems.go, findings_model.go,
// view.go) supplies only its ListSpec and the Engine's per-field
// predicates/comparators for its own item type.
package console

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// §8.7 parameter names, shared across every list's ListSpec/Match wiring
// (batches.go, problems.go, findings_model.go, view.go all filter on the
// same concepts) — named once so the same literal isn't repeated at every
// call site.
const (
	paramCause    = "cause"
	paramSeverity = "severity"
	paramSurface  = "surface"
	paramScenario = "scenario"
	paramBatch    = "batch"
	paramBuild    = "build"
	paramStatus   = "status"
	paramKind     = "kind"
	paramVerdict  = "verdict"
	paramOutcome  = "outcome"
)

// Sort directions (§8.7 `dir=`).
const (
	dirAsc  = "asc"
	dirDesc = "desc"
)

// filterAll is the closed-filter value that turns a filter off (kind=all,
// status=all, steps=all — §8.7).
const filterAll = "all"

// QueryError names a bad parameter and its allowed values (§8.7): the
// page renders 400 naming the parameter and its allowed values with a
// link that drops it; the API answers 400 {error, allowed}.
type QueryError struct {
	Param   string
	Allowed []string
}

func (e *QueryError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("console: unknown parameter %q", e.Param)
	}
	return fmt.Sprintf("console: parameter %q: allowed %s", e.Param, strings.Join(e.Allowed, ", "))
}

// ClosedFilter is one closed-set filter's declaration (§8.7 table). Min
// marks a filter that takes one minimum value rather than an OR-set —
// `severity` is the only one today ("severity, which is one minimum
// everywhere").
type ClosedFilter struct {
	Name    string
	Allowed []string
	Min     bool
}

// SortKey is one list's sort key (§8.7 table): Name is the `sort=` value,
// DefaultDir its default direction absent `dir=`.
type SortKey struct {
	Name       string
	DefaultDir string // "asc" | "desc"
}

// ListSpec declares one list's full §8.7 query surface.
type ListSpec struct {
	Closed      []ClosedFilter
	Open        []string // open-value filter names (batch, scenario, build, surface): matched exactly, or as a kind-prefix when the value ends "*"
	Sorts       []SortKey
	DefaultSort string
	// Defaults holds a closed filter's default value when absent from the
	// URL (e.g. kind=evaluation, status=live).
	Defaults map[string]string
	// HasSince declares this list takes `since` (§8.4: a Go duration or
	// <n>d); DefaultSince is its default when omitted ("" = ParseWindow's
	// own 24h default). NoSinceDefault marks a list whose table row names
	// no default at all (only /problems and /findings state one, §8.7) —
	// for those, an absent `since` leaves the window unbounded (every item
	// matches, regardless of Time) rather than falling back to
	// ParseWindow's 24h.
	HasSince       bool
	DefaultSince   string
	NoSinceDefault bool
	// AllowObs declares this list also takes `obs` (§8.3: "/r/ also takes
	// obs").
	AllowObs bool
}

func (s ListSpec) closedFilter(name string) (ClosedFilter, bool) {
	for _, c := range s.Closed {
		if c.Name == name {
			return c, true
		}
	}
	return ClosedFilter{}, false
}

func (s ListSpec) hasOpen(name string) bool {
	return slices.Contains(s.Open, name)
}

func (s ListSpec) sortKey(name string) (SortKey, bool) {
	for _, k := range s.Sorts {
		if k.Name == name {
			return k, true
		}
	}
	return SortKey{}, false
}

// allowedParamNames lists every parameter this list accepts (for an
// "unknown parameter" QueryError's Allowed) — notice/n (and obs, when
// AllowObs) are accepted everywhere but omitted here since they are never
// "the" thing a caller should switch to.
func (s ListSpec) allowedParamNames() []string {
	names := make([]string, 0, len(s.Closed)+len(s.Open)+3)
	for _, c := range s.Closed {
		names = append(names, c.Name)
	}
	names = append(names, s.Open...)
	if len(s.Sorts) > 0 {
		names = append(names, "sort", "dir")
	}
	if s.HasSince {
		names = append(names, "since")
	}
	sort.Strings(names)
	return names
}

// Query is one list request's parsed, validated parameters (§8.7): "the
// state lives in the URL."
type Query struct {
	Closed      map[string][]string
	SeverityMin string
	Open        map[string]string
	HasSince    bool
	Since       time.Duration
	Sort        string
	Dir         string
	Notice      string
	N           string
	Obs         string
}

func sortNames(sorts []SortKey) []string {
	out := make([]string, len(sorts))
	for i, s := range sorts {
		out[i] = s.Name
	}
	return out
}

func containsStr(list []string, v string) bool {
	return slices.Contains(list, v)
}

// Parse validates values against spec (§8.7): an unknown parameter or a
// closed-set value outside its set is refused with a QueryError naming
// the bad parameter and its allowed values; an open filter accepts any
// value (an open value matching nothing yields an empty list from Apply,
// never an error here). notice/n are accepted on every list and never
// validated; obs only when spec.AllowObs.
func Parse(spec ListSpec, values url.Values) (Query, error) {
	q := Query{Closed: map[string][]string{}, Open: map[string]string{}, HasSince: spec.HasSince}
	for key, vals := range values {
		val := ""
		if len(vals) > 0 {
			val = vals[len(vals)-1]
		}
		switch key {
		case "notice":
			q.Notice = val
			continue
		case "n":
			q.N = val
			continue
		case "obs":
			if !spec.AllowObs {
				return Query{}, &QueryError{Param: "obs", Allowed: spec.allowedParamNames()}
			}
			q.Obs = val
			continue
		case "sort":
			if len(spec.Sorts) == 0 {
				return Query{}, &QueryError{Param: "sort", Allowed: spec.allowedParamNames()}
			}
			sk, ok := spec.sortKey(val)
			if !ok {
				return Query{}, &QueryError{Param: "sort", Allowed: sortNames(spec.Sorts)}
			}
			q.Sort = sk.Name
			continue
		case "dir":
			if len(spec.Sorts) == 0 {
				return Query{}, &QueryError{Param: "dir", Allowed: spec.allowedParamNames()}
			}
			if val != dirAsc && val != dirDesc {
				return Query{}, &QueryError{Param: "dir", Allowed: []string{dirAsc, dirDesc}}
			}
			q.Dir = val
			continue
		case "since":
			if !spec.HasSince {
				return Query{}, &QueryError{Param: "since", Allowed: spec.allowedParamNames()}
			}
			d, err := ParseWindow(val)
			if err != nil {
				return Query{}, &QueryError{Param: "since", Allowed: []string{"a Go duration (e.g. 24h) or <n>d (e.g. 7d)"}}
			}
			q.Since = d
			continue
		}
		if cf, ok := spec.closedFilter(key); ok {
			if cf.Min {
				if !containsStr(cf.Allowed, val) {
					return Query{}, &QueryError{Param: key, Allowed: cf.Allowed}
				}
				q.SeverityMin = val
				continue
			}
			parts := strings.Split(val, ",")
			for _, p := range parts {
				if !containsStr(cf.Allowed, p) {
					return Query{}, &QueryError{Param: key, Allowed: cf.Allowed}
				}
			}
			q.Closed[key] = parts
			continue
		}
		if spec.hasOpen(key) {
			q.Open[key] = val
			continue
		}
		return Query{}, &QueryError{Param: key, Allowed: spec.allowedParamNames()}
	}

	for _, cf := range spec.Closed {
		if _, ok := q.Closed[cf.Name]; ok {
			continue
		}
		def, ok := spec.Defaults[cf.Name]
		if !ok {
			continue
		}
		if cf.Min {
			if q.SeverityMin == "" {
				q.SeverityMin = def
			}
			continue
		}
		q.Closed[cf.Name] = strings.Split(def, ",")
	}
	if spec.HasSince && q.Since == 0 && !spec.NoSinceDefault {
		d, err := ParseWindow(spec.DefaultSince)
		if err != nil {
			d = defaultWindow
		}
		q.Since = d
	}
	if len(spec.Sorts) > 0 {
		if q.Sort == "" {
			q.Sort = spec.DefaultSort
		}
		if q.Dir == "" {
			if sk, ok := spec.sortKey(q.Sort); ok {
				q.Dir = sk.DefaultDir
			}
		}
	}
	return q, nil
}

// OpenMatch is the exact/prefix rule open filters use (§8.7): a value
// ending "*" matches an item's field as a prefix ("tool:*" matches a
// kind); otherwise it must match exactly.
func OpenMatch(field, value string) bool {
	if prefix, ok := strings.CutSuffix(value, "*"); ok {
		return strings.HasPrefix(field, prefix)
	}
	return field == value
}

// FilterFunc reports whether item satisfies one closed/open filter's
// value — the same function serves both filter kinds (a closed filter's
// value is always one of ClosedFilter.Allowed; an open filter's may be
// anything, including an OpenMatch "prefix*").
type FilterFunc[T any] func(item T, name, value string) bool

// SeverityFunc returns item's own severity, for the `severity` minimum
// filter.
type SeverityFunc[T any] func(item T) string

// TimeFunc returns item's own time, for a HasSince list's window
// membership.
type TimeFunc[T any] func(item T) time.Time

// SortSpec is one sort key's engine wiring: Primary compares a and b on
// the field itself in natural ascending order (dir="desc" flips only
// this); Tiebreak compares them on the table's documented tie-break field,
// always in its own fixed direction (the table names no reversible
// direction for a tie-break, so Apply never flips it); Unknown reports
// whether item's value for this key is unknown ("sorts last in both
// directions").
type SortSpec[T any] struct {
	Primary  func(a, b T) int
	Tiebreak func(a, b T) int
	Unknown  func(item T) bool
}

// Engine is one list's full wiring: how to match every filter, read
// severity/time, and compare every sort key.
type Engine[T any] struct {
	Spec     ListSpec
	Match    FilterFunc[T]
	Severity SeverityFunc[T]
	Time     TimeFunc[T]
	Sorts    map[string]SortSpec[T]
}

func (e Engine[T]) matches(item T, q Query, now time.Time) bool {
	if q.HasSince && q.Since > 0 && e.Time != nil {
		t := e.Time(item)
		if t.IsZero() || t.Before(now.Add(-q.Since)) || t.After(now) {
			return false
		}
	}
	for name, values := range q.Closed {
		ok := false
		for _, v := range values {
			if e.Match(item, name, v) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if q.SeverityMin != "" && e.Severity != nil {
		if problemSeverityRank(e.Severity(item)) < problemSeverityRank(q.SeverityMin) {
			return false
		}
	}
	for name, val := range q.Open {
		if !e.Match(item, name, val) {
			return false
		}
	}
	return true
}

// OptionCounts is one closed filter's per-value counts under the query's
// OTHER active filters (§8.7: "Each filter option shows its count under
// the other active filters") — this filter's own active values excluded
// from that computation.
type OptionCounts map[string]int

func cloneClosedWithout(m map[string][]string, except string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		if k == except {
			continue
		}
		out[k] = v
	}
	return out
}

// Apply filters items against q, sorts the result (q.Sort/q.Dir), and
// computes every closed filter's per-value counts under the query's other
// active filters. now anchors a HasSince list's window.
func (e Engine[T]) Apply(items []T, q Query, now time.Time) (result []T, counts map[string]OptionCounts) {
	counts = make(map[string]OptionCounts)
	for _, cf := range e.Spec.Closed {
		if cf.Min {
			continue
		}
		oc := make(OptionCounts, len(cf.Allowed))
		for _, v := range cf.Allowed {
			qv := q
			qv.Closed = cloneClosedWithout(q.Closed, cf.Name)
			qv.Closed[cf.Name] = []string{v}
			n := 0
			for _, it := range items {
				if e.matches(it, qv, now) {
					n++
				}
			}
			oc[v] = n
		}
		counts[cf.Name] = oc
	}

	filtered := make([]T, 0, len(items))
	for _, it := range items {
		if e.matches(it, q, now) {
			filtered = append(filtered, it)
		}
	}
	e.sortItems(filtered, q)
	return filtered, counts
}

func (e Engine[T]) sortItems(items []T, q Query) {
	spec, ok := e.Sorts[q.Sort]
	if !ok || spec.Primary == nil {
		return
	}
	desc := q.Dir == "desc"
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if spec.Unknown != nil {
			au, bu := spec.Unknown(a), spec.Unknown(b)
			if au != bu {
				return !au // known sorts before unknown, in both directions
			}
			if au && bu {
				return false
			}
		}
		c := spec.Primary(a, b)
		if desc {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
		if spec.Tiebreak != nil {
			return spec.Tiebreak(a, b) < 0
		}
		return false
	})
}

// cmpInt and cmpTime are small ascending three-way comparators — every
// list's SortSpec.Primary/Tiebreak is built from these.
func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	default:
		return 0
	}
}

func cmpString(a, b string) int {
	return strings.Compare(a, b)
}
