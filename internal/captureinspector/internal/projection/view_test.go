package projection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

func TestBuild_CompleteCaptureProducesVersionedEvidenceView(t *testing.T) {
	t.Parallel()

	sessionDir := completeFixture(t)
	view, err := Build(context.Background(), sessionDir)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if view.FormatVersion != FormatVersion1 || view.Capture.ID != "capture-view-fixture" {
		t.Fatalf("view identity = %+v", view)
	}
	if !view.Integrity.Valid || !view.Integrity.Complete || view.Integrity.State != "ok" {
		t.Fatalf("integrity = %+v", view.Integrity)
	}
	if len(view.RawFiles) != 3 || len(view.Timeline) < 4 {
		t.Fatalf("raw/timeline = %d/%d", len(view.RawFiles), len(view.Timeline))
	}
	if len(view.Contexts) != 1 || view.Contexts[0].ToolCount != 1 || view.Contexts[0].MessageCount != 1 {
		t.Fatalf("context projection = %+v", view.Contexts)
	}
	providerEventDetail, err := ReadProviderEventDetail(sessionDir, "exchange-000001", 1)
	if err != nil || !strings.Contains(providerEventDetail.Payload, "message_start") {
		t.Fatalf("ReadProviderEventDetail() = %+v, %v", providerEventDetail, err)
	}
	rawPage, err := ReadRawRecordPage(sessionDir, "provider.jsonl", 0, 2)
	if err != nil || len(rawPage.Items) != 2 || !rawPage.HasMore || rawPage.NextAfter != rawPage.Items[1].Seq {
		t.Fatalf("ReadRawRecordPage() = %+v, %v", rawPage, err)
	}
	contextDetail, err := ReadContextDetail(sessionDir, "exchange-000001")
	if err != nil || contextDetail.Model != "claude-test" || len(contextDetail.Tools) != 1 || len(contextDetail.Messages) != 1 {
		t.Fatalf("ReadContextDetail() = %+v, %v", contextDetail, err)
	}
	if len(view.ClientRuns) != 1 || view.Overview.ClientTurns != 1 || view.Overview.ReportedCostUSD != 0.125 {
		t.Fatalf("client projection = runs:%+v overview:%+v", view.ClientRuns, view.Overview)
	}
	if len(view.Conversation) != 4 || view.Conversation[1].ToolUses != 1 || view.Conversation[2].ToolResults != 1 {
		t.Fatalf("conversation projection = %+v", view.Conversation)
	}
	lineDetail, err := ReadArtifactLine(sessionDir, view.Conversation[1].ArtifactPath, view.Conversation[1].Line, 1<<20)
	if err != nil || !strings.Contains(lineDetail.Content, "toolu_builtin") || lineDetail.Truncated {
		t.Fatalf("ReadArtifactLine() = %+v, %v", lineDetail, err)
	}
	if len(view.Tools) != 1 || view.Tools[0].Category != "builtin" || view.Tools[0].ToolName != "Bash" || view.Tools[0].Propagation != propagationClientResult {
		t.Fatalf("built-in tools = %+v", view.Tools)
	}
	toolDetail, err := ReadToolExecutionDetail(sessionDir, view.Tools[0])
	if err != nil || toolDetail.Category != "builtin" || toolDetail.ToolName != "Bash" || toolDetail.ResultText != "ok" || toolDetail.IsError {
		t.Fatalf("ReadToolExecutionDetail() = %+v, %v", toolDetail, err)
	}
	inputMetric := metricWithID(view.Metrics, "provider.tokens.input")
	costMetric := metricWithID(view.Metrics, "client.cost")
	if inputMetric == nil || inputMetric.Value != nil || inputMetric.MissingCount != 1 {
		t.Fatalf("missing provider usage metric = %+v", inputMetric)
	}
	if costMetric == nil || costMetric.Value == nil || *costMetric.Value != 0.125 || costMetric.Unit != "usd" || costMetric.Scope == "" || costMetric.EvidenceBasis == "" {
		t.Fatalf("client cost metric = %+v", costMetric)
	}
	if len(view.Metrics) < 100 {
		t.Fatalf("metric catalog has %d metrics, want at least 100 evidence dimensions", len(view.Metrics))
	}
	for _, projectedMetric := range view.Metrics {
		if projectedMetric.Scope == "" || projectedMetric.Unit == "" || projectedMetric.Denominator == nil || projectedMetric.EvidenceBasis == "" || projectedMetric.MissingCount < 0 {
			t.Fatalf("metric lacks dimensions: %+v", projectedMetric)
		}
	}
	comparison := Compare(view, view)
	inputDelta := metricDeltaWithID(comparison.Metrics, "provider.tokens.input")
	costDelta := metricDeltaWithID(comparison.Metrics, "client.cost")
	if inputDelta == nil || inputDelta.Left != nil || inputDelta.Right != nil || inputDelta.Delta != nil {
		t.Fatalf("unknown comparison metric = %+v", inputDelta)
	}
	if costDelta == nil || costDelta.Delta == nil || *costDelta.Delta != 0 {
		t.Fatalf("known comparison metric = %+v", costDelta)
	}
	if !edgeExists(view.Edges, "exchange-has-context", "exchange:exchange-000001", "context:exchange-000001") || !edgeExists(view.Edges, "artifact-has-event", "client-artifact:eval/suite/scenario/transcript.jsonl", view.Conversation[0].ID) {
		t.Fatalf("evidence graph edges = %+v", view.Edges)
	}
	for _, event := range view.Timeline {
		if event.ID == "" || len(event.Evidence) == 0 {
			t.Fatalf("timeline event lacks deterministic evidence: %+v", event)
		}
	}
}

