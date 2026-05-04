// Package eval — user-sim classifier.
//
// ClassifyTranscriptTail decides what state the agent is in by inspecting the
// stream-json transcript file produced by `claude -p --output-format stream-json`.
// The verdict drives the user-sim loop in RunBehavioralScenario:
//
//   - Done    → break, retrospective fires.
//   - Waiting → spawn user-sim, inject reply via `claude --resume`.
//   - Error / MaxTurns → break, log reason, retrospective fires.
//   - Working → loop must not act yet (mid-tool roundtrip, no terminal event).
//
// The rule table is documented in plans/flow-eval-usersim-2026-05-04.md.
// Updating rules here REQUIRES updating the table there and the canned
// transcripts under testdata/usersim/.
package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// VerdictKind enumerates the five possible classifier outcomes.
type VerdictKind int

const (
	// VerdictWorking — no terminal `result` event yet; loop must wait.
	VerdictWorking VerdictKind = iota
	// VerdictDone — agent declared task complete (text markers or successful verify).
	VerdictDone
	// VerdictWaiting — agent ended turn with text-only content awaiting user input.
	VerdictWaiting
	// VerdictError — agent process / API error (is_error=true).
	VerdictError
	// VerdictMaxTurns — claude headless hit its max-turns cap.
	VerdictMaxTurns
)

func (k VerdictKind) String() string {
	switch k {
	case VerdictWorking:
		return "working"
	case VerdictDone:
		return "done"
	case VerdictWaiting:
		return "waiting"
	case VerdictError:
		return "error"
	case VerdictMaxTurns:
		return "max_turns"
	default:
		return "unknown"
	}
}

// Verdict is the classifier output. LastAssistantText is the final assistant
// message text (concatenated text content blocks), used by the user-sim prompt
// builder. Reason carries which rule fired, for debugging.
type Verdict struct {
	Kind              VerdictKind
	LastAssistantText string
	Reason            string
}

// ClassifyTranscriptTail reads logFile (stream-json) and returns the verdict
// per the rule table in plans/flow-eval-usersim-2026-05-04.md §"Detection rules".
// Pure function: no side effects beyond reading the file.
func ClassifyTranscriptTail(logFile string) (Verdict, error) {
	events, err := parseStreamJSON(logFile)
	if err != nil {
		return Verdict{}, err
	}

	finalText, lastAsstHasToolUse := lastAssistantTextAndShape(events)
	resultEv, hasResult := findLastResult(events)

	// No result event yet → still working (mid-roundtrip).
	if !hasResult {
		return Verdict{Kind: VerdictWorking, LastAssistantText: finalText, Reason: "no_terminal_event"}, nil
	}

	// Rule 1 — fatal error flag set on result.
	// "error_max_turns" is also is_error=true upstream, so check max_turns subtype FIRST.
	if resultEv.Subtype == "error_max_turns" || resultEv.StopReason == "max_turns" {
		return Verdict{Kind: VerdictMaxTurns, LastAssistantText: finalText, Reason: "rule2_max_turns"}, nil
	}
	if resultEv.IsError {
		return Verdict{Kind: VerdictError, LastAssistantText: finalText, Reason: "rule1_is_error"}, nil
	}

	// `result.result` is the canonical final assistant text from claude headless.
	// Prefer it over the last-assistant scan when present (covers cases where
	// result event content drifts from streaming events).
	canonicalText := finalText
	if strings.TrimSpace(resultEv.ResultText) != "" {
		canonicalText = resultEv.ResultText
	}

	// Rule 4 — last tool_use was zerops_verify with success-shaped result.
	// Checked BEFORE rule 3 because verify-success is a stronger signal than
	// text markers (covers "verify confirms healthy. anything else?" tail).
	if lastVerifyOK(events) {
		return Verdict{Kind: VerdictDone, LastAssistantText: canonicalText, Reason: "rule4_verify_success"}, nil
	}

	hasQuestionMark := strings.Contains(textTail(canonicalText, 200), "?")
	hasModalPhrase := modalPhraseRE.MatchString(canonicalText)

	// Rule 3 — done text markers AND zero `?` in the final assistant turn.
	if doneMarkerRE.MatchString(canonicalText) && !hasQuestionMark {
		return Verdict{Kind: VerdictDone, LastAssistantText: canonicalText, Reason: "rule3_done_markers"}, nil
	}

	// At this point, end_turn with text-only content. lastAsstHasToolUse=false
	// means the agent's final message was pure text (didn't end on a tool call).
	if !lastAsstHasToolUse {
		// Rule 5 — explicit waiting markers in final text.
		if hasQuestionMark || hasModalPhrase {
			reason := "rule5_question_mark"
			if !hasQuestionMark && hasModalPhrase {
				reason = "rule5_modal_phrase"
			}
			return Verdict{Kind: VerdictWaiting, LastAssistantText: canonicalText, Reason: reason}, nil
		}
		// Rule 6 — text-only end without done-markers AND without question
		// markers. Conservative: treat as waiting. False-positive cost is one
		// throwaway user-sim turn ("go ahead"); false-negative cost is a
		// hypothetical retrospective. We'd rather over-prompt the user-sim.
		return Verdict{Kind: VerdictWaiting, LastAssistantText: canonicalText, Reason: "rule6_text_only_end_conservative"}, nil
	}

	// Last assistant message contained tool_use but stream terminated with
	// result event — the tool roundtrip completed and agent ended turn after
	// tool result. Treat as done if any done markers appeared in canonical
	// text; otherwise this is an unusual shape — treat as working.
	if doneMarkerRE.MatchString(canonicalText) {
		return Verdict{Kind: VerdictDone, LastAssistantText: canonicalText, Reason: "rule3_post_tool_done"}, nil
	}
	return Verdict{Kind: VerdictWorking, LastAssistantText: canonicalText, Reason: "rule7_post_tool_no_markers"}, nil
}

