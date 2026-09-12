// Package console: this file, pages_run.go, owns GET /r/<runId> (§8.3
// FM-51's run page) — split out of pages.go (S-SHELL item 1) so each page
// owns one file.
package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// runPageData is GET /r/<runId> (§8.3 FM-51's seven-part page).
type runPageData struct {
	Meta pageMeta
	Row  RunRow

	// VerdictDisplay/VerdictReasonText are part 1's verdict badge and its
	// reason (runVerdictDisplay/runVerdictReasonText): the "stalled"
	// sentinel substituted for a running row past its budget, plus a
	// reason line for blocked/not-started/running/stalled.
	VerdictDisplay    string
	VerdictReasonText string

	// HasCard is true iff DisplayedObs is non-nil with status "ok" — the
	// only case with an outcome/headline/story/findings to show (§7.5:
	// "none when unobserved or failed"). DisplayedObs is row.Observation
	// unless ?obs= names a different one of the run's own observations
	// (§8.3: "renders that stored version in the card"); ViewingOlder
	// marks that case for the "showing an earlier assessment" note.
	HasCard      bool
	DisplayedObs *observer.Observation
	ViewingOlder bool
	OutcomeText  string
	Story        *storyView
	SectionLinks []runSectionLinkView

	// FailedNewestText is item 1 (FIX2)'s own banner: non-empty exactly
	// when the run's current (newest) observation failed and an older ok
	// one is shown in the card's place — "" (including every ?obs= view,
	// which asked for one exact version and gets exactly that) leaves the
	// card's usual ViewingOlder banner as the only one that can render.
	FailedNewestText string
	FailedNewestHref string

	Disputed     bool
	DisputedWhy  string
	DisputedHref string

	Findings         []findingView
	UnverifiedQuotes int
	TotalQuotes      int
	// QuotesFoundText is item 11 (FIX2)'s own footer wording, "quotes
	// found <found>/<total>" — replacing "<n> of <m> quotes unverified".
	QuotesFoundText string

	// WhyNoCard/RawAnswer/LastAgentMessage/ToolErrors render in HasCard's
	// place: the §8.8 reason DisplayedObs has no usable assessment, the
	// unparsed answer's capped preview, and "How the run ended" (last
	// agent message, tool errors with step links).
	WhyNoCard string
	// WhyNoCardFailed is FIX3 item 6's own flag: true exactly when
	// WhyNoCard describes an observation whose assessment ATTEMPT itself
	// failed (status error/unparsed) — current or a viewed-older one —
	// so the page can show the "failed" outcome chip and the "nothing is
	// retried automatically" note instead of a bare, unlabeled sentence.
	WhyNoCardFailed  bool
	RawAnswer        string
	LastAgentMessage string
	ToolErrors       []toolErrorView

	CheckRows []checkRowView
	// JudgedChecks is part 3's "Verdict right" list (FIX2 item 5): one
	// line per judged check, its id soft-wrapped.
	JudgedChecks []judgedCheckView

	LiveStatusText      string
	PreStoreFailureText string
	ShowAssessForm      bool
	ModelOptions        []modelOptionView

	OlderObservations []olderObsView

	Steps            []stepView
	StepsError       string
	VisibleStepCount int
	StepsFiltered    bool
	StepsAllURL      string
	// StepsSuppressed is item 2 (FIX2)'s own dedup: true when Steps failed
	// to load for the exact same reason RecordError already said once —
	// the section then renders neither a filter bar nor a second copy of
	// the same sentence.
	StepsSuppressed bool
	StepsFilters    FilterBarView

	TaskPrompt  string
	SelfReview  string
	RecordError string
	// RecordErrorDetail is item 2's own forensic-block detail: the raw
	// error chain behind RecordError, rendered once more (as <dt>Error</dt>)
	// inside the "Run metadata & forensic view" disclosure — "" unless
	// RecordError is set.
	RecordErrorDetail string
	EvidenceWarning   string
}

type runSectionLinkView struct {
	Href, Label, Count string
}

// findingView is one Findings-section entry (§8.3 part 4): N is its
// 1-based display index ("F<n>", id="f<n>") — computed once here so the
// template never needs template-side arithmetic.
type findingView struct {
	N int
	observer.Finding
	EvidenceViews []findingEvidenceView
}

type findingEvidenceView struct {
	observer.Evidence
	Target stepTargetView
}

type storyView struct {
	Task, Expected, Did, Ending string
	Stuck                       *stuckView
}

type stuckView struct {
	From, To stepTargetView
	What     string
}

