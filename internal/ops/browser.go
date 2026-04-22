// Package ops — BrowserBatch wraps agent-browser with guaranteed lifecycle.
//
// The recipe workflow's close-step browser verification repeatedly burned
// on two failure modes: (1) missing close → daemon stays alive holding a
// Chrome process → fork budget exhausted → next Bash call crashes with
// "Resource temporarily unavailable"; (2) sequencing several bash calls
// that each spawn a new daemon round-trip, racing the single Chrome
// instance. BrowserBatch exists to make both mistakes impossible:
//
//   - Exactly ONE agent-browser invocation per URL.
//   - Tool controls the batch shape: [open url] + caller commands +
//     [errors] + [console] + [close]. Any open/close the caller puts
//     inside commands is stripped — the canonical wrappers are the only
//     lifecycle markers.
//   - All calls serialized via a package-level mutex. Two tools cannot
//     run in parallel. Mutex acquisition is ctx-aware — a cancelled
//     caller does not pile up behind a stuck predecessor.
//   - stdout/stderr are capped per stream so a runaway console-log flood
//     cannot OOM the zcp process.
//   - Context timeout bounded (default 120s, max 300s). On timeout,
//     fork-exhaustion signature in stderr, or a non-zero exit with no
//     parseable structured output, the tool runs pkill recovery
//     automatically and surfaces a ForkRecoveryAttempted flag instead of
//     propagating the raw error.
//   - Structured JSON output — errorsOutput and consoleOutput extracted
//     from the canonical penultimate steps so the caller doesn't have
//     to scan a string. Fields are populated ONLY on a clean run; a
//     failed run leaves them empty so the caller cannot mistake a partial
//     walk for a successful one.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// BrowserBatchInput is the caller-facing input for a browser walk.
type BrowserBatchInput struct {
	// URL is the page to open. Required.
	URL string `json:"url"`

	// Commands are the inner agent-browser commands to run AFTER open and
	// BEFORE the auto-appended [errors], [console], [close]. Each element
	// is one agent-browser command as a string array — e.g. ["snapshot",
	// "-i", "-c"], ["click", "@e1"], ["find", "role", "button", "Submit",
	// "click"]. Any "open" or "close" elements are stripped; the tool
	// always prepends [open url] and appends [close].
	Commands [][]string `json:"commands,omitempty"`

	// TimeoutSeconds bounds the whole batch. Default 120, max 300.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`

	// ForceReset, when true, runs RecoverFork BEFORE the batch — fully
	// kills any existing agent-browser daemon and Chrome process tree,
	// waits postRecoveryGrace for kernel reap, then starts fresh. Use
	// this when a previous call returned forkRecoveryAttempted=true
	// without the retry succeeding, or when a CDP-timeout / Target-
	// closed / Protocol-error string appeared in step errors. Adds
	// ~2s pre-roll; do not enable on every call — it defeats the
	// persistent-daemon fast path.
	ForceReset bool `json:"forceReset,omitempty" jsonschema:"Force full reset of agent-browser daemon + Chrome before starting. Use after CDP-timeout or repeat-recovery failures."`
}

