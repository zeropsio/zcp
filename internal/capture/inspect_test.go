package capture

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDeriveModelContext_ReportsHistoryRemovalAsReset(t *testing.T) {
	t.Parallel()
	firstBody := []byte(`{"model":"test","messages":[{"role":"user","content":"one"},{"role":"assistant","content":"two"}]}`)
	_, state, err := deriveModelContext("exchange-1", "session", firstBody, RawEvidence{}, modelContextState{})
	if err != nil {
		t.Fatalf("derive first context: %v", err)
	}
	secondBody := []byte(`{"model":"test","messages":[{"role":"user","content":"one"}]}`)
	second, _, err := deriveModelContext("exchange-2", "session", secondBody, RawEvidence{}, state)
	if err != nil {
		t.Fatalf("derive second context: %v", err)
	}
	if !second.ContextReset || second.CommonPrefixMessages != 1 || second.RemovedMessages != 1 || second.AddedMessages != 0 {
		t.Fatalf("reset context = %+v", second)
	}
}

func TestDeriveModelContext_IgnoresEphemeralCacheControlForLineage(t *testing.T) {
	t.Parallel()
	firstBody := []byte(`{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"one","cache_control":{"type":"ephemeral"}}]}]}`)
	_, state, err := deriveModelContext("exchange-1", "session", firstBody, RawEvidence{}, modelContextState{})
	if err != nil {
		t.Fatalf("derive first context: %v", err)
	}
	secondBody := []byte(`{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"one"}]},{"role":"assistant","content":"two"}]}`)
	second, _, err := deriveModelContext("exchange-2", "session", secondBody, RawEvidence{}, state)
	if err != nil {
		t.Fatalf("derive second context: %v", err)
	}
	if second.ContextReset || second.HistoryRewritten || second.CommonPrefixMessages != 1 || second.AddedMessages != 1 {
		t.Fatalf("cache-control lineage = %+v", second)
	}
}

func TestInspectProviderResponseMetadata_DistinguishesExplicitZeroFromMissing(t *testing.T) {
	t.Parallel()
	decoded := []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0,\"cache_read_input_tokens\":0}}}\n\n")
	metadata, err := inspectProviderResponseMetadata(decoded)
	if err != nil {
		t.Fatalf("inspectProviderResponseMetadata() error = %v", err)
	}
	if !metadata.inputTokensObserved || metadata.inputTokens != 0 || !metadata.cacheReadInputTokensObserved || metadata.cacheReadInputTokens != 0 {
		t.Fatalf("explicit zero usage = %+v", metadata)
	}
	if metadata.cacheCreationInputTokensObserved || metadata.outputTokensObserved {
		t.Fatalf("missing usage fields became observed = %+v", metadata)
	}
}