// checkRowView is one Failed-and-blocked-checks row (§8.3 part 5): the
// check itself plus the observer's judgement of it, when the current
// observation judged it (§7.5 checks.judged).
type checkRowView struct {
	FailedCheck
	Judged *observer.JudgedCheck
	// Anchor is this row's HTML fragment id (checkAnchor) — precomputed
	// once so every href="#..." pointing at this row and the row's own
	// id="..." always agree.
	Anchor string
	// Expected/Observed shadow FailedCheck's own fields of the same name
	// (Go embedding: an outer field wins over a promoted one) with item
	// 19's monotonic-clock text normalized — every template site that
	// reads .Expected/.Observed off a checkRowView (the checks table and
	// the Why-this-verdict strip, both built from the same CheckRows
	// slice) gets the normalized text for free, with nothing left to drift.
	Expected, Observed string
	// IDWrapped is FailedCheck.ID with a soft line-break opportunity
	// (softWrapID) after every "/ . - _" — item 5: the checks table wraps
	// a long id only at those characters, never mid-word. The
	// Why-this-verdict strip still prints the plain .ID (a shorter
	// context, and already inside a link).
	IDWrapped string
	// WhyPrefix/WhyText are the Why-this-verdict line's own two halves
	// (FIX2 item 4): WhyPrefix names the check's own result ("Failed
	// because" / "Blocked" — never "Failed because" for a check that was
	// merely blocked), and WhyText is "expected X, got Y", or — when a
	// blocked check carries neither (it could not even attempt to grade)
	// — "could not run (source <source>)" instead of two empty values.
	WhyPrefix, WhyText string
}

// judgedCheckView is one "Verdict right" line (§8.3 part 3, FIX2 item 5):
// the observer's own judgement plus its id soft-wrapped (softWrapID) so a
// long one gets a wrap opportunity instead of clipping.
type judgedCheckView struct {
	observer.JudgedCheck
	IDWrapped string
}

func buildJudgedCheckViews(judged []observer.JudgedCheck) []judgedCheckView {
	out := make([]judgedCheckView, len(judged))
	for i, j := range judged {
		out[i] = judgedCheckView{JudgedCheck: j, IDWrapped: softWrapID(j.ID)}
	}
	return out
}

// zeroWidthSpace (U+200B) is a soft line-break opportunity with no visible
// mark — softWrapID's own building block.
const zeroWidthSpace = '​'

// softWrapID inserts a zero-width space (U+200B) after every "/", ".", "-"
// and "_" in id — a soft line-break opportunity a browser can wrap at
// without ever showing anything, so a long check id (item 5's failed-checks
// table) wraps only at those characters instead of mid-word. Plain text,
// not markup: the result still goes through html/template's normal
// auto-escaping like any other field.
func softWrapID(id string) string {
	var b strings.Builder
	for _, r := range id {
		b.WriteRune(r)
		switch r {
		case '/', '.', '-', '_':
			b.WriteRune(zeroWidthSpace)
		}
	}
	return b.String()
}

