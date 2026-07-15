package projection

import (
	"sort"
	"strings"
)

type metricInputs struct {
	providerErrors        int
	providerRequestBytes  int64
	providerResponseBytes int64
	largestResponseBytes  int64
	providerDurations     []float64
	providerWaits         []float64

	contextResets        int
	removedMessages      int
	historyRewrites      int
	rewrittenMessages    int
	contextAddedMessages int
	contextAddedBytes    int
	contextSystemChanges int
	contextToolChanges   int
	contextRequestSizes  []float64
	contextMessageSizes  []float64
	contextToolSizes     []float64

	mcpRequests      int
	mcpNotifications int
	mcpFailures      int
	mcpDurations     []float64

	toolMCP       int
	toolBuiltin   int
	toolDifferent int
	toolMissing   int
	toolAmbiguous int

	providerTextBytes      int
	providerThinkingBytes  int
	providerInputJSONBytes int
	providerTextBlocks     int
	providerThinkingBlocks int
	providerToolBlocks     int

	provenanceOccurrences int
	provenanceBytes       int
	artifactBytes         int64
}

type metricFactory struct{ scope string }

func (factory metricFactory) known(id, name, unit string, value float64, sampleCount int, basis, description string) Metric {
	return Metric{
		ID: id, Name: name, Unit: unit, Scope: factory.scope, Value: metricNumber(value),
		Denominator: metricNumber(float64(sampleCount)), SampleCount: sampleCount,
		EvidenceBasis: basis, Description: description,
	}
}

func (factory metricFactory) ratio(id, name string, value float64, sampleCount int, denominator float64, basis, description string) Metric {
	metric := factory.known(id, name, "count", value, sampleCount, basis, description)
	metric.Denominator = metricNumber(denominator)
	return metric
}

func buildMetrics(view *View) {
	inputs := collectMetricInputs(view)
	factory := metricFactory{scope: "capture:" + view.Capture.ID}
	view.Metrics = append(view.Metrics, captureIntegrityScopeMetrics(view, factory, inputs)...)
	view.Metrics = append(view.Metrics, providerContextMetrics(view, factory, inputs)...)
	view.Metrics = append(view.Metrics, mcpToolMetrics(view, factory, inputs)...)
	view.Metrics = append(view.Metrics, clientProvenanceMetrics(view, factory, inputs)...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "provider.exchange.duration", "Provider exchange duration", "ms", inputs.providerDurations, len(view.Exchanges), "raw-timestamps")...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "provider.wait", "Provider wait", "ms", inputs.providerWaits, len(view.Exchanges), "raw-timestamps")...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "mcp.request.duration", "MCP request duration", "ms", inputs.mcpDurations, inputs.mcpRequests, "jsonrpc-id")...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "context.request.size", "Context request size", "bytes", inputs.contextRequestSizes, len(view.Contexts), "raw")...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "context.message.size", "Context message section size", "bytes", inputs.contextMessageSizes, len(view.Contexts), "derived-exact")...)
	view.Metrics = append(view.Metrics, distributionMetrics(factory.scope, "context.tool_schema.size", "Context tool-schema section size", "bytes", inputs.contextToolSizes, len(view.Contexts), "derived-exact")...)
	view.Metrics = append(view.Metrics,
		usageMetric(view, "provider.tokens.input", "Provider input tokens", func(snapshot ContextSnapshot) (int64, bool) {
			return snapshot.InputTokens, snapshot.InputTokensObserved
		}),
		usageMetric(view, "provider.tokens.cache_creation", "Provider cache-creation input tokens", func(snapshot ContextSnapshot) (int64, bool) {
			return snapshot.CacheCreationInputTokens, snapshot.CacheCreationInputTokensObserved
		}),
		usageMetric(view, "provider.tokens.cache_read", "Provider cache-read input tokens", func(snapshot ContextSnapshot) (int64, bool) {
			return snapshot.CacheReadInputTokens, snapshot.CacheReadInputTokensObserved
		}),
		usageMetric(view, "provider.tokens.output", "Provider output tokens", func(snapshot ContextSnapshot) (int64, bool) {
			return snapshot.OutputTokens, snapshot.OutputTokensObserved
		}),
	)
	applyMetricMissingSemantics(view)
	attachMetricEvidence(view)
}

