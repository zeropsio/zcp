package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// eventTypeToolUse is the jsonl event.type for a tool invocation.
const eventTypeToolUse = "tool_use"

// AgentSummary is one agent's tally across its jsonl. Captures tool-call
// counts, fact-record payloads (verbatim for downstream trace), and
// dispatch prompts received (for sub-agents).
type AgentSummary struct {
	ID                string            `json:"id"`
	Role              string            `json:"role"`
	ToolCallCount     int               `json:"toolCallCount"`
	ToolCountByName   map[string]int    `json:"toolCountByName"`
	FactRecords       []json.RawMessage `json:"factRecords,omitempty"`
	DispatchPrompt    string            `json:"dispatchPrompt,omitempty"`
	CompletionPayload string            `json:"completionPayload,omitempty"`
	FirstSeq          int               `json:"firstSeq"`
	LastSeq           int               `json:"lastSeq"`
}

// WriteAgentSummaries walks a RawTree and writes one summary per agent
// plus an index.md. Outputs land under <outputDir>/agents/.
func WriteAgentSummaries(tree *RawTree, outputDir string) ([]AgentSummary, error) {
	dir := filepath.Join(outputDir, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir agents: %w", err)
	}

	var summaries []AgentSummary
	if tree.Main != nil {
		summaries = append(summaries, summarize(tree.Main))
	}
	for _, sub := range tree.Subs {
		summaries = append(summaries, summarize(sub))
	}

	for _, s := range summaries {
		if err := writeJSON(filepath.Join(dir, s.ID+".json"), s); err != nil {
			return nil, err
		}
	}
	return summaries, writeAgentIndex(filepath.Join(dir, "index.md"), summaries)
}

func summarize(trace *AgentTrace) AgentSummary {
	s := AgentSummary{
		ID:              trace.ID,
		Role:            trace.Role,
		ToolCountByName: map[string]int{},
	}
	if s.ID == "" {
		s.ID = "main"
	}
	if len(trace.Events) > 0 {
		s.FirstSeq = trace.Events[0].Sequence
		s.LastSeq = trace.Events[len(trace.Events)-1].Sequence
	}
	for _, ev := range trace.Events {
		switch ev.Type {
		case eventTypeToolUse:
			s.ToolCallCount++
			s.ToolCountByName[ev.Name]++
			if ev.Name == "zerops_record_fact" {
				s.FactRecords = append(s.FactRecords, ev.ToolUse)
			}
			if ev.Name == "Agent" {
				// Capture the dispatched prompt bytes verbatim so v3's
				// dispatch-integrity gate can diff against engine build.
				prompt := extractAgentPrompt(ev.ToolUse)
				if prompt != "" && s.DispatchPrompt == "" {
					s.DispatchPrompt = prompt
				}
			}
		case "completion":
			if len(ev.Result) > 0 {
				s.CompletionPayload = string(ev.Result)
			}
		}
	}
	return s
}

func extractAgentPrompt(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if p, ok := obj.Input["prompt"].(string); ok {
		return p
	}
	return ""
}

func writeAgentIndex(path string, summaries []AgentSummary) error {
	var b strings.Builder
	b.WriteString("# Agent index\n\n")
	b.WriteString("| id | role | tool calls | facts | dispatch bytes |\n")
	b.WriteString("|---|---|---:|---:|---:|\n")
	for _, s := range summaries {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d |\n",
			s.ID, s.Role, s.ToolCallCount, len(s.FactRecords),
			len(s.DispatchPrompt))
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