// checkAnchor turns a check id into a safe HTML fragment identifier. A
// check id can embed a "/" (e.g. "decision/x") — html/template's
// contextual autoescaping percent-encodes that inside an href="#..." URL
// context but leaves it untouched in a plain id="..." attribute, so the
// same raw id used in both places would mismatch (TestPages_
// RunInPageLinksResolve). Every character outside [A-Za-z0-9_-] maps to
// "-", so an anchor built with this and used on both sides always agrees.
func checkAnchor(id string) string {
	var b strings.Builder
	b.WriteString("check-")
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// olderObsView is one "earlier assessments" row: its own outcome/headline
// or failure text (never empty, §8.8) and the ?obs= link that renders it.
type olderObsView struct {
	ObsID     string
	Model     string
	CreatedAt time.Time
	Outcome   string
	Text      string
	Href      string
	Selected  bool
}

// toolErrorView is one "How the run ended" tool-error line.
type toolErrorView struct {
	Step   int
	Tool   string
	Result string
	Target stepTargetView
}

// stepCitation is one finding's evidence entry rendered inside the step it
// cites (§8.3 part 6's "Cited by F<n>" callout).
type stepCitation struct {
	FindingN int
	Title    string
	Quote    string
}

// stepView is one Steps-section row: the record step plus its tool
// result's JSON escapes decoded (§8.3: "tool results decoded") and the
// findings citing it.
type stepView struct {
	observer.Step
	DecodedResult string
	DisplayInput  string
	DisplayResult string
	Preview       string
	Citations     []stepCitation
}

// modelOptionView is one entry of the Assess form's model picker.
type modelOptionView struct {
	Value    string
	Label    string
	Selected bool
}

// modelDisplayLabels names the allowlisted models the way the Assess form
// shows them (review finding: "Sonnet 5 (default)", "Opus 5 (stronger)",
// "Fable 5.1 (strongest)") — display text only; observer.Models/DefaultModel
// stay the identity the server actually validates against.
var modelDisplayLabels = map[string]string{
	observer.DefaultModel: "Sonnet 5 (default)",
	"claude-opus-5":       "Opus 5 (stronger)",
	"claude-fable-5-1":    "Fable 5.1 (strongest)",
}

func modelLabel(v string) string {
	if l, ok := modelDisplayLabels[v]; ok {
		return l
	}
	return v
}

// buildModelOptions renders the picker preselected to preselect (empty
// defaults to observer.DefaultModel).
func buildModelOptions(preselect string) []modelOptionView {
	if preselect == "" {
		preselect = observer.DefaultModel
	}
	out := make([]modelOptionView, 0, len(observer.Models))
	for _, m := range observer.Models {
		out = append(out, modelOptionView{Value: m, Label: modelLabel(m), Selected: m == preselect})
	}
	return out
}

func (s *Server) handleRunPage(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/r/")
	if !farm.ValidRunID(runID) {
		s.renderNotFound(w, r, "Run not found", "This run is no longer available.", "Back to overview", "/")
		return
	}

	q, err := Parse(runStepsListSpec(), r.URL.Query())
	if err != nil {
		var qerr *QueryError
		if !errors.As(err, &qerr) {
			qerr = &QueryError{}
		}
		s.renderBadQuery(w, r, "", qerr)
		return
	}

	ctx := r.Context()
	row, err := loadRunRow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState, s.runCache, s.summaryCache)
	if err != nil {
		s.renderRunError(w, r, err)
		return
	}

	older, err := loadOlderObservations(ctx, s.cfg.Store, runID, row.OlderObsIDs)
	if err != nil {
		s.renderRunError(w, r, err)
		return
	}

	displayedObs, viewingOlder, failedNewestText, failedNewestHref, ok := resolveDisplayedObs(row, older, q)
	if !ok {
		s.renderNotFound(w, r, "Assessment not found", "This stored assessment does not belong to the run or is no longer available.", "Back to current run", "/r/"+runID)
		return
	}

	busy := runQueued(s.queueState, runID)
	preselectModel := observer.DefaultModel
	if row.Observation != nil {
		preselectModel = row.Observation.Model
	}

	data := runPageData{
		Meta:              s.pageMeta(r, row.Scenario, "", busy),
		Row:               row,
		VerdictDisplay:    runVerdictDisplay(row),
		VerdictReasonText: runVerdictReasonText(row),
		DisplayedObs:      displayedObs,
		ViewingOlder:      viewingOlder,
		FailedNewestText:  failedNewestText,
		FailedNewestHref:  failedNewestHref,
		OlderObservations: buildOlderObsViews(runID, older, q.Obs),
		ModelOptions:      buildModelOptions(preselectModel),
	}
	data.ShowAssessForm = row.DoneExists && !assessmentWorkUnavailable(row) && runDidWork(row) && !data.Meta.Observer.Hidden && !busy
	data.LiveStatusText, data.PreStoreFailureText = s.runLiveStatus(runID)

	judged := populateAssessmentCard(&data, row, displayedObs, viewingOlder)
	data.CheckRows = buildCheckRows(row.FailedChecks, judged)
	data.JudgedChecks = buildJudgedCheckViews(judged)

	// Item 4 (FIX2): drop the page-head reason line when it would only
	// repeat the bare check ids the Why-this-verdict/checks-table lines
	// below already itemize in full (each with its own expected/observed) —
	// blockedCheckIDsReason is verdictReason's own id-list fallback, so an
	// exact match means that fallback fired rather than a real summary
	// detail/error worth keeping.
	if len(row.FailedChecks) > 0 && data.VerdictReasonText == blockedCheckIDsReason(row.Verdict, row.FailedChecks) {
		data.VerdictReasonText = ""
	}

	if row.DoneExists {
		s.populateRecordAndSteps(ctx, &data, runID, q, r.URL.Query(), displayedObs)
	}
	if data.FailedNewestHref != "" && row.Observation != nil {
		data.FailedNewestHref = listURL("/r/"+runID, r.URL.Query(), map[string]string{"obs": row.Observation.ObsID})
	}
	data.SectionLinks = buildRunSectionLinks(data)

	renderPage(w, "run", data)
}

