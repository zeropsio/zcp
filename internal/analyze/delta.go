package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeltaReport compares current vs baseline harness outputs. Fires
// regressions and documents structural / content / behavioral
// differences. Structural diff reads baseline's machine-report.json;
// content diff walks the copied content/ trees.
type DeltaReport struct {
	BaselineDir string            `json:"baselineDir"`
	CurrentDir  string            `json:"currentDir"`
	Structural  []DeltaChange     `json:"structural,omitempty"`
	Content     []ContentDelta    `json:"content,omitempty"`
	Agents      []AgentDelta      `json:"agents,omitempty"`
	Regressions []DeltaRegression `json:"regressions,omitempty"`
}

// DeltaChange is one bar-level metric shift (e.g. subagent-count,
// writer-first-pass-failure-count).
type DeltaChange struct {
	Metric    string `json:"metric"`
	Baseline  any    `json:"baseline"`
	Current   any    `json:"current"`
	Regressed bool   `json:"regressed"`
}

// ContentDelta is one file's presence / authorship / size diff.
type ContentDelta struct {
	Path         string `json:"path"`
	State        string `json:"state"` // added | removed | changed
	BaselineSize int    `json:"baselineSize,omitempty"`
	CurrentSize  int    `json:"currentSize,omitempty"`
	SizeDelta    int    `json:"sizeDelta"`
}

// AgentDelta is per-agent behavior drift.
type AgentDelta struct {
	AgentID           string `json:"agentID"`
	ToolCallDelta     int    `json:"toolCallDelta"`
	FactRecordDelta   int    `json:"factRecordDelta"`
	DispatchByteDelta int    `json:"dispatchByteDelta"`
}

// DeltaRegression names a specific regression pattern the harness
// watches: writer-owned-path rewrite, action=skip on signal-grade
// substep, SHA mismatch on verify-subagent-dispatch, etc.
type DeltaRegression struct {
	Pattern string `json:"pattern"`
	Detail  string `json:"detail"`
}

// WriteDelta compares two harness output roots (`analysis/` dirs) and
// writes delta/{structural,content,agents,regressions}.md plus
// regressions.json under <currentRoot>/delta/.
func WriteDelta(baselineRoot, currentRoot string) (*DeltaReport, error) {
	dir := filepath.Join(currentRoot, "delta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir delta: %w", err)
	}

	report := &DeltaReport{BaselineDir: baselineRoot, CurrentDir: currentRoot}

	// Structural: compare agents/index aggregated metrics.
	baselineAgents := readAgentSummaries(baselineRoot)
	currentAgents := readAgentSummaries(currentRoot)
	report.Structural = compareStructural(baselineAgents, currentAgents)
	report.Agents = compareAgents(baselineAgents, currentAgents)

	// Content: walk each side's content/ tree.
	report.Content = compareContent(
		filepath.Join(baselineRoot, "content"),
		filepath.Join(currentRoot, "content"))

	// Regressions: signals the delta watches for.
	report.Regressions = detectRegressions(baselineAgents, currentAgents)

	// Write outputs.
	if err := writeJSON(filepath.Join(dir, "regressions.json"), report.Regressions); err != nil {
		return nil, err
	}
	if err := writeDeltaMarkdown(filepath.Join(dir, "summary.md"), report); err != nil {
		return nil, err
	}
	return report, nil
}

func readAgentSummaries(root string) map[string]AgentSummary {
	out := map[string]AgentSummary{}
	agentsDir := filepath.Join(root, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || e.Name() == "index.json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(agentsDir, e.Name()))
		if err != nil {
			continue
		}
		var s AgentSummary
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		id := s.ID
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		out[id] = s
	}
	return out
}

func compareStructural(base, cur map[string]AgentSummary) []DeltaChange {
	baseTools, curTools := 0, 0
	baseFacts, curFacts := 0, 0
	for _, s := range base {
		baseTools += s.ToolCallCount
		baseFacts += len(s.FactRecords)
	}
	for _, s := range cur {
		curTools += s.ToolCallCount
		curFacts += len(s.FactRecords)
	}
	return []DeltaChange{
		{Metric: "agents", Baseline: len(base), Current: len(cur),
			Regressed: len(cur) > len(base)*2},
		{Metric: "total-tool-calls", Baseline: baseTools, Current: curTools,
			Regressed: curTools > baseTools*2},
		{Metric: "total-fact-records", Baseline: baseFacts, Current: curFacts,
			Regressed: curFacts < baseFacts/2},
	}
}

