package projection

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/capture"
)

const (
	traceKindPhase           = "phase"
	traceKindPrompt          = "prompt"
	traceKindModelText       = "model-text"
	traceKindThinking        = "thinking"
	traceKindTool            = "tool"
	traceKindContext         = "context"
	traceImportancePrimary   = "primary"
	traceImportanceSecondary = "secondary"
	traceImportanceWarning   = "warning"
	traceContentLimit        = 1 << 20
	traceToolPartResult      = "result"
)

type traceScope struct {
	invocationID string
	phase        string
	sessionID    string
}

type traceMessageWire struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type traceBlockWire struct {
	typeName string
	text     string
	thinking string
	content  json.RawMessage
	input    json.RawMessage
}

func BuildSessionTrace(sessionDir string, view *View, filter TraceFilter) (*SessionTrace, error) {
	if view == nil {
		return nil, errors.New("capture view is required")
	}
	scopes, invocationSessions := traceScopes(view)
	if filter.SessionID == "" && filter.InvocationID != "" {
		filter.SessionID = invocationSessions[filter.InvocationID]
	}
	if filter.SessionID == "" && filter.InvocationID == "" && len(view.Sessions) > 0 {
		filter.SessionID = view.Sessions[0].ID
	}
	trace := &SessionTrace{
		FormatVersion: FormatVersion1, CaptureID: view.Capture.ID,
		SessionID: filter.SessionID, InvocationID: filter.InvocationID, Steps: []TraceStep{},
		Summary: TraceSummary{ContentBytesKnown: true},
	}
	contexts := make(map[string]ContextSnapshot, len(view.Contexts))
	for _, context := range view.Contexts {
		contexts[context.ExchangeID] = context
	}
	blocksByExchange := make(map[string][]ProviderBlock)
	for _, block := range view.ProviderBlocks {
		blocksByExchange[block.ExchangeID] = append(blocksByExchange[block.ExchangeID], block)
	}
	for exchangeID := range blocksByExchange {
		sort.Slice(blocksByExchange[exchangeID], func(i, j int) bool {
			return blocksByExchange[exchangeID][i].StartedOffset < blocksByExchange[exchangeID][j].StartedOffset
		})
	}
	stopReasons := make(map[string]string)
	for _, event := range view.ProviderEvents {
		if event.StopReason != "" {
			stopReasons[event.ExchangeID] = event.StopReason
		}
	}
	tools := make(map[string]ToolExecution, len(view.Tools)*2)
	for _, tool := range view.Tools {
		if tool.ToolUseID == "" {
			continue
		}
		tools["\x00"+tool.ToolUseID] = tool
		if tool.ProposalExchangeID != "" {
			tools[tool.ProposalExchangeID+"\x00"+tool.ToolUseID] = tool
		}
	}
	seenPrompts := make(map[[sha256.Size]byte]bool)
	lastPhaseKey := ""
	selectedExchanges := 0
	selectedExchangeIDs := []string{}
	for _, exchange := range view.Exchanges {
		contextSnapshot, isMessageExchange := contexts[exchange.ID]
		if !isMessageExchange || !traceExchangeSelected(exchange, scopes[exchange.ID], filter) {
			continue
		}
		selectedExchanges++
		selectedExchangeIDs = append(selectedExchangeIDs, exchange.ID)
		scope := scopes[exchange.ID]
		phaseKey := scope.invocationID + "\x00" + scope.phase
		if phaseKey != "\x00" && phaseKey != lastPhaseKey {
			appendTraceStep(trace, TraceStep{
				ID: "trace-phase:" + scope.invocationID + ":" + scope.phase, Kind: traceKindPhase, Actor: "phase",
				Title: tracePhaseTitle(scope), Status: "boundary", Importance: traceImportanceSecondary,
				SessionID: scope.sessionID, InvocationID: scope.invocationID, Phase: scope.phase,
				CorrelationBasis: "explicit-lifecycle", ContentRefs: []TraceContentRef{}, Evidence: []EvidenceRef{}, Sizes: []TraceSize{},
			})
			lastPhaseKey = phaseKey
		}
		if contextSnapshot.ContextReset || contextSnapshot.HistoryRewritten {
			title := "Context history rewritten"
			status := "warning"
			importance := traceImportanceWarning
			hidden := false
			if contextSnapshot.ContextReset {
				title = "Context history reset"
				status = statusError
			} else if selectedExchanges == 2 && contextSnapshot.RewrittenMessages <= 1 {
				title = "Client context initialized"
				status = "technical"
				importance = traceImportanceSecondary
				hidden = true
			}
			appendTraceStep(trace, TraceStep{
				ID: "trace-context:" + exchange.ID, Kind: traceKindContext, Actor: "context", Title: title,
				Status: status, Importance: importance, HiddenByDefault: hidden, SessionID: exchange.ClientSessionID,
				InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID,
				SizeBytes: contextSnapshot.AddedMessageBytes, SizeObserved: true,
				Sizes:       []TraceSize{{Label: "added", Bytes: contextSnapshot.AddedMessageBytes, Observed: true}},
				ContentRefs: []TraceContentRef{}, Evidence: []EvidenceRef{contextSnapshot.Evidence}, CorrelationBasis: "normalized-prefix-equality",
			})
		}
		technicalExchange, err := addTracePromptSteps(trace, sessionDir, exchange, contextSnapshot, scope, selectedExchanges == 1, seenPrompts)
		if err != nil {
			return nil, err
		}
		addTraceResponseSteps(trace, exchange, scope, blocksByExchange[exchange.ID], tools, stopReasons[exchange.ID], technicalExchange)
	}
	if selectedExchanges == 0 {
		return nil, fmt.Errorf("no provider exchanges match session %q invocation %q", filter.SessionID, filter.InvocationID)
	}
	markFinalTraceResponse(trace)
	trace.Flow = buildSessionFlow(view, trace, selectedExchangeIDs)
	trace.Summary.StepCount = len(trace.Steps)
	return trace, nil
}

