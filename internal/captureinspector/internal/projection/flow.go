package projection

import (
	"fmt"
	"strings"
)

const (
	flowLaneUser    = "user"
	flowLaneContext = "context"
	flowLaneModel   = "model"
	flowLaneTool    = "tool"
)

type sessionFlowBuilder struct {
	flow                  SessionFlow
	exchanges             map[string]ProviderExchange
	contexts              map[string]ContextSnapshot
	stepsByExchange       map[string][]TraceStep
	tools                 map[string]ToolExecution
	toolOrder             []ToolExecution
	scopes                map[string]traceScope
	phaseEvidence         map[string][]EvidenceRef
	nodeIndex             map[string]int
	turnIndexByExchange   map[string]int
	contextNodeByExchange map[string]string
	toolNodeByExecution   map[string]string
	previousContextID     string
	previousModelID       string
	previousContext       ContextSnapshot
	previousExchange      ProviderExchange
}

type flowToolLink struct {
	step   TraceStep
	nodeID string
}

// buildSessionFlow projects the metadata-only trace into a deterministic causal
// map. It only emits links supported by provider order, normalized context
// continuity, explicit tool-use correlation, or lifecycle scope evidence.
func buildSessionFlow(view *View, trace *SessionTrace, exchangeIDs []string) SessionFlow {
	builder := newSessionFlowBuilder(view, trace)
	if view == nil || trace == nil {
		return builder.flow
	}
	for _, exchangeID := range exchangeIDs {
		builder.addTurn(exchangeID)
	}
	builder.addPropagationEdges()
	builder.flow.Phases = buildFlowPhases(builder.flow.Turns, builder.phaseEvidence)
	builder.flow.Summary.TurnCount = len(builder.flow.Turns)
	builder.flow.Summary.NodeCount = len(builder.flow.Nodes)
	builder.flow.Summary.EdgeCount = len(builder.flow.Edges)
	return builder.flow
}

func newSessionFlowBuilder(view *View, trace *SessionTrace) *sessionFlowBuilder {
	builder := &sessionFlowBuilder{
		flow: SessionFlow{
			Lanes: []FlowLane{
				{ID: flowLaneUser, Title: "User input", Order: 1},
				{ID: flowLaneContext, Title: "Model context", Order: 2},
				{ID: flowLaneModel, Title: "Claude", Order: 3},
				{ID: flowLaneTool, Title: "Tools", Order: 4},
			},
			Phases: []FlowPhase{}, Turns: []FlowTurn{}, Nodes: []FlowNode{}, Edges: []FlowEdge{},
		},
		exchanges: make(map[string]ProviderExchange), contexts: make(map[string]ContextSnapshot),
		stepsByExchange: make(map[string][]TraceStep), tools: make(map[string]ToolExecution), toolOrder: []ToolExecution{},
		scopes: make(map[string]traceScope), phaseEvidence: make(map[string][]EvidenceRef), nodeIndex: make(map[string]int),
		turnIndexByExchange: make(map[string]int), contextNodeByExchange: make(map[string]string),
		toolNodeByExecution: make(map[string]string),
	}
	if view == nil || trace == nil {
		return builder
	}
	for _, exchange := range view.Exchanges {
		builder.exchanges[exchange.ID] = exchange
	}
	for _, context := range view.Contexts {
		builder.contexts[context.ExchangeID] = context
	}
	for _, step := range trace.Steps {
		if step.ExchangeID != "" {
			builder.stepsByExchange[step.ExchangeID] = append(builder.stepsByExchange[step.ExchangeID], step)
		}
	}
	for _, tool := range view.Tools {
		builder.tools[tool.ID] = tool
		builder.toolOrder = append(builder.toolOrder, tool)
	}
	builder.scopes, _ = traceScopes(view)
	for _, run := range view.EvalRuns {
		for _, scenario := range run.Scenarios {
			for _, invocation := range scenario.Invocations {
				builder.phaseEvidence[invocation.ID] = append([]EvidenceRef(nil), invocation.Evidence...)
			}
		}
	}
	return builder
}

