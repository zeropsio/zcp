package observer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// StepKind is the kind of one numbered step (docs/spec-eval-farm.md §7.2).
type StepKind string

const (
	StepUser     StepKind = "user"
	StepThinking StepKind = "thinking"
	StepAgent    StepKind = "agent"
	StepTool     StepKind = "tool"
)

// noResultRecorded is what a tool step's result reads when no tool_result
// block in the transcript shares its tool_use_id (§7.2 point 3).
const noResultRecorded = "(no result recorded)"

// noUserSimTextRecorded is what a resumed segment's synthesized user step
// reads when meta.json carries no reply for that segment (§7.2 point 2).
const noUserSimTextRecorded = "(user-sim turn; text not recorded)"

// Step is one numbered unit of a run's record (docs/spec-eval-farm.md §7.2).
// Numbering is 1-based and a pure function of the bundle's bytes
// (TestSteps_SameBytesSameNumbers).
type Step struct {
	N    int
	Kind StepKind

	// Text carries the step's content for kind user/thinking/agent.
	Text string

	// Tool* carry a kind-tool step's content. ToolInputJSON is the
	// transcript's raw input JSON, compacted — never decoded and
	// re-encoded (§7.3), so a quote copied from it matches the step
	// byte-for-byte. ToolHasResult distinguishes a genuinely empty result
	// from "no matching tool_result block found" (ToolResultText reads
	// noResultRecorded in the latter case).
	ToolName       string
	ToolInputJSON  string
	ToolResultText string
	ToolIsError    bool
	ToolHasResult  bool
}

// rawEvent is the subset of one transcript.jsonl line's fields BuildSteps
// needs. Claude Code's own stream-json schema, not negotiable here.
type rawEvent struct {
	Type    string      `json:"type"`
	Subtype string      `json:"subtype"`
	Message *rawMessage `json:"message"`
}

type rawMessage struct {
	Content []rawContentBlock `json:"content"`
}

type rawContentBlock struct {
	Type string `json:"type"`

	// thinking / text blocks.
	Thinking string `json:"thinking"`
	Text     string `json:"text"`

	// tool_use blocks.
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`

	// tool_result blocks.
	ToolUseID string          `json:"tool_use_id"` //nolint:tagliatelle // upstream Claude Code schema
	IsError   bool            `json:"is_error"`    //nolint:tagliatelle // upstream Claude Code schema
	Content   json.RawMessage `json:"content"`
}

// toolResult is one tool_result block's resolved text + error flag.
type toolResult struct {
	text    string
	isError bool
}

// BuildSteps numbers one run's record into steps (docs/spec-eval-farm.md
// §7.2): step 1 is always the synthesized task-prompt user step; the
// transcript (Claude Code stream-json) is then read in file order, each
// resumed segment (a system/init event after the first) contributing one
// synthesized user step from resumeReplies, and each assistant event
// contributing one step per content block. resumeReplies[k] is the k-th
// (0-based) resumed segment's reply text (meta.json.userSim.turns[k].reply);
// a segment past the end of resumeReplies reads noUserSimTextRecorded.
func BuildSteps(taskPrompt string, transcript []byte, resumeReplies []string) ([]Step, error) {
	events, err := parseEvents(transcript)
	if err != nil {
		return nil, err
	}
	results := collectToolResults(events)

	steps := []Step{{N: 1, Kind: StepUser, Text: taskPrompt}}
	n := 1
	initsSeen := 0

	for _, ev := range events {
		switch {
		case ev.Type == "system" && ev.Subtype == "init":
			initsSeen++
			if initsSeen == 1 {
				continue // the run's own start, not a resumed segment
			}
			k := initsSeen - 2
			reply := noUserSimTextRecorded
			if k >= 0 && k < len(resumeReplies) {
				reply = resumeReplies[k]
			}
			n++
			steps = append(steps, Step{N: n, Kind: StepUser, Text: reply})

		case ev.Type == "assistant" && ev.Message != nil:
			for _, block := range ev.Message.Content {
				n++
				steps = append(steps, buildAssistantStep(n, block, results))
			}
		}
	}
	return steps, nil
}

// buildAssistantStep converts one assistant content block into its step,
// per block type (§7.2 point 3).
func buildAssistantStep(n int, block rawContentBlock, results map[string]toolResult) Step {
	switch block.Type {
	case "thinking":
		return Step{N: n, Kind: StepThinking, Text: block.Thinking}
	case "tool_use":
		input := compactJSON(block.Input)
		res, found := results[block.ID]
		if !found {
			return Step{
				N: n, Kind: StepTool,
				ToolName: block.Name, ToolInputJSON: input,
				ToolResultText: noResultRecorded, ToolHasResult: false,
			}
		}
		return Step{
			N: n, Kind: StepTool,
			ToolName: block.Name, ToolInputJSON: input,
			ToolResultText: res.text, ToolIsError: res.isError, ToolHasResult: true,
		}
	default: // "text" and anything else render as an agent step
		return Step{N: n, Kind: StepAgent, Text: block.Text}
	}
}

// parseEvents reads transcript.jsonl line by line. A blank line is skipped;
// a malformed line is an error (the transcript is a required input, §7.2).
func parseEvents(transcript []byte) ([]rawEvent, error) {
	var events []rawEvent
	for i, line := range bytes.Split(transcript, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("transcript.jsonl line %d: %w", i+1, err)
		}
		events = append(events, ev)
	}
	return events, nil
}

// collectToolResults scans every event for tool_result blocks (they live on
// "user" events) and indexes them by tool_use_id, in file order — a first
// pass so a tool_use step can look its result up regardless of relative
// position.
func collectToolResults(events []rawEvent) map[string]toolResult {
	results := make(map[string]toolResult)
	for _, ev := range events {
		if ev.Type != "user" || ev.Message == nil {
			continue
		}
		for _, block := range ev.Message.Content {
			if block.Type != "tool_result" || block.ToolUseID == "" {
				continue
			}
			results[block.ToolUseID] = toolResult{
				text:    toolResultText(block.Content),
				isError: block.IsError,
			}
		}
	}
	return results
}

// toolResultText concatenates a tool_result block's text content items, in
// order. The Anthropic content shape is an array of {"type":"text","text":
// "…"} objects; a plain JSON string is accepted as a fallback for a
// differently-shaped transport.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var items []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &items); err == nil {
		var b strings.Builder
		for _, item := range items {
			if item.Type != "" && item.Type != "text" {
				continue
			}
			b.WriteString(item.Text)
		}
		return b.String()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// compactJSON returns raw compacted (json.Compact) — never decoded and
// re-encoded, per §7.3, so a quote copied from the digest matches the step
// byte-for-byte. Empty/absent input reads "{}".
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
