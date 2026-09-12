// Package console: this file, pages_findings.go, owns GET /findings (§8.3
// FM-51's findings page) over findings_model.go's FindingRow/
// BuildFindingRows/findingEngine (already implemented) and §8.7's shared
// list engine (listnav.go) — split out of pages.go (S-SHELL item 1) so
// each page owns one file.
package console

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// findingOwners is §7.5's owner vocabulary, ZCP-first (§8.8: "Lists order
// causes ZCP first"). This page itself no longer filters or groups by raw
// owner (superseded by findingListSpec's class-level `cause` filter,
// §8.7) — the slice remains solely because labels.go's causeOrder builds
// the /terms Cause table from this exact variable ("the findings page's
// own filter order and every vocabulary listing ... can never drift
// apart"); renaming or removing it would break that file, which is
// outside this brief's write-set.
var findingOwners = []string{"zcp-guidance", "zcp-tool", "platform", "agent", "scenario", "evaluator"}

// evidenceView is one finding's evidence entry with its step link resolved
// (§8.3: "evidence (step link, quote found/quote not found)").
type evidenceView struct {
	Quote    string
	Verified bool
	Step     int
	StepLink string // "" unless Step is an exact visible target on the run page
}

// findingItemView is one /findings row (§8.3 item 4's fields, reused here):
// FindingRow plus its evidence resolved into evidenceView and (filter-
// tester finding, round 1) links that narrow /findings to this row's own
// scenario/batch/build/surface — the open filters worked when typed into
// the URL but nothing on the page linked to them.
type findingItemView struct {
	FindingRow
	Evidence     []evidenceView
	FindingLink  string
	ScenarioLink string
	BatchLink    string
	BuildLink    string
	SurfaceLink  string // "" when the finding carries no surface (item 11's own guard)
}

func newFindingItemView(f FindingRow, path string, values url.Values, availableSteps map[int]bool) findingItemView {
	v := findingItemView{
		FindingRow:   f,
		FindingLink:  fmt.Sprintf("/r/%s#f%d", f.RunID, f.Index+1),
		ScenarioLink: listURL(path, values, map[string]string{paramScenario: f.Scenario}),
		BatchLink:    listURL(path, values, map[string]string{paramBatch: f.Batch}),
		BuildLink:    listURL(path, values, map[string]string{paramBuild: f.Build.Sha12()}),
	}
	if f.Surface != "" {
		v.SurfaceLink = listURL(path, values, map[string]string{paramSurface: f.Surface})
	}
	for _, e := range f.Evidence {
		link := ""
		if e.Step > 0 && availableSteps[e.Step] {
			link = fmt.Sprintf("/r/%s#s%d", f.RunID, e.Step)
		}
		v.Evidence = append(v.Evidence, evidenceView{Quote: e.Quote, Verified: e.Verified, Step: e.Step, StepLink: link})
	}
	return v
}

// sinceParamName is the §8.4/§8.7 `since` query parameter's name, named
// once so this file's two references to it (the filter label switch and
// sinceRawOrDefault) never drift apart.
const sinceParamName = "since"

// findingSortLabels names findingListSpec's sort keys (§8.7 table) for the
// page's sort row.
var findingSortLabels = map[string]string{
	"severity": "Severity", "newest": "Newest", paramCause: "Cause",
}

// findingLabeler names findingListSpec's filters and their values for the
// filter bar (buildListNav).
var findingLabeler = listLabeler{
	Param: listParamLabel,
	Value: func(param, value string) string {
		switch param {
		case paramCause:
			if l := causeClassDisplay[value]; l != "" {
				return l
			}
		case paramSeverity:
			return severityLabel(value)
		}
		return value
	},
	Title: func(param, value string) string {
		if param == paramSeverity {
			return severityMinTooltip(value)
		}
		return ""
	},
}