func (builder *sessionFlowBuilder) addTurn(exchangeID string) {
	exchange, exchangeFound := builder.exchanges[exchangeID]
	contextSnapshot, contextFound := builder.contexts[exchangeID]
	if !exchangeFound || !contextFound {
		return
	}
	exchangeSteps := builder.stepsByExchange[exchangeID]
	scope := builder.scopeForExchange(exchangeID, exchangeSteps)
	turnIndex := builder.appendTurn(exchange, scope, flowTurnHiddenByDefault(exchangeSteps))
	promptIDs := builder.addPromptNodes(turnIndex, exchange, scope, exchangeSteps)
	contextID := builder.addContextNode(turnIndex, exchange, contextSnapshot, scope, exchangeSteps)
	modelID := builder.addModelNode(turnIndex, exchange, scope, exchangeSteps)
	toolLinks := builder.addToolNodes(turnIndex, exchangeID, exchangeSteps)
	builder.addTurnEdges(promptIDs, contextID, modelID, toolLinks)
	builder.addContinuityEdges(exchange, contextSnapshot, contextID)
	if len(toolLinks) > 1 {
		builder.flow.Summary.BranchCount++
	}
	builder.previousContextID = contextID
	builder.previousModelID = modelID
	builder.previousContext = contextSnapshot
	builder.previousExchange = exchange
}

func (builder *sessionFlowBuilder) scopeForExchange(exchangeID string, steps []TraceStep) traceScope {
	scope := builder.scopes[exchangeID]
	for _, step := range steps {
		if scope.phase == "" {
			scope.phase = step.Phase
		}
		if scope.invocationID == "" {
			scope.invocationID = step.InvocationID
		}
	}
	return scope
}

func (builder *sessionFlowBuilder) appendTurn(exchange ProviderExchange, scope traceScope, hidden bool) int {
	order := len(builder.flow.Turns) + 1
	turn := FlowTurn{
		ID: "flow-turn:" + exchange.ID, Order: order, ExchangeID: exchange.ID,
		InvocationID: scope.invocationID, Phase: scope.phase, Model: exchange.Model,
		Status: exchange.Status, HiddenByDefault: hidden,
		StartedAt: exchange.StartedAt, EndedAt: exchange.EndedAt, NodeIDs: []string{},
	}
	builder.flow.Turns = append(builder.flow.Turns, turn)
	index := len(builder.flow.Turns) - 1
	builder.turnIndexByExchange[exchange.ID] = index
	return index
}

