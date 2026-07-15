package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func canonicalProviderToolResult(content json.RawMessage, isError bool) (string, error) {
	normalized, err := normalizeToolResultContent(content)
	if err != nil {
		return "", err
	}
	return marshalCanonicalJSON(map[string]any{
		"content": normalized,
		"isError": isError,
	})
}

func canonicalMCPToolResult(resultRaw, errorRaw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(errorRaw)) > 0 && !bytes.Equal(bytes.TrimSpace(errorRaw), []byte("null")) {
		value, err := decodeCanonicalJSON(errorRaw)
		if err != nil {
			return "", err
		}
		return marshalCanonicalJSON(map[string]any{
			"isError":      true,
			"jsonrpcError": value,
		})
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(resultRaw, &fields); err != nil {
		return "", err
	}
	content, err := normalizeToolResultContent(fields["content"])
	if err != nil {
		return "", fmt.Errorf("normalize result content: %w", err)
	}
	isError := false
	if raw := bytes.TrimSpace(fields["isError"]); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &isError); err != nil {
			return "", fmt.Errorf("decode result isError: %w", err)
		}
	}
	canonical := map[string]any{
		"content": content,
		"isError": isError,
	}
	for name, raw := range fields {
		if name == "content" || name == "isError" {
			continue
		}
		value, err := decodeCanonicalJSON(raw)
		if err != nil {
			return "", fmt.Errorf("decode result field %s: %w", name, err)
		}
		canonical[name] = value
	}
	return marshalCanonicalJSON(canonical)
}

func normalizeToolResultContent(raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []any{}, nil
	}
	value, err := decodeCanonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	if text, ok := value.(string); ok {
		return []any{map[string]any{"text": text, "type": "text"}}, nil
	}
	return value, nil
}

func decodeCanonicalJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON value has trailing content")
	}
	return value, nil
}

func marshalCanonicalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