func TestBuildSessionTrace_OrdersStoryWithoutLeakingPlaintext(t *testing.T) {
	t.Parallel()
	sessionDir := completeFixture(t)
	view, err := Build(context.Background(), sessionDir)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	trace, err := BuildSessionTrace(sessionDir, view, TraceFilter{SessionID: "client-session"})
	if err != nil || len(trace.Steps) != 2 || trace.Steps[0].Kind != traceKindPrompt || trace.Steps[1].Kind != traceKindModelText {
		t.Fatalf("BuildSessionTrace() = %+v, %v", trace, err)
	}
	traceJSON, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	if bytes.Contains(traceJSON, []byte("hello back")) || bytes.Contains(traceJSON, []byte(`"hello"`)) {
		t.Fatalf("trace skeleton leaked plaintext: %s", traceJSON)
	}
	promptDetail, err := ReadTraceContent(sessionDir, view, trace.Steps[0].ContentRefs[0].ID)
	if err != nil || promptDetail.Content != "hello" {
		t.Fatalf("prompt trace content = %+v, %v", promptDetail, err)
	}
	responseDetail, err := ReadTraceContent(sessionDir, view, trace.Steps[1].ContentRefs[0].ID)
	if err != nil || responseDetail.Content != "hello back" {
		t.Fatalf("response trace content = %+v, %v", responseDetail, err)
	}
}