var (
	// doneMarkerRE — phrases agent uses when declaring task complete.
	// Word-boundary anchored to avoid matching "deployedconfig" etc.
	doneMarkerRE = regexp.MustCompile(`(?i)\b(deployed|ready(?:\s+to\s+go)?|set\s+up|complete(?:d|ly)?|verified|live|done|all\s+set|up\s+and\s+running)\b|✓`)

	// modalPhraseRE — phrases that signal a question even without `?`.
	// Anchored on word boundaries so "I should I" inside a sentence still hits.
	modalPhraseRE = regexp.MustCompile(`(?i)\b(should\s+I|do\s+you\s+want|would\s+you\s+(?:prefer|like)|let\s+me\s+know|please\s+confirm|shall\s+I|which\s+would\s+you\s+like|either\s+is\s+fine)\b`)
)

func parseStreamJSON(logFile string) ([]parsedEvent, error) {
	f, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22) // 4MiB cap per line

	var out []parsedEvent
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		ev, err := decodeEvent(line)
		if err != nil {
			// Best-effort: skip malformed lines (claude headless can emit
			// occasional non-event diagnostics on stderr-merged streams).
			continue
		}
		out = append(out, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}
	return out, nil
}

// parsedEvent is the runtime-typed projection used by the classifier.
type parsedEvent struct {
	Type    string
	Subtype string

	// result fields
	IsError    bool
	StopReason string
	ResultText string

	// assistant fields
	AssistantText  string   // concatenated text-block content
	ToolUseNames   []string // names of tool_use blocks in this assistant message (in order)
	ToolUseIDs     []string // matching ids, parallel to ToolUseNames
	HasAssistantTU bool     // true if this assistant message contained any tool_use

	// user fields (tool_result carrier)
	ToolResultID   string
	ToolResultText string
}

