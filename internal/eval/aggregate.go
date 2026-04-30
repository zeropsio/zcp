// Aggregator for scenario-suite EVAL REPORT triage. Parses each scenario's
// agent self-assessment (markdown produced via assessmentInstructions in
// prompt.go), groups Failure chains by Root cause taxonomy, and renders a
// human-readable triage.md so the human triaging the suite sees ONE
// document per suite instead of opening N transcripts.
//
// Design choices:
//   - Loose markdown parsing. Agents don't always follow the exact schema;
//     aggregator is forgiving (heading variants, missing sections, body
//     plain text vs bullet list).
//   - Root cause taxonomy fixed to the four constants prompt.go advertises:
//     WRONG_KNOWLEDGE / MISSING_KNOWLEDGE / UNCLEAR_GUIDANCE / PLATFORM_ISSUE.
//     Anything else falls into UNCATEGORIZED so non-conforming entries still
//     surface (vs being silently dropped).
//   - Per-suite output only. Cross-suite trend analysis is a Phase 2 concern.
package eval

import (
	"bufio"
	"fmt"
	"sort"
	"strings"
)

// RootCause is the taxonomy emitted by the EVAL REPORT failure-chain entries.
// Stable string set so the aggregator can group across scenarios.
type RootCause string

const (
	RootWrongKnowledge   RootCause = "WRONG_KNOWLEDGE"
	RootMissingKnowledge RootCause = "MISSING_KNOWLEDGE"
	RootUnclearGuidance  RootCause = "UNCLEAR_GUIDANCE"
	RootPlatformIssue    RootCause = "PLATFORM_ISSUE"
	RootUncategorized    RootCause = "UNCATEGORIZED"
)

// rootCauseNorm normalises the agent's free-form root-cause text to the
// closed taxonomy. Agents sometimes write "wrong knowledge" or
// "MissingKnowledge" — case-fold + space-strip and match.
func rootCauseNorm(s string) RootCause {
	cleaned := strings.ToUpper(strings.TrimSpace(s))
	cleaned = strings.ReplaceAll(cleaned, " ", "_")
	cleaned = strings.ReplaceAll(cleaned, "-", "_")
	switch cleaned {
	case "WRONG_KNOWLEDGE":
		return RootWrongKnowledge
	case "MISSING_KNOWLEDGE":
		return RootMissingKnowledge
	case "UNCLEAR_GUIDANCE":
		return RootUnclearGuidance
	case "PLATFORM_ISSUE":
		return RootPlatformIssue
	}
	return RootUncategorized
}

// FailureChainEntry is one structured failure step from a scenario's
// EVAL REPORT. Fields are best-effort — any of them may be empty when
// the agent wrote a less-formal entry.
type FailureChainEntry struct {
	ScenarioID string    `json:"scenarioId"`
	Step       string    `json:"step,omitempty"`
	Received   string    `json:"received,omitempty"`
	Did        string    `json:"did,omitempty"`
	WentWrong  string    `json:"wentWrong,omitempty"`
	Recovered  string    `json:"recovered,omitempty"`
	RootCause  RootCause `json:"rootCause"`
	RawBody    string    `json:"rawBody"` // Original markdown for debugging when fields drop out.
}

// InformationGapEntry is one entry from the Information gaps section.
type InformationGapEntry struct {
	ScenarioID string `json:"scenarioId"`
	Trying     string `json:"trying,omitempty"`
	Tried      string `json:"tried,omitempty"`
	Guessed    string `json:"guessed,omitempty"`
	Should     string `json:"should,omitempty"`
	RawBody    string `json:"rawBody"`
}

