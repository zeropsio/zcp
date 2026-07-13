package capture

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type providerInspectionData struct {
	sessionID             string
	status                string
	recordCount           int
	exchangeCount         int
	unattributedExchanges int
	claudeSessions        []ClaudeSessionInspection
	exchanges             []inspectedProviderExchange
	contexts              []ModelContextInspection
	toolUses              []inspectedProviderToolUse
	toolResults           map[string]inspectedProviderToolResult
	warnings              []string
}

type inspectedProviderExchange struct {
	id              string
	claudeSessionID string
	model           string
	startedAt       time.Time
	source          RawEvidence
}

type inspectedProviderToolUse struct {
	toolUseID       string
	name            string
	argumentsJSON   string
	claudeSessionID string
	invocationID    string
	source          RawEvidence
}

type inspectedProviderToolResult struct {
	text    string
	isError bool
	source  RawEvidence
}

type providerExchangeRecords struct {
	id      string
	records []Record
}

type providerRequestWire struct {
	Model    string                `json:"model"`
	Messages []providerMessageWire `json:"messages"`
	Metadata struct {
		UserID string `json:"user_id"` //nolint:tagliatelle // Anthropic Messages wire schema
	} `json:"metadata"`
}

type claudeMetadataUserID struct {
	SessionID string `json:"session_id"` //nolint:tagliatelle // JSON encoded by Claude inside metadata.user_id
}

type providerMessageWire struct {
	Content json.RawMessage `json:"content"`
}

type providerMessageBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

type providerSSEEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

type providerToolBlockState struct {
	index         int
	toolUseID     string
	name          string
	initialInput  json.RawMessage
	partialInput  strings.Builder
	decodedOffset int64
}

