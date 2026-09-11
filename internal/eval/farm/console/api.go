package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// notRecorded matches §7.2's rendering of a missing optional bundle file.
const notRecorded = "(not recorded)"

// --- §8.4 FM-52 JSON shapes ---

// RunsListObservation is one runs-list row's "observation" field.
type RunsListObservation struct {
	ObsID            string   `json:"obsId"`
	Status           string   `json:"status"`
	Headline         string   `json:"headline"`
	FindingTitles    []string `json:"findingTitles"`
	UnverifiedQuotes int      `json:"unverifiedQuotes"`
}

// RunsListItem is one GET /api/runs.md|.json row (FM-52).
type RunsListItem struct {
	RunID         string               `json:"runId"`
	Batch         string               `json:"batch"`
	Scenario      string               `json:"scenario"`
	Verdict       string               `json:"verdict"`
	StartedAt     time.Time            `json:"startedAt"`
	DurationSec   float64              `json:"durationSec"`
	CostUsd       float64              `json:"costUsd"`
	ObserverState string               `json:"observerState"`
	Observation   *RunsListObservation `json:"observation,omitempty"`
}

// RunDetail is GET /api/runs/<runId>.md|.json (FM-52).
type RunDetail struct {
	RunID           string                `json:"runId"`
	Batch           string                `json:"batch"`
	Scenario        string                `json:"scenario"`
	Verdict         string                `json:"verdict"`
	StartedAt       time.Time             `json:"startedAt"`
	DurationSec     float64               `json:"durationSec"`
	CostUsd         float64               `json:"costUsd"`
	CandidateSha256 string                `json:"candidateSha256"`
	EvaluatorSha256 string                `json:"evaluatorSha256"`
	ObserverState   string                `json:"observerState"`
	Observation     *observer.Observation `json:"observation,omitempty"`
	OlderObsIDs     []string              `json:"olderObsIds"`
	FailedChecks    []FailedCheck         `json:"failedChecks"`
	EvidenceSteps   []int                 `json:"evidenceSteps"`
}

// StepJSON is one element of GET /api/runs/<runId>/steps.md|.json (FM-52).
type StepJSON struct {
	N       int    `json:"n"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Input   string `json:"input,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"isError,omitempty"`
	Text    string `json:"text,omitempty"`
}

// FindingItem is one element of GET /api/findings.md|.json (FM-52).
type FindingItem struct {
	Owner          string `json:"owner"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	What           string `json:"what"`
	RunID          string `json:"runId"`
	Steps          []int  `json:"steps"`
	QuotesVerified int    `json:"quotesVerified"`
	QuotesTotal    int    `json:"quotesTotal"`
	LookAt         string `json:"lookAt"`
	Fix            string `json:"fix"`
}

func isJSONRequest(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, ".json")
}

// runsListItemFromRow narrows a RunRow (view.go) to a runs-list row.
func runsListItemFromRow(row RunRow) RunsListItem {
	item := RunsListItem{
		RunID: row.RunID, Batch: row.Batch, Scenario: row.Scenario, Verdict: row.Verdict,
		StartedAt: row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd,
		ObserverState: row.ObserverState,
	}
	if row.Observation != nil {
		item.Observation = &RunsListObservation{
			ObsID: row.Observation.ObsID, Status: row.Observation.Status, Headline: row.Observation.Headline,
			FindingTitles: findingTitles(row.Observation), UnverifiedQuotes: unverifiedQuotes(row.Observation),
		}
	}
	return item
}

func findingTitles(obs *observer.Observation) []string {
	titles := make([]string, 0, len(obs.Findings))
	for _, f := range obs.Findings {
		titles = append(titles, f.Title)
	}
	return titles
}

func unverifiedQuotes(obs *observer.Observation) int {
	n := 0
	for _, f := range obs.Findings {
		for _, e := range f.Evidence {
			if !e.Verified {
				n++
			}
		}
	}
	return n
}

func renderRunsListMD(items []RunsListItem) string {
	var b strings.Builder
	for _, it := range items {
		headline, titles := "("+it.ObserverState+")", ""
		if it.Observation != nil {
			headline = it.Observation.Headline
			titles = strings.Join(it.Observation.FindingTitles, "; ")
		}
		fmt.Fprintf(&b, "- %s — %s — %s — $%.4f — %s — findings: %s\n",
			it.RunID, it.Scenario, it.Verdict, it.CostUsd, headline, titles)
	}
	return b.String()
}

// handleRunsList implements GET /api/runs.md|.json?since=<window> and
// ?batch=<id> (FM-52). batch wins when both are given.
func (s *Server) handleRunsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var rows []RunRow
	if batch := q.Get("batch"); batch != "" {
		if !farm.ValidBatchID(batch) {
			http.NotFound(w, r)
			return
		}
		br, err := batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		rows = br
	} else {
		window, err := ParseWindow(q.Get("since"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rows, err = rowsSinceWindow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, window, s.now(), s.queueState)
		if err != nil {
			writeStoreError(w, err)
			return
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].RunID < rows[j].RunID })
	items := make([]RunsListItem, len(rows))
	for i, row := range rows {
		items[i] = runsListItemFromRow(row)
	}

	if isJSONRequest(r) {
		writeJSON(w, struct {
			Runs []RunsListItem `json:"runs"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderRunsListMD(items))
}