func decodeEvent(line []byte) (parsedEvent, error) {
	var raw struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		// result
		IsError    bool   `json:"is_error"`    //nolint:tagliatelle // upstream
		StopReason string `json:"stop_reason"` //nolint:tagliatelle // upstream
		Result     string `json:"result"`
		// assistant / user
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Name      string `json:"name"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"` //nolint:tagliatelle // upstream
				// tool_result content (nested)
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return parsedEvent{}, err
	}

	pe := parsedEvent{
		Type:       raw.Type,
		Subtype:    raw.Subtype,
		IsError:    raw.IsError,
		StopReason: raw.StopReason,
		ResultText: raw.Result,
	}

	switch raw.Type {
	case eventTypeAssistant:
		var texts []string
		for _, c := range raw.Message.Content {
			switch c.Type {
			case contentTypeText:
				if strings.TrimSpace(c.Text) != "" {
					texts = append(texts, c.Text)
				}
			case contentTypeToolUse:
				pe.HasAssistantTU = true
				pe.ToolUseNames = append(pe.ToolUseNames, c.Name)
				pe.ToolUseIDs = append(pe.ToolUseIDs, c.ID)
			}
		}
		pe.AssistantText = strings.Join(texts, "\n")
	case eventTypeUser:
		// User events carry tool_result content blocks. We need the (id, text)
		// pair to correlate with the most recent tool_use.
		for _, c := range raw.Message.Content {
			if c.Type == contentTypeToolRes {
				pe.ToolResultID = c.ToolUseID
				var rtexts []string
				for _, inner := range c.Content {
					if inner.Type == contentTypeText {
						rtexts = append(rtexts, inner.Text)
					}
				}
				pe.ToolResultText = strings.Join(rtexts, "\n")
			}
		}
	}
	return pe, nil
}

// findLastResult returns the last result event in the transcript, or
// (zero, false) if no terminal event has been emitted yet.
func findLastResult(events []parsedEvent) (parsedEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventTypeResult {
			return events[i], true
		}
	}
	return parsedEvent{}, false
}

// lastAssistantTextAndShape returns the concatenated text of the last
// assistant message AND whether that message contained any tool_use. Both are
// needed because rule branches differ for "ended with text-only" vs
// "ended with tool_use that completed before result event".
func lastAssistantTextAndShape(events []parsedEvent) (text string, hasToolUse bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventTypeAssistant {
			return events[i].AssistantText, events[i].HasAssistantTU
		}
	}
	return "", false
}

// lastVerifyOK reports whether the most-recent tool_use across the transcript
// was zerops_verify AND its tool_result indicates success. Used by rule 4.
//
// "Most recent" means the LAST tool_use globally, not last-per-message — verify
// is typically the closing tool call of a successful flow, but other text-only
// assistant messages may follow it. We scan tool_use events in reverse and
// match the result by id.
func lastVerifyOK(events []parsedEvent) bool {
	// Find the last tool_use across all assistant events.
	var lastID, lastName string
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type != eventTypeAssistant || len(ev.ToolUseNames) == 0 {
			continue
		}
		// Take the LAST tool_use within this message.
		idx := len(ev.ToolUseNames) - 1
		lastName = ev.ToolUseNames[idx]
		lastID = ev.ToolUseIDs[idx]
		break
	}
	if lastID == "" {
		return false
	}
	if !isVerifyTool(lastName) {
		return false
	}
	// Find the matching tool_result by id.
	for _, ev := range events {
		if ev.Type == eventTypeUser && ev.ToolResultID == lastID {
			return verifyResultIndicatesSuccess(ev.ToolResultText)
		}
	}
	return false
}

func isVerifyTool(name string) bool {
	// Tool names appear in stream-json as the bare MCP method name, e.g.
	// "mcp__zerops__zerops_verify". Match by suffix to tolerate transport
	// prefix variations.
	return strings.HasSuffix(name, "zerops_verify")
}

