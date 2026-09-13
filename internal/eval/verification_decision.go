package eval

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// toolArgRowKind names which of ToolArgEntry's three shapes an entry
// declares, and builds the `tool_arg/<kind>/<expr>` row id (FM-59, FM-60).
type toolArgRowKind string

const (
	toolArgNever  toolArgRowKind = "never"
	toolArgAlways toolArgRowKind = "always"
	toolArgMax    toolArgRowKind = "max"
)

// toolArgEntryShape resolves which shape entry declares and the call-shape
// expression it names. ok is false for a malformed entry (none of
// Never/Always/(Max+Call) set) — scenario.go's validate() is expected to
// reject that before it reaches here, but this function stays total.
func toolArgEntryShape(entry ToolArgEntry) (kind toolArgRowKind, expr string, ok bool) {
	switch {
	case entry.Never != "":
		return toolArgNever, entry.Never, true
	case entry.Always != "":
		return toolArgAlways, entry.Always, true
	case entry.Call != "":
		return toolArgMax, entry.Call, true
	default:
		return "", "", false
	}
}

// evaluateToolArgRows grades every toolArg entry against transcript.jsonl
// (docs/spec-eval-farm.md §4.1/§4.2 FM-59/FM-60): `never` fails on one
// matching call, `always` fails on zero matching calls, `max: N` fails on
// more than N matching calls. A run with no transcript freezes every row
// blocked, never passed (FM-59).
func evaluateToolArgRows(entries []ToolArgEntry, transcriptPath string, _ []capture.MCPToolCall, _ bool, now time.Time) []RequiredCheck {
	uses, present := ReadTranscriptToolUses(transcriptPath)
	rows := make([]RequiredCheck, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, evaluateToolArgRow(entry, uses, present, now))
	}
	return rows
}

func evaluateToolArgRow(entry ToolArgEntry, uses []TranscriptToolUse, transcriptPresent bool, now time.Time) RequiredCheck {
	kind, expr, ok := toolArgEntryShape(entry)
	if !ok {
		return RequiredCheck{
			ID: "tool_arg/invalid", Check: "toolArg", Result: CheckBlocked, ObservedAt: now, Source: "transcript",
			Message: "toolArg entry declares none of never/always/max+call",
		}
	}
	id := fmt.Sprintf("tool_arg/%s/%s", kind, expr)
	check := "toolArg"
	if !transcriptPresent {
		return RequiredCheck{ID: id, Check: check, Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "transcript", Message: "no transcript.jsonl for this run"}
	}
	shape, err := ParseCallShape(expr)
	if err != nil {
		// scenario.go's validate() rejects an unparseable expression at
		// load time; reaching here with one is a programming error, not a
		// run-time state — this function stays total, never panics.
		return RequiredCheck{ID: id, Check: check, Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "transcript", Message: fmt.Sprintf("unparseable call-shape expression: %v", err)}
	}
	matches := matchingToolUses(shape, uses)

	switch kind {
	case toolArgNever:
		if len(matches) > 0 {
			return RequiredCheck{
				ID: id, Check: check, Scope: expr, Result: CheckFailed, ObservedAt: now, Source: "transcript",
				Expected: "never called", Observed: describeToolUse(matches[0]),
				Message: fmt.Sprintf("forbidden call %s matched %d time(s)", expr, len(matches)),
			}
		}
		return RequiredCheck{
			ID: id, Check: check, Scope: expr, Result: CheckPassed, ObservedAt: now, Source: "transcript",
			Expected: "never called", Observed: "not called",
			Message: fmt.Sprintf("no call matched %s", expr),
		}
	case toolArgAlways:
		if len(matches) == 0 {
			return RequiredCheck{
				ID: id, Check: check, Scope: expr, Result: CheckFailed, ObservedAt: now, Source: "transcript",
				Expected: "called at least once", Observed: "not called",
				Message: fmt.Sprintf("no call matched required shape %s", expr),
			}
		}
		return RequiredCheck{
			ID: id, Check: check, Scope: expr, Result: CheckPassed, ObservedAt: now, Source: "transcript",
			Expected: "called at least once", Observed: describeToolUse(matches[0]),
			Message: fmt.Sprintf("%d call(s) matched %s", len(matches), expr),
		}
	case toolArgMax:
		if len(matches) > entry.Max {
			return RequiredCheck{
				ID: id, Check: check, Scope: expr, Result: CheckFailed, ObservedAt: now, Source: "transcript",
				Expected: fmt.Sprintf("at most %d call(s)", entry.Max), Observed: describeToolUse(matches[0]),
				Message: fmt.Sprintf("%s matched %d time(s), max %d", expr, len(matches), entry.Max),
			}
		}
		return RequiredCheck{
			ID: id, Check: check, Scope: expr, Result: CheckPassed, ObservedAt: now, Source: "transcript",
			Expected: fmt.Sprintf("at most %d call(s)", entry.Max),
			Message:  fmt.Sprintf("%s matched %d time(s), within max %d", expr, len(matches), entry.Max),
		}
	default:
		return RequiredCheck{ID: id, Check: check, Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "transcript", Message: "unreachable toolArg kind"}
	}
}