func runDetailFromRow(row RunRow) RunDetail {
	steps := evidenceSteps(row.Observation)
	older := row.OlderObsIDs
	if older == nil {
		older = []string{}
	}
	failed := row.FailedChecks
	if failed == nil {
		failed = []FailedCheck{}
	}
	if steps == nil {
		steps = []int{}
	}
	return RunDetail{
		RunID: row.RunID, Batch: row.Batch, Scenario: row.Scenario, Verdict: row.Verdict,
		StartedAt: row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd,
		CandidateSha256: row.CandidateSha256, EvaluatorSha256: row.EvaluatorSha256,
		ObserverState: row.ObserverState, Observation: row.Observation,
		OlderObsIDs: older, FailedChecks: failed, EvidenceSteps: steps,
	}
}

// formatStepRanges collapses a sorted, deduplicated step-number list into
// compact ranges ("12-15, 18, 22-23") for the run-detail markdown's
// "the step ranges its evidence cites" line (§8.3 FM-51).
func formatStepRanges(steps []int) string {
	if len(steps) == 0 {
		return "(none)"
	}
	var parts []string
	start, prev := steps[0], steps[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, n := range steps[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()
	return strings.Join(parts, ", ")
}

func renderRunDetailMD(d RunDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nscenario: %s\nbatch: %s\nverdict: %s\nstarted: %s\nduration: %.1fs\ncost: $%.4f\ncandidate: %s\nevaluator: %s\nobserverState: %s\n\n",
		d.RunID, d.Scenario, d.Batch, d.Verdict, d.StartedAt.UTC().Format(time.RFC3339),
		d.DurationSec, d.CostUsd, d.CandidateSha256, d.EvaluatorSha256, d.ObserverState)

	if d.Observation != nil {
		b.WriteString(observer.Render(*d.Observation))
	} else {
		b.WriteString("Observer: " + notRecorded + "\n")
	}

	b.WriteString("\nFailed/blocked checks:\n")
	if len(d.FailedChecks) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, c := range d.FailedChecks {
			fmt.Fprintf(&b, "- %s %s expected=%q observed=%q source=%s\n", c.ID, c.Result, c.Expected, c.Observed, c.Source)
		}
	}

	b.WriteString("\nEvidence steps: " + formatStepRanges(d.EvidenceSteps) + "\n")
	return b.String()
}

// writeStoreError maps a view.go lookup error to its HTTP status: a not-
// found id (already grammar/existence checked) is 404; anything else is a
// genuine backend failure (502 — the bucket, not the caller, is at fault).
func writeStoreError(w http.ResponseWriter, err error) {
	if isNotFoundErr(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, ErrBatchNotFound) || errors.Is(err, ErrRunNotFound) || errors.Is(err, observer.ErrResultsNotFound)
}

