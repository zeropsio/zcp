package capture

import (
	"fmt"
	"io"
	"strings"
)

func renderFullInspectionHeader(writer io.Writer, report *InspectionReport) error {
	integrity := "INVALID"
	if report.Integrity.Valid && report.Integrity.Complete {
		integrity = "OK"
	} else if report.Integrity.Valid {
		integrity = "INCOMPLETE"
	}
	if _, err := fmt.Fprintf(writer, "Capture: %s\nStatus: %s\nIntegrity: %s (%d manifest files, %d provider records, %d lifecycle records, %d MCP records, %d provenance records)\nProvider exchanges: %d\nMCP streams: %d\n",
		report.SessionID, report.Status, integrity, report.Integrity.ManifestFilesVerified, report.Integrity.ProviderRecords,
		report.Integrity.LifecycleRecords, report.Integrity.MCPRecords, report.Integrity.ProvenanceRecords,
		report.ProviderExchanges, len(report.MCPStreams)); err != nil {
		return fmt.Errorf("render inspection summary: %w", err)
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(writer, "Warning: %s\n", warning); err != nil {
			return fmt.Errorf("render inspection warning: %w", err)
		}
	}
	return nil
}

func renderFullClaudeSessions(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintln(writer, "\nClaude sessions:"); err != nil {
		return fmt.Errorf("render Claude session heading: %w", err)
	}
	for _, session := range report.ClaudeSessions {
		if _, err := fmt.Fprintf(writer, "- %s exchanges=%d models=%s\n", session.SessionID, session.ProviderExchanges, strings.Join(session.Models, ",")); err != nil {
			return fmt.Errorf("render Claude session summary: %w", err)
		}
	}
	if report.UnattributedProviderExchanges > 0 {
		if _, err := fmt.Fprintf(writer, "- unattributed exchanges=%d\n", report.UnattributedProviderExchanges); err != nil {
			return fmt.Errorf("render unattributed provider summary: %w", err)
		}
	}
	return nil
}

func renderFullContexts(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintln(writer, "\nModel context requests:"); err != nil {
		return fmt.Errorf("render model context heading: %w", err)
	}
	for _, modelContext := range report.ModelContexts {
		if _, err := fmt.Fprintf(writer, "- %s session=%s model=%s request=%dB system=%d/%dB tools=%d(mcp=%d,builtin=%d)/%dB messages=%d/%dB added=%d/%dB systemChanged=%v toolsChanged=%v tokens[in=%d cacheCreate=%d cacheRead=%d out=%d]\n",
			modelContext.ExchangeID, modelContext.ClaudeSessionID, modelContext.Model, modelContext.RequestBytes,
			modelContext.SystemBlocks, modelContext.SystemBytes, modelContext.ToolCount, modelContext.MCPToolCount,
			modelContext.BuiltInToolCount, modelContext.ToolBytes, modelContext.MessageCount, modelContext.MessageBytes,
			modelContext.AddedMessages, modelContext.AddedMessageBytes, modelContext.SystemChanged, modelContext.ToolsChanged,
			modelContext.InputTokens, modelContext.CacheCreationInputTokens, modelContext.CacheReadInputTokens, modelContext.OutputTokens); err != nil {
			return fmt.Errorf("render model context request: %w", err)
		}
	}
	return nil
}

func renderFullEvalRuns(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintln(writer, "\nEval runs:"); err != nil {
		return fmt.Errorf("render eval run heading: %w", err)
	}
	for _, evalRun := range report.EvalRuns {
		if _, err := fmt.Fprintf(writer, "- eval %s status=%s scenarios=%d\n", evalRun.EvalRunID, evalRun.Status, len(evalRun.Scenarios)); err != nil {
			return fmt.Errorf("render eval run: %w", err)
		}
		for _, scenario := range evalRun.Scenarios {
			if _, err := fmt.Fprintf(writer, "  - scenario %s status=%s invocations=%d artifacts=%d\n", scenario.ScenarioRunID, scenario.Status, len(scenario.Invocations), len(scenario.Artifacts)); err != nil {
				return fmt.Errorf("render eval scenario: %w", err)
			}
			for _, invocation := range scenario.Invocations {
				if _, err := fmt.Fprintf(writer, "    - %s phase=%s session=%s status=%s exchanges=%d mcp=%d\n", invocation.InvocationID, invocation.Phase, invocation.ClaudeSessionID, invocation.Status, invocation.ProviderExchanges, invocation.MCPStreams); err != nil {
					return fmt.Errorf("render eval invocation: %w", err)
				}
			}
		}
	}
	return nil
}

