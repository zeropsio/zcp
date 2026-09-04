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
	"image/png"
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

	// Screenshot, when true, inserts a screenshot step right
	// after Commands and before the errors/console/network-requests
	// tail. The captured PNG is returned via BrowserBatchResult.Screenshot.
	Screenshot bool `json:"screenshot,omitempty"`
}

// BrowserStepResult is one step from agent-browser's --json output.
type BrowserStepResult struct {
	Command   []string        `json:"command"`
	Success   bool            `json:"success"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *string         `json:"error,omitempty"`
	ErrorKind string          `json:"errorKind,omitempty"`
}

// ErrorKind values classify a step's Error text into a coarse category so
// callers can branch (retry a different selector, wait longer, escalate)
// without re-parsing agent-browser's free-text error message. Empty
// string means the step carried no error.
const (
	ErrorKindSelectorNotFound = "selector-not-found"
	ErrorKindNotEditable      = "not-editable"
	ErrorKindTimeout          = "timeout"
	ErrorKindWedge            = "wedge"
	ErrorKindOther            = "other"
)

// classifyStepError maps one step's raw agent-browser error text to an
// ErrorKind. Patterns are best-effort substrings, checked case-insensitive.
// cdpWedgeSignals is checked FIRST and wins over every other pattern:
// "CDP command timed out" contains "timed out" (which would otherwise
// match the generic timeout case) but must classify as wedge — those
// signals are also what scanStepsForCDPWedge uses to trigger RecoverFork,
// so the two must never disagree about which steps are wedge-related.
func classifyStepError(msg string) string {
	lc := strings.ToLower(msg)
	for _, sig := range cdpWedgeSignals {
		if strings.Contains(lc, strings.ToLower(sig)) {
			return ErrorKindWedge
		}
	}
	switch {
	case strings.Contains(lc, "no element"), strings.Contains(lc, "no such element"), strings.Contains(lc, "not found"):
		return ErrorKindSelectorNotFound
	case strings.Contains(lc, "not editable"), strings.Contains(lc, "not fillable"):
		return ErrorKindNotEditable
	case strings.Contains(lc, "timeout"), strings.Contains(lc, "timed out"):
		return ErrorKindTimeout
	default:
		return ErrorKindOther
	}
}

// BrowserBatchResult is the structured return value.
type BrowserBatchResult struct {
	URL                   string                   `json:"url"`
	Steps                 []BrowserStepResult      `json:"steps,omitempty"`
	ErrorsOutput          json.RawMessage          `json:"errorsOutput,omitempty"`
	ConsoleOutput         json.RawMessage          `json:"consoleOutput,omitempty"`
	NetworkOutput         []NetworkRequest         `json:"networkOutput,omitempty"`
	Screenshot            *BrowserScreenshotResult `json:"screenshot,omitempty"`
	DurationMs            int64                    `json:"durationMs"`
	ForkRecoveryAttempted bool                     `json:"forkRecoveryAttempted,omitempty"`
	OutputTruncated       bool                     `json:"outputTruncated,omitempty"`
	Message               string                   `json:"message,omitempty"`
}

// BrowserScreenshotResult carries a captured screenshot. PNG is excluded
// from JSON (json:"-") — the tools layer promotes it into a dedicated MCP
// image content block instead of duplicating megabytes of base64 inside
// the text/JSON result; Width/Height stay in the JSON as useful metadata
// for a caller that only reads text.
type BrowserScreenshotResult struct {
	PNG    []byte `json:"-"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// NetworkRequest is one entry from BrowserBatchResult.NetworkOutput.
// The auto-appended tail step already asks agent-browser for the 4xx/5xx
// slice (`network requests --status 400-599`, see browserFlagStatus);
// parseNetworkRequests keeps the `Status >= 400` filter too so the
// meaning is pinned in Go even if the CLI flag's behavior ever drifts.
//
// Field names mirror agent-browser's Playwright-style request/response
// model (route/har/requests support implies that lineage — see
// `network --help`: route/unroute/requests/har). Measured on z3-eval
// (agent-browser 0.35.1, live `--status 400-599` output): entries here
// carry no "failure" field — a request that failed at the network layer
// (DNS, connection refused, aborted) or is still in flight has no
// "status" key either, and nothing in the JSON distinguishes the two
// cases from each other, so neither is reported. This only surfaces
// requests that received a 4xx/5xx HTTP response. The exact --json
// wrapper shape (bare array vs. `{"requests": [...]}`) was not itself
// re-confirmed with --status applied — parseNetworkRequests tolerates
// both so a wrapper-key mismatch degrades to "nothing parsed" rather
// than a hard error.
type NetworkRequest struct {
	URL          string `json:"url"`
	Method       string `json:"method,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	Status       int    `json:"status,omitempty"`
	StatusText   string `json:"statusText,omitempty"`
}

// parseNetworkRequests decodes the "network requests" step result and
// filters to HTTP status >= 400 (belt-and-suspenders on top of the
// --status 400-599 the tail step already requested — see NetworkRequest
// doc comment). Accepts either a bare JSON array of requests or an
// object with a "requests" key. Returns nil on any parse failure or an
// empty/absent result — a malformed network-tail payload must never
// fail the whole batch.
func parseNetworkRequests(raw json.RawMessage) []NetworkRequest {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var all []NetworkRequest
	if err := json.Unmarshal(raw, &all); err != nil {
		var wrapped struct {
			Requests []NetworkRequest `json:"requests"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil
		}
		all = wrapped.Requests
	}
	filtered := make([]NetworkRequest, 0, len(all))
	for _, r := range all {
		if r.Status >= 400 {
			filtered = append(filtered, r)
		}
	}
	return filtered
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
	// browserCmdEval is the raw eval command. Stripped from caller
	// commands like open/close — dedicated commands (get/find/is/set)
	// produce structured, parseable output; eval's return value does
	// not have a stable shape. The tool description has always told
	// callers not to use it; this enforces it.
	browserCmdEval = "eval"
	// browserCmdErrors, browserCmdConsole, browserCmdNetwork name the
	// three auto-appended reporting commands, in the order they always
	// appear in the tail (errors, console, network requests, close).
	browserCmdErrors  = "errors"
	browserCmdConsole = "console"
	browserCmdNetwork = "network"
	// browserSubcmdRequests is the "network requests" subcommand — see
	// agent-browser 0.35.1 `network --help`: "requests [options]  List
	// captured requests".
	browserSubcmdRequests = "requests"
	// browserFlagStatus + browserStatusRange4xx5xx bound the auto-
	// appended network-requests tail step to failures at the source.
	// Measured on z3-eval (agent-browser 0.35.1): each request entry in
	// `network requests --json` is ~1 KB (headers/responseHeaders/
	// postData included), so an unfiltered log on a real page blows the
	// batch's 1 MiB stdout cap (docs.zerops.io 41 reqs/40 KB is fine,
	// nytimes.com 471 reqs/845 KB and amazon.com 614 reqs/1.33 MB are
	// not) and would truncate the WHOLE batch output, not just the
	// network tail. `--status <code>` accepts "200", "2xx", or a
	// "400-499" range per `--help`; a status-filtered page has one
	// entry (~2 KB). See NetworkRequest's doc comment for what the
	// filtered JSON does (and does not) carry.
	browserFlagStatus        = "--status"
	browserStatusRange4xx5xx = "400-599"
	// browserCmdScreenshot is the agent-browser screenshot command.
	// agent-browser 0.35.1 `screenshot --help`: "Usage: agent-browser
	// screenshot [selector] [path]". Inside `batch` the annotate flag is
	// NOT honoured: measured 2026-09-04 on z3-eval, [screenshot --annotate
	// <path>] fails with "Element not found: --annotate" (the flag is read
	// as the selector) and [screenshot <path> --annotate] reports success
	// but writes no file; the plain [screenshot <path>] step writes the
	// PNG. buildCanonicalBatch therefore emits the plain form.
	browserCmdScreenshot = "screenshot"
)

