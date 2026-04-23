package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RawTree is the lossless reconstruction of a recipe run's session-log
// tree. main.json captures the main session; each Agent dispatch nests
// the corresponding sub-agent session as a child. Consumed by v3's
// analyze-recipe-run-v3 harness (see plan §10).
type RawTree struct {
	Main     *AgentTrace   `json:"main"`
	Subs     []*AgentTrace `json:"subs,omitempty"`
	Slug     string        `json:"slug"`
	RunLabel string        `json:"runLabel"`
}

// AgentTrace is one agent's session — main or sub. Every jsonl event
// lands as an Event, in wall-clock order.
type AgentTrace struct {
	ID         string     `json:"id"`
	Role       string     `json:"role"`
	SourceFile string     `json:"sourceFile"`
	Events     []RawEvent `json:"events"`
}

// RawEvent wraps one assistant / user / tool_use / tool_result record
// from a jsonl line. Raw is the original line for lossless round-trip.
type RawEvent struct {
	Type     string          `json:"type"`
	Name     string          `json:"name,omitempty"`
	ToolUse  json.RawMessage `json:"toolUse,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Raw      string          `json:"-"`
	Sequence int             `json:"sequence"`
}

// WalkSessionsLogs reads a SESSIONS_LOGS dir (main-session.jsonl + one
// per-subagent jsonl + meta.json files) and produces a RawTree. Each
// jsonl becomes an AgentTrace. Callers pass an output dir; the raw
// outputs land under <outputDir>/raw/.
func WalkSessionsLogs(logsDir, outputDir, slug, runLabel string) (*RawTree, error) {
	if err := os.MkdirAll(filepath.Join(outputDir, "raw"), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir raw: %w", err)
	}

	tree := &RawTree{Slug: slug, RunLabel: runLabel}

	mainPath := filepath.Join(logsDir, "main-session.jsonl")
	if _, err := os.Stat(mainPath); err == nil {
		trace, err := readJSONL(mainPath, "main")
		if err != nil {
			return nil, err
		}
		tree.Main = trace
	}

	// Sub-sessions: any *.jsonl that isn't main-session.jsonl.
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil, fmt.Errorf("read logs dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") || name == "main-session.jsonl" {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		role := readMetaRole(filepath.Join(logsDir, id+".meta.json"))
		trace, err := readJSONL(filepath.Join(logsDir, name), role)
		if err != nil {
			return nil, err
		}
		trace.ID = id
		tree.Subs = append(tree.Subs, trace)
	}

	// Write raw outputs.
	if err := writeJSON(filepath.Join(outputDir, "raw", "tree.json"), tree); err != nil {
		return nil, err
	}
	if tree.Main != nil {
		if err := writeJSON(filepath.Join(outputDir, "raw", "main.json"), tree.Main); err != nil {
			return nil, err
		}
	}
	for _, sub := range tree.Subs {
		path := filepath.Join(outputDir, "raw", "sub-"+sub.ID+".json")
		if err := writeJSON(path, sub); err != nil {
			return nil, err
		}
	}
	if err := writeTreeMarkdown(filepath.Join(outputDir, "raw", "tree.md"), tree); err != nil {
		return nil, err
	}
	return tree, nil
}

func readJSONL(path, role string) (*AgentTrace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	trace := &AgentTrace{
		SourceFile: path,
		Role:       role,
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	seq := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(line, &generic); err != nil {
			continue // skip malformed — harness is lossy-tolerant
		}
		ev := RawEvent{Sequence: seq, Raw: string(line)}
		if t, ok := generic["type"]; ok {
			_ = json.Unmarshal(t, &ev.Type)
		}
		if n, ok := generic["name"]; ok {
			_ = json.Unmarshal(n, &ev.Name)
		}
		if tu, ok := generic["tool_use"]; ok {
			ev.ToolUse = tu
		}
		if r, ok := generic["result"]; ok {
			ev.Result = r
		}
		trace.Events = append(trace.Events, ev)
		seq++
	}
	return trace, scanner.Err()
}

func readMetaRole(metaPath string) string {
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return "subagent"
	}
	var m struct {
		Role        string `json:"role"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "subagent"
	}
	if m.Role != "" {
		return m.Role
	}
	return m.Description
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func writeTreeMarkdown(path string, tree *RawTree) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Session tree — %s (%s)\n\n", tree.Slug, tree.RunLabel)
	if tree.Main != nil {
		fmt.Fprintf(&b, "## Main session — %d events\n\n", len(tree.Main.Events))
		writeEventSummary(&b, tree.Main, 80)
	}
	for _, sub := range tree.Subs {
		fmt.Fprintf(&b, "\n## Sub: %s (%s) — %d events\n\n",
			sub.ID, sub.Role, len(sub.Events))
		writeEventSummary(&b, sub, 80)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func writeEventSummary(b *strings.Builder, trace *AgentTrace, limit int) {
	end := min(len(trace.Events), limit)
	for i := range end {
		ev := trace.Events[i]
		fmt.Fprintf(b, "- seq=%d type=%s name=%s\n", ev.Sequence, ev.Type, ev.Name)
	}
	if len(trace.Events) > limit {
		fmt.Fprintf(b, "- … %d more events\n", len(trace.Events)-limit)
	}
}
