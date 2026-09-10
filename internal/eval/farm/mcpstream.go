package farm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

// MCPToolCall is one tools/call request/response pair reconstructed from a
// run's captured MCP stdio window (capture/mcp/zcp-<pid>.jsonl, written by
// internal/capture's MCPRecorder). S5 (decision rows) and S7 (coverage)
// consume this to know which tool ran, with what action, and what lifecycle
// phase its result carried.
type MCPToolCall struct {
	// Tool is the MCP tool name ("zerops_workflow", "zerops_deploy", ...).
	Tool string
	// Action is arguments["action"] when the call's arguments carry a
	// string "action" key, else "".
	Action string
	// Arguments is the call's full arguments object. Its shape varies by
	// tool, so it is a plain map rather than a typed struct.
	Arguments map[string]any
	// ResultText is the joined text of the result's content parts (or, for
	// an error result, the compact JSON of the error object).
	ResultText string
	// ResultIsError is the result's isError flag (or true for a JSON-RPC
	// error response).
	ResultIsError bool
	// EnvelopePhase is the workflow.StateEnvelope "phase" field recovered
	// from whichever carrier ResultText uses (docs/spec-mate.md §1): the
	// top-level "envelope" key of a JSON-document result, or the last
	// trailing fenced ```json zcp-envelope``` block of a prose result.
	// Empty when neither carrier is present, or the carrier's body doesn't
	// parse (a malformed envelope is ignored, per §1.3).
	EnvelopePhase string
}

// ReadMCPStream reads one capture/mcp/zcp-<pid>.jsonl file and returns the
// tool calls it observed, in call order.
func ReadMCPStream(path string) ([]MCPToolCall, error) {
	records, err := capture.ReadRecords(path)
	if err != nil {
		return nil, fmt.Errorf("farm: read MCP capture %s: %w", path, err)
	}

	stdin, err := reconstructStream(records, capture.RecordMCPStdinChunk)
	if err != nil {
		return nil, fmt.Errorf("farm: reconstruct stdin %s: %w", path, err)
	}
	stdout, err := reconstructStream(records, capture.RecordMCPStdoutChunk)
	if err != nil {
		return nil, fmt.Errorf("farm: reconstruct stdout %s: %w", path, err)
	}

	calls, orderByID := parseToolCallRequests(splitLines(stdin))
	if err := applyToolCallResponses(calls, orderByID, splitLines(stdout)); err != nil {
		return nil, fmt.Errorf("farm: parse MCP responses %s: %w", path, err)
	}
	return calls, nil
}

// reconstructStream concatenates every record of kind (stdin or stdout
// chunks, in file order) after base64-decoding each body.
func reconstructStream(records []capture.Record, kind string) ([]byte, error) {
	var stream []byte
	for _, record := range records {
		if record.Kind != kind {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("seq %d: decode body: %w", record.Seq, err)
		}
		stream = append(stream, chunk...)
	}
	return stream, nil
}

func splitLines(stream []byte) [][]byte {
	var lines [][]byte
	for line := range bytes.SplitSeq(stream, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

type mcpRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type mcpCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// parseToolCallRequests scans the stdin lines for "tools/call" requests. It
// returns the calls in encounter order plus a lookup from the request's
// JSON-RPC id (as its raw JSON text) to that call's index, so responses can
// be matched up regardless of interleaving with other JSON-RPC traffic
// (initialize, tools/list, notifications).
func parseToolCallRequests(lines [][]byte) ([]MCPToolCall, map[string]int) {
	var calls []MCPToolCall
	byID := make(map[string]int)
	for _, line := range lines {
		var message mcpRPCMessage
		if err := json.Unmarshal(line, &message); err != nil || message.Method != "tools/call" {
			continue
		}
		var params mcpCallParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			continue
		}
		action, _ := params.Arguments["action"].(string)
		calls = append(calls, MCPToolCall{
			Tool:      params.Name,
			Action:    action,
			Arguments: params.Arguments,
		})
		byID[string(message.ID)] = len(calls) - 1
	}
	return calls, byID
}

// applyToolCallResponses scans the stdout lines for tools/call responses and
// fills in each matched call's result fields.
func applyToolCallResponses(calls []MCPToolCall, byID map[string]int, lines [][]byte) error {
	for _, line := range lines {
		var message mcpRPCMessage
		if err := json.Unmarshal(line, &message); err != nil {
			continue
		}
		if len(message.ID) == 0 {
			continue // notification, e.g. notifications/progress
		}
		index, ok := byID[string(message.ID)]
		if !ok {
			continue
		}
		text, isError, err := decodeToolCallResult(message)
		if err != nil {
			return err
		}
		calls[index].ResultText = text
		calls[index].ResultIsError = isError
		calls[index].EnvelopePhase = extractEnvelopePhase(text)
	}
	return nil
}

func decodeToolCallResult(message mcpRPCMessage) (text string, isError bool, err error) {
	if len(message.Error) > 0 && !bytes.Equal(bytes.TrimSpace(message.Error), []byte("null")) {
		return string(message.Error), true, nil
	}
	var result mcpCallResult
	if err := json.Unmarshal(message.Result, &result); err != nil {
		return "", false, fmt.Errorf("decode tools/call result: %w", err)
	}
	var texts []string
	for _, part := range result.Content {
		if part.Type == "text" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n"), result.IsError, nil
}

// envelopePhaseOnly is the only field this package reads out of a
// workflow.StateEnvelope — importing internal/workflow itself would pull an
// L3 dependency into this reader for one field.
type envelopePhaseOnly struct {
	Phase string `json:"phase"`
}

const envelopeFenceOpen = "```json zcp-envelope"
const envelopeFenceClose = "```"

// extractEnvelopePhase implements the reducer rule of docs/spec-mate.md
// §1.3: try the JSON carrier (a top-level "envelope" key) first, then the
// last complete fenced block. A malformed or absent envelope yields "".
func extractEnvelopePhase(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var doc struct {
			Envelope *envelopePhaseOnly `json:"envelope"`
		}
		if err := json.Unmarshal([]byte(trimmed), &doc); err == nil && doc.Envelope != nil {
			return doc.Envelope.Phase
		}
	}
	if body, ok := lastFencedEnvelopeBlock(text); ok {
		var envelope envelopePhaseOnly
		if err := json.Unmarshal([]byte(body), &envelope); err == nil {
			return envelope.Phase
		}
	}
	return ""
}

// lastFencedEnvelopeBlock returns the body of the last line-anchored
// ```json zcp-envelope ... ``` block in text (docs/spec-mate.md §1.1/§1.3:
// "the match is line-anchored"; "the last complete block wins").
func lastFencedEnvelopeBlock(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	var opens []int
	for i, line := range lines {
		if strings.TrimSpace(line) == envelopeFenceOpen {
			opens = append(opens, i)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(opens)))
	for _, open := range opens {
		for end := open + 1; end < len(lines); end++ {
			if strings.TrimSpace(lines[end]) == envelopeFenceClose {
				return strings.Join(lines[open+1:end], "\n"), true
			}
		}
	}
	return "", false
}