func TestBuildSessionTrace_ProjectsDeterministicMetadataOnlyFlow(t *testing.T) {
	t.Parallel()
	sessionDir := completeFixture(t)
	view, err := Build(context.Background(), sessionDir)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	trace, err := BuildSessionTrace(sessionDir, view, TraceFilter{SessionID: "client-session"})
	if err != nil {
		t.Fatalf("BuildSessionTrace() error = %v", err)
	}
	if len(trace.Flow.Lanes) != 4 || len(trace.Flow.Turns) != 1 || trace.Flow.Summary.TurnCount != 1 {
		t.Fatalf("flow structure = %+v", trace.Flow)
	}
	if len(trace.Flow.Phases) != 1 || trace.Flow.Phases[0].Basis == "" {
		t.Fatalf("flow phase basis = %+v", trace.Flow.Phases)
	}
	if !flowNodeExists(trace.Flow.Nodes, "flow-prompt:exchange-000001", "user") ||
		!flowNodeExists(trace.Flow.Nodes, "flow-context:exchange-000001", "context") ||
		!flowNodeExists(trace.Flow.Nodes, "flow-model:exchange-000001", "model") {
		t.Fatalf("flow nodes = %+v", trace.Flow.Nodes)
	}
	if !flowEdgeExists(trace.Flow.Edges, "prompt-input", "flow-prompt:exchange-000001", "flow-context:exchange-000001", "exact") ||
		!flowEdgeExists(trace.Flow.Edges, "provider-request", "flow-context:exchange-000001", "flow-model:exchange-000001", "exact") {
		t.Fatalf("flow edges = %+v", trace.Flow.Edges)
	}
	encoded, err := json.Marshal(trace.Flow)
	if err != nil {
		t.Fatalf("marshal flow: %v", err)
	}
	if bytes.Contains(encoded, []byte("hello back")) || bytes.Contains(encoded, []byte(`"hello"`)) {
		t.Fatalf("flow projection leaked plaintext: %s", encoded)
	}
	repeated, err := BuildSessionTrace(sessionDir, view, TraceFilter{SessionID: "client-session"})
	if err != nil {
		t.Fatalf("repeated BuildSessionTrace() error = %v", err)
	}
	repeatedJSON, err := json.Marshal(repeated.Flow)
	if err != nil {
		t.Fatalf("marshal repeated flow: %v", err)
	}
	if !bytes.Equal(encoded, repeatedJSON) {
		t.Fatal("flow projection is not deterministic across identical builds")
	}
}

func TestBuildSessionFlow_PreservesPropagationDifferenceAndContextRewrite(t *testing.T) {
	t.Parallel()
	evidenceOne := EvidenceRef{ID: "evidence-one", File: "provider.jsonl", SeqStart: 1}
	evidenceTwo := EvidenceRef{ID: "evidence-two", File: "provider.jsonl", SeqStart: 2}
	view := &View{
		Capture: CaptureSummary{ID: "flow-fixture"},
		Exchanges: []ProviderExchange{
			{ID: "exchange-1", ClientSessionID: "session", Model: "claude-test", RequestBytes: 1000, ResponseBytes: 400, Status: "complete", Evidence: []EvidenceRef{evidenceOne}},
			{ID: "exchange-2", ClientSessionID: "session", Model: "claude-test", RequestBytes: 2000, ResponseBytes: 300, Status: "complete", Evidence: []EvidenceRef{evidenceTwo}},
		},
		Contexts: []ContextSnapshot{
			{ExchangeID: "exchange-1", RequestBytes: 1000, SystemBytes: 100, ToolBytes: 200, MessageBytes: 600, OtherBytes: 100, AddedMessageBytes: 150, Evidence: evidenceOne},
			{ExchangeID: "exchange-2", RequestBytes: 2000, SystemBytes: 100, ToolBytes: 200, MessageBytes: 1600, OtherBytes: 100, AddedMessageBytes: 1623, HistoryRewritten: true, RewrittenMessages: 1, Evidence: evidenceTwo},
		},
		Tools: []ToolExecution{{
			ID: "tool-1", ToolName: "zerops_import", ToolUseID: "toolu-1", ProposalExchangeID: "exchange-1", ResultExchangeID: "exchange-2",
			ArgumentsBytes: 162, ResultBytes: 1163, ProviderResultBytes: 1623, IsError: true, Propagation: propagationDifferent,
			CorrelationBasis: "tool-use-id", Evidence: []EvidenceRef{evidenceOne, evidenceTwo},
		}},
	}
	trace := &SessionTrace{CaptureID: "flow-fixture", SessionID: "session", Steps: []TraceStep{
		{ID: "trace-prompt", Order: 1, Kind: traceKindPrompt, Actor: "user", Title: "User prompt", ExchangeID: "exchange-1", SizeBytes: 150, SizeObserved: true, Evidence: []EvidenceRef{evidenceOne}},
		{ID: "trace-model", Order: 2, Kind: traceKindModelText, Actor: "claude", Title: "Claude response", ExchangeID: "exchange-1", SizeBytes: 75, SizeObserved: true, Evidence: []EvidenceRef{evidenceOne}},
		{ID: "trace-tool", Order: 3, Kind: traceKindTool, Actor: "tool", Title: "zerops_import", ExchangeID: "exchange-1", ToolExecutionID: "tool-1", Propagation: propagationDifferent, Status: statusError, SizeBytes: 1325, SizeObserved: true, Evidence: []EvidenceRef{evidenceOne, evidenceTwo}},
		{ID: "trace-final", Order: 4, Kind: traceKindModelText, Actor: "claude", Title: "Final model response", ExchangeID: "exchange-2", SizeBytes: 300, SizeObserved: true, Evidence: []EvidenceRef{evidenceTwo}},
	}}
	flow := buildSessionFlow(view, trace, []string{"exchange-1", "exchange-2"})
	propagation := flowEdgeWithKind(flow.Edges, "tool-result")
	if propagation == nil || propagation.FromID != "flow-tool:tool-1" || propagation.ToID != "flow-context:exchange-2" || propagation.Status != propagationDifferent {
		t.Fatalf("propagation edge = %+v", propagation)
	}
	if !propagation.SourceBytesObserved || !propagation.TargetBytesObserved || !propagation.DeltaBytesObserved || propagation.SourceBytes != 1163 || propagation.TargetBytes != 1623 || propagation.DeltaBytes != 460 {
		t.Fatalf("propagation byte dimensions = %+v", propagation)
	}
	carry := flowEdgeWithKind(flow.Edges, "context-carry")
	if carry == nil || carry.Status != "rewritten" || carry.FromID != "flow-context:exchange-1" || carry.ToID != "flow-context:exchange-2" {
		t.Fatalf("context carry edge = %+v", carry)
	}
	for _, edge := range flow.Edges {
		if edge.ID == "" || edge.FromID == "" || edge.ToID == "" || edge.Basis == "" {
			t.Fatalf("flow edge lacks deterministic evidence basis: %+v", edge)
		}
	}
}

