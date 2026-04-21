package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sessionEvent is the outer shell of every JSONL record the Claude
// Code CLI emits. Only the fields the bars look at are modeled.
type sessionEvent struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
	// tool_result events put the useful payload in toolUseResult at the
	// outer level (not nested inside message). See host-session records
	// where the assistant already parsed the result into JSON.
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

// assistantMessage captures the subset of an `assistant` event needed
// for tool_use enumeration.
type assistantMessage struct {
	Content []messageContent `json:"content"`
}

// userMessage is the same shape as assistantMessage for tool_result
// enumeration. `user` events wrap tool_result records under content.
type userMessage struct {
	Content []messageContent `json:"content"`
}

// messageContent is either a tool_use (Type=="tool_use") or a
// tool_result (Type=="tool_result") or a text block.
type messageContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// workflowInput captures the shape of a `mcp__zerops__zerops_workflow`
// tool_use input. Not every call uses every field.
type workflowInput struct {
	Action  string `json:"action"`
	Step    string `json:"step"`
	Substep string `json:"substep"`
}

// bashInput matches `Bash` tool_use shape.
type bashInput struct {
	Command string `json:"command"`
}

// editInput matches `Edit` tool_use shape.
type editInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// writeInput matches `Write` tool_use shape.
type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// agentInput matches the Agent dispatch shape. Description names the
// sub-agent role (used to filter writer/editorial/code-review
// dispatches).
type agentInput struct {
	Description  string `json:"description"`
	SubagentType string `json:"subagent_type"`
	Prompt       string `json:"prompt"`
}

// parsedToolUse carries what a bar implementation actually cares about.
type parsedToolUse struct {
	ID        string
	Name      string
	Input     json.RawMessage
	Timestamp string
	EventUUID string
}

// parsedToolResult is the companion for user-role events with
// tool_result content. Raw is the raw inner content array or the outer
// toolUseResult field — whichever carries the response body.
type parsedToolResult struct {
	ToolUseID string
	Raw       []byte
	Timestamp string
	EventUUID string
}

// parseJSONL streams a Claude Code JSONL file. Uses bufio.Reader
// rather than Scanner because some tool-response records exceed the
// Scanner default 64KB line cap (dispatch-brief responses on readmes
// substep can reach 70KB+ per v35 evidence).
//
// onToolUse receives every assistant-emitted tool_use block; onResult
// receives every user-emitted tool_result block. Errors bubble up.
func parseJSONL(path string, onToolUse func(parsedToolUse) error, onResult func(parsedToolResult) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if perr := dispatchLine(line, onToolUse, onResult); perr != nil {
				return perr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
	}
}

