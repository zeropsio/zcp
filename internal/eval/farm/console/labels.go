// Package console: this file, labels.go, is the display-vocabulary layer
// (docs/spec-eval-farm.md §8.8 FM-56): one word per concept, the same
// wherever the console shows it, each carrying its one-line definition as a
// badge's title attribute. It also serves GET /terms, the rendered
// glossary. Every value here is a label over data another file resolves
// (view.go's RunRow.Verdict/ObserverState, observer.Finding.Owner/Severity,
// observer.Observation.Outcome) — labels.go adds no new resolution logic of
// its own, and every enum value it knows about is pinned by
// TestLabels_EveryEnumValueHasALabel.
package console

import (
	"net/http"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// verdictStalled is reserved for a run a later slice's RunRow.Stalled flag
// marks as gone past its batch's deadline (§8.8: "stalled — no result
// after the batch's deadline … plus 30 minutes") — nothing in this slice
// ever produces this value; the label exists now so /terms already lists
// the term and verdictLabel/verdictTooltip already handle it once that
// slice starts passing it.
const verdictStalled = "stalled"

// vocabEntry is one (value, label, tooltip) row of a §8.8 vocabulary.
type vocabEntry struct{ value, label, tooltip string }

// verdictVocab is §8.8's verdict vocabulary, in display order.
var verdictVocab = []vocabEntry{
	{farm.VerdictPassed, "passed", "every check held"},
	{farm.VerdictFailed, "failed", "a check proved the run wrong"},
	{farm.VerdictBlocked, "blocked", "could not be graded — reason shown"},
	{farm.VerdictNotRun, "not started", "the run has not started yet"},
	{verdictRunning, "running", "the run is in progress"},
	{verdictStalled, "stalled", "no result after the batch's deadline, plus 30 minutes"},
}

// verdictLabel and verdictTooltip look v up in verdictVocab, falling back
// to v itself (label) or "" (tooltip) for a value this table does not
// know about, so an unrecognized verdict is still shown, never dropped.
func verdictLabel(v string) string   { return vocabLabel(verdictVocab, v) }
func verdictTooltip(v string) string { return vocabTooltip(verdictVocab, v) }

// severityVocab is §8.8's severity vocabulary.
var severityVocab = []vocabEntry{
	{observer.SeverityHigh, "High", "the goal was missed, something was destroyed, or (for a test cause) the verdict is wrong"},
	{observer.SeverityMedium, "Medium", "it cost many steps or much time"},
	{observer.SeverityLow, "Low", "ZCP text or behavior that is wrong but cost this run nothing"},
}

func severityLabel(v string) string   { return vocabLabel(severityVocab, v) }
func severityTooltip(v string) string { return vocabTooltip(severityVocab, v) }

func vocabLabel(vocab []vocabEntry, v string) string {
	for _, e := range vocab {
		if e.value == v {
			return e.label
		}
	}
	return v
}

func vocabTooltip(vocab []vocabEntry, v string) string {
	for _, e := range vocab {
		if e.value == v {
			return e.tooltip
		}
	}
	return ""
}

// causeEntry is one owner value's cause label and cause class (§8.8).
type causeEntry struct{ label, class string }

// causeVocab is §8.8's cause (finding-owner) vocabulary.
var causeVocab = map[string]causeEntry{
	"zcp-guidance": {"ZCP guidance", "ZCP"},
	"zcp-tool":     {"ZCP tool", "ZCP"},
	"platform":     {"Zerops platform", "Platform"},
	"agent":        {"Agent mistake", "Agent"},
	"scenario":     {"Test scenario", "Test"},
	"evaluator":    {"Test check", "Test"},
}

// causeLabel and causeClass look owner (observer.Finding.Owner) up in
// causeVocab, falling back to owner/"" for a value this table does not
// know about.
func causeLabel(owner string) string {
	if e, ok := causeVocab[owner]; ok {
		return e.label
	}
	return owner
}

func causeClass(owner string) string {
	if e, ok := causeVocab[owner]; ok {
		return e.class
	}
	return ""
}

// causeOrder is the ZCP-first display order for the six cause/owner values
// (§8.8: "Lists order causes ZCP first"). It reuses pages_findings.go's
// findingOwners rather than re-declaring the order, so the findings page's
// own filter order and every vocabulary listing (the /terms table, a later
// legend) can never drift apart.
var causeOrder = findingOwners

// outcomeNone is a RunRow-level assessment outcome (§8.8, 2026-09 update):
// unlike observer.OutcomeOK/Problem/Inconclusive — which describe one
// stored observation — "none" describes a run with no *current* ok
// observation to summarize (unobserved, or its current observation
// failed). No observer.Observation ever carries this value; a later
// slice's RunRow.Outcome field does.
const outcomeNone = "none"

// assessmentOutcomeVocab is §8.8's assessment-outcome vocabulary —
// observer.Observation.Outcome (ok/problem/inconclusive), derived, never
// model-authored, plus the RunRow-level "none".
var assessmentOutcomeVocab = []vocabEntry{
	{observer.OutcomeOK, "OK", "the checks and the observer agree the run is fine"},
	{observer.OutcomeProblem, "Problem", "the observer found something worth a maintainer's attention"},
	{observer.OutcomeInconclusive, "Inconclusive", "the observer could not tell either way"},
	{outcomeNone, "none", "no current ok observation to summarize"},
}

func assessmentOutcomeLabel(v string) string { return vocabLabel(assessmentOutcomeVocab, v) }

// assessmentOutcomeClass maps an outcome value onto app.css's .outcome
// badge (item 5's "assessment outcome: its own badge"), falling back to
// the neutral "other" look for a value this table does not know about.
func assessmentOutcomeClass(v string) string {
	switch v {
	case observer.OutcomeOK, observer.OutcomeProblem, observer.OutcomeInconclusive, outcomeNone:
		return "outcome outcome-" + v
	default:
		return "outcome outcome-other"
	}
}

// assessmentStateVocab is §8.8's assessment-state vocabulary, over view.go's
// five observerState values (resolveObserverState's return set).
var assessmentStateVocab = []vocabEntry{
	{observerStateObserved, "assessed", "an observer has read this run and written an assessment"},
	{observerStateObserving, "assessing…", "an observation is queued or running right now"},
	{observerStateNotObserved, "not assessed yet", "run not finished, or waiting for the worker's next pass"},
	{observerStateOff, "not assessed — batch ran without observer", "the batch's manifest names no observer model"},
	{observerStateDisabled, "not assessed — automatic assessment is off on this console", "ZCP_FARM_OBSERVER=off"},
}

func assessmentStateLabel(v string) string   { return vocabLabel(assessmentStateVocab, v) }
func assessmentStateTooltip(v string) string { return vocabTooltip(assessmentStateVocab, v) }

// problemStatusVocab is §8.6/§8.8's problem-status vocabulary (2026-09
// update): new/first-seen/recurring/gone/unconfirmed, in the §8.7 filter's
// own order. The values match the §8.7 `status=` filter's wire spelling
// ("first-seen", hyphenated); MODEL's problems.go (not yet landed) is the
// eventual source of these as Go constants — this table is a plain string
// mirror of the spec text until it does.
var problemStatusVocab = []vocabEntry{
	{"new", "new", "hit on the newest build only, and one of its scenarios was assessed on an older build without hitting it (a regression)"},
	{"first-seen", "first seen", "hit on the newest build only, and none of its scenarios was assessed on an older build"},
	{"recurring", "recurring", "hit on the newest build and on an older one"},
	{"gone", "gone", "not hit on the newest build although one of its scenarios was assessed there, and hit on an older build"},
	{"unconfirmed", "unconfirmed", "not hit on the newest build and none of its scenarios was assessed there"},
}

func problemStatusLabel(v string) string   { return vocabLabel(problemStatusVocab, v) }
func problemStatusTooltip(v string) string { return vocabTooltip(problemStatusVocab, v) }

// problemStatusLive reports whether v is one of the "live" statuses (§8.6:
// "live = recurring, new or first seen").
func problemStatusLive(v string) bool {
	return v == "new" || v == "first-seen" || v == "recurring"
}

// glossaryTerm is one row of §8.8 FM-56's full glossary, for GET /terms.
type glossaryTerm struct{ Term, Definition string }

// glossaryTerms transcribes §8.8's own prose, one row per defined term, in
// the spec's own order.
var glossaryTerms = []glossaryTerm{
	{"Batch", "One farm run: a set of scenarios against one ZCP build."},
	{"Evaluation batch", "A batch where at least one run finished."},
	{"Empty batch", "A batch where no run finished (setup failures, aborted, stalled)."},
	{"Run", "One scenario done once by an agent in a fresh project."},
	{"Scenario", "A scripted user task plus the automatic checks that grade it."},
	{"ZCP build", "The candidate binary, identified by its sha256; shown as its git commit (12 chars, + modified when built from a dirty tree) when the manifest records it, else build <sha256[:12]>. The label is display only; two binaries are two builds even at one commit."},
	{"Verdict", "The automatic checks' result, never the observer's — see the table below."},
	{"Check", "One automatic test: expected, observed, where the observed value came from."},
	{"Observer", "An AI model that reads a finished run and writes an assessment; it never changes the verdict."},
	{"Assessment", "What the observer writes about a run: an outcome and, when there is a problem, findings."},
	{"Goal reached", "Did the user get what they asked for, whatever the checks say."},
	{"Verdict right", "Did the checks judge correctly."},
	{"Disputed", "The current observation has checks.agree: false: a check judged wrong, or a check the observer says is missing; counted under the verdict it disputes, passed included."},
	{"Self-review honest", "Does the agent's after-run summary match the record."},
	{"Finding", "One problem in one run, with quotes, where to look and a fix."},
	{"Problem", "The same finding across runs — see the problem status table below."},
	{"Severity", "High, medium or low — see the table below."},
	{"Cause", "The finding's owner — see the table below."},
	{"Cause class", "ZCP (guidance, tool) · Test (scenario, check) · Agent · Platform. Lists order causes ZCP first."},
	{"Surface", "The part of ZCP (or the farm) a finding sits in."},
	{"Anchor", "The exact ZCP text or error code to search for."},
	{"Quote found", "The quoted words occur in the cited step; it does not prove the finding right."},
	{"Agent cost", "The run's model spend, without the observer; — when not recorded."},
}

// vocabRow is one rendered row of a /terms vocabulary table: the raw
// value, its label, and its tooltip (empty when the vocabulary carries
// none — /terms only sets a title attribute when Tooltip is non-empty).
type vocabRow struct{ Value, Label, Tooltip string }

// termsPageData is GET /terms (§8.3 FM-51's glossary page).
type termsPageData struct {
	Meta            pageMeta
	Glossary        []glossaryTerm
	Verdicts        []vocabRow
	Severities      []vocabRow
	Causes          []vocabRow
	Outcomes        []vocabRow
	States          []vocabRow
	ProblemStatuses []vocabRow
}

func vocabRows(vocab []vocabEntry) []vocabRow {
	rows := make([]vocabRow, len(vocab))
	for i, e := range vocab {
		rows[i] = vocabRow{Value: e.value, Label: e.label, Tooltip: e.tooltip}
	}
	return rows
}

// handleTermsPage serves GET /terms: §8.8's glossary, plus every enum
// value this file knows a label for (TestLabels_EveryEnumValueHasALabel
// pins that the set below is exhaustive).
func (s *Server) handleTermsPage(w http.ResponseWriter, r *http.Request) {
	data := termsPageData{
		Meta:            s.pageMeta(r, "Terms", navTerms, false),
		Glossary:        glossaryTerms,
		Verdicts:        vocabRows(verdictVocab),
		Severities:      vocabRows(severityVocab),
		Outcomes:        vocabRows(assessmentOutcomeVocab),
		States:          vocabRows(assessmentStateVocab),
		ProblemStatuses: vocabRows(problemStatusVocab),
	}
	for _, owner := range causeOrder {
		data.Causes = append(data.Causes, vocabRow{Value: owner, Label: causeLabel(owner), Tooltip: causeClass(owner) + " cause"})
	}
	renderPage(w, "terms", data)
}