// ParsedAssessment is the structured form of one scenario's EVAL REPORT.
// Outcome is upper-cased ("SUCCESS"/"PARTIAL"/"FAILURE") with a free-text
// detail tail.
type ParsedAssessment struct {
	Outcome          string                `json:"outcome"`
	OutcomeDetail    string                `json:"outcomeDetail,omitempty"`
	FailureChains    []FailureChainEntry   `json:"failureChains,omitempty"`
	InformationGaps  []InformationGapEntry `json:"informationGaps,omitempty"`
	WastedSteps      []string              `json:"wastedSteps,omitempty"`
	WastedStepsTotal int                   `json:"wastedStepsTotal,omitempty"`
	WhatWorked       []string              `json:"whatWorked,omitempty"`
	HasReport        bool                  `json:"hasReport"`
}

// ParseAssessment extracts the structured shape from a scenario's
// `## EVAL REPORT` body. Receives the full assessment markdown (already
// extracted via ExtractAssessment).
//
// The parser is intentionally LOOSE: it scans for `### <Section>` headings
// and yields the body of each section to a per-section parser. Missing
// sections are tolerated; sections not in the schema are ignored. Any
// "EVAL REPORT" prefix is stripped.
func ParseAssessment(body string) ParsedAssessment {
	out := ParsedAssessment{HasReport: false}
	body = strings.TrimSpace(body)
	if body == "" {
		return out
	}
	out.HasReport = true

	// Drop the "## EVAL REPORT" heading if present so section detection works
	// uniformly across reports that include vs exclude the H2 prefix.
	if idx := strings.Index(body, "## EVAL REPORT"); idx >= 0 {
		body = body[idx+len("## EVAL REPORT"):]
	}

	sections := splitH3Sections(body)
	for heading, content := range sections {
		switch normHeading(heading) {
		case "deployment outcome":
			out.Outcome, out.OutcomeDetail = parseOutcome(content)
		case "failure chains":
			out.FailureChains = parseFailureChains(content)
		case "information gaps":
			out.InformationGaps = parseInformationGaps(content)
		case "wasted steps":
			out.WastedSteps, out.WastedStepsTotal = parseWastedSteps(content)
		case "what worked well":
			out.WhatWorked = parseBulletList(content)
		}
	}
	return out
}

// normHeading lower-cases + trims the H3 heading text for switch matching.
func normHeading(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// splitH3Sections walks the body line-by-line and yields {heading -> content}
// keyed off `### Heading` lines. Content runs until the next `### ` heading
// or end-of-body. Headings are deduped by last-write-wins (an agent that
// emits two `### Failure chains` blocks gets the second one).
func splitH3Sections(body string) map[string]string {
	out := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var currentHeading string
	var currentBody strings.Builder

	flush := func() {
		if currentHeading != "" {
			out[currentHeading] = strings.TrimSpace(currentBody.String())
		}
		currentBody.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "### ") {
			flush()
			currentHeading = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			continue
		}
		if currentHeading != "" {
			currentBody.WriteString(line)
			currentBody.WriteString("\n")
		}
	}
	flush()
	return out
}

// parseOutcome reads the "State: ..." line and splits the SUCCESS/PARTIAL/
// FAILURE token from the trailing detail. If no "State:" prefix is found
// the entire content becomes the detail and outcome is empty.
func parseOutcome(content string) (state, detail string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Strip a leading "State:" or "**State**:" if present.
		stripped := line
		for _, prefix := range []string{"**State**:", "State:", "**state**:", "state:"} {
			if rest, ok := strings.CutPrefix(stripped, prefix); ok {
				stripped = strings.TrimSpace(rest)
				break
			}
		}
		// First token = state.
		fields := strings.SplitN(stripped, " ", 2)
		state = strings.ToUpper(strings.TrimSpace(fields[0]))
		// Strip trailing punctuation common in agent output (",", "-", "(").
		state = strings.TrimRight(state, ":,-(")
		if len(fields) > 1 {
			detail = strings.TrimSpace(fields[1])
		}
		return
	}
	return "", ""
}