func collectMetricInputs(view *View) metricInputs {
	inputs := metricInputs{
		providerDurations:   make([]float64, 0, len(view.Exchanges)),
		providerWaits:       make([]float64, 0, len(view.Exchanges)),
		contextRequestSizes: make([]float64, 0, len(view.Contexts)),
		contextMessageSizes: make([]float64, 0, len(view.Contexts)),
		contextToolSizes:    make([]float64, 0, len(view.Contexts)),
		mcpDurations:        make([]float64, 0, len(view.MCPCalls)),
	}
	for _, exchange := range view.Exchanges {
		inputs.providerRequestBytes += exchange.RequestBytes
		inputs.providerResponseBytes += exchange.ResponseBytes
		if exchange.ResponseBytes > inputs.largestResponseBytes {
			inputs.largestResponseBytes = exchange.ResponseBytes
		}
		if exchange.ErrorPresent || exchange.Status == statusError {
			inputs.providerErrors++
		}
		if exchange.TimingObserved {
			inputs.providerDurations = append(inputs.providerDurations, float64(exchange.DurationMs))
		}
		if exchange.ProviderWaitObserved {
			inputs.providerWaits = append(inputs.providerWaits, float64(exchange.ProviderWaitMs))
		}
	}
	collectContextMetricInputs(view, &inputs)
	collectMCPToolMetricInputs(view, &inputs)
	collectProviderProvenanceMetricInputs(view, &inputs)
	return inputs
}

func collectContextMetricInputs(view *View, inputs *metricInputs) {
	for _, context := range view.Contexts {
		inputs.contextRequestSizes = append(inputs.contextRequestSizes, float64(context.RequestBytes))
		inputs.contextMessageSizes = append(inputs.contextMessageSizes, float64(context.MessageBytes))
		inputs.contextToolSizes = append(inputs.contextToolSizes, float64(context.ToolBytes))
		inputs.contextAddedMessages += context.AddedMessages
		inputs.contextAddedBytes += context.AddedMessageBytes
		inputs.removedMessages += context.RemovedMessages
		inputs.rewrittenMessages += context.RewrittenMessages
		if context.ContextReset {
			inputs.contextResets++
		}
		if context.HistoryRewritten {
			inputs.historyRewrites++
		}
		if context.SystemChanged {
			inputs.contextSystemChanges++
		}
		if context.ToolsChanged {
			inputs.contextToolChanges++
		}
	}
}

func collectMCPToolMetricInputs(view *View, inputs *metricInputs) {
	for _, call := range view.MCPCalls {
		switch call.Kind {
		case mcpMessageRequest:
			inputs.mcpRequests++
		case mcpMessageNotification:
			inputs.mcpNotifications++
		}
		if call.Status == statusError || call.Status == "tool-error" || call.Status == "pending" || call.Kind == "unmatched-response" {
			inputs.mcpFailures++
		}
		if call.Kind == mcpMessageRequest && call.TimingObserved {
			inputs.mcpDurations = append(inputs.mcpDurations, float64(call.DurationMs))
		}
	}
	for _, tool := range view.Tools {
		if tool.Category == toolCategoryMCP {
			inputs.toolMCP++
		} else {
			inputs.toolBuiltin++
		}
		switch tool.Propagation {
		case propagationDifferent:
			inputs.toolDifferent++
		case propagationMissing:
			inputs.toolMissing++
		case propagationAmbiguous:
			inputs.toolAmbiguous++
		}
	}
}

