package projection

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

type clientStreamEvent struct {
	Type              string
	Subtype           string
	SessionID         string
	RequestID         string
	Model             string
	ClaudeCodeVersion string
	Timestamp         string
	DurationMS        *int64
	TTFTMS            *int64
	NumTurns          *int
	TotalCostUSD      *float64
	PermissionDenials []json.RawMessage
	StopReason        string
	TerminalReason    string
	Message           clientMessage
}

type clientMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type clientContentBlock struct {
	Type      string
	ID        string
	Name      string
	Input     json.RawMessage
	ToolUseID string
	Content   json.RawMessage
	Text      string
	Thinking  string
	IsError   bool
}

func (event *clientStreamEvent) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := decodeStringFields(fields, map[string]*string{
		"type": &event.Type, "subtype": &event.Subtype, "session_id": &event.SessionID,
		"request_id": &event.RequestID, "model": &event.Model, "claude_code_version": &event.ClaudeCodeVersion,
		"timestamp": &event.Timestamp, "stop_reason": &event.StopReason, "terminal_reason": &event.TerminalReason,
	}); err != nil {
		return err
	}
	if raw := fields["duration_ms"]; len(raw) > 0 {
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		event.DurationMS = &value
	}
	if raw := fields["ttft_ms"]; len(raw) > 0 {
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		event.TTFTMS = &value
	}
	if raw := fields["num_turns"]; len(raw) > 0 {
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		event.NumTurns = &value
	}
	if raw := fields["total_cost_usd"]; len(raw) > 0 {
		var value float64
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		event.TotalCostUSD = &value
	}
	if raw := fields["permission_denials"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &event.PermissionDenials); err != nil {
			return err
		}
	}
	if raw := fields["message"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &event.Message); err != nil {
			return err
		}
	}
	return nil
}

func (block *clientContentBlock) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := decodeStringFields(fields, map[string]*string{
		"type": &block.Type, "id": &block.ID, "name": &block.Name, "tool_use_id": &block.ToolUseID,
		blockTypeText: &block.Text, "thinking": &block.Thinking,
	}); err != nil {
		return err
	}
	block.Input = append(block.Input[:0], fields["input"]...)
	block.Content = append(block.Content[:0], fields["content"]...)
	if raw := fields["is_error"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &block.IsError); err != nil {
			return err
		}
	}
	return nil
}

func decodeStringFields(fields map[string]json.RawMessage, targets map[string]*string) error {
	for key, target := range targets {
		raw := fields[key]
		if len(raw) == 0 {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("decode %s: %w", key, err)
		}
	}
	return nil
}

type pendingClientTool struct {
	index int
}

func addClientArtifacts(view *View, sessionDir string, manifest *capture.SessionManifestDocument) {
	for _, file := range manifest.Files {
		if file.Kind != capture.ManifestFileEval {
			continue
		}
		base := filepath.Base(file.Path)
		if base != "transcript.jsonl" && base != "retrospective.jsonl" {
			continue
		}
		path, err := resolveFile(sessionDir, file.Path)
		if err != nil {
			view.Diagnostics = append(view.Diagnostics, clientArtifactDiagnostic(file.Path, err))
			continue
		}
		if err := projectClientArtifact(view, path, file.Path, strings.TrimSuffix(base, ".jsonl")); err != nil {
			view.Diagnostics = append(view.Diagnostics, clientArtifactDiagnostic(file.Path, err))
		}
	}
}

