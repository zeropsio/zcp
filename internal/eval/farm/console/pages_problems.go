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
	"sort"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// problemAnchorID turns a Problem's Key into the stable HTML fragment used
// both on /problems and by the Overview's Top problems links.
func problemAnchorID(key string) string {
	return safeFragment("p-", key)
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
	FindingLink  string
	Quote        string
	QuoteStep    int
	ScenarioLink string
	BatchLink    string
	BuildLink    string
}

func newProblemMemberView(f FindingRow, path string, values url.Values) problemMemberView {
	v := problemMemberView{
		FindingRow:   f,
		FindingLink:  fmt.Sprintf("/r/%s#f%d", f.RunID, f.Index+1),
		ScenarioLink: listURL(path, values, map[string]string{paramScenario: f.Scenario}),
		BatchLink:    listURL(path, values, map[string]string{paramBatch: f.Batch}),
		BuildLink:    listURL(path, values, map[string]string{paramBuild: f.Build.Sha12()}),
	}
	for _, evidence := range f.Evidence {
		if evidence.Verified && evidence.Step > 0 && evidence.Quote != "" {
			v.Quote, v.QuoteStep = evidence.Quote, evidence.Step
			break
		}
	}
	return v
}

// problemRowView is one /problems disclosure row: Problem plus its members
// resolved into problemMemberView, AnchorID (item 12) — the stable id the
// Overview's Top problems now links into — and SurfaceLink (filter-tester
// finding, round 1): the row's own surface chip narrows to that surface,
// "" when the problem carries no surface (item 11's own guard).
type problemRowView struct {
	Problem
	AnchorID    string
	SurfaceLink string
	FindingLink string
	Members     []problemMemberView
	// Compact UTC dates keep comparative facts readable in the closed row.
	LastSeenShort, FirstSeenShort string
}

func newProblemRowView(p Problem, path string, values url.Values) problemRowView {
	lastSeen, firstSeen := unknownDash, unknownDash
	if p.LastSeenKnown {
		lastSeen = fmtTimeShort(p.LastSeen)
	}
	if p.FirstSeenKnown {
		firstSeen = fmtTimeShort(p.FirstSeen)
	}
	row := problemRowView{
		Problem: p, AnchorID: problemAnchorID(p.Key),
		LastSeenShort: lastSeen, FirstSeenShort: firstSeen,
	}
	if p.Surface != "" {
		row.SurfaceLink = listURL(path, values, map[string]string{paramSurface: p.Surface})
	}
	for _, m := range p.Members {
		row.Members = append(row.Members, newProblemMemberView(m, path, values))
	}
	if len(row.Members) > 0 {
		row.FindingLink = row.Members[0].FindingLink
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
	"rank": "Priority", "severity": "Severity", "runs": "Runs hit",
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
				return "regressed, first seen, recurring or still emitted"
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
	EmptyTitle  string
	EmptyDetail string
	// FailedAssessmentPrefix/FailedAssessmentRuns are item 9 (FIX3)'s own
	// warning: BuildProblems only clusters current observations with
	// status ok, so a run whose assessment errored or failed to parse
	// silently drops out of every aggregate below it — this names which
	// runs, rather than leaving the reader to trust a list that quietly
	// excluded them. Split so the template links each run id itself
	// (never a pre-built HTML string in Go — every page value here still
	// goes through {{...}}'s normal auto-escaping, per pages.go's own
	// invariant); FailedAssessmentRuns is nil (no banner) when none failed.
	FailedAssessmentPrefix string
	FailedAssessmentRuns   []RunRow
}

// buildFailedAssessmentBanner finds every run in scope whose current
// observation failed (status error/unparsed) — BuildProblems (§8.6) never
// sees these, since it clusters status-ok observations only — and renders
// the banner's own prefix text (item 9, FIX3): "" (no runs) means no
// banner at all.
func buildFailedAssessmentBanner(runs []ProblemsRun) (prefix string, affected []RunRow) {
	for _, r := range runs {
		if observationFailed(r.Row.Observation) {
			affected = append(affected, r.Row)
		}
	}
	switch len(affected) {
	case 0:
		return "", nil
	case 1:
		return "1 run's assessment could not be parsed — its findings are missing from this list: ", affected
	default:
		return fmt.Sprintf("%d runs' assessments could not be parsed — their findings are missing from this list: ", len(affected)), affected
	}
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

	ctx := r.Context()
	now := s.now()
	// Item 1 (FIX3): status is computed over the full farm history
	// (allRuns), never just this page's own since window — that window
	// only decides scopeRuns, i.e. which problems are shown at all.
	allRuns, err := s.allProblemsRuns(ctx)
	if err != nil {
		s.renderStoreError(w, r, http.StatusBadGateway, "Problems unavailable", "The problem history could not be read.")
		return
	}
	scopeRuns := problemsRunsInWindow(allRuns, q.Since, now)
	all := BuildProblemsScoped(allRuns, problemsRunIDSet(scopeRuns), s.stepTextFinder(ctx))

	filtered, counts := problemEngine().Apply(all, q, now)
	liveN, highN := 0, 0
	for _, p := range filtered {
		if isLiveStatus(p.Status) {
			liveN++
		}
		if p.Severity == observer.SeverityHigh {
			highN++
		}
	}

	values := r.URL.Query()
	rows := make([]problemRowView, len(filtered))
	for i, p := range filtered {
		rows[i] = newProblemRowView(p, "/problems", values)
	}

	nav := buildListNav("/problems", spec, q, values, counts, problemSortLabels, problemLabeler)
	groupOrder := map[string]int{paramStatus: -2, "since": -1}
	sort.SliceStable(nav.Filters.Groups, func(i, j int) bool {
		return groupOrder[nav.Filters.Groups[i].Name] < groupOrder[nav.Filters.Groups[j].Name]
	})
	failedPrefix, failedRuns := buildFailedAssessmentBanner(scopeRuns)
	emptyTitle, emptyDetail := "No problems match these filters", "Try a different cause, status or severity, or reset filters to the default live view."
	if len(all) == 0 {
		assessed := 0
		for _, run := range scopeRuns {
			if run.Row.Observation != nil && run.Row.Observation.Status == observationStatusOK {
				assessed++
			}
		}
		switch {
		case len(scopeRuns) == 0:
			emptyTitle, emptyDetail = "No runs in this window", "Choose a wider window to inspect earlier evaluations."
		case assessed == 0:
			emptyTitle, emptyDetail = "No assessed runs in this window", "Problems need a successful assessment. Runs without one do not establish that the build is clean."
		default:
			emptyTitle = "No problems reported in this window"
			emptyDetail = fmt.Sprintf("%d of %d runs have a successful current assessment. A lack of findings is not proof that unassessed runs are clean.", assessed, len(scopeRuns))
		}
	}

	renderPage(w, "problems", problemsPageData{
		Meta:                   s.pageMeta(r, "Problems", navProblems, false),
		Nav:                    nav,
		Summary:                fmt.Sprintf("%d matching problem%s · %d live · %d high", len(filtered), pluralS(len(filtered)), liveN, highN),
		NewestBuild:            newestBuildLabel(allRuns),
		Rows:                   rows,
		EmptyTitle:             emptyTitle,
		EmptyDetail:            emptyDetail,
		FailedAssessmentPrefix: failedPrefix,
		FailedAssessmentRuns:   failedRuns,
	})
}
