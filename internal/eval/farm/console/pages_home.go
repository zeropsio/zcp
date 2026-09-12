// Package console: this file, pages_home.go, owns GET / (§8.3 FM-51's
// Overview page) — split out of pages.go (S-SHELL item 1) so each page owns
// one file. The page answers "what is broken in ZCP right now" (FM-51) in
// three blocks, top to bottom: the newest evaluation batch (item 1), the
// first five live problems (item 2, §8.6), and the Overview batches list
// (item 3, §8.7's own table row).
package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// homePageData is GET / (§8.3 FM-51).
type homePageData struct {
	Meta        pageMeta
	Latest      *latestEvaluationView
	TopProblems []topProblemView
	Nav         listNav
	// SortByKey lets the table template place each of Nav.Sorts' four
	// headers (newest/zcp/failed/cost) next to the column it actually
	// sorts (Started/ZCP findings/Verdicts/Cost) rather than in
	// ListSpec.Sorts' declaration order.
	SortByKey map[string]SortHeaderView
	Batches   []homeBatchRow
	MatchingN int
	TotalN    int
}

// sortHeadersByKey indexes a list's sort headers by key, for a template
// that places them next to a specific column rather than ranging over them
// in declaration order.
func sortHeadersByKey(sorts []SortHeaderView) map[string]SortHeaderView {
	m := make(map[string]SortHeaderView, len(sorts))
	for _, sh := range sorts {
		m[sh.Key] = sh
	}
	return m
}

// verdictDisputeView is one verdict bucket of the Latest evaluation panel's
// counts, with its disputed sub-count (§8.3: "verdict counts with disputed
// verdicts counted (`2 failed (1 disputed)`)").
type verdictDisputeView struct {
	Verdict  string
	Count    int
	Disputed int
}

// failedRunLine is one line of the Latest evaluation panel's "one line per
// failed or blocked run" list.
type failedRunLine struct {
	RunID      string
	Scenario   string
	Verdict    string
	Headline   string
	DisputeWhy string // "" unless the run's current observation disputes a check
}

// latestEvaluationView is the Overview's "Latest evaluation" panel (§8.3
// item 1): the newest batch whose set is gate/all with a finished run (else
// the newest batch with one), its verdict counts, its failed/blocked runs,
// and the vs-previous-batch line.
type latestEvaluationView struct {
	BatchID   string
	CreatedAt time.Time
	Build     BuildInfo
	Set       string

	Verdicts        []verdictDisputeView
	FailedOrBlocked []failedRunLine

	HasPrev      bool
	PrevBatchID  string
	Diff         BatchDiff
	Comparison   batchComparisonView
	ObservedN    int
	ObservedM    int
	UnavailableN int
}

// topProblemView is one line of the Overview's "Top problems now" panel
// (§8.3 item 2): the first five live problems (§8.6). ID is a stable HTML
// fragment id derived from the problem's own key (pages_problems.go's
// problemAnchorID) — item 12: "link only the title, to that problem on
// /problems", so a viewer lands on the same row problemRowView gave that id.
type topProblemView struct {
	ID          string
	Severity    string
	CauseLabels []string
	Surface     string
	Title       string
	HowOften    string
}

// dotView is one run's dot in the Overview batches table (§8.3 item 3):
// "one dot per run ordered as on the batch page (tooltip `scenario —
// verdict`)".
type dotView struct {
	Verdict string
	Tooltip string
}

// homeBatchRow is one row of the Overview batches table: a BatchRow
// (batches.go) plus this page's own additive facts — the ordered dots, the
// formatted cost string (§8.3: "— when unknown, $2.19 + 7 unknown") and the
// short "11 Sep 18:31" date (item 6: "fewer, non-wrapping columns" —
// fmtTime's own long "2 Jan 2006, 15:04 UTC" form is for a single-batch
// page's header, not a 100-odd-row table).
type homeBatchRow struct {
	BatchRow
	Dots         []dotView
	Cost         string
	StartedShort string
}

// unknownDash is this file's own "unknown/none" marker (§8.8's own "—"
// convention) — named once so goconst's repeated-literal check sees one
// declaration instead of two raw copies (fmtTimeShort/formatBatchCost).
const unknownDash = "—"

// fmtTimeShort renders a timestamp as "11 Sep 18:31" (item 6) — the
// Overview batches table's own compact date, distinct from fmtTime's long
// form used everywhere a page names one batch/run's own time.
func fmtTimeShort(t time.Time) string {
	if t.IsZero() {
		return unknownDash
	}
	return t.UTC().Format("2 Jan 15:04")
}

