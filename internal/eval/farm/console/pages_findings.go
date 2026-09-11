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
	StepLink string // "" when the evidence cites no step (Step <= 0)
}

// findingItemView is one /findings row (§8.3 item 4's fields, reused here):
// FindingRow plus its evidence resolved into evidenceView.
type findingItemView struct {
	FindingRow
	Evidence []evidenceView
}

func newFindingItemView(f FindingRow) findingItemView {
	v := findingItemView{FindingRow: f}
	for _, e := range f.Evidence {
		link := ""
		if e.Step > 0 {
			link = fmt.Sprintf("/r/%s#s%d", f.RunID, e.Step)
		}
		v.Evidence = append(v.Evidence, evidenceView{Quote: e.Quote, Verified: e.Verified, StepLink: link})
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
	Param: func(param string) string {
		switch param {
		case paramCause:
			return "Cause"
		case paramSeverity:
			return "Severity"
		case paramSurface:
			return "Surface"
		case paramScenario:
			return "Scenario"
		case paramBatch:
			return "Batch"
		case paramBuild:
			return "Build"
		case sinceParamName:
			return "Window"
		default:
			return param
		}
	},
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
			return severityTooltip(value)
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
	Meta    pageMeta
	Nav     listNav
	Summary string
	Items   []findingItemView
}

func (s *Server) handleFindingsPage(w http.ResponseWriter, r *http.Request) {
	spec := findingListSpec()
	q, err := Parse(spec, r.URL.Query())
	if err != nil {
		var qerr *QueryError
		errors.As(err, &qerr)
		s.renderBadQuery(w, r, navFindings, qerr)
		return
	}

	rows, err := rowsSinceWindow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, q.Since, s.now(), s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	all := BuildFindingRows(rows)
	filtered, counts := findingEngine().Apply(all, q, s.now())

	items := make([]findingItemView, len(filtered))
	for i, f := range filtered {
		items[i] = newFindingItemView(f)
	}

	values := r.URL.Query()
	renderPage(w, "findings", findingsPageData{
		Meta:    s.pageMeta(r, "Findings", navFindings, false),
		Nav:     buildListNav("/findings", spec, q, values, counts, findingSortLabels, findingLabeler),
		Summary: fmt.Sprintf("%d finding%s in %s", len(all), pluralS(len(all)), sinceLabelText(sinceRawOrDefault(values, spec))),
		Items:   items,
	})
}