// sinceLabelText renders a §8.4 window string for people: "<n>d" as
// "<n> days" ("1 day" for n=1); any other form (a plain Go duration like
// "24h") is shown verbatim.
func sinceLabelText(raw string) string {
	days, ok := strings.CutSuffix(raw, "d")
	if !ok {
		return raw
	}
	n, err := strconv.Atoi(days)
	if err != nil || n <= 0 {
		return raw
	}
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// sinceRawOrDefault is the raw ?since= value the summary line names: the
// URL's own value, else the list's own default (findingListSpec's "7d").
func sinceRawOrDefault(values url.Values, spec ListSpec) string {
	if v := values.Get(sinceParamName); v != "" {
		return v
	}
	return spec.DefaultSince
}

// findingsPageData is GET /findings (§8.3 FM-51).
type findingsPageData struct {
	Meta            pageMeta
	Nav             listNav
	Summary         string
	SourceRuns      int
	AssessedRuns    int
	UnavailableRuns int
	Total           int
	Items           []findingItemView
	EmptyTitle      string
	EmptyDetail     string
}

func (s *Server) handleFindingsPage(w http.ResponseWriter, r *http.Request) {
	spec := findingListSpec()
	q, err := parseHTMLQuery(spec, r.URL.Query())
	if err != nil {
		var qerr *QueryError
		errors.As(err, &qerr)
		s.renderBadQuery(w, r, navFindings, qerr)
		return
	}

	rows, err := rowsSinceWindow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, q.Since, s.now(), s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		s.renderStoreError(w, r, http.StatusBadGateway, "Findings unavailable", "The finding evidence could not be read.", "load findings", err)
		return
	}
	all := BuildFindingRows(rows)
	filtered, counts := findingEngine().Apply(all, q, s.now())
	assessedRuns, unavailableRuns := 0, 0
	for _, row := range rows {
		if assessmentWorkUnavailable(row) {
			unavailableRuns++
		}
		if row.Observation != nil && row.Observation.Status == observationStatusOK {
			assessedRuns++
		}
	}
	var emptyTitle, emptyDetail string
	switch {
	case len(rows) == 0:
		emptyTitle = "No source runs in this window"
		emptyDetail = "No run records are available in this time window."
	case unavailableRuns > 0:
		emptyTitle = "Evidence is incomplete"
		if unavailableRuns == 1 {
			emptyDetail = "One source run has unavailable evidence, so the findings view may be incomplete."
		} else {
			emptyDetail = fmt.Sprintf("%d source runs have unavailable evidence, so the findings view may be incomplete.", unavailableRuns)
		}
	case assessedRuns == 0:
		emptyTitle = "No successfully assessed runs"
		if len(rows) == 1 {
			emptyDetail = "The source run has no successful assessment to report findings from."
		} else {
			emptyDetail = fmt.Sprintf("The %d source runs have no successful assessments to report findings from.", len(rows))
		}
	default:
		emptyTitle = "No findings reported"
		if assessedRuns == 1 {
			emptyDetail = "The successfully assessed run reported no findings."
		} else {
			emptyDetail = fmt.Sprintf("The %d successfully assessed runs reported no findings.", assessedRuns)
		}
	}

	values := r.URL.Query()
	availableSteps := make(map[string]map[int]bool, len(rows))
	for _, row := range rows {
		if row.VisibleStepNumbers == nil {
			continue
		}
		steps := make(map[int]bool, len(row.VisibleStepNumbers))
		for _, step := range row.VisibleStepNumbers {
			steps[step] = true
		}
		availableSteps[row.RunID] = steps
	}
	items := make([]findingItemView, len(filtered))
	for i, f := range filtered {
		items[i] = newFindingItemView(f, pathFindings, values, availableSteps[f.RunID])
	}

	meta := s.pageMeta(r, "Findings", navFindings, false)
	data := findingsPageData{
		Meta:            meta,
		Nav:             buildListNav(pathFindings, spec, q, values, counts, findingSortLabels, findingLabeler),
		Summary:         fmt.Sprintf("%d matching finding%s in %s", len(filtered), pluralS(len(filtered)), sinceLabelText(sinceRawOrDefault(values, spec))),
		SourceRuns:      len(rows),
		AssessedRuns:    assessedRuns,
		UnavailableRuns: unavailableRuns,
		Total:           len(all),
		Items:           items,
		EmptyTitle:      emptyTitle,
		EmptyDetail:     emptyDetail,
	}
	renderPage(w, "findings", data)
}
