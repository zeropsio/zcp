package projection

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

func buildEdges(view *View) {
	view.Edges = view.Edges[:0]
	seen := make(map[string]bool)
	add := func(kind, from, to, basis string, evidence []EvidenceRef) {
		if from == "" || to == "" {
			return
		}
		key := kind + "\x00" + from + "\x00" + to
		if seen[key] {
			return
		}
		seen[key] = true
		sum := sha256.Sum256([]byte(key))
		view.Edges = append(view.Edges, Edge{
			ID: fmt.Sprintf("edge:%x", sum[:8]), Kind: kind, FromID: from, ToID: to, Basis: basis,
			Evidence: append([]EvidenceRef(nil), evidence...),
		})
	}
	captureID := "capture:" + view.Capture.ID
	for _, run := range view.EvalRuns {
		runID := "eval:" + run.ID
		add("capture-has-eval", captureID, runID, "lifecycle", firstRunEvidence(run))
		for _, scenario := range run.Scenarios {
			scenarioID := "scenario:" + run.ID + ":" + scenario.ID
			add("eval-has-scenario", runID, scenarioID, "lifecycle", firstScenarioEvidence(scenario))
			for _, invocation := range scenario.Invocations {
				invocationID := "invocation:" + invocation.ID
				add("scenario-has-invocation", scenarioID, invocationID, "lifecycle", invocation.Evidence)
				if invocation.ClientSessionID != "" {
					add("invocation-bound-to-session", invocationID, "session:"+invocation.ClientSessionID, "explicit-bind", invocation.Evidence)
				}
				for _, exchangeID := range invocation.ExchangeIDs {
					add("invocation-contains-exchange", invocationID, "exchange:"+exchangeID, "session-time-window", invocation.Evidence)
				}
				for _, file := range invocation.MCPFiles {
					add("invocation-launched-mcp", invocationID, "mcp:"+file, "capture-side-channel", invocation.Evidence)
				}
			}
		}
	}
	for _, block := range view.ProviderBlocks {
		add("exchange-has-provider-block", "exchange:"+block.ExchangeID, block.ID, "decoded-stream-order", []EvidenceRef{block.Evidence})
	}
	for _, context := range view.Contexts {
		add("exchange-has-context", "exchange:"+context.ExchangeID, "context:"+context.ExchangeID, "request-identity", []EvidenceRef{context.Evidence})
	}
	for _, tool := range view.Tools {
		toolID := "tool-execution:" + tool.ID
		if tool.ProposalExchangeID != "" {
			add("exchange-proposed-tool", "exchange:"+tool.ProposalExchangeID, toolID, "provider-tool-use-id", tool.Evidence)
		}
		if tool.MCPFile != "" {
			basis := tool.CorrelationBasis
			if basis == "" {
				basis = "unknown"
			}
			add("tool-dispatched-to-mcp", toolID, "mcp:"+tool.MCPFile, basis, tool.Evidence)
		}
		if tool.ResultExchangeID != "" {
			add("tool-result-entered-context", toolID, "exchange:"+tool.ResultExchangeID, tool.Propagation, tool.Evidence)
		}
		if tool.ClientArtifact != "" {
			add("artifact-observed-tool", "client-artifact:"+tool.ClientArtifact, toolID, "stream-json-tool-use-id", tool.Evidence)
		}
	}
	for _, call := range view.MCPCalls {
		add("mcp-process-has-message", "mcp:"+call.File, "mcp-call:"+call.ID, call.CorrelationBasis, call.Evidence)
	}
	for _, event := range view.Conversation {
		add("artifact-has-event", "client-artifact:"+event.ArtifactPath, event.ID, "artifact-line", event.Evidence)
	}
	sort.Slice(view.Edges, func(i, j int) bool { return view.Edges[i].ID < view.Edges[j].ID })
}

func firstRunEvidence(run EvalRun) []EvidenceRef {
	for _, scenario := range run.Scenarios {
		if evidence := firstScenarioEvidence(scenario); len(evidence) > 0 {
			return evidence
		}
	}
	return nil
}

func firstScenarioEvidence(scenario Scenario) []EvidenceRef {
	for _, invocation := range scenario.Invocations {
		if len(invocation.Evidence) > 0 {
			return invocation.Evidence
		}
	}
	return nil
}
