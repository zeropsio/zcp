package projection

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

type providerSSEWire struct {
	Type            string
	Index           int
	ContentBlock    providerSSEContentBlock
	ContentBlockRaw json.RawMessage
	Delta           providerSSEDelta
	DeltaRaw        json.RawMessage
}

type providerSSEContentBlock struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Data     string          `json:"data"`
}

type providerSSEDelta struct {
	Type        string
	PartialJSON string
	Text        string
	Thinking    string
	Data        string
	StopReason  string
}

func (wire *providerSSEWire) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw := fields["type"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &wire.Type); err != nil {
			return err
		}
	}
	if raw := fields["index"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &wire.Index); err != nil {
			return err
		}
	}
	if raw := fields["content_block"]; len(raw) > 0 {
		wire.ContentBlockRaw = append(wire.ContentBlockRaw[:0], raw...)
		if err := json.Unmarshal(raw, &wire.ContentBlock); err != nil {
			return err
		}
	}
	if raw := fields["delta"]; len(raw) > 0 {
		wire.DeltaRaw = append(wire.DeltaRaw[:0], raw...)
		var deltaFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &deltaFields); err != nil {
			return err
		}
		if err := decodeStringFields(deltaFields, map[string]*string{
			"type": &wire.Delta.Type, "partial_json": &wire.Delta.PartialJSON,
			"text": &wire.Delta.Text, "thinking": &wire.Delta.Thinking,
			"data": &wire.Delta.Data, "stop_reason": &wire.Delta.StopReason,
		}); err != nil {
			return err
		}
	}
	return nil
}

const (
	maxProviderEventPage    = 2_000
	maxProviderEventPayload = 1 << 20
)

type providerBlockAccumulator struct {
	value        ProviderBlock
	initialInput int
	partialInput int
}

func ReadProviderEventDetail(sessionDir, exchangeID string, ordinal int) (*ProviderEventDetail, error) {
	if exchangeID == "" || ordinal < 1 {
		return nil, fmt.Errorf("provider event exchange and positive ordinal are required")
	}
	kind, path, err := inventoriedFile(sessionDir, "provider.jsonl")
	if err != nil {
		return nil, err
	}
	if kind != capture.ManifestFileProvider {
		return nil, fmt.Errorf("provider.jsonl is not a provider file")
	}
	records, err := capture.ReadRecords(path)
	if err != nil {
		return nil, err
	}
	var headers map[string][]string
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
				return nil, err
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
		return nil, fmt.Errorf("provider response body for %s not found", exchangeID)
	}
	decoded, err := capture.DecodeProviderResponseBody(body, headers)
	if err != nil {
		return nil, err
	}
	current := 0
	var offset int64
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
		current++
		if current != ordinal {
			continue
		}
		if len(payload) > maxProviderEventPayload {
			return nil, fmt.Errorf("provider event payload exceeds %d-byte detail limit", maxProviderEventPayload)
		}
		evidence.ID = fmt.Sprintf("%s:event:%06d", rawRangeID(evidence.File, evidence.SeqStart, evidence.SeqEnd), ordinal)
		evidence.DecodedOffset = lineOffset
		evidence.ByteLength = int64(len(payload))
		return &ProviderEventDetail{ExchangeID: exchangeID, Ordinal: ordinal, DecodedOffset: lineOffset, Payload: string(payload), Evidence: evidence}, nil
	}
	return nil, fmt.Errorf("provider event %s#%d not found", exchangeID, ordinal)
}

func PageProviderEvents(view *View, offset, limit int) (*ProviderEventPage, error) {
	if offset < 0 || offset > len(view.ProviderEvents) {
		return nil, fmt.Errorf("provider event offset %d is outside 0-%d", offset, len(view.ProviderEvents))
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > maxProviderEventPage {
		limit = maxProviderEventPage
	}
	end := min(offset+limit, len(view.ProviderEvents))
	return &ProviderEventPage{
		FormatVersion: FormatVersion1, CaptureID: view.Capture.ID, Offset: offset, Limit: limit,
		Total: len(view.ProviderEvents), Items: append([]ProviderEvent(nil), view.ProviderEvents[offset:end]...),
	}, nil
}

func projectProviderResponses(view *View, file string, records []capture.Record) {
	type responseAccumulator struct {
		headers  map[string][]string
		body     []byte
		evidence EvidenceRef
	}
	responses := make(map[string]*responseAccumulator)
	var order []string
	for _, record := range records {
		if record.ExchangeID == "" {
			continue
		}
		response := responses[record.ExchangeID]
		if response == nil {
			response = &responseAccumulator{evidence: EvidenceRef{File: file, ExchangeID: record.ExchangeID}}
			responses[record.ExchangeID] = response
			order = append(order, record.ExchangeID)
		}
		switch record.Kind {
		case capture.RecordProviderResponseStart:
			response.headers = record.Headers
		case capture.RecordProviderResponseBody:
			chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
			if err != nil {
				view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "provider.sse.decode", Severity: "warning", Summary: fmt.Sprintf("%s seq %d: %v", file, record.Seq, err), Basis: "raw"})
				continue
			}
			if response.evidence.SeqStart == 0 {
				response.evidence.SeqStart = record.Seq
			}
			response.evidence.SeqEnd = record.Seq
			response.evidence.ObservedAt = record.Time
			response.evidence.ByteLength += int64(len(chunk))
			response.body = append(response.body, chunk...)
		}
	}
	for _, exchangeID := range order {
		response := responses[exchangeID]
		if len(response.body) == 0 || !headerContains(response.headers, "Content-Type", "text/event-stream") {
			continue
		}
		decoded, err := capture.DecodeProviderResponseBody(response.body, response.headers)
		if err != nil {
			view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "provider.sse.decode", Severity: "warning", Summary: fmt.Sprintf("%s %s: %v", file, exchangeID, err), Basis: "raw", Evidence: []EvidenceRef{response.evidence}})
			continue
		}
		projectProviderSSE(view, exchangeID, decoded, response.evidence)
	}
}

