package eval

import (
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

// behavioralReportFixture builds a minimal finalized capture window with one
// eval run/scenario and, optionally, provider exchanges + lifecycle
// invocation markers, plus eval artifacts (meta.json, verification.json)
// written directly (independent-oracle literals, not the report's own
// rendering). This mirrors internal/capture/inspect_test.go's fixture
// builder but stays in package eval since BuildBehavioralReport is the
// function under test.
type behavioralReportFixtureConfig struct {
	evalRunID     string
	scenarioRunID string
	invocations   []behavioralReportInvocationConfig
	metaResult    *TaskOutcome
	processIDs    []ProcessIdentity
	binding       *ExecutionBindingRecord
	writeMeta     bool
	writeVerify   bool
	checks        []RequiredCheck
	// terminal is the capture terminal status applied uniformly to the
	// recorder, lifecycle recorder, and manifest. Defaults to
	// capture.CaptureComplete when empty.
	terminal string
}

type behavioralReportInvocationConfig struct {
	invocationID string
	phase        string
	status       string
	exchangeIDs  []string
	// bindExchange, when true, records one provider exchange per exchangeID
	// with the given usage; when false, ExchangeIDs stays populated (bound
	// by lifecycle) but no provider exchange exists, testing "unattributed".
	recordUsage bool
	usage       capture.ModelContextInspection
}

func recordFixtureExchange(t *testing.T, recorder *capture.Recorder, exchangeID string, inputTokens, outputTokens int64, observed bool) {
	t.Helper()
	usageJSON := "{}"
	if observed {
		usageJSON = `{"input_tokens":` + itoa(inputTokens) + `,"output_tokens":` + itoa(outputTokens) + `}`
	}
	requestBody := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hi"}]}`)
	responseBody := []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":" + usageJSON + "}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	requestHash := sha256.Sum256(requestBody)
	responseHash := sha256.Sum256(responseBody)
	records := []capture.Record{
		{Kind: capture.RecordProviderRequestStart, ExchangeID: exchangeID, Direction: "client_to_provider", Method: http.MethodPost, Path: "/v1/messages", Headers: http.Header{"Content-Type": []string{"application/json"}}},
		{Kind: capture.RecordProviderRequestBody, ExchangeID: exchangeID, Direction: "client_to_provider", BodyBase64: base64.StdEncoding.EncodeToString(requestBody), BodyBytes: int64(len(requestBody))},
		{Kind: capture.RecordProviderRequestEnd, ExchangeID: exchangeID, Direction: "client_to_provider", BodyBytes: int64(len(requestBody)), SHA256: hex.EncodeToString(requestHash[:])},
		{Kind: capture.RecordProviderResponseStart, ExchangeID: exchangeID, Direction: "provider_to_client", StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"text/event-stream"}}},
		{Kind: capture.RecordProviderResponseBody, ExchangeID: exchangeID, Direction: "provider_to_client", BodyBase64: base64.StdEncoding.EncodeToString(responseBody), BodyBytes: int64(len(responseBody))},
		{Kind: capture.RecordProviderResponseEnd, ExchangeID: exchangeID, Direction: "provider_to_client", BodyBytes: int64(len(responseBody)), SHA256: hex.EncodeToString(responseHash[:])},
	}
	for _, record := range records {
		if err := recorder.Record(record); err != nil {
			t.Fatalf("Recorder.Record(%s) error = %v", record.Kind, err)
		}
	}
}

func itoa(v int64) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "0"
	}
	return string(data)
}

