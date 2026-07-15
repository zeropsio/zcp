package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type mcpInspectionData struct {
	summary     MCPStreamInspection
	recordCount int
	calls       []inspectedMCPCall
}

type inspectedMCPCall struct {
	name          string
	argumentsJSON string
	requestID     string
	invocationID  string
	source        RawEvidence
	result        *inspectedMCPResult
}

type inspectedMCPResult struct {
	text      string
	canonical string
	isError   bool
	source    RawEvidence
}

type mcpRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type mcpCallResult struct {
	Content []mcpResultContent `json:"content"`
	IsError bool               `json:"isError"`
}

type mcpResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type inspectionStreamChunk struct {
	offset int64
	seq    uint64
	time   time.Time
	data   []byte
}

type inspectionStreamLine struct {
	data   []byte
	source RawEvidence
}

func inspectMCPFile(path, relative, expectedCaptureID string) (*mcpInspectionData, error) {
	records, err := ReadRecords(path)
	if err != nil {
		return nil, err
	}
	if _, err := validateInspectionRecordSequence(records, RecordMCPStreamStart, RecordMCPStreamEnd, expectedCaptureID); err != nil {
		return nil, err
	}
	terminal := records[len(records)-1]
	input, inputChunks, err := reconstructMCPStream(records, RecordMCPStdinChunk)
	if err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	output, outputChunks, err := reconstructMCPStream(records, RecordMCPStdoutChunk)
	if err != nil {
		return nil, fmt.Errorf("stdout: %w", err)
	}
	if err := validateMCPStreamTerminal(terminal, input, output); err != nil {
		return nil, err
	}
	inputLines, err := inspectionStreamLines(input, inputChunks, relative)
	if err != nil {
		return nil, fmt.Errorf("stdin framing: %w", err)
	}
	outputLines, err := inspectionStreamLines(output, outputChunks, relative)
	if err != nil {
		return nil, fmt.Errorf("stdout framing: %w", err)
	}

	start := records[0]
	data := &mcpInspectionData{
		recordCount: len(records),
		summary: MCPStreamInspection{
			File:          relative,
			Status:        terminal.CaptureStatus,
			EvalRunID:     start.EvalRunID,
			ScenarioRunID: start.ScenarioRunID,
			InvocationID:  start.InvocationID,
			Phase:         start.Phase,
			InputBytes:    int64(len(input)),
			OutputBytes:   int64(len(output)),
		},
	}
	callsByID := make(map[string]int)
	for _, line := range inputLines {
		message, err := decodeMCPMessage(line.data)
		if err != nil {
			return nil, fmt.Errorf("stdin offset %d: %w", line.source.StreamOffset, err)
		}
		if message.Method != "tools/call" {
			continue
		}
		var params mcpCallParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return nil, fmt.Errorf("decode tools/call params: %w", err)
		}
		arguments, err := compactInspectionJSON(params.Arguments)
		if err != nil {
			return nil, fmt.Errorf("decode tools/call arguments: %w", err)
		}
		requestID, err := compactInspectionJSON(message.ID)
		if err != nil {
			return nil, fmt.Errorf("decode tools/call ID: %w", err)
		}
		call := inspectedMCPCall{
			name:          params.Name,
			argumentsJSON: arguments,
			requestID:     requestID,
			invocationID:  start.InvocationID,
			source:        line.source,
		}
		data.calls = append(data.calls, call)
		callsByID[requestID] = len(data.calls) - 1
	}
	data.summary.ToolCalls = len(data.calls)

	for _, line := range outputLines {
		message, err := decodeMCPMessage(line.data)
		if err != nil {
			return nil, fmt.Errorf("stdout offset %d: %w", line.source.StreamOffset, err)
		}
		if message.Method == "notifications/progress" {
			data.summary.ProgressNotifications++
			continue
		}
		if len(message.ID) == 0 {
			continue
		}
		requestID, err := compactInspectionJSON(message.ID)
		if err != nil {
			return nil, fmt.Errorf("decode response ID: %w", err)
		}
		callIndex, ok := callsByID[requestID]
		if !ok {
			continue
		}
		result, err := inspectMCPResult(message, line.source)
		if err != nil {
			return nil, fmt.Errorf("decode tools/call response %s: %w", requestID, err)
		}
		data.calls[callIndex].result = result
	}
	return data, nil
}