const (
	// browserScaleDelta is added to CURRENT container usage (relative "+"
	// form), so the granted headroom is independent of baseline load.
	// browserScaleWindow is zsc's minimum (10m); the boost auto-reverts when
	// idle or after the window, so we never leave the container scaled up.
	browserScaleDelta  = "+2GiB"
	browserScaleWindow = "10m"
	// browserScaleExecTimeout bounds the zsc call so a hung CLI never blocks
	// the browser longer than necessary.
	browserScaleExecTimeout = 15 * time.Second
	// browserHeadroomTargetBytes is the free-RAM floor (ceiling − current) we
	// want before launching Chrome. ~1.2 GiB comfortably covers a page render
	// on top of the agent container's ~1.8 GiB baseline.
	browserHeadroomTargetBytes = 1288490188 // 1.2 GiB
	// browserScaleSettle bounds the wait for the grant to land — zsc returns
	// after REQUESTING the scale, and "up to 10 seconds for resources to be
	// assigned" (zsc scale --help).
	browserScaleSettle = 11 * time.Second
)

// browserScale grants the Chrome renderer RAM headroom before launch.
// agent-browser/Chrome spikes 300-700 MB; on a tightly-provisioned agent
// container (default 2 GiB, ~90% baseline from code-server + the agent + the
// MCP server) that spike thrashes the cgroup ceiling and every CDP command
// times out (open/snapshot hang while daemon-level commands still respond —
// the wedge users hit). `zsc scale ram` grants RAM live (no restart —
// verified) and auto-reverts, so we boost right before the batch and let it
// lapse. Best-effort + injectable: a scale failure (zsc absent, maxRam
// ceiling) never blocks the browser; agent-browser proceeds and degrades
// exactly as before.
var browserScale = defaultBrowserScale