func traceScopes(view *View) (map[string]traceScope, map[string]string) {
	scopes := make(map[string]traceScope)
	invocationSessions := make(map[string]string)
	for _, run := range view.EvalRuns {
		for _, scenario := range run.Scenarios {
			for _, invocation := range scenario.Invocations {
				invocationSessions[invocation.ID] = invocation.ClientSessionID
				for _, exchangeID := range invocation.ExchangeIDs {
					scopes[exchangeID] = traceScope{invocationID: invocation.ID, phase: invocation.Phase, sessionID: invocation.ClientSessionID}
				}
			}
		}
	}
	return scopes, invocationSessions
}

func traceExchangeSelected(exchange ProviderExchange, scope traceScope, filter TraceFilter) bool {
	if filter.InvocationID != "" && scope.invocationID != filter.InvocationID {
		return false
	}
	if filter.SessionID != "" && exchange.ClientSessionID != filter.SessionID {
		return false
	}
	return true
}

func tracePhaseTitle(scope traceScope) string {
	if scope.phase != "" {
		return scope.phase
	}
	if scope.invocationID != "" {
		return scope.invocationID
	}
	return "Session"
}

func addTracePromptSteps(trace *SessionTrace, sessionDir string, exchange ProviderExchange, snapshot ContextSnapshot, scope traceScope, first bool, seenPrompts map[[sha256.Size]byte]bool) (bool, error) {
	detail, err := ReadContextDetail(sessionDir, exchange.ID)
	if err != nil {
		return false, fmt.Errorf("read trace request %s: %w", exchange.ID, err)
	}
	start := snapshot.CommonPrefixMessages
	if first {
		start = 0
	}
	if start < 0 || start > len(detail.Messages) {
		start = 0
	}
	technicalExchange := false
	for messageIndex := start; messageIndex < len(detail.Messages); messageIndex++ {
		var message traceMessageWire
		if err := json.Unmarshal(detail.Messages[messageIndex].JSON, &message); err != nil {
			return false, fmt.Errorf("decode trace message %s[%d]: %w", exchange.ID, messageIndex, err)
		}
		if message.Role != "user" {
			continue
		}
		content := bytes.TrimSpace(message.Content)
		if len(content) == 0 || bytes.Equal(content, []byte("null")) {
			continue
		}
		if content[0] == '"' {
			var text string
			if err := json.Unmarshal(content, &text); err != nil {
				return false, err
			}
			title, hidden, importance, technical := tracePromptPresentation(text, seenPrompts)
			technicalExchange = technicalExchange || technical
			addPromptTraceStep(trace, exchange, scope, messageIndex, -1, blockTypeText, len([]byte(text)), detail.Evidence, title, hidden, importance)
			continue
		}
		if content[0] != '[' {
			addPromptTraceStep(trace, exchange, scope, messageIndex, -2, "content", len(content), detail.Evidence, "User content", false, traceImportancePrimary)
			continue
		}
		var blocks []json.RawMessage
		if err := json.Unmarshal(content, &blocks); err != nil {
			return false, err
		}
		for blockIndex, raw := range blocks {
			block, err := decodeTraceBlock(raw)
			if err != nil {
				return false, err
			}
			if block.typeName == blockTypeToolResult {
				continue
			}
			size := len(raw)
			if block.typeName == blockTypeText {
				size = len([]byte(block.text))
			}
			title, hidden, importance := "User "+strings.ReplaceAll(block.typeName, "_", " "), false, traceImportancePrimary
			if block.typeName == blockTypeText {
				var technical bool
				title, hidden, importance, technical = tracePromptPresentation(block.text, seenPrompts)
				technicalExchange = technicalExchange || technical
			}
			addPromptTraceStep(trace, exchange, scope, messageIndex, blockIndex, block.typeName, size, detail.Evidence, title, hidden, importance)
		}
	}
	return technicalExchange, nil
}