func decodeMCPMessage(line []byte) (*mcpRPCMessage, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return &mcpRPCMessage{}, nil
	}
	if line[0] == '[' {
		return nil, fmt.Errorf("JSON-RPC batches are not supported by this inspector version")
	}
	var message mcpRPCMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func reconstructMCPStream(records []Record, kind string) ([]byte, []inspectionStreamChunk, error) {
	var stream []byte
	var chunks []inspectionStreamChunk
	var expectedOffset int64
	for _, record := range records {
		if record.Kind != kind {
			continue
		}
		if record.StreamOffset != expectedOffset {
			return nil, nil, fmt.Errorf("seq %d stream offset = %d, want %d", record.Seq, record.StreamOffset, expectedOffset)
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("decode seq %d: %w", record.Seq, err)
		}
		if int64(len(chunk)) != record.BodyBytes {
			return nil, nil, fmt.Errorf("seq %d bytes = %d, declared %d", record.Seq, len(chunk), record.BodyBytes)
		}
		hash := sha256.Sum256(chunk)
		actualHash := hex.EncodeToString(hash[:])
		if record.SHA256 != actualHash {
			return nil, nil, fmt.Errorf("seq %d hash = %s, reconstructed %s", record.Seq, record.SHA256, actualHash)
		}
		chunks = append(chunks, inspectionStreamChunk{offset: expectedOffset, seq: record.Seq, time: record.Time, data: chunk})
		stream = append(stream, chunk...)
		expectedOffset += int64(len(chunk))
	}
	return stream, chunks, nil
}

func validateMCPStreamTerminal(terminal Record, input, output []byte) error {
	inputHash := sha256.Sum256(input)
	outputHash := sha256.Sum256(output)
	if terminal.InputBytes != int64(len(input)) || terminal.InputSHA256 != hex.EncodeToString(inputHash[:]) {
		return fmt.Errorf("terminal stdin integrity mismatch")
	}
	if terminal.OutputBytes != int64(len(output)) || terminal.OutputSHA256 != hex.EncodeToString(outputHash[:]) {
		return fmt.Errorf("terminal stdout integrity mismatch")
	}
	return nil
}

func inspectionStreamLines(stream []byte, chunks []inspectionStreamChunk, relative string) ([]inspectionStreamLine, error) {
	var lines []inspectionStreamLine
	for start := 0; start < len(stream); {
		newline := bytes.IndexByte(stream[start:], '\n')
		end := len(stream)
		lineEnd := end
		if newline >= 0 {
			lineEnd = start + newline
			end = lineEnd + 1
		}
		line := bytes.TrimSuffix(stream[start:lineEnd], []byte{'\r'})
		if len(bytes.TrimSpace(line)) > 0 {
			startChunk := inspectionChunkAt(chunks, int64(start))
			endChunk := inspectionChunkAt(chunks, int64(end-1))
			if startChunk == nil || endChunk == nil {
				return nil, fmt.Errorf("line offset %d-%d is outside recorded chunks", start, end)
			}
			lines = append(lines, inspectionStreamLine{
				data: append([]byte(nil), line...),
				source: RawEvidence{
					File:         relative,
					SeqStart:     startChunk.seq,
					SeqEnd:       endChunk.seq,
					StreamOffset: int64(start),
					ByteLength:   int64(end - start),
					ObservedAt:   endChunk.time,
				},
			})
		}
		start = end
	}
	return lines, nil
}

func inspectionChunkAt(chunks []inspectionStreamChunk, offset int64) *inspectionStreamChunk {
	index := sort.Search(len(chunks), func(index int) bool {
		return chunks[index].offset+int64(len(chunks[index].data)) > offset
	})
	if index == len(chunks) || chunks[index].offset > offset {
		return nil
	}
	return &chunks[index]
}

func inspectMCPResult(message *mcpRPCMessage, source RawEvidence) (*inspectedMCPResult, error) {
	canonical, err := canonicalMCPToolResult(message.Result, message.Error)
	if err != nil {
		return nil, err
	}
	if len(message.Error) > 0 && !bytes.Equal(bytes.TrimSpace(message.Error), []byte("null")) {
		text, err := compactInspectionJSON(message.Error)
		if err != nil {
			return nil, err
		}
		return &inspectedMCPResult{text: text, canonical: canonical, isError: true, source: source}, nil
	}
	var result mcpCallResult
	if err := json.Unmarshal(message.Result, &result); err != nil {
		return nil, err
	}
	var texts []string
	for _, content := range result.Content {
		if content.Type == "text" {
			texts = append(texts, content.Text)
		}
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		var err error
		text, err = compactInspectionJSON(message.Result)
		if err != nil {
			return nil, err
		}
	}
	return &inspectedMCPResult{text: text, canonical: canonical, isError: result.IsError, source: source}, nil
}
