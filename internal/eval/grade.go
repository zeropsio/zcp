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
// extracted tool calls and returns the aggregated result.
func Grade(sc *Scenario, log string, calls []ToolCall) GradeResult {
	r := GradeResult{ScenarioID: sc.ID}

	r.Failures = append(r.Failures, checkMustCallTools(sc.Expect.MustCallTools, calls)...)
	r.Failures = append(r.Failures, checkWorkflowCallsMin(sc.Expect.WorkflowCallsMin, calls)...)
	r.Failures = append(r.Failures, checkMustEnterWorkflow(sc.Expect.MustEnterWorkflow, calls)...)
	r.Failures = append(r.Failures, checkForbiddenPatterns(sc.Expect.ForbiddenPatterns, log)...)

	r.Passed = len(r.Failures) == 0
	return r
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