func collectProviderProvenanceMetricInputs(view *View, inputs *metricInputs) {
	for _, block := range view.ProviderBlocks {
		inputs.providerTextBytes += block.TextBytes
		inputs.providerThinkingBytes += block.ThinkingBytes
		inputs.providerInputJSONBytes += block.InputJSONBytes
		switch block.Type {
		case blockTypeText:
			inputs.providerTextBlocks++
		case traceKindThinking, blockTypeRedactedThinking:
			inputs.providerThinkingBlocks++
		case blockTypeToolUse, blockTypeServerToolUse:
			inputs.providerToolBlocks++
		}
	}
	for _, source := range view.Sources {
		inputs.provenanceOccurrences += source.Occurrences
		inputs.provenanceBytes += source.MatchedBytes
	}
	for _, artifact := range view.Artifacts {
		inputs.artifactBytes += artifact.SizeBytes
	}
}

func captureIntegrityScopeMetrics(view *View, factory metricFactory, inputs metricInputs) []Metric {
	return []Metric{
		factory.known("capture.bundle.bytes", "Canonical bundle size", "bytes", float64(view.Overview.BundleBytes), len(view.RawFiles), "manifest", "Sum of manifest-inventoried canonical file sizes."),
		factory.known("graph.edges", "Evidence graph edges", "count", float64(len(view.Edges)), len(view.Edges), "derived-exact", "Explicit versioned relationships with stated join bases."),
		factory.known("capture.duration", "Capture duration", "ms", float64(view.Capture.DurationMs), boolCount(!view.Capture.EndedAt.IsZero()), "manifest", "Terminal manifest end minus start."),
		factory.known("integrity.valid", "Integrity checks valid", "boolean", float64(boolCount(view.Integrity.Valid)), 1, "manifest-validation", "All performed structural/hash checks passed."),
		factory.known("integrity.complete", "Capture complete", "boolean", float64(boolCount(view.Integrity.Complete)), 1, "terminal-records", "Manifest and all required streams are terminal and complete."),
		factory.known("integrity.manifest_files_verified", "Manifest files verified", "count", float64(view.Integrity.ManifestFilesVerified), len(view.RawFiles), "manifest-validation", "Inventoried files whose size and SHA-256 were verified."),
		factory.known("integrity.warnings", "Inspection warnings", "count", float64(view.Integrity.WarningCount), 1, "derived-exact", "Structural inspection warnings; not semantic findings."),
		factory.known("integrity.provider_records", "Provider records", "count", float64(view.Integrity.ProviderRecords), 1, "raw", "Validated provider JSONL records."),
		factory.known("integrity.lifecycle_records", "Lifecycle records", "count", float64(view.Integrity.LifecycleRecords), 1, "raw", "Validated lifecycle JSONL records."),
		factory.known("integrity.mcp_records", "MCP records", "count", float64(view.Integrity.MCPRecords), len(view.MCPProcesses), "raw", "Validated MCP JSONL records."),
		factory.known("integrity.provenance_records", "Provenance records", "count", float64(view.Integrity.ProvenanceRecords), 1, "raw", "Validated capture-time composition records."),
		factory.known("capture.files", "Canonical files", "count", float64(len(view.RawFiles)), len(view.RawFiles), "manifest", "Manifest-inventoried canonical files."),
		factory.known("capture.artifacts", "Eval artifacts", "count", float64(len(view.Artifacts)), len(view.Artifacts), "manifest", "Manifest-inventoried eval artifacts."),
		factory.known("capture.artifact_bytes", "Eval artifact bytes", "bytes", float64(inputs.artifactBytes), len(view.Artifacts), "manifest", "Exact manifest sizes of eval artifacts."),
		factory.known("scope.eval_runs", "Eval runs", "count", float64(view.Overview.EvalRuns), view.Overview.EvalRuns, "lifecycle", "Explicit eval run scopes."),
		factory.known("scope.scenarios", "Scenario runs", "count", float64(view.Overview.Scenarios), view.Overview.Scenarios, "lifecycle", "Explicit scenario scopes."),
		factory.known("scope.invocations", "Client invocations", "count", float64(view.Overview.Invocations), view.Overview.Invocations, "lifecycle", "Explicit invocation phase scopes."),
		factory.known("scope.client_sessions", "Observed client sessions", "count", float64(view.Overview.ClientSessions), view.Overview.ClientSessions, "client-adapter", "Verified provider client-session identities."),
	}
}