func (s *Server) handleHomePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	allBatches, err := loadBatchRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		http.Error(w, "list batches: "+err.Error(), http.StatusInternalServerError)
		return
	}

	spec := batchListSpec()
	q, err := Parse(spec, r.URL.Query())
	if err != nil {
		var qerr *QueryError
		errors.As(err, &qerr)
		s.renderBadQuery(w, r, navOverview, qerr)
		return
	}

	now := s.now()
	filtered, counts := batchEngine().Apply(allBatches, q, now)
	nav := buildListNav("/", spec, q, r.URL.Query(), counts, homeBatchSortLabels, homeBatchLabeler())

	rows, err := s.buildHomeBatchRows(ctx, filtered)
	if err != nil {
		http.Error(w, "batch rows: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var latest *latestEvaluationView
	if latestRow, ok := pickLatestEvaluationBatch(allBatches); ok {
		latest, err = s.buildLatestEvaluation(ctx, allBatches, latestRow)
		if err != nil {
			http.Error(w, "latest evaluation: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	top, err := s.buildTopProblems(ctx, now)
	if err != nil {
		http.Error(w, "top problems: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderPage(w, "home", homePageData{
		Meta:        s.pageMeta(r, "Overview", navOverview, s.anyObservationInFlight()),
		Latest:      latest,
		TopProblems: top,
		Nav:         nav,
		SortByKey:   sortHeadersByKey(nav.Sorts),
		Batches:     rows,
		MatchingN:   len(filtered),
		TotalN:      len(allBatches),
	})
}

// anyObservationInFlight reports whether any observation is queued or
// running anywhere on the console (§8.5) — the Overview page's own §8.5
// "a job of this page is in flight" scope, since it shows every batch.
func (s *Server) anyObservationInFlight() bool {
	if s.cfg.Queue == nil {
		return false
	}
	queued, running := s.cfg.Queue.Stats()
	return queued+running > 0
}

// buildHomeBatchRows resolves the dots and the cost string for every
// visible batch (post-filter/sort) — the table's own additive facts beyond
// BatchRow.
func (s *Server) buildHomeBatchRows(ctx context.Context, batches []BatchRow) ([]homeBatchRow, error) {
	out := make([]homeBatchRow, len(batches))
	for i, b := range batches {
		runRows, err := batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, b.BatchID, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			return nil, fmt.Errorf("console: home batch row %s: %w", b.BatchID, err)
		}
		out[i] = homeBatchRow{BatchRow: b, Dots: buildDots(runRows, s.now()), Cost: formatBatchCost(b), StartedShort: fmtTimeShort(b.CreatedAt)}
	}
	return out, nil
}

// formatBatchCost implements §8.3's agent-cost rendering: "—" when every
// run's cost is unknown (or the batch has no runs), else the sum of known
// costs, with " + N unknown" appended when some are ("$2.19 + 7 unknown").
func formatBatchCost(b BatchRow) string {
	knownN := b.ObservedM - b.CostUnknownN
	if knownN <= 0 {
		return unknownDash
	}
	cost := fmt.Sprintf("$%.2f", b.TotalCostUsd)
	if b.CostUnknownN > 0 {
		return fmt.Sprintf("%s + %d unknown", cost, b.CostUnknownN)
	}
	return cost
}

// homeDotLabel is one dot's tooltip verdict word: "stalled" for a running
// row past its budget (Stalled), else the plain verdict label.
func homeDotLabel(row RunRow) string {
	if row.Verdict == "" && assessmentWorkUnavailable(row) {
		return "automatic verdict unavailable"
	}
	if row.Verdict == verdictRunning && row.Stalled {
		return verdictLabel(verdictStalled)
	}
	return verdictLabel(row.Verdict)
}

// buildDots renders one dotView per run in the batch page's own order
// (orderedAsBatchPage), tooltip "scenario — verdict" (§8.3 item 3).
func buildDots(rows []RunRow, now time.Time) []dotView {
	ordered := orderedAsBatchPage(rows, now)
	out := make([]dotView, len(ordered))
	for i, r := range ordered {
		out[i] = dotView{Verdict: r.Verdict, Tooltip: r.Scenario + " — " + homeDotLabel(r)}
	}
	return out
}

// homeBatchSortLabels names the Overview batches list's four sort keys
// (§8.7 table) for buildListNav's sortable headers.
var homeBatchSortLabels = map[string]string{
	"newest": "Started",
	"zcp":    "ZCP findings",
	"failed": "Failed/blocked",
	"cost":   "Cost",
}

// homeBatchLabeler names the Overview batches list's own filter (`kind`)
// and its values, plus the shared `since` param, for buildListNav.
func homeBatchLabeler() listLabeler {
	return listLabeler{
		Param: listParamLabel,
		Value: func(param, value string) string {
			if param != paramKind {
				return value
			}
			switch value {
			case batchKindEvaluation:
				return "Evaluation"
			case batchKindEmpty:
				return "Empty"
			case batchKindUnavailable:
				return "Unavailable"
			case filterAll:
				return "All"
			default:
				return value
			}
		},
		Title: func(param, value string) string {
			if param == paramKind {
				switch value {
				case batchKindEmpty:
					return "Every run has readable evidence of zero work"
				case batchKindUnavailable:
					return "No run proves work and at least one run's evidence is not available yet or could not be read"
				}
			}
			return ""
		},
	}
}

// pickLatestEvaluationBatch implements §8.3 item 1's batch choice: the
// newest batch (rows is already newest-first, loadBatchRows) whose set is
// gate or all with a finished run, else the newest batch with one.
func pickLatestEvaluationBatch(rows []BatchRow) (BatchRow, bool) {
	var fallback BatchRow
	haveFallback := false
	for _, b := range rows {
		if b.Kind != batchKindEvaluation {
			continue
		}
		if !haveFallback {
			fallback, haveFallback = b, true
		}
		if b.Set == "gate" || b.Set == filterAll { // filterAll ("all"): farm.BatchManifest's own Set enum value, not a query filter — reused only for its text (goconst)
			return b, true
		}
	}
	return fallback, haveFallback
}

// verdictDisputeCounts implements §8.3 item 1's per-verdict counts with
// disputed runs counted within the verdict they disputed ("passed
// included", per §8.8's own Disputed definition).
func verdictDisputeCounts(rows []RunRow) []verdictDisputeView {
	counts := make(map[string]int, len(rows))
	disputed := make(map[string]int, len(rows))
	for _, r := range rows {
		if r.Verdict == "" {
			continue
		}
		counts[r.Verdict]++
		if r.Disputed {
			disputed[r.Verdict]++
		}
	}
	ordered := orderedVerdictCounts(counts)
	out := make([]verdictDisputeView, len(ordered))
	for i, vc := range ordered {
		out[i] = verdictDisputeView{Verdict: vc.Verdict, Count: vc.Count, Disputed: disputed[vc.Verdict]}
	}
	return out
}

// disputeWhy returns why obs's checks.agree is false: the format-1 why
// sentence, else the first incorrect judged check's why (format 2), else ""
// (never expected once Disputed is true, but tolerated).
func disputeWhy(obs *observer.Observation) string {
	if obs == nil {
		return ""
	}
	if obs.Checks.Why != "" {
		return obs.Checks.Why
	}
	for _, jc := range obs.Checks.Judged {
		if !jc.Correct {
			return jc.Why
		}
	}
	return ""
}

// failedOrBlockedLines implements §8.3 item 1's "one line per failed or
// blocked run: scenario, headline, and `observer disputes: <why>` when the
// checks were judged wrong" — headline is the current ok observation's
// headline when there is one, else the run's own assessment-state wording
// (view.go's ObserverStateText), matching the vocabulary every other
// surface falls back to for a run with nothing to summarize.
func failedOrBlockedLines(rows []RunRow) []failedRunLine {
	var out []failedRunLine
	for _, r := range rows {
		if r.Verdict != farm.VerdictFailed && r.Verdict != farm.VerdictBlocked {
			continue
		}
		line := failedRunLine{RunID: r.RunID, Scenario: r.Scenario, Verdict: r.Verdict, Headline: r.ObserverStateText}
		if r.Observation != nil && r.Observation.Status == observationStatusOK && r.Observation.Headline != "" {
			line.Headline = r.Observation.Headline
		}
		if r.Disputed {
			line.DisputeWhy = disputeWhy(r.Observation)
		}
		out = append(out, line)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scenario < out[j].Scenario })
	return out
}

// buildLatestEvaluation implements §8.3 item 1 end to end over latestRow,
// the batch pickLatestEvaluationBatch already chose — the caller renders no
// panel at all when that pick found nothing (never an empty placeholder).
func (s *Server) buildLatestEvaluation(ctx context.Context, allBatches []BatchRow, latestRow BatchRow) (*latestEvaluationView, error) {
	runRows, err := batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, latestRow.BatchID, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		return nil, fmt.Errorf("console: latest evaluation: %w", err)
	}

	view := &latestEvaluationView{
		BatchID: latestRow.BatchID, CreatedAt: latestRow.CreatedAt, Build: latestRow.Build, Set: latestRow.Set,
		Verdicts:        verdictDisputeCounts(runRows),
		FailedOrBlocked: failedOrBlockedLines(runRows),
		ObservedN:       latestRow.ObservedN,
		ObservedM:       latestRow.ObservedM,
	}
	for _, row := range runRows {
		if assessmentWorkUnavailable(row) {
			view.UnavailableN++
		}
	}

	if prevRow, found := PreviousSameSet(allBatches, latestRow); found {
		prevRunRows, err := batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, prevRow.BatchID, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			return nil, fmt.Errorf("console: latest evaluation vs previous: %w", err)
		}
		view.HasPrev, view.PrevBatchID = true, prevRow.BatchID
		view.Diff = CompareBatches(prevRunRows, runRows)
		view.Comparison = buildBatchComparison(view.Diff, runRows)
	}
	return view, nil
}

