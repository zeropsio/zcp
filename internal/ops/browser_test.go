// Tests for: BrowserBatch — canonical agent-browser batch wrapper.
//
// These tests lock the lifecycle contract the recipe workflow depends on:
//   - Tool auto-prepends ["open", url] and auto-appends ["errors"],
//     ["console"], ["close"] — agent never manages lifecycle directly.
//   - Any "open" or "close" the agent accidentally passes in Commands is
//     stripped so we never double-open or double-close the daemon.
//   - stdin is valid JSON of the fully-built batch, fed to
//     `agent-browser batch --json`.
//   - Fork-exhaustion signatures in stderr/stdout trigger auto-recovery
//     (pkill) and surface a clear message instead of a raw error.
//   - Context-deadline timeout also triggers recovery.
//   - JSON output is parsed, with errorsOutput/consoleOutput extracted
//     from the canonical penultimate steps.
//
// These tests do NOT run in parallel. BrowserBatch uses a package-level
// browserRun global overridden via OverrideBrowserRunnerForTest, and the
// underlying browserMu serializes all calls — running tests in parallel
// would either race the override or one test would acquire the mutex and
// starve others. Both problems vanish by keeping the suite sequential.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeBrowserRunner captures calls and returns scripted results.
type fakeBrowserRunner struct {
	lookPathErr error

	lastStdin   string
	lastTimeout time.Duration
	runStdout   string
	runStderr   string
	runErr      error

	recoverCalls int
}

func (f *fakeBrowserRunner) LookPath() (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return "/usr/local/bin/agent-browser", nil
}

func (f *fakeBrowserRunner) Run(_ context.Context, stdin string, timeout time.Duration) (string, string, error) {
	f.lastStdin = stdin
	f.lastTimeout = timeout
	return f.runStdout, f.runStderr, f.runErr
}

func (f *fakeBrowserRunner) RecoverFork(_ context.Context) {
	f.recoverCalls++
}

// parseStdinBatch parses the JSON array the tool sent to agent-browser.
func parseStdinBatch(t *testing.T, stdin string) [][]string {
	t.Helper()
	var batch [][]string
	if err := json.Unmarshal([]byte(stdin), &batch); err != nil {
		t.Fatalf("stdin is not a JSON [][]string: %v\nstdin: %s", err, stdin)
	}
	return batch
}

// makeStdout builds a valid agent-browser --json output for a given batch.
// Uses map[string]any instead of a struct-with-RawMessage — errchkjson
// treats RawMessage as unsafe, and this is a test helper, so going through
// plain maps keeps the marshal total.
func makeStdout(t *testing.T, batch [][]string) string {
	t.Helper()
	out := make([]map[string]any, 0, len(batch))
	for _, cmd := range batch {
		var res map[string]any
		switch cmd[0] {
		case "errors":
			res = map[string]any{"errors": []any{}}
		case "console":
			res = map[string]any{"logs": []any{}}
		default:
			res = map[string]any{"ok": true}
		}
		out = append(out, map[string]any{
			"command": cmd,
			"success": true,
			"result":  res,
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("makeStdout marshal: %v", err)
	}
	return string(b)
}

func TestBrowserBatch_URLRequired(t *testing.T) {
	fake := &fakeBrowserRunner{}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "url") && !strings.Contains(err.Error(), "URL") {
		t.Errorf("error should mention url: %v", err)
	}
}

func TestBrowserBatch_AgentBrowserNotFound(t *testing.T) {
	fake := &fakeBrowserRunner{lookPathErr: errors.New("exec: \"agent-browser\": executable file not found in $PATH")}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when agent-browser missing")
	}
	if !strings.Contains(err.Error(), "agent-browser") {
		t.Errorf("error should mention agent-browser: %v", err)
	}
}