// resolveDisplayedObs implements §8.3/§8.7's ?obs=<obsId> resolution (a
// missing or foreign obsId is rejected — ok is false, the caller answers
// 404) plus item 1 (FIX2)'s own fallback: absent ?obs=, a failed current
// observation is replaced by the newest ok one, with failedNewestText
// naming the failed attempt for the banner above the substituted card.
func resolveDisplayedObs(row RunRow, older []observer.Observation, q Query) (obs *observer.Observation, viewingOlder bool, failedNewestText, failedNewestHref string, ok bool) {
	if q.Obs != "" {
		found, foundOK := findObservation(row, older, q.Obs)
		if !foundOK {
			return nil, false, "", "", false
		}
		viewingOlder = row.Observation == nil || found.ObsID != row.Observation.ObsID
		return found, viewingOlder, "", "", true
	}

	obs = row.Observation
	if obs != nil && observationFailed(obs) {
		if okObs, found := findNewestOK(older); found {
			failedNewestText = fmt.Sprintf("The newest assessment (%s, %s) failed: %s; showing %s, %s.",
				fmtTime(obs.CreatedAt), obs.Model, assessmentFailureReason(obs),
				fmtTime(okObs.CreatedAt), okObs.Model)
			failedNewestHref = "/r/" + row.RunID + "?obs=" + url.QueryEscape(obs.ObsID)
			obs = okObs
		}
	}
	return obs, false, failedNewestText, failedNewestHref, true
}

// populateAssessmentCard implements §8.3 part 3 (the Assessment card) and
// its "no card" fallback, filling data in place and returning the judged
// checks the checks table/Verdict-right list both need.
func populateAssessmentCard(data *runPageData, row RunRow, displayedObs *observer.Observation, viewingOlder bool) []observer.JudgedCheck {
	switch {
	case displayedObs != nil && displayedObs.Status == observationStatusOK:
		data.HasCard = true
		findings := buildFindingViews(displayedObs.Findings)
		data.Findings = findings
		data.UnverifiedQuotes = unverifiedQuotes(displayedObs)
		for _, f := range displayedObs.Findings {
			data.TotalQuotes += len(f.Evidence)
		}
		data.QuotesFoundText = fmt.Sprintf("quotes found %d/%d", data.TotalQuotes-data.UnverifiedQuotes, data.TotalQuotes)
		if computeDisputed(displayedObs) {
			data.Disputed = true
			data.DisputedWhy, data.DisputedHref = disputedInfo(displayedObs, findings)
		}
		outcome := displayedObs.EffectiveOutcome()
		if outcome == "" {
			outcome = outcomeNone
		}
		data.OutcomeText = outcome
		return displayedObs.Checks.Judged
	case viewingOlder && displayedObs != nil:
		// A specific non-ok observation was asked for by id: say why that
		// one has no card, not why the run's current state does. It is
		// always a failed attempt (the ok case is HasCard, above).
		data.WhyNoCard = "assessment failed — " + assessmentFailureReason(displayedObs)
		data.WhyNoCardFailed = true
		if displayedObs.Status == observationStatusUnparsed {
			data.RawAnswer = capRaw(displayedObs.Raw)
		}
	default:
		// row.Observation (nil or failed) — its own §8.8 wording already
		// distinguishes every case, including "assessment failed — <reason>".
		data.WhyNoCard = row.ObserverStateText
		data.WhyNoCardFailed = row.Observation != nil && observationFailed(row.Observation)
		if displayedObs != nil && displayedObs.Status == observationStatusUnparsed {
			data.RawAnswer = capRaw(displayedObs.Raw)
		}
	}
	return nil
}

