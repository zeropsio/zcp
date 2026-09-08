package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEvalCaptureArgs_ReportCaptureDirNotIntercepted(t *testing.T) {
	t.Parallel()

	handled, _ := runEvalWithOptionalScopedCapture([]string{"behavioral", "report", "--capture", "/some/session/dir", "--eval", "run-1", "--scenario", "scenario-1"})
	if handled {
		t.Fatalf("runEvalWithOptionalScopedCapture() handled=true for behavioral report, want false (falls through to runEval)")
	}
}

func TestParseEvalCaptureArgs_RunCaptureRawStillIntercepted(t *testing.T) {
	t.Parallel()

	_, requested, err := parseEvalCaptureArgs([]string{"behavioral", "run", "--id", "x", "--capture", "raw"})
	if err != nil {
		t.Fatalf("parseEvalCaptureArgs() error = %v", err)
	}
	if !requested {
		t.Fatalf("parseEvalCaptureArgs() requested = false, want true for --capture raw")
	}
}

func TestRunBehavioralReport_MissingFlags_ExitCode2(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runBehavioralReport([]string{"--capture", "/tmp/x"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runBehavioralReport() exit = %d, want 2\nstderr: %s", code, stderr.String())
	}
}

func TestRunBehavioralReport_UnresolvableScope_ExitCode1(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runBehavioralReport([]string{"--capture", dir, "--eval", "run-1", "--scenario", "scenario-1"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runBehavioralReport() exit = %d, want 1\nstderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("runBehavioralReport() stdout = %q, want empty (diagnostic-only)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "eval behavioral report:") {
		t.Fatalf("stderr missing diagnostic prefix: %s", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_Report_EndToEnd_TextAndJSON
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_Report_EndToEnd_TextAndJSON(t *testing.T) {
	h := newCLIHarness(t, "FAILED")
	h.server.hideAppForFirstDirectCall()
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-report-e2e", "required")

	runArgs := append([]string{"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw"}, cliBindingArgs(t)...)
	exitCode, stderr := h.run(t, nil, runArgs...)
	if exitCode != 1 {
		t.Fatalf("run exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
	}

	sessionDir := findCaptureSessionDir(t, h.home)
	evalRunID := findCaptureEvalRunID(t, sessionDir, "cli-report-e2e")

	textCode, textOut := h.run(t, nil, "eval", "behavioral", "report", "--capture", sessionDir, "--eval", evalRunID, "--scenario", "cli-report-e2e")
	jsonCode, jsonOut := h.run(t, nil, "eval", "behavioral", "report", "--capture", sessionDir, "--eval", evalRunID, "--scenario", "cli-report-e2e", "--format", "json")

	if textCode != 0 {
		t.Fatalf("text report exit = %d, want 0 (failed task in a complete window is still a successful report)\noutput:\n%s", textCode, textOut)
	}
	for _, want := range []string{"Result:", "Checks:", "Invocations:", "MCP:", "Findings:", "Gaps:"} {
		if !strings.Contains(textOut, want) {
			t.Errorf("text report missing section %q\noutput:\n%s", want, textOut)
		}
	}
	if jsonCode != 0 {
		t.Fatalf("json report exit = %d, want 0\noutput:\n%s", jsonCode, jsonOut)
	}
	var decoded struct {
		Result struct {
			Task struct {
				Result string `json:"result"`
			} `json:"task"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("parse json report: %v\noutput:\n%s", err, jsonOut)
	}
	if decoded.Result.Task.Result != "failed" {
		t.Errorf("json report result.task.result = %q, want failed", decoded.Result.Task.Result)
	}
	if !strings.Contains(textOut, "result=failed") {
		t.Errorf("text report missing result=failed\noutput:\n%s", textOut)
	}
}

// findCaptureEvalRunID discovers the timestamp-generated eval run id under
// <sessionDir>/eval/*/<scenarioID> — the runner chooses it, so tests cannot
// predict it in advance.
func findCaptureEvalRunID(t *testing.T, sessionDir, scenarioID string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(sessionDir, "eval", "*", scenarioID))
	if err != nil {
		t.Fatalf("glob eval run dirs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("eval run dirs for scenario %s: got %d, want 1: %v", scenarioID, len(matches), matches)
	}
	return filepath.Base(filepath.Dir(matches[0]))
}