func projectProviderSSE(view *View, exchangeID string, decoded []byte, source EvidenceRef) {
	states := make(map[int]*providerBlockAccumulator)
	var stateOrder []int
	ordinal := 0
	var offset int64
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
			view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "provider.sse.event.parse", Severity: "warning", Summary: fmt.Sprintf("%s at decoded offset %d: %v", exchangeID, lineOffset, err), Basis: "raw", Evidence: []EvidenceRef{source}})
			continue
		}
		ordinal++
		evidence := source
		evidence.ID = fmt.Sprintf("%s:event:%06d", rawRangeID(source.File, source.SeqStart, source.SeqEnd), ordinal)
		evidence.DecodedOffset = lineOffset
		evidence.ByteLength = int64(len(payload))
		view.ProviderEvents = append(view.ProviderEvents, ProviderEvent{
			ID: fmt.Sprintf("provider-event:%s:%06d", exchangeID, ordinal), ExchangeID: exchangeID, Ordinal: ordinal,
			Type: wire.Type, Index: wire.Index, BlockType: wire.ContentBlock.Type, DeltaType: wire.Delta.Type,
			StopReason: wire.Delta.StopReason, DecodedOffset: lineOffset, PayloadBytes: len(payload),
			TimestampBasis: "response-entity-end", Evidence: evidence,
		})
		view.ProviderEventTotal++
		switch wire.Type {
		case "content_block_start":
			state := &providerBlockAccumulator{
				value: ProviderBlock{
					ID: fmt.Sprintf("provider-block:%s:%d:%d", exchangeID, wire.Index, lineOffset), ExchangeID: exchangeID,
					Index: wire.Index, Type: wire.ContentBlock.Type, ToolUseID: wire.ContentBlock.ID, ToolName: wire.ContentBlock.Name,
					TextBytes: len(wire.ContentBlock.Text), ThinkingBytes: len(wire.ContentBlock.Thinking) + len(wire.ContentBlock.Data),
					StartedOffset: lineOffset, Status: "incomplete", Evidence: evidence,
				},
				initialInput: len(bytes.TrimSpace(wire.ContentBlock.Input)),
			}
			states[wire.Index] = state
			stateOrder = append(stateOrder, wire.Index)
		case "content_block_delta":
			state := states[wire.Index]
			if state == nil {
				continue
			}
			state.value.TextBytes += len(wire.Delta.Text)
			state.value.ThinkingBytes += len(wire.Delta.Thinking) + len(wire.Delta.Data)
			if wire.Delta.Type == "input_json_delta" {
				state.partialInput += len(wire.Delta.PartialJSON)
			}
		case "content_block_stop":
			state := states[wire.Index]
			if state == nil {
				continue
			}
			finalizeProviderBlock(view, state, lineOffset, evidence, "complete")
			delete(states, wire.Index)
		}
	}
	if len(states) > 0 {
		sort.Ints(stateOrder)
		for _, index := range stateOrder {
			state := states[index]
			if state == nil {
				continue
			}
			finalizeProviderBlock(view, state, offset, state.value.Evidence, "incomplete")
			delete(states, index)
		}
	}
}

func finalizeProviderBlock(view *View, state *providerBlockAccumulator, completedOffset int64, evidence EvidenceRef, status string) {
	state.value.CompletedOffset = completedOffset
	state.value.Status = status
	if state.partialInput > 0 {
		state.value.InputJSONBytes = state.partialInput
	} else {
		state.value.InputJSONBytes = state.initialInput
	}
	state.value.Evidence.SeqEnd = evidence.SeqEnd
	state.value.Evidence.DecodedOffset = state.value.StartedOffset
	state.value.Evidence.ByteLength = completedOffset - state.value.StartedOffset
	view.ProviderBlocks = append(view.ProviderBlocks, state.value)
}

func headerContains(headers map[string][]string, name, value string) bool {
	for headerName, values := range headers {
		if !strings.EqualFold(headerName, name) {
			continue
		}
		for _, candidate := range values {
			if strings.Contains(strings.ToLower(candidate), strings.ToLower(value)) {
				return true
			}
		}
	}
	return false
}