// handleRunsSubroute dispatches every GET /api/runs/<rest> request: a bare
// "<runId>.md|.json" is the run detail; "<runId>/steps.md|.json",
// "<runId>/self-review.md|.json" and "<runId>/files/<path>" are its
// sub-resources (FM-52). Every id is checked against its FM-47 grammar
// before any store call (TestAPI_InvalidRunOrBatchIDRejected).
func (s *Server) handleRunsSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}

	runID, tail, hasTail := strings.Cut(rest, "/")
	if !hasTail {
		id, ok := trimMDOrJSON(runID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.handleRunDetail(w, r, id)
		return
	}

	switch {
	case tail == "steps.md" || tail == "steps.json":
		s.handleSteps(w, r, runID)
	case tail == "self-review.md" || tail == "self-review.json":
		s.handleSelfReview(w, r, runID)
	case strings.HasPrefix(tail, "files/"):
		s.handleFile(w, r, runID, strings.TrimPrefix(tail, "files/"))
	default:
		http.NotFound(w, r)
	}
}

func trimMDOrJSON(s string) (string, bool) {
	if base, ok := strings.CutSuffix(s, ".md"); ok {
		return base, true
	}
	if base, ok := strings.CutSuffix(s, ".json"); ok {
		return base, true
	}
	return "", false
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	row, err := loadRunRow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	detail := runDetailFromRow(row)
	if isJSONRequest(r) {
		writeJSON(w, detail)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderRunDetailMD(detail))
}

// loadSteps rebuilds runID's full, untruncated step record (§7.2) straight
// from the bucket.
func loadSteps(ctx context.Context, store observer.ObjectStore, runID string) ([]observer.Step, error) {
	bundle, err := observer.NewSinkBundle(ctx, store, runID)
	if err != nil {
		return nil, err
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return nil, err
	}
	taskPrompt, err := observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return nil, err
	}
	transcript, err := observer.LoadTranscript(bundle, resultsDir)
	if err != nil {
		return nil, err
	}
	meta, err := observer.LoadMeta(bundle, resultsDir)
	if err != nil {
		return nil, err
	}
	return observer.BuildSteps(taskPrompt, transcript, resumeReplies(meta))
}

// resumeReplies extracts meta.json's userSim.turns[].reply, in order,
// for BuildSteps' resumed-segment numbering (§7.2) — mirrors
// cmd/zcp/eval_farm_observe.go's helper of the same name (a different
// package: DirBundle's own local pipeline vs. the console's SinkBundle
// one, so the small helper is duplicated rather than shared across the
// cmd/internal boundary).
func resumeReplies(meta eval.BehavioralResult) []string {
	if meta.UserSim == nil {
		return nil
	}
	replies := make([]string, len(meta.UserSim.Turns))
	for i, turn := range meta.UserSim.Turns {
		replies[i] = turn.Reply
	}
	return replies
}

func stepJSON(s observer.Step) StepJSON {
	out := StepJSON{N: s.N, Kind: string(s.Kind)}
	if s.Kind == observer.StepTool {
		out.Tool, out.Input, out.Result, out.IsError = s.ToolName, s.ToolInputJSON, s.ToolResultText, s.ToolIsError
		return out
	}
	out.Text = s.Text
	return out
}

func renderStepsMD(steps []observer.Step) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.Kind {
		case observer.StepTool:
			fmt.Fprintf(&b, "#%d tool %s %s\n", s.N, s.ToolName, s.ToolInputJSON)
			if s.ToolIsError {
				fmt.Fprintf(&b, "  → ERROR %s\n", s.ToolResultText)
			} else {
				fmt.Fprintf(&b, "  → %s\n", s.ToolResultText)
			}
		case observer.StepUser, observer.StepThinking, observer.StepAgent:
			fmt.Fprintf(&b, "#%d %s %s\n", s.N, s.Kind, s.Text)
		}
	}
	return b.String()
}