func inspectProviderFile(path, relative string) (*providerInspectionData, error) {
	records, err := ReadRecords(path)
	if err != nil {
		return nil, err
	}
	if err := validateInspectionRecordSequence(records, RecordSessionEnd); err != nil {
		return nil, err
	}
	data := &providerInspectionData{
		recordCount: len(records),
		toolResults: make(map[string]inspectedProviderToolResult),
	}
	if len(records) > 0 {
		data.sessionID = records[0].SessionID
		data.status = records[len(records)-1].CaptureStatus
	}

	exchangesByID := make(map[string]*providerExchangeRecords)
	var exchanges []*providerExchangeRecords
	for _, record := range records {
		if record.ExchangeID == "" {
			continue
		}
		exchange := exchangesByID[record.ExchangeID]
		if exchange == nil {
			exchange = &providerExchangeRecords{id: record.ExchangeID}
			exchangesByID[record.ExchangeID] = exchange
			exchanges = append(exchanges, exchange)
		}
		exchange.records = append(exchange.records, record)
	}
	data.exchangeCount = len(exchanges)
	sessionsByID := make(map[string]*ClaudeSessionInspection)
	contextStates := make(map[string]modelContextState)
	for _, exchange := range exchanges {
		requestBody, requestSource, err := reconstructInspectionBody(exchange.records, RecordProviderRequestBody, relative, exchange.id)
		if err != nil {
			return nil, fmt.Errorf("exchange %s request: %w", exchange.id, err)
		}
		if err := validateInspectionBodyEnd(exchange.records, RecordProviderRequestEnd, requestBody); err != nil {
			return nil, fmt.Errorf("exchange %s request: %w", exchange.id, err)
		}
		if len(requestBody) > 0 {
			if err := collectProviderToolResults(requestBody, requestSource, data.toolResults); err != nil {
				return nil, fmt.Errorf("exchange %s request JSON: %w", exchange.id, err)
			}
		}
		requestStart := firstInspectionRecord(exchange.records, RecordProviderRequestStart)
		var sessionID, model, identityWarning string
		if requestStart != nil && requestStart.Path == "/v1/messages" {
			sessionID, model, identityWarning = inspectClaudeRequestIdentity(requestBody)
			if identityWarning != "" {
				data.warnings = append(data.warnings, fmt.Sprintf("exchange %s Claude session identity: %s", exchange.id, identityWarning))
			}
		}
		observedAt := requestSource.ObservedAt
		if requestStart != nil {
			observedAt = requestStart.Time
		}
		data.exchanges = append(data.exchanges, inspectedProviderExchange{
			id:              exchange.id,
			claudeSessionID: sessionID,
			model:           model,
			startedAt:       observedAt,
			source:          requestSource,
		})
		contextIndex := -1
		if requestStart != nil && requestStart.Path == "/v1/messages" && len(requestBody) > 0 {
			contextView, nextState, contextErr := deriveModelContext(exchange.id, sessionID, requestBody, requestSource, contextStates[sessionID])
			if contextErr != nil {
				return nil, fmt.Errorf("exchange %s context: %w", exchange.id, contextErr)
			}
			data.contexts = append(data.contexts, contextView)
			contextIndex = len(data.contexts) - 1
			if sessionID != "" {
				contextStates[sessionID] = nextState
			}
		}
		if sessionID == "" {
			data.unattributedExchanges++
		} else {
			session := sessionsByID[sessionID]
			if session == nil {
				session = &ClaudeSessionInspection{SessionID: sessionID, FirstObservedAt: observedAt, FirstSource: requestSource}
				sessionsByID[sessionID] = session
				data.claudeSessions = append(data.claudeSessions, *session)
			}
			// The slice stores values for stable public ownership; update its
			// matching entry after mutating the indexed accumulator below.
			session.ProviderExchanges++
			session.LastObservedAt = observedAt
			if model != "" && !slicesContains(session.Models, model) {
				session.Models = append(session.Models, model)
			}
		}

		responseStarted := firstInspectionRecord(exchange.records, RecordProviderResponseStart)
		if responseStarted == nil {
			continue
		}
		responseBody, responseSource, err := reconstructInspectionBody(exchange.records, RecordProviderResponseBody, relative, exchange.id)
		if err != nil {
			return nil, fmt.Errorf("exchange %s response: %w", exchange.id, err)
		}
		if err := validateInspectionBodyEnd(exchange.records, RecordProviderResponseEnd, responseBody); err != nil {
			return nil, fmt.Errorf("exchange %s response: %w", exchange.id, err)
		}
		if len(responseBody) == 0 {
			continue
		}
		decoded, err := decodeProviderResponse(responseBody, responseStarted.Headers)
		if err != nil {
			return nil, fmt.Errorf("exchange %s decode response: %w", exchange.id, err)
		}
		toolUses, err := parseProviderSSEToolUses(decoded, responseSource, sessionID)
		if err != nil {
			return nil, fmt.Errorf("exchange %s parse SSE: %w", exchange.id, err)
		}
		data.toolUses = append(data.toolUses, toolUses...)
		if contextIndex >= 0 {
			metadata, metadataErr := inspectProviderResponseMetadata(decoded)
			if metadataErr != nil {
				return nil, fmt.Errorf("exchange %s response metadata: %w", exchange.id, metadataErr)
			}
			applyProviderResponseMetadata(&data.contexts[contextIndex], metadata)
		}
	}
	data.claudeSessions = data.claudeSessions[:0]
	for _, session := range sessionsByID {
		sort.Strings(session.Models)
		data.claudeSessions = append(data.claudeSessions, *session)
	}
	sort.Slice(data.claudeSessions, func(i, j int) bool {
		if data.claudeSessions[i].FirstObservedAt.Equal(data.claudeSessions[j].FirstObservedAt) {
			return data.claudeSessions[i].SessionID < data.claudeSessions[j].SessionID
		}
		return data.claudeSessions[i].FirstObservedAt.Before(data.claudeSessions[j].FirstObservedAt)
	})
	return data, nil
}