func TestBrowserBatch_BuildsCanonicalShape(t *testing.T) {
	batch := [][]string{
		{"open", "https://example.com/app"},
		{"snapshot", "-i", "-c"},
		{"click", "@e1"},
		{"errors"},
		{"console"},
		{"close"},
	}
	fake := &fakeBrowserRunner{runStdout: makeStdout(t, batch)}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com/app",
		Commands: [][]string{
			{"snapshot", "-i", "-c"},
			{"click", "@e1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := parseStdinBatch(t, fake.lastStdin)
	if len(got) != 6 {
		t.Fatalf("expected 6 commands in batch, got %d: %v", len(got), got)
	}
	if got[0][0] != "open" || got[0][1] != "https://example.com/app" {
		t.Errorf("first command must be [\"open\", url], got: %v", got[0])
	}
	if got[1][0] != "snapshot" {
		t.Errorf("second command should be agent's snapshot, got: %v", got[1])
	}
	if got[2][0] != "click" {
		t.Errorf("third command should be agent's click, got: %v", got[2])
	}
	if got[3][0] != "errors" {
		t.Errorf("fourth (auto) command should be errors, got: %v", got[3])
	}
	if got[4][0] != "console" {
		t.Errorf("fifth (auto) command should be console, got: %v", got[4])
	}
	if got[5][0] != "close" {
		t.Errorf("last (auto) command must be close, got: %v", got[5])
	}

	if result.URL != "https://example.com/app" {
		t.Errorf("result.URL = %q", result.URL)
	}
	if len(result.Steps) != 6 {
		t.Errorf("expected 6 parsed steps, got %d", len(result.Steps))
	}
	if len(result.ErrorsOutput) == 0 {
		t.Errorf("ErrorsOutput should be populated from final [errors] step")
	}
	if len(result.ConsoleOutput) == 0 {
		t.Errorf("ConsoleOutput should be populated from final [console] step")
	}
	if result.ForkRecoveryAttempted {
		t.Errorf("ForkRecoveryAttempted should be false on success")
	}
}

func TestBrowserBatch_StripsAgentOpenAndClose(t *testing.T) {
	// Agent erroneously includes open/close in their Commands. Tool must
	// strip both so the canonical wrappers don't double-apply.
	fake := &fakeBrowserRunner{runStdout: `[]`}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com",
		Commands: [][]string{
			{"open", "https://example.com"}, // duplicate open
			{"snapshot"},
			{"close"}, // duplicate close
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := parseStdinBatch(t, fake.lastStdin)
	openCount, closeCount := 0, 0
	for _, cmd := range got {
		if cmd[0] == "open" {
			openCount++
		}
		if cmd[0] == "close" {
			closeCount++
		}
	}
	if openCount != 1 {
		t.Errorf("expected exactly 1 open, got %d: %v", openCount, got)
	}
	if closeCount != 1 {
		t.Errorf("expected exactly 1 close, got %d: %v", closeCount, got)
	}
	// Close must still be last.
	if got[len(got)-1][0] != "close" {
		t.Errorf("close must be last element, got: %v", got[len(got)-1])
	}
}

func TestBrowserBatch_ForkExhaustionTriggersRecovery(t *testing.T) {
	fake := &fakeBrowserRunner{
		runStderr: "fork failed: resource temporarily unavailable\n",
		runErr:    errors.New("exit status 1"),
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("fork exhaustion should not error — surface in result: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("expected ForkRecoveryAttempted=true")
	}
	if fake.recoverCalls != 1 {
		t.Errorf("expected 1 recovery call, got %d", fake.recoverCalls)
	}
	if result.Message == "" {
		t.Error("expected a recovery message")
	}
	if !strings.Contains(strings.ToLower(result.Message), "fork") {
		t.Errorf("recovery message should mention fork: %q", result.Message)
	}
}

func TestBrowserBatch_PthreadCreateAlsoTriggersRecovery(t *testing.T) {
	fake := &fakeBrowserRunner{
		runStderr: "pthread_create: Resource temporarily unavailable\n",
		runErr:    errors.New("exit status 1"),
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("pthread_create should trigger recovery")
	}
}

func TestBrowserBatch_ContextDeadlineTriggersRecovery(t *testing.T) {
	fake := &fakeBrowserRunner{runErr: context.DeadlineExceeded}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("deadline exceeded should trigger recovery")
	}
	if !strings.Contains(strings.ToLower(result.Message), "timed out") {
		t.Errorf("timeout message should say 'timed out': %q", result.Message)
	}
}

func TestBrowserBatch_TimeoutDefaultAndCap(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  time.Duration
	}{
		{"default", 0, 120 * time.Second},
		{"explicit", 60, 60 * time.Second},
		{"cap", 99999, 300 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBrowserRunner{runStdout: `[]`}
			defer OverrideBrowserRunnerForTest(fake)()

			_, err := BrowserBatch(context.Background(), BrowserBatchInput{
				URL:            "https://example.com",
				TimeoutSeconds: tt.input,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fake.lastTimeout != tt.want {
				t.Errorf("timeout = %v, want %v", fake.lastTimeout, tt.want)
			}
		})
	}
}

func TestBrowserBatch_UnparseableOutput(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: "not-json-at-all"}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Message == "" {
		t.Error("unparseable output should surface in Message")
	}
	if !strings.Contains(result.Message, "parse") {
		t.Errorf("message should mention parse: %q", result.Message)
	}
}