// handleSteps implements GET /api/runs/<runId>/steps.md|.json?from=<n>&to=<m>
// (FM-52): those steps, untruncated.
func (s *Server) handleSteps(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	from, to, err := parseFromTo(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	all, err := loadSteps(r.Context(), s.cfg.Store, runID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var sel []observer.Step
	for _, st := range all {
		if st.N >= from && st.N <= to {
			sel = append(sel, st)
		}
	}
	if isJSONRequest(r) {
		out := make([]StepJSON, len(sel))
		for i, st := range sel {
			out[i] = stepJSON(st)
		}
		writeJSON(w, out)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderStepsMD(sel))
}

func parseFromTo(q map[string][]string) (from, to int, err error) {
	get := func(k string) (int, error) {
		v := ""
		if vs, ok := q[k]; ok && len(vs) > 0 {
			v = vs[0]
		}
		if v == "" {
			return 0, fmt.Errorf("%s is required", k)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %q", k, v)
		}
		return n, nil
	}
	from, err = get("from")
	if err != nil {
		return 0, 0, err
	}
	to, err = get("to")
	if err != nil {
		return 0, 0, err
	}
	if from > to {
		return 0, 0, fmt.Errorf("from (%d) must be <= to (%d)", from, to)
	}
	return from, to, nil
}

// handleSelfReview implements GET /api/runs/<runId>/self-review.md|.json
// (FM-52).
func (s *Server) handleSelfReview(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	bundle, err := observer.NewSinkBundle(r.Context(), s.cfg.Store, runID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	text, err := observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if text == "" {
		text = notRecorded
	}
	if isJSONRequest(r) {
		writeJSON(w, struct {
			RunID      string `json:"runId"`
			SelfReview string `json:"selfReview"`
		}{runID, text})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, text)
}

// handleFile implements GET /api/runs/<runId>/files/<path> (FM-52): one
// bundle file under results/, as text/plain; any other path is 404. Cached
// (server.go's fileCache) once the run's done.json exists — the bundle is
// then immutable (§7.6 FM-47).
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request, runID, filePath string) {
	if !farm.ValidRunID(runID) || !validBundleFilePath(filePath) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	cacheKey := runID + "/" + filePath

	if data, ok := s.files.get(cacheKey); ok {
		writeTextFile(w, data)
		return
	}

	doneExists, _, err := s.cfg.Store.Head(ctx, doneKey(runID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	bundle, err := observer.NewSinkBundle(ctx, s.cfg.Store, runID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := bundle.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if doneExists {
		s.files.set(cacheKey, data)
	}
	writeTextFile(w, data)
}

func writeTextFile(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// validBundleFilePath enforces FM-52's "files/ only under results/"
// boundary: filePath must start with "results/", have a non-empty tail,
// and carry no ".." traversal segment (a raw or percent-decoded one alike
// — net/http already percent-decodes r.URL.Path before this ever runs).
func validBundleFilePath(filePath string) bool {
	if !strings.HasPrefix(filePath, "results/") {
		return false
	}
	if strings.Contains(filePath, "..") {
		return false
	}
	if path.Clean(filePath) != filePath {
		return false
	}
	return len(filePath) > len("results/")
}

// --- Findings (§8.4 FM-52's GET /api/findings.md|.json?since=<window>) ---

func findingItemsFromRows(rows []RunRow) []FindingItem {
	var items []FindingItem
	for _, row := range rows {
		if row.Observation == nil {
			continue
		}
		for _, f := range row.Observation.Findings {
			steps := make([]int, 0, len(f.Evidence))
			verified, total := 0, 0
			for _, e := range f.Evidence {
				total++
				if e.Verified {
					verified++
				}
				if e.Step > 0 {
					steps = append(steps, e.Step)
				}
			}
			sort.Ints(steps)
			items = append(items, FindingItem{
				Owner: f.Owner, Severity: f.Severity, Title: f.Title, What: f.What,
				RunID: row.RunID, Steps: steps, QuotesVerified: verified, QuotesTotal: total,
				LookAt: f.LookAt, Fix: f.Fix,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Owner != items[j].Owner {
			return items[i].Owner < items[j].Owner
		}
		return severityRank(items[i].Severity) < severityRank(items[j].Severity)
	})
	return items
}

func severityRank(sev string) int {
	switch sev {
	case observer.SeverityHigh:
		return 0
	case observer.SeverityMedium:
		return 1
	case observer.SeverityLow:
		return 2
	default:
		return 3
	}
}

func renderFindingsMD(items []FindingItem) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s · %s] %s (%s, steps %s) — %s\n",
			it.Owner, it.Severity, it.Title, it.RunID, formatStepRanges(it.Steps), it.What)
	}
	return b.String()
}

// handleFindings implements GET /api/findings.md|.json?since=<window>
// (FM-52): every finding of every run in the window, grouped by owner then
// severity.
func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	window, err := ParseWindow(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := rowsSinceWindow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, window, s.now(), s.queueState)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := findingItemsFromRows(rows)
	if isJSONRequest(r) {
		writeJSON(w, struct {
			Findings []FindingItem `json:"findings"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderFindingsMD(items))
}
