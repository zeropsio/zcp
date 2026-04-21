package eval

import (
	"fmt"
	"strings"
)

const workflowToolName = "zerops_workflow"

// GradeResult summarizes whether the scenario passed its expectations.
type GradeResult struct {
	ScenarioID string   `json:"scenarioId"`
	Passed     bool     `json:"passed"`
	Failures   []string `json:"failures,omitempty"`
}

// Grade runs every expectation in the scenario against the captured log +
// extracted tool calls and (if RequireAssessment) the agent's EVAL REPORT.
// An empty assessment is treated as "not reported"; callers extract it via
// ExtractAssessment before calling Grade. Scenarios that set FinalURLStatus
// must use GradeWithProbe instead — Grade skips the HTTP check.
func Grade(sc *Scenario, log string, calls []ToolCall, assessment string) GradeResult {
	return GradeWithProbe(sc, log, calls, assessment, nil)
}

// GradeWithProbe is Grade plus the final-URL HTTP probe assertion. Runner
// calls this so the grader can gate on "the deployed app actually answers
// over the internet" — closing the gap that makes a deploy look successful
// in the control plane while returning 502 externally.
func GradeWithProbe(sc *Scenario, log string, calls []ToolCall, assessment string, probe *FinalURLProbe) GradeResult {
	r := GradeResult{ScenarioID: sc.ID}

	r.Failures = append(r.Failures, checkMustCallTools(sc.Expect.MustCallTools, calls)...)
	r.Failures = append(r.Failures, checkWorkflowCallsMin(sc.Expect.WorkflowCallsMin, calls)...)
	r.Failures = append(r.Failures, checkMustEnterWorkflow(sc.Expect.MustEnterWorkflow, calls)...)
	r.Failures = append(r.Failures, checkForbiddenPatterns(sc.Expect.ForbiddenPatterns, log)...)
	r.Failures = append(r.Failures, checkRequiredPatterns(sc.Expect.RequiredPatterns, calls)...)
	r.Failures = append(r.Failures, checkAssessment(sc.Expect.RequireAssessment, assessment)...)
	r.Failures = append(r.Failures, checkFinalURLStatus(sc.Expect.FinalURLStatus, probe)...)

	r.Passed = len(r.Failures) == 0
	return r
}

// checkFinalURLStatus asserts the runner's end-to-end HTTP probe hit the
// expected status. An expectation of 0 disables the check (default for
// scenarios that don't opt in). A non-zero expectation with nil probe is
// always a failure — it means the scenario author wired the assertion but
// the runner didn't (or couldn't) execute the probe.
func checkFinalURLStatus(want int, probe *FinalURLProbe) []string {
	if want == 0 {
		return nil
	}
	if probe == nil {
		return []string{fmt.Sprintf("finalUrlStatus: expected %d but runner did not execute the probe (set finalUrlHostname on the scenario)", want)}
	}
	if probe.Err != "" {
		return []string{fmt.Sprintf("finalUrlStatus: probe of %s failed: %s", probe.URL, probe.Err)}
	}
	if probe.Got != want {
		return []string{fmt.Sprintf("finalUrlStatus: GET %s returned %d, want %d", probe.URL, probe.Got, want)}
	}
	return nil
}

// checkAssessment gates a scenario on the agent's own EVAL REPORT self-assessment.
// When RequireAssessment is true, the assessment must be non-empty AND report
// "State: SUCCESS" under the "Deployment outcome" section. The grader reuses
// isSuccessfulAssessment from runner.go — recipe eval and scenario eval share
// the same success criteria.
func checkAssessment(required bool, assessment string) []string {
	if !required {
		return nil
	}
	if assessment == "" {
		return []string{"requireAssessment: agent did not produce an '## EVAL REPORT' self-assessment"}
	}
	if !isSuccessfulAssessment(assessment) {
		return []string{"requireAssessment: 'Deployment outcome' did not report State: SUCCESS"}
	}
	return nil
}

func checkMustCallTools(want []string, calls []ToolCall) []string {
	called := make(map[string]bool, len(calls))
	for _, c := range calls {
		called[c.Name] = true
	}
	var failures []string
	for _, tool := range want {
		if !called[tool] {
			failures = append(failures, fmt.Sprintf("mustCallTools: %q never called", tool))
		}
	}
	return failures
}

func checkWorkflowCallsMin(minCalls int, calls []ToolCall) []string {
	if minCalls <= 0 {
		return nil
	}
	count := 0
	for _, c := range calls {
		if c.Name == workflowToolName {
			count++
		}
	}
	if count < minCalls {
		return []string{fmt.Sprintf("workflowCallsMin: got %d, want >= %d", count, minCalls)}
	}
	return nil
}

func checkMustEnterWorkflow(want []string, calls []ToolCall) []string {
	entered := make(map[string]bool)
	for _, c := range calls {
		if c.Name != workflowToolName {
			continue
		}
		// Input is JSON — cheap substring match on action=start + workflow=<name>
		// avoids pulling in a full parser for a regression check.
		if !strings.Contains(c.Input, `"action":"start"`) && !strings.Contains(c.Input, `"action": "start"`) {
			continue
		}
		for _, name := range []string{"bootstrap", "develop", "recipe", "cicd"} {
			if strings.Contains(c.Input, `"workflow":"`+name+`"`) || strings.Contains(c.Input, `"workflow": "`+name+`"`) {
				entered[name] = true
			}
		}
	}
	var failures []string
	for _, name := range want {
		if !entered[name] {
			failures = append(failures, fmt.Sprintf("mustEnterWorkflow: %q never started", name))
		}
	}
	return failures
}

func checkForbiddenPatterns(patterns []string, log string) []string {
	var failures []string
	for _, p := range patterns {
		if strings.Contains(log, p) {
			failures = append(failures, fmt.Sprintf("forbiddenPattern %q present in log", p))
		}
	}
	return failures
}

// checkRequiredPatterns asserts each pattern appears in at least one tool
// call's Input OR Result. Used to gate on structural choices the agent made
// or signals it received — e.g. `"route":"classic"` (agent input) or
// `"collisions":["db"]` (discovery response). Match is substring-based with
// JSON-spacing normalization, so a single canonical form catches both
// `"key":"value"` and `"key": "value"`.
//
// Scanning both Input and Result is load-bearing for state-detection
// scenarios: the things we want to prove the agent saw (collisions,
// routeOptions, resume sessions) come back as tool call results, not the
// agent's own inputs.
func checkRequiredPatterns(patterns []string, calls []ToolCall) []string {
	if len(patterns) == 0 {
		return nil
	}
	normalize := func(s string) string { return strings.ReplaceAll(s, `": "`, `":"`) }
	// Concatenate Input + a separator + Result for each call, normalized
	// once. Cheap enough, and a single Contains walk hits both halves.
	corpus := make([]string, 0, len(calls))
	for _, c := range calls {
		corpus = append(corpus, normalize(c.Input)+"\x00"+normalize(c.Result))
	}
	var failures []string
	for _, p := range patterns {
		needle := normalize(p)
		hit := false
		for _, entry := range corpus {
			if strings.Contains(entry, needle) {
				hit = true
				break
			}
		}
		if !hit {
			failures = append(failures, fmt.Sprintf("requiredPattern %q never seen in any tool call input or result", p))
		}
	}
	return failures
}