// defaultBrowserScale boosts container RAM via `zsc scale ram +2GiB 10m` when
// free headroom is below target, then waits for the grant to land so Chrome
// has room before it launches. No-op outside a ZCP container (no zsc on PATH)
// or when headroom is already sufficient (a prior boost still active, or an
// idle container). Returns the zsc error for the caller to LOG — never to act
// on; the browser runs regardless.
func defaultBrowserScale(ctx context.Context) error {
	if _, err := exec.LookPath("zsc"); err != nil {
		return nil // not a ZCP container — nothing to scale
	}
	if free, ok := containerFreeRAMBytes(); ok && free >= browserHeadroomTargetBytes {
		return nil // already enough headroom (idle, or a prior boost still active)
	}
	cctx, cancel := context.WithTimeout(ctx, browserScaleExecTimeout)
	defer cancel()
	if err := exec.CommandContext(cctx, "zsc", "scale", "ram", browserScaleDelta, browserScaleWindow).Run(); err != nil {
		return fmt.Errorf("zsc scale ram: %w", err)
	}
	// zsc returns after REQUESTING the grant; it lands within ~10s. Poll the
	// cgroup ceiling so the headroom is live before Chrome launches.
	deadline := time.Now().Add(browserScaleSettle)
	for time.Now().Before(deadline) {
		if free, ok := containerFreeRAMBytes(); ok && free >= browserHeadroomTargetBytes {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// containerFreeRAMBytes returns free bytes (cgroup-v2 memory.max − .current),
// ok=false when unreadable (not a Linux container) or unbounded. Best-effort:
// callers proceed on !ok.
func containerFreeRAMBytes() (int64, bool) {
	maxRaw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return 0, false
	}
	maxStr := strings.TrimSpace(string(maxRaw))
	if maxStr == "max" { // unbounded ceiling — effectively unlimited headroom
		return 1 << 62, true
	}
	maxB, err := strconv.ParseInt(maxStr, 10, 64)
	if err != nil {
		return 0, false
	}
	curRaw, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0, false
	}
	curB, err := strconv.ParseInt(strings.TrimSpace(string(curRaw)), 10, 64)
	if err != nil {
		return 0, false
	}
	if curB >= maxB {
		return 0, true
	}
	return maxB - curB, true
}

// OverrideBrowserScaleForTest swaps the RAM pre-scaler and returns a restore
// func. Tests use this to assert the invocation without shelling out to zsc.
func OverrideBrowserScaleForTest(fn func(context.Context) error) func() {
	old := browserScale
	browserScale = fn
	return func() { browserScale = old }
}

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

// resolveBrowserTimeout clamps the caller-requested timeout to
// [browserDefaultTimeout, browserMaxTimeout] — 0/negative falls back to
// the default, anything past the max is capped.
func resolveBrowserTimeout(seconds int) time.Duration {
	timeout := time.Duration(seconds) * time.Second
	switch {
	case timeout <= 0:
		timeout = browserDefaultTimeout
	case timeout > browserMaxTimeout:
		timeout = browserMaxTimeout
	}
	return timeout
}

// prepareScreenshotTempFile reserves a unique file path for the
// "screenshot <path>" step — agent-browser writes the PNG to
// disk directly, it isn't returned inline in the step's JSON result.
// Returns a cleanup func that removes the file; callers must defer it
// unconditionally, since the file may never be produced (a failed run).
// When want is false, path is "" and cleanup is a no-op.
func prepareScreenshotTempFile(want bool) (path string, cleanup func(), err error) {
	if !want {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp("", "zcp-browser-screenshot-*.png")
	if err != nil {
		return "", func() {}, fmt.Errorf("create screenshot temp file: %w", err)
	}
	path = f.Name()
	_ = f.Close()
	return path, func() { _ = os.Remove(path) }, nil
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

	timeout := resolveBrowserTimeout(input.TimeoutSeconds)

	screenshotPath, cleanupScreenshot, err := prepareScreenshotTempFile(input.Screenshot)
	if err != nil {
		return nil, err
	}
	defer cleanupScreenshot()

	batch := buildCanonicalBatch(input.URL, input.Commands, screenshotPath)
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

	// Grant the Chrome renderer RAM headroom before launch — on a tightly
	// provisioned agent container the 300-700 MB spike otherwise thrashes the
	// cgroup ceiling and every CDP command times out. Best-effort +
	// auto-reverting (see browserScale); a scale failure never blocks the
	// browser, it just degrades to the pre-fix behaviour.
	if err := browserScale(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "zerops_browser: RAM pre-scale failed (continuing): %v\n", err)
	}

	start := time.Now()
	stdout, stderr, truncated, runErr := browserRun.Run(ctx, string(stdinBytes), timeout)
	duration := time.Since(start)

	result := &BrowserBatchResult{
		URL:             input.URL,
		DurationMs:      duration.Milliseconds(),
		OutputTruncated: truncated,
	}

	if done, recovered := classifyRunResult(ctx, result, stdout, stderr, runErr, timeout); done {
		recoveryNeeded = recovered
		return result, nil
	}

	// runErr == nil here — classifyRunResult returns done=true for every
	// non-zero-exit path except the "steps parsed despite a message" case,
	// which can't happen (parse failure always returns done=true too). So
	// output extraction is safe to run unconditionally past this point;
	// it stays behind its own runErr guard for defense in depth and to
	// keep the "must not populate on a partial walk" invariant explicit.
	if runErr == nil {
		populateBrowserBatchOutputs(result, input.Screenshot, screenshotPath)
	}

	return result, nil
}

// classifyRunResult inspects one agent-browser invocation's raw outcome
// and mutates result accordingly: fork-exhaustion, a context-deadline
// timeout, and any other non-zero exit are recorded in result.Message;
// stdout (when non-empty) is parsed into result.Steps with each step's
// ErrorKind classified; a CDP-wedge signal buried in step errors (even
// on exit 0) triggers the same recovery path. Every RecoverFork call
// site here mirrors the original inline logic — see git history for the
// pre-extraction version if the branch-by-branch behavior needs review.
//
// Returns done=true when BrowserBatch must return result immediately
// (every branch except "steps parsed cleanly, keep going" is terminal),
// and recovered=true when the caller must set recoveryNeeded so
// postRecoveryGrace runs after the mutex unlocks.
func classifyRunResult(ctx context.Context, result *BrowserBatchResult, stdout, stderr string, runErr error, timeout time.Duration) (done, recovered bool) {
	// Fork-exhaustion detection runs BEFORE exit-code checks — the daemon
	// sometimes exits 0 after logging the error, sometimes exits non-zero.
	// Check stderr only: stdout is JSON containing user-controlled text
	// (page titles, console logs) that could match a loose substring.
	if isForkExhausted(stderr) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		result.Message = "Fork budget exhausted (agent-browser or Chrome could not spawn a process). " +
			"pkill recovery ran automatically. Before retrying, stop background dev processes on every dev container " +
			"(e.g. `ssh apidev \"pkill -f 'nest start'\"`) — those are the usual culprit."
		return true, true
	}

	// Context-deadline timeout → recovery + clear message.
	if runErr != nil && errors.Is(runErr, context.DeadlineExceeded) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		result.Message = fmt.Sprintf("agent-browser timed out after %s. pkill recovery ran automatically. "+
			"Retry with a shorter command sequence, or raise timeoutSeconds (max %ds).",
			timeout, int(browserMaxTimeout.Seconds()))
		return true, true
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
				return true, true
			}
			return true, false
		}
		// Classify every step's error text into a coarse ErrorKind — this
		// runs regardless of overall run success, since Steps is preserved
		// for diagnosis on both success and failure paths below.
		classifyStepErrors(result.Steps)
	}

	// If we had a non-zero exit but parsed no steps at all, the daemon
	// likely crashed before emitting any output. Reap orphaned children.
	if runErr != nil && len(result.Steps) == 0 {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		return true, true
	}

	// Cx-BROWSER-RECOVERY-COMPLETE: CDP-wedge signals buried in per-step
	// errors (even with exit 0) indicate Chrome hung behind a stuck CDP
	// connection. The daemon is alive; Chrome isn't responding. Reap the
	// whole process tree so the next call doesn't reattach to the zombie.
	if sig, hit := scanStepsForCDPWedge(result.Steps); hit {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		result.Message = fmt.Sprintf(
			"Chrome wedged behind CDP (signal: %s). Full reset ran automatically. "+
				"Retry with forceReset=true if the next call still wedges.",
			sig,
		)
		// Intentionally do not populate ErrorsOutput / ConsoleOutput —
		// the partial walk must not look like a successful one.
		return true, true
	}

	return false, false
}