func (builder *sessionFlowBuilder) addPromptNodes(turnIndex int, exchange ProviderExchange, scope traceScope, steps []TraceStep) []string {
	visiblePrompts, technicalPrompts := splitFlowPrompts(steps)
	nodeIDs := []string{}
	groups := []struct {
		steps     []TraceStep
		technical bool
	}{
		{steps: visiblePrompts},
		{steps: technicalPrompts, technical: true},
	}
	for _, group := range groups {
		if len(group.steps) == 0 {
			continue
		}
		nodeID := "flow-prompt:" + exchange.ID
		if group.technical {
			nodeID = "flow-prompt-technical:" + exchange.ID
		}
		size, observed := sumFlowStepBytes(group.steps)
		node := FlowNode{
			ID: nodeID, Lane: flowLaneUser, Kind: traceKindPrompt,
			Title: flowPromptTitle(group.steps, group.technical), Status: "input",
			HiddenByDefault: group.technical, TurnID: builder.flow.Turns[turnIndex].ID,
			ExchangeID: exchange.ID, InvocationID: scope.invocationID, Phase: scope.phase,
			PrimaryBytes: size, PrimaryBytesObserved: observed, DeltaBytes: size, DeltaBytesObserved: observed,
			StartedAt:  exchange.StartedAt,
			Dimensions: []TraceSize{{Label: "new input", Bytes: size, Observed: observed}},
			StepIDs:    flowStepIDs(group.steps), Evidence: flowStepEvidence(group.steps),
		}
		builder.appendTurnNode(turnIndex, node)
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs
}

func (builder *sessionFlowBuilder) addContextNode(turnIndex int, exchange ProviderExchange, context ContextSnapshot, scope traceScope, steps []TraceStep) string {
	contextSteps := flowStepsWithKind(steps, traceKindContext)
	technicalInitialization := allFlowStepsHidden(contextSteps)
	status := "continued"
	switch {
	case context.ContextReset:
		status = "reset"
	case context.HistoryRewritten && technicalInitialization:
		status = "initialized"
	case context.HistoryRewritten:
		status = "rewritten"
	}
	nodeID := "flow-context:" + exchange.ID
	node := FlowNode{
		ID: nodeID, Lane: flowLaneContext, Kind: traceKindContext, Title: "Model context",
		Subtitle: exchange.Model, Status: status, HiddenByDefault: builder.flow.Turns[turnIndex].HiddenByDefault,
		TurnID: builder.flow.Turns[turnIndex].ID, ExchangeID: exchange.ID,
		InvocationID: scope.invocationID, Phase: scope.phase, Model: exchange.Model,
		PrimaryBytes: context.RequestBytes, PrimaryBytesObserved: true,
		DeltaBytes: context.AddedMessageBytes, DeltaBytesObserved: true,
		ContextReset: context.ContextReset, HistoryRewritten: context.HistoryRewritten && !technicalInitialization,
		StartedAt: exchange.StartedAt,
		Dimensions: []TraceSize{
			{Label: "system", Bytes: context.SystemBytes, Observed: true},
			{Label: "tool schemas", Bytes: context.ToolBytes, Observed: true},
			{Label: "messages", Bytes: context.MessageBytes, Observed: true},
			{Label: "metadata / other", Bytes: context.OtherBytes, Observed: true},
			{Label: "new messages", Bytes: context.AddedMessageBytes, Observed: true},
		},
		StepIDs:  flowStepIDs(contextSteps),
		Evidence: mergeFlowEvidence([]EvidenceRef{context.Evidence}, exchange.Evidence),
	}
	builder.appendTurnNode(turnIndex, node)
	builder.contextNodeByExchange[exchange.ID] = nodeID
	if context.RequestBytes > builder.flow.Summary.MaxContextBytes {
		builder.flow.Summary.MaxContextBytes = context.RequestBytes
	}
	return nodeID
}

func (builder *sessionFlowBuilder) addModelNode(turnIndex int, exchange ProviderExchange, scope traceScope, steps []TraceStep) string {
	modelSteps := flowModelSteps(steps)
	toolSteps := flowStepsWithKind(steps, traceKindTool)
	textSteps := flowStepsWithKind(steps, traceKindModelText)
	thinkingSteps := flowStepsWithKind(steps, traceKindThinking)
	textBytes, textObserved := sumFlowStepBytes(textSteps)
	thinkingBytes, thinkingObserved := sumFlowStepBytes(thinkingSteps)
	toolInputBytes := builder.toolInputBytes(toolSteps)
	responseComplete := exchange.Status == "complete"
	status := exchange.Status
	if exchange.ErrorPresent {
		status = statusError
	}
	nodeID := "flow-model:" + exchange.ID
	node := FlowNode{
		ID: nodeID, Lane: flowLaneModel, Kind: "model-turn",
		Title:    flowModelTitle(builder.flow.Turns[turnIndex].Order, modelSteps),
		Subtitle: exchange.Model, Status: status, HiddenByDefault: builder.flow.Turns[turnIndex].HiddenByDefault,
		TurnID: builder.flow.Turns[turnIndex].ID, ExchangeID: exchange.ID,
		InvocationID: scope.invocationID, Phase: scope.phase, Model: exchange.Model,
		StopReason: flowStopReason(modelSteps), PrimaryBytes: textBytes + thinkingBytes + toolInputBytes,
		PrimaryBytesObserved: textObserved && thinkingObserved && responseComplete,
		TextBlockCount:       len(textSteps), ThinkingBlockCount: len(thinkingSteps), ToolCount: len(toolSteps),
		StartedAt: exchange.ResponseAt, EndedAt: exchange.EndedAt,
		DurationMs: responseStreamDuration(exchange), TimingObserved: exchange.TimingObserved,
		Dimensions: []TraceSize{
			{Label: "model text", Bytes: textBytes, Observed: textObserved},
			{Label: "thinking", Bytes: thinkingBytes, Observed: thinkingObserved},
			{Label: "tool arguments", Bytes: toolInputBytes, Observed: true},
			{Label: "response wire", Bytes: int(exchange.ResponseBytes), Observed: responseComplete},
		},
		StepIDs: flowStepIDs(modelSteps), Evidence: mergeFlowEvidence(flowStepEvidence(modelSteps), exchange.Evidence),
	}
	builder.appendTurnNode(turnIndex, node)
	return nodeID
}

func (builder *sessionFlowBuilder) toolInputBytes(steps []TraceStep) int {
	total := 0
	for _, step := range steps {
		if tool, ok := builder.tools[step.ToolExecutionID]; ok {
			total += tool.ArgumentsBytes
		} else {
			total += flowDimensionBytes(step.Sizes, "arguments")
		}
	}
	return total
}

func (builder *sessionFlowBuilder) addToolNodes(turnIndex int, exchangeID string, steps []TraceStep) []flowToolLink {
	toolSteps := flowStepsWithKind(steps, traceKindTool)
	links := make([]flowToolLink, 0, len(toolSteps))
	for _, step := range toolSteps {
		nodeID := "flow-tool-step:" + step.ID
		title, subtitle := step.Title, "Observed tool proposal"
		primaryBytes, primaryObserved := step.SizeBytes, step.SizeObserved
		deltaBytes, deltaObserved := 0, false
		if tool, ok := builder.tools[step.ToolExecutionID]; ok {
			nodeID = "flow-tool:" + tool.ID
			title = firstNonEmpty(tool.ToolName, step.Title, "Tool")
			subtitle = flowToolSubtitle(tool)
			primaryBytes = tool.ResultBytes
			primaryObserved = tool.ResultBytes > 0 || !tool.CompletedAt.IsZero()
			if tool.ProviderResultBytes > 0 && primaryObserved {
				deltaBytes = tool.ProviderResultBytes - tool.ResultBytes
				deltaObserved = true
			}
			builder.toolNodeByExecution[tool.ID] = nodeID
		}
		node := FlowNode{
			ID: nodeID, Lane: flowLaneTool, Kind: traceKindTool, Title: title, Subtitle: subtitle,
			Status: step.Status, Propagation: step.Propagation, HiddenByDefault: step.HiddenByDefault,
			TurnID: builder.flow.Turns[turnIndex].ID, ExchangeID: exchangeID,
			InvocationID: step.InvocationID, Phase: step.Phase, ToolExecutionID: step.ToolExecutionID,
			PrimaryBytes: primaryBytes, PrimaryBytesObserved: primaryObserved,
			DeltaBytes: deltaBytes, DeltaBytesObserved: deltaObserved,
			StartedAt: step.StartedAt, EndedAt: step.EndedAt,
			DurationMs: step.DurationMs, TimingObserved: step.TimingObserved,
			Dimensions: append([]TraceSize(nil), step.Sizes...), StepIDs: []string{step.ID},
			Evidence: append([]EvidenceRef(nil), step.Evidence...),
		}
		builder.appendTurnNode(turnIndex, node)
		if step.ToolExecutionID != "" {
			builder.toolNodeByExecution[step.ToolExecutionID] = nodeID
		}
		links = append(links, flowToolLink{step: step, nodeID: nodeID})
	}
	return links
}

func (builder *sessionFlowBuilder) addTurnEdges(promptIDs []string, contextID, modelID string, tools []flowToolLink) {
	for _, promptID := range promptIDs {
		prompt := builder.flow.Nodes[builder.nodeIndex[promptID]]
		builder.addEdge(FlowEdge{
			Kind: "prompt-input", FromID: promptID, ToID: contextID, Status: "exact",
			Basis: "provider-request-message", HiddenByDefault: prompt.HiddenByDefault,
			Bytes: prompt.PrimaryBytes, BytesObserved: prompt.PrimaryBytesObserved,
			Evidence: append([]EvidenceRef(nil), prompt.Evidence...),
		})
	}
	context := builder.flow.Nodes[builder.nodeIndex[contextID]]
	builder.addEdge(FlowEdge{
		Kind: "provider-request", FromID: contextID, ToID: modelID, Status: "exact",
		Basis: "provider-exchange", HiddenByDefault: context.HiddenByDefault,
		Bytes: context.PrimaryBytes, BytesObserved: context.PrimaryBytesObserved,
		Evidence: append([]EvidenceRef(nil), context.Evidence...),
	})
	for _, link := range tools {
		builder.addEdge(FlowEdge{
			Kind: "tool-proposal", FromID: modelID, ToID: link.nodeID, Status: "exact",
			Basis: "provider-content-block-order", HiddenByDefault: link.step.HiddenByDefault,
			Bytes: flowDimensionBytes(link.step.Sizes, "arguments"), BytesObserved: true,
			Evidence: append([]EvidenceRef(nil), link.step.Evidence...),
		})
	}
}

func (builder *sessionFlowBuilder) addContinuityEdges(exchange ProviderExchange, context ContextSnapshot, contextID string) {
	if builder.previousContextID == "" {
		return
	}
	status := builder.flow.Nodes[builder.nodeIndex[contextID]].Status
	if status == "" {
		status = "continued"
	}
	builder.addEdge(FlowEdge{
		Kind: "context-carry", FromID: builder.previousContextID, ToID: contextID,
		Status: status, Basis: "normalized-prefix-equality",
		HiddenByDefault: builder.flow.Nodes[builder.nodeIndex[contextID]].HiddenByDefault,
		Bytes:           context.RequestBytes, BytesObserved: true,
		SourceBytes: builder.previousContext.RequestBytes, SourceBytesObserved: true,
		TargetBytes: context.RequestBytes, TargetBytesObserved: true,
		DeltaBytes: context.RequestBytes - builder.previousContext.RequestBytes, DeltaBytesObserved: true,
		Evidence: []EvidenceRef{builder.previousContext.Evidence, context.Evidence},
	})
	builder.addEdge(FlowEdge{
		Kind: "observed-next-request", FromID: builder.previousModelID, ToID: contextID,
		Status: "sequence", Basis: "provider-exchange-order", HiddenByDefault: true,
		Evidence: mergeFlowEvidence(builder.previousExchange.Evidence, exchange.Evidence),
	})
}

func (builder *sessionFlowBuilder) addPropagationEdges() {
	for _, tool := range builder.toolOrder {
		fromID := builder.toolNodeByExecution[tool.ID]
		if fromID == "" {
			continue
		}
		toID := builder.contextNodeByExchange[tool.ResultExchangeID]
		if toID == "" && flowNeedsResultBoundary(tool) {
			toID = builder.addResultBoundary(fromID, tool)
		}
		if toID == "" {
			continue
		}
		builder.addEdge(flowToolResultEdge(tool, fromID, toID))
	}
}

func flowNeedsResultBoundary(tool ToolExecution) bool {
	return tool.Propagation == propagationMissing || tool.Propagation == propagationAmbiguous || tool.ResultExchangeID != ""
}

func (builder *sessionFlowBuilder) addResultBoundary(fromID string, tool ToolExecution) string {
	from := builder.flow.Nodes[builder.nodeIndex[fromID]]
	nodeID := "flow-result-boundary:" + tool.ID
	title := "Result context not observed"
	status := firstNonEmpty(tool.Propagation, "unknown")
	if tool.ResultExchangeID != "" {
		title = "Result outside selected scope"
		status = "boundary"
	}
	node := FlowNode{
		ID: nodeID, Lane: flowLaneContext, Kind: "result-boundary", Title: title, Status: status,
		TurnID: from.TurnID, ExchangeID: tool.ResultExchangeID,
		InvocationID: from.InvocationID, Phase: from.Phase,
		PrimaryBytes: tool.ProviderResultBytes, PrimaryBytesObserved: tool.ProviderResultBytes > 0,
		Dimensions: []TraceSize{}, StepIDs: []string{}, Evidence: append([]EvidenceRef(nil), tool.Evidence...),
	}
	builder.addNode(node)
	if turnIndex, ok := builder.turnIndexByExchange[tool.ProposalExchangeID]; ok {
		builder.flow.Turns[turnIndex].NodeIDs = append(builder.flow.Turns[turnIndex].NodeIDs, nodeID)
	}
	return nodeID
}

func flowToolResultEdge(tool ToolExecution, fromID, toID string) FlowEdge {
	sourceObserved := tool.ResultBytes > 0 || !tool.CompletedAt.IsZero()
	targetObserved := tool.ResultExchangeID != "" && tool.Propagation != propagationMissing && tool.Propagation != propagationAmbiguous
	edge := FlowEdge{
		Kind: "tool-result", FromID: fromID, ToID: toID,
		Status: firstNonEmpty(tool.Propagation, "unknown"), Basis: tool.CorrelationBasis,
		SourceBytes: tool.ResultBytes, SourceBytesObserved: sourceObserved,
		TargetBytes: tool.ProviderResultBytes, TargetBytesObserved: targetObserved,
		Evidence: append([]EvidenceRef(nil), tool.Evidence...),
	}
	if edge.Basis == "" {
		edge.Basis = "explicit-tool-result-correlation"
	}
	switch {
	case targetObserved:
		edge.Bytes, edge.BytesObserved = tool.ProviderResultBytes, true
	case sourceObserved:
		edge.Bytes, edge.BytesObserved = tool.ResultBytes, true
	}
	if sourceObserved && targetObserved {
		edge.DeltaBytes = tool.ProviderResultBytes - tool.ResultBytes
		edge.DeltaBytesObserved = true
	}
	return edge
}

func (builder *sessionFlowBuilder) appendTurnNode(turnIndex int, node FlowNode) {
	builder.addNode(node)
	builder.flow.Turns[turnIndex].NodeIDs = append(builder.flow.Turns[turnIndex].NodeIDs, node.ID)
}

func (builder *sessionFlowBuilder) addNode(node FlowNode) {
	node.Order = len(builder.flow.Nodes) + 1
	if node.Dimensions == nil {
		node.Dimensions = []TraceSize{}
	}
	if node.StepIDs == nil {
		node.StepIDs = []string{}
	}
	if node.Evidence == nil {
		node.Evidence = []EvidenceRef{}
	}
	builder.nodeIndex[node.ID] = len(builder.flow.Nodes)
	builder.flow.Nodes = append(builder.flow.Nodes, node)
	if node.PrimaryBytesObserved && node.PrimaryBytes > builder.flow.Summary.MaxPayloadBytes {
		builder.flow.Summary.MaxPayloadBytes = node.PrimaryBytes
	}
	if node.Status == statusError {
		builder.flow.Summary.ErrorCount++
	}
	if node.Propagation == propagationDifferent {
		builder.flow.Summary.DifferenceCount++
	}
}

func (builder *sessionFlowBuilder) addEdge(edge FlowEdge) {
	if edge.Evidence == nil {
		edge.Evidence = []EvidenceRef{}
	}
	edge.ID = flowEdgeID(edge.Kind, edge.FromID, edge.ToID)
	builder.flow.Edges = append(builder.flow.Edges, edge)
}

func splitFlowPrompts(steps []TraceStep) ([]TraceStep, []TraceStep) {
	visible := []TraceStep{}
	technical := []TraceStep{}
	for _, step := range steps {
		if step.Kind != traceKindPrompt {
			continue
		}
		if step.HiddenByDefault {
			technical = append(technical, step)
		} else {
			visible = append(visible, step)
		}
	}
	return visible, technical
}

func allFlowStepsHidden(steps []TraceStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if !step.HiddenByDefault {
			return false
		}
	}
	return true
}