// parseFailureChains splits the content into per-entry blocks (separated by
// blank lines or "- **Step**" markers) and parses each into a FailureChainEntry.
// Empty content or "No failure chains" yields an empty slice.
func parseFailureChains(content string) []FailureChainEntry {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if strings.Contains(strings.ToLower(content), "no failure chains") {
		return nil
	}

	entries := splitFailureBlocks(content)
	out := make([]FailureChainEntry, 0, len(entries))
	for _, blk := range entries {
		entry := FailureChainEntry{RawBody: blk}
		entry.Step = extractField(blk, "Step")
		entry.Received = extractField(blk, "What you received")
		entry.Did = extractField(blk, "What you did with it")
		entry.WentWrong = extractField(blk, "What went wrong")
		entry.Recovered = extractField(blk, "How you recovered")
		entry.RootCause = rootCauseNorm(extractField(blk, "Root cause"))
		// Drop completely empty entries (the splitter sometimes hands
		// us a stray blank from list separators).
		if entry.Step == "" && entry.WentWrong == "" && entry.RootCause == RootUncategorized && entry.RawBody == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// splitFailureBlocks splits a Failure chains body into per-entry blocks.
// Heuristic: each block starts with a line beginning `- **Step**` (the
// canonical first field). Anything before the first such marker is dropped
// (intro prose like "For each problem you encountered..." sometimes gets
// echoed back).
func splitFailureBlocks(content string) []string {
	lines := strings.Split(content, "\n")
	var blocks []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			blocks = append(blocks, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Block boundary: start of a new entry.
		if strings.HasPrefix(trimmed, "- **Step**") || strings.HasPrefix(trimmed, "**Step**") {
			flush()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()
	return blocks
}

// parseInformationGaps follows the same per-block pattern as failure chains.
// Each entry begins with a "What you were trying to do" or similar marker.
func parseInformationGaps(content string) []InformationGapEntry {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	blocks := splitGapBlocks(content)
	out := make([]InformationGapEntry, 0, len(blocks))
	for _, blk := range blocks {
		entry := InformationGapEntry{RawBody: blk}
		entry.Trying = extractField(blk, "What you were trying to do")
		entry.Tried = extractField(blk, "What query/tool you tried")
		entry.Guessed = extractField(blk, "What you had to guess or figure out on your own")
		entry.Should = extractField(blk, "What the knowledge base SHOULD contain")
		if entry.Trying == "" && entry.Tried == "" && entry.Guessed == "" && entry.Should == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// splitGapBlocks splits the Information gaps section into per-entry blocks.
// Marker is a top-level "- " bullet (info-gap entries start with a dash).
func splitGapBlocks(content string) []string {
	lines := strings.Split(content, "\n")
	var blocks []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			blocks = append(blocks, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	inEntry := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Top-level bullet at column 0 starts a new entry. Indented bullets
		// (sub-fields) belong to the current entry.
		if strings.HasPrefix(line, "- ") && !inEntry {
			inEntry = true
			current.WriteString(line)
			current.WriteString("\n")
			continue
		}
		if strings.HasPrefix(line, "- ") && inEntry && trimmed == "-" {
			// Empty bullet — boundary signal in some agent outputs.
			flush()
			inEntry = false
			continue
		}
		if line == "" && inEntry {
			// Blank line ends the current entry.
			flush()
			inEntry = false
			continue
		}
		if inEntry {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	flush()
	return blocks
}

// parseWastedSteps reads the bulleted list and the "Total wasted tool calls" line.
func parseWastedSteps(content string) (steps []string, total int) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "total wasted") {
			// "Total wasted tool calls: N"
			if _, after, ok := strings.Cut(line, ":"); ok {
				numStr := strings.TrimRight(strings.TrimSpace(after), ".")
				total = atoiSafeLeading(numStr)
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			steps = append(steps, rest)
		}
	}
	return
}

// atoiSafeLeading reads leading digits from s and returns their value. Used
// to tolerate trailing non-digit junk like "5 (estimated)" — agents are not
// always clean in their formatting. Returns 0 when s starts with anything
// other than a digit.
func atoiSafeLeading(s string) int {
	digits := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		digits = digits*10 + int(r-'0')
	}
	return digits
}

// parseBulletList collects all top-level "- " bullets, stripping the marker.
func parseBulletList(content string) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// extractField finds a "Field: value" line in the block and returns the
// trimmed value. The prompt.go schema uses TWO marker styles depending on
// the section: Failure chains use `**Field**:` (markdown bold), while
// Information gaps use plain `Field:`. extractField tries both so a
// single helper handles every section.
//
// Multi-line values (continuation lines without a marker) are appended
// until the next field marker or blank line. Empty when the field is
// absent. Sub-bulleted fields (`  - Field: value`) are recognized too.
func extractField(block, field string) string {
	// Try bold form first (Failure chains schema).
	if v := extractFieldWithMarker(block, "**"+field+"**:"); v != "" {
		return v
	}
	// Fall back to plain form (Information gaps schema). Field name with
	// trailing colon, possibly preceded by "- " bullet or whitespace.
	return extractFieldWithMarker(block, field+":")
}

func extractFieldWithMarker(block, marker string) string {
	_, after, ok := strings.Cut(block, marker)
	if !ok {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(after))
	var out strings.Builder
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// Stop at next field marker or blank line.
		if !first {
			if trimmed == "" {
				break
			}
			// Heuristic stop tokens: next bold-marker field, next plain bullet
			// (which would be either next entry OR a sub-field — both end this
			// field's value).
			if strings.HasPrefix(trimmed, "- **") ||
				strings.HasPrefix(trimmed, "**") ||
				strings.HasPrefix(trimmed, "- ") {
				break
			}
		}
		if first {
			first = false
			out.WriteString(trimmed)
		} else {
			out.WriteString(" ")
			out.WriteString(trimmed)
		}
	}
	return strings.TrimSpace(out.String())
}

// ---------------------------------------------------------------------------
// Triage assembly
// ---------------------------------------------------------------------------

// ScenarioBrief is the per-scenario row in the triage summary.
type ScenarioBrief struct {
	ID            string   `json:"id"`
	GraderPassed  bool     `json:"graderPassed"`
	Outcome       string   `json:"outcome"` // SUCCESS / PARTIAL / FAILURE / "" (no report)
	OutcomeDetail string   `json:"outcomeDetail,omitempty"`
	FailureCount  int      `json:"failureCount"`
	GapCount      int      `json:"gapCount"`
	WastedTotal   int      `json:"wastedTotal"`
	HasReport     bool     `json:"hasReport"`
	GraderFails   []string `json:"graderFails,omitempty"`
}

// Triage is the per-suite aggregated view. Used to render triage.md.
type Triage struct {
	SuiteID            string                            `json:"suiteId"`
	Scenarios          []ScenarioBrief                   `json:"scenarios"`
	GroupedByRootCause map[RootCause][]FailureChainEntry `json:"groupedByRootCause"`
	InformationGaps    []InformationGapEntry             `json:"informationGaps,omitempty"`
	WastedStepsByTool  map[string]int                    `json:"wastedStepsByTool,omitempty"`
	WastedStepsTotal   int                               `json:"wastedStepsTotal"`
	WhatWorked         []string                          `json:"whatWorked,omitempty"`
}

// AggregateScenarioSuite walks every ScenarioResult, parses its assessment,
// and returns the per-suite triage view. Pure function — no I/O.
func AggregateScenarioSuite(suite *ScenarioSuiteResult) Triage {
	t := Triage{
		SuiteID:            suite.SuiteID,
		GroupedByRootCause: make(map[RootCause][]FailureChainEntry),
		WastedStepsByTool:  make(map[string]int),
	}

	whatWorkedSet := make(map[string]bool)

	for _, r := range suite.Results {
		brief := ScenarioBrief{
			ID:           r.ScenarioID,
			GraderPassed: r.Grade.Passed,
			GraderFails:  r.Grade.Failures,
		}

		parsed := ParseAssessment(r.Assessment)
		brief.HasReport = parsed.HasReport
		brief.Outcome = parsed.Outcome
		brief.OutcomeDetail = parsed.OutcomeDetail
		brief.FailureCount = len(parsed.FailureChains)
		brief.GapCount = len(parsed.InformationGaps)
		brief.WastedTotal = parsed.WastedStepsTotal

		// Stamp scenario ID onto each entry before grouping.
		for _, fc := range parsed.FailureChains {
			fc.ScenarioID = r.ScenarioID
			t.GroupedByRootCause[fc.RootCause] = append(t.GroupedByRootCause[fc.RootCause], fc)
		}
		for _, gap := range parsed.InformationGaps {
			gap.ScenarioID = r.ScenarioID
			t.InformationGaps = append(t.InformationGaps, gap)
		}
		for _, ws := range parsed.WastedSteps {
			tool := extractToolFromWastedStep(ws)
			t.WastedStepsByTool[tool]++
		}
		t.WastedStepsTotal += parsed.WastedStepsTotal
		for _, ww := range parsed.WhatWorked {
			if !whatWorkedSet[ww] {
				whatWorkedSet[ww] = true
				t.WhatWorked = append(t.WhatWorked, ww)
			}
		}

		t.Scenarios = append(t.Scenarios, brief)
	}

	sort.Strings(t.WhatWorked)
	return t
}

// extractToolFromWastedStep tries to pull the leading tool name from a
// wasted-step entry like "zerops_logs — called twice for nothing" or
// "Called zerops_workflow when zerops_status would have sufficed".
// Best-effort; falls back to the first word if no zerops_ prefix is found.
func extractToolFromWastedStep(s string) string {
	idx := strings.Index(s, "zerops_")
	if idx >= 0 {
		// Stop at next whitespace or punctuation.
		end := idx
		for end < len(s) {
			r := s[end]
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				end++
				continue
			}
			break
		}
		return s[idx:end]
	}
	// Fallback: first whitespace-delimited token.
	if i := strings.IndexAny(s, " \t,:."); i > 0 {
		return s[:i]
	}
	return s
}

// ---------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------

// RenderTriageMarkdown emits the human-readable triage doc. Fixed layout:
//  1. Suite header + per-scenario verdict table
//  2. Failure chains grouped by Root cause (frequency-ranked)
//  3. Information gaps (flat list, scenario-tagged)
//  4. Wasted steps by tool (frequency table)
//  5. What worked well (positive signal, dedup'd)
func RenderTriageMarkdown(t Triage) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Triage — suite `%s`\n\n", t.SuiteID)

	// 1. Per-scenario verdict
	fmt.Fprintf(&b, "## Per-scenario verdict\n\n")
	fmt.Fprintf(&b, "| Scenario | Grader | Outcome | Failures | Gaps | Wasted |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|\n")
	pass, fail := 0, 0
	for _, s := range t.Scenarios {
		grader := statusFail
		if s.GraderPassed {
			grader = statusPass
			pass++
		} else {
			fail++
		}
		outcome := s.Outcome
		if outcome == "" {
			outcome = "(no report)"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %d | %d |\n",
			s.ID, grader, outcome, s.FailureCount, s.GapCount, s.WastedTotal)
	}
	fmt.Fprintf(&b, "\n**Totals**: %d/%d grader-PASS, %d failures across %d scenarios.\n\n",
		pass, len(t.Scenarios), countFailures(t), len(t.Scenarios))

	// Per-scenario grader failure detail (when the grader didn't pass).
	hasGraderFails := false
	for _, s := range t.Scenarios {
		if !s.GraderPassed && len(s.GraderFails) > 0 {
			hasGraderFails = true
			break
		}
	}
	if hasGraderFails {
		fmt.Fprintf(&b, "### Grader failure detail\n\n")
		for _, s := range t.Scenarios {
			if s.GraderPassed || len(s.GraderFails) == 0 {
				continue
			}
			fmt.Fprintf(&b, "- **`%s`**:\n", s.ID)
			for _, f := range s.GraderFails {
				fmt.Fprintf(&b, "  - %s\n", f)
			}
		}
		fmt.Fprintln(&b)
	}

	// 2. Failure chains grouped by root cause.
	fmt.Fprintf(&b, "## Failure chains by root cause\n\n")
	if len(t.GroupedByRootCause) == 0 {
		fmt.Fprintf(&b, "_No failure chains reported._\n\n")
	} else {
		// Order: primary taxonomy first (most actionable), then UNCATEGORIZED last.
		order := []RootCause{
			RootWrongKnowledge, RootMissingKnowledge, RootUnclearGuidance,
			RootPlatformIssue, RootUncategorized,
		}
		for _, rc := range order {
			entries := t.GroupedByRootCause[rc]
			if len(entries) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %s (%d)\n\n", rc, len(entries))
			for _, e := range entries {
				fmt.Fprintf(&b, "- **`%s`** — step `%s`:\n", e.ScenarioID, e.Step)
				if e.WentWrong != "" {
					fmt.Fprintf(&b, "  - Went wrong: %s\n", e.WentWrong)
				}
				if e.Received != "" {
					fmt.Fprintf(&b, "  - Received: %s\n", e.Received)
				}
				if e.Did != "" {
					fmt.Fprintf(&b, "  - Did: %s\n", e.Did)
				}
				if e.Recovered != "" {
					fmt.Fprintf(&b, "  - Recovered: %s\n", e.Recovered)
				}
			}
			fmt.Fprintln(&b)
		}
	}

	// 3. Information gaps.
	fmt.Fprintf(&b, "## Information gaps\n\n")
	if len(t.InformationGaps) == 0 {
		fmt.Fprintf(&b, "_None reported._\n\n")
	} else {
		for _, g := range t.InformationGaps {
			fmt.Fprintf(&b, "- **`%s`**: tried %q\n", g.ScenarioID, firstNonEmpty(g.Trying, g.Tried))
			if g.Should != "" {
				fmt.Fprintf(&b, "  - SHOULD: %s\n", g.Should)
			}
		}
		fmt.Fprintln(&b)
	}

	// 4. Wasted steps by tool.
	fmt.Fprintf(&b, "## Wasted tool calls — by tool\n\n")
	if len(t.WastedStepsByTool) == 0 {
		fmt.Fprintf(&b, "_None reported._\n\n")
	} else {
		// Sort tools by wasted-call count descending.
		type kv struct {
			Tool  string
			Count int
		}
		pairs := make([]kv, 0, len(t.WastedStepsByTool))
		for tool, n := range t.WastedStepsByTool {
			pairs = append(pairs, kv{tool, n})
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].Count != pairs[j].Count {
				return pairs[i].Count > pairs[j].Count
			}
			return pairs[i].Tool < pairs[j].Tool
		})
		fmt.Fprintf(&b, "| Tool | Wasted calls |\n|---|---|\n")
		for _, p := range pairs {
			fmt.Fprintf(&b, "| `%s` | %d |\n", p.Tool, p.Count)
		}
		fmt.Fprintf(&b, "\n**Total wasted calls**: %d.\n\n", t.WastedStepsTotal)
	}

	// 5. What worked.
	fmt.Fprintf(&b, "## What worked well\n\n")
	if len(t.WhatWorked) == 0 {
		fmt.Fprintf(&b, "_None reported._\n")
	} else {
		for _, w := range t.WhatWorked {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}

	return b.String()
}

func countFailures(t Triage) int {
	n := 0
	for _, entries := range t.GroupedByRootCause {
		n += len(entries)
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