func inspectClaudeRequestIdentity(body []byte) (sessionID, model, warning string) {
	if len(body) == 0 {
		return "", "", "request body is empty"
	}
	var request providerRequestWire
	if err := json.Unmarshal(body, &request); err != nil {
		return "", "", "request is not JSON: " + err.Error()
	}
	if request.Metadata.UserID == "" {
		return "", request.Model, "metadata.user_id is absent"
	}
	var identity claudeMetadataUserID
	if err := json.Unmarshal([]byte(request.Metadata.UserID), &identity); err != nil {
		return "", request.Model, "metadata.user_id is not Claude JSON: " + err.Error()
	}
	if identity.SessionID == "" {
		return "", request.Model, "metadata.user_id.session_id is absent"
	}
	return identity.SessionID, request.Model, ""
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateInspectionRecordSequence(records []Record, terminalKind string) error {
	if len(records) == 0 {
		return errors.New("raw record file is empty")
	}
	var expected uint64 = 1
	for _, record := range records {
		if record.Kind == RecordCaptureGap {
			return fmt.Errorf("capture gap covers seq %d-%d (%d records, %d bytes)", record.GapStartSeq, record.GapEndSeq, record.DroppedRecords, record.DroppedBytes)
		}
		if record.Seq != expected {
			return fmt.Errorf("record sequence discontinuity: got %d, want %d", record.Seq, expected)
		}
		expected++
	}
	if records[len(records)-1].Kind != terminalKind {
		return fmt.Errorf("terminal record = %q, want %q", records[len(records)-1].Kind, terminalKind)
	}
	return nil
}

func reconstructInspectionBody(records []Record, bodyKind, relative, exchangeID string) ([]byte, RawEvidence, error) {
	var body []byte
	evidence := RawEvidence{File: relative, ExchangeID: exchangeID, StreamOffset: -1}
	for _, record := range records {
		if record.Kind != bodyKind {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			return nil, evidence, fmt.Errorf("decode seq %d body: %w", record.Seq, err)
		}
		if int64(len(chunk)) != record.BodyBytes {
			return nil, evidence, fmt.Errorf("seq %d body bytes = %d, declared %d", record.Seq, len(chunk), record.BodyBytes)
		}
		if evidence.SeqStart == 0 {
			evidence.SeqStart = record.Seq
		}
		evidence.SeqEnd = record.Seq
		evidence.ObservedAt = record.Time
		evidence.ByteLength += int64(len(chunk))
		body = append(body, chunk...)
	}
	return body, evidence, nil
}

func validateInspectionBodyEnd(records []Record, endKind string, body []byte) error {
	end := firstInspectionRecord(records, endKind)
	if end == nil {
		return fmt.Errorf("missing %s", endKind)
	}
	if end.BodyBytes != int64(len(body)) {
		return fmt.Errorf("%s bytes = %d, reconstructed %d", endKind, end.BodyBytes, len(body))
	}
	hash := sha256.Sum256(body)
	actual := hex.EncodeToString(hash[:])
	if end.SHA256 != actual {
		return fmt.Errorf("%s hash = %s, reconstructed %s", endKind, end.SHA256, actual)
	}
	return nil
}

func firstInspectionRecord(records []Record, kind string) *Record {
	for index := range records {
		if records[index].Kind == kind {
			return &records[index]
		}
	}
	return nil
}

func collectProviderToolResults(body []byte, source RawEvidence, results map[string]inspectedProviderToolResult) error {
	var request providerRequestWire
	if err := json.Unmarshal(body, &request); err != nil {
		return err
	}
	for _, message := range request.Messages {
		if len(message.Content) == 0 || message.Content[0] != '[' {
			continue
		}
		var blocks []providerMessageBlock
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			return fmt.Errorf("decode message content: %w", err)
		}
		for _, block := range blocks {
			if block.Type != "tool_result" || block.ToolUseID == "" {
				continue
			}
			if _, exists := results[block.ToolUseID]; exists {
				continue
			}
			text, err := providerToolResultText(block.Content)
			if err != nil {
				return fmt.Errorf("decode tool result %s: %w", block.ToolUseID, err)
			}
			results[block.ToolUseID] = inspectedProviderToolResult{text: text, isError: block.IsError, source: source}
		}
	}
	return nil
}