func TestAppendRawRecordSummary_BoundsInitialProjection(t *testing.T) {
	t.Parallel()
	view := &View{RawRecords: []RawRecordSummary{}, Diagnostics: []StructuralDiagnostic{}}
	for sequence := 1; sequence <= maxProjectedRawRecords+10; sequence++ {
		appendRawRecordSummary(view, RawRecordSummary{Seq: uint64(sequence)})
	}
	if len(view.RawRecords) != maxProjectedRawRecords || view.RawRecordTotal != maxProjectedRawRecords+10 || !view.RawRecordsTruncated {
		t.Fatalf("bounded raw records = len:%d total:%d truncated:%v", len(view.RawRecords), view.RawRecordTotal, view.RawRecordsTruncated)
	}
}

func TestProjectProviderSSE_IndexesEveryEventAndBlockTypeWithoutPlaintext(t *testing.T) {
	t.Parallel()
	decoded := []byte("" +
		`data: {"type":"message_start","message":{"id":"msg_1"}}` + "\n\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hi"}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}` + "\n\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\\\"query\\\":\\\"zcp\\\"}"}}` + "\n\n" +
		`data: {"type":"content_block_stop","index":1}` + "\n\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n")
	view := &View{ProviderEvents: []ProviderEvent{}, ProviderBlocks: []ProviderBlock{}, Diagnostics: []StructuralDiagnostic{}}
	evidence := EvidenceRef{File: "provider.jsonl", SeqStart: 10, SeqEnd: 20, ExchangeID: "exchange-1"}
	projectProviderSSE(view, "exchange-1", decoded, evidence)
	if len(view.ProviderEvents) != 8 || len(view.ProviderBlocks) != 2 {
		t.Fatalf("provider projection = events:%+v blocks:%+v diagnostics:%+v", view.ProviderEvents, view.ProviderBlocks, view.Diagnostics)
	}
	if view.ProviderBlocks[0].Type != "text" || view.ProviderBlocks[0].TextBytes != len("hi there") || view.ProviderBlocks[0].Status != "complete" {
		t.Fatalf("text block = %+v", view.ProviderBlocks[0])
	}
	if view.ProviderBlocks[1].Type != "server_tool_use" || view.ProviderBlocks[1].ToolName != "web_search" || view.ProviderBlocks[1].InputJSONBytes == 0 {
		t.Fatalf("server tool block = %+v", view.ProviderBlocks[1])
	}
	page, err := PageProviderEvents(view, 1, 2)
	if err != nil || page.Total != 8 || len(page.Items) != 2 || page.Items[0].Ordinal != 2 {
		t.Fatalf("PageProviderEvents() = %+v, %v", page, err)
	}
}

