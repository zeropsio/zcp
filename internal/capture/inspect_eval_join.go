package capture

import "fmt"

func assignProviderToolInvocations(toolUses []inspectedProviderToolUse, evalRuns []EvalRunInspection) {
	for toolIndex := range toolUses {
		toolUse := &toolUses[toolIndex]
		for _, evalRun := range evalRuns {
			for _, scenario := range evalRun.Scenarios {
				for _, invocation := range scenario.Invocations {
					if invocation.ClaudeSessionID != toolUse.claudeSessionID || toolUse.source.ObservedAt.Before(invocation.StartedAt) || (!invocation.EndedAt.IsZero() && toolUse.source.ObservedAt.After(invocation.EndedAt)) {
						continue
					}
					toolUse.invocationID = invocation.InvocationID
					break
				}
			}
		}
	}
}

func joinEvalMCPStreams(evalRuns []EvalRunInspection, streams []MCPStreamInspection) []string {
	var warnings []string
	for _, stream := range streams {
		if stream.InvocationID == "" {
			continue
		}
		matched := false
		for runIndex := range evalRuns {
			run := &evalRuns[runIndex]
			if run.EvalRunID != stream.EvalRunID {
				continue
			}
			for scenarioIndex := range run.Scenarios {
				scenario := &run.Scenarios[scenarioIndex]
				if scenario.ScenarioRunID != stream.ScenarioRunID {
					continue
				}
				for invocationIndex := range scenario.Invocations {
					invocation := &scenario.Invocations[invocationIndex]
					if invocation.InvocationID != stream.InvocationID {
						continue
					}
					if invocation.Phase != stream.Phase {
						warnings = append(warnings, fmt.Sprintf("MCP stream %s phase %q differs from invocation %q", stream.File, stream.Phase, invocation.Phase))
						matched = true
						break
					}
					invocation.MCPStreams++
					invocation.MCPFiles = append(invocation.MCPFiles, stream.File)
					matched = true
					break
				}
			}
		}
		if !matched {
			warnings = append(warnings, fmt.Sprintf("MCP stream %s references unknown eval invocation %s", stream.File, stream.InvocationID))
		}
	}
	return warnings
}