// BrowserStepResult is one step from agent-browser's --json output.
type BrowserStepResult struct {
	Command []string        `json:"command"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *string         `json:"error,omitempty"`
}

// BrowserBatchResult is the structured return value.
type BrowserBatchResult struct {
	URL                   string              `json:"url"`
	Steps                 []BrowserStepResult `json:"steps,omitempty"`
	ErrorsOutput          json.RawMessage     `json:"errorsOutput,omitempty"`
	ConsoleOutput         json.RawMessage     `json:"consoleOutput,omitempty"`
	DurationMs            int64               `json:"durationMs"`
	ForkRecoveryAttempted bool                `json:"forkRecoveryAttempted,omitempty"`
	OutputTruncated       bool                `json:"outputTruncated,omitempty"`
	Message               string              `json:"message,omitempty"`
}

// browserRunner abstracts the agent-browser invocation for testability.
type browserRunner interface {
	LookPath() (string, error)
	Run(ctx context.Context, stdin string, timeout time.Duration) (stdout, stderr string, truncated bool, err error)
	RecoverFork(ctx context.Context)
}

// execBrowserRunner is the production runner.
type execBrowserRunner struct{}

func (execBrowserRunner) LookPath() (string, error) {
	return exec.LookPath("agent-browser")
}

// browserOutputCap bounds each of stdout/stderr to 1 MiB. A hostile or
// runaway console-log flood from the browsed page can emit megabytes of
// text — unbounded, that would OOM the zcp container. 1 MiB is far more
// than any legitimate verification walk produces.
const browserOutputCap = 1 << 20

// capBuffer is a write sink that caps accepted bytes at cap. Writes past
// the cap are silently dropped and Truncated is set. Matches io.Writer.
type capBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	remaining := c.cap - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *capBuffer) String() string { return c.buf.String() }

func (execBrowserRunner) Run(ctx context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, "agent-browser", "batch", "--json")
	cmd.Stdin = strings.NewReader(stdin)
	out := &capBuffer{cap: browserOutputCap}
	errBuf := &capBuffer{cap: browserOutputCap}
	cmd.Stdout = out
	cmd.Stderr = errBuf
	err := cmd.Run()
	// If the parent ctx is still alive but the child ctx deadlined, normalise.
	if err != nil && errors.Is(rctx.Err(), context.DeadlineExceeded) {
		err = context.DeadlineExceeded
	}
	return out.String(), errBuf.String(), out.truncated || errBuf.truncated, err
}

// browserRecoveryOps carries the syscalls + exec calls RecoverFork needs,
// behind overridable function fields. Tests swap these out with spies
// so the real kill/pkill side effects never run on the test machine.
// This replaces the pkill-only recovery path that v27 proved insufficient:
// Chrome processes inherited by the daemon's process group are now reaped
// via a negative-pid SIGKILL read off the daemon's pidfile, and the
// stale pidfile + socket are removed so the next CLI invocation launches
// a fresh daemon instead of attaching to a zombie.
type browserRecoveryOps struct {
	// pidfilePath returns the absolute path to the agent-browser pidfile
	// for the current session ("default" unless AGENT_BROWSER_SESSION is
	// set). Also returns the directory (so socket candidates can be
	// derived) and the session name.
	pidfilePath func() (pidfile, socketDir, session string, err error)
	// readFile reads the pidfile bytes.
	readFile func(path string) ([]byte, error)
	// removeFile removes a stale pidfile / socket. Non-existent is fine.
	removeFile func(path string) error
	// kill issues a signal to a PID. Negative PID = process group.
	kill func(pid int, sig syscall.Signal) error
	// pkillRun runs pkill <args> under ctx. Non-zero exit (no matches)
	// is swallowed by the caller. Returns only fatal errors.
	pkillRun func(ctx context.Context, args ...string) error
}

// defaultBrowserRecoveryOps wires the production implementations.
// The `kill` implementation is platform-specific (see
// browser_kill_unix.go + browser_kill_windows.go) — agent-browser is
// a Linux-container tool; the Windows build compiles to a no-op so
// the zcp CLI itself remains cross-platform.
func defaultBrowserRecoveryOps() browserRecoveryOps {
	return browserRecoveryOps{
		pidfilePath: resolveAgentBrowserPaths,
		readFile:    os.ReadFile,
		removeFile:  os.Remove,
		kill:        defaultKill,
		pkillRun: func(ctx context.Context, args ...string) error {
			return exec.CommandContext(ctx, "pkill", args...).Run()
		},
	}
}

// browserRecovery holds the active recovery ops. Tests override via
// OverrideBrowserRecoveryOpsForTest.
var browserRecovery = defaultBrowserRecoveryOps()

// OverrideBrowserRecoveryOpsForTest swaps the recovery ops and returns
// a restore function. Tests use this to assert on syscalls / pkill
// invocations without executing them for real.
func OverrideBrowserRecoveryOpsForTest(ops browserRecoveryOps) func() {
	old := browserRecovery
	browserRecovery = ops
	return func() { browserRecovery = old }
}

// resolveAgentBrowserPaths returns (pidfile, socketDir, session) by
// combining the user home directory (or AGENT_BROWSER_SOCKET_DIR when
// set) with AGENT_BROWSER_SESSION (default: "default"). Session name
// governs both pidfile and socket candidates, matching the v0.21.4
// agent-browser layout.
func resolveAgentBrowserPaths() (string, string, string, error) {
	session := os.Getenv("AGENT_BROWSER_SESSION")
	if session == "" {
		session = "default"
	}
	dir := os.Getenv("AGENT_BROWSER_SOCKET_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", "", err
		}
		dir = filepath.Join(home, ".agent-browser")
	}
	return filepath.Join(dir, session+".pid"), dir, session, nil
}

// RecoverFork performs a full agent-browser + Chrome reset.
//
// Attempt 1 — read the daemon pidfile, kill its process group via
// syscall.Kill(-pid, SIGKILL) so every Chrome helper the daemon forked
// is reaped regardless of binary name. Then kill the daemon itself.
// Remove the stale pidfile and socket files so the next CLI invocation
// spawns a fresh daemon.
//
// Attempt 2 — pkill fallback for anything that escaped the group. The
// legacy `pkill -9 -f agent-browser-` pattern stays (it matches the
// daemon binary family). New: `pkill -9 --exact <name>` runs for each
// Chrome binary family (chrome, chromium, chromium-browser,
// google-chrome, headless_shell). `--exact` matches the process basename
// only — never argv tail — so code-server's `--no-chrome` CLI flag is
// untouched. Using `-f` against `chrome` would match code-server and
// kill the user's editor (happened once in a v27 run; never again).
//
// Errors from pkill are swallowed — exit 1 (no matches) is the common
// case after a clean run that doesn't need recovery. Failure of the
// pidfile path falls through to the pkill fallback; failure of both
// leaves the system unchanged and the caller sees forkRecoveryAttempted=
// true via the call-site bookkeeping.
func (execBrowserRunner) RecoverFork(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ops := browserRecovery

	// Attempt 1: pidfile-based process-group kill.
	if ops.pidfilePath != nil {
		if pidfile, dir, session, err := ops.pidfilePath(); err == nil {
			if data, readErr := ops.readFile(pidfile); readErr == nil {
				if pid, atoiErr := strconv.Atoi(strings.TrimSpace(string(data))); atoiErr == nil && pid > 0 {
					// Negative PID → kill the process group. Captures Chrome
					// and every helper inherited from the daemon's fork.
					if ops.kill != nil {
						_ = ops.kill(-pid, killSignal)
						_ = ops.kill(pid, killSignal)
					}
				}
			}
			if ops.removeFile != nil {
				_ = ops.removeFile(pidfile)
				// Both socket candidate forms — agent-browser v0.21.4 uses
				// `<session>.sock` for the default session and may also
				// write `agent-browser.<session>.sock` when a non-default
				// AGENT_BROWSER_SESSION is exported.
				_ = ops.removeFile(filepath.Join(dir, session+".sock"))
				_ = ops.removeFile(filepath.Join(dir, "agent-browser."+session+".sock"))
			}
		}
	}

	// Attempt 2: pattern fallback for anything that escaped the group.
	if ops.pkillRun != nil {
		_ = ops.pkillRun(pctx, "-9", "-f", "agent-browser-")
		for _, name := range chromeBinaryNames {
			_ = ops.pkillRun(pctx, "-9", "--exact", name)
		}
	}
}

// chromeBinaryNames lists the process basename(s) agent-browser v0.21.4
// may launch for the headless Chrome it drives. pkill --exact matches
// argv[0] only so each name here is the binary name as it appears in
// /proc/<pid>/comm — never the full CLI.
var chromeBinaryNames = []string{
	"chrome",
	"chromium",
	"chromium-browser",
	"google-chrome",
	"headless_shell",
}

// browserRun is the active runner. Tests override via OverrideBrowserRunnerForTest.
var browserRun browserRunner = execBrowserRunner{}

// browserMu serializes all BrowserBatch calls. agent-browser uses a
// single persistent daemon per container — concurrent calls either race
// the daemon or spawn a second one that exceeds the fork budget.
var browserMu sync.Mutex

// OverrideBrowserRunnerForTest replaces the browser runner. Returns restore.
func OverrideBrowserRunnerForTest(r browserRunner) func() {
	old := browserRun
	browserRun = r
	return func() { browserRun = old }
}

// AgentBrowserAvailable reports whether agent-browser is on PATH. Used by
// server registration to gate the zerops_browser tool — it is pointless to
// expose a tool that will always fail its LookPath check.
func AgentBrowserAvailable() bool {
	_, err := browserRun.LookPath()
	return err == nil
}

const (
	browserDefaultTimeout = 120 * time.Second
	browserMaxTimeout     = 300 * time.Second
	// browserCmdClose is the agent-browser close command. Named constant
	// so the three usages below stay in sync if the CLI ever changes.
	browserCmdClose = "close"
	// browserCmdOpen is the agent-browser open command.
	browserCmdOpen = "open"
)

// postRecoveryGrace is the pause after a pkill recovery, to give the
// kernel time to reap SIGKILL'd processes before the caller's next
// attempt. Runs OUTSIDE the package mutex so it does not block other
// browser calls — only the caller that triggered the recovery waits.
// Not a const so tests can override it to avoid sleeping for real.
var postRecoveryGrace = 2 * time.Second

// OverridePostRecoveryGraceForTest sets postRecoveryGrace and returns a
// restore function. Tests use this to avoid paying the real 2-second
// sleep on every recovery-triggering assertion.
func OverridePostRecoveryGraceForTest(d time.Duration) func() {
	old := postRecoveryGrace
	postRecoveryGrace = d
	return func() { postRecoveryGrace = old }
}

// lockBrowserMu acquires browserMu, honouring ctx cancellation. On success
// the caller owns the mutex and must Unlock it. On ctx cancellation a
// cleanup goroutine is spawned that will Lock+Unlock on behalf of the
// abandoned attempt once its turn arrives, so no permanent lock leak
// occurs.
func lockBrowserMu(ctx context.Context) error {
	acquired := make(chan struct{})
	go func() {
		browserMu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return nil
	case <-ctx.Done():
		// Caller bailed. Spawn a cleanup goroutine that releases the mutex
		// as soon as the original Lock() attempt succeeds. The caller
		// returns ctx.Err() immediately and other waiters eventually make
		// progress.
		go func() {
			<-acquired
			browserMu.Unlock()
		}()
		return ctx.Err()
	}
}

// cdpWedgeSignals are per-step error substrings that indicate Chrome
// wedged behind a stuck CDP connection even when the daemon returned
// exit 0. A match on any of these auto-runs RecoverFork so the next
// call doesn't reattach to the same zombie Chrome.
var cdpWedgeSignals = []string{
	"CDP command timed out",
	"Target closed",
	"Protocol error",
}

// BrowserBatch runs one bounded agent-browser session against the given URL.
// See package doc for the lifecycle contract.
func BrowserBatch(ctx context.Context, input BrowserBatchInput) (*BrowserBatchResult, error) {
	if strings.TrimSpace(input.URL) == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"url is required",
			"Pass the subdomain URL to walk (e.g. the appstage zerops.app URL from zerops_discover)",
		)
	}

	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	switch {
	case timeout <= 0:
		timeout = browserDefaultTimeout
	case timeout > browserMaxTimeout:
		timeout = browserMaxTimeout
	}

	batch := buildCanonicalBatch(input.URL, input.Commands)
	stdinBytes, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	// Serialize: only one agent-browser session at a time across the whole
	// process. Ctx-aware so a cancelled caller does not pile up behind a
	// stuck predecessor.
	if err := lockBrowserMu(ctx); err != nil {
		return nil, fmt.Errorf("acquire browser lock: %w", err)
	}
	// Explicit unlock so we can run post-recovery grace OUTSIDE the
	// critical section. Without this, every waiter would block for the
	// 2-second kernel-reap pause.
	recoveryNeeded := false
	defer func() {
		browserMu.Unlock()
		if recoveryNeeded {
			time.Sleep(postRecoveryGrace)
		}
	}()

	if _, err := browserRun.LookPath(); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"agent-browser not found on PATH",
			"This tool is only available inside the ZCP container. agent-browser is pre-installed there.",
		)
	}

	// Cx-BROWSER-RECOVERY-COMPLETE: ForceReset fires RecoverFork BEFORE
	// the batch starts, giving the kernel postRecoveryGrace to reap
	// SIGKILL'd processes. Use when a prior call returned
	// forkRecoveryAttempted=true without the retry succeeding, or when
	// CDP-timeout step errors surfaced in the last run's result.Steps.
	if input.ForceReset {
		browserRun.RecoverFork(ctx)
		time.Sleep(postRecoveryGrace)
	}

	start := time.Now()
	stdout, stderr, truncated, runErr := browserRun.Run(ctx, string(stdinBytes), timeout)
	duration := time.Since(start)

	result := &BrowserBatchResult{
		URL:             input.URL,
		DurationMs:      duration.Milliseconds(),
		OutputTruncated: truncated,
	}

	// Fork-exhaustion detection runs BEFORE exit-code checks — the daemon
	// sometimes exits 0 after logging the error, sometimes exits non-zero.
	// Check stderr only: stdout is JSON containing user-controlled text
	// (page titles, console logs) that could match a loose substring.
	if isForkExhausted(stderr) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		recoveryNeeded = true
		result.Message = "Fork budget exhausted (agent-browser or Chrome could not spawn a process). " +
			"pkill recovery ran automatically. Before retrying, stop background dev processes on every dev container " +
			"(e.g. `ssh apidev \"pkill -f 'nest start'\"`) — those are the usual culprit."
		return result, nil
	}

	// Context-deadline timeout → recovery + clear message.
	if runErr != nil && errors.Is(runErr, context.DeadlineExceeded) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		recoveryNeeded = true
		result.Message = fmt.Sprintf("agent-browser timed out after %s. pkill recovery ran automatically. "+
			"Retry with a shorter command sequence, or raise timeoutSeconds (max %ds).",
			timeout, int(browserMaxTimeout.Seconds()))
		return result, nil
	}

	// Any other non-zero exit: record the raw error in Message BEFORE
	// attempting to parse stdout, so a subsequent parse failure does not
	// overwrite the exit-code context.
	if runErr != nil {
		result.Message = fmt.Sprintf("agent-browser exited with error: %v", runErr)
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			result.Message += "\nstderr: " + trimmed
		}
	}

	// Parse structured output if any. On parse failure, APPEND to Message
	// rather than overwrite — preserves any existing exit-code context.
	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &result.Steps); err != nil {
			parseMsg := fmt.Sprintf("failed to parse agent-browser --json output: %v\nraw output:\n%s", err, stdout)
			if result.Message == "" {
				result.Message = parseMsg
			} else {
				result.Message += "\n" + parseMsg
			}
			// Unparseable JSON on top of a non-zero exit strongly suggests
			// the daemon crashed without emitting structured output. Reap
			// any leaked Chrome helpers so the next call has a fresh
			// fork budget.
			if runErr != nil {
				browserRun.RecoverFork(ctx)
				result.ForkRecoveryAttempted = true
				recoveryNeeded = true
			}
			return result, nil
		}
	}

	// If we had a non-zero exit but parsed no steps at all, the daemon
	// likely crashed before emitting any output. Reap orphaned children.
	if runErr != nil && len(result.Steps) == 0 {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		recoveryNeeded = true
		return result, nil
	}

	// Cx-BROWSER-RECOVERY-COMPLETE: CDP-wedge signals buried in per-step
	// errors (even with exit 0) indicate Chrome hung behind a stuck CDP
	// connection. The daemon is alive; Chrome isn't responding. Reap the
	// whole process tree so the next call doesn't reattach to the zombie.
	if sig, hit := scanStepsForCDPWedge(result.Steps); hit {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		recoveryNeeded = true
		result.Message = fmt.Sprintf(
			"Chrome wedged behind CDP (signal: %s). Full reset ran automatically. "+
				"Retry with forceReset=true if the next call still wedges.",
			sig,
		)
		// Intentionally do not populate ErrorsOutput / ConsoleOutput —
		// the partial walk must not look like a successful one.
		return result, nil
	}

	// Extract the canonical errors/console steps ONLY on a successful run.
	// On a non-zero exit we preserve Steps for diagnosis but we must NOT
	// populate ErrorsOutput/ConsoleOutput — those fields are the load-
	// bearing signal the recipe close step reads, and a partial walk must
	// not look like a successful one.
	if runErr == nil {
		if n := len(result.Steps); n >= 3 {
			if isCommand(result.Steps[n-3].Command, "errors") {
				result.ErrorsOutput = result.Steps[n-3].Result
			}
			if isCommand(result.Steps[n-2].Command, "console") {
				result.ConsoleOutput = result.Steps[n-2].Result
			}
		}
	}

	return result, nil
}

