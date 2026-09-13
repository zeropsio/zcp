package eval

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// TranscriptToolUse is one tool_use block read from a run's transcript.jsonl
// (docs/spec-eval-farm.md §4.2 FM-59): every assistant tool call, MCP tools
// AND the agent's own Bash/Read/… tool_use blocks alike. Tool is normalised
// (see ReadTranscriptToolUses); Input is the block's raw arguments object;
// At is the owning stream-json event's timestamp, zero when the transcript
// line carries none.
type TranscriptToolUse struct {
	Tool  string
	Input map[string]any
	At    time.Time
}

// transcriptEvent is the minimal stream-json shape ReadTranscriptToolUses
// needs from one line of transcript.jsonl (claude --output-format
// stream-json): the type discriminator, an optional per-line timestamp, and
// — for an assistant event — the message's content blocks.
type transcriptEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content []transcriptContentBlock `json:"content"`
	} `json:"message"`
}

// transcriptContentBlock is one message.content[] entry. Only tool_use
// blocks (Type == contentTypeToolUse) carry Name/Input; other block types
// (text, tool_result, …) are skipped by ReadTranscriptToolUses.
type transcriptContentBlock struct {
	Type  string         `json:"type"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ReadTranscriptToolUses reads path (a run's transcript.jsonl, one
// stream-json event per line — docs/spec-eval-farm.md §4.2 FM-59) and
// returns every tool_use block across all assistant events, in file order.
// A line that fails to parse as JSON, or whose type isn't "assistant", is
// skipped — never fatal, so one corrupt line never loses the rest of the
// transcript. present is false only when path itself could not be read (the
// "no transcript" case FM-59's toolArg rows must block on, never silently
// pass as zero uses); present is true even if every line turns out
// malformed or carries no tool_use block, in which case uses is nil.
func ReadTranscriptToolUses(path string) (uses []TranscriptToolUse, present bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22) // 4MiB cap per line, matching extractSessionID's reader
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event transcriptEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Type != eventTypeAssistant {
			continue
		}
		at := parseTranscriptTimestamp(event.Timestamp)
		for _, block := range event.Message.Content {
			if block.Type != contentTypeToolUse {
				continue
			}
			uses = append(uses, TranscriptToolUse{
				Tool:  normaliseTranscriptToolName(block.Name),
				Input: block.Input,
				At:    at,
			})
		}
	}
	return uses, true
}

// parseTranscriptTimestamp parses a stream-json event's "timestamp" field
// (RFC3339Nano). Empty or unparsable input yields the zero time rather than
// an error — a per-line timestamp is a courtesy, not a guarantee.
func parseTranscriptTimestamp(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// normaliseTranscriptToolName strips an MCP tool's server prefix so
// "mcp__zerops__zerops_deploy" and "mcp__zcp__zerops_deploy" both normalise
// to "zerops_deploy" — the same bare name capture.MCPToolCall.Tool carries
// for the same call, so a `never`/toolArg{never} shape grades identically
// against either source. Anything not starting with "mcp__" (the agent's
// own tools — Bash, Read, …) is returned unchanged.
func normaliseTranscriptToolName(name string) string {
	if !strings.HasPrefix(name, "mcp__") {
		return name
	}
	idx := strings.LastIndex(name, "__")
	if idx < 0 {
		return name
	}
	return name[idx+2:]
}