func TestScanRoot_DoesNotFollowManifestSymlink(t *testing.T) {
	t.Parallel()

	external := completeFixture(t)
	root := t.TempDir()
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(alias, 0o700); err != nil {
		t.Fatalf("mkdir alias: %v", err)
	}
	if err := os.Symlink(filepath.Join(external, "manifest.json"), filepath.Join(alias, "manifest.json")); err != nil {
		t.Fatalf("symlink manifest: %v", err)
	}
	entries, err := ScanRoot(context.Background(), root)
	if err == nil && len(entries) != 0 {
		t.Fatalf("ScanRoot() followed a manifest symlink and indexed %q", entries[0].ID)
	}
}

func TestReadArtifactLine_RejectsSymlinkedParentEscape(t *testing.T) {
	t.Parallel()
	sessionDir := completeFixture(t)
	relative := filepath.Join("eval", "suite", "scenario", "transcript.jsonl")
	data, err := os.ReadFile(filepath.Join(sessionDir, relative))
	if err != nil {
		t.Fatalf("read transcript fixture: %v", err)
	}
	external := t.TempDir()
	externalPath := filepath.Join(external, "suite", "scenario")
	if err := os.MkdirAll(externalPath, 0o700); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	if err := os.WriteFile(filepath.Join(externalPath, "transcript.jsonl"), data, 0o600); err != nil {
		t.Fatalf("write external transcript: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(sessionDir, "eval")); err != nil {
		t.Fatalf("remove eval directory: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(sessionDir, "eval")); err != nil {
		t.Fatalf("symlink eval directory: %v", err)
	}
	if _, err := ReadArtifactLine(sessionDir, filepath.ToSlash(relative), 1, 1024); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ReadArtifactLine() error = %v, want symlink rejection", err)
	}
}

func TestAddMCPRecordEvents_ProjectsAllJSONRPCMethodsAcrossChunkBoundaries(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)
	request := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n")
	split := 19
	response := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n")
	records := []capture.Record{
		{Seq: 1, Time: started, Kind: capture.RecordMCPStreamStart, InvocationID: "invocation", Phase: "agent.initial"},
		{Seq: 2, Time: started.Add(time.Millisecond), Kind: capture.RecordMCPStdinChunk, Direction: "client_to_zcp", BodyBase64: base64.StdEncoding.EncodeToString(request[:split]), BodyBytes: int64(split), InvocationID: "invocation", Phase: "agent.initial"},
		{Seq: 3, Time: started.Add(2 * time.Millisecond), Kind: capture.RecordMCPStdinChunk, Direction: "client_to_zcp", BodyBase64: base64.StdEncoding.EncodeToString(request[split:]), BodyBytes: int64(len(request) - split), InvocationID: "invocation", Phase: "agent.initial"},
		{Seq: 4, Time: started.Add(8 * time.Millisecond), Kind: capture.RecordMCPStdoutChunk, Direction: "zcp_to_client", BodyBase64: base64.StdEncoding.EncodeToString(response), BodyBytes: int64(len(response)), InvocationID: "invocation", Phase: "agent.initial"},
	}
	view := &View{MCPCalls: []MCPCall{}, Timeline: []TimelineEvent{}}
	addMCPRecordEvents(view, "mcp/zcp-1.jsonl", records)
	if len(view.MCPCalls) != 2 {
		t.Fatalf("MCP calls = %+v", view.MCPCalls)
	}
	call := view.MCPCalls[0]
	if call.Method != "tools/list" || call.Status != "ok" || call.DurationMs != 6 || len(call.Evidence) != 2 || call.Evidence[0].SeqStart != 2 || call.Evidence[0].SeqEnd != 3 {
		t.Fatalf("correlated MCP call = %+v", call)
	}
	if view.MCPCalls[1].Kind != "notification" || view.MCPCalls[1].Method != "notifications/initialized" {
		t.Fatalf("MCP notification = %+v", view.MCPCalls[1])
	}
}

