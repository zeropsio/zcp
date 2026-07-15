package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInspectSession_RejectsSymlinkedEvidenceParent(t *testing.T) {
	t.Parallel()

	external := makeCompleteIdentityFixture(t, "external-capture")
	malicious := t.TempDir()
	if err := os.Symlink(external, filepath.Join(malicious, "evidence")); err != nil {
		t.Fatalf("symlink evidence parent: %v", err)
	}
	writeManifestFromSources(t, malicious, "declared-capture", []manifestSource{
		{kind: ManifestFileProvider, relative: "evidence/provider.jsonl", source: filepath.Join(external, "provider.jsonl")},
		{kind: ManifestFileLifecycle, relative: "evidence/lifecycle.jsonl", source: filepath.Join(external, "lifecycle.jsonl")},
	})
	if report, err := InspectSession(malicious); err == nil {
		t.Fatalf("InspectSession() followed a symlink parent and returned session %q", report.SessionID)
	}
}

func TestInspectSession_RejectsEvidenceFromDifferentCaptureIdentity(t *testing.T) {
	t.Parallel()

	external := makeCompleteIdentityFixture(t, "external-capture")
	malicious := t.TempDir()
	for _, name := range []string{"provider.jsonl", "lifecycle.jsonl"} {
		data, err := os.ReadFile(filepath.Join(external, name))
		if err != nil {
			t.Fatalf("read external %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(malicious, name), data, 0o600); err != nil {
			t.Fatalf("write malicious %s: %v", name, err)
		}
	}
	writeManifestFromSources(t, malicious, "declared-capture", []manifestSource{
		{kind: ManifestFileProvider, relative: "provider.jsonl", source: filepath.Join(malicious, "provider.jsonl")},
		{kind: ManifestFileLifecycle, relative: "lifecycle.jsonl", source: filepath.Join(malicious, "lifecycle.jsonl")},
	})
	if report, err := InspectSession(malicious); err == nil {
		t.Fatalf("InspectSession() accepted mixed identity and returned session %q", report.SessionID)
	}
}

func TestInspectSession_RequiresProviderStreamStart(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	provider := filepath.Join(sessionDir, "provider.jsonl")
	data, err := json.Marshal(Record{Seq: 1, SessionID: "provider-no-start", Kind: RecordSessionEnd, CaptureStatus: CaptureComplete, Time: time.Now().UTC()})
	if err != nil {
		t.Fatalf("marshal terminal: %v", err)
	}
	if err := os.WriteFile(provider, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write provider: %v", err)
	}
	writeManifestFromSources(t, sessionDir, "provider-no-start", []manifestSource{{kind: ManifestFileProvider, relative: "provider.jsonl", source: provider}})
	if report, err := InspectSession(sessionDir); err == nil {
		t.Fatalf("InspectSession() accepted terminal-only provider stream: %+v", report.Integrity)
	}
}

func TestInspectSession_RequiresMCPStreamStart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: "mcp-no-start"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: "mcp-no-start", Provider: ProviderManifestInfo{Origin: "https://api.anthropic.com"}})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if err := recorder.CloseDaemon(CaptureComplete); err != nil {
		t.Fatalf("CloseDaemon() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(recorder.SessionDir(), "mcp"), 0o700); err != nil {
		t.Fatalf("mkdir MCP: %v", err)
	}
	empty := sha256.Sum256(nil)
	record := Record{
		Seq: 1, Time: time.Now().UTC(), SessionID: "mcp-no-start", Kind: RecordMCPStreamEnd,
		ProcessID: 1, CaptureStatus: CaptureComplete,
		InputSHA256: hex.EncodeToString(empty[:]), OutputSHA256: hex.EncodeToString(empty[:]),
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal MCP terminal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recorder.SessionDir(), "mcp", "zcp-1.jsonl"), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write MCP terminal: %v", err)
	}
	if err := manifest.FinalizeDaemon(CaptureComplete); err != nil {
		t.Fatalf("FinalizeDaemon() error = %v", err)
	}
	if report, err := InspectSession(recorder.SessionDir()); err == nil {
		t.Fatalf("InspectSession() accepted terminal-only MCP stream: %+v", report.Integrity)
	}
}