func flowTurnHiddenByDefault(steps []TraceStep) bool {
	meaningful := 0
	for _, step := range steps {
		if step.Kind == traceKindPhase {
			continue
		}
		meaningful++
		if !step.HiddenByDefault {
			return false
		}
	}
	return meaningful > 0
}

func flowPromptTitle(steps []TraceStep, technical bool) string {
	if len(steps) == 1 {
		return steps[0].Title
	}
	if technical {
		return fmt.Sprintf("Client-added context · %d blocks", len(steps))
	}
	return fmt.Sprintf("User input · %d blocks", len(steps))
}

func flowModelTitle(turnOrder int, steps []TraceStep) string {
	for _, step := range steps {
		if step.Title == "Final model response" {
			return "Final answer"
		}
	}
	return fmt.Sprintf("Claude turn %d", turnOrder)
}

func flowStopReason(steps []TraceStep) string {
	for _, step := range steps {
		if step.StopReason != "" {
			return step.StopReason
		}
	}
	return ""
}

func flowToolSubtitle(tool ToolExecution) string {
	category := strings.TrimSpace(tool.Category)
	if category == "" {
		return "Tool execution"
	}
	if category == toolCategoryMCP {
		return "MCP tool"
	}
	return strings.ToUpper(category[:1]) + category[1:] + " tool"
}