func addPromptTraceStep(trace *SessionTrace, exchange ProviderExchange, scope traceScope, messageIndex, blockIndex int, blockType string, size int, evidence EvidenceRef, title string, hidden bool, importance string) {
	ref := TraceContentRef{
		ID: traceRequestRef(exchange.ID, messageIndex, blockIndex), Kind: "prompt", Label: "Prompt content",
		Bytes: size, BytesObserved: true, FormatHint: traceFormatHint(blockType), RevealRequired: true, Evidence: evidence,
	}
	appendTraceStep(trace, TraceStep{
		ID: fmt.Sprintf("trace-prompt:%s:%d:%d", exchange.ID, messageIndex, blockIndex), Kind: traceKindPrompt,
		Actor: "user", Title: title, Status: "input", Importance: importance, HiddenByDefault: hidden,
		SessionID: exchange.ClientSessionID, InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID,
		StartedAt: exchange.StartedAt, SizeBytes: size, SizeObserved: true,
		Sizes: []TraceSize{{Label: "content", Bytes: size, Observed: true}}, ContentRefs: []TraceContentRef{ref},
		Evidence: []EvidenceRef{evidence}, CorrelationBasis: "provider-request-message",
	})
}

func tracePromptPresentation(text string, seen map[[sha256.Size]byte]bool) (string, bool, string, bool) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "<system-reminder>") {
		return "Client system reminder", true, traceImportanceSecondary, false
	}
	if strings.HasPrefix(trimmed, "<session>") && strings.Contains(trimmed, "Write the title") {
		return "Session title prompt", true, traceImportanceSecondary, true
	}
	normalized := strings.TrimSpace(strings.TrimPrefix(trimmed, "<session>"))
	normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "</session>"))
	hash := sha256.Sum256([]byte(normalized))
	if seen[hash] {
		return "Repeated user prompt", true, traceImportanceSecondary, false
	}
	seen[hash] = true
	return "User prompt", false, traceImportancePrimary, false
}