// verifyResultIndicatesSuccess inspects the verify tool_result text for healthy
// signals. Verify returns JSON with checks[] array and overall summary; we
// look for the canonical markers without parsing the full schema, since
// classifier is supposed to stay schema-tolerant.
func verifyResultIndicatesSuccess(resultText string) bool {
	low := strings.ToLower(resultText)
	// Hard-fail markers — verify failed, not done.
	if strings.Contains(low, `"status":"failing"`) || strings.Contains(low, `"status":"failed"`) {
		return false
	}
	if strings.Contains(low, `"isfailing":true`) {
		return false
	}
	// Success markers.
	return strings.Contains(low, `"status":"healthy"`) ||
		strings.Contains(low, `"summary":"all healthy"`) ||
		strings.Contains(low, `"allhealthy":true`)
}

// textTail returns the last n characters of s (full string if shorter).
// Used for question-mark detection to avoid matching `?` deep in a tool-call
// args dump that the agent may have echoed.
func textTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ============================================================================
// User-sim loop — drives multi-turn simulated user when the agent is waiting.
// ============================================================================

// Default user-sim runtime knobs. Override via Scenario.UserSim per-scenario.
const (
	defaultUserSimMaxTurns     = 10
	defaultUserSimStageTimeout = 15 * time.Minute
	defaultUserSimModel        = "claude-haiku-4-5-20251001"
	// loopRepeatRatio — fraction of edit distance to longest-string length
	// below which two normalized agent tails are considered duplicates.
	// 0.15 ≈ "≤15% of chars differ" — calibrated against the original
	// plan's "< 30 edits on 200-char tail" rule (30/200 = 0.15).
	loopRepeatRatio = 0.15
	// loopRepeatMinLen — strings shorter than this can't be ratio-compared
	// reliably (a 4-char question and an unrelated 4-char question share
	// most of the alphabet by chance). For sub-threshold strings we require
	// near-exact equality (≤1 edit) to flag a loop.
	loopRepeatMinLen        = 20
	loopRepeatTailWindow    = 200 // chars of agent text compared
	userSimAgentExcerptCap  = 200 // chars stored in meta.json per turn
	userSimRecentExcerptCap = 800 // chars per recent agent text in prompt
	userSimMaxTurnsKept     = 3   // recent-window depth for prompt assembly
)

// Loop termination reason — written verbatim to meta.json.userSim.terminatedBy.
const (
	TerminatedAgentDone     = "agent_declared_done"
	TerminatedSimSatisfied  = "user_sim_satisfied"
	TerminatedStuckLoop     = "stuck_loop"
	TerminatedMaxIterations = "max_iterations"
	TerminatedAgentError    = "agent_error"
	TerminatedAgentMaxTurns = "agent_max_turns"
	TerminatedTimeout       = "timeout"
	TerminatedUnknown       = "unknown"
)

// defaultPersona is used when scenario.UserPersona is empty. Phrasing aims at
// the most common reasonable-developer profile — happy with sensible defaults,
// pushes back only on goal-violating suggestions.
const defaultPersona = `You are a developer who initiated this task. You want it done with sensible
defaults. Compatible substitutions are fine if the agent explains them.
You'll only push back if something contradicts your stated goal. You don't
know the Zerops platform's internal details, but you know your own goal.`

// satisfactionMarkers are substrings (matched lowercased) in a user-sim reply
// that signal "task complete from the user's perspective". Match → loop ends
// with TerminatedSimSatisfied. Phrases mirror what real users type when
// closing a chat.
var satisfactionMarkers = []string{
	"thanks, looks good",
	"that's all i needed",
	"thats all i needed",
	"all set",
	"perfect, done",
	"looks good, done",
	"thanks, all done",
	"looks good — thanks",
	"looks good - thanks",
	"looks good, thanks",
}

// UserSimRunner produces a single user reply for a fully-formed prompt.
// Implementations are typically a headless `claude` invocation; tests stub
// this to inject canned replies.
type UserSimRunner interface {
	Reply(ctx context.Context, prompt string) (string, error)
}