func TestInspectSession_CorrelatesProviderMCPAndProviderResult(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixture(t)
	report, err := InspectSession(sessionDir)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if !report.ManifestPresent || !report.Integrity.Valid {
		t.Fatalf("integrity = %+v, manifestPresent = %v", report.Integrity, report.ManifestPresent)
	}
	if report.ProviderExchanges != 2 || len(report.MCPStreams) != 1 {
		t.Fatalf("boundaries = provider:%d mcp:%d", report.ProviderExchanges, len(report.MCPStreams))
	}
	if len(report.ClaudeSessions) != 1 || report.ClaudeSessions[0].SessionID != "claude-session-fixture" || report.ClaudeSessions[0].ProviderExchanges != 2 {
		t.Fatalf("Claude sessions = %+v, want one exact two-exchange session", report.ClaudeSessions)
	}
	if report.UnattributedProviderExchanges != 0 {
		t.Fatalf("unattributed provider exchanges = %d, want 0", report.UnattributedProviderExchanges)
	}
	if len(report.EvalRuns) != 1 || report.EvalRuns[0].EvalRunID != "suite-fixture" || len(report.EvalRuns[0].Scenarios) != 1 {
		t.Fatalf("eval runs = %+v", report.EvalRuns)
	}
	invocations := report.EvalRuns[0].Scenarios[0].Invocations
	if len(invocations) != 1 || invocations[0].ClaudeSessionID != "claude-session-fixture" || invocations[0].ProviderExchanges != 2 || invocations[0].MCPStreams != 1 {
		t.Fatalf("eval invocations = %+v, want bound two-exchange/one-MCP invocation", invocations)
	}
	if len(report.ModelContexts) != 2 {
		t.Fatalf("model context count = %d, want 2", len(report.ModelContexts))
	}
	secondContext := report.ModelContexts[1]
	if secondContext.ClaudeSessionID != "claude-session-fixture" || secondContext.MessageCount != 3 || secondContext.AddedMessages != 2 || secondContext.ToolCount != 1 || secondContext.MCPToolCount != 1 {
		t.Fatalf("second model context = %+v", secondContext)
	}
	if len(report.Correlations) != 1 {
		t.Fatalf("correlation count = %d, want 1", len(report.Correlations))
	}
	correlation := report.Correlations[0]
	if correlation.ProviderToolName != "mcp__zerops__zerops_workflow" || correlation.MCPToolName != "zerops_workflow" || correlation.InvocationID != "weather/agent.initial" || correlation.ClaudeSessionID != "claude-session-fixture" {
		t.Fatalf("tool identity = %+v", correlation)
	}
	if !strings.Contains(correlation.ArgumentsJSON, `"bootstrapMode":"dev"`) {
		t.Fatalf("arguments = %s, want dev plan", correlation.ArgumentsJSON)
	}
	if !correlation.MCPIsError || !strings.Contains(correlation.MCPResultText, "INVALID_PARAMETER") {
		t.Fatalf("MCP result = %+v", correlation)
	}
	if !correlation.ProviderResultObserved || correlation.ProviderResultStatus != "exact" || !strings.Contains(correlation.ProviderResultText, "INVALID_PARAMETER") || !correlation.ProviderResultIsError || correlation.ProviderToolUseID != "toolu_1" || !correlation.ArgumentsEqual {
		t.Fatalf("evidence equality = %+v", correlation)
	}
	if len(correlation.CompositionMatches) != 1 || len(correlation.CompositionMatches[0].Components) != 1 || correlation.CompositionMatches[0].Components[0].Owner != "workflow.dynamicFixture" {
		t.Fatalf("composition provenance = %+v", correlation.CompositionMatches)
	}
	if correlation.ProviderSource.File != "provider.jsonl" || correlation.ProviderSource.SeqStart == 0 || correlation.ProviderSource.SeqEnd < correlation.ProviderSource.SeqStart {
		t.Fatalf("provider source = %+v", correlation.ProviderSource)
	}
	if correlation.MCPCallSource.File != "mcp/zcp-4242.jsonl" || correlation.MCPCallSource.SeqStart == 0 || correlation.MCPCallSource.StreamOffset < 0 {
		t.Fatalf("MCP call source = %+v", correlation.MCPCallSource)
	}
	if correlation.MCPResultSource.File != "mcp/zcp-4242.jsonl" || correlation.MCPResultSource.SeqStart == 0 {
		t.Fatalf("MCP result source = %+v", correlation.MCPResultSource)
	}

	var rendered bytes.Buffer
	if err := RenderInspection(&rendered, report); err != nil {
		t.Fatalf("RenderInspection() error = %v", err)
	}
	for _, want := range []string{
		"Integrity: OK",
		"MODEL -> MCP zerops_workflow",
		`"bootstrapMode":"dev"`,
		"mcp/zcp-4242.jsonl status=complete input=",
		"toolCalls=1",
		"MCP <- ERROR",
		"INVALID_PARAMETER",
		"provider tool_result: canonical-exact",
		"provider.jsonl seq",
		"mcp/zcp-4242.jsonl seq",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("rendered inspection missing %q:\n%s", want, rendered.String())
		}
	}
}

func TestFilterInspection_EvalInvocationReturnsWholeLinkedEvidence(t *testing.T) {
	t.Parallel()

	report, err := InspectSession(writeInspectionFixture(t))
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	filtered, err := FilterInspection(report, InspectionFilter{EvalRunID: "suite-fixture", ScenarioRunID: "weather", InvocationID: "weather/agent.initial"})
	if err != nil {
		t.Fatalf("FilterInspection() error = %v", err)
	}
	if len(filtered.EvalRuns) != 1 || len(filtered.ModelContexts) != 2 || len(filtered.MCPStreams) != 1 || len(filtered.Correlations) != 1 || filtered.ProviderExchanges != 2 {
		t.Fatalf("filtered report = eval:%d contexts:%d mcp:%d correlations:%d exchanges:%d", len(filtered.EvalRuns), len(filtered.ModelContexts), len(filtered.MCPStreams), len(filtered.Correlations), filtered.ProviderExchanges)
	}
	if _, err := FilterInspection(report, InspectionFilter{EvalRunID: "missing"}); err == nil {
		t.Fatal("FilterInspection() accepted missing eval run")
	}
}

