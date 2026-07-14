package projection

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

type mcpMessageWire struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type mcpResultWire struct {
	IsError bool `json:"isError"`
}

type mcpLineAssembler struct {
	buffer       []byte
	streamOffset int64
	lineOffset   int64
	firstSeq     uint64
	firstTime    time.Time
	firstRecord  capture.Record
}

type projectedMCPMessage struct {
	wire       mcpMessageWire
	bytes      int
	evidence   EvidenceRef
	invocation string
	phase      string
}

func projectMCPMessages(view *View, file string, records []capture.Record) {
	assemblers := make(map[string]*mcpLineAssembler)
	pending := make(map[string]int)
	for _, record := range records {
		if record.Kind != capture.RecordMCPStdinChunk && record.Kind != capture.RecordMCPStdoutChunk {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil || int64(len(chunk)) != record.BodyBytes {
			view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
				Code: "mcp.message.decode", Severity: "warning", Summary: fmt.Sprintf("%s seq %d has an invalid body encoding or length", file, record.Seq), Basis: "raw",
				Evidence: []EvidenceRef{{ID: rawRecordID(file, record.Seq), File: file, SeqStart: record.Seq, SeqEnd: record.Seq, ObservedAt: record.Time}},
			})
			continue
		}
		assembler := assemblers[record.Direction]
		if assembler == nil {
			assembler = &mcpLineAssembler{}
			assemblers[record.Direction] = assembler
		}
		messages := assembler.feed(file, record, chunk, view)
		for _, message := range messages {
			consumeMCPMessage(view, file, message, pending)
		}
	}
	for direction, assembler := range assemblers {
		if len(bytes.TrimSpace(assembler.buffer)) == 0 {
			continue
		}
		view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
			Code: "mcp.message.incomplete", Severity: "warning", Summary: fmt.Sprintf("%s %s ends with an incomplete JSON-RPC line", file, direction), Basis: "raw",
			Evidence: []EvidenceRef{{ID: rawRangeID(file, assembler.firstSeq, assembler.firstRecord.Seq), File: file, SeqStart: assembler.firstSeq, SeqEnd: assembler.firstRecord.Seq, StreamOffset: assembler.lineOffset, ObservedAt: assembler.firstRecord.Time}},
		})
	}
	for _, call := range view.MCPCalls {
		if call.File != file {
			continue
		}
		title := call.Method
		if call.ToolName != "" {
			title += " " + call.ToolName
		}
		view.Timeline = append(view.Timeline, TimelineEvent{
			ID: "timeline:" + call.ID, Kind: "mcp." + call.Kind, Lane: "mcp:" + file, Title: title,
			Status: call.Status, Basis: call.CorrelationBasis, StartedAt: call.StartedAt, EndedAt: call.CompletedAt,
			DurationMs: call.DurationMs, InvocationID: call.InvocationID, Phase: call.Phase,
			Evidence: append([]EvidenceRef(nil), call.Evidence...),
		})
	}
}

func (assembler *mcpLineAssembler) feed(file string, record capture.Record, chunk []byte, view *View) []projectedMCPMessage {
	if len(assembler.buffer) == 0 {
		assembler.firstSeq = record.Seq
		assembler.firstTime = record.Time
		assembler.firstRecord = record
		assembler.lineOffset = assembler.streamOffset
	}
	assembler.buffer = append(assembler.buffer, chunk...)
	assembler.streamOffset += int64(len(chunk))
	assembler.firstRecord = record
	var messages []projectedMCPMessage
	for {
		newline := bytes.IndexByte(assembler.buffer, '\n')
		if newline < 0 {
			break
		}
		line := bytes.TrimSpace(assembler.buffer[:newline])
		lineBytes := newline + 1
		evidence := EvidenceRef{
			ID: rawRangeID(file, assembler.firstSeq, record.Seq), File: file, SeqStart: assembler.firstSeq, SeqEnd: record.Seq,
			StreamOffset: assembler.lineOffset, ByteLength: int64(lineBytes), ObservedAt: record.Time,
		}
		if len(line) > 0 {
			var wire mcpMessageWire
			if err := json.Unmarshal(line, &wire); err != nil {
				view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
					Code: "mcp.message.parse", Severity: "warning", Summary: fmt.Sprintf("%s JSON-RPC line at offset %d: %v", file, assembler.lineOffset, err), Basis: "raw", Evidence: []EvidenceRef{evidence},
				})
			} else {
				messages = append(messages, projectedMCPMessage{wire: wire, bytes: len(line), evidence: evidence, invocation: record.InvocationID, phase: record.Phase})
			}
		}
		assembler.buffer = assembler.buffer[lineBytes:]
		assembler.lineOffset += int64(lineBytes)
		assembler.firstSeq = record.Seq
		assembler.firstTime = record.Time
	}
	return messages
}

func consumeMCPMessage(view *View, file string, message projectedMCPMessage, pending map[string]int) {
	requestID := strings.TrimSpace(string(message.wire.ID))
	if message.wire.Method != "" {
		kind := mcpMessageRequest
		status := "pending"
		if requestID == "" || requestID == "null" {
			kind = "notification"
			status = "observed"
		}
		call := MCPCall{
			ID: fmt.Sprintf("mcp-call:%s:%d", file, message.evidence.SeqStart), File: file, RequestID: requestID,
			Kind: kind, Method: message.wire.Method, ToolName: message.wire.Params.Name, Status: status,
			InvocationID: message.invocation, Phase: message.phase, RequestBytes: message.bytes,
			StartedAt: message.evidence.ObservedAt, CorrelationBasis: "stream-sequence", Evidence: []EvidenceRef{message.evidence},
		}
		view.MCPCalls = append(view.MCPCalls, call)
		if kind == mcpMessageRequest {
			pending[file+"\x00"+requestID] = len(view.MCPCalls) - 1
		}
		return
	}
	if requestID == "" || requestID == "null" {
		return
	}
	key := file + "\x00" + requestID
	index, ok := pending[key]
	if !ok {
		view.MCPCalls = append(view.MCPCalls, MCPCall{
			ID: fmt.Sprintf("mcp-response:%s:%d", file, message.evidence.SeqStart), File: file, RequestID: requestID,
			Kind: "unmatched-response", Status: mcpResponseStatus(message.wire), ResponseBytes: message.bytes,
			StartedAt: message.evidence.ObservedAt, CompletedAt: message.evidence.ObservedAt,
			CorrelationBasis: "unmatched", Evidence: []EvidenceRef{message.evidence},
		})
		return
	}
	call := &view.MCPCalls[index]
	call.Status = mcpResponseStatus(message.wire)
	call.ResponseBytes = message.bytes
	call.CompletedAt = message.evidence.ObservedAt
	if !call.StartedAt.IsZero() && !call.CompletedAt.IsZero() {
		call.DurationMs = call.CompletedAt.Sub(call.StartedAt).Milliseconds()
		call.TimingObserved = call.DurationMs >= 0
	}
	call.CorrelationBasis = "jsonrpc-id"
	call.Evidence = append(call.Evidence, message.evidence)
	delete(pending, key)
}

func mcpResponseStatus(message mcpMessageWire) string {
	errorRaw := bytes.TrimSpace(message.Error)
	if len(errorRaw) > 0 && !bytes.Equal(errorRaw, []byte("null")) {
		return statusError
	}
	var result mcpResultWire
	if json.Unmarshal(message.Result, &result) == nil && result.IsError {
		return "tool-error"
	}
	return "ok"
}
