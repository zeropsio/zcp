package eval

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// usageBucketAgent / usageBucketOverhead name the two phase buckets that
// spec-testing-architecture §10.5 splits provider usage into (PhaseMapping values).
const (
	usageBucketAgent    = "agent"
	usageBucketOverhead = "overhead"
)

// UsageValue is one usage number with an explicit observed flag — a field
// whose joined exchanges never reported it prints "unknown", never "0"
// (docs/spec-testing-architecture.md §10.5 item 3).
type UsageValue struct {
	Value    int64 `json:"value"`
	Observed bool  `json:"observed"`
}

// UsageTotals is the four-number usage summary §10.5 item 3 requires.
type UsageTotals struct {
	Input         UsageValue `json:"input"`
	CacheCreation UsageValue `json:"cacheCreation"`
	CacheRead     UsageValue `json:"cacheRead"`
	Output        UsageValue `json:"output"`
}

// InvocationReportRow is one recorded lifecycle phase's row in the
// Invocations section (§10.5 item 3).
type InvocationReportRow struct {
	InvocationID  string      `json:"invocationId"`
	Phase         string      `json:"phase"`
	Status        string      `json:"status"`
	ExchangeCount int         `json:"exchangeCount"`
	Unattributed  bool        `json:"unattributed"`
	MCPCalls      int         `json:"mcpCalls"`
	Usage         UsageTotals `json:"usage"`
}

// PhaseTotals holds the report's own agent-vs-overhead phase mapping and the
// two totals it produces (§10.5 item 3: "two separate totals, never one
// sum").
type PhaseTotals struct {
	Agent        UsageTotals       `json:"agent"`
	Overhead     UsageTotals       `json:"overhead"`
	PhaseMapping map[string]string `json:"phaseMapping"` // phase -> "agent" | "overhead" | "unmapped"
}

// ProcessIdentitySummary is copied observation counts only — never a
// re-derived acceptance verdict (§10.5 item 1).
type ProcessIdentitySummary struct {
	Observations      int `json:"observations"`
	Counted           int `json:"counted"`
	MatchesCandidate  int `json:"matchesCandidate"`
	ProjectMismatches int `json:"projectMismatches"`
}

// ResultSection is §10.5 item 1.
type ResultSection struct {
	Task struct {
		Mode     string    `json:"mode"`
		Result   string    `json:"result"`
		FrozenAt time.Time `json:"frozenAt"`
	} `json:"task"`
	Execution struct {
		Error string `json:"error"`
	} `json:"execution"`
	TaskEnd struct {
		Persisted bool `json:"persisted"`
		Settled   bool `json:"settled"`
	} `json:"taskEnd"`
	Capture struct {
		Status   string `json:"status"`
		Valid    bool   `json:"valid"`
		Complete bool   `json:"complete"`
	} `json:"capture"`
	Binding         *ExecutionBindingRecord `json:"binding,omitempty"`
	ProcessIdentity ProcessIdentitySummary  `json:"processIdentity"`
}

// MCPStreamRow is one stream's row in the MCP section (§10.5 item 4). Calls
// is a count of recorded tool calls — never the number of tools advertised
// in the stream's MCP schema.
type MCPStreamRow struct {
	File          string `json:"file"`
	ToolCalls     int    `json:"toolCalls"`
	Notifications int    `json:"notifications"`
	InputBytes    int64  `json:"inputBytes"`
	OutputByte    int64  `json:"outputBytes"`
}

// Finding is one fact with a coordinate (§10.5 item 5).
type Finding struct {
	Text       string `json:"text"`
	Coordinate string `json:"coordinate"`
}

