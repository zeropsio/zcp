// Tests for: BrowserBatch — canonical agent-browser batch wrapper.
//
// These tests lock the lifecycle contract the recipe workflow depends on:
//   - Tool auto-prepends ["open", url] and auto-appends ["errors"],
//     ["console"], ["close"] — agent never manages lifecycle directly.
//   - Any "open" or "close" the agent accidentally passes in Commands is
//     stripped so we never double-open or double-close the daemon.
//   - stdin is valid JSON of the fully-built batch, fed to
//     `agent-browser batch --json`.
//   - Fork-exhaustion signatures in stderr (only) trigger auto-recovery
//     (pkill) and surface a clear message instead of a raw error.
//   - Context-deadline timeout also triggers recovery.
//   - Any other non-zero exit with no parseable output also triggers
//     recovery — daemon crashes must reap leaked Chrome helpers.
//   - On non-zero exit, ErrorsOutput/ConsoleOutput are NOT populated
//     from partial steps — those fields must only reflect a clean walk.
//   - JSON output is parsed, with errorsOutput/consoleOutput extracted
//     from the canonical penultimate steps on successful runs.
//   - Calls are serialized through browserMu — two concurrent callers
//     cannot execute Run at the same instant.
//   - A ctx-cancelled caller returns immediately without piling up
//     behind a stuck predecessor.
//
// These tests do NOT run in parallel. BrowserBatch uses a package-level
// browserRun global overridden via OverrideBrowserRunnerForTest, and the
// underlying browserMu serializes all calls — running tests in parallel
// would either race the override or one test would acquire the mutex and
// starve others. Both problems vanish by keeping the suite sequential.
//
// All tests drop postRecoveryGrace to zero via TestMain so the recovery
// paths don't pay the 2-second kernel-reap sleep on each assertion.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestMain shaves the 2-second post-recovery grace down to zero so
// recovery-triggering tests don't serialize the suite through multiple
// kernel-reap sleeps. Each test still exercises the real recovery path;
// we just don't pay for a sleep we can't observe.
func TestMain(m *testing.M) {
	restore := OverridePostRecoveryGraceForTest(0)
	code := m.Run()
	restore()
	os.Exit(code)
}

// fakeBrowserRunner captures calls and returns scripted results.
type fakeBrowserRunner struct {
	lookPathErr error

	lastStdin     string
	lastTimeout   time.Duration
	runStdout     string
	runStderr     string
	runTruncated  bool
	runErr        error
	recoverCalls  int
	recoverCallMu sync.Mutex
}

func (f *fakeBrowserRunner) LookPath() (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return "/usr/local/bin/agent-browser", nil
}

func (f *fakeBrowserRunner) Run(_ context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	f.lastStdin = stdin
	f.lastTimeout = timeout
	return f.runStdout, f.runStderr, f.runTruncated, f.runErr
}