func writeBehavioralReportFixture(t *testing.T, cfg behavioralReportFixtureConfig) string {
	t.Helper()
	root := t.TempDir()
	recorder, err := capture.NewRecorder(capture.RecorderConfig{RootDir: root, SessionID: "report-fixture", Label: "report-fixture"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	manifest, err := capture.NewSessionManifest(capture.SessionManifestConfig{
		SessionDir: recorder.SessionDir(),
		SessionID:  "report-fixture",
		Command:    []string{"fixture"},
	})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	lifecycle, err := capture.NewLifecycleRecorder(recorder.SessionDir(), "report-fixture")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}

	terminal := cfg.terminal
	if terminal == "" {
		terminal = capture.CaptureComplete
	}
	mark := func(marker capture.LifecycleMarker) {
		if _, err := lifecycle.Mark(marker); err != nil {
			t.Fatalf("lifecycle.Mark(%s) error = %v", marker.Kind, err)
		}
	}
	mark(capture.LifecycleMarker{Kind: capture.LifecycleEvalRunStart, EvalRunID: cfg.evalRunID})
	mark(capture.LifecycleMarker{Kind: capture.LifecycleScenarioStart, EvalRunID: cfg.evalRunID, ScenarioRunID: cfg.scenarioRunID})
	for _, inv := range cfg.invocations {
		mark(capture.LifecycleMarker{Kind: capture.LifecycleInvocationStart, EvalRunID: cfg.evalRunID, ScenarioRunID: cfg.scenarioRunID, InvocationID: inv.invocationID, Phase: inv.phase})
		for _, exchangeID := range inv.exchangeIDs {
			if inv.recordUsage {
				recordFixtureExchange(t, recorder, exchangeID, inv.usage.InputTokens, inv.usage.OutputTokens, inv.usage.InputTokensObserved)
			}
		}
		mark(capture.LifecycleMarker{Kind: capture.LifecycleInvocationEnd, EvalRunID: cfg.evalRunID, ScenarioRunID: cfg.scenarioRunID, InvocationID: inv.invocationID, Phase: inv.phase, Status: inv.status})
	}
	mark(capture.LifecycleMarker{Kind: capture.LifecycleScenarioEnd, EvalRunID: cfg.evalRunID, ScenarioRunID: cfg.scenarioRunID, Status: terminal})
	mark(capture.LifecycleMarker{Kind: capture.LifecycleEvalRunEnd, EvalRunID: cfg.evalRunID, Status: terminal})
	if err := lifecycle.Close(terminal); err != nil {
		t.Fatalf("LifecycleRecorder.Close() error = %v", err)
	}
	if err := recorder.Close(terminal, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}

	if cfg.writeMeta {
		evalDir := filepath.Join(recorder.SessionDir(), "eval", cfg.evalRunID, cfg.scenarioRunID)
		if err := os.MkdirAll(evalDir, 0o700); err != nil {
			t.Fatalf("mkdir eval dir: %v", err)
		}
		result := BehavioralResult{
			ScenarioID:      cfg.scenarioRunID,
			Task:            cfg.metaResult,
			ProcessIdentity: cfg.processIDs,
			Binding:         cfg.binding,
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatalf("marshal meta.json: %v", err)
		}
		if err := os.WriteFile(filepath.Join(evalDir, "meta.json"), data, 0o600); err != nil {
			t.Fatalf("write meta.json: %v", err)
		}
		if cfg.writeVerify {
			doc := VerificationDocument{
				FormatVersion: VerificationDocumentFormat2,
				Mode:          "required",
				Result:        aggregateTaskResult(cfg.checks),
				FrozenAt:      time.Now().UTC(),
				Checks:        cfg.checks,
			}
			verifyData, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatalf("marshal verification.json: %v", err)
			}
			if err := os.WriteFile(filepath.Join(evalDir, "verification.json"), verifyData, 0o600); err != nil {
				t.Fatalf("write verification.json: %v", err)
			}
		}
	}

	if err := manifest.Finalize(terminal, 0); err != nil {
		t.Fatalf("manifest.Finalize() error = %v", err)
	}
	return recorder.SessionDir()
}

func TestBehavioralReport_ForeignOrAmbiguousScope_DiagnosticOnly(t *testing.T) {
	t.Parallel()

	sessionDir := writeBehavioralReportFixture(t, behavioralReportFixtureConfig{
		evalRunID: "run-1", scenarioRunID: "scenario-1", writeMeta: true,
		metaResult: &TaskOutcome{Mode: "required", Result: CheckPassed, FrozenAt: time.Now().UTC()},
	})

	if _, _, err := BuildBehavioralReport(sessionDir, "run-1", "wrong-scenario"); err == nil {
		t.Fatal("BuildBehavioralReport() with wrong scenario succeeded, want scope error")
	} else if !strings.Contains(err.Error(), "scope:") {
		t.Fatalf("BuildBehavioralReport() error = %v, want scope: prefix", err)
	}

	if _, _, err := BuildBehavioralReport(sessionDir, "wrong-run", "scenario-1"); err == nil {
		t.Fatal("BuildBehavioralReport() with wrong eval run succeeded, want scope error")
	}
}