// fmtDateShort renders a timestamp as "10 Sep" (item 6) — problemHowOften's
// own bare date, distinct from fmtTimeShort's "11 Sep 18:31" (which still
// carries a time, useful for a single batch row but not for "since <day>").
func fmtDateShort(t time.Time) string {
	if t.IsZero() {
		return unknownDash
	}
	return t.UTC().Format("2 Jan")
}

// problemHowOften renders the Overview's "how often" column (item 6,
// FIX2): the status and the totals a Problem already carries — "recurring
// · 4 runs in 4 batches since 10 Sep", "regressed on this build · 1 run" —
// replacing the old, silent "hit a/b runs on <newest build>" phrase.
// StatusNew/StatusFirstSeen are, by §8.6's own rule, hit on the newest
// build only, so a batch/since clause would add nothing; every other
// status names how far back it has been seen.
func problemHowOften(p Problem) string {
	runs := fmt.Sprintf("%d run%s", p.RunsTotal, pluralS(p.RunsTotal))
	switch p.Status {
	case StatusNew:
		return fmt.Sprintf("%s on this build · %s", problemStatusLabel(p.Status), runs)
	case StatusFirstSeen:
		return fmt.Sprintf("%s · %s", problemStatusLabel(p.Status), runs)
	default:
		batchesWord := "batches"
		if p.BatchesTotal == 1 {
			batchesWord = "batch"
		}
		return fmt.Sprintf("%s · %s in %d %s since %s",
			problemStatusLabel(p.Status), runs, p.BatchesTotal, batchesWord, fmtDateShort(p.FirstSeen))
	}
}

