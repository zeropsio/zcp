package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

type modelContextRequest struct {
	Model    string            `json:"model"`
	System   json.RawMessage   `json:"system"`
	Tools    []json.RawMessage `json:"tools"`
	Messages []json.RawMessage `json:"messages"`
}

type modelContextTool struct {
	Name string `json:"name"`
}

type modelContextState struct {
	present    bool
	systemHash [sha256.Size]byte
	toolsHash  [sha256.Size]byte
	messages   [][]byte
}

type providerResponseMetadata struct {
	messageID                        string
	inputTokens                      int64
	inputTokensObserved              bool
	cacheCreationInputTokens         int64
	cacheCreationInputTokensObserved bool
	cacheReadInputTokens             int64
	cacheReadInputTokensObserved     bool
	outputTokens                     int64
	outputTokensObserved             bool
}

type providerUsageWire struct {
	InputTokens              *int64 `json:"input_tokens"`                //nolint:tagliatelle // Anthropic wire schema
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"` //nolint:tagliatelle // Anthropic wire schema
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`     //nolint:tagliatelle // Anthropic wire schema
	OutputTokens             *int64 `json:"output_tokens"`               //nolint:tagliatelle // Anthropic wire schema
}

type providerMetadataEvent struct {
	Type    string            `json:"type"`
	Usage   providerUsageWire `json:"usage"`
	Message struct {
		ID    string            `json:"id"`
		Usage providerUsageWire `json:"usage"`
	} `json:"message"`
}

func deriveModelContext(exchangeID, sessionID string, body []byte, source RawEvidence, previous modelContextState) (ModelContextInspection, modelContextState, error) {
	var request modelContextRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return ModelContextInspection{}, modelContextState{}, err
	}
	contextView := ModelContextInspection{
		ExchangeID:      exchangeID,
		ClaudeSessionID: sessionID,
		Model:           request.Model,
		RequestBytes:    len(body),
		SystemBytes:     len(request.System),
		ToolCount:       len(request.Tools),
		MessageCount:    len(request.Messages),
		Source:          source,
	}
	contextView.SystemBlocks = countSystemBlocks(request.System)
	canonicalTools := make([][]byte, 0, len(request.Tools))
	for _, rawTool := range request.Tools {
		contextView.ToolBytes += len(rawTool)
		canonical, err := compactContextJSON(rawTool)
		if err != nil {
			return ModelContextInspection{}, modelContextState{}, fmt.Errorf("compact tool: %w", err)
		}
		canonicalTools = append(canonicalTools, canonical)
		var tool modelContextTool
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return ModelContextInspection{}, modelContextState{}, fmt.Errorf("decode tool name: %w", err)
		}
		if len(tool.Name) >= len("mcp__") && tool.Name[:len("mcp__")] == "mcp__" {
			contextView.MCPToolCount++
		} else {
			contextView.BuiltInToolCount++
		}
	}
	canonicalMessages := make([][]byte, 0, len(request.Messages))
	for _, rawMessage := range request.Messages {
		contextView.MessageBytes += len(rawMessage)
		canonical, err := normalizeContextLineageJSON(rawMessage)
		if err != nil {
			return ModelContextInspection{}, modelContextState{}, fmt.Errorf("normalize message lineage: %w", err)
		}
		canonicalMessages = append(canonicalMessages, canonical)
	}
	commonMessages := 0
	for commonMessages < len(previous.messages) && commonMessages < len(canonicalMessages) && bytes.Equal(previous.messages[commonMessages], canonicalMessages[commonMessages]) {
		commonMessages++
	}
	contextView.CommonPrefixMessages = commonMessages
	contextView.AddedMessages = len(canonicalMessages) - commonMessages
	if previous.present && commonMessages < len(previous.messages) {
		if len(canonicalMessages) < len(previous.messages) {
			contextView.RemovedMessages = len(previous.messages) - commonMessages
			contextView.ContextReset = true
		} else {
			contextView.RewrittenMessages = len(previous.messages) - commonMessages
			contextView.HistoryRewritten = true
		}
	}
	for index := commonMessages; index < len(request.Messages); index++ {
		contextView.AddedMessageBytes += len(request.Messages[index])
	}
	canonicalSystem, err := compactContextJSON(request.System)
	if err != nil {
		return ModelContextInspection{}, modelContextState{}, fmt.Errorf("compact system: %w", err)
	}
	joinedTools := bytes.Join(canonicalTools, []byte{0})
	next := modelContextState{
		present:    true,
		systemHash: sha256.Sum256(canonicalSystem),
		toolsHash:  sha256.Sum256(joinedTools),
		messages:   canonicalMessages,
	}
	if previous.present {
		contextView.SystemChanged = previous.systemHash != next.systemHash
		contextView.ToolsChanged = previous.toolsHash != next.toolsHash
	}
	return contextView, next, nil
}

func countSystemBlocks(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	if raw[0] != '[' {
		return 1
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return 0
	}
	return len(blocks)
}

func normalizeContextLineageJSON(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		delete(object, "cache_control")
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var normalized bytes.Buffer
		normalized.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				normalized.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			normalized.Write(encodedKey)
			normalized.WriteByte(':')
			value, err := normalizeContextLineageJSON(object[key])
			if err != nil {
				return nil, err
			}
			normalized.Write(value)
		}
		normalized.WriteByte('}')
		return normalized.Bytes(), nil
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			return nil, err
		}
		var normalized bytes.Buffer
		normalized.WriteByte('[')
		for index, item := range array {
			if index > 0 {
				normalized.WriteByte(',')
			}
			value, err := normalizeContextLineageJSON(item)
			if err != nil {
				return nil, err
			}
			normalized.Write(value)
		}
		normalized.WriteByte(']')
		return normalized.Bytes(), nil
	default:
		return compactContextJSON(raw)
	}
}

func compactContextJSON(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func inspectProviderResponseMetadata(decoded []byte) (providerResponseMetadata, error) {
	var metadata providerResponseMetadata
	for rawLine := range bytes.SplitSeq(decoded, []byte{'\n'}) {
		line := bytes.TrimSuffix(rawLine, []byte{'\r'})
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var event providerMetadataEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return providerResponseMetadata{}, err
		}
		switch event.Type {
		case "message_start":
			metadata.messageID = event.Message.ID
			applyProviderUsage(&metadata, event.Message.Usage)
		case "message_delta":
			applyProviderUsage(&metadata, event.Usage)
		}
	}
	return metadata, nil
}

func applyProviderUsage(metadata *providerResponseMetadata, usage providerUsageWire) {
	if usage.InputTokens != nil {
		metadata.inputTokens = *usage.InputTokens
		metadata.inputTokensObserved = true
	}
	if usage.CacheCreationInputTokens != nil {
		metadata.cacheCreationInputTokens = *usage.CacheCreationInputTokens
		metadata.cacheCreationInputTokensObserved = true
	}
	if usage.CacheReadInputTokens != nil {
		metadata.cacheReadInputTokens = *usage.CacheReadInputTokens
		metadata.cacheReadInputTokensObserved = true
	}
	if usage.OutputTokens != nil {
		metadata.outputTokens = *usage.OutputTokens
		metadata.outputTokensObserved = true
	}
}

func applyProviderResponseMetadata(contextView *ModelContextInspection, metadata providerResponseMetadata) {
	contextView.ProviderMessageID = metadata.messageID
	contextView.InputTokens = metadata.inputTokens
	contextView.InputTokensObserved = metadata.inputTokensObserved
	contextView.CacheCreationInputTokens = metadata.cacheCreationInputTokens
	contextView.CacheCreationInputTokensObserved = metadata.cacheCreationInputTokensObserved
	contextView.CacheReadInputTokens = metadata.cacheReadInputTokens
	contextView.CacheReadInputTokensObserved = metadata.cacheReadInputTokensObserved
	contextView.OutputTokens = metadata.outputTokens
	contextView.OutputTokensObserved = metadata.outputTokensObserved
}