func TestAddMCPRecordEvents_IDsIncludeCanonicalLineCoordinates(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)
	request := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"first\"}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"second\"}}\n")
	records := []capture.Record{{
		Seq: 1, Time: started, Kind: capture.RecordMCPStdinChunk, Direction: "client_to_zcp",
		BodyBase64: base64.StdEncoding.EncodeToString(request), BodyBytes: int64(len(request)),
	}}
	view := &View{MCPCalls: []MCPCall{}, Timeline: []TimelineEvent{}}
	addMCPRecordEvents(view, "mcp/zcp-1.jsonl", records)
	if len(view.MCPCalls) != 2 {
		t.Fatalf("MCP calls = %+v", view.MCPCalls)
	}
	if view.MCPCalls[0].ID == view.MCPCalls[1].ID {
		t.Fatalf("distinct lines in one raw record share ID %q", view.MCPCalls[0].ID)
	}
}

func TestBuildEdges_UsesActualToolCorrelationBasis(t *testing.T) {
	t.Parallel()

	view := &View{Capture: CaptureSummary{ID: "audit"}, Tools: []ToolExecution{{
		ID: "tool:1", ProposalExchangeID: "exchange-1", MCPFile: "mcp/zcp-1.jsonl",
		ArgumentsEqual: false, CorrelationBasis: "name-order",
	}}}
	buildEdges(view)
	for _, edge := range view.Edges {
		if edge.Kind == "tool-dispatched-to-mcp" && edge.Basis != "name-order" {
			t.Fatalf("edge basis = %q, want source correlation basis", edge.Basis)
		}
	}
}

func TestBuild_InvalidCaptureReturnsManifestOnlyDegradedView(t *testing.T) {
	t.Parallel()
	sessionDir := completeFixture(t)
	providerPath := filepath.Join(sessionDir, "provider.jsonl")
	file, err := os.OpenFile(providerPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open provider for tamper: %v", err)
	}
	if _, err := file.WriteString("tamper\n"); err != nil {
		_ = file.Close()
		t.Fatalf("tamper provider: %v", err)
	}
	_ = file.Close()
	view, err := Build(context.Background(), sessionDir)
	if err != nil {
		t.Fatalf("Build(invalid) error = %v", err)
	}
	if view.Integrity.Valid || view.Integrity.Complete || view.Integrity.State != "invalid" || len(view.Diagnostics) == 0 || len(view.RawRecords) != 0 {
		t.Fatalf("invalid degraded view = integrity:%+v diagnostics:%+v raw:%d", view.Integrity, view.Diagnostics, len(view.RawRecords))
	}
}

func TestBuild_PartialCaptureNeverClaimsIntegrityOK(t *testing.T) {
	t.Parallel()
	view, err := Build(context.Background(), fixtureWithStatus(t, capture.CapturePartial))
	if err != nil {
		t.Fatalf("Build(partial) error = %v", err)
	}
	if !view.Integrity.Valid || view.Integrity.Complete || view.Integrity.State != "incomplete" || view.Capture.Status != capture.CapturePartial {
		t.Fatalf("partial integrity = %+v capture=%+v", view.Integrity, view.Capture)
	}
}