func providerContextMetrics(view *View, factory metricFactory, inputs metricInputs) []Metric {
	return []Metric{
		factory.known("provider.exchanges", "Provider exchanges", "count", float64(view.Overview.ProviderExchanges), view.Overview.ProviderExchanges, "raw", "HTTP exchanges reconstructed from provider record boundaries."),
		factory.known("provider.sse.events", "Provider SSE events", "count", float64(len(view.ProviderEvents)), len(view.ProviderEvents), "decoded-stream-order", "Every framed provider SSE data event with decoded offset."),
		factory.known("provider.content_blocks", "Provider content blocks", "count", float64(len(view.ProviderBlocks)), len(view.ProviderBlocks), "decoded-stream-order", "All provider content block types, including text, thinking, and server tools."),
		factory.known("provider.exchanges.unattributed", "Unattributed provider exchanges", "count", float64(view.Overview.UnattributedExchanges), view.Overview.ProviderExchanges, "derived-exact", "Exchanges without verified client-session identity."),
		factory.known("provider.request.bytes.total", "Messages request bytes", "bytes", float64(view.Overview.TotalRequestBytes), len(view.Contexts), "raw", "Sum of exact Messages request entity bytes."),
		factory.known("provider.request.bytes.largest", "Largest Messages request", "bytes", float64(view.Overview.LargestRequestBytes), len(view.Contexts), "raw", "Largest exact Messages request entity."),
		factory.known("provider.http.request_bytes", "All provider request bytes", "bytes", float64(inputs.providerRequestBytes), len(view.Exchanges), "raw", "Exact request entity bytes over every provider exchange."),
		factory.known("provider.http.response_bytes", "All provider response bytes", "bytes", float64(inputs.providerResponseBytes), len(view.Exchanges), "raw", "Exact response entity bytes over every provider exchange."),
		factory.known("provider.http.largest_response", "Largest provider response", "bytes", float64(inputs.largestResponseBytes), len(view.Exchanges), "raw", "Largest exact provider response entity."),
		factory.ratio("provider.http.errors", "Provider exchange errors", float64(inputs.providerErrors), len(view.Exchanges), float64(len(view.Exchanges)), "raw", "Exchange terminal errors or error status in capture records."),
		factory.known("context.system.bytes.cumulative", "System bytes, cumulative", "bytes", float64(view.Overview.TotalSystemBytes), len(view.Contexts), "derived-exact", "JSON wire-fragment bytes summed across context snapshots."),
		factory.known("context.tools.bytes.cumulative", "Tool-schema bytes, cumulative", "bytes", float64(view.Overview.TotalToolSchemaBytes), len(view.Contexts), "derived-exact", "Tool definition JSON bytes summed across context snapshots."),
		factory.known("context.messages.bytes.cumulative", "Message bytes, cumulative", "bytes", float64(view.Overview.TotalMessageBytes), len(view.Contexts), "derived-exact", "Message JSON bytes summed across context snapshots."),
		factory.known("context.snapshots", "Model context snapshots", "count", float64(len(view.Contexts)), len(view.Contexts), "request-identity", "Parsed Messages request contexts."),
		factory.known("context.messages.added", "Messages added", "count", float64(inputs.contextAddedMessages), len(view.Contexts), "normalized-prefix-equality", "Messages after the shared request-to-request history prefix."),
		factory.known("context.messages.added_bytes", "Added message bytes", "bytes", float64(inputs.contextAddedBytes), len(view.Contexts), "derived-exact", "JSON bytes of messages after the shared history prefix."),
		factory.known("context.system_changes", "System context changes", "count", float64(inputs.contextSystemChanges), len(view.Contexts), "byte-equality", "Request transitions whose canonical system JSON changed."),
		factory.known("context.tool_schema_changes", "Tool-schema changes", "count", float64(inputs.contextToolChanges), len(view.Contexts), "byte-equality", "Request transitions whose canonical tool definitions changed."),
		factory.known("context.resets", "Context history resets", "count", float64(inputs.contextResets), len(view.Contexts), "request-prefix-equality", "Snapshots where prior message history is no longer a complete prefix and shrank."),
		factory.known("context.messages.removed", "Messages removed at context resets", "count", float64(inputs.removedMessages), len(view.Contexts), "request-prefix-equality", "Prior-prefix messages absent after a shorter next request."),
		factory.known("context.history.rewrites", "Context history rewrites", "count", float64(inputs.historyRewrites), len(view.Contexts), "normalized-prefix-equality", "Prior normalized history diverged without shrinking; not labeled as a reset."),
		factory.known("context.messages.rewritten", "Messages at rewritten suffixes", "count", float64(inputs.rewrittenMessages), len(view.Contexts), "normalized-prefix-equality", "Prior messages after the shared normalized prefix in rewrite events."),
		factory.known("provider.blocks.text", "Provider text blocks", "count", float64(inputs.providerTextBlocks), len(view.ProviderBlocks), "decoded-stream-order", "Provider content blocks typed text."),
		factory.known("provider.blocks.thinking", "Provider thinking blocks", "count", float64(inputs.providerThinkingBlocks), len(view.ProviderBlocks), "decoded-stream-order", "Provider thinking or redacted-thinking blocks; content remains gated."),
		factory.known("provider.blocks.tool", "Provider tool-use blocks", "count", float64(inputs.providerToolBlocks), len(view.ProviderBlocks), "decoded-stream-order", "Provider tool_use and server_tool_use blocks."),
		factory.known("provider.text.bytes", "Provider text bytes", "bytes", float64(inputs.providerTextBytes), len(view.ProviderBlocks), "decoded-stream-order", "Decoded text string bytes without exposing content."),
		factory.known("provider.thinking.bytes", "Provider thinking bytes", "bytes", float64(inputs.providerThinkingBytes), len(view.ProviderBlocks), "decoded-stream-order", "Decoded thinking/data string bytes without exposing content."),
		factory.known("provider.tool_input.bytes", "Provider tool input JSON bytes", "bytes", float64(inputs.providerInputJSONBytes), len(view.ProviderBlocks), "decoded-stream-order", "Decoded tool input JSON delta bytes."),
	}
}