func addTraceResponseSteps(trace *SessionTrace, exchange ProviderExchange, scope traceScope, blocks []ProviderBlock, tools map[string]ToolExecution, stopReason string, technicalExchange bool) {
	for _, block := range blocks {
		switch block.Type {
		case blockTypeText:
			if block.TextBytes == 0 {
				continue
			}
			ref := traceProviderBlockRef(block)
			title, importance, hidden := "Claude response", traceImportancePrimary, false
			if technicalExchange {
				title, importance, hidden = "Generated session title", traceImportanceSecondary, true
			}
			appendTraceStep(trace, TraceStep{
				ID: "trace-model:" + block.ID, Kind: traceKindModelText, Actor: "claude", Title: title,
				Status: "output", Importance: importance, HiddenByDefault: hidden, SessionID: exchange.ClientSessionID,
				InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID, StopReason: stopReason,
				StartedAt: exchange.ResponseAt, EndedAt: exchange.EndedAt,
				DurationMs: responseStreamDuration(exchange), TimingObserved: exchange.TimingObserved,
				SizeBytes: block.TextBytes, SizeObserved: true, Sizes: []TraceSize{{Label: blockTypeText, Bytes: block.TextBytes, Observed: true}},
				ContentRefs: []TraceContentRef{{ID: ref, Kind: "model-text", Label: "Model text", Bytes: block.TextBytes, BytesObserved: true, FormatHint: "markdown", RevealRequired: true, Evidence: block.Evidence}},
				Evidence:    []EvidenceRef{block.Evidence}, CorrelationBasis: "decoded-sse-block-order",
			})
		case traceKindThinking, blockTypeRedactedThinking:
			ref := traceProviderBlockRef(block)
			appendTraceStep(trace, TraceStep{
				ID: "trace-thinking:" + block.ID, Kind: traceKindThinking, Actor: "claude", Title: "Claude thinking",
				Status: "hidden", Importance: traceImportanceSecondary, HiddenByDefault: true,
				SessionID: exchange.ClientSessionID, InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID,
				StartedAt: exchange.ResponseAt, SizeBytes: block.ThinkingBytes, SizeObserved: true,
				Sizes:       []TraceSize{{Label: "thinking", Bytes: block.ThinkingBytes, Observed: true}},
				ContentRefs: []TraceContentRef{{ID: ref, Kind: traceKindThinking, Label: "Thinking", Bytes: block.ThinkingBytes, BytesObserved: true, FormatHint: blockTypeText, RevealRequired: true, Evidence: block.Evidence}},
				Evidence:    []EvidenceRef{block.Evidence}, CorrelationBasis: "decoded-sse-block-order",
			})
		case blockTypeToolUse, blockTypeServerToolUse:
			tool, ok := tools[exchange.ID+"\x00"+block.ToolUseID]
			if !ok {
				tool, ok = tools["\x00"+block.ToolUseID]
			}
			if ok {
				addTraceToolStep(trace, exchange, scope, tool)
			} else {
				appendTraceStep(trace, unmatchedTraceToolStep(exchange, scope, block))
			}
		default:
			appendTraceStep(trace, TraceStep{
				ID: "trace-provider-block:" + block.ID, Kind: "provider-block", Actor: "provider",
				Title: "Provider " + strings.ReplaceAll(block.Type, "_", " "), Status: block.Status,
				Importance: traceImportanceSecondary, HiddenByDefault: true, SessionID: exchange.ClientSessionID,
				InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID,
				SizeBytes: block.TextBytes + block.ThinkingBytes + block.InputJSONBytes, SizeObserved: true,
				Sizes: []TraceSize{}, ContentRefs: []TraceContentRef{}, Evidence: []EvidenceRef{block.Evidence},
				CorrelationBasis: "decoded-sse-block-order",
			})
		}
	}
}