func flowModelSteps(steps []TraceStep) []TraceStep {
	selected := []TraceStep{}
	for _, step := range steps {
		switch step.Kind {
		case traceKindModelText, traceKindThinking, "provider-block":
			selected = append(selected, step)
		}
	}
	return selected
}

func flowStepsWithKind(steps []TraceStep, kind string) []TraceStep {
	selected := []TraceStep{}
	for _, step := range steps {
		if step.Kind == kind {
			selected = append(selected, step)
		}
	}
	return selected
}

func sumFlowStepBytes(steps []TraceStep) (int, bool) {
	total := 0
	observed := true
	for _, step := range steps {
		if !step.SizeObserved {
			observed = false
			continue
		}
		total += step.SizeBytes
	}
	return total, observed
}

func flowDimensionBytes(dimensions []TraceSize, label string) int {
	for _, dimension := range dimensions {
		if dimension.Label == label && dimension.Observed {
			return dimension.Bytes
		}
	}
	return 0
}

func flowStepIDs(steps []TraceStep) []string {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	return ids
}

func flowStepEvidence(steps []TraceStep) []EvidenceRef {
	evidence := []EvidenceRef{}
	for _, step := range steps {
		evidence = mergeFlowEvidence(evidence, step.Evidence)
	}
	return evidence
}

