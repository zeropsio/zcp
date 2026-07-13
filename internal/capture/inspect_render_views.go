package capture

import (
	"fmt"
	"io"
	"strings"
)

func RenderInspectionSummary(writer io.Writer, report *InspectionReport) error {
	integrity := "INVALID"
	if report.Integrity.Valid && report.Integrity.Complete {
		integrity = "OK"
	} else if report.Integrity.Valid {
		integrity = "INCOMPLETE"
	}
	if _, err := fmt.Fprintf(writer, "Capture: %s\nStatus: %s\nIntegrity: %s\nProvider exchanges: %d\nClaude sessions: %d\nEval runs: %d\nMCP streams: %d\n",
		report.SessionID, report.Status, integrity, report.ProviderExchanges, len(report.ClaudeSessions), len(report.EvalRuns), len(report.MCPStreams)); err != nil {
		return fmt.Errorf("render inspection summary: %w", err)
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(writer, "Warning: %s\n", warning); err != nil {
			return fmt.Errorf("render inspection warning: %w", err)
		}
	}
	for _, evalRun := range report.EvalRuns {
		if _, err := fmt.Fprintf(writer, "Eval %s status=%s scenarios=%d\n", evalRun.EvalRunID, evalRun.Status, len(evalRun.Scenarios)); err != nil {
			return fmt.Errorf("render eval summary: %w", err)
		}
	}
	return nil
}

func RenderContextInspection(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintf(writer, "Capture: %s\nModel context requests: %d\n", report.SessionID, len(report.ModelContexts)); err != nil {
		return fmt.Errorf("render context summary: %w", err)
	}
	for _, contextView := range report.ModelContexts {
		if _, err := fmt.Fprintf(writer, "%s session=%s model=%s providerMessage=%s request=%dB\n  system blocks=%d bytes=%d changed=%v\n  tools total=%d mcp=%d builtin=%d bytes=%d changed=%v\n  messages total=%d bytes=%d added=%d addedBytes=%d\n  usage input=%d cacheCreate=%d cacheRead=%d output=%d\n  source=%s seq=%d-%d\n",
			contextView.ExchangeID, contextView.ClaudeSessionID, contextView.Model, contextView.ProviderMessageID, contextView.RequestBytes,
			contextView.SystemBlocks, contextView.SystemBytes, contextView.SystemChanged,
			contextView.ToolCount, contextView.MCPToolCount, contextView.BuiltInToolCount, contextView.ToolBytes, contextView.ToolsChanged,
			contextView.MessageCount, contextView.MessageBytes, contextView.AddedMessages, contextView.AddedMessageBytes,
			contextView.InputTokens, contextView.CacheCreationInputTokens, contextView.CacheReadInputTokens, contextView.OutputTokens,
			contextView.Source.File, contextView.Source.SeqStart, contextView.Source.SeqEnd,
		); err != nil {
			return fmt.Errorf("render context exchange: %w", err)
		}
	}
	return nil
}

func RenderTimelineInspection(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintf(writer, "Capture: %s\n", report.SessionID); err != nil {
		return fmt.Errorf("render timeline summary: %w", err)
	}
	for _, evalRun := range report.EvalRuns {
		if _, err := fmt.Fprintf(writer, "Eval %s status=%s\n", evalRun.EvalRunID, evalRun.Status); err != nil {
			return err
		}
		for _, scenario := range evalRun.Scenarios {
			if _, err := fmt.Fprintf(writer, "  Scenario %s status=%s artifacts=%d\n", scenario.ScenarioRunID, scenario.Status, len(scenario.Artifacts)); err != nil {
				return err
			}
			for _, invocation := range scenario.Invocations {
				if _, err := fmt.Fprintf(writer, "    %s phase=%s session=%s status=%s exchanges=%s mcp=%s\n", invocation.InvocationID, invocation.Phase, invocation.ClaudeSessionID, invocation.Status, strings.Join(invocation.ExchangeIDs, ","), strings.Join(invocation.MCPFiles, ",")); err != nil {
					return err
				}
			}
		}
	}
	for index, correlation := range report.Correlations {
		if _, err := fmt.Fprintf(writer, "%d. %s -> %s resultBytes=%d error=%v providerResult=%v\n", index+1, correlation.ProviderToolName, correlation.MCPToolName, correlation.MCPResultBytes, correlation.MCPIsError, correlation.ProviderResultObserved); err != nil {
			return fmt.Errorf("render tool timeline: %w", err)
		}
		if err := renderEvidence(writer, "provider", correlation.ProviderSource); err != nil {
			return err
		}
		if err := renderEvidence(writer, "mcp", correlation.MCPCallSource); err != nil {
			return err
		}
	}
	return nil
}