func mcpToolMetrics(view *View, factory metricFactory, inputs metricInputs) []Metric {
	propagationTotal := view.Overview.PropagationExact + view.Overview.PropagationMissing
	return []Metric{
		factory.known("mcp.processes", "MCP processes", "count", float64(view.Overview.MCPProcesses), view.Overview.MCPProcesses, "raw", "Manifest-inventoried MCP stdio streams."),
		factory.known("mcp.stdin.bytes", "MCP stdin bytes", "bytes", float64(view.Overview.MCPInputBytes), view.Overview.MCPProcesses, "raw", "Exact captured MCP client-to-server bytes."),
		factory.known("mcp.stdout.bytes", "MCP stdout bytes", "bytes", float64(view.Overview.MCPOutputBytes), view.Overview.MCPProcesses, "raw", "Exact captured MCP server-to-client bytes."),
		factory.known("mcp.jsonrpc.requests", "MCP JSON-RPC requests", "count", float64(inputs.mcpRequests), len(view.MCPCalls), "stream-sequence", "All framed JSON-RPC requests, not only tools/call."),
		factory.known("mcp.jsonrpc.notifications", "MCP JSON-RPC notifications", "count", float64(inputs.mcpNotifications), len(view.MCPCalls), "stream-sequence", "All framed client and server notifications."),
		factory.ratio("mcp.jsonrpc.failures", "MCP JSON-RPC error/unmatched/pending", float64(inputs.mcpFailures), len(view.MCPCalls), float64(len(view.MCPCalls)), "jsonrpc-id", "Protocol errors, tool errors, unmatched responses, and requests without a response."),
		factory.known("tools.executions", "Tool executions", "count", float64(view.Overview.ToolExecutions), view.Overview.ToolExecutions, "derived-exact", "MCP correlations plus observed built-in client tool uses."),
		factory.known("tools.mcp", "MCP tool executions", "count", float64(inputs.toolMCP), view.Overview.ToolExecutions, "derived-exact", "Executions correlated across provider and MCP boundaries."),
		factory.known("tools.builtin", "Built-in client tool executions", "count", float64(inputs.toolBuiltin), view.Overview.ToolExecutions, "stream-json-tool-use-id", "Non-MCP tool uses observed in client stream-json artifacts."),
		factory.ratio("tools.errors", "Tool errors", float64(view.Overview.ToolErrors), view.Overview.ToolExecutions, float64(view.Overview.ToolExecutions), "derived-exact", "Executions whose canonical result reports an error."),
		factory.ratio("mcp.propagation.exact", "MCP results propagated exactly", float64(view.Overview.PropagationExact), propagationTotal, float64(propagationTotal), "canonical-result-equality", "Complete normalized MCP result structure reproduced in a later provider request."),
		factory.ratio("mcp.propagation.not_exact", "MCP results not exact", float64(view.Overview.PropagationMissing), propagationTotal, float64(propagationTotal), "canonical-result-equality", "MCP results absent, structurally different, or ambiguous in later provider context."),
		factory.known("mcp.propagation.different", "MCP result propagated differently", "count", float64(inputs.toolDifferent), inputs.toolMCP, "canonical-result-equality", "Provider tool_result exists but its complete normalized structure differs from the MCP result."),
		factory.known("mcp.propagation.missing", "MCP provider result missing", "count", float64(inputs.toolMissing), inputs.toolMCP, "tool-use-id", "No provider tool_result was found for a completed MCP result."),
		factory.known("mcp.propagation.ambiguous", "MCP propagation ambiguous", "count", float64(inputs.toolAmbiguous), inputs.toolMCP, "incomplete-evidence", "MCP result evidence is unavailable, so propagation cannot be assessed."),
	}
}