func addTraceToolStep(trace *SessionTrace, exchange ProviderExchange, scope traceScope, tool ToolExecution) {
	status := "ok"
	importance := traceImportancePrimary
	if tool.IsError {
		status = statusError
		importance = traceImportanceWarning
	}
	if tool.Propagation == propagationDifferent || tool.Propagation == propagationMissing || tool.Propagation == propagationAmbiguous {
		importance = traceImportanceWarning
	}
	refs := []TraceContentRef{
		{ID: traceToolRef(tool.ID, "arguments"), Kind: "tool-arguments", Label: "Arguments", Bytes: tool.ArgumentsBytes, BytesObserved: true, FormatHint: "json", RevealRequired: true, Evidence: firstEvidence(tool.Evidence)},
	}
	sizes := []TraceSize{{Label: "arguments", Bytes: tool.ArgumentsBytes, Observed: true}}
	if tool.ResultBytes > 0 || !tool.CompletedAt.IsZero() {
		refs = append(refs, TraceContentRef{ID: traceToolRef(tool.ID, traceToolPartResult), Kind: "tool-result", Label: "Exact tool result", Bytes: tool.ResultBytes, BytesObserved: true, FormatHint: "auto", RevealRequired: true, Evidence: evidenceAt(tool.Evidence, 2)})
		sizes = append(sizes, TraceSize{Label: "result", Bytes: tool.ResultBytes, Observed: true})
	}
	if tool.ProviderResultBytes > 0 {
		refs = append(refs, TraceContentRef{ID: traceToolRef(tool.ID, "provider-result"), Kind: "provider-result", Label: "Result in model context", Bytes: tool.ProviderResultBytes, BytesObserved: true, FormatHint: "auto", RevealRequired: true, Evidence: lastEvidence(tool.Evidence)})
		sizes = append(sizes, TraceSize{Label: "in context", Bytes: tool.ProviderResultBytes, Observed: true})
	}
	appendTraceStep(trace, TraceStep{
		ID: "trace-tool:" + tool.ID, Kind: traceKindTool, Actor: "tool", Title: tool.ToolName,
		Status: status, Importance: importance, GroupID: "tool-group:" + tool.ID,
		SessionID: exchange.ClientSessionID, InvocationID: firstNonEmpty(tool.InvocationID, scope.invocationID), Phase: scope.phase,
		ExchangeID: exchange.ID, ToolExecutionID: tool.ID, Propagation: tool.Propagation,
		StartedAt: tool.StartedAt, EndedAt: tool.CompletedAt, DurationMs: tool.DurationMs, TimingObserved: tool.TimingObserved,
		SizeBytes: tool.ArgumentsBytes + tool.ResultBytes, SizeObserved: true, Sizes: sizes, ContentRefs: refs,
		Evidence: append([]EvidenceRef(nil), tool.Evidence...), CorrelationBasis: tool.CorrelationBasis,
	})
}

func unmatchedTraceToolStep(exchange ProviderExchange, scope traceScope, block ProviderBlock) TraceStep {
	return TraceStep{
		ID: "trace-tool-unmatched:" + block.ID, Kind: traceKindTool, Actor: "tool", Title: block.ToolName,
		Status: "unmatched", Importance: traceImportanceWarning, SessionID: exchange.ClientSessionID,
		InvocationID: scope.invocationID, Phase: scope.phase, ExchangeID: exchange.ID,
		SizeBytes: block.InputJSONBytes, SizeObserved: true,
		Sizes:       []TraceSize{{Label: "arguments", Bytes: block.InputJSONBytes, Observed: true}},
		ContentRefs: []TraceContentRef{{ID: traceProviderBlockRef(block), Kind: "tool-arguments", Label: "Arguments", Bytes: block.InputJSONBytes, BytesObserved: true, FormatHint: "json", RevealRequired: true, Evidence: block.Evidence}},
		Evidence:    []EvidenceRef{block.Evidence}, CorrelationBasis: "provider-tool-use-only",
	}
}

func appendTraceStep(trace *SessionTrace, step TraceStep) {
	step.Order = len(trace.Steps) + 1
	if step.ContentRefs == nil {
		step.ContentRefs = []TraceContentRef{}
	}
	if step.Evidence == nil {
		step.Evidence = []EvidenceRef{}
	}
	if step.Sizes == nil {
		step.Sizes = []TraceSize{}
	}
	trace.Steps = append(trace.Steps, step)
	if step.SizeObserved {
		trace.Summary.ContentBytes += step.SizeBytes
	} else if step.Kind != traceKindPhase {
		trace.Summary.ContentBytesKnown = false
	}
	switch step.Kind {
	case traceKindPrompt:
		trace.Summary.PromptCount++
	case traceKindModelText, traceKindThinking:
		trace.Summary.ModelBlockCount++
	case traceKindTool:
		trace.Summary.ToolCount++
	}
	if step.Status == statusError {
		trace.Summary.ErrorCount++
	}
	if step.Propagation == propagationDifferent {
		trace.Summary.DifferenceCount++
	}
}