// populateRecordAndSteps fills data's Record (§8.3 part 7) and Steps
// (part 6) sections for a run with done.json — split out of
// handleRunPage so that function's own branching stays within budget.
// Item 2 (FIX2): a missing bundle file (live bugs /r/gate5-…, /r/gate1-…)
// maps to one plain sentence instead of a raw Go error chain — the chain
// itself still reaches the page, once, in the "Run metadata & forensic
// view" block (RecordErrorDetail); Steps says nothing more when its own
// failure shares that exact root cause (StepsSuppressed).
func (s *Server) populateRecordAndSteps(ctx context.Context, data *runPageData, runID string, q Query, rawQuery url.Values, displayedObs *observer.Observation) {
	rawSteps, stepsErr := loadSteps(ctx, s.cfg.Store, runID)
	targets := newStepTargetIndex(runID, rawQuery, nil, nil)

	taskPrompt, selfReview, textsErr := loadRunTexts(ctx, s.cfg.Store, runID)
	switch {
	case textsErr != nil && isBundleNotFound(textsErr):
		data.RecordError = bundleNotFoundSentence
		data.EvidenceWarning = bundleNotFoundSentence
		data.RecordErrorDetail = textsErr.Error()
	case textsErr != nil:
		data.RecordError = "record unavailable — " + textsErr.Error()
		data.EvidenceWarning = data.RecordError
		data.RecordErrorDetail = textsErr.Error()
	default:
		data.TaskPrompt, data.SelfReview = taskPrompt, selfReview
	}

	switch {
	case stepsErr == nil:
		visibleSteps := visibleEvidenceSteps(rawSteps)
		cited := map[int]bool{}
		for _, n := range evidenceSteps(displayedObs) {
			cited[n] = true
		}
		mode := stepsFilterMode(q)
		filtered := FilterSteps(visibleSteps, mode, cited)
		data.Steps = buildStepViews(filtered, buildStepCitations(data.Findings))
		data.VisibleStepCount = len(filtered)
		data.StepsFiltered = mode != filterAll
		data.StepsAllURL = listURL("/r/"+runID, rawQuery, map[string]string{"steps": filterAll})
		targets = newStepTargetIndex(runID, rawQuery, visibleSteps, filtered)
		// The run step choices are mutually exclusive views. The shared
		// parser accepts the historic closed-filter syntax, but links from
		// this page replace the view so choosing Errors while on Cited can
		// never leave an inert "cited,errors" URL behind.
		navSpec := runStepsListSpec()
		navSpec.Closed[0].Single = true
		nav := buildListNav("/r/"+runID, navSpec, q, rawQuery, stepsFilterCounts(visibleSteps, cited), nil, stepsFilterLabeler)
		data.StepsFilters = nav.Filters
		if !data.HasCard {
			data.LastAgentMessage = lastAgentMessageText(rawSteps)
			data.ToolErrors = toolErrorViews(rawSteps)
		}
	case isBundleNotFound(stepsErr) && data.RecordError == bundleNotFoundSentence:
		// Same root cause, already said once under Record — nothing more
		// to say here.
		data.StepsSuppressed = true
	case isBundleNotFound(stepsErr):
		data.StepsError = bundleNotFoundSentence
		if data.EvidenceWarning == "" {
			data.EvidenceWarning = bundleNotFoundSentence
		}
	default:
		// §8.3: "a run with no done.json or no task prompt renders the
		// header and its reason — never a 502" (live bug:
		// /r/gate5-resume-after-compaction). A corrupt/partial bundle
		// degrades this section instead of failing the whole page.
		data.StepsError = "steps unavailable — " + stepsErr.Error()
		if data.EvidenceWarning == "" {
			data.EvidenceWarning = data.StepsError
		}
	}
	populateEvidenceTargets(data, displayedObs, targets)
	for i := range data.ToolErrors {
		data.ToolErrors[i].Target = targets.target(data.ToolErrors[i].Step)
	}
}

func buildRunSectionLinks(data runPageData) []runSectionLinkView {
	links := make([]runSectionLinkView, 0, 6)
	if data.Row.DoneExists {
		links = append(links, runSectionLinkView{Href: "#automatic-checks", Label: "Automatic checks"})
	}
	links = append(links, runSectionLinkView{Href: "#observation", Label: "Assessment"})
	if len(data.Findings) > 0 {
		links = append(links, runSectionLinkView{Href: "#findings", Label: "Findings", Count: fmtInt(len(data.Findings))})
	}
	if len(data.CheckRows) > 0 {
		links = append(links, runSectionLinkView{Href: "#failed-checks", Label: "Checks", Count: fmtInt(len(data.CheckRows))})
	}
	if data.Row.DoneExists {
		links = append(links,
			runSectionLinkView{Href: "#steps", Label: "Steps", Count: fmtInt(data.VisibleStepCount)},
			runSectionLinkView{Href: "#record", Label: "Record"},
		)
	}
	return links
}

func populateEvidenceTargets(data *runPageData, obs *observer.Observation, targets stepTargetIndex) {
	for i := range data.Findings {
		finding := &data.Findings[i]
		finding.EvidenceViews = make([]findingEvidenceView, len(finding.Evidence))
		for j, evidence := range finding.Evidence {
			finding.EvidenceViews[j] = findingEvidenceView{Evidence: evidence, Target: targets.target(evidence.Step)}
		}
	}
	if obs == nil || obs.Story == nil {
		return
	}
	story := obs.Story
	data.Story = &storyView{Task: story.Task, Expected: story.Expected, Did: story.Did, Ending: story.Ending}
	if story.Stuck != nil {
		data.Story.Stuck = &stuckView{From: targets.target(story.Stuck.From), To: targets.target(story.Stuck.To), What: story.Stuck.What}
	}
}