func (f *fakeBrowserRunner) RecoverFork(_ context.Context) {
	f.recoverCallMu.Lock()
	f.recoverCalls++
	f.recoverCallMu.Unlock()
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

// TestBrowserBatch_ForkSignatureOnlyInStdoutNoRecovery guards the stderr-only
// detection policy: agent-browser's own JSON stdout may legitimately contain
// text like "resource temporarily unavailable" (page title, console log from
// the browsed site) and MUST NOT spuriously trigger pkill recovery.
func TestBrowserBatch_ForkSignatureOnlyInStdoutNoRecovery(t *testing.T) {
	// A tiny valid JSON array with a result containing the matching text.
	stdoutWithMatch := `[{"command":["open","https://example.com"],"success":true,"result":{"title":"Resource temporarily unavailable - my-app"}},{"command":["errors"],"success":true,"result":{"errors":[]}},{"command":["console"],"success":true,"result":{"logs":[]}},{"command":["close"],"success":true,"result":{}}]`
	fake := &fakeBrowserRunner{
		runStdout: stdoutWithMatch,
		runStderr: "", // clean stderr — the only place we check
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ForkRecoveryAttempted {
		t.Error("fork signature in stdout must NOT trigger recovery — stderr is the only trusted source")
	}
	if fake.recoverCalls != 0 {
		t.Errorf("expected 0 recovery calls, got %d", fake.recoverCalls)
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

// TestBrowserBatch_UnknownCrashTriggersRecovery guards the new broadened
// recovery policy: a non-zero exit with no parseable structured output
// indicates the daemon crashed, so Chrome helpers must be reaped even
// though the stderr signature is unknown.
func TestBrowserBatch_UnknownCrashTriggersRecovery(t *testing.T) {
	fake := &fakeBrowserRunner{
		runStderr: "panic: runtime error: invalid memory address or nil pointer dereference\n",
		runStdout: "", // daemon died before emitting any JSON
		runErr:    errors.New("exit status 2"),
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("daemon crash with no stdout should trigger recovery")
	}
	if fake.recoverCalls != 1 {
		t.Errorf("expected 1 recovery call, got %d", fake.recoverCalls)
	}
	if !strings.Contains(result.Message, "exit status 2") {
		t.Errorf("message should preserve exit status: %q", result.Message)
	}
}

// TestBrowserBatch_PartialJSONWithExitErrorKeepsBothMessages guards the
// Message-ordering fix: a non-zero exit + unparseable stdout must result
// in a Message that contains BOTH the exit error and the parse error.
func TestBrowserBatch_PartialJSONWithExitErrorKeepsBothMessages(t *testing.T) {
	fake := &fakeBrowserRunner{
		runStdout: "not-json",
		runStderr: "something bad happened\n",
		runErr:    errors.New("exit status 1"),
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Message, "exit status 1") {
		t.Errorf("Message should contain exit error: %q", result.Message)
	}
	if !strings.Contains(result.Message, "parse") {
		t.Errorf("Message should also contain parse error: %q", result.Message)
	}
	if !strings.Contains(result.Message, "stderr: something bad happened") {
		t.Errorf("Message should preserve stderr context: %q", result.Message)
	}
}

// TestBrowserBatch_ErrorsOutputNotPopulatedOnFailedRun guards the "partial
// walk must not look like a successful one" invariant: on non-zero exit,
// ErrorsOutput/ConsoleOutput must remain empty even if the last 3 steps
// happen to be the canonical errors/console/close sequence.
func TestBrowserBatch_ErrorsOutputNotPopulatedOnFailedRun(t *testing.T) {
	// Full valid JSON with canonical tail, but runErr is set.
	batch := [][]string{
		{"open", "https://example.com"},
		{"errors"},
		{"console"},
		{"close"},
	}
	fake := &fakeBrowserRunner{
		runStdout: makeStdout(t, batch),
		runErr:    errors.New("exit status 1"),
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ErrorsOutput) != 0 {
		t.Errorf("ErrorsOutput must NOT be populated on non-zero exit, got: %s", result.ErrorsOutput)
	}
	if len(result.ConsoleOutput) != 0 {
		t.Errorf("ConsoleOutput must NOT be populated on non-zero exit, got: %s", result.ConsoleOutput)
	}
	if len(result.Steps) == 0 {
		t.Error("Steps should still be preserved for diagnosis")
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

func TestBrowserBatch_OutputTruncationFlag(t *testing.T) {
	fake := &fakeBrowserRunner{
		runStdout:    `[]`,
		runTruncated: true,
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OutputTruncated {
		t.Error("OutputTruncated should propagate from runner to result")
	}
}

// TestCapBufferTruncates verifies the output-cap buffer stops accepting
// bytes past its limit and flips the truncated flag. A runaway console-log
// flood must not OOM the zcp process.
func TestCapBufferTruncates(t *testing.T) {
	c := &capBuffer{cap: 10}
	n, err := c.Write([]byte("1234567"))
	if err != nil || n != 7 {
		t.Fatalf("first write: n=%d err=%v", n, err)
	}
	if c.truncated {
		t.Error("should not be truncated yet")
	}
	// Spill past the cap in one write.
	n, err = c.Write([]byte("89012345"))
	if err != nil || n != 8 {
		t.Fatalf("second write: n=%d err=%v", n, err)
	}
	if !c.truncated {
		t.Error("should be truncated after exceeding cap")
	}
	if got := c.String(); got != "1234567890" {
		t.Errorf("cap should hold first 10 bytes, got %q", got)
	}
	// Further writes are fully dropped.
	n, _ = c.Write([]byte("more"))
	if n != 4 {
		t.Errorf("writes past cap should report full length, got %d", n)
	}
	if got := c.String(); got != "1234567890" {
		t.Errorf("content should stay at cap, got %q", got)
	}
}

// TestBrowserBatch_SerializesCalls spawns several concurrent callers and
// asserts they execute sequentially inside Run. This is the load-bearing
// safety property: one agent-browser session at a time across the process.
// The test uses a runner that busy-waits in Run to make overlap detectable.
func TestBrowserBatch_SerializesCalls(t *testing.T) {
	const concurrency = 5

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	runner := &serializationRunner{
		onRun: func() {
			cur := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				oldMax := maxInFlight.Load()
				if cur <= oldMax || maxInFlight.CompareAndSwap(oldMax, cur) {
					break
				}
			}
			// Hold the "session" briefly to make any overlap visible.
			time.Sleep(5 * time.Millisecond)
		},
	}
	defer OverrideBrowserRunnerForTest(runner)()

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
			if err != nil {
				t.Errorf("concurrent call errored: %v", err)
			}
		})
	}
	wg.Wait()

	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("max concurrent Run invocations = %d, want 1 — browser calls must be serialized", got)
	}
}

// serializationRunner is a runner that calls onRun inside Run so tests
// can observe concurrency.
type serializationRunner struct {
	onRun func()
}

func (*serializationRunner) LookPath() (string, error) { return "/fake/agent-browser", nil }

func (s *serializationRunner) Run(_ context.Context, _ string, _ time.Duration) (string, string, bool, error) {
	s.onRun()
	return `[]`, "", false, nil
}

func (*serializationRunner) RecoverFork(_ context.Context) {}

// TestRecoverFork_ReadsPidfileAndKillsProcessGroup is the Cx-BROWSER-
// RECOVERY-COMPLETE RED→GREEN guard for the pidfile path. Port from the
// v27 archive: RecoverFork must read the daemon pidfile, issue a
// process-group SIGKILL (negative pid) to reap Chrome + every helper
// the daemon forked, kill the daemon itself, then remove the stale
// pidfile + socket files so the next CLI invocation spawns a fresh
// daemon instead of attaching to a zombie.
func TestRecoverFork_ReadsPidfileAndKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidfile := dir + "/default.pid"
	if err := os.WriteFile(pidfile, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Stage an empty socket file so removeFile has something to remove.
	socketPath := dir + "/default.sock"
	if err := os.WriteFile(socketPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	var killArgs []struct {
		pid int
		sig syscall.Signal
	}
	var removedPaths []string
	var pkillCalls [][]string

	restore := OverrideBrowserRecoveryOpsForTest(browserRecoveryOps{
		pidfilePath: func() (string, string, string, error) {
			return pidfile, dir, "default", nil
		},
		readFile:   os.ReadFile,
		removeFile: func(p string) error { removedPaths = append(removedPaths, p); return os.Remove(p) },
		kill: func(pid int, sig syscall.Signal) error {
			killArgs = append(killArgs, struct {
				pid int
				sig syscall.Signal
			}{pid, sig})
			return nil
		},
		pkillRun: func(_ context.Context, args ...string) error {
			pkillCalls = append(pkillCalls, append([]string(nil), args...))
			return nil
		},
	})
	defer restore()

	execBrowserRunner{}.RecoverFork(context.Background())

	// Process-group kill (negative PID) + daemon kill, both SIGKILL.
	if len(killArgs) != 2 {
		t.Fatalf("expected 2 kill calls (group + daemon), got %d: %+v", len(killArgs), killArgs)
	}
	if killArgs[0].pid != -12345 || killArgs[0].sig != killSignal {
		t.Errorf("first kill must be process-group SIGKILL for -12345, got pid=%d sig=%v", killArgs[0].pid, killArgs[0].sig)
	}
	if killArgs[1].pid != 12345 || killArgs[1].sig != killSignal {
		t.Errorf("second kill must be daemon SIGKILL for 12345, got pid=%d sig=%v", killArgs[1].pid, killArgs[1].sig)
	}

	// Pidfile + both socket candidates must be removed.
	want := []string{pidfile, dir + "/default.sock", dir + "/agent-browser.default.sock"}
	for _, w := range want {
		if !slices.Contains(removedPaths, w) {
			t.Errorf("expected removeFile to be called for %q; got %v", w, removedPaths)
		}
	}
}

// TestRecoverFork_PkillExactFallback pins attempt 2 of the recovery
// path: the legacy `pkill -9 -f agent-browser-` invocation stays AND
// five new `pkill -9 --exact <name>` invocations fire for Chrome binary
// variants. `--exact` is critical — matching argv[0] only means
// code-server's `--no-chrome` CLI flag can never be matched (v27
// incident), and every real Chrome process is reaped regardless of its
// absolute path.
func TestRecoverFork_PkillExactFallback(t *testing.T) {
	var pkillCalls [][]string
	restore := OverrideBrowserRecoveryOpsForTest(browserRecoveryOps{
		pidfilePath: func() (string, string, string, error) {
			// Signal "no pidfile" by returning an error — forces attempt 2 only.
			return "", "", "", errors.New("no home")
		},
		readFile:   os.ReadFile,
		removeFile: os.Remove,
		kill: func(_ int, _ syscall.Signal) error {
			t.Error("kill must not fire when pidfilePath errors")
			return nil
		},
		pkillRun: func(_ context.Context, args ...string) error {
			pkillCalls = append(pkillCalls, append([]string(nil), args...))
			return nil
		},
	})
	defer restore()

	execBrowserRunner{}.RecoverFork(context.Background())

	// 1 legacy -f call + 5 --exact calls (one per Chrome variant).
	if len(pkillCalls) != 6 {
		t.Fatalf("expected 6 pkill invocations (1 -f + 5 --exact), got %d: %+v", len(pkillCalls), pkillCalls)
	}
	// First call is the legacy pattern.
	if got := pkillCalls[0]; len(got) != 3 || got[0] != "-9" || got[1] != "-f" || got[2] != "agent-browser-" {
		t.Errorf("pkill[0] must be [-9 -f agent-browser-], got %v", got)
	}
	// Remaining 5 calls are --exact against each Chrome name.
	wantNames := map[string]bool{
		"chrome": true, "chromium": true, "chromium-browser": true,
		"google-chrome": true, "headless_shell": true,
	}
	for i, call := range pkillCalls[1:] {
		if len(call) != 3 || call[0] != "-9" || call[1] != "--exact" {
			t.Errorf("pkill[%d] must be [-9 --exact <name>], got %v", i+1, call)
			continue
		}
		if !wantNames[call[2]] {
			t.Errorf("pkill[%d] name %q not in expected set", i+1, call[2])
		}
		delete(wantNames, call[2])
	}
	for leftover := range wantNames {
		t.Errorf("expected pkill --exact call for %q was missing", leftover)
	}
}

// TestBrowserBatch_ForceResetRunsRecoveryBeforeBatch verifies the
// ForceReset input flag triggers RecoverFork BEFORE the batch starts,
// giving the kernel postRecoveryGrace (overridden to 0 in TestMain) to
// reap SIGKILL'd processes. The call order is asserted via a hook that
// records what ran when.
func TestBrowserBatch_ForceResetRunsRecoveryBeforeBatch(t *testing.T) {
	var order []string
	fake := &fakeBrowserRunner{
		runStdout: `[]`,
	}
	runnerWrap := &orderingRunner{inner: fake, order: &order}
	defer OverrideBrowserRunnerForTest(runnerWrap)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL:        "https://example.com",
		ForceReset: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) < 2 {
		t.Fatalf("expected at least recover + run events, got %v", order)
	}
	if order[0] != "recover" {
		t.Errorf("first event must be 'recover', got %v", order)
	}
	if order[1] != "run" {
		t.Errorf("second event must be 'run', got %v", order)
	}
	if fake.recoverCalls != 1 {
		t.Errorf("expected 1 RecoverFork call, got %d", fake.recoverCalls)
	}
}

// orderingRunner wraps a runner so tests can observe call ordering.
type orderingRunner struct {
	inner *fakeBrowserRunner
	order *[]string
}

func (o *orderingRunner) LookPath() (string, error) { return o.inner.LookPath() }
func (o *orderingRunner) Run(ctx context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	*o.order = append(*o.order, "run")
	return o.inner.Run(ctx, stdin, timeout)
}
func (o *orderingRunner) RecoverFork(ctx context.Context) {
	*o.order = append(*o.order, "recover")
	o.inner.RecoverFork(ctx)
}

// TestBrowserBatch_CDPTimeoutInStepsTriggersRecovery: daemon returned
// exit 0 with a step error matching a CDP-wedge signal. Tool must
// auto-RecoverFork, set ForkRecoveryAttempted, and surface a message
// naming the triggering signal — the next call's ForceReset retry is
// the escape hatch the recipe guide teaches.
func TestBrowserBatch_CDPTimeoutInStepsTriggersRecovery(t *testing.T) {
	errMsg := "CDP command timed out: DOM.enable"
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": false, "error": errMsg},
		{"command": []string{"errors"}, "success": true, "result": map[string]any{"errors": []any{}}},
		{"command": []string{"console"}, "success": true, "result": map[string]any{"logs": []any{}}},
		{"command": []string{"close"}, "success": true, "result": map[string]any{}},
	}
	raw, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeBrowserRunner{
		runStdout: string(raw),
		runStderr: "",
		runErr:    nil, // exit 0 despite the per-step wedge
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("CDP-timeout in step errors must trigger RecoverFork")
	}
	if fake.recoverCalls != 1 {
		t.Errorf("expected 1 RecoverFork call, got %d", fake.recoverCalls)
	}
	if !strings.Contains(result.Message, "Chrome wedged") {
		t.Errorf("message must name 'Chrome wedged' signal; got: %q", result.Message)
	}
	if !strings.Contains(result.Message, "CDP command timed out") {
		t.Errorf("message must name the triggering signal substring; got: %q", result.Message)
	}
	// Partial walk: errorsOutput / consoleOutput must not leak through.
	if len(result.ErrorsOutput) != 0 {
		t.Errorf("ErrorsOutput must stay empty on CDP-wedge recovery; got %s", result.ErrorsOutput)
	}
}

// TestBrowserBatch_TargetClosedAlsoTriggersRecovery: the wedge
// detection fires on "Target closed" too, not only CDP timeout —
// every known-wedge signature in cdpWedgeSignals is load-bearing.
func TestBrowserBatch_TargetClosedAlsoTriggersRecovery(t *testing.T) {
	errMsg := "Target closed"
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": false, "error": errMsg},
		{"command": []string{"close"}, "success": true, "result": map[string]any{}},
	}
	raw, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeBrowserRunner{runStdout: string(raw)}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ForkRecoveryAttempted {
		t.Error("'Target closed' in step errors must trigger RecoverFork")
	}
}