func markFinalTraceResponse(trace *SessionTrace) {
	for index := len(trace.Steps) - 1; index >= 0; index-- {
		if trace.Steps[index].Kind == traceKindModelText {
			trace.Steps[index].Title = "Final model response"
			return
		}
		if trace.Steps[index].Kind == traceKindTool {
			return
		}
	}
}

func responseStreamDuration(exchange ProviderExchange) int64 {
	if exchange.ResponseAt.IsZero() || exchange.EndedAt.IsZero() {
		return 0
	}
	return exchange.EndedAt.Sub(exchange.ResponseAt).Milliseconds()
}

func traceRequestRef(exchangeID string, messageIndex, blockIndex int) string {
	return fmt.Sprintf("request:%s:%d:%d", encodeTracePart(exchangeID), messageIndex, blockIndex)
}

func traceProviderBlockRef(block ProviderBlock) string {
	return fmt.Sprintf("response:%s:%d:%d", encodeTracePart(block.ExchangeID), block.Index, block.StartedOffset)
}

func traceToolRef(toolID, part string) string {
	return "tool:" + encodeTracePart(toolID) + ":" + part
}

func encodeTracePart(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeTracePart(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func ReadTraceContent(sessionDir string, view *View, ref string) (*TraceContentDetail, error) {
	parts := strings.Split(ref, ":")
	if len(parts) == 0 {
		return nil, errors.New("trace content ref is empty")
	}
	switch parts[0] {
	case "request":
		return readTraceRequestContent(sessionDir, ref, parts)
	case "response":
		return readTraceResponseContent(sessionDir, ref, parts)
	case "tool":
		return readTraceToolContent(sessionDir, view, ref, parts)
	default:
		return nil, fmt.Errorf("unsupported trace content ref %q", ref)
	}
}

func readTraceRequestContent(sessionDir, ref string, parts []string) (*TraceContentDetail, error) {
	if len(parts) != 4 {
		return nil, errors.New("invalid request trace content ref")
	}
	exchangeID, err := decodeTracePart(parts[1])
	if err != nil {
		return nil, err
	}
	messageIndex, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, err
	}
	blockIndex, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, err
	}
	detail, err := ReadContextDetail(sessionDir, exchangeID)
	if err != nil {
		return nil, err
	}
	if messageIndex < 0 || messageIndex >= len(detail.Messages) {
		return nil, fmt.Errorf("trace message index %d not found", messageIndex)
	}
	var message traceMessageWire
	if err := json.Unmarshal(detail.Messages[messageIndex].JSON, &message); err != nil {
		return nil, err
	}
	content := bytes.TrimSpace(message.Content)
	kind := "prompt"
	var value string
	switch blockIndex {
	case -1:
		if err := json.Unmarshal(content, &value); err != nil {
			return nil, err
		}
	case -2:
		value = compactRawJSON(content)
	default:
		var blocks []json.RawMessage
		if err := json.Unmarshal(content, &blocks); err != nil {
			return nil, err
		}
		if blockIndex < 0 || blockIndex >= len(blocks) {
			return nil, fmt.Errorf("trace block index %d not found", blockIndex)
		}
		block, err := decodeTraceBlock(blocks[blockIndex])
		if err != nil {
			return nil, err
		}
		kind = block.typeName
		switch {
		case block.text != "":
			value = block.text
		case block.thinking != "":
			value = block.thinking
		case len(block.content) > 0:
			value = rawContentText(block.content)
		case len(block.input) > 0:
			value = compactRawJSON(block.input)
		default:
			value = compactRawJSON(blocks[blockIndex])
		}
	}
	return newTraceContentDetail(ref, kind, value, detail.Evidence), nil
}

func readTraceResponseContent(sessionDir, ref string, parts []string) (*TraceContentDetail, error) {
	if len(parts) != 4 {
		return nil, errors.New("invalid response trace content ref")
	}
	exchangeID, err := decodeTracePart(parts[1])
	if err != nil {
		return nil, err
	}
	blockIndex, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, err
	}
	startedOffset, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, err
	}
	kind, content, evidence, err := readProviderBlockContent(sessionDir, exchangeID, blockIndex, startedOffset)
	if err != nil {
		return nil, err
	}
	return newTraceContentDetail(ref, kind, content, evidence), nil
}