func clientProvenanceMetrics(view *View, factory metricFactory, inputs metricInputs) []Metric {
	return []Metric{
		factory.known("client.turns", "Client-reported turns", "count", float64(view.Overview.ClientTurns), clientReportCount(view.ClientRuns, func(run ClientRun) int { return run.TurnReports }), "external-artifact", "Sum of stream-json result num_turns values."),
		factory.known("client.duration", "Client-reported duration", "ms", float64(view.Overview.ClientDurationMs), clientReportCount(view.ClientRuns, func(run ClientRun) int { return run.DurationReports }), "external-artifact", "Sum of stream-json result duration_ms values."),
		factory.known("client.ttft", "Client-reported TTFT", "ms", float64(view.Overview.ClientTTFTMs), clientReportCount(view.ClientRuns, func(run ClientRun) int { return run.TTFTReports }), "external-artifact", "Sum of stream-json result ttft_ms values; not provider first-byte latency."),
		factory.known("client.cost", "Client-reported cost", "usd", view.Overview.ReportedCostUSD, clientReportCount(view.ClientRuns, func(run ClientRun) int { return run.CostReports }), "external-artifact", "Sum of stream-json total_cost_usd values."),
		factory.known("client.rate_limits", "Client rate-limit events", "count", float64(view.Overview.RateLimitEvents), len(view.ClientRuns), "external-artifact", "Observed stream-json rate_limit_event records."),
		factory.known("client.thinking_events", "Client thinking-token events", "count", float64(view.Overview.ThinkingEvents), len(view.ClientRuns), "external-artifact", "Observed stream-json thinking_tokens system records."),
		factory.known("client.permission_denials", "Client permission denials", "count", float64(view.Overview.PermissionDenials), clientResultCount(view.ClientRuns), "external-artifact", "Permission denial entries reported by stream-json result records."),
		factory.known("client.stream_events", "Client stream-json events", "count", float64(len(view.Conversation)), len(view.ClientRuns), "external-artifact", "Framed stream-json lines across transcript and retrospective artifacts."),
		factory.known("provenance.owners", "Provenance source owners", "count", float64(len(view.Sources)), len(view.Sources), "exact-match-or-composition", "Distinct current-corpus or capture-time source owners."),
		factory.known("provenance.occurrences", "Provenance occurrences", "count", float64(inputs.provenanceOccurrences), len(view.Sources), "exact-match-or-composition", "Matched source occurrences across tool results."),
		factory.known("provenance.matched_bytes", "Provenance matched bytes", "bytes", float64(inputs.provenanceBytes), len(view.Sources), "exact-match-or-composition", "Bytes attributed by exact current-corpus or capture-time composition evidence."),
	}
}