// AgentResumeFunc resumes the agent's claude session with userMsg and appends
// the resulting events to transcriptFile. Production wires this to
// Runner.spawnClaudeResumeAppend; tests inject a closure that swaps in a
// canned next-state.
type AgentResumeFunc func(ctx context.Context, sessionID, userMsg, transcriptFile string) error

// ClassifyFunc returns the verdict for the current state of transcriptFile.
// Production passes ClassifyTranscriptTail directly; tests can inject for
// branch-specific exercise.
type ClassifyFunc func(transcriptFile string) (Verdict, error)

// UserSimResult is the per-stage user-sim trace persisted in meta.json. Empty
// Turns + TerminatedBy="agent_declared_done" is the happy path: agent didn't
// need user input.
type UserSimResult struct {
	PersonaUsed     string        `json:"personaUsed"`     // "default" | "scenario-override"
	Model           string        `json:"model,omitempty"` // user-sim transport model
	Turns           []UserSimTurn `json:"turns"`
	TerminatedBy    string        `json:"terminatedBy"`
	StuckOnQuestion string        `json:"stuckOnQuestion,omitempty"`
	TotalWallTime   Duration      `json:"totalWallTime"`
}

// UserSimTurn captures one classify→reply→resume cycle. AgentTextExcerpt is
// truncated to userSimAgentExcerptCap so the meta.json stays readable for
// long-question scenarios.
type UserSimTurn struct {
	Iteration        int      `json:"iteration"`
	AgentTextExcerpt string   `json:"agentTextExcerpt"`
	Reply            string   `json:"reply"`
	WallTime         Duration `json:"wallTime"`
}

// runUserSimLoop drives the classify→user-sim→agent-resume cycle until one of
// the termination conditions fires. Pure orchestration — all dependencies
// (sim runner, agent resume, classifier) are injected so the loop is fully
// unit-testable without spawning real `claude` processes. Mutates result in
// place (UserSim sub-struct).
//
// Termination reasons (priority order):
//  1. Agent declared done (verdict=Done) → TerminatedAgentDone
//  2. User-sim reply contains satisfaction marker → TerminatedSimSatisfied
//  3. Loop detected (same agent question hash twice) → TerminatedStuckLoop
//  4. Max user-sim turns hit → TerminatedMaxIterations
//  5. Stage timeout → TerminatedTimeout (driven by ctx deadline upstream)
//  6. Agent error / max-turns from claude headless → TerminatedAgent*
func runUserSimLoop(
	ctx context.Context,
	sc *Scenario,
	sessionID, transcriptFile string,
	sim UserSimRunner,
	resume AgentResumeFunc,
	classify ClassifyFunc,
	result *BehavioralResult,
) error {
	if result.UserSim == nil {
		result.UserSim = &UserSimResult{}
	}
	persona := sc.UserPersona
	if persona == "" {
		result.UserSim.PersonaUsed = "default"
		persona = defaultPersona
	} else {
		result.UserSim.PersonaUsed = "scenario-override"
	}
	maxTurns := defaultUserSimMaxTurns
	if sc.UserSim != nil && sc.UserSim.MaxTurns > 0 {
		maxTurns = sc.UserSim.MaxTurns
	}
	if sc.UserSim != nil && sc.UserSim.Model != "" {
		result.UserSim.Model = sc.UserSim.Model
	} else {
		result.UserSim.Model = defaultUserSimModel
	}

	loopStart := time.Now()
	defer func() {
		result.UserSim.TotalWallTime = Duration(time.Since(loopStart))
	}()

	var prevAgentText string
	for i := 0; i < maxTurns; i++ {
		if err := ctx.Err(); err != nil {
			result.UserSim.TerminatedBy = TerminatedTimeout
			return nil
		}
		verdict, err := classify(transcriptFile)
		if err != nil {
			return fmt.Errorf("classify transcript: %w", err)
		}
		switch verdict.Kind {
		case VerdictDone:
			result.UserSim.TerminatedBy = TerminatedAgentDone
			return nil
		case VerdictError:
			result.UserSim.TerminatedBy = TerminatedAgentError
			return nil
		case VerdictMaxTurns:
			result.UserSim.TerminatedBy = TerminatedAgentMaxTurns
			return nil
		case VerdictWorking:
			// Agent still mid-roundtrip — should not happen if scenario spawn
			// has fully terminated before runUserSimLoop is invoked. Treat
			// defensively: break out so the rest of the pipeline (retrospective)
			// still fires.
			result.UserSim.TerminatedBy = TerminatedUnknown
			return nil
		case VerdictWaiting:
			if isLoopRepeat(prevAgentText, verdict.LastAssistantText) {
				result.UserSim.TerminatedBy = TerminatedStuckLoop
				result.UserSim.StuckOnQuestion = trunc(verdict.LastAssistantText, 400)
				return nil
			}
			prompt := BuildUserSimPrompt(persona, sc.Prompt, verdict.LastAssistantText, result.UserSim.Turns)
			turnStart := time.Now()
			reply, err := sim.Reply(ctx, prompt)
			if err != nil {
				return fmt.Errorf("user-sim reply: %w", err)
			}
			turn := UserSimTurn{
				Iteration:        i + 1,
				AgentTextExcerpt: trunc(verdict.LastAssistantText, userSimAgentExcerptCap),
				Reply:            reply,
				WallTime:         Duration(time.Since(turnStart)),
			}
			result.UserSim.Turns = append(result.UserSim.Turns, turn)

			if isSatisfied(reply) {
				result.UserSim.TerminatedBy = TerminatedSimSatisfied
				return nil
			}
			if err := resume(ctx, sessionID, reply, transcriptFile); err != nil {
				return fmt.Errorf("agent resume: %w", err)
			}
			prevAgentText = verdict.LastAssistantText
		}
	}
	result.UserSim.TerminatedBy = TerminatedMaxIterations
	return nil
}