func TestInspectSession_RejectsMissingTerminalRecord(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixture(t)
	if err := os.Remove(filepath.Join(sessionDir, manifestFilename)); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	path := filepath.Join(sessionDir, recordsFilename)
	records, err := ReadRecords(path)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, record := range records[:len(records)-1] {
		if err := encoder.Encode(record); err != nil {
			t.Fatalf("encode truncated record: %v", err)
		}
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatalf("write truncated provider: %v", err)
	}
	_, err = InspectSession(sessionDir)
	if err == nil || !strings.Contains(err.Error(), "terminal record") {
		t.Fatalf("InspectSession() error = %v, want missing terminal", err)
	}
}

func TestInspectSession_RejectsManifestHashMismatchBeforeParsing(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixture(t)
	providerPath := filepath.Join(sessionDir, "provider.jsonl")
	providerBytes, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider capture: %v", err)
	}
	providerBytes[0] ^= 1
	if err := os.WriteFile(providerPath, providerBytes, 0o600); err != nil {
		t.Fatalf("tamper provider capture: %v", err)
	}

	_, err = InspectSession(sessionDir)
	if err == nil || !strings.Contains(err.Error(), "manifest hash mismatch") {
		t.Fatalf("InspectSession() error = %v, want manifest hash mismatch", err)
	}
}

func TestInspectSession_LegacyCaptureWithoutManifestRemainsInspectable(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixture(t)
	if err := os.Remove(filepath.Join(sessionDir, manifestFilename)); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	report, err := InspectSession(sessionDir)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if report.ManifestPresent || !report.Integrity.Valid || !report.Integrity.Complete || len(report.Correlations) != 1 {
		t.Fatalf("legacy report = %+v", report)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "manifest") {
		t.Fatalf("legacy warnings = %v, want missing manifest", report.Warnings)
	}
}

func TestInspectSession_UncleanCrashPrefixRemainsSummarizable(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, recordsFilename), nil, 0o600); err != nil {
		t.Fatalf("write provider prefix: %v", err)
	}
	if _, err := NewSessionManifest(SessionManifestConfig{SessionDir: sessionDir, SessionID: "unclean-session"}); err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if _, err := RecoverUncleanSessionManifest(sessionDir); err != nil {
		t.Fatalf("RecoverUncleanSessionManifest() error = %v", err)
	}
	report, err := InspectSession(sessionDir)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if report.Status != CaptureUnclean || report.Integrity.Complete {
		t.Fatalf("unclean report = %+v", report)
	}
	var rendered bytes.Buffer
	if err := RenderInspectionSummary(&rendered, report); err != nil {
		t.Fatalf("RenderInspectionSummary() error = %v", err)
	}
	if !strings.Contains(rendered.String(), "Integrity: INCOMPLETE") {
		t.Fatalf("unclean summary = %s", rendered.String())
	}
}

func TestInspectSession_PartialCaptureNeverRendersIntegrityOK(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixtureWith(t, CapturePartial, "gzip")
	report, err := InspectSession(sessionDir)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if !report.Integrity.Valid || report.Integrity.Complete {
		t.Fatalf("integrity = %+v, want hash-valid but incomplete", report.Integrity)
	}
	var rendered bytes.Buffer
	if err := RenderInspection(&rendered, report); err != nil {
		t.Fatalf("RenderInspection() error = %v", err)
	}
	if strings.Contains(rendered.String(), "Integrity: OK") || !strings.Contains(rendered.String(), "Integrity: INCOMPLETE") {
		t.Fatalf("partial rendering must be visibly incomplete:\n%s", rendered.String())
	}
}

