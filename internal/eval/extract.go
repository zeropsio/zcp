package eval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractAssessment scans a stream-json JSONL log for the agent's self-assessment.
// It searches all assistant messages for text containing "## EVAL REPORT".
// Returns the assessment text and whether it was found.
func ExtractAssessment(log string) (string, bool) {
	if log == "" {
		return "", false
	}

	var lastAssessment string

	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg streamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Type != "assistant" {
			continue
		}

		for _, block := range msg.Message.Content {
			if block.Type != "text" {
				continue
			}
			if idx := strings.Index(block.Text, "## EVAL REPORT"); idx >= 0 {
				lastAssessment = block.Text[idx:]
			}
		}
	}

	if lastAssessment == "" {
		return "", false
	}
	return lastAssessment, true
}

// ExtractToolCalls parses stream-json JSONL and returns an ordered list of tool calls
// with their results matched by tool_use_id.
func ExtractToolCalls(log string) []ToolCall {
	if log == "" {
		return nil
	}

	// First pass: collect pending tool uses
	type pendingTool struct {
		name  string
		input string
	}
	pending := make(map[string]pendingTool) // tool_use_id → pending
	var order []string                      // preserve insertion order

	var calls []ToolCall

	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg streamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "assistant":
			for _, block := range msg.Message.Content {
				if block.Type != "tool_use" {
					continue
				}
				inputJSON, err := json.Marshal(block.Input)
				if err != nil {
					inputJSON = []byte("{}")
				}
				name := normalizeToolName(block.Name)
				pending[block.ID] = pendingTool{name: name, input: string(inputJSON)}
				order = append(order, block.ID)
			}
		case "user":
			for _, block := range msg.Message.Content {
				if block.Type != "tool_result" {
					continue
				}
				if p, ok := pending[block.ToolUseID]; ok {
					calls = append(calls, ToolCall{
						Name:   p.name,
						Input:  p.input,
						Result: extractResultContent(block.Content),
					})
					delete(pending, block.ToolUseID)
				}
			}
		}
	}

	// Append any pending tools without results
	for _, id := range order {
		if p, ok := pending[id]; ok {
			calls = append(calls, ToolCall{
				Name:  p.name,
				Input: p.input,
			})
		}
	}

	return calls
}

// normalizeToolName strips MCP server prefix from tool names.
// e.g., "mcp__zcp__zerops_discover" → "zerops_discover"
func normalizeToolName(name string) string {
	if idx := strings.LastIndex(name, "__"); idx >= 0 {
		return name[idx+2:]
	}
	return name
}

// extractResultContent handles both string and structured tool result content.
func extractResultContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// --- Stream JSON types ---

// streamMessage represents a single line in Claude's stream-json output.
type streamMessage struct {
	Type    string        `json:"type"`
	Message streamContent `json:"message"`
}

type streamContent struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string `json:"type"`                  // "text", "tool_use", "tool_result"
	Text      string `json:"text,omitempty"`        // for type="text"
	ID        string `json:"id,omitempty"`          // for type="tool_use"
	Name      string `json:"name,omitempty"`        // for type="tool_use"
	Input     any    `json:"input,omitempty"`       // for type="tool_use"
	ToolUseID string `json:"tool_use_id,omitempty"` //nolint:tagliatelle // Claude API wire format
	Content   any    `json:"content,omitempty"`     // for type="tool_result" (string or structured)
}
