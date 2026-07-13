package capture

import "fmt"

type InspectionFilter struct {
	EvalRunID     string
	ScenarioRunID string
	InvocationID  string
}

// FilterInspection returns an eval-scoped logical view while retaining global
// raw integrity status. Canonical files are never copied or rewritten.
func FilterInspection(report *InspectionReport, filter InspectionFilter) (*InspectionReport, error) {
	if filter.EvalRunID == "" && filter.ScenarioRunID == "" && filter.InvocationID == "" {
		return report, nil
	}
	filtered := *report
	filtered.EvalRuns = nil
	exchangeIDs := make(map[string]bool)
	invocationIDs := make(map[string]bool)
	sessionIDs := make(map[string]bool)
	foundRun := filter.EvalRunID == ""
	foundScenario := filter.ScenarioRunID == ""
	foundInvocation := filter.InvocationID == ""
	for _, run := range report.EvalRuns {
		if filter.EvalRunID != "" && run.EvalRunID != filter.EvalRunID {
			continue
		}
		foundRun = true
		filteredRun := EvalRunInspection{EvalRunID: run.EvalRunID, Status: run.Status}
		for _, scenario := range run.Scenarios {
			if filter.ScenarioRunID != "" && scenario.ScenarioRunID != filter.ScenarioRunID {
				continue
			}
			foundScenario = true
			filteredScenario := EvalScenarioInspection{
				ScenarioRunID: scenario.ScenarioRunID,
				Status:        scenario.Status,
				Artifacts:     append([]string(nil), scenario.Artifacts...),
			}
			for _, invocation := range scenario.Invocations {
				if filter.InvocationID != "" && invocation.InvocationID != filter.InvocationID {
					continue
				}
				foundInvocation = true
				copyInvocation := invocation
				copyInvocation.ExchangeIDs = append([]string(nil), invocation.ExchangeIDs...)
				copyInvocation.MCPFiles = append([]string(nil), invocation.MCPFiles...)
				filteredScenario.Invocations = append(filteredScenario.Invocations, copyInvocation)
				invocationIDs[invocation.InvocationID] = true
				if invocation.ClaudeSessionID != "" {
					sessionIDs[invocation.ClaudeSessionID] = true
				}
				for _, exchangeID := range invocation.ExchangeIDs {
					exchangeIDs[exchangeID] = true
				}
			}
			if filter.InvocationID == "" || len(filteredScenario.Invocations) > 0 {
				filteredRun.Scenarios = append(filteredRun.Scenarios, filteredScenario)
			}
		}
		if (filter.ScenarioRunID == "" && filter.InvocationID == "") || len(filteredRun.Scenarios) > 0 {
			filtered.EvalRuns = append(filtered.EvalRuns, filteredRun)
		}
	}
	if !foundRun {
		return nil, fmt.Errorf("eval run %q not found in capture", filter.EvalRunID)
	}
	if !foundScenario {
		return nil, fmt.Errorf("eval scenario %q not found in capture", filter.ScenarioRunID)
	}
	if !foundInvocation {
		return nil, fmt.Errorf("eval invocation %q not found in capture", filter.InvocationID)
	}

	filtered.ModelContexts = nil
	for _, modelContext := range report.ModelContexts {
		if exchangeIDs[modelContext.ExchangeID] {
			filtered.ModelContexts = append(filtered.ModelContexts, modelContext)
		}
	}
	filtered.MCPStreams = nil
	for _, stream := range report.MCPStreams {
		if invocationIDs[stream.InvocationID] {
			filtered.MCPStreams = append(filtered.MCPStreams, stream)
		}
	}
	filtered.Correlations = nil
	for _, correlation := range report.Correlations {
		if exchangeIDs[correlation.ProviderSource.ExchangeID] {
			filtered.Correlations = append(filtered.Correlations, correlation)
		}
	}
	filtered.ClaudeSessions = nil
	for _, session := range report.ClaudeSessions {
		if !sessionIDs[session.SessionID] {
			continue
		}
		copySession := session
		copySession.ProviderExchanges = 0
		for _, modelContext := range filtered.ModelContexts {
			if modelContext.ClaudeSessionID == session.SessionID {
				copySession.ProviderExchanges++
			}
		}
		filtered.ClaudeSessions = append(filtered.ClaudeSessions, copySession)
	}
	filtered.ProviderExchanges = len(exchangeIDs)
	filtered.UnattributedProviderExchanges = 0
	return &filtered, nil
}