func readTraceToolContent(sessionDir string, view *View, ref string, parts []string) (*TraceContentDetail, error) {
	if len(parts) != 3 || view == nil {
		return nil, errors.New("invalid tool trace content ref")
	}
	toolID, err := decodeTracePart(parts[1])
	if err != nil {
		return nil, err
	}
	for _, execution := range view.Tools {
		if execution.ID != toolID {
			continue
		}
		detail, err := ReadToolExecutionDetail(sessionDir, execution)
		if err != nil {
			return nil, err
		}
		var value, kind string
		evidence := firstEvidence(detail.Evidence)
		switch parts[2] {
		case "arguments":
			value, kind = detail.ArgumentsJSON, "tool-arguments"
		case traceToolPartResult:
			value, kind = detail.ResultText, "tool-result"
			evidence = evidenceAt(detail.Evidence, 2)
		case "provider-result":
			value, kind = detail.ProviderResultText, "provider-result"
			evidence = lastEvidence(detail.Evidence)
		default:
			return nil, fmt.Errorf("unknown tool trace content part %q", parts[2])
		}
		return newTraceContentDetail(ref, kind, value, evidence), nil
	}
	return nil, fmt.Errorf("trace tool %q not found", toolID)
}

func newTraceContentDetail(ref, kind, value string, evidence EvidenceRef) *TraceContentDetail {
	bytesTotal := len([]byte(value))
	preview, truncated := boundedTraceContent(value)
	return &TraceContentDetail{
		Ref: ref, Kind: kind, Content: preview, Bytes: bytesTotal,
		FormatCandidates: traceFormatCandidates(value), Truncated: truncated, Evidence: evidence,
	}
}

func boundedTraceContent(value string) (string, bool) {
	if len(value) <= traceContentLimit {
		return value, false
	}
	preview := value[:traceContentLimit]
	for len(preview) > 0 && !utf8.ValidString(preview) {
		preview = preview[:len(preview)-1]
	}
	return preview, true
}

func traceFormatCandidates(value string) []string {
	trimmed := strings.TrimSpace(value)
	candidates := []string{blockTypeText}
	if json.Valid([]byte(trimmed)) {
		return []string{"json", blockTypeText}
	}
	var nested string
	if json.Unmarshal([]byte(trimmed), &nested) == nil && json.Valid([]byte(strings.TrimSpace(nested))) {
		return []string{"nested-json", "json", blockTypeText}
	}
	if strings.Contains(value, "```") || strings.Contains(value, "\n#") || strings.HasPrefix(trimmed, "#") {
		candidates = append([]string{"markdown"}, candidates...)
	}
	if strings.Contains(value, "\n") && strings.Contains(value, ": ") {
		candidates = append(candidates, "yaml")
	}
	return candidates
}

func traceFormatHint(blockType string) string {
	switch blockType {
	case blockTypeText:
		return "markdown"
	case blockTypeToolUse, blockTypeToolResult:
		return "json"
	default:
		return "auto"
	}
}

func decodeTraceBlock(raw json.RawMessage) (traceBlockWire, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return traceBlockWire{}, err
	}
	var block traceBlockWire
	stringsByKey := map[string]*string{"type": &block.typeName, blockTypeText: &block.text, "thinking": &block.thinking}
	if err := decodeStringFields(fields, stringsByKey); err != nil {
		return traceBlockWire{}, err
	}
	block.content = append(block.content, fields["content"]...)
	block.input = append(block.input, fields["input"]...)
	return block, nil
}

