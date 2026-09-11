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

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

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
// one problem's member finding.
type problemMemberView struct {
	FindingRow
	Quote     string
	QuoteStep int
}

func newProblemMemberView(f FindingRow) problemMemberView {
	v := problemMemberView{FindingRow: f}
	if len(f.Evidence) > 0 {
		v.Quote = f.Evidence[0].Quote
		v.QuoteStep = f.Evidence[0].Step
	}
	return v
}

// problemRowView is one /problems table row: Problem plus its members
// resolved into problemMemberView.
type problemRowView struct {
	Problem
	Members []problemMemberView
}

func newProblemRowView(p Problem) problemRowView {
	row := problemRowView{Problem: p}
	for _, m := range p.Members {
		row.Members = append(row.Members, newProblemMemberView(m))
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
			return severityTooltip(value)
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
	rows := make([]problemRowView, len(filtered))
	for i, p := range filtered {
		rows[i] = newProblemRowView(p)
	}

	values := r.URL.Query()
	renderPage(w, "problems", problemsPageData{
		Meta:        s.pageMeta(r, "Problems", navProblems, false),
		Nav:         buildListNav("/problems", spec, q, values, counts, problemSortLabels, problemLabeler),
		Summary:     fmt.Sprintf("%d live problem%s · %d high", liveN, pluralS(liveN), highN),
		NewestBuild: newestBuildLabel(runs),
		Rows:        rows,
	})
}