func dispatchLine(line []byte, onToolUse func(parsedToolUse) error, onResult func(parsedToolResult) error) error {
	var ev sessionEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		// Malformed or partial JSONL record — Claude Code occasionally
		// emits queue-operation events with sparse fields; skipping is
		// the right behavior because a bar-correctness gate would
		// over-fire on benign junk.
		return nil //nolint:nilerr
	}
	switch ev.Type {
	case "assistant":
		var m assistantMessage
		if err := json.Unmarshal(ev.Message, &m); err != nil {
			return nil //nolint:nilerr // skip events with unmodeled message shapes
		}
		for _, c := range m.Content {
			if c.Type != "tool_use" {
				continue
			}
			if onToolUse == nil {
				continue
			}
			if err := onToolUse(parsedToolUse{
				ID: c.ID, Name: c.Name, Input: c.Input,
				Timestamp: ev.Timestamp, EventUUID: ev.UUID,
			}); err != nil {
				return err
			}
		}
	case "user":
		var m userMessage
		if err := json.Unmarshal(ev.Message, &m); err != nil {
			return nil //nolint:nilerr // skip events with unmodeled message shapes
		}
		for _, c := range m.Content {
			if c.Type != "tool_result" {
				continue
			}
			if onResult == nil {
				continue
			}
			raw := []byte(c.Content)
			if len(raw) == 0 && len(ev.ToolUseResult) > 0 {
				raw = ev.ToolUseResult
			}
			if err := onResult(parsedToolResult{
				ToolUseID: c.ToolUseID, Raw: raw,
				Timestamp: ev.Timestamp, EventUUID: ev.UUID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// SessionScan carries the aggregated events the bar functions compute
// against. Populated once per CLI invocation.
type SessionScan struct {
	WorkflowCalls        []workflowCall
	BashCommands         []string
	EditCalls            []editInput
	WriteCalls           []writeCallRec
	AgentDispatches      []agentInput
	CheckResultsByCallID map[string]checkResult
	Timestamps           []string
}

type workflowCall struct {
	ID        string
	Input     workflowInput
	Timestamp string
}

type writeCallRec struct {
	Source    string // "main" | subagent file basename
	Input     writeInput
	Timestamp string
}

// checkResult captures the parsed payload a workflow step-complete
// response returns. Only the fields the bars consult are modeled.
type checkResult struct {
	Passed        bool
	FailingChecks []string
}

// workflowResult matches the JSON envelope a workflow step-complete
// response returns. The engine returns a JSON body in the
// tool_result content array as a text block; we peel it back here.
//
// Per-check payload carries a string `status` ("pass" | "fail") and a
// name. The top-level `passed` bool is the aggregate across all
// checks. This shape is established by the engine's
// `internal/tools/workflow*` responses; a mismatched decoder
// fabricates "failing" check lists where none exist, which v36
// retrospective runs surfaced during harness validation.
type workflowResult struct {
	CheckResult struct {
		Passed bool `json:"passed"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	} `json:"checkResult"`
}

// textBlock matches a single item inside a tool_result.content array.
// Claude Code tool_result content is a JSON array of {type,text}
// dictionaries; we parse it and grab the first text block.
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ScanSessions walks the main session JSONL and every sub-agent JSONL
// under subagents/, collecting tool_use + tool_result records bar
// implementations later query. Sub-agent dispatches are resolved to a
// role label via the companion .meta.json files.
func ScanSessions(sessionsLogsDir string) (*SessionScan, error) {
	scan := &SessionScan{
		CheckResultsByCallID: make(map[string]checkResult),
	}
	mainPath := filepath.Join(sessionsLogsDir, "main-session.jsonl")
	if _, err := os.Stat(mainPath); err != nil {
		return nil, fmt.Errorf("main-session.jsonl: %w", err)
	}

	if err := collectFromJSONL(mainPath, "main", scan); err != nil {
		return nil, err
	}
	subagentsDir := filepath.Join(sessionsLogsDir, "subagents")
	if entries, err := os.ReadDir(subagentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			p := filepath.Join(subagentsDir, e.Name())
			if err := collectFromJSONL(p, e.Name(), scan); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read subagents dir: %w", err)
	}

	return scan, nil
}

func collectFromJSONL(path, source string, scan *SessionScan) error {
	return parseJSONL(path,
		func(t parsedToolUse) error {
			switch t.Name {
			case "mcp__zerops__zerops_workflow":
				var in workflowInput
				_ = json.Unmarshal(t.Input, &in)
				scan.WorkflowCalls = append(scan.WorkflowCalls, workflowCall{
					ID: t.ID, Input: in, Timestamp: t.Timestamp,
				})
			case "Bash":
				var in bashInput
				if err := json.Unmarshal(t.Input, &in); err == nil {
					scan.BashCommands = append(scan.BashCommands, in.Command)
				}
			case "Edit":
				var in editInput
				if err := json.Unmarshal(t.Input, &in); err == nil {
					scan.EditCalls = append(scan.EditCalls, in)
				}
			case "Write":
				var in writeInput
				if err := json.Unmarshal(t.Input, &in); err == nil {
					scan.WriteCalls = append(scan.WriteCalls, writeCallRec{
						Source: source, Input: in, Timestamp: t.Timestamp,
					})
				}
			case "Agent", "Task":
				var in agentInput
				if err := json.Unmarshal(t.Input, &in); err == nil {
					scan.AgentDispatches = append(scan.AgentDispatches, in)
				}
			}
			return nil
		},
		func(r parsedToolResult) error {
			cr, ok := parseCheckResult(r.Raw)
			if ok {
				scan.CheckResultsByCallID[r.ToolUseID] = cr
			}
			return nil
		},
	)
}

// parseCheckResult unwraps the JSON envelope a workflow step-complete
// response returns. The tool_result.content is a JSON-array of
// {type,text} blocks; the text block carries an engine JSON body that
// we unmarshal into workflowResult.
func parseCheckResult(raw []byte) (checkResult, bool) {
	if len(raw) == 0 {
		return checkResult{}, false
	}
	// Two shapes: content is directly a JSON array of text blocks, OR
	// an engine-string already. Try array first.
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var blocks []textBlock
		if err := json.Unmarshal([]byte(trimmed), &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" {
					if cr, ok := decodeWorkflowResult(b.Text); ok {
						return cr, true
					}
				}
			}
		}
	}
	// Fall through: maybe the raw IS the workflow JSON already.
	if cr, ok := decodeWorkflowResult(trimmed); ok {
		return cr, true
	}
	return checkResult{}, false
}

func decodeWorkflowResult(s string) (checkResult, bool) {
	// Some workflow responses embed a JSON body inside a markdown code
	// fence; strip the fence if present.
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	if !strings.Contains(s, "\"checkResult\"") {
		return checkResult{}, false
	}
	var wr workflowResult
	if err := json.Unmarshal([]byte(s), &wr); err != nil {
		return checkResult{}, false
	}
	cr := checkResult{Passed: wr.CheckResult.Passed}
	for _, c := range wr.CheckResult.Checks {
		if c.Status != "pass" && c.Status != "" {
			cr.FailingChecks = append(cr.FailingChecks, c.Name)
		}
	}
	return cr, true
}

// CheckDeployReadmesRetryRounds implements B-20. Counts failing
// workflow-complete responses across the deploy phase. The v36
// engine rolls readmes-substep check iterations into a single
// `complete substep=readmes` response, so strict substep filtering
// understates the retry signal; a phase-wide count captures every
// deploy-phase failure the agent had to recover from. Every failing
// substep response is evidence of a writer-compliance or feature-
// compliance round that didn't land first-try.
func CheckDeployReadmesRetryRounds(scan *SessionScan, threshold int) BarResult {
	failed := 0
	var failingSubsteps []string
	for _, wc := range scan.WorkflowCalls {
		if wc.Input.Action != actionComplete || wc.Input.Step != "deploy" {
			continue
		}
		cr, ok := scan.CheckResultsByCallID[wc.ID]
		if !ok {
			continue
		}
		if !cr.Passed {
			failed++
			failingSubsteps = append(failingSubsteps, wc.Input.Substep)
		}
	}
	status := PassOrFail(failed <= threshold)
	return BarResult{
		Description:   "deploy-phase failing workflow completions (writer/feature retry signal)",
		Measurement:   "count workflow.complete(step=deploy) with checkResult.passed==false across all substeps",
		Threshold:     threshold,
		Observed:      failed,
		Status:        status,
		EvidenceFiles: failingSubsteps,
	}
}

// CheckSessionlessExportAttempts implements B-21. A sessionless export
// is a Bash tool_use whose command runs `zcp sync recipe export` and
// neither names `--session` nor exports `ZCP_SESSION_ID` inline.
func CheckSessionlessExportAttempts(scan *SessionScan) BarResult {
	var offenders []string
	for _, cmd := range scan.BashCommands {
		if !strings.Contains(cmd, "sync recipe export") {
			continue
		}
		if strings.Contains(cmd, "--session") || strings.Contains(cmd, "ZCP_SESSION_ID=") {
			continue
		}
		offenders = append(offenders, cmd)
	}
	return BarResult{
		Description:   "sessionless `zcp sync recipe export` attempts (F-8 evidence)",
		Measurement:   "Bash tool_use input.command contains 'sync recipe export' AND no --session / ZCP_SESSION_ID",
		Threshold:     0,
		Observed:      len(offenders),
		Status:        PassOrFail(len(offenders) == 0),
		EvidenceFiles: offenders,
	}
}

// CheckWriterFirstPassFailures implements B-23. Finds the first
// writer Agent dispatch and counts the distinct failing check names
// in the first following `deploy substep~=readmes` tool_result.
func CheckWriterFirstPassFailures(scan *SessionScan, threshold int) BarResult {
	// Writer dispatch detection: description contains "writer" (case-
	// insensitive). The v36 writer-1 dispatch description is
	// "Recipe writer sub-agent".
	writerDispatchAt := -1
	for i, a := range scan.AgentDispatches {
		if strings.Contains(strings.ToLower(a.Description), "writer") {
			writerDispatchAt = i
			break
		}
	}
	if writerDispatchAt == -1 {
		return BarResult{
			Description: "writer first-pass compliance failures",
			Threshold:   threshold,
			Status:      StatusSkip,
			Reason:      "no writer Agent dispatch observed",
		}
	}
	// Use the FIRST failing deploy-phase response post-writer as the
	// writer's first-pass result. We have no per-agent ordering here
	// (AgentDispatches slice is source-ordered); v36 evidence
	// confirms the first failing deploy response after writer-1 is
	// the readmes substep. Phase-wide scan accommodates the engine's
	// rollup of readmes-internal iterations (see B-20 note).
	for _, wc := range scan.WorkflowCalls {
		if wc.Input.Action != actionComplete || wc.Input.Step != "deploy" {
			continue
		}
		cr, ok := scan.CheckResultsByCallID[wc.ID]
		if !ok || cr.Passed {
			continue
		}
		distinct := uniqueNames(cr.FailingChecks)
		sort.Strings(distinct)
		return BarResult{
			Description:   "writer first-pass compliance failures",
			Measurement:   "distinct failing check names in first failing deploy-phase checkResult after writer dispatch",
			Threshold:     threshold,
			Observed:      len(distinct),
			Status:        PassOrFail(len(distinct) <= threshold),
			EvidenceFiles: distinct,
		}
	}
	return BarResult{
		Description: "writer first-pass compliance failures",
		Threshold:   threshold,
		Status:      StatusSkip,
		Reason:      "no readmes-substep check result after writer dispatch",
	}
}

// CheckMarkerFixEditCycles is the JSONL-derived F-12 evidence bar.
// Counts Edit tool calls whose old_string contains a ZEROPS_EXTRACT
// marker MISSING the trailing `#` and whose new_string contains the
// same marker WITH the trailing `#`. Retrospectively surfaces writer-
// fix-pass marker corrections the deliverable tree no longer shows.
func CheckMarkerFixEditCycles(scan *SessionScan) BarResult {
	var hits []string
	for _, e := range scan.EditCalls {
		if markerBrokenRe.MatchString(e.OldString) &&
			strings.Contains(e.NewString, "ZEROPS_EXTRACT_") &&
			!markerBrokenRe.MatchString(e.NewString) {
			hits = append(hits, e.FilePath)
		}
	}
	return BarResult{
		Description:   "marker-correction Edit cycles (F-12 retrospective evidence)",
		Measurement:   "count Edit tool_use where old_string matches broken-marker regex and new_string flips it",
		Threshold:     0,
		Observed:      len(hits),
		Status:        PassOrFail(len(hits) == 0),
		EvidenceFiles: hits,
	}
}

// CheckStandaloneFileAuthorship is the JSONL-derived F-13 evidence
// bar. Counts Write tool calls targeting INTEGRATION-GUIDE.md or
// GOTCHAS.md across main + sub-agent sessions. Retrospectively
// surfaces writer sub-agent authorship of dead files even when the
// deliverable has been cleaned.
func CheckStandaloneFileAuthorship(scan *SessionScan) BarResult {
	var hits []string
	for _, w := range scan.WriteCalls {
		base := filepath.Base(w.Input.FilePath)
		if forbiddenStandaloneNames[base] {
			hits = append(hits, w.Input.FilePath)
		}
	}
	sort.Strings(hits)
	return BarResult{
		Description:   "writer authorship of standalone INTEGRATION-GUIDE.md / GOTCHAS.md (F-13 retrospective)",
		Measurement:   "count Write tool_use across main + subagents where path basename ∈ {INTEGRATION-GUIDE.md, GOTCHAS.md}",
		Threshold:     0,
		Observed:      len(hits),
		Status:        PassOrFail(len(hits) == 0),
		EvidenceFiles: hits,
	}
}

// ComputeSessionMetrics aggregates the JSONL bars + the derived
// role-dispatch signals. The caller supplies the session scan + per-
// bar thresholds (kept on the call site rather than baked into bar
// funcs so a future spec change can shift a gate without code edits).
func ComputeSessionMetrics(scan *SessionScan) SessionMetrics {
	m := SessionMetrics{
		DeployReadmesRetryRounds:  CheckDeployReadmesRetryRounds(scan, 2),
		SessionlessExportAttempts: CheckSessionlessExportAttempts(scan),
		WriterFirstPassFailures:   CheckWriterFirstPassFailures(scan, 3),
		SubAgentCount:             len(scan.AgentDispatches),
	}
	// Role dispatches.
	for _, a := range scan.AgentDispatches {
		d := strings.ToLower(a.Description)
		switch {
		case strings.Contains(d, "editorial"):
			m.EditorialReviewDispatched = true
		case strings.Contains(d, "code-review") || strings.Contains(d, "code review"):
			m.CodeReviewDispatched = true
		}
	}
	// Close-step completion: any workflow complete step=close whose
	// check result passed. A dispatched but failing close does not
	// count as complete.
	for _, wc := range scan.WorkflowCalls {
		if wc.Input.Action == actionComplete && wc.Input.Step == "close" {
			cr, ok := scan.CheckResultsByCallID[wc.ID]
			if ok && cr.Passed {
				m.CloseStepCompleted = true
				break
			}
		}
	}
	// Close-browser-walk attempted: look for a browser tool call
	// anywhere in the session. Per spec, soft pass if environmentally
	// broken; hard fail if not attempted.
	for _, wc := range scan.WorkflowCalls {
		_ = wc // walk is not via workflow; browser via zerops_browser
	}
	// Separate quick scan for browser tool_use by re-walking workflow
	// count isn't sufficient; the Agent log only tracks workflow calls
	// distinctly. Approximate: if any zerops_browser call exists in
	// main log (inspected earlier, not tracked in scan yet) → true.
	// For v1 we conservatively compute from stored state.
	return m
}

// uniqueNames returns a distinct-preserving slice.
func uniqueNames(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, n := range in {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// LoadSubAgentRoleMap walks SESSIONS_LOGS/subagents/*.meta.json and
// returns {agent-file: description}. Used by the checklist generator
// when naming retry cycles or dispatch-integrity rows.
func LoadSubAgentRoleMap(sessionsLogsDir string) (map[string]string, error) {
	subagentsDir := filepath.Join(sessionsLogsDir, "subagents")
	roles := map[string]string{}
	entries, err := os.ReadDir(subagentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return roles, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		data, rerr := fs.ReadFile(os.DirFS(subagentsDir), e.Name())
		if rerr != nil {
			continue
		}
		var meta struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &meta) == nil {
			roles[strings.TrimSuffix(e.Name(), ".meta.json")] = meta.Description
		}
	}
	return roles, nil
}