func readProviderBlockContent(sessionDir, exchangeID string, wantedIndex int, wantedOffset int64) (string, string, EvidenceRef, error) {
	decoded, source, err := readDecodedProviderResponse(sessionDir, exchangeID)
	if err != nil {
		return "", "", EvidenceRef{}, err
	}
	var offset int64
	active := false
	blockType := ""
	var content strings.Builder
	partialInput := false
	for rawLine := range bytes.SplitSeq(decoded, []byte{'\n'}) {
		lineOffset := offset
		offset += int64(len(rawLine) + 1)
		line := bytes.TrimSuffix(rawLine, []byte{'\r'})
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var wire providerSSEWire
		if err := json.Unmarshal(payload, &wire); err != nil {
			return "", "", EvidenceRef{}, err
		}
		if wire.Type == "content_block_start" && wire.Index == wantedIndex && lineOffset == wantedOffset {
			active = true
			blockType = wire.ContentBlock.Type
			switch blockType {
			case blockTypeText:
				content.WriteString(wire.ContentBlock.Text)
			case traceKindThinking:
				content.WriteString(wire.ContentBlock.Thinking)
			case blockTypeRedactedThinking:
				content.WriteString(wire.ContentBlock.Data)
			case blockTypeToolUse, blockTypeServerToolUse:
				if len(bytes.TrimSpace(wire.ContentBlock.Input)) > 0 {
					content.WriteString(compactRawJSON(wire.ContentBlock.Input))
				}
			default:
				content.WriteString(compactRawJSON(wire.ContentBlockRaw))
			}
			continue
		}
		if !active || wire.Index != wantedIndex {
			continue
		}
		switch wire.Type {
		case "content_block_delta":
			switch wire.Delta.Type {
			case "text_delta":
				content.WriteString(wire.Delta.Text)
			case "thinking_delta":
				content.WriteString(wire.Delta.Thinking)
			case "input_json_delta":
				if !partialInput {
					content.Reset()
					partialInput = true
				}
				content.WriteString(wire.Delta.PartialJSON)
			}
		case "content_block_stop":
			source.DecodedOffset = wantedOffset
			source.ByteLength = offset - wantedOffset
			return blockType, content.String(), source, nil
		}
	}
	if active {
		source.DecodedOffset = wantedOffset
		return blockType, content.String(), source, nil
	}
	return "", "", EvidenceRef{}, fmt.Errorf("provider block %s[%d] at %d not found", exchangeID, wantedIndex, wantedOffset)
}

func readDecodedProviderResponse(sessionDir, exchangeID string) ([]byte, EvidenceRef, error) {
	kind, path, err := inventoriedFile(sessionDir, "provider.jsonl")
	if err != nil {
		return nil, EvidenceRef{}, err
	}
	if kind != capture.ManifestFileProvider {
		return nil, EvidenceRef{}, errors.New("provider.jsonl is not provider evidence")
	}
	records, err := capture.ReadRecords(path)
	if err != nil {
		return nil, EvidenceRef{}, err
	}
	var headers http.Header
	var body []byte
	evidence := EvidenceRef{File: "provider.jsonl", ExchangeID: exchangeID}
	for _, record := range records {
		if record.ExchangeID != exchangeID {
			continue
		}
		switch record.Kind {
		case capture.RecordProviderResponseStart:
			headers = record.Headers
		case capture.RecordProviderResponseBody:
			chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
			if err != nil {
				return nil, EvidenceRef{}, err
			}
			if evidence.SeqStart == 0 {
				evidence.SeqStart = record.Seq
			}
			evidence.SeqEnd = record.Seq
			evidence.ObservedAt = record.Time
			evidence.ByteLength += int64(len(chunk))
			body = append(body, chunk...)
		}
	}
	if len(body) == 0 {
		return nil, EvidenceRef{}, fmt.Errorf("provider response for %s has no body", exchangeID)
	}
	decoded, err := capture.DecodeProviderResponseBody(body, headers)
	if err != nil {
		return nil, EvidenceRef{}, err
	}
	return decoded, evidence, nil
}

func firstEvidence(values []EvidenceRef) EvidenceRef {
	if len(values) == 0 {
		return EvidenceRef{}
	}
	return values[0]
}

func evidenceAt(values []EvidenceRef, index int) EvidenceRef {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return firstEvidence(values)
}

func lastEvidence(values []EvidenceRef) EvidenceRef {
	if len(values) == 0 {
		return EvidenceRef{}
	}
	return values[len(values)-1]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