// matchingToolUses returns every use in uses that shape.Matches accepts.
// TranscriptToolUse carries no MCP-only fields (Action, ResultText, …), so
// it is adapted into the capture.MCPToolCall shape Matches already knows —
// the same matcher grades a `never` entry (over the captured MCP stream)
// and a `toolArg{never}` entry (over the transcript) identically for the
// same call-shape expression.
func matchingToolUses(shape CallShape, uses []TranscriptToolUse) []TranscriptToolUse {
	var out []TranscriptToolUse
	for _, use := range uses {
		if shape.Matches(capture.MCPToolCall{Tool: use.Tool, Arguments: use.Input}) {
			out = append(out, use)
		}
	}
	return out
}

// describeToolUse renders a compact, human-scannable description of a
// matched transcript tool_use for a toolArg row's Observed field: tool name
// plus its JSON-compact input, capped at 200 characters.
func describeToolUse(use TranscriptToolUse) string {
	argsText := "{}"
	if data, err := json.Marshal(use.Input); err == nil {
		argsText = string(data)
	}
	return truncate(fmt.Sprintf("%s %s", use.Tool, argsText), 200)
}

// evaluateToolResultRows grades every toolResult entry against the run's
// captured MCP stream (docs/spec-eval-farm.md §4.1/§4.2 FM-59): `contains`
// passes iff some call to the named tool has a ResultText containing the
// substring. No captured stream freezes every row blocked, never passed.
func evaluateToolResultRows(entries []ToolResultEntry, _ string, calls []capture.MCPToolCall, streamPresent bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, evaluateToolResultRow(entry, calls, streamPresent, now))
	}
	return rows
}

func evaluateToolResultRow(entry ToolResultEntry, calls []capture.MCPToolCall, streamPresent bool, now time.Time) RequiredCheck {
	id := fmt.Sprintf("tool_result/%s/%s", entry.Tool, entry.Contains)
	if !streamPresent {
		return RequiredCheck{ID: id, Check: "toolResult", Scope: entry.Tool, Result: CheckBlocked, ObservedAt: now, Source: "mcp-stream", Message: "no captured MCP stream for this run"}
	}
	for _, call := range calls {
		if call.Tool != entry.Tool {
			continue
		}
		if strings.Contains(call.ResultText, entry.Contains) {
			return RequiredCheck{
				ID: id, Check: "toolResult", Scope: entry.Tool, Result: CheckPassed, ObservedAt: now, Source: "mcp-stream",
				Expected: fmt.Sprintf("result contains %q", entry.Contains), Observed: truncate(call.ResultText, 200),
				Message: fmt.Sprintf("a call to %s produced a result containing %q", entry.Tool, entry.Contains),
			}
		}
	}
	return RequiredCheck{
		ID: id, Check: "toolResult", Scope: entry.Tool, Result: CheckFailed, ObservedAt: now, Source: "mcp-stream",
		Expected: fmt.Sprintf("result contains %q", entry.Contains), Observed: "no matching call",
		Message: fmt.Sprintf("no call to %s produced a result containing %q", entry.Tool, entry.Contains),
	}
}

// evaluateMustOfferRows grades every mustOffer regex against the run's
// captured MCP stream (docs/spec-eval-farm.md §4.1/§4.2 FM-59): passed iff
// the regex matches at least one captured call's ResultText. No captured
// stream freezes every row blocked, never passed.
func evaluateMustOfferRows(entries []string, _ string, calls []capture.MCPToolCall, streamPresent bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i, expr := range entries {
		rows = append(rows, evaluateMustOfferRow(i+1, expr, calls, streamPresent, now))
	}
	return rows
}

func evaluateMustOfferRow(n int, expr string, calls []capture.MCPToolCall, streamPresent bool, now time.Time) RequiredCheck {
	id := fmt.Sprintf("must_offer/%d", n)
	if !streamPresent {
		return RequiredCheck{ID: id, Check: "mustOffer", Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "mcp-stream", Message: "no captured MCP stream for this run"}
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return RequiredCheck{ID: id, Check: "mustOffer", Scope: expr, Result: CheckBlocked, ObservedAt: now, Source: "mcp-stream", Message: fmt.Sprintf("invalid regex %q: %v", expr, err)}
	}
	for _, call := range calls {
		if re.MatchString(call.ResultText) {
			return RequiredCheck{
				ID: id, Check: "mustOffer", Scope: expr, Result: CheckPassed, ObservedAt: now, Source: "mcp-stream",
				Expected: fmt.Sprintf("a result matches /%s/", expr), Observed: truncate(call.ResultText, 200),
				Message: fmt.Sprintf("a captured result matched /%s/", expr),
			}
		}
	}
	return RequiredCheck{
		ID: id, Check: "mustOffer", Scope: expr, Result: CheckFailed, ObservedAt: now, Source: "mcp-stream",
		Expected: fmt.Sprintf("a result matches /%s/", expr), Observed: "no matching result",
		Message: fmt.Sprintf("no captured result matched /%s/", expr),
	}
}

// truncate caps s at n bytes, used to keep a RequiredCheck's Observed field
// scannable (FM-59: "compact args cap 200 chars").
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
