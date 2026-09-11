// Package console: this file, pages_problems.go, owns GET /problems
// (§8.3 FM-51's problems page) over §8.6's problem clustering
// (problems.go's BuildProblems/problemEngine, already implemented) and
// §8.7's shared list engine (listnav.go) — split out like every other
// page (pages_batch.go, pages_run.go, pages_findings.go) so each page
// owns one file.
package console

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// problemAnchorID turns a Problem's Key into a stable HTML fragment id
// (item 12: "give each problem row a stable id from its key") — every
// character outside [A-Za-z0-9_-] maps to "-", mirroring pages_run.go's
// checkAnchor, so the same key always resolves to the same id both on
// /problems (problemRowView.AnchorID) and linked from the Overview's Top
// problems now (pages_home.go's topProblemView.ID).
func problemAnchorID(key string) string {
	var b strings.Builder
	b.WriteString("p-")
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// pluralS returns "" for n==1, else "s" — shared by this file and
// pages_findings.go's summary lines.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// newestBuildLabel names §8.6's "newest build" for display ("hit a/b runs
// on <newest build>") — "" when runs carries no assessed batch at all
// (newestBuildSha returns "").
func newestBuildLabel(runs []ProblemsRun) string {
	sha := newestBuildSha(runs)
	if sha == "" {
		return ""
	}
	for _, r := range runs {
		if r.Row.Build.Sha256 == sha {
			return r.Row.Build.Label()
		}
	}
	return ""
}

// problemMemberView adds §8.3's "first found quote with its step link" to
// one problem's member finding, plus (filter-tester finding, round 1) links
// that narrow /problems to this member's own scenario/batch/build — the
// open filters worked when typed into the URL but nothing on the page
// linked to them.
type problemMemberView struct {
	FindingRow
	Quote        string
	QuoteStep    int
	ScenarioLink string
	BatchLink    string
	BuildLink    string
}

func newProblemMemberView(f FindingRow, path string, values url.Values) problemMemberView {
	v := problemMemberView{
		FindingRow:   f,
		ScenarioLink: listURL(path, values, map[string]string{paramScenario: f.Scenario}),
		BatchLink:    listURL(path, values, map[string]string{paramBatch: f.Batch}),
		BuildLink:    listURL(path, values, map[string]string{paramBuild: f.Build.Sha12()}),
	}
	if len(f.Evidence) > 0 {
		v.Quote = f.Evidence[0].Quote
		v.QuoteStep = f.Evidence[0].Step
	}
	return v
}

// problemRowView is one /problems table row: Problem plus its members
// resolved into problemMemberView, AnchorID (item 12) — the stable id the
// Overview's Top problems now links into — and SurfaceLink (filter-tester
// finding, round 1): the row's own surface chip narrows to that surface,
// "" when the problem carries no surface (item 11's own guard).
type problemRowView struct {
	Problem
	AnchorID    string
	SurfaceLink string
	Members     []problemMemberView
	// LastSeenShort/FirstSeenShort are item 8's own compact dates
	// (pages_home.go's fmtTimeShort — "11 Sep 18:31" — reused rather than
	// fmtTime's long, wrapping "11 Sep 2026, 18:31 UTC") — a stacked-table
	// row is already tall with this row's other cells, so a wrapping date
	// column is the difference between a compact row and a ~180px one.
	LastSeenShort, FirstSeenShort string
}

func newProblemRowView(p Problem, path string, values url.Values) problemRowView {
	row := problemRowView{
		Problem: p, AnchorID: problemAnchorID(p.Key),
		LastSeenShort: fmtTimeShort(p.LastSeen), FirstSeenShort: fmtTimeShort(p.FirstSeen),
	}
	if p.Surface != "" {
		row.SurfaceLink = listURL(path, values, map[string]string{paramSurface: p.Surface})
	}
	for _, m := range p.Members {
		row.Members = append(row.Members, newProblemMemberView(m, path, values))
	}
	return row
}

// statusLiveValue is the problemListSpec `status` filter's "live" value
// (§8.7: "status=live|recurring|new|first-seen|gone|unconfirmed|all",
// default live) — named once so problemLabeler's Value/Title switches
// never drift apart on the literal.
const statusLiveValue = "live"

// problemSortLabels names problemListSpec's sort keys (§8.7 table) for
// their column headers.
var problemSortLabels = map[string]string{
	"rank": "Problem", "severity": "Severity", "runs": "Runs hit",
	"last": "Last seen", "first": "First seen",
}

// problemLabeler names problemListSpec's filters and their values for the
// filter bar (buildListNav).
var problemLabeler = listLabeler{
	Param: listParamLabel,
	Value: func(param, value string) string {
		switch param {
		case paramCause:
			if l := causeClassDisplay[value]; l != "" {
				return l
			}
			return value
		case paramSeverity:
			return severityLabel(value)
		case paramStatus:
			switch value {
			case filterAll:
				return "All"
			case statusLiveValue:
				return "Live"
			default:
				return problemStatusLabel(value)
			}
		default:
			return value
		}
	},
	Title: func(param, value string) string {
		switch param {
		case paramSeverity:
			return severityMinTooltip(value)
		case paramStatus:
			switch value {
			case statusLiveValue:
				return "new, first seen or recurring"
			case filterAll:
				return ""
			default:
				return problemStatusTooltip(value)
			}
		default:
			return ""
		}
	},
}

// problemsPageData is GET /problems (§8.3 FM-51).
type problemsPageData struct {
	Meta        pageMeta
	Nav         listNav
	Summary     string
	NewestBuild string
	Rows        []problemRowView
}

func (s *Server) handleProblemsPage(w http.ResponseWriter, r *http.Request) {
	spec := problemListSpec()
	q, err := Parse(spec, r.URL.Query())
	if err != nil {
		var qerr *QueryError
		errors.As(err, &qerr)
		s.renderBadQuery(w, r, navProblems, qerr)
		return
	}

	runs, err := s.problemsRunsSinceWindow(r.Context(), q.Since, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	all := BuildProblems(runs)

	liveN, highN := 0, 0
	for _, p := range all {
		if !isLiveStatus(p.Status) {
			continue
		}
		liveN++
		if p.Severity == observer.SeverityHigh {
			highN++
		}
	}

	filtered, counts := problemEngine().Apply(all, q, s.now())
	values := r.URL.Query()
	rows := make([]problemRowView, len(filtered))
	for i, p := range filtered {
		rows[i] = newProblemRowView(p, "/problems", values)
	}

	renderPage(w, "problems", problemsPageData{
		Meta:        s.pageMeta(r, "Problems", navProblems, false),
		Nav:         buildListNav("/problems", spec, q, values, counts, problemSortLabels, problemLabeler),
		Summary:     fmt.Sprintf("%d live problem%s · %d high", liveN, pluralS(liveN), highN),
		NewestBuild: newestBuildLabel(runs),
		Rows:        rows,
	})
}