func applyMetricMissingSemantics(view *View) {
	for index := range view.Metrics {
		metric := &view.Metrics[index]
		if view.Capture.Status == statusRunning && strings.HasPrefix(metric.ID, "integrity.") {
			metric.Value = nil
			metric.SampleCount = 0
			metric.MissingCount = 1
			continue
		}
		switch metric.ID {
		case "capture.duration":
			if view.Capture.EndedAt.IsZero() {
				metric.Value = nil
				metric.MissingCount = 1
			}
		case "client.turns", "client.duration", "client.ttft", "client.cost":
			metric.MissingCount = clientResultCount(view.ClientRuns) - metric.SampleCount
			if metric.SampleCount == 0 {
				metric.Value = nil
			}
		case "client.rate_limits", "client.thinking_events":
			metric.MissingCount = len(view.ClientRuns) - metric.SampleCount
			if metric.SampleCount == 0 {
				metric.Value = nil
			}
		case "client.permission_denials":
			if metric.SampleCount == 0 {
				metric.Value = nil
			}
		}
	}
}

func distributionMetrics(scope, idPrefix, name, unit string, values []float64, expected int, basis string) []Metric {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	metrics := make([]Metric, 0, 6)
	add := func(suffix, label string, value *float64) {
		metrics = append(metrics, Metric{
			ID: idPrefix + "." + suffix, Name: name + " " + label, Unit: unit, Scope: scope,
			Value: value, Denominator: metricNumber(float64(expected)), SampleCount: len(ordered),
			MissingCount: expected - len(ordered), EvidenceBasis: basis,
			Description: "Distribution over observations with both required boundaries; missing timings remain unknown.",
		})
	}
	if len(ordered) == 0 {
		for _, item := range []struct{ suffix, label string }{{"total", "total"}, {"average", "average"}, {"minimum", "minimum"}, {"p50", "p50"}, {"p95", "p95"}, {"maximum", "maximum"}} {
			add(item.suffix, item.label, nil)
		}
		return metrics
	}
	total := float64(0)
	for _, value := range ordered {
		total += value
	}
	average := total / float64(len(ordered))
	minimum := ordered[0]
	p50 := ordered[(len(ordered)-1)*50/100]
	p95 := ordered[(len(ordered)-1)*95/100]
	maximum := ordered[len(ordered)-1]
	add("total", "total", metricNumber(total))
	add("average", "average", metricNumber(average))
	add("minimum", "minimum", metricNumber(minimum))
	add("p50", "p50", metricNumber(p50))
	add("p95", "p95", metricNumber(p95))
	add("maximum", "maximum", metricNumber(maximum))
	return metrics
}

func usageMetric(view *View, id, name string, read func(ContextSnapshot) (int64, bool)) Metric {
	var total int64
	observed := 0
	for _, snapshot := range view.Contexts {
		value, ok := read(snapshot)
		if !ok {
			continue
		}
		total += value
		observed++
	}
	metric := Metric{
		ID: id, Name: name, Unit: "tokens", Scope: "capture:" + view.Capture.ID,
		Denominator: metricNumber(float64(len(view.Contexts))), SampleCount: observed,
		MissingCount: len(view.Contexts) - observed, EvidenceBasis: "provider-reported",
		Description: "Sum over provider Messages responses with this usage field present; missing responses remain unknown.",
	}
	if observed > 0 {
		metric.Value = metricNumber(float64(total))
	}
	return metric
}

func clientReportCount(runs []ClientRun, read func(ClientRun) int) int {
	total := 0
	for _, run := range runs {
		total += read(run)
	}
	return total
}

func clientResultCount(runs []ClientRun) int {
	total := 0
	for _, run := range runs {
		total += run.ResultEvents
	}
	return total
}

func metricNumber(value float64) *float64 { return &value }

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