// runVerdictDisplay substitutes labels.go's reserved "stalled" sentinel for
// a running row past its budget grace period (RunRow.Stalled) — the raw
// Verdict field itself stays verdictRunning (view.go's isStalled doc
// comment), so every display site (badge class/icon/label/tooltip) must
// apply this substitution rather than reading Row.Verdict directly.
func runVerdictDisplay(row RunRow) string {
	if row.Verdict == verdictRunning && row.Stalled {
		return verdictStalled
	}
	return row.Verdict
}

// runVerdictReasonText is part 1's "with its reason" fact: the resolved
// per-run detail (verdictReason, for blocked/not-started) when there is
// one, else the vocabulary's own definition for a state with no per-run
// detail to show (running, stalled) — never a reason for passed (nothing
// to explain) or failed (its reason is "Why this verdict", part 2).
func runVerdictReasonText(row RunRow) string {
	if row.VerdictReason != "" {
		return row.VerdictReason
	}
	switch runVerdictDisplay(row) {
	case verdictRunning, verdictStalled:
		return vocabTooltip(verdictVocab, runVerdictDisplay(row))
	default:
		return ""
	}
}

// bundleNotFoundSentence is item 2 (FIX2)'s one plain sentence for a run
// whose bundle carries no results/ directory, or is missing its task
// prompt or transcript — the shape behind the live bugs at
// /r/gate5-resume-after-compaction and /r/gate1-… (a raw Go error chain
// reaching the page). Any other read failure (a genuine store error) is
// left as its own message: only "there is nothing here" is safe to soften
// into one sentence — the raw chain still reaches the page via
// RecordErrorDetail, in the "Run metadata & forensic view" block.
const bundleNotFoundSentence = "This run left no task prompt or transcript in its bundle."

// isBundleNotFound reports whether err is the "nothing here" shape
// bundleNotFoundSentence describes: observer.ResultsDir's own sentinel
// (no results/ dir at all) or a plain missing-file error from reading one
// of its files (task-prompt.txt, transcript.jsonl) — both wrapped with
// %w by loadRunTexts/loadSteps, so errors.Is still sees through the chain.
func isBundleNotFound(err error) bool {
	return errors.Is(err, observer.ErrResultsNotFound) || errors.Is(err, os.ErrNotExist)
}

// findObservation resolves ?obs=<obsId> against runID's own observations —
// the current one plus every older version, already loaded for the
// "earlier assessments" list — never a bare store fetch, so an id
// belonging to a different run is rejected exactly like one that never
// existed (§8.3: "a missing or foreign obsId → 404").
func findObservation(row RunRow, older []observer.Observation, obsID string) (*observer.Observation, bool) {
	if row.Observation != nil && row.Observation.ObsID == obsID {
		return row.Observation, true
	}
	for i := range older {
		if older[i].ObsID == obsID {
			return &older[i], true
		}
	}
	return nil, false
}

// findNewestOK returns the newest observation with status ok among older
// (view.go's RunRow.OlderObsIDs is newest-last, and loadOlderObservations
// resolves them in that same order) — item 1's fallback target when the
// run's current observation failed.
func findNewestOK(older []observer.Observation) (*observer.Observation, bool) {
	for i := len(older) - 1; i >= 0; i-- {
		if older[i].Status == observationStatusOK {
			return &older[i], true
		}
	}
	return nil, false
}

func buildFindingViews(findings []observer.Finding) []findingView {
	out := make([]findingView, len(findings))
	for i, f := range findings {
		out[i] = findingView{N: i + 1, Finding: f}
	}
	return out
}

// disputedInfo implements the review finding "the disputed line linking to
// the explaining finding or the judged check's row": the first
// evaluator-owned finding when there is one (§7.5: the finding that says a
// check is wrong or missing), else the first incorrect judged check, else —
// format 1, which carries neither — the stored checks.why with no link.
func disputedInfo(obs *observer.Observation, findings []findingView) (why, href string) {
	for _, f := range findings {
		if f.Owner == "evaluator" {
			return f.What, fmt.Sprintf("#f%d", f.N)
		}
	}
	for _, j := range obs.Checks.Judged {
		if !j.Correct {
			return j.Why, "#" + checkAnchor(j.ID)
		}
	}
	return obs.Checks.Why, ""
}

// checkWhyPrefix is the Why-this-verdict line's own result word (FIX2 item
// 4): "Blocked" for a check that could not be graded, "Failed because" for
// one that ran and proved the run wrong — never "Failed because" for both,
// which mislabeled a blocked check as failed.
func checkWhyPrefix(result string) string {
	if result == farm.VerdictBlocked {
		return "Blocked"
	}
	return "Failed because"
}