func projectClientArtifact(view *View, path, relative, kind string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	run := ClientRun{ArtifactPath: relative, Kind: kind}
	pending := make(map[string]pendingClientTool)
	seenTools := make(map[string]bool)
	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			var event clientStreamEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return fmt.Errorf("line %d: %w", lineNumber, err)
			}
			if run.ClientSessionID == "" {
				run.ClientSessionID = event.SessionID
			}
			evidence := EvidenceRef{ID: rawRecordID(relative, uint64(lineNumber)), File: relative, SeqStart: uint64(lineNumber), SeqEnd: uint64(lineNumber)}
			if parsed, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
				evidence.ObservedAt = parsed
			}
			conversation, err := summarizeConversationEvent(event, relative, kind, uint64(lineNumber), evidence)
			if err != nil {
				return fmt.Errorf("line %d conversation content: %w", lineNumber, err)
			}
			view.Conversation = append(view.Conversation, conversation)
			switch event.Type {
			case "system":
				if event.Subtype == "init" {
					if event.Model != "" {
						run.Model = event.Model
					}
					if event.ClaudeCodeVersion != "" {
						run.ClientVersion = event.ClaudeCodeVersion
					}
				}
				if event.Subtype == "thinking_tokens" {
					run.ThinkingEvents++
				}
			case "rate_limit_event":
				run.RateLimitEvents++
			case "assistant":
				run.AssistantEvents++
				blocks, err := decodeClientBlocks(event.Message.Content)
				if err != nil {
					return fmt.Errorf("line %d assistant content: %w", lineNumber, err)
				}
				for _, block := range blocks {
					if block.Type != blockTypeToolUse || block.ID == "" || seenTools[block.ID] || strings.HasPrefix(block.Name, "mcp__") {
						continue
					}
					seenTools[block.ID] = true
					tool := ToolExecution{
						ID: "client-tool:" + relative + ":" + block.ID, ClientSessionID: event.SessionID, Category: toolCategoryBuiltin, ToolName: block.Name,
						ToolUseID: block.ID, ClientArtifact: relative,
						ArgumentsBytes: len(block.Input), ArgumentsEqual: true, Propagation: "pending-client-result",
						CorrelationBasis: "joined-id", Evidence: []EvidenceRef{evidence},
					}
					view.Tools = append(view.Tools, tool)
					pending[block.ID] = pendingClientTool{index: len(view.Tools) - 1}
				}
			case "user":
				run.UserEvents++
				blocks, err := decodeClientBlocks(event.Message.Content)
				if err != nil {
					return fmt.Errorf("line %d user content: %w", lineNumber, err)
				}
				for _, block := range blocks {
					if block.Type != blockTypeToolResult || block.ToolUseID == "" {
						continue
					}
					match, ok := pending[block.ToolUseID]
					if !ok {
						continue
					}
					tool := &view.Tools[match.index]
					tool.ResultBytes = len(block.Content)
					tool.IsError = block.IsError
					tool.Propagation = "client-result"
					tool.CompletedAt = evidence.ObservedAt
					tool.Evidence = append(tool.Evidence, evidence)
					delete(pending, block.ToolUseID)
				}
			case "result":
				run.ResultEvents++
				run.ResultStatus = event.Subtype
				if event.DurationMS != nil {
					run.DurationMs += *event.DurationMS
					run.DurationReports++
				}
				if event.TTFTMS != nil {
					run.TTFTMs += *event.TTFTMS
					run.TTFTReports++
				}
				if event.NumTurns != nil {
					run.Turns += *event.NumTurns
					run.TurnReports++
				}
				if event.TotalCostUSD != nil {
					run.ReportedCostUSD += *event.TotalCostUSD
					run.CostReports++
				}
				run.PermissionDenials += len(event.PermissionDenials)
				run.StopReason = event.StopReason
				run.TerminalReason = event.TerminalReason
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
	}
	if lineNumber == 0 {
		return errors.New("artifact is empty")
	}
	run.Evidence = []EvidenceRef{{ID: rawRangeID(relative, 1, uint64(lineNumber)), File: relative, SeqStart: 1, SeqEnd: uint64(lineNumber)}}
	view.ClientRuns = append(view.ClientRuns, run)
	view.Overview.ClientTurns += run.Turns
	view.Overview.ClientDurationMs += run.DurationMs
	view.Overview.ClientTTFTMs += run.TTFTMs
	view.Overview.RateLimitEvents += run.RateLimitEvents
	view.Overview.ThinkingEvents += run.ThinkingEvents
	view.Overview.PermissionDenials += run.PermissionDenials
	view.Overview.ReportedCostUSD += run.ReportedCostUSD
	for _, unmatched := range pending {
		tool := &view.Tools[unmatched.index]
		view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
			Code: "client.tool_result.missing", Severity: "warning", Summary: "Built-in tool use has no matching stream-json tool result",
			Basis: "joined-id", ScopeID: tool.ID, Evidence: append([]EvidenceRef(nil), tool.Evidence...),
		})
	}
	return nil
}

func summarizeConversationEvent(event clientStreamEvent, artifact, kind string, line uint64, evidence EvidenceRef) (ConversationEvent, error) {
	value := ConversationEvent{
		ID: rawRecordID(artifact, line), ArtifactPath: artifact, ArtifactKind: kind, Line: line,
		Type: event.Type, Subtype: event.Subtype, Role: event.Message.Role, ClientSessionID: event.SessionID,
		RequestID: event.RequestID, ContentBytes: len(event.Message.Content), ObservedAt: evidence.ObservedAt,
		ContentTypes: []string{}, IsError: event.Type == "result" && event.Subtype != "success", Evidence: []EvidenceRef{evidence},
	}
	blocks, err := decodeClientBlocks(event.Message.Content)
	if err != nil {
		return ConversationEvent{}, err
	}
	if len(blocks) == 0 {
		var text string
		if json.Unmarshal(event.Message.Content, &text) == nil {
			value.ContentTypes = append(value.ContentTypes, blockTypeText)
			value.TextBytes = len(text)
		}
	}
	for _, block := range blocks {
		if block.Type != "" {
			value.ContentTypes = append(value.ContentTypes, block.Type)
		}
		switch block.Type {
		case blockTypeText:
			value.TextBytes += len(block.Text)
		case traceKindThinking, blockTypeRedactedThinking:
			value.ThinkingBytes += len(block.Thinking)
		case blockTypeToolUse:
			value.ToolUses++
		case blockTypeToolResult:
			value.ToolResults++
			value.IsError = value.IsError || block.IsError
		}
	}
	return value, nil
}

func decodeClientBlocks(raw json.RawMessage) ([]clientContentBlock, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] != '[' {
		return nil, nil
	}
	var blocks []clientContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func clientArtifactDiagnostic(path string, err error) StructuralDiagnostic {
	return StructuralDiagnostic{Code: "client.artifact.parse", Severity: "warning", Summary: fmt.Sprintf("%s: %v", path, err), Basis: "external", ScopeID: path}
}