// BehavioralReport is the read-only §10.5 projection of one scenario run's
// frozen evidence. It has no verdict authority: Result.Task.Result is
// verification.json's, Result.Capture.* is InspectSession's,
// Result.ProcessIdentity is a count of recorded observations.
type BehavioralReport struct {
	Result      ResultSection         `json:"result"`
	Checks      []RequiredCheck       `json:"checks"`
	Invocations []InvocationReportRow `json:"invocations"`
	Totals      PhaseTotals           `json:"totals"`
	MCP         []MCPStreamRow        `json:"mcp"`
	Findings    []Finding             `json:"findings"`
	Gaps        []string              `json:"gaps"`
	Sources     map[string]string     `json:"sources"`
}

const behavioralReportCleanupNote = "later copy and cleanup are operator notes outside the capture and are not assessed"

// BuildBehavioralReport opens the capture window read-only, resolves exactly
// one eval run + scenario, and projects its frozen evidence into a
// BehavioralReport. Never a network, provider, platform or model call; never
// writes into the window (docs/spec-capture-inspector.md §8.5).
func BuildBehavioralReport(sessionDir, evalRunID, scenarioRunID string) (*BehavioralReport, *capture.InspectionReport, error) {
	inspection, err := capture.InspectSession(sessionDir)
	if err != nil {
		return nil, nil, fmt.Errorf("scope: %w", err)
	}
	filtered, err := capture.FilterInspection(inspection, capture.InspectionFilter{EvalRunID: evalRunID, ScenarioRunID: scenarioRunID})
	if err != nil {
		return nil, inspection, fmt.Errorf("scope: %w", err)
	}
	if len(filtered.EvalRuns) != 1 || len(filtered.EvalRuns[0].Scenarios) != 1 {
		return nil, inspection, fmt.Errorf("scope: eval run %q scenario %q does not resolve to exactly one run", evalRunID, scenarioRunID)
	}
	run := filtered.EvalRuns[0]
	scenario := run.Scenarios[0]

	report := &BehavioralReport{Sources: map[string]string{}}

	manifest, manifestErr := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if manifestErr != nil {
		// InspectSession has already validated the manifest, so this is
		// unexpected; surface the real error instead of a "legacy window".
		return nil, inspection, fmt.Errorf("manifest: %w", manifestErr)
	}

	metaPath := path.Join("eval", evalRunID, scenarioRunID, "meta.json")
	metaBytes, err := capture.ReadManifestFile(sessionDir, manifest, metaPath)
	if err != nil {
		return nil, inspection, fmt.Errorf("scope: read %s: %w", metaPath, err)
	}
	var result BehavioralResult
	if err := json.Unmarshal(metaBytes, &result); err != nil {
		return nil, inspection, fmt.Errorf("scope: decode %s: %w", metaPath, err)
	}
	report.Sources["result"] = metaPath

	verificationPath := path.Join("eval", evalRunID, scenarioRunID, "verification.json")
	var verification *VerificationDocument
	if verificationBytes, verifyErr := capture.ReadManifestFile(sessionDir, manifest, verificationPath); verifyErr == nil {
		var doc VerificationDocument
		if err := json.Unmarshal(verificationBytes, &doc); err == nil {
			verification = &doc
			report.Sources["checks"] = verificationPath
		}
	}

	fillResultSection(report, &result, inspection.Integrity, inspection.Status, verification)
	if verification != nil {
		report.Checks = verification.Checks
	}
	fillInvocations(report, scenario, filtered.ModelContexts, filtered.MCPStreams)
	fillMCP(report, filtered.MCPStreams)
	fillFindings(report, &result, verification)
	fillGaps(report, &result, verification, inspection.Integrity)

	return report, inspection, nil
}