func providerToolResultText(content json.RawMessage) (string, error) {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return "", nil
	}
	if content[0] == '"' {
		var text string
		if err := json.Unmarshal(content, &text); err != nil {
			return "", err
		}
		return text, nil
	}
	if content[0] == '[' {
		var blocks []providerMessageBlock
		if err := json.Unmarshal(content, &blocks); err != nil {
			return "", err
		}
		var texts []string
		for _, block := range blocks {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n"), nil
	}
	return compactInspectionJSON(content)
}

func decodeProviderResponse(body []byte, headers http.Header) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding")))
	gzipEncoded := false
	switch encoding {
	case "", "identity":
		// Some historical Claude captures omitted Content-Encoding even though
		// the entity retained gzip bytes. Preserve that explicitly supported
		// compatibility lane without treating arbitrary encodings as plaintext.
		gzipEncoded = len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
	case "gzip":
		gzipEncoded = true
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding %q", encoding)
	}
	if !gzipEncoded {
		return body, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open gzip stream: %w", err)
	}
	decoded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read gzip stream: %w", err)
	}
	return decoded, nil
}

func parseProviderSSEToolUses(decoded []byte, source RawEvidence, claudeSessionID string) ([]inspectedProviderToolUse, error) {
	states := make(map[int]*providerToolBlockState)
	var order []int
	var toolUses []inspectedProviderToolUse
	var offset int64
	for _, rawLine := range bytes.Split(decoded, []byte{'\n'}) {
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
		var event providerSSEEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("decode SSE data at offset %d: %w", lineOffset, err)
		}
		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type != "tool_use" {
				continue
			}
			states[event.Index] = &providerToolBlockState{
				index:         event.Index,
				toolUseID:     event.ContentBlock.ID,
				name:          event.ContentBlock.Name,
				initialInput:  append(json.RawMessage(nil), event.ContentBlock.Input...),
				decodedOffset: lineOffset,
			}
			order = append(order, event.Index)
		case "content_block_delta":
			state := states[event.Index]
			if state != nil && event.Delta.Type == "input_json_delta" {
				state.partialInput.WriteString(event.Delta.PartialJSON)
			}
		case "content_block_stop":
			state := states[event.Index]
			if state == nil {
				continue
			}
			toolUse, err := finalizeProviderToolBlock(state, source, claudeSessionID)
			if err != nil {
				return nil, err
			}
			toolUses = append(toolUses, toolUse)
			delete(states, event.Index)
		}
	}
	if len(states) > 0 {
		sort.Ints(order)
		for _, index := range order {
			state := states[index]
			if state == nil {
				continue
			}
			toolUse, err := finalizeProviderToolBlock(state, source, claudeSessionID)
			if err != nil {
				return nil, err
			}
			toolUses = append(toolUses, toolUse)
		}
	}
	return toolUses, nil
}

func finalizeProviderToolBlock(state *providerToolBlockState, source RawEvidence, claudeSessionID string) (inspectedProviderToolUse, error) {
	input := state.initialInput
	if state.partialInput.Len() > 0 {
		input = json.RawMessage(state.partialInput.String())
	}
	arguments, err := compactInspectionJSON(input)
	if err != nil {
		return inspectedProviderToolUse{}, fmt.Errorf("decode tool input for %s: %w", state.name, err)
	}
	source.DecodedOffset = state.decodedOffset
	return inspectedProviderToolUse{
		toolUseID:       state.toolUseID,
		name:            state.name,
		argumentsJSON:   arguments,
		claudeSessionID: claudeSessionID,
		source:          source,
	}, nil
}

func compactInspectionJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	return compact.String(), nil
}