// BuildUserSimPrompt assembles the prompt body for the user-sim claude call.
// Format follows the template in plans/flow-eval-usersim-2026-05-04.md
// §"User-sim prompt structure". Recent exchanges are reconstructed from
// prior UserSimTurn entries (agent excerpt + user reply pairs) plus the
// current agent message.
func BuildUserSimPrompt(persona, originalPrompt, lastAgentText string, priorTurns []UserSimTurn) string {
	if persona == "" {
		persona = defaultPersona
	}
	var b strings.Builder
	b.WriteString(`You are simulating a real user in a chat with a coding agent. The user
originally said:

  "`)
	b.WriteString(originalPrompt)
	b.WriteString(`"

Your persona:
`)
	b.WriteString(persona)
	b.WriteString(`

The agent has been working on the task and has now turned to ask you
something. Reply as the user would:

- Brief (1-3 sentences max). Real users don't write essays.
- Don't pretend to be helpful or knowledgeable about the platform.
- If the agent suggests a substitution with a clear reason, accept it
  and ask them to mention it in the final summary.
- If the agent's question is unclear, say so plainly.
- If the agent asks permission for something you didn't ask for and
  it's not necessary, push back briefly.
- If the agent's question shows they've finished the work and just
  want a confirmation, say "thanks, looks good" or "that's all I
  needed" — these phrases signal task completion to the runner.
- Never ask the agent for code, configuration, or platform details.
  You're the user, not a developer pair.

Recent conversation (oldest first):

`)

	// Take the last userSimMaxTurnsKept turns from priorTurns (chronological)
	// to build the recent window. Each turn contributes one [agent] line +
	// one [you] line.
	recent := priorTurns
	if len(recent) > userSimMaxTurnsKept {
		recent = recent[len(recent)-userSimMaxTurnsKept:]
	}
	for _, t := range recent {
		b.WriteString("[agent, prior turn]: ")
		b.WriteString(trunc(t.AgentTextExcerpt, userSimRecentExcerptCap))
		b.WriteString("\n[you]: ")
		b.WriteString(trunc(t.Reply, userSimRecentExcerptCap))
		b.WriteString("\n")
	}
	b.WriteString("[agent, most recent]: ")
	b.WriteString(lastAgentText)
	b.WriteString(`

Reply now.`)

	return b.String()
}