// TestBrowserBatch_ForceResetGatedByMutex: a caller that arrives with
// ForceReset=true while another caller holds browserMu waits for the
// in-flight call to complete before running its own RecoverFork +
// batch. The mutex holds across ForceReset too.
func TestBrowserBatch_ForceResetGatedByMutex(t *testing.T) {
	held := make(chan struct{})
	release := make(chan struct{})
	var runCount atomic.Int32

	blocker := &serializationRunner{
		onRun: func() {
			if runCount.Add(1) == 1 {
				close(held)
				<-release
			}
		},
	}
	defer OverrideBrowserRunnerForTest(blocker)()

	// Start the blocker in the background so it holds the mutex.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	}()
	<-held

	// Second caller requests ForceReset. It must wait for the blocker.
	gotResult := make(chan struct{})
	go func() {
		defer close(gotResult)
		_, _ = BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://second.example.com", ForceReset: true})
	}()

	select {
	case <-gotResult:
		t.Fatal("ForceReset caller should not complete while mutex is held")
	case <-time.After(50 * time.Millisecond):
		// Expected — it's blocked behind browserMu.
	}

	// Let the blocker finish so the ForceReset caller can proceed.
	close(release)
	select {
	case <-gotResult:
		// Expected progress.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForceReset caller never made progress after mutex release")
	}
	<-done
}

// TestBrowserBatch_CtxCancelWhileLockHeld verifies that a caller whose ctx
// is already cancelled returns immediately without blocking on browserMu,
// even when another caller is mid-flight.
func TestBrowserBatch_CtxCancelWhileLockHeld(t *testing.T) {
	held := make(chan struct{})
	release := make(chan struct{})

	blocker := &serializationRunner{
		onRun: func() {
			close(held)
			<-release
		},
	}
	defer OverrideBrowserRunnerForTest(blocker)()

	// Start the blocker in the background.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	}()

	// Wait until the blocker is inside Run (mutex held).
	<-held

	// A caller with a pre-cancelled ctx must return quickly, not block.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	returned := make(chan error, 1)
	go func() {
		_, err := BrowserBatch(ctx, BrowserBatchInput{URL: "https://example.com"})
		returned <- err
	}()

	select {
	case err := <-returned:
		if err == nil {
			t.Error("expected ctx cancellation error")
		} else if !strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "context deadline") &&
			!strings.Contains(err.Error(), "acquire browser lock") {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled caller blocked on browserMu — ctx-aware lock is broken")
	}

	// Let the blocker finish so we don't leak the goroutine.
	close(release)
	<-done
}