// scanStepsForCDPWedge returns the first matching signal substring and
// true if any step's error field contains one of the CDP-wedge markers
// in cdpWedgeSignals. The "" sentinel in the return means "no match".
func scanStepsForCDPWedge(steps []BrowserStepResult) (string, bool) {
	for _, s := range steps {
		if s.Error == nil {
			continue
		}
		msg := *s.Error
		for _, sig := range cdpWedgeSignals {
			if strings.Contains(msg, sig) {
				return sig, true
			}
		}
	}
	return "", false
}

// buildCanonicalBatch assembles [open url] + stripped caller commands +
// [errors] [console] [close]. Any open/close in the caller's commands is
// silently dropped — the canonical wrappers are the only lifecycle markers.
func buildCanonicalBatch(url string, commands [][]string) [][]string {
	inner := make([][]string, 0, len(commands))
	for _, cmd := range commands {
		if len(cmd) == 0 {
			continue
		}
		switch cmd[0] {
		case browserCmdOpen, browserCmdClose:
			// Strip — caller should not manage lifecycle.
			continue
		}
		inner = append(inner, cmd)
	}
	batch := make([][]string, 0, len(inner)+4)
	batch = append(batch, []string{browserCmdOpen, url})
	batch = append(batch, inner...)
	batch = append(batch, []string{"errors"}, []string{"console"}, []string{browserCmdClose})
	return batch
}

// isCommand reports whether a step's command starts with the given name.
func isCommand(cmd []string, name string) bool {
	return len(cmd) > 0 && cmd[0] == name
}

// isForkExhausted matches stderr signatures we've seen in production
// incidents (v4 and v5 of the recipe workflow both hit this). Checked
// against stderr ONLY — stdout is user-controlled JSON that could match
// these substrings legitimately (page title "Resource temporarily
// unavailable" error page, for example).
func isForkExhausted(stderr string) bool {
	lc := strings.ToLower(stderr)
	return strings.Contains(lc, "resource temporarily unavailable") ||
		strings.Contains(lc, "pthread_create") ||
		strings.Contains(lc, "fork failed")
}