// isSatisfied reports whether reply contains a task-complete marker per the
// satisfactionMarkers list. Lowercased substring match.
func isSatisfied(reply string) bool {
	low := strings.ToLower(reply)
	for _, m := range satisfactionMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// isLoopRepeat reports whether prev and current agent texts are near-duplicates.
// Compares the last loopRepeatTailWindow chars of each, normalized
// (lowercased + whitespace collapsed). For strings at least loopRepeatMinLen
// long, the metric is Levenshtein-distance / max-length < loopRepeatRatio
// (≈15%). For shorter strings, ratio is unreliable so we require ≤1 edit
// (near-identical). Empty prev returns false (first iteration cannot repeat).
func isLoopRepeat(prev, current string) bool {
	if prev == "" || current == "" {
		return false
	}
	a := normalizeForLoopCheck(textTail(prev, loopRepeatTailWindow))
	b := normalizeForLoopCheck(textTail(current, loopRepeatTailWindow))
	maxLen := max(len(a), len(b))
	dist := levenshtein(a, b)
	if maxLen < loopRepeatMinLen {
		return dist <= 1
	}
	return float64(dist)/float64(maxLen) < loopRepeatRatio
}

func normalizeForLoopCheck(s string) string {
	low := strings.ToLower(s)
	// Collapse runs of whitespace to single space.
	var b strings.Builder
	prevSpace := false
	for _, r := range low {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// levenshtein computes the edit distance between two strings. Standard
// dynamic-programming implementation; O(len(a)*len(b)) time, O(min) space.
// Used only on tail-200 normalized strings so cost is trivially bounded.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	// Single-row DP, O(min(len(a), len(b))) space.
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt3(a, b, c int) int {
	return min(a, min(b, c))
}

// trunc returns s capped at n runes, with a single-character ellipsis when
// truncated. Byte-aware to keep output ASCII-safe in JSON.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// ============================================================================
// claudeUserSimRunner — production UserSimRunner backed by `claude -p`.
// ============================================================================

// claudeUserSimRunner spawns `claude -p <prompt> --output-format stream-json
// --no-session-persistence --max-turns 1 --model <model>` for each Reply.
// No MCP config — the user-sim must not invoke project tools, only respond
// as the user. Output is parsed for the assistant text events.
type claudeUserSimRunner struct {
	model     string
	extraArgs []string
}

// NewClaudeUserSimRunner returns a UserSimRunner that exec's `claude` with
// the given model. Empty model → defaultUserSimModel.
func NewClaudeUserSimRunner(model string) UserSimRunner {
	if model == "" {
		model = defaultUserSimModel
	}
	return &claudeUserSimRunner{model: model}
}

func (c *claudeUserSimRunner) Reply(ctx context.Context, prompt string) (string, error) {
	args := make([]string, 0, 11+len(c.extraArgs))
	args = append(args,
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--max-turns", "1",
		"--model", c.model,
	)
	args = append(args, c.extraArgs...)

	cmd := exec.CommandContext(ctx, "claude", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude user-sim: %w", err)
	}
	reply, err := extractAssistantTextFromStream(out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply), nil
}

// extractAssistantTextFromStream parses stream-json bytes and returns the
// concatenated text of assistant events. Used by claudeUserSimRunner; mirrors
// extractSelfReview for the retrospective path but takes bytes instead of
// a file (the user-sim output is streamed in-memory rather than persisted).
func extractAssistantTextFromStream(data []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	var parts []string
	for scanner.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != eventTypeAssistant {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type == contentTypeText && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan user-sim output: %w", err)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("user-sim returned no assistant text events")
	}
	return strings.Join(parts, "\n\n"), nil
}