func TestBuild_RunningCaptureUsesDurablePrefixWithoutClaimingIntegrity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	recorder, err := capture.NewRecorder(capture.RecorderConfig{RootDir: root, SessionID: "running-view-fixture", Label: "running"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	lifecycle, err := capture.NewLifecycleRecorder(recorder.SessionDir(), "running-view-fixture")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	if _, err := capture.NewSessionManifest(capture.SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: "running-view-fixture", Label: "running"}); err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if _, err := lifecycle.Mark(capture.LifecycleMarker{Kind: capture.LifecycleEvalRunStart, EvalRunID: "running-suite"}); err != nil {
		t.Fatalf("lifecycle.Mark() error = %v", err)
	}
	if err := recorder.Record(capture.Record{Kind: capture.RecordProviderRequestStart, ExchangeID: "exchange-running", Method: http.MethodPost, Path: "/v1/messages"}); err != nil {
		t.Fatalf("Recorder.Record() error = %v", err)
	}
	if err := recorder.CloseDaemon(capture.CapturePartial); err != nil {
		t.Fatalf("Recorder.CloseDaemon() error = %v", err)
	}
	t.Cleanup(func() { _ = lifecycle.Close(capture.CapturePartial) })
	view, err := Build(context.Background(), recorder.SessionDir())
	if err != nil {
		t.Fatalf("Build(running) error = %v", err)
	}
	if view.Integrity.State != "running" || view.Integrity.Complete || view.Capture.Status != capture.CaptureRunning {
		t.Fatalf("running integrity = %+v capture=%+v", view.Integrity, view.Capture)
	}
	if len(view.Exchanges) != 1 || !timelineHasKind(view.Timeline, capture.LifecycleEvalRunStart) {
		t.Fatalf("running durable prefix = exchanges:%+v timeline:%+v", view.Exchanges, view.Timeline)
	}
	durationMetric := metricWithID(view.Metrics, "capture.duration")
	if durationMetric == nil || durationMetric.Value != nil || durationMetric.MissingCount != 1 {
		t.Fatalf("running duration metric = %+v", durationMetric)
	}
}