func mergeFlowEvidence(groups ...[]EvidenceRef) []EvidenceRef {
	merged := []EvidenceRef{}
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, evidence := range group {
			key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", evidence.ID, evidence.File, evidence.SeqStart, evidence.SeqEnd, evidence.DecodedOffset)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, evidence)
		}
	}
	return merged
}

func buildFlowPhases(turns []FlowTurn, phaseEvidence map[string][]EvidenceRef) []FlowPhase {
	phases := []FlowPhase{}
	for start := 0; start < len(turns); {
		end := start
		key := turns[start].InvocationID + "\x00" + turns[start].Phase
		for end+1 < len(turns) && turns[end+1].InvocationID+"\x00"+turns[end+1].Phase == key {
			end++
		}
		title := turns[start].Phase
		if title == "" {
			title = "Session"
		}
		basis := "provider-session-order"
		evidence := []EvidenceRef{}
		if turns[start].InvocationID != "" {
			basis = "explicit-lifecycle"
			evidence = append(evidence, phaseEvidence[turns[start].InvocationID]...)
		}
		phases = append(phases, FlowPhase{
			ID: "flow-phase:" + turns[start].ID, Title: title, InvocationID: turns[start].InvocationID,
			StartTurnID: turns[start].ID, EndTurnID: turns[end].ID,
			StartTurnOrder: turns[start].Order, EndTurnOrder: turns[end].Order,
			Basis: basis, Evidence: evidence,
		})
		start = end + 1
	}
	return phases
}

func flowEdgeID(kind, fromID, toID string) string {
	return "flow-edge:" + kind + ":" + fromID + ":" + toID
}