func fillResultSection(report *BehavioralReport, result *BehavioralResult, integrity capture.InspectionIntegrity, captureStatus string, verification *VerificationDocument) {
	if result.Task != nil {
		report.Result.Task.Mode = result.Task.Mode
		report.Result.Task.Result = string(result.Task.Result)
		report.Result.Task.FrozenAt = result.Task.FrozenAt
	} else if verification != nil {
		report.Result.Task.Mode = verification.Mode
		report.Result.Task.Result = string(verification.Result)
		report.Result.Task.FrozenAt = verification.FrozenAt
	}
	report.Result.Execution.Error = result.Error
	if result.TaskEnd != nil {
		report.Result.TaskEnd.Persisted = result.TaskEnd.Persisted
		report.Result.TaskEnd.Settled = result.TaskEnd.Settled
	}
	report.Result.Capture.Status = captureStatus
	report.Result.Capture.Valid = integrity.Valid
	report.Result.Capture.Complete = integrity.Complete
	report.Result.Binding = result.Binding
	summary := ProcessIdentitySummary{Observations: len(result.ProcessIdentity)}
	for _, observation := range result.ProcessIdentity {
		// Only observations the runner classified as counted carry a digest
		// and an environment; unobservable/unsupported ones are listed in
		// Observations and nowhere else.
		if observation.Classification != processIdentityCounted {
			continue
		}
		summary.Counted++
		if observation.MatchesCandidate {
			summary.MatchesCandidate++
		}
		if result.Binding != nil && observation.ProjectID != "" && observation.ProjectID != result.Binding.ProjectID {
			summary.ProjectMismatches++
		}
	}
	report.Result.ProcessIdentity = summary
}

// invocationPhaseBucket maps a recorded lifecycle phase to the report's own
// agent-vs-overhead grouping (§10.5 item 3).
func invocationPhaseBucket(phase string) string {
	switch {
	case len(phase) >= 6 && phase[:6] == "agent.":
		return usageBucketAgent
	case len(phase) >= 9 && phase[:9] == "user-sim.":
		return usageBucketOverhead
	case phase == "retrospective":
		return usageBucketOverhead
	default:
		return "unmapped"
	}
}

func fillInvocations(report *BehavioralReport, scenario capture.EvalScenarioInspection, contexts []capture.ModelContextInspection, streams []capture.MCPStreamInspection) {
	byExchange := make(map[string]capture.ModelContextInspection, len(contexts))
	for _, context := range contexts {
		byExchange[context.ExchangeID] = context
	}
	// An invocation's MCP call count is the sum over the streams the
	// inspection attributed to it (§10.5 item 3).
	mcpCallsByInvocation := make(map[string]int)
	for _, stream := range streams {
		if stream.InvocationID != "" {
			mcpCallsByInvocation[stream.InvocationID] += stream.ToolCalls
		}
	}
	report.Totals.PhaseMapping = map[string]string{}

	invocations := append([]capture.EvalInvocationInspection(nil), scenario.Invocations...)
	sort.Slice(invocations, func(i, j int) bool { return invocations[i].StartSource.SeqStart < invocations[j].StartSource.SeqStart })

	for _, invocation := range invocations {
		row := InvocationReportRow{
			InvocationID:  invocation.InvocationID,
			Phase:         invocation.Phase,
			Status:        invocation.Status,
			ExchangeCount: len(invocation.ExchangeIDs),
		}
		row.Usage, row.Unattributed = joinInvocationUsage(invocation.ExchangeIDs, byExchange)
		row.MCPCalls = mcpCallsByInvocation[invocation.InvocationID]
		report.Invocations = append(report.Invocations, row)

		bucket := invocationPhaseBucket(invocation.Phase)
		report.Totals.PhaseMapping[invocation.Phase] = bucket
		addUsageToTotal(report, bucket, row.Usage)
	}
}

func joinInvocationUsage(exchangeIDs []string, byExchange map[string]capture.ModelContextInspection) (UsageTotals, bool) {
	if len(exchangeIDs) == 0 {
		return UsageTotals{}, true
	}
	var totals UsageTotals
	totals.Input.Observed, totals.CacheCreation.Observed, totals.CacheRead.Observed, totals.Output.Observed = true, true, true, true
	joined := 0
	for _, exchangeID := range exchangeIDs {
		context, ok := byExchange[exchangeID]
		if !ok {
			continue
		}
		joined++
		totals.Input.Value += context.InputTokens
		totals.Input.Observed = totals.Input.Observed && context.InputTokensObserved
		totals.CacheCreation.Value += context.CacheCreationInputTokens
		totals.CacheCreation.Observed = totals.CacheCreation.Observed && context.CacheCreationInputTokensObserved
		totals.CacheRead.Value += context.CacheReadInputTokens
		totals.CacheRead.Observed = totals.CacheRead.Observed && context.CacheReadInputTokensObserved
		totals.Output.Value += context.OutputTokens
		totals.Output.Observed = totals.Output.Observed && context.OutputTokensObserved
	}
	if joined == 0 {
		return UsageTotals{}, true
	}
	return totals, false
}