func TestScanRoot_UsesManifestWithoutRequiringBodyProjection(t *testing.T) {
	t.Parallel()

	sessionDir := completeFixture(t)
	entries, err := ScanRoot(context.Background(), filepath.Dir(sessionDir))
	if err != nil {
		t.Fatalf("ScanRoot() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "capture-view-fixture" || entries[0].Status != capture.CaptureComplete {
		t.Fatalf("entries = %+v", entries)
	}

	nestedRoot := t.TempDir()
	nestedSource := completeFixture(t)
	nestedDestination := filepath.Join(nestedRoot, "suite", "scenario", "capture")
	if err := os.MkdirAll(filepath.Dir(nestedDestination), 0o700); err != nil {
		t.Fatalf("mkdir nested root: %v", err)
	}
	if err := os.Rename(nestedSource, nestedDestination); err != nil {
		t.Fatalf("move nested capture: %v", err)
	}
	entries, err = ScanRoot(context.Background(), nestedRoot)
	if err != nil || len(entries) != 1 || entries[0].ID != "capture-view-fixture" {
		t.Fatalf("ScanRoot(nested) = %+v, %v", entries, err)
	}

	duplicateRoot := t.TempDir()
	for _, name := range []string{"one", "two"} {
		source := completeFixture(t)
		destination := filepath.Join(duplicateRoot, name)
		if err := os.Rename(source, destination); err != nil {
			t.Fatalf("move duplicate capture %s: %v", name, err)
		}
	}
	if _, err := ScanRoot(context.Background(), duplicateRoot); err == nil || !strings.Contains(err.Error(), "duplicate capture ID") {
		t.Fatalf("ScanRoot(duplicates) error = %v", err)
	}
}

func flowNodeExists(nodes []FlowNode, id, lane string) bool {
	for _, node := range nodes {
		if node.ID == id && node.Lane == lane {
			return true
		}
	}
	return false
}

func flowEdgeExists(edges []FlowEdge, kind, from, to, status string) bool {
	for _, edge := range edges {
		if edge.Kind == kind && edge.FromID == from && edge.ToID == to && edge.Status == status {
			return true
		}
	}
	return false
}

func flowEdgeWithKind(edges []FlowEdge, kind string) *FlowEdge {
	for index := range edges {
		if edges[index].Kind == kind {
			return &edges[index]
		}
	}
	return nil
}

func edgeExists(edges []Edge, kind, from, to string) bool {
	for _, edge := range edges {
		if edge.Kind == kind && edge.FromID == from && edge.ToID == to {
			return true
		}
	}
	return false
}

func timelineHasKind(events []TimelineEvent, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func metricDeltaWithID(metrics []MetricDelta, id string) *MetricDelta {
	for index := range metrics {
		if metrics[index].ID == id {
			return &metrics[index]
		}
	}
	return nil
}

func metricWithID(metrics []Metric, id string) *Metric {
	for index := range metrics {
		if metrics[index].ID == id {
			return &metrics[index]
		}
	}
	return nil
}

func completeFixture(t *testing.T) string {
	t.Helper()
	return fixtureWithStatus(t, capture.CaptureComplete)
}

func fixtureWithStatus(t *testing.T, status string) string {
	t.Helper()
	root := t.TempDir()
	recorder, err := capture.NewRecorder(capture.RecorderConfig{RootDir: root, SessionID: "capture-view-fixture", Label: "view-fixture"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	lifecycle, err := capture.NewLifecycleRecorder(recorder.SessionDir(), "capture-view-fixture")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	manifest, err := capture.NewSessionManifest(capture.SessionManifestConfig{
		SessionDir: recorder.SessionDir(), SessionID: "capture-view-fixture", Label: "view-fixture",
		Build:    capture.CaptureBuildInfo{Version: "test", Commit: "deadbeef"},
		Provider: capture.ProviderManifestInfo{Origin: "https://api.anthropic.com", ProxyURL: "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	requestBody := []byte(`{"model":"claude-test","metadata":{"user_id":"{\"session_id\":\"client-session\"}"},"system":[{"type":"text","text":"system"}],"tools":[{"name":"Bash","description":"shell"}],"messages":[{"role":"user","content":"hello"}]}`)
	requestHash := sha256.Sum256(requestBody)
	responseBody := []byte("" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fixture\"}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello back\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n")
	responseHash := sha256.Sum256(responseBody)
	for _, record := range []capture.Record{
		{Kind: capture.RecordProviderRequestStart, ExchangeID: "exchange-000001", Method: http.MethodPost, Path: "/v1/messages"},
		{Kind: capture.RecordProviderRequestBody, ExchangeID: "exchange-000001", BodyBase64: base64.StdEncoding.EncodeToString(requestBody), BodyBytes: int64(len(requestBody))},
		{Kind: capture.RecordProviderRequestEnd, ExchangeID: "exchange-000001", BodyBytes: int64(len(requestBody)), SHA256: hex.EncodeToString(requestHash[:])},
		{Kind: capture.RecordProviderResponseStart, ExchangeID: "exchange-000001", StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"text/event-stream"}}},
		{Kind: capture.RecordProviderResponseBody, ExchangeID: "exchange-000001", BodyBase64: base64.StdEncoding.EncodeToString(responseBody), BodyBytes: int64(len(responseBody))},
		{Kind: capture.RecordProviderResponseEnd, ExchangeID: "exchange-000001", BodyBytes: int64(len(responseBody)), SHA256: hex.EncodeToString(responseHash[:])},
	} {
		if err := recorder.Record(record); err != nil {
			t.Fatalf("record provider fixture %s: %v", record.Kind, err)
		}
	}
	evalDir := filepath.Join(recorder.SessionDir(), "eval", "suite", "scenario")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		t.Fatalf("mkdir eval fixture: %v", err)
	}
	transcript := "" +
		`{"type":"system","subtype":"init","session_id":"client-session","model":"claude-test"}` + "\n" +
		`{"type":"assistant","session_id":"client-session","request_id":"request-1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_builtin","name":"Bash","input":{"command":"echo ok"}}]}}` + "\n" +
		`{"type":"user","session_id":"client-session","timestamp":"2026-07-13T20:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_builtin","content":"ok","is_error":false}]}}` + "\n" +
		`{"type":"result","subtype":"success","session_id":"client-session","duration_ms":1200,"ttft_ms":80,"num_turns":1,"total_cost_usd":0.125,"permission_denials":[]}` + "\n"
	if err := os.WriteFile(filepath.Join(evalDir, "transcript.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatalf("write transcript fixture: %v", err)
	}
	if err := recorder.CloseDaemon(status); err != nil {
		t.Fatalf("Recorder.CloseDaemon() error = %v", err)
	}
	if err := lifecycle.Close(status); err != nil {
		t.Fatalf("LifecycleRecorder.Close() error = %v", err)
	}
	if err := manifest.FinalizeDaemon(status); err != nil {
		t.Fatalf("FinalizeDaemon() error = %v", err)
	}
	return recorder.SessionDir()
}
