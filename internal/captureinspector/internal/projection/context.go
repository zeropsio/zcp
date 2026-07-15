package projection

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/capture"
)

const maxContextDetailBytes = 4 << 20

type contextRequestWire struct {
	Model    string            `json:"model"`
	System   json.RawMessage   `json:"system"`
	Tools    []json.RawMessage `json:"tools"`
	Messages []json.RawMessage `json:"messages"`
	Metadata json.RawMessage   `json:"metadata"`
}

type contextToolWire struct {
	Name string `json:"name"`
}
type contextMessageWire struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type contextBlockWire struct {
	Type string `json:"type"`
}

func ReadContextDetail(sessionDir, exchangeID string) (*ContextDetail, error) {
	if exchangeID == "" {
		return nil, errors.New("context exchange ID is required")
	}
	kind, path, err := inventoriedFile(sessionDir, "provider.jsonl")
	if err != nil {
		return nil, err
	}
	if kind != capture.ManifestFileProvider {
		return nil, errors.New("provider.jsonl is not a provider file")
	}
	records, err := capture.ReadRecords(path)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	var seqStart, seqEnd uint64
	for _, record := range records {
		if record.ExchangeID != exchangeID || record.Kind != capture.RecordProviderRequestBody {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode provider request seq %d: %w", record.Seq, err)
		}
		if body.Len()+len(decoded) > maxContextDetailBytes {
			return nil, fmt.Errorf("provider request exceeds %d-byte context detail limit", maxContextDetailBytes)
		}
		if seqStart == 0 {
			seqStart = record.Seq
		}
		seqEnd = record.Seq
		_, _ = body.Write(decoded)
	}
	if seqStart == 0 {
		return nil, fmt.Errorf("provider request body for %s not found", exchangeID)
	}
	var request contextRequestWire
	if err := json.Unmarshal(body.Bytes(), &request); err != nil {
		return nil, fmt.Errorf("decode provider request: %w", err)
	}
	detail := &ContextDetail{
		ExchangeID: exchangeID, Model: request.Model, RequestBytes: body.Len(), Tools: []ContextToolDetail{}, Messages: []ContextMessageDetail{},
		System: append(json.RawMessage(nil), request.System...), Metadata: append(json.RawMessage(nil), request.Metadata...),
		RawRequest: append(json.RawMessage(nil), body.Bytes()...),
		Evidence:   EvidenceRef{ID: rawRangeID("provider.jsonl", seqStart, seqEnd), File: "provider.jsonl", SeqStart: seqStart, SeqEnd: seqEnd, ExchangeID: exchangeID, ByteLength: int64(body.Len())},
	}
	for _, raw := range request.Tools {
		var tool contextToolWire
		_ = json.Unmarshal(raw, &tool)
		detail.Tools = append(detail.Tools, ContextToolDetail{Name: tool.Name, Bytes: len(raw), JSON: append(json.RawMessage(nil), raw...)})
	}
	for _, raw := range request.Messages {
		var message contextMessageWire
		_ = json.Unmarshal(raw, &message)
		item := ContextMessageDetail{Role: message.Role, Bytes: len(raw), JSON: append(json.RawMessage(nil), raw...)}
		content := bytes.TrimSpace(message.Content)
		if len(content) > 0 && content[0] == '[' {
			var blocks []contextBlockWire
			if json.Unmarshal(content, &blocks) == nil {
				for _, block := range blocks {
					if block.Type != "" {
						item.ContentTypes = append(item.ContentTypes, block.Type)
					}
				}
			}
		} else if len(content) > 0 {
			item.ContentTypes = []string{blockTypeText}
		}
		detail.Messages = append(detail.Messages, item)
	}
	return detail, nil
}