func TestInspectSession_RejectsUnknownManifestFormat(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixture(t)
	manifestPath := filepath.Join(sessionDir, manifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	data = bytes.Replace(data, []byte(ManifestFormat1), []byte("zcp-capture-unknown-99"), 1)
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, err = InspectSession(sessionDir)
	if err == nil || !strings.Contains(err.Error(), "unsupported capture manifest format") {
		t.Fatalf("InspectSession() error = %v, want unsupported format", err)
	}
}

func TestInspectSession_RejectsUnsupportedContentEncoding(t *testing.T) {
	t.Parallel()

	sessionDir := writeInspectionFixtureWith(t, CaptureComplete, "br")
	_, err := InspectSession(sessionDir)
	if err == nil || !strings.Contains(err.Error(), `unsupported Content-Encoding "br"`) {
		t.Fatalf("InspectSession() error = %v, want unsupported encoding", err)
	}
}

func writeInspectionFixture(t *testing.T) string {
	t.Helper()
	return writeInspectionFixtureWith(t, CaptureComplete, "gzip")
}

func writeInspectionFixtureWith(t *testing.T, terminalStatus, responseEncoding string) string {
	t.Helper()

	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: "inspect-session", Label: "inspect"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{
		SessionDir: recorder.SessionDir(),
		SessionID:  "inspect-session",
		Label:      "inspect",
		Command:    []string{"fixture"},
		Provider:   ProviderManifestInfo{Origin: "https://provider.example", ProxyURL: "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	lifecycle, err := NewLifecycleRecorder(recorder.SessionDir(), "inspect-session")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	for _, marker := range []LifecycleMarker{
		{Kind: LifecycleEvalRunStart, EvalRunID: "suite-fixture"},
		{Kind: LifecycleScenarioStart, EvalRunID: "suite-fixture", ScenarioRunID: "weather"},
		{Kind: LifecycleInvocationStart, EvalRunID: "suite-fixture", ScenarioRunID: "weather", InvocationID: "weather/agent.initial", Phase: "agent.initial"},
	} {
		if _, err := lifecycle.Mark(marker); err != nil {
			t.Fatalf("lifecycle.Mark(%s) error = %v", marker.Kind, err)
		}
	}

	arguments := `{"action":"start","workflow":"bootstrap","route":"classic","plan":[{"runtime":{"devHostname":"appdev","type":"nodejs@22","bootstrapMode":"dev"}}]}`
	providerArguments := strings.ReplaceAll(arguments, `\"`, `"`)
	metadataUserID := strconv.Quote(`{"device_id":"device-fixture","account_uuid":"account-fixture","session_id":"claude-session-fixture"}`)
	requestOne := []byte(`{"model":"claude-test","metadata":{"user_id":` + metadataUserID + `},"messages":[{"role":"user","content":"deploy"}],"tools":[{"name":"mcp__zerops__zerops_workflow"}]}`)
	responseOneDecoded := []byte(
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"mcp__zerops__zerops_workflow\",\"input\":{}}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":" + strconv.Quote(providerArguments) + "}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)
	responseOne := responseOneDecoded
	if responseEncoding == "gzip" {
		responseOne = gzipBytes(t, responseOneDecoded)
	}
	recordInspectionExchange(t, recorder, "exchange-000001", requestOne, responseOne, http.Header{
		"Content-Type":     []string{"text/event-stream; charset=utf-8"},
		"Content-Encoding": []string{responseEncoding},
	})

	resultText := `{"code":"INVALID_PARAMETER","error":"plan is not accepted in action=start"}`
	resultText = strings.ReplaceAll(resultText, `\"`, `"`)
	writeInspectionCompositionFixture(t, recorder.SessionDir(), resultText)
	requestTwo := []byte(`{
		"model":"claude-test",
		"metadata":{"user_id":` + metadataUserID + `},
		"messages":[
			{"role":"user","content":"deploy"},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"mcp__zerops__zerops_workflow","input":` + providerArguments + `}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":` + strconv.Quote(resultText) + `,"is_error":true}]}
		],
		"tools":[{"name":"mcp__zerops__zerops_workflow"}]
	}`)
	responseTwo := []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	recordInspectionExchange(t, recorder, "exchange-000002", requestTwo, responseTwo, http.Header{"Content-Type": []string{"text/event-stream"}})

	mcpRecorder, err := NewMCPRecorder(MCPRecorderConfig{
		SessionDir: recorder.SessionDir(), SessionID: "inspect-session", ProcessID: 4242,
		EvalRunID: "suite-fixture", ScenarioRunID: "weather", InvocationID: "weather/agent.initial", Phase: "agent.initial",
	})
	if err != nil {
		t.Fatalf("NewMCPRecorder() error = %v", err)
	}
	mcpRecorder.recordInput([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"zerops_workflow","arguments":` + providerArguments + "}}\n"))
	mcpRecorder.recordOutput([]byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":` + strconv.Quote(resultText) + `}],"isError":true}}` + "\n"))
	if err := mcpRecorder.Close(terminalStatus); err != nil {
		t.Fatalf("MCPRecorder.Close() error = %v", err)
	}
	for _, marker := range []LifecycleMarker{
		{Kind: LifecycleInvocationBind, EvalRunID: "suite-fixture", ScenarioRunID: "weather", InvocationID: "weather/agent.initial", Phase: "agent.initial", ClaudeSessionID: "claude-session-fixture"},
		{Kind: LifecycleInvocationEnd, EvalRunID: "suite-fixture", ScenarioRunID: "weather", InvocationID: "weather/agent.initial", Phase: "agent.initial", Status: terminalStatus},
		{Kind: LifecycleScenarioEnd, EvalRunID: "suite-fixture", ScenarioRunID: "weather", Status: terminalStatus},
		{Kind: LifecycleEvalRunEnd, EvalRunID: "suite-fixture", Status: terminalStatus},
	} {
		if _, err := lifecycle.Mark(marker); err != nil {
			t.Fatalf("lifecycle.Mark(%s) error = %v", marker.Kind, err)
		}
	}
	if err := lifecycle.Close(terminalStatus); err != nil {
		t.Fatalf("LifecycleRecorder.Close() error = %v", err)
	}
	if err := recorder.Close(terminalStatus, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}
	if err := manifest.Finalize(terminalStatus, 0); err != nil {
		t.Fatalf("manifest.Finalize() error = %v", err)
	}
	return recorder.SessionDir()
}

func writeInspectionCompositionFixture(t *testing.T, sessionDir, output string) {
	t.Helper()
	outputSum := sha256.Sum256([]byte(output))
	componentSum := sha256.Sum256([]byte(output))
	record := CompositionRecord{
		Time: time.Now().UTC(), CaptureID: "inspect-session", ProcessID: 4242, Surface: "fixture.dynamic",
		OutputBytes: len(output), OutputSHA256: hex.EncodeToString(outputSum[:]),
		Components: []CompositionComponent{{Kind: "dynamic", Owner: "workflow.dynamicFixture", Start: 0, End: len(output), SHA256: hex.EncodeToString(componentSum[:])}},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal composition fixture: %v", err)
	}
	data = append(data, '\n')
	dir := filepath.Join(sessionDir, "provenance")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir composition fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zcp-4242.jsonl"), data, 0o600); err != nil {
		t.Fatalf("write composition fixture: %v", err)
	}
}