func addUsageToTotal(report *BehavioralReport, bucket string, usage UsageTotals) {
	var target *UsageTotals
	switch bucket {
	case usageBucketAgent:
		target = &report.Totals.Agent
	case usageBucketOverhead:
		target = &report.Totals.Overhead
	default:
		return
	}
	target.Input.Value += usage.Input.Value
	target.CacheCreation.Value += usage.CacheCreation.Value
	target.CacheRead.Value += usage.CacheRead.Value
	target.Output.Value += usage.Output.Value
	target.Input.Observed = target.Input.Observed || usage.Input.Observed
	target.CacheCreation.Observed = target.CacheCreation.Observed || usage.CacheCreation.Observed
	target.CacheRead.Observed = target.CacheRead.Observed || usage.CacheRead.Observed
	target.Output.Observed = target.Output.Observed || usage.Output.Observed
}

func fillMCP(report *BehavioralReport, streams []capture.MCPStreamInspection) {
	for _, stream := range streams {
		report.MCP = append(report.MCP, MCPStreamRow{
			File:          stream.File,
			ToolCalls:     stream.ToolCalls,
			Notifications: stream.ProgressNotifications,
			InputBytes:    stream.InputBytes,
			OutputByte:    stream.OutputBytes,
		})
	}
}

func fillFindings(report *BehavioralReport, result *BehavioralResult, verification *VerificationDocument) {
	if result.Task != nil {
		report.Findings = append(report.Findings, Finding{
			Text:       fmt.Sprintf("task result: %s %s", result.Task.Mode, result.Task.Result),
			Coordinate: "meta.json",
		})
	}
	if verification != nil {
		for _, check := range verification.Checks {
			if check.Result == CheckFailed || check.Result == CheckBlocked {
				report.Findings = append(report.Findings, Finding{
					Text:       fmt.Sprintf("check %s: %s", check.ID, check.Result),
					Coordinate: "verification.json",
				})
				break
			}
		}
	}
	if len(result.ProcessIdentity) > 0 {
		report.Findings = append(report.Findings, Finding{
			Text:       fmt.Sprintf("process identity: %d observations", len(result.ProcessIdentity)),
			Coordinate: "meta.json",
		})
	}
	unattributed := 0
	for _, row := range report.Invocations {
		if row.Unattributed {
			unattributed++
		}
	}
	if unattributed > 0 {
		report.Findings = append(report.Findings, Finding{
			Text:       fmt.Sprintf("%d invocation(s) have no joined provider exchanges", unattributed),
			Coordinate: "provider.jsonl",
		})
	}
	totalCalls := 0
	for _, stream := range report.MCP {
		totalCalls += stream.ToolCalls
	}
	if len(report.MCP) > 0 {
		report.Findings = append(report.Findings, Finding{
			Text:       fmt.Sprintf("%d total MCP tool call(s) across %d stream(s)", totalCalls, len(report.MCP)),
			Coordinate: "mcp/",
		})
	}
	if len(report.Findings) > 5 {
		report.Findings = report.Findings[:5]
	}
}

