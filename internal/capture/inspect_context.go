package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	messageID                string
	inputTokens              int64
	cacheCreationInputTokens int64
	cacheReadInputTokens     int64
	outputTokens             int64
}

type providerUsageWire struct {
	InputTokens              int64 `json:"input_tokens"`                //nolint:tagliatelle // Anthropic wire schema
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"` //nolint:tagliatelle // Anthropic wire schema
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`     //nolint:tagliatelle // Anthropic wire schema
	OutputTokens             int64 `json:"output_tokens"`               //nolint:tagliatelle // Anthropic wire schema
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
		canonical, err := compactContextJSON(rawMessage)
		if err != nil {
			return ModelContextInspection{}, modelContextState{}, fmt.Errorf("compact message: %w", err)
		}
		canonicalMessages = append(canonicalMessages, canonical)
	}
	commonMessages := 0
	for commonMessages < len(previous.messages) && commonMessages < len(canonicalMessages) && bytes.Equal(previous.messages[commonMessages], canonicalMessages[commonMessages]) {
		commonMessages++
	}
	contextView.AddedMessages = len(canonicalMessages) - commonMessages
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
	for _, rawLine := range bytes.Split(decoded, []byte{'\n'}) {
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
			metadata.inputTokens = event.Message.Usage.InputTokens
			metadata.cacheCreationInputTokens = event.Message.Usage.CacheCreationInputTokens
			metadata.cacheReadInputTokens = event.Message.Usage.CacheReadInputTokens
			metadata.outputTokens = event.Message.Usage.OutputTokens
		case "message_delta":
			if event.Usage.OutputTokens != 0 {
				metadata.outputTokens = event.Usage.OutputTokens
			}
		}
	}
	return metadata, nil
}

func applyProviderResponseMetadata(contextView *ModelContextInspection, metadata providerResponseMetadata) {
	contextView.ProviderMessageID = metadata.messageID
	contextView.InputTokens = metadata.inputTokens
	contextView.CacheCreationInputTokens = metadata.cacheCreationInputTokens
	contextView.CacheReadInputTokens = metadata.cacheReadInputTokens
	contextView.OutputTokens = metadata.outputTokens
}