// checkWhyText is the Why-this-verdict line's own body (FIX2 item 4):
// "expected X, got Y" normally, or — when a blocked check carries neither
// (it could not even attempt to grade) — "could not run (source <source>)"
// instead of two empty values.
func checkWhyText(source, expected, observed string) string {
	if expected == "" && observed == "" {
		return fmt.Sprintf("could not run (source %s)", source)
	}
	return fmt.Sprintf("expected %s, got %s", expected, observed)
}

func buildCheckRows(failed []FailedCheck, judged []observer.JudgedCheck) []checkRowView {
	byID := make(map[string]*observer.JudgedCheck, len(judged))
	for i := range judged {
		byID[judged[i].ID] = &judged[i]
	}
	out := make([]checkRowView, len(failed))
	for i, c := range failed {
		expected, observed := formatCheckValue(c.Expected), formatCheckValue(c.Observed)
		out[i] = checkRowView{
			FailedCheck: c, Judged: byID[c.ID], Anchor: checkAnchor(c.ID),
			Expected: expected, Observed: observed,
			IDWrapped: softWrapID(c.ID),
			WhyPrefix: checkWhyPrefix(c.Result), WhyText: checkWhyText(c.Source, expected, observed),
		}
	}
	return out
}

// buildOlderObsViews renders row.OlderObsIDs' full documents (already
// loaded) as the "earlier assessments" list: outcome and headline, or —
// review finding — the failure text in its place, never an empty line.
func buildOlderObsViews(runID string, older []observer.Observation, selectedObsID string) []olderObsView {
	out := make([]olderObsView, len(older))
	for i := range older {
		o := &older[len(older)-1-i]
		text := o.Headline
		outcome := o.EffectiveOutcome()
		switch {
		case o.Status != observationStatusOK:
			// FIX3 item 6: this version's own attempt failed — "failed",
			// never the misleading "none" ("no current ok observation").
			text = assessmentFailureReason(o)
			outcome = outcomeFailed
		case outcome == "":
			outcome = outcomeNone
		}
		out[i] = olderObsView{
			ObsID: o.ObsID, Model: o.Model, CreatedAt: o.CreatedAt,
			Outcome: outcome, Text: text,
			Href:     "/r/" + runID + "?obs=" + url.QueryEscape(o.ObsID),
			Selected: o.ObsID == selectedObsID,
		}
	}
	return out
}

// runLiveStatus implements §8.5's run-card live text: a queued/running job
// ("Assessing with <model> — started <time>, usually 1–2 min; the result
// replaces the one below"), else the last pre-store failure ("The attempt
// at <time> failed before anything was stored: <error>. Re-assess to
// retry."), else neither. A queued job has no StartedAt yet, so it shows
// its EnqueuedAt instead.
func (s *Server) runLiveStatus(runID string) (live, failure string) {
	if s.cfg.Queue == nil {
		return "", ""
	}
	if info, ok := s.cfg.Queue.Job(runID); ok {
		return formatQueuedJobText(info), ""
	}
	if f, ok := s.cfg.Queue.LastFailure(runID); ok {
		return "", fmt.Sprintf("The attempt at %s failed before anything was stored: %s. Re-assess to retry.", fmtTime(f.At), f.Err)
	}
	return "", ""
}

const lastAgentMessageCap = 300

// lastAgentMessageText is "How the run ended"'s own text: the last
// kind=agent step, capped at 300 chars (§8.3).
func lastAgentMessageText(steps []observer.Step) string {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Kind == observer.StepAgent {
			return capRunes(steps[i].Text, lastAgentMessageCap)
		}
	}
	return ""
}

func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// toolErrorViews is "How the run ended"'s tool-error list: every kind=tool
// step whose result was an error, result decoded like every other tool
// result on this page (§8.3).
func toolErrorViews(steps []observer.Step) []toolErrorView {
	var out []toolErrorView
	for _, st := range steps {
		if st.Kind == observer.StepTool && st.ToolIsError {
			out = append(out, toolErrorView{Step: st.N, Tool: st.ToolName, Result: observer.DecodeJSONEscapes(st.ToolResultText)})
		}
	}
	return out
}