// RenderBehavioralReportText renders the six §10.5 sections as plain text.
// The process-identity line prints copied counts only — never a verdict
// word — mirroring the "no verdict authority" rule.
func RenderBehavioralReportText(report *BehavioralReport) string {
	var b []byte
	line := func(format string, args ...any) {
		b = fmt.Appendf(b, format, args...)
		b = append(b, '\n')
	}

	line("Result:")
	line("  Task:      mode=%s result=%s frozenAt=%s (source: %s)", report.Result.Task.Mode, report.Result.Task.Result, report.Result.Task.FrozenAt.Format(time.RFC3339), report.Sources["result"])
	line("  Execution: error=%q", report.Result.Execution.Error)
	line("  Task-end:  persisted=%t settled=%t", report.Result.TaskEnd.Persisted, report.Result.TaskEnd.Settled)
	line("  Capture:   status=%s valid=%t complete=%t", report.Result.Capture.Status, report.Result.Capture.Valid, report.Result.Capture.Complete)
	if report.Result.Binding != nil {
		line("  Binding:   candidate=%s project=%s", report.Result.Binding.Candidate, report.Result.Binding.ProjectID)
	}
	pi := report.Result.ProcessIdentity
	line("  Process identity: observations=%d counted=%d matchesCandidate=%d projectMismatches=%d", pi.Observations, pi.Counted, pi.MatchesCandidate, pi.ProjectMismatches)

	line("")
	line("Checks:")
	for _, check := range report.Checks {
		line("  %s: %s (expected=%q observed=%q source=%s)", check.ID, check.Result, check.Expected, check.Observed, check.Source)
	}

	line("")
	line("Invocations:")
	for _, row := range report.Invocations {
		line("  %s [%s] status=%s exchanges=%d unattributed=%t mcpCalls=%d", row.InvocationID, row.Phase, row.Status, row.ExchangeCount, row.Unattributed, row.MCPCalls)
		line("    usage: %s", renderUsage(row.Usage))
	}
	line("  totals.agent:    %s", renderUsage(report.Totals.Agent))
	line("  totals.overhead: %s", renderUsage(report.Totals.Overhead))
	phases := make([]string, 0, len(report.Totals.PhaseMapping))
	for phase := range report.Totals.PhaseMapping {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	for _, phase := range phases {
		line("  phase mapping: %s -> %s", phase, report.Totals.PhaseMapping[phase])
	}

	line("")
	line("MCP:")
	for _, stream := range report.MCP {
		line("  %s: calls=%d notifications=%d inputBytes=%d outputBytes=%d", stream.File, stream.ToolCalls, stream.Notifications, stream.InputBytes, stream.OutputByte)
	}

	line("")
	line("Findings:")
	for _, finding := range report.Findings {
		line("  - %s (%s)", finding.Text, finding.Coordinate)
	}

	line("")
	line("Gaps:")
	for _, gap := range report.Gaps {
		line("  - %s", gap)
	}

	return string(b)
}

func renderUsage(usage UsageTotals) string {
	format := func(v UsageValue) string {
		if !v.Observed {
			return TerminatedUnknown
		}
		return fmt.Sprintf("%d", v.Value)
	}
	return fmt.Sprintf("input=%s cacheCreation=%s cacheRead=%s output=%s", format(usage.Input), format(usage.CacheCreation), format(usage.CacheRead), format(usage.Output))
}

func fillGaps(report *BehavioralReport, result *BehavioralResult, verification *VerificationDocument, integrity capture.InspectionIntegrity) {
	if len(result.ProcessIdentity) == 0 {
		report.Gaps = append(report.Gaps, "no process-identity observation recorded")
	}
	usageUnobserved := false
	for _, row := range report.Invocations {
		if !row.Usage.Input.Observed || !row.Usage.CacheCreation.Observed || !row.Usage.CacheRead.Observed || !row.Usage.Output.Observed {
			usageUnobserved = true
		}
	}
	if usageUnobserved {
		report.Gaps = append(report.Gaps, "usage unobserved for at least one invocation")
	}
	if verification == nil {
		report.Gaps = append(report.Gaps, "no verification block")
	}
	if !integrity.Complete {
		report.Gaps = append(report.Gaps, "incomplete capture")
	}
	report.Gaps = append(report.Gaps, behavioralReportCleanupNote)
}