func recordInspectionExchange(t *testing.T, recorder *Recorder, exchangeID string, requestBody, responseBody []byte, responseHeaders http.Header) {
	t.Helper()

	requestHash := sha256.Sum256(requestBody)
	responseHash := sha256.Sum256(responseBody)
	records := []Record{
		{Kind: RecordProviderRequestStart, ExchangeID: exchangeID, Direction: "client_to_provider", Method: http.MethodPost, Path: "/v1/messages", Headers: http.Header{"Content-Type": []string{"application/json"}}},
		{Kind: RecordProviderRequestBody, ExchangeID: exchangeID, Direction: "client_to_provider", BodyBase64: base64.StdEncoding.EncodeToString(requestBody), BodyBytes: int64(len(requestBody))},
		{Kind: RecordProviderRequestEnd, ExchangeID: exchangeID, Direction: "client_to_provider", BodyBytes: int64(len(requestBody)), SHA256: hex.EncodeToString(requestHash[:])},
		{Kind: RecordProviderResponseStart, ExchangeID: exchangeID, Direction: "provider_to_client", StatusCode: http.StatusOK, Headers: responseHeaders},
		{Kind: RecordProviderResponseBody, ExchangeID: exchangeID, Direction: "provider_to_client", BodyBase64: base64.StdEncoding.EncodeToString(responseBody), BodyBytes: int64(len(responseBody))},
		{Kind: RecordProviderResponseEnd, ExchangeID: exchangeID, Direction: "provider_to_client", BodyBytes: int64(len(responseBody)), SHA256: hex.EncodeToString(responseHash[:])},
	}
	for _, record := range records {
		if err := recorder.Record(record); err != nil {
			t.Fatalf("Recorder.Record(%s) error = %v", record.Kind, err)
		}
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return compressed.Bytes()
}