func TestBehavioralReport_MissingUsageNotZero_AndPhasesSeparated(t *testing.T) {
	t.Parallel()

	sessionDir := writeBehavioralReportFixture(t, behavioralReportFixtureConfig{
		evalRunID: "run-1", scenarioRunID: "scenario-1", writeMeta: true,
		metaResult: &TaskOutcome{Mode: "observe", Result: CheckPassed, FrozenAt: time.Now().UTC()},
		invocations: []behavioralReportInvocationConfig{
			{
				invocationID: "scenario-1/agent.initial", phase: "agent.initial", status: "complete",
				exchangeIDs: []string{"exchange-a"}, recordUsage: true,
				usage: capture.ModelContextInspection{InputTokens: 10, OutputTokens: 5, InputTokensObserved: false},
			},
			{
				invocationID: "scenario-1/retrospective", phase: "retrospective", status: "complete",
				exchangeIDs: []string{}, recordUsage: false,
			},
		},
	})

	report, _, err := BuildBehavioralReport(sessionDir, "run-1", "scenario-1")
	if err != nil {
		t.Fatalf("BuildBehavioralReport() error = %v", err)
	}
	if len(report.Invocations) != 2 {
		t.Fatalf("Invocations = %d, want 2", len(report.Invocations))
	}
	agentRow := report.Invocations[0]
	if agentRow.Usage.Input.Observed {
		t.Errorf("agent invocation usage.input.observed = true, want false (unobserved field must print unknown)")
	}
	retroRow := report.Invocations[1]
	if !retroRow.Unattributed {
		t.Errorf("retrospective invocation with no joined exchanges: Unattributed = false, want true")
	}
	if report.Totals.PhaseMapping["agent.initial"] != "agent" {
		t.Errorf("phase mapping[agent.initial] = %q, want agent", report.Totals.PhaseMapping["agent.initial"])
	}
	if report.Totals.PhaseMapping["retrospective"] != "overhead" {
		t.Errorf("phase mapping[retrospective] = %q, want overhead", report.Totals.PhaseMapping["retrospective"])
	}
}

func TestBehavioralReport_ProcessIdentity_CopiedCountsOnly(t *testing.T) {
	t.Parallel()

	sessionDir := writeBehavioralReportFixture(t, behavioralReportFixtureConfig{
		evalRunID: "run-1", scenarioRunID: "scenario-1", writeMeta: true,
		metaResult: &TaskOutcome{Mode: "required", Result: CheckPassed, FrozenAt: time.Now().UTC()},
		binding:    &ExecutionBindingRecord{ProjectID: "proj-a"},
		processIDs: []ProcessIdentity{
			{PID: 100, MatchesCandidate: true, ProjectID: "proj-a", Classification: "match"},
			{PID: 200, MatchesCandidate: false, ProjectID: "proj-b", Classification: "mismatch"},
		},
	})

	report, _, err := BuildBehavioralReport(sessionDir, "run-1", "scenario-1")
	if err != nil {
		t.Fatalf("BuildBehavioralReport() error = %v", err)
	}
	if report.Result.ProcessIdentity.Observations != 2 || report.Result.ProcessIdentity.MatchesCandidate != 1 || report.Result.ProcessIdentity.ProjectMismatches != 1 {
		t.Fatalf("ProcessIdentity summary = %+v", report.Result.ProcessIdentity)
	}
	rendered := RenderBehavioralReportText(report)
	for _, verdict := range []string{" ok\n", " blocked\n"} {
		if strings.Contains(rendered, "Process identity"+verdict) {
			t.Errorf("process identity section carries a verdict word; report has no verdict authority:\n%s", rendered)
		}
	}
}

func TestBehavioralReport_ValidButIncomplete_FullReportExit1(t *testing.T) {
	t.Parallel()

	sessionDir := writeBehavioralReportFixture(t, behavioralReportFixtureConfig{
		evalRunID: "run-1", scenarioRunID: "scenario-1", writeMeta: true,
		metaResult: &TaskOutcome{Mode: "observe", Result: CheckPassed, FrozenAt: time.Now().UTC()},
		terminal:   capture.CapturePartial,
	})

	report, inspection, err := BuildBehavioralReport(sessionDir, "run-1", "scenario-1")
	if err != nil {
		t.Fatalf("BuildBehavioralReport() error = %v, want a report even though incomplete", err)
	}
	if !inspection.Integrity.Valid {
		t.Fatalf("Integrity.Valid = false, want true (hash-valid prefix)")
	}
	if inspection.Integrity.Complete {
		t.Fatalf("Integrity.Complete = true, want false (this test rewrote status to partial)")
	}
	found := false
	for _, gap := range report.Gaps {
		if gap == "incomplete capture" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Gaps = %v, want \"incomplete capture\"", report.Gaps)
	}
}