// classifyStepErrors stamps ErrorKind on every step that carries an
// Error, using classifyStepError.
func classifyStepErrors(steps []BrowserStepResult) {
	for i := range steps {
		if steps[i].Error != nil {
			steps[i].ErrorKind = classifyStepError(*steps[i].Error)
		}
	}
}

// populateBrowserBatchOutputs fills ErrorsOutput/ConsoleOutput/
// NetworkOutput/Screenshot from a successful run's Steps. Tail is
// always errors, console, network requests, close (from
// buildCanonicalBatch), with an optional screenshot step one further
// back when requested — indexed from the end so an arbitrary number of
// caller commands before it doesn't shift the offsets. Callers must
// only invoke this when runErr == nil: on a non-zero exit these fields
// must stay unpopulated so a partial walk never looks like a successful
// one.
func populateBrowserBatchOutputs(result *BrowserBatchResult, screenshotRequested bool, screenshotPath string) {
	if n := len(result.Steps); n >= 4 {
		if isCommand(result.Steps[n-4].Command, browserCmdErrors) {
			result.ErrorsOutput = result.Steps[n-4].Result
		}
		if isCommand(result.Steps[n-3].Command, browserCmdConsole) {
			result.ConsoleOutput = result.Steps[n-3].Result
		}
		if isCommand(result.Steps[n-2].Command, browserCmdNetwork) {
			result.NetworkOutput = parseNetworkRequests(result.Steps[n-2].Result)
		}
	}
	if !screenshotRequested {
		return
	}
	// The screenshot step sits one further back — right before errors.
	// Read the file the runner wrote to screenshotPath; the step's own
	// JSON result carries no usable image data on this CLI, so bytes +
	// dimensions come from the file itself (png.DecodeConfig), not from
	// Steps[...].Result.
	n := len(result.Steps)
	if n < 5 || !isCommand(result.Steps[n-5].Command, browserCmdScreenshot) {
		return
	}
	data, readErr := os.ReadFile(screenshotPath)
	if readErr != nil {
		return
	}
	width, height := 0, 0
	if cfg, cfgErr := png.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
		width, height = cfg.Width, cfg.Height
	}
	result.Screenshot = &BrowserScreenshotResult{PNG: data, Width: width, Height: height}
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
// an optional [screenshot screenshotPath] + [errors] [console]
// [network requests] [close]. Any open/close/eval in the caller's
// commands is silently dropped — the canonical wrappers are the only
// lifecycle markers, and dedicated commands replace eval. screenshotPath
// == "" omits the screenshot step entirely.
func buildCanonicalBatch(url string, commands [][]string, screenshotPath string) [][]string {
	inner := make([][]string, 0, len(commands))
	for _, cmd := range commands {
		if len(cmd) == 0 {
			continue
		}
		switch cmd[0] {
		case browserCmdOpen, browserCmdClose:
			// Strip — caller should not manage lifecycle.
			continue
		case browserCmdEval:
			// Strip — dedicated commands produce structured output.
			continue
		}
		inner = append(inner, cmd)
	}
	batch := make([][]string, 0, len(inner)+6)
	batch = append(batch, []string{browserCmdOpen, url})
	batch = append(batch, inner...)
	if screenshotPath != "" {
		batch = append(batch, []string{browserCmdScreenshot, screenshotPath})
	}
	batch = append(batch,
		[]string{browserCmdErrors},
		[]string{browserCmdConsole},
		[]string{browserCmdNetwork, browserSubcmdRequests, browserFlagStatus, browserStatusRange4xx5xx},
		[]string{browserCmdClose},
	)
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