func renderFullMCPStreams(writer io.Writer, report *InspectionReport) error {
	if _, err := fmt.Fprintln(writer, "\nMCP process streams:"); err != nil {
		return fmt.Errorf("render MCP stream heading: %w", err)
	}
	for _, stream := range report.MCPStreams {
		if _, err := fmt.Fprintf(writer, "- %s status=%s input=%d output=%d toolCalls=%d progress=%d invocation=%s phase=%s\n", stream.File, stream.Status, stream.InputBytes, stream.OutputBytes, stream.ToolCalls, stream.ProgressNotifications, stream.InvocationID, stream.Phase); err != nil {
			return fmt.Errorf("render MCP stream summary: %w", err)
		}
	}
	return nil
}

func renderFullCorrelations(writer io.Writer, correlations []ToolCorrelation) error {
	if _, err := fmt.Fprintln(writer, "\nCausal tool timeline:"); err != nil {
		return fmt.Errorf("render inspection timeline heading: %w", err)
	}
	for index, correlation := range correlations {
		if err := renderFullCorrelation(writer, index+1, correlation); err != nil {
			return err
		}
	}
	return nil
}

func renderFullCorrelation(writer io.Writer, index int, correlation ToolCorrelation) error {
	if _, err := fmt.Fprintf(writer, "\n%d. MODEL -> MCP %s args=%s\n", index, correlation.MCPToolName, inspectionPreview(correlation.ArgumentsJSON, inspectionArgumentPreviewBytes)); err != nil {
		return fmt.Errorf("render model tool call: %w", err)
	}
	if err := renderEvidence(writer, "provider", correlation.ProviderSource); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "   MCP call id=%s arguments=%s\n", correlation.MCPRequestID, equalityLabel(correlation.ArgumentsEqual)); err != nil {
		return fmt.Errorf("render MCP tool call: %w", err)
	}
	if err := renderEvidence(writer, "mcp call", correlation.MCPCallSource); err != nil {
		return err
	}
	if correlation.MCPResultSource.File == "" {
		return nil
	}
	return renderFullCorrelationResult(writer, correlation)
}

func renderFullCorrelationResult(writer io.Writer, correlation ToolCorrelation) error {
	resultKind := "OK"
	if correlation.MCPIsError {
		resultKind = "ERROR"
	}
	if _, err := fmt.Fprintf(writer, "   MCP <- %s (%d bytes) %s\n", resultKind, correlation.MCPResultBytes, inspectionPreview(correlation.MCPResultText, inspectionResultPreviewBytes)); err != nil {
		return fmt.Errorf("render MCP result: %w", err)
	}
	if err := renderEvidence(writer, "mcp result", correlation.MCPResultSource); err != nil {
		return err
	}
	if err := renderFullSources(writer, correlation); err != nil {
		return err
	}
	providerResult := "missing"
	if correlation.ProviderResultObserved {
		providerResult = "verbatim"
	}
	if _, err := fmt.Fprintf(writer, "   provider tool_result: %s\n", providerResult); err != nil {
		return fmt.Errorf("render provider result equality: %w", err)
	}
	if correlation.ProviderResultObserved {
		return renderEvidence(writer, "provider result", correlation.ProviderResultSource)
	}
	return nil
}

func renderFullSources(writer io.Writer, correlation ToolCorrelation) error {
	for _, source := range correlation.SourceMatches {
		if _, err := fmt.Fprintf(writer, "   current-corpus exact source: %s (%s, %d bytes)\n", source.AtomID, source.File, source.MatchedBytes); err != nil {
			return fmt.Errorf("render current-corpus source match: %w", err)
		}
	}
	for _, composition := range correlation.CompositionMatches {
		if _, err := fmt.Fprintf(writer, "   capture-time composition: %s record=%d surface=%s output=%d bytes\n", composition.File, composition.Record, composition.Surface, composition.OutputBytes); err != nil {
			return fmt.Errorf("render composition source match: %w", err)
		}
		for _, component := range composition.Components {
			if _, err := fmt.Fprintf(writer, "     - %s owner=%s span=%d:%d\n", component.Kind, component.Owner, component.Start, component.End); err != nil {
				return fmt.Errorf("render composition component: %w", err)
			}
		}
	}
	return nil
}