// buildTopProblems implements §8.3 item 2: the first five live problems
// (§8.6), over the same since window /problems defaults to (30d) — kept in
// sync with problemListSpec's own DefaultSince rather than a second literal.
func (s *Server) buildTopProblems(ctx context.Context, now time.Time) ([]topProblemView, error) {
	window, err := ParseWindow(problemListSpec().DefaultSince)
	if err != nil {
		return nil, fmt.Errorf("console: top problems: %w", err)
	}
	// Item 1 (FIX3): status is computed over the full farm history
	// (allRuns), never just this page's own 30d window — that window only
	// decides inScope (which problems are shown at all).
	allRuns, err := s.allProblemsRuns(ctx)
	if err != nil {
		return nil, err
	}
	inScope := problemsRunIDSet(problemsRunsInWindow(allRuns, window, now))
	problems := BuildProblemsScoped(allRuns, inScope, s.stepTextFinder(ctx))

	var out []topProblemView
	for _, p := range problems {
		if !isLiveStatus(p.Status) {
			continue
		}
		out = append(out, topProblemView{
			ID:       problemAnchorID(p.Key),
			Severity: p.Severity, CauseLabels: p.CauseLabels, Surface: p.Surface, Title: p.Title,
			HowOften: problemHowOften(p),
		})
		if len(out) == 5 {
			break
		}
	}
	return out, nil
}