func compareAgents(base, cur map[string]AgentSummary) []AgentDelta {
	var out []AgentDelta
	seen := map[string]bool{}
	for id, b := range base {
		c := cur[id]
		out = append(out, AgentDelta{
			AgentID:           id,
			ToolCallDelta:     c.ToolCallCount - b.ToolCallCount,
			FactRecordDelta:   len(c.FactRecords) - len(b.FactRecords),
			DispatchByteDelta: len(c.DispatchPrompt) - len(b.DispatchPrompt),
		})
		seen[id] = true
	}
	for id, c := range cur {
		if seen[id] {
			continue
		}
		out = append(out, AgentDelta{
			AgentID:           id,
			ToolCallDelta:     c.ToolCallCount,
			FactRecordDelta:   len(c.FactRecords),
			DispatchByteDelta: len(c.DispatchPrompt),
		})
	}
	return out
}

func compareContent(baseRoot, curRoot string) []ContentDelta {
	baseFiles := sizeMap(baseRoot)
	curFiles := sizeMap(curRoot)
	var out []ContentDelta
	for path, baseSize := range baseFiles {
		curSize, ok := curFiles[path]
		if !ok {
			out = append(out, ContentDelta{Path: path, State: "removed",
				BaselineSize: baseSize, SizeDelta: -baseSize})
			continue
		}
		if curSize != baseSize {
			out = append(out, ContentDelta{Path: path, State: "changed",
				BaselineSize: baseSize, CurrentSize: curSize,
				SizeDelta: curSize - baseSize})
		}
	}
	for path, curSize := range curFiles {
		if _, ok := baseFiles[path]; !ok {
			out = append(out, ContentDelta{Path: path, State: "added",
				CurrentSize: curSize, SizeDelta: curSize})
		}
	}
	return out
}

func sizeMap(root string) map[string]int {
	out := map[string]int{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, ".meta.json") {
			return nil //nolint:nilerr // walk-tolerant
		}
		rel, _ := filepath.Rel(root, path)
		if info, err := os.Stat(path); err == nil {
			out[rel] = int(info.Size())
		}
		return nil
	})
	return out
}

func detectRegressions(base, cur map[string]AgentSummary) []DeltaRegression {
	var out []DeltaRegression
	// Regression: subagent disappeared.
	for id := range base {
		if _, ok := cur[id]; !ok {
			out = append(out, DeltaRegression{
				Pattern: "subagent-disappeared",
				Detail:  fmt.Sprintf("%s present in baseline, absent in current", id),
			})
		}
	}
	// Regression: fact-record count collapsed.
	baseFacts, curFacts := 0, 0
	for _, s := range base {
		baseFacts += len(s.FactRecords)
	}
	for _, s := range cur {
		curFacts += len(s.FactRecords)
	}
	if baseFacts > 0 && curFacts*2 < baseFacts {
		out = append(out, DeltaRegression{
			Pattern: "fact-record-collapse",
			Detail:  fmt.Sprintf("baseline=%d current=%d", baseFacts, curFacts),
		})
	}
	return out
}

func writeDeltaMarkdown(path string, r *DeltaReport) error {
	var b strings.Builder
	b.WriteString("# Delta report\n\n")
	fmt.Fprintf(&b, "- baseline: `%s`\n- current:  `%s`\n\n", r.BaselineDir, r.CurrentDir)

	b.WriteString("## Structural\n\n")
	b.WriteString("| metric | baseline | current | regressed |\n")
	b.WriteString("|---|---:|---:|---|\n")
	for _, c := range r.Structural {
		fmt.Fprintf(&b, "| %s | %v | %v | %v |\n", c.Metric, c.Baseline, c.Current, c.Regressed)
	}

	b.WriteString("\n## Content changes\n\n")
	for _, c := range r.Content {
		fmt.Fprintf(&b, "- [%s] %s (%+d bytes)\n", c.State, c.Path, c.SizeDelta)
	}

	b.WriteString("\n## Regressions detected\n\n")
	if len(r.Regressions) == 0 {
		b.WriteString("- (none)\n")
	}
	for _, rg := range r.Regressions {
		fmt.Fprintf(&b, "- %s — %s\n", rg.Pattern, rg.Detail)
	}

	return os.WriteFile(path, []byte(b.String()), 0o600)
}
