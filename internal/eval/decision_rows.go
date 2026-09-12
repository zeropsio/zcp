package eval

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// CallShape is one parsed `never`/coverage call-shape expression
// (docs/spec-eval-farm.md §4.1 FM-30). Grammar:
//
//	<tool>                   — matches any call to that tool
//	<tool>{k=v,k2=v2}        — matches a call to that tool where, for every
//	                            k=v pair, arguments[k] JSON-stringified
//	                            equals v exactly (string comparison)
//
// Anything else is a parse error at scenario load (scenario.go's validate,
// FM-30/FM-32).
type CallShape struct {
	Tool string
	Args map[string]string
	// argOrder preserves the source order of Args for a stable String().
	argOrder []string
}

// callShapePattern matches "<tool>" or "<tool>{k=v,k2=v2,...}". Tool and
// key names are identifier-shaped (letters, digits, underscore); values are
// anything but "}" or ",".
var callShapePattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:\{([^}]*)\})?$`)

// ParseCallShape parses one call-shape expression. Returns an error whose
// message names the offending expression for any input that isn't
// `<tool>` or `<tool>{k=v,...}`.
func ParseCallShape(expr string) (CallShape, error) {
	match := callShapePattern.FindStringSubmatch(strings.TrimSpace(expr))
	if match == nil {
		return CallShape{}, fmt.Errorf("invalid call-shape expression %q (want `<tool>` or `<tool>{k=v,k2=v2}`)", expr)
	}
	shape := CallShape{Tool: match[1], Args: map[string]string{}}
	argsBlob := match[2]
	if argsBlob == "" {
		return shape, nil
	}
	for pair := range strings.SplitSeq(argsBlob, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			return CallShape{}, fmt.Errorf("invalid call-shape expression %q: empty argument constraint", expr)
		}
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return CallShape{}, fmt.Errorf("invalid call-shape expression %q: argument constraint %q must be k=v", expr, pair)
		}
		value = strings.TrimSpace(value)
		if _, exists := shape.Args[key]; exists {
			return CallShape{}, fmt.Errorf("invalid call-shape expression %q: duplicate key %q", expr, key)
		}
		shape.Args[key] = value
		shape.argOrder = append(shape.argOrder, key)
	}
	return shape, nil
}

// String reconstructs the canonical call-shape expression, used to build
// the `decision/<expr>` row id (FM-30).
func (c CallShape) String() string {
	if len(c.argOrder) == 0 {
		return c.Tool
	}
	parts := make([]string, 0, len(c.argOrder))
	for _, key := range c.argOrder {
		parts = append(parts, fmt.Sprintf("%s=%s", key, c.Args[key]))
	}
	return fmt.Sprintf("%s{%s}", c.Tool, strings.Join(parts, ","))
}

// Matches reports whether call satisfies the shape: the tool name matches
// exactly, and every k=v argument constraint holds — v is compared as a
// string against the JSON-stringified value of arguments[k] (FM-30).
func (c CallShape) Matches(call capture.MCPToolCall) bool {
	if call.Tool != c.Tool {
		return false
	}
	for key, want := range c.Args {
		got, ok := call.Arguments[key]
		if !ok {
			return false
		}
		if jsonScalarText(got) != want {
			return false
		}
	}
	return true
}

// jsonScalarText renders an argument value the way a call-shape constraint
// compares against it: JSON-stringified, with a bare string unwrapped from
// its surrounding quotes so `override=true` matches a JSON bool AND a JSON
// string "true" the same way an author would expect from reading the
// expression.
func jsonScalarText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

// decisionRowID builds the stable row id for a `never` decision row
// (docs/spec-eval-farm.md §4.1 FM-30).
func decisionRowID(expr string) string {
	return "decision/" + expr
}

// EvaluateNeverRows grades every `never` call-shape expression against the
// run's captured MCP tool calls (FM-30): one matching call anywhere in the
// stream fails the row (`decision/<expr>`); zero matching calls passes it
// (the check is executable whenever the run produced any captured stream);
// no captured stream at all blocks it (never a silent skip). Gating — the
// returned rows belong in the scenario's aggregated check set
// (aggregateTaskResult, docs/spec-testing-architecture.md §10.1).
func EvaluateNeverRows(never []string, calls []capture.MCPToolCall, streamPresent bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(never))
	for _, expr := range never {
		rows = append(rows, evaluateNeverRow(expr, calls, streamPresent, now))
	}
	return rows
}

func evaluateNeverRow(expr string, calls []capture.MCPToolCall, streamPresent bool, now time.Time) RequiredCheck {
	id := decisionRowID(expr)
	if !streamPresent {
		return RequiredCheck{ID: id, Check: "decision_never", Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "mcpstream", Message: "no captured MCP stream for this run"}
	}
	shape, err := ParseCallShape(expr)
	if err != nil {
		// scenario.go's validate() rejects this at parse time; a bad
		// expression reaching here is a programming error, not a run-time
		// blocked state — but this function stays total, never panics.
		return RequiredCheck{ID: id, Check: "decision_never", Scope: expr, Result: CheckBlocked, ObservedAt: now, Message: fmt.Sprintf("unparseable call-shape expression: %v", err)}
	}
	for _, call := range calls {
		if shape.Matches(call) {
			return RequiredCheck{
				ID: id, Check: "decision_never", Scope: expr,
				Result: CheckFailed, Expected: "never called", Observed: "called", ObservedAt: now, Source: "mcpstream",
				Message: fmt.Sprintf("forbidden call %s matched: tool=%s action=%s", expr, call.Tool, call.Action),
			}
		}
	}
	return RequiredCheck{
		ID: id, Check: "decision_never", Scope: expr,
		Result: CheckPassed, Expected: "never called", Observed: "not called", ObservedAt: now, Source: "mcpstream",
		Message: fmt.Sprintf("no call matched %s", expr),
	}
}

// askWhenRowID builds the stable row id for an `askWhen` advisory row
// (docs/spec-eval-farm.md §4.1 FM-31).
func askWhenRowID(errorCode string) string {
	return "decision/askWhen/" + errorCode
}

// AskWhenObservation is the correlated evidence needed to grade one
// askWhen row (FM-31): whether ErrorCode appeared anywhere in the run, and
// if so, whether a user-simulation turn occurred between that appearance
// and the next mutating MCP call. Building UserSimTurnAsked requires
// correlating two capture channels by wall-clock time — the MCP tool-call
// stream this package reads (capture.MCPToolCall carries no timestamp) and the
// lifecycle-marker stream that records each user-sim invocation
// (internal/eval/capture.go, internal/eval/behavioral_capture_phases.go) —
// which is run-orchestration state outside this slice's write-set,
// so a caller assembles this value; EvaluateAskWhenRow only grades it.
type AskWhenObservation struct {
	ErrorCode        string
	ErrorSeen        bool
	UserSimTurnAsked bool
}

// EvaluateAskWhenRows grades every askWhen entry advisory-only (FM-31): the
// returned rows must NEVER be passed into aggregateTaskResult — §10.1's
// aggregation applies only to outcome rows and to `never` rows. Keep these
// in the verification.json Advisory list (or print them via `report`)
// instead.
func EvaluateAskWhenRows(observations []AskWhenObservation, streamPresent bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(observations))
	for _, obs := range observations {
		rows = append(rows, EvaluateAskWhenRow(obs, streamPresent, now))
	}
	return rows
}

// EvaluateAskWhenRow grades one askWhen observation. See AskWhenObservation
// and EvaluateAskWhenRows for the advisory-only contract.
func EvaluateAskWhenRow(obs AskWhenObservation, streamPresent bool, now time.Time) RequiredCheck {
	id := askWhenRowID(obs.ErrorCode)
	check := "decision_ask_when"
	if !streamPresent {
		return RequiredCheck{ID: id, Check: check, Scope: obs.ErrorCode, Result: CheckBlocked, ObservedAt: now, Source: "mcpstream", Message: "no captured MCP stream for this run"}
	}
	if !obs.ErrorSeen {
		return RequiredCheck{ID: id, Check: check, Scope: obs.ErrorCode, Result: CheckPassed, Expected: "n/a", Observed: "error code not seen", ObservedAt: now, Source: "mcpstream", Message: fmt.Sprintf("error code %s did not occur", obs.ErrorCode)}
	}
	if obs.UserSimTurnAsked {
		return RequiredCheck{ID: id, Check: check, Scope: obs.ErrorCode, Result: CheckPassed, Expected: "user-sim turn before next mutating call", Observed: "asked", ObservedAt: now, Source: "mcpstream+usersim", Message: fmt.Sprintf("agent asked the user-sim after %s before its next mutating call", obs.ErrorCode)}
	}
	return RequiredCheck{ID: id, Check: check, Scope: obs.ErrorCode, Result: CheckFailed, Expected: "user-sim turn before next mutating call", Observed: "not asked", ObservedAt: now, Source: "mcpstream+usersim", Message: fmt.Sprintf("agent did not ask the user-sim after %s before its next mutating call", obs.ErrorCode)}
}

// BuildAskWhenObservations correlates every verification.askWhen error code
// against calls (the run's captured MCP tool-call stream, chronological)
// and turns (the user-sim loop's recorded turns) to produce the
// AskWhenObservation values EvaluateAskWhenRows grades (docs/spec-eval-farm.md
// §4.1 FM-31). mutatingTools names the tools whose annotations mark them
// non-read-only (internal/tools.MutatingToolNames()) — the vocabulary for
// "the next mutating call".
func BuildAskWhenObservations(codes []string, calls []capture.MCPToolCall, turns []UserSimTurn, mutatingTools map[string]bool) []AskWhenObservation {
	observations := make([]AskWhenObservation, 0, len(codes))
	for _, code := range codes {
		observations = append(observations, buildAskWhenObservation(code, calls, turns, mutatingTools))
	}
	return observations
}

// buildAskWhenObservation implements FM-31's rule, stated exactly:
//
//   - The trigger is the FIRST call in calls whose ResultText contains the
//     JSON fragment `"code":"<code>"` for THIS entry's code. No trigger →
//     ErrorSeen=false. A call's ResultIsError alone never anchors the
//     window — only the coded fragment identifies that a call is this
//     entry's trigger, so an earlier, unrelated error never wrongly
//     anchors it.
//   - UserSimTurnAsked is true iff a turn's StartedAt falls strictly after
//     the trigger call's At and strictly before the At of the next call
//     (following the trigger, by index — calls is assumed chronological)
//     whose Tool is in mutatingTools, or there is no such later mutating
//     call (the window is then open-ended).
func buildAskWhenObservation(code string, calls []capture.MCPToolCall, turns []UserSimTurn, mutatingTools map[string]bool) AskWhenObservation {
	fragment := fmt.Sprintf(`"code":"%s"`, code)
	triggerIndex := -1
	var triggerAt time.Time
	for i, call := range calls {
		if strings.Contains(call.ResultText, fragment) {
			triggerIndex = i
			triggerAt = call.At
			break
		}
	}
	if triggerIndex == -1 {
		return AskWhenObservation{ErrorCode: code, ErrorSeen: false}
	}

	var nextMutationAt time.Time
	hasNextMutation := false
	for i := triggerIndex + 1; i < len(calls); i++ {
		if mutatingTools[calls[i].Tool] {
			nextMutationAt = calls[i].At
			hasNextMutation = true
			break
		}
	}

	for _, turn := range turns {
		if !turn.StartedAt.After(triggerAt) {
			continue
		}
		if hasNextMutation && !turn.StartedAt.Before(nextMutationAt) {
			continue
		}
		return AskWhenObservation{ErrorCode: code, ErrorSeen: true, UserSimTurnAsked: true}
	}
	return AskWhenObservation{ErrorCode: code, ErrorSeen: true, UserSimTurnAsked: false}
}

// LoadScenarioMCPCalls locates and reads every mcp/zcp-<pid>.jsonl capture
// file under sessionDir whose records are tagged with evalRunID/
// scenarioRunID, and returns their tool calls concatenated in chronological
// order (one file per agent invocation — the initial run plus every
// resume, each spawning its own MCP server process with its own pid).
// present is false only when sessionDir is empty or no file under it
// belongs to this scenario run — the "no captured stream" case the
// decision rows must block on, never silently pass.
func LoadScenarioMCPCalls(sessionDir, evalRunID, scenarioRunID string) (calls []capture.MCPToolCall, present bool, err error) {
	paths, err := ScenarioMCPStreamPaths(sessionDir, evalRunID, scenarioRunID)
	if err != nil {
		return nil, false, err
	}
	if len(paths) == 0 {
		return nil, false, nil
	}
	for _, path := range paths {
		fileCalls, readErr := capture.ReadMCPStream(path)
		if readErr != nil {
			return nil, false, fmt.Errorf("read MCP stream %s: %w", path, readErr)
		}
		calls = append(calls, fileCalls...)
	}
	return calls, true, nil
}

// ScenarioMCPStreamPaths locates every mcp/zcp-<pid>.jsonl capture file
// under sessionDir whose records are tagged with evalRunID/scenarioRunID,
// returned in chronological order (one file per agent invocation — the
// initial run plus every resume, each spawning its own MCP server process
// with its own pid). Shared by LoadScenarioMCPCalls (which reads and
// concatenates the calls) and behavioral_run.go's RuntimeInputs assembly
// (which needs the raw paths for O6/O8's own reads). An empty sessionDir or
// no owning file returns a nil, non-error result.
func ScenarioMCPStreamPaths(sessionDir, evalRunID, scenarioRunID string) ([]string, error) {
	if sessionDir == "" {
		return nil, nil
	}
	paths, err := filepath.Glob(filepath.Join(sessionDir, "mcp", "zcp-*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob MCP capture files: %w", err)
	}
	type scoped struct {
		path  string
		start time.Time
	}
	var owned []scoped
	for _, path := range paths {
		records, readErr := capture.ReadRecords(path)
		if readErr != nil {
			return nil, fmt.Errorf("read MCP capture %s: %w", path, readErr)
		}
		belongs, start := scenarioOwnsStream(records, evalRunID, scenarioRunID)
		if belongs {
			owned = append(owned, scoped{path: path, start: start})
		}
	}
	if len(owned) == 0 {
		return nil, nil
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].start.Before(owned[j].start) })
	out := make([]string, len(owned))
	for i, o := range owned {
		out[i] = o.path
	}
	return out, nil
}

// scenarioOwnsStream reports whether any record in records is tagged with
// evalRunID/scenarioRunID, and returns the earliest record's time — used to
// order multiple invocation files chronologically.
func scenarioOwnsStream(records []capture.Record, evalRunID, scenarioRunID string) (bool, time.Time) {
	var earliest time.Time
	owns := false
	for _, record := range records {
		if record.EvalRunID == evalRunID && record.ScenarioRunID == scenarioRunID {
			owns = true
		}
		if earliest.IsZero() || record.Time.Before(earliest) {
			earliest = record.Time
		}
	}
	return owns, earliest
}
