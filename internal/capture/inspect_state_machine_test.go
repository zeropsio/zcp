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

func TestInspectSession_IncompleteProviderExchangeIsNotComplete(t *testing.T) {
	t.Parallel()

	empty := sha256.Sum256(nil)
	report, err := inspectProviderStateFixture(t, "incomplete-exchange", []Record{
		{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1", Method: "POST", Path: inspectionMessagesPath},
		{Kind: RecordProviderRequestEnd, ExchangeID: "exchange-1", SHA256: hex.EncodeToString(empty[:])},
	})
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if report.Integrity.Complete {
		t.Fatalf("request with neither response nor exchange error reported integrity complete; warnings=%v", report.Warnings)
	}
}

func TestInspectSession_IncompleteProviderResponseIsNotComplete(t *testing.T) {
	t.Parallel()

	empty := sha256.Sum256(nil)
	report, err := inspectProviderStateFixture(t, "incomplete-response", []Record{
		{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1", Method: "POST", Path: inspectionMessagesPath},
		{Kind: RecordProviderRequestEnd, ExchangeID: "exchange-1", SHA256: hex.EncodeToString(empty[:])},
		{Kind: RecordProviderResponseStart, ExchangeID: "exchange-1", StatusCode: 200},
	})
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if report.Integrity.Complete {
		t.Fatalf("unterminated provider response reported integrity complete; warnings=%v", report.Warnings)
	}
}

func TestInspectSession_ProviderExchangeErrorIsTerminal(t *testing.T) {
	t.Parallel()

	report, err := inspectProviderStateFixture(t, "exchange-error", []Record{
		{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1", Method: "POST", Path: inspectionMessagesPath},
		{Kind: RecordProviderExchangeError, ExchangeID: "exchange-1", Error: "read request failed"},
	})
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if !report.Integrity.Complete {
		t.Fatalf("terminal exchange error made complete stream incomplete: warnings=%v", report.Warnings)
	}
}

func TestInspectSession_RejectsIllegalProviderTransitions(t *testing.T) {
	t.Parallel()

	empty := sha256.Sum256(nil)
	tests := []struct {
		name    string
		records []Record
	}{
		{
			name: "body before request start",
			records: []Record{
				{Kind: RecordProviderRequestBody, ExchangeID: "exchange-1"},
			},
		},
		{
			name: "response before request end",
			records: []Record{
				{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1"},
				{Kind: RecordProviderResponseStart, ExchangeID: "exchange-1"},
			},
		},
		{
			name: "record after exchange error",
			records: []Record{
				{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1"},
				{Kind: RecordProviderExchangeError, ExchangeID: "exchange-1", Error: "failed"},
				{Kind: RecordProviderResponseStart, ExchangeID: "exchange-1"},
			},
		},
		{
			name: "duplicate request end",
			records: []Record{
				{Kind: RecordProviderRequestStart, ExchangeID: "exchange-1"},
				{Kind: RecordProviderRequestEnd, ExchangeID: "exchange-1", SHA256: hex.EncodeToString(empty[:])},
				{Kind: RecordProviderRequestEnd, ExchangeID: "exchange-1", SHA256: hex.EncodeToString(empty[:])},
			},
		},
		{
			name: "foreign stream kind",
			records: []Record{
				{Kind: RecordMCPStdinChunk, ExchangeID: "exchange-1"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if report, err := inspectProviderStateFixture(t, "illegal-provider", test.records); err == nil {
				t.Fatalf("InspectSession() accepted illegal transition with integrity %+v", report.Integrity)
			}
		})
	}
}

func TestInspectSession_RejectsForeignMCPRecordKind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: "mcp-kind"})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: "mcp-kind", Provider: ProviderManifestInfo{Origin: "https://api.anthropic.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.CloseDaemon(CaptureComplete); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(recorder.SessionDir(), "mcp"), 0o700); err != nil {
		t.Fatal(err)
	}
	empty := sha256.Sum256(nil)
	now := time.Now().UTC()
	records := []Record{
		{Seq: 1, Time: now, SessionID: "mcp-kind", Kind: RecordMCPStreamStart, ProcessID: 1},
		{Seq: 2, Time: now, SessionID: "mcp-kind", Kind: RecordProviderRequestStart, ExchangeID: "exchange-1"},
		{Seq: 3, Time: now, SessionID: "mcp-kind", Kind: RecordMCPStreamEnd, ProcessID: 1, CaptureStatus: CaptureComplete, InputSHA256: hex.EncodeToString(empty[:]), OutputSHA256: hex.EncodeToString(empty[:])},
	}
	writeInspectionRecordLines(t, filepath.Join(recorder.SessionDir(), "mcp", "zcp-1.jsonl"), records)
	if err := manifest.FinalizeDaemon(CaptureComplete); err != nil {
		t.Fatal(err)
	}
	if report, err := InspectSession(recorder.SessionDir()); err == nil {
		t.Fatalf("InspectSession() accepted provider record in MCP stream: %+v", report.Integrity)
	}
}

func TestInspectSession_LifecycleRejectsIllegalHierarchyTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []LifecycleMarker
	}{
		{
			name: "child starts after parent end",
			records: []LifecycleMarker{
				{Kind: LifecycleEvalRunStart, EvalRunID: "run"},
				{Kind: LifecycleEvalRunEnd, EvalRunID: "run", Status: CaptureComplete},
				{Kind: LifecycleScenarioStart, EvalRunID: "run", ScenarioRunID: "scenario"},
				{Kind: LifecycleScenarioEnd, EvalRunID: "run", ScenarioRunID: "scenario", Status: CaptureComplete},
			},
		},
		{
			name: "parent ends before child",
			records: []LifecycleMarker{
				{Kind: LifecycleEvalRunStart, EvalRunID: "run"},
				{Kind: LifecycleScenarioStart, EvalRunID: "run", ScenarioRunID: "scenario"},
				{Kind: LifecycleEvalRunEnd, EvalRunID: "run", Status: CaptureComplete},
			},
		},
		{
			name: "scenario ends before invocation",
			records: []LifecycleMarker{
				{Kind: LifecycleEvalRunStart, EvalRunID: "run"},
				{Kind: LifecycleScenarioStart, EvalRunID: "run", ScenarioRunID: "scenario"},
				{Kind: LifecycleInvocationStart, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent"},
				{Kind: LifecycleScenarioEnd, EvalRunID: "run", ScenarioRunID: "scenario", Status: CaptureComplete},
			},
		},
		{
			name: "duplicate invocation bind",
			records: []LifecycleMarker{
				{Kind: LifecycleEvalRunStart, EvalRunID: "run"},
				{Kind: LifecycleScenarioStart, EvalRunID: "run", ScenarioRunID: "scenario"},
				{Kind: LifecycleInvocationStart, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent"},
				{Kind: LifecycleInvocationBind, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent", ClaudeSessionID: "claude"},
				{Kind: LifecycleInvocationBind, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent", ClaudeSessionID: "claude"},
			},
		},
		{
			name: "duplicate invocation end",
			records: []LifecycleMarker{
				{Kind: LifecycleEvalRunStart, EvalRunID: "run"},
				{Kind: LifecycleScenarioStart, EvalRunID: "run", ScenarioRunID: "scenario"},
				{Kind: LifecycleInvocationStart, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent"},
				{Kind: LifecycleInvocationEnd, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent", Status: CaptureComplete},
				{Kind: LifecycleInvocationEnd, EvalRunID: "run", ScenarioRunID: "scenario", InvocationID: "invocation", Phase: "agent", Status: CaptureComplete},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if report, err := inspectLifecycleStateFixture(t, "illegal-lifecycle", test.records); err == nil && report.Integrity.Complete {
				t.Fatalf("InspectSession() accepted illegal lifecycle hierarchy: %+v", report.EvalRuns)
			}
		})
	}
}

func TestInspectSession_LegacyInspectionRejectsSymlinkEvidence(t *testing.T) {
	t.Parallel()

	external := makeCompleteIdentityFixture(t, "legacy-external")
	legacy := t.TempDir()
	for _, name := range []string{recordsFilename, lifecycleFilename} {
		if err := os.Symlink(filepath.Join(external, name), filepath.Join(legacy, name)); err != nil {
			t.Fatal(err)
		}
	}
	if report, err := InspectSession(legacy); err == nil {
		t.Fatalf("legacy inspection followed symlink evidence and reported valid=%v complete=%v", report.Integrity.Valid, report.Integrity.Complete)
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("legacy symlink error = %v", err)
	}
}

func TestInspectSession_LegacyInspectionRejectsSymlinkedOptionalEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		subdir     string
		symlinkDir bool
	}{
		{name: "MCP directory", subdir: "mcp", symlinkDir: true},
		{name: "MCP file", subdir: "mcp"},
		{name: "provenance directory", subdir: "provenance", symlinkDir: true},
		{name: "provenance file", subdir: "provenance"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			external := makeCompleteIdentityFixture(t, "legacy-optional-external")
			legacy := t.TempDir()
			copyInspectionFile(t, filepath.Join(external, recordsFilename), filepath.Join(legacy, recordsFilename))
			copyInspectionFile(t, filepath.Join(external, lifecycleFilename), filepath.Join(legacy, lifecycleFilename))

			outsideDir := filepath.Join(t.TempDir(), test.subdir)
			if err := os.MkdirAll(outsideDir, 0o700); err != nil {
				t.Fatal(err)
			}
			outsideFile := filepath.Join(outsideDir, "outside.jsonl")
			if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			insideDir := filepath.Join(legacy, test.subdir)
			if test.symlinkDir {
				if err := os.Symlink(outsideDir, insideDir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(insideDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outsideFile, filepath.Join(insideDir, "outside.jsonl")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := InspectSession(legacy); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("legacy optional symlink error = %v", err)
			}
		})
	}
}

func inspectProviderStateFixture(t *testing.T, captureID string, middle []Record) (*InspectionReport, error) {
	t.Helper()
	sessionDir := t.TempDir()
	now := time.Now().UTC()
	records := make([]Record, 0, len(middle)+2)
	records = append(records, Record{Time: now, SessionID: captureID, Kind: RecordSessionStart})
	records = append(records, middle...)
	records = append(records, Record{Time: now, SessionID: captureID, Kind: RecordSessionEnd, CaptureStatus: CaptureComplete})
	for index := range records {
		records[index].Seq = uint64(index + 1)
		records[index].Time = now
		records[index].SessionID = captureID
	}
	path := filepath.Join(sessionDir, recordsFilename)
	writeInspectionRecordLines(t, path, records)
	writeManifestFromSources(t, sessionDir, captureID, []manifestSource{{kind: ManifestFileProvider, relative: recordsFilename, source: path}})
	return InspectSession(sessionDir)
}

func inspectLifecycleStateFixture(t *testing.T, captureID string, middle []LifecycleMarker) (*InspectionReport, error) {
	t.Helper()
	sessionDir := makeCompleteIdentityFixture(t, captureID)
	now := time.Now().UTC()
	records := make([]LifecycleRecord, 0, len(middle)+2)
	records = append(records, LifecycleRecord{Time: now, CaptureID: captureID, LifecycleMarker: LifecycleMarker{Kind: LifecycleStreamStart, Status: CaptureRunning}})
	for _, marker := range middle {
		records = append(records, LifecycleRecord{Time: now, CaptureID: captureID, LifecycleMarker: marker})
	}
	records = append(records, LifecycleRecord{Time: now, CaptureID: captureID, LifecycleMarker: LifecycleMarker{Kind: LifecycleStreamEnd, Status: CaptureComplete}})
	data := make([]byte, 0, len(records)*256)
	for index := range records {
		records[index].Seq = uint64(index + 1)
		line, err := json.Marshal(records[index])
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(filepath.Join(sessionDir, lifecycleFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteManifestFileHash(t, sessionDir, lifecycleFilename, data)
	return InspectSession(sessionDir)
}

func writeInspectionRecordLines(t *testing.T, path string, records []Record) {
	t.Helper()
	data := make([]byte, 0, len(records)*256)
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyInspectionFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