func TestInspectSession_OpenLifecycleScopeIsIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: "lifecycle-open"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	lifecycle, err := NewLifecycleRecorder(recorder.SessionDir(), "lifecycle-open")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: "lifecycle-open", Provider: ProviderManifestInfo{Origin: "https://api.anthropic.com"}})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if err := recorder.CloseDaemon(CaptureComplete); err != nil {
		t.Fatalf("CloseDaemon() error = %v", err)
	}
	if err := lifecycle.Close(CaptureComplete); err != nil {
		t.Fatalf("lifecycle Close() error = %v", err)
	}
	if err := manifest.FinalizeDaemon(CaptureComplete); err != nil {
		t.Fatalf("FinalizeDaemon() error = %v", err)
	}

	path := filepath.Join(recorder.SessionDir(), lifecycleFilename)
	now := time.Now().UTC()
	records := []LifecycleRecord{
		{Seq: 1, Time: now, CaptureID: "lifecycle-open", LifecycleMarker: LifecycleMarker{Kind: LifecycleStreamStart, Status: CaptureRunning}},
		{Seq: 2, Time: now, CaptureID: "lifecycle-open", LifecycleMarker: LifecycleMarker{Kind: LifecycleEvalRunStart, EvalRunID: "run-open"}},
		{Seq: 3, Time: now, CaptureID: "lifecycle-open", LifecycleMarker: LifecycleMarker{Kind: LifecycleStreamEnd, Status: CaptureComplete}},
	}
	var lifecycleData []byte
	for _, record := range records {
		line, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatalf("marshal lifecycle: %v", marshalErr)
		}
		lifecycleData = append(lifecycleData, line...)
		lifecycleData = append(lifecycleData, '\n')
	}
	if err := os.WriteFile(path, lifecycleData, 0o600); err != nil {
		t.Fatalf("rewrite lifecycle: %v", err)
	}
	rewriteManifestFileHash(t, recorder.SessionDir(), lifecycleFilename, lifecycleData)

	report, err := InspectSession(recorder.SessionDir())
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if report.Integrity.Complete {
		t.Fatalf("open lifecycle scope reported complete; warnings=%v", report.Warnings)
	}
}

func TestParseProviderSSEToolUses_IncompleteBlockIsNotCompleted(t *testing.T) {
	t.Parallel()

	decoded := []byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu-incomplete\",\"name\":\"mcp__zerops__zerops_workflow\",\"input\":{}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"action\\\":\\\"start\\\"}\"}}\n\n")
	uses, err := parseProviderSSEToolUses(decoded, RawEvidence{File: "provider.jsonl", SeqStart: 1, SeqEnd: 2}, "session")
	if err == nil && len(uses) != 0 {
		t.Fatalf("incomplete SSE block produced completed tool uses: %+v", uses)
	}
}

func TestCorrelateToolEvidence_DifferentArgumentsStayUnmatched(t *testing.T) {
	t.Parallel()

	providerUses := []inspectedProviderToolUse{{
		toolUseID: "toolu-mismatch", name: "mcp__zerops__zerops_workflow", argumentsJSON: `{"action":"start"}`,
		source: RawEvidence{File: "provider.jsonl", ExchangeID: "exchange-1"},
	}}
	providerResults := map[string]inspectedProviderToolResult{
		"toolu-mismatch": {text: "same-result", source: RawEvidence{File: "provider.jsonl", ExchangeID: "exchange-2"}},
	}
	calls := []inspectedMCPCall{{
		name: "zerops_workflow", argumentsJSON: `{"action":"status"}`, requestID: "1",
		source: RawEvidence{File: "mcp/zcp-1.jsonl"},
		result: &inspectedMCPResult{text: "same-result", source: RawEvidence{File: "mcp/zcp-1.jsonl"}},
	}}
	if correlations := correlateToolEvidence(providerUses, providerResults, calls); len(correlations) != 0 {
		t.Fatalf("mismatched arguments produced correlations: %+v", correlations)
	}
}