// buildStepCitations maps a step number to every finding whose evidence
// cites it (§8.3 part 6's "Cited by F<n>" callout) — step 0 (the CHECKS
// citation) is never a real step, so it is excluded here.
func buildStepCitations(findings []findingView) map[int][]stepCitation {
	m := map[int][]stepCitation{}
	for _, f := range findings {
		for _, e := range f.Evidence {
			if e.Step <= 0 {
				continue
			}
			m[e.Step] = append(m[e.Step], stepCitation{FindingN: f.N, Title: f.Title, Quote: e.Quote})
		}
	}
	return m
}

func buildStepViews(steps []observer.Step, citations map[int][]stepCitation) []stepView {
	out := make([]stepView, len(steps))
	for i, st := range steps {
		decoded := observer.DecodeJSONEscapes(st.ToolResultText)
		out[i] = stepView{
			Step: st, DecodedResult: decoded, DisplayInput: displayToolInput(st.ToolInputJSON),
			DisplayResult: displayToolResult(st.ToolResultText), Preview: decoded, Citations: citations[st.N],
		}
	}
	return out
}

// stepsFilterMode reads the run-steps list's one closed filter (§8.7:
// "steps=all|cited|errors") — Parse's Defaults already guarantee a value.
func stepsFilterMode(q Query) string {
	if v := q.Closed["steps"]; len(v) > 0 {
		return v[0]
	}
	return filterAll
}

// stepsAllowedValues returns the steps= filter's own allowed values
// (view.go's runStepsListSpec, in its own declared order: all, cited,
// errors) — read from that spec rather than repeated here as literals, so
// "cited"/"errors" each stay at their one declaration site (goconst).
func stepsAllowedValues() []string {
	cf, _ := runStepsListSpec().closedFilter("steps")
	return cf.Allowed
}

// stepsFilterDisplay pairs stepsAllowedValues()'s values with this page's
// display text, by position — index 0 is "all", 1 "cited", 2 "errors" per
// that spec's own order, never compared against a repeated literal here.
var stepsFilterDisplay = []struct{ Label, Title string }{
	{"All", ""},
	{"Cited", "steps some finding's evidence cites"},
	{"Errors", "tool steps whose result was an error"},
}

func stepsFilterLabelFor(v string) (label, title string) {
	for i, a := range stepsAllowedValues() {
		if a != v {
			continue
		}
		if i < len(stepsFilterDisplay) {
			return stepsFilterDisplay[i].Label, stepsFilterDisplay[i].Title
		}
		return v, ""
	}
	return v, ""
}

// stepsFilterCounts computes the steps= filter bar's per-option counts
// (§8.7: "each filter option shows its count") over the run's own, full
// step list — never the already-filtered one.
func stepsFilterCounts(steps []observer.Step, cited map[int]bool) map[string]OptionCounts {
	oc := make(OptionCounts, len(stepsAllowedValues()))
	for _, v := range stepsAllowedValues() {
		oc[v] = len(FilterSteps(steps, v, cited))
	}
	return map[string]OptionCounts{"steps": oc}
}

// stepsFilterLabeler names the steps= filter's own group and options for
// buildListNav (listnav.go) — the run page's only list (§8.7 table's "run
// steps" row).
var stepsFilterLabeler = listLabeler{
	Param: func(string) string { return "Steps" },
	Value: func(_, v string) string { label, _ := stepsFilterLabelFor(v); return label },
	Title: func(_, v string) string { _, title := stepsFilterLabelFor(v); return title },
}

// loadRunTexts reads runId's task prompt and self-review straight from its
// bundle — the same two bundle files api.go's handleSelfReview/loadSteps
// read, via the same observer.Bundle helpers (§7.2).
func loadRunTexts(ctx context.Context, store observer.ObjectStore, runID string) (taskPrompt, selfReview string, err error) {
	bundle, err := observer.NewSinkBundle(ctx, store, runID)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: new bundle: %w", err)
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: results dir: %w", err)
	}
	taskPrompt, err = observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: task prompt: %w", err)
	}
	selfReview, err = observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: self-review: %w", err)
	}
	if selfReview == "" {
		selfReview = observer.NotRecorded
	}
	return taskPrompt, selfReview, nil
}

// loadOlderObservations resolves every older obsId (view.go's
// RunRow.OlderObsIDs, newest-last) to its full document, for the run page's
// "older observation versions" section (§8.3 FM-51).
func loadOlderObservations(ctx context.Context, store observer.ObjectStore, runID string, ids []string) ([]observer.Observation, error) {
	obsStore := observer.NewStore(store)
	out := make([]observer.Observation, 0, len(ids))
	for _, id := range ids {
		obs, err := obsStore.GetObservation(ctx, runID, id)
		if err != nil {
			return nil, fmt.Errorf("console: get older observation %s: %w", id, err)
		}
		out = append(out, obs)
	}
	return out, nil
}