func TestCorrelateToolEvidence_ExactRequiresWholeResultEvidence(t *testing.T) {
	t.Parallel()

	message := &mcpRPCMessage{ID: json.RawMessage(`1`), Result: json.RawMessage(`{"content":[{"type":"text","text":"same"},{"type":"image","data":"MISSING_IMAGE"}],"isError":false}`)}
	mcpResult, err := inspectMCPResult(message, RawEvidence{File: "mcp/zcp-1.jsonl"})
	if err != nil {
		t.Fatalf("inspectMCPResult() error = %v", err)
	}
	providerText, err := providerToolResultText(json.RawMessage(`[{"type":"text","text":"same"}]`))
	if err != nil {
		t.Fatalf("providerToolResultText() error = %v", err)
	}
	uses := []inspectedProviderToolUse{{toolUseID: "toolu-1", name: "mcp__zerops__zerops_workflow", argumentsJSON: `{"x":1}`}}
	results := map[string]inspectedProviderToolResult{
		"toolu-1": {text: providerText, canonical: mustCanonicalProviderResult(t, json.RawMessage(`[{"type":"text","text":"same"}]`), false)},
	}
	calls := []inspectedMCPCall{{name: "zerops_workflow", argumentsJSON: `{"x":1}`, requestID: "1", result: mcpResult}}
	correlations := correlateToolEvidence(uses, results, calls)
	if len(correlations) != 1 {
		t.Fatalf("correlations = %d, want 1", len(correlations))
	}
	if correlations[0].ProviderResultStatus == "exact" {
		t.Fatalf("non-text MCP content was discarded: %+v", correlations[0])
	}
}

func mustCanonicalProviderResult(t *testing.T, content json.RawMessage, isError bool) string {
	t.Helper()
	canonical, err := canonicalProviderToolResult(content, isError)
	if err != nil {
		t.Fatalf("canonicalProviderToolResult() error = %v", err)
	}
	return canonical
}

type manifestSource struct {
	kind     string
	relative string
	source   string
}

func makeCompleteIdentityFixture(t *testing.T, id string) string {
	t.Helper()
	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: id})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	lifecycle, err := NewLifecycleRecorder(recorder.SessionDir(), id)
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: id, Provider: ProviderManifestInfo{Origin: "https://api.anthropic.com"}})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if err := recorder.CloseDaemon(CaptureComplete); err != nil {
		t.Fatalf("CloseDaemon() error = %v", err)
	}
	if err := lifecycle.Close(CaptureComplete); err != nil {
		t.Fatalf("lifecycle Close() error = %v", err)
	}
	if err := manifest.FinalizeDaemon(CaptureComplete); err != nil {
		t.Fatalf("FinalizeDaemon() error = %v", err)
	}
	return recorder.SessionDir()
}

func writeManifestFromSources(t *testing.T, sessionDir, id string, sources []manifestSource) {
	t.Helper()
	started := time.Now().Add(-time.Second).UTC()
	ended := time.Now().UTC()
	document := SessionManifestDocument{
		FormatVersion: ManifestFormat1, SessionID: id, Plaintext: true,
		StartedAt: started, EndedAt: &ended, Status: CaptureComplete,
	}
	for _, source := range sources {
		data, err := os.ReadFile(source.source)
		if err != nil {
			t.Fatalf("read source %s: %v", source.source, err)
		}
		sum := sha256.Sum256(data)
		document.Files = append(document.Files, ManifestFile{
			Kind: source.kind, Path: source.relative, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
		})
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, manifestFilename), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func rewriteManifestFileHash(t *testing.T, sessionDir, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(sessionDir, manifestFilename)
	document, err := ReadSessionManifest(path)
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	sum := sha256.Sum256(data)
	found := false
	for index := range document.Files {
		if document.Files[index].Path == relative {
			document.Files[index].SizeBytes = int64(len(data))
			document.Files[index].SHA256 = hex.EncodeToString(sum[:])
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest file %q not found", relative)
	}
	manifestData, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, append(manifestData, '\n'), 0o600); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
}

func TestCanonicalToolResult_NormalizesProviderStringAndSingleTextBlock(t *testing.T) {
	t.Parallel()

	fromString, err := canonicalProviderToolResult(json.RawMessage(`"hello"`), false)
	if err != nil {
		t.Fatalf("canonical string: %v", err)
	}
	fromBlocks, err := canonicalProviderToolResult(json.RawMessage(`[{"text":"hello","type":"text"}]`), false)
	if err != nil {
		t.Fatalf("canonical blocks: %v", err)
	}
	if fromString != fromBlocks {
		t.Fatalf("canonical string %s differs from text block %s", fromString, fromBlocks)
	}
	if strings.Contains(fromString, "MISSING_IMAGE") {
		t.Fatal("unexpected fixture contamination")
	}
}
