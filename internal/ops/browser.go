// Package ops — BrowserBatch walks one page on the container's warm browser.
// The runner keeps agent-browser's daemon and Chrome in one bounded systemd
// service between calls, serialises calls on a lock and recycles the service
// after any call that did not end cleanly. Output is capped and partial walks
// never populate successful output fields.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// BrowserBatchInput is the caller-facing input for a browser walk.
type BrowserBatchInput struct {
	// URL is the page to open. Required.
	URL string `json:"url"`

	// Commands are the inner agent-browser commands to run AFTER open and
	// the first-paint wait and BEFORE the auto-appended [errors], [console],
	// [network requests]. Each element is one agent-browser command as a
	// string array — e.g. ["snapshot", "-i", "-c"], ["click", "#submit"],
	// ["find", "role", "link", "click", "--name", "Users"]. Any "open",
	// "close", "tab" or "window" elements do not run; each comes back first
	// in the result's steps as a failed invalid-command step. An "eval"
	// step's result is its value as JSON text (evalValuesAsText). A
	// leading ["set", "viewport", w, h] runs before the open instead; without
	// one the page loads at browserDefaultViewport, whatever an earlier call
	// set.
	Commands [][]string `json:"commands,omitempty"`

	// TimeoutSeconds bounds the whole call, the wait for the browser
	// included, and a slow page's load: an open agent-browser gave up on
	// while the page's server answers goes on within it (slow-page). Default
	// 120, max 300.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`

	// Isolation is how much of earlier calls' history the page sees:
	// BrowserIsolationTab (default), BrowserIsolationFreshContext or
	// BrowserIsolationNewBrowser.
	Isolation string `json:"isolation,omitempty"`

	// Screenshot, when true, inserts a screenshot step right
	// after Commands and before the errors/console/network-requests
	// tail. The captured PNG is returned via BrowserBatchResult.Screenshot.
	Screenshot bool `json:"screenshot,omitempty"`

	// FullPage takes the screenshot of the whole page instead of the
	// viewport; it implies Screenshot.
	FullPage bool `json:"fullPage,omitempty"`
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
	// ErrorKindInvalidCommand is a command agent-browser does not know —
	// an unknown command, subcommand or find action: the call was wrong,
	// not the page.
	ErrorKindInvalidCommand = "invalid-command"
	// ErrorKindStaleRef is an @eN ref that no snapshot of the step's tab
	// made — a ref from an earlier call's tab or one guessed before its
	// snapshot ran.
	ErrorKindStaleRef = "stale-ref"
	// ErrorKindScriptTimeout is an eval whose promise never settled before
	// agent-browser's CDP timeout while the page still answered a trivial
	// evaluate (runBrowserSteps' liveness probe): the script hung, not the
	// page — e.g. a wait on every animation's finished, which a looping one
	// never gives. It is stamped by the walk, never read off the error text.
	ErrorKindScriptTimeout = "script-timeout"
	// ErrorKindSlowPage is a step the daemon did not answer while a plain
	// request to the page got an answer (an open of a page whose server is
	// slow): the page, not Chrome. Stamped by the walk; the walk goes on
	// within the call's timeout and the browser is kept.
	ErrorKindSlowPage = "slow-page"
	// ErrorKindServerError is a page whose server answered 5xx: the open
	// that found it, and each caller command after it, which did not run.
	// Stamped by the walk; the browser is kept.
	ErrorKindServerError = "server-error"
	ErrorKindOther       = "other"
)

// classifyStepError maps one step's raw agent-browser error text to an
// ErrorKind. Patterns are best-effort substrings, checked case-insensitive.
// cdpWedgeSignals is checked FIRST and wins over every other pattern:
// "CDP command timed out" contains "timed out" (which would otherwise
// match the generic timeout case) but must classify as wedge — those
// signals are also what scanStepsForCDPWedge uses to recycle the browser,
// so the two must never disagree about which steps are wedge-related.
func classifyStepError(msg string) string {
	lc := strings.ToLower(msg)
	for _, sig := range cdpWedgeSignals {
		if strings.Contains(lc, strings.ToLower(sig)) {
			return ErrorKindWedge
		}
	}
	switch {
	case strings.Contains(lc, "unknown command"), strings.Contains(lc, "unknown subcommand"), strings.Contains(lc, "unknown action"):
		return ErrorKindInvalidCommand
	case strings.Contains(lc, "unknown ref"):
		return ErrorKindStaleRef
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

// BrowserBatchResult is the structured return value. FreshBrowser says the
// call ran in a Chrome started for it (new-browser, a warm browser that could
// not be reused, or none running); BrowserRecycled says the browser was
// stopped as the call ended.
type BrowserBatchResult struct {
	URL             string                   `json:"url"`
	Steps           []BrowserStepResult      `json:"steps,omitempty"`
	ErrorsOutput    json.RawMessage          `json:"errorsOutput,omitempty"`
	ConsoleOutput   json.RawMessage          `json:"consoleOutput,omitempty"`
	NetworkOutput   []NetworkRequest         `json:"networkOutput,omitempty"`
	Screenshot      *BrowserScreenshotResult `json:"screenshot,omitempty"`
	DurationMs      int64                    `json:"durationMs"`
	FreshBrowser    bool                     `json:"freshBrowser,omitempty"`
	BrowserRecycled bool                     `json:"browserRecycled,omitempty"`
	OutputTruncated bool                     `json:"outputTruncated,omitempty"`
	Message         string                   `json:"message,omitempty"`
	// FullResult is the file holding the whole result when the reply cut its
	// outputs to fit the model's text. Set by the tools layer.
	FullResult string `json:"fullResult,omitempty"`
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
	// CapturedHeight is set when the capture was taller than
	// browserScreenshotMaxHeight: how tall it was. The image keeps its top
	// Height px.
	CapturedHeight int `json:"capturedHeight,omitempty"`
	// Picture names the screenshot where the Mate's own pictures are kept
	// ("shot-3") — how a change's description shows it, as
	// ![what it shows](shot-3). Set by the tools layer, which keeps it.
	Picture string `json:"picture,omitempty"`
	// KeptFor is how long the picture is kept after it is taken ("7 days"),
	// so the Mate knows how long it has to show it. Set with Picture.
	KeptFor string `json:"keptFor,omitempty"`
}

// NetworkRequest is one entry from BrowserBatchResult.NetworkOutput.
// The auto-appended tail step already asks agent-browser for the 4xx/5xx
// slice (`network requests --status 400-599`, see browserFlagStatus);
// parseNetworkRequests keeps the `Status >= 400` filter too so the
// meaning is pinned in Go even if the CLI flag's behavior ever drifts.
//
// Field names mirror agent-browser's Playwright-style request/response
// model (route/har/requests support implies that lineage — see
// `network --help`: route/unroute/requests/har). In agent-browser 0.35.1
// live `--status 400-599` output, entries here
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

// browserRunner abstracts the container's browser for testability.
type browserRunner interface {
	LookPath() (string, error)
	// Acquire waits, until ctx ends, for the container's browser, readies it
	// for one call at the given isolation and returns release. release(true)
	// hands the browser back warm; release(false) recycles it. fresh reports
	// that the browser was started for this call instead of reused.
	Acquire(ctx context.Context, isolation string) (release func(reuse bool) error, fresh bool, err error)
	// Run walks the JSON [][]string batch in stdin and answers with the
	// JSON []BrowserStepResult of the steps that finished. err is set only
	// when the walk could not finish (ctx ended, a step got no answer).
	Run(ctx context.Context, stdin string, timeout time.Duration) (stdout, stderr string, truncated bool, err error)
}

// Isolation levels, from cheapest. A tab shares cookies, storage, HTTP cache
// and service workers with earlier calls, the way one developer's browser
// does; a fresh context shares none of them; a new browser also leaves
// behind Chrome's process state (a wedge, GPU or memory).
const (
	BrowserIsolationTab          = "tab"
	BrowserIsolationFreshContext = "fresh-context"
	BrowserIsolationNewBrowser   = "new-browser"
)

// resolveBrowserIsolation maps "" to the default tab and refuses any other
// value outside the three levels.
func resolveBrowserIsolation(isolation string) (string, error) {
	switch isolation {
	case "":
		return BrowserIsolationTab, nil
	case BrowserIsolationTab, BrowserIsolationFreshContext, BrowserIsolationNewBrowser:
		return isolation, nil
	}
	return "", platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("isolation %q is not one of tab, fresh-context, new-browser", isolation),
		"Omit isolation for a tab in the warm browser; fresh-context for a first visit; new-browser after a wedge",
	)
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

// browserRun is the active runner. Tests override via OverrideBrowserRunnerForTest.
var browserRun browserRunner = execBrowserRunner{}

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
	// browserCmdClose is the agent-browser close command; refused in
	// caller commands, since closing would end the shared warm browser.
	browserCmdClose = "close"
	// browserCmdOpen is the agent-browser open command.
	browserCmdOpen = "open"
	// browserCmdSet and browserSubcmdViewport form [set viewport w h].
	browserCmdSet         = "set"
	browserSubcmdViewport = "viewport"
	// browserCmdTab and browserCmdWindow open, switch and close tabs and
	// contexts; refused in caller commands, since the tool keeps the warm
	// browser's base tab and closes every other one when a call ends.
	browserCmdTab    = "tab"
	browserCmdWindow = "window"
	// browserCmdEval runs a script in the page; its step's result is the
	// value as JSON text, cut at browserEvalTextCap (evalValuesAsText).
	browserCmdEval = "eval"
	// browserEvalTextCap bounds an eval value's text in the result: room for
	// a store's state or a list of computed styles, not a page's worth.
	browserEvalTextCap = 10000
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
	// In agent-browser 0.35.1, each request entry in
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
	// NOT honoured in agent-browser 0.35.1: [screenshot --annotate
	// <path>] fails with "Element not found: --annotate" (the flag is read
	// as the selector) and [screenshot <path> --annotate] reports success
	// but writes no file; the plain [screenshot <path>] step writes the
	// PNG. buildCanonicalBatch therefore emits the plain form.
	browserCmdScreenshot = "screenshot"
	// browserFlagFull makes the screenshot step capture the whole page
	// (agent-browser 0.38.2 `screenshot --help`: "--full, -f  Capture full
	// page (not just viewport)"). Each step runs as its own CLI call, so the
	// flag is honoured: measured 2026-10-16, [screenshot --full <path>] of
	// docs.zerops.io/references/import wrote a 1365×10870 PNG.
	browserFlagFull = "--full"
	// browserScreenshotMaxHeight bounds a returned image's height: the
	// Claude API refuses an image with a side over 8000 px, and a full page
	// runs past it (docs.zerops.io/references/import at 1365 px wide is
	// 1365×10870 px, 2.6 MB; agent-browser 0.38.2, 2026-10-16).
	browserScreenshotMaxHeight = 8000
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

// containerMemoryMax reads the container's cgroup-v2 memory ceiling: limit 0
// when unbounded, ok=false when unreadable (not a Linux container).
func containerMemoryMax() (limit int64, ok bool) {
	raw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(raw))
	if s == "max" {
		return 0, true
	}
	limit, err = strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return limit, true
}

// containerFreeRAMBytes returns free bytes (cgroup-v2 memory.max − .current),
// ok=false when unreadable (not a Linux container) or unbounded. Best-effort:
// callers proceed on !ok.
func containerFreeRAMBytes() (int64, bool) {
	maxB, ok := containerMemoryMax()
	if !ok {
		return 0, false
	}
	if maxB == 0 { // unbounded ceiling — effectively unlimited headroom
		return 1 << 62, true
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

// cdpWedgeSignals are per-step error substrings that indicate Chrome
// wedged behind a stuck CDP connection even when the daemon returned
// exit 0, or a daemon that took a command and never answered (agent-browser's
// client gives up after at least 30 s). A match on any of these recycles the
// browser so the next call doesn't reattach to the same zombie Chrome.
var cdpWedgeSignals = []string{
	cdpCommandTimedOut,
	"Target closed",
	"Protocol error",
	"no response came back",
}

// cdpCommandTimedOut is agent-browser's answer to a CDP command past its
// timeout — on an eval, also a promise that never settled (evalOnLivePage).
const cdpCommandTimedOut = "CDP command timed out"

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

// BrowserBatch walks one page on the container's warm browser. See package
// doc for the lifecycle contract.
func BrowserBatch(ctx context.Context, input BrowserBatchInput) (*BrowserBatchResult, error) {
	if strings.TrimSpace(input.URL) == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"url is required",
			"Pass the subdomain URL to walk (e.g. the appstage zerops.app URL from zerops_discover)",
		)
	}
	isolation, err := resolveBrowserIsolation(input.Isolation)
	if err != nil {
		return nil, err
	}

	timeout := resolveBrowserTimeout(input.TimeoutSeconds)
	// One deadline bounds the whole call: the wait for the browser, the
	// walk and the paint wait inside it.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	wantScreenshot := input.Screenshot || input.FullPage
	screenshotPath, cleanupScreenshot, err := prepareScreenshotTempFile(wantScreenshot)
	if err != nil {
		return nil, err
	}
	defer cleanupScreenshot()

	batch, refused := buildCanonicalBatch(input.URL, input.Commands, screenshotPath, input.FullPage)
	stdinBytes, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	if _, err := browserRun.LookPath(); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"agent-browser not found on PATH",
			"This tool is only available inside the ZCP container. agent-browser is pre-installed there.",
		)
	}

	release, fresh, err := browserRun.Acquire(ctx, isolation)
	if err != nil {
		return nil, fmt.Errorf("acquire browser: %w", err)
	}

	// Grant the Chrome renderer RAM headroom before the page loads — on a
	// tightly provisioned agent container the 300-700 MB spike otherwise
	// thrashes the cgroup ceiling and every CDP command times out.
	// Best-effort + auto-reverting (see browserScale); a scale failure never
	// blocks the browser, it just degrades to the pre-fix behaviour.
	if err := browserScale(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "zerops_browser: RAM pre-scale failed (continuing): %v\n", err)
	}

	start := time.Now()
	stdout, stderr, truncated, runErr := browserRun.Run(ctx, string(stdinBytes), timeout)
	duration := time.Since(start)

	result := &BrowserBatchResult{
		URL:             input.URL,
		DurationMs:      duration.Milliseconds(),
		FreshBrowser:    fresh,
		OutputTruncated: truncated,
	}

	done, recycle := classifyRunResult(result, stdout, stderr, runErr, timeout)
	// Only a walk that ended on its own hands the browser back warm.
	reuse := runErr == nil && !recycle
	result.BrowserRecycled = !reuse
	// agent-browser cannot dispose a browser context, so the browser that
	// holds a fresh context ends with the call instead of keeping it.
	if err := release(reuse && isolation != BrowserIsolationFreshContext); err != nil {
		fmt.Fprintf(os.Stderr, "zerops_browser: browser release: %v\n", err)
	}
	if !done && runErr == nil {
		populateBrowserBatchOutputs(result, wantScreenshot, screenshotPath)
	}
	if result.Message == "" {
		result.Message = pageVerdict(result.Steps, timeout)
	}
	omitScreenshotTempPath(result, screenshotPath)
	evalValuesAsText(result.Steps)
	// The refused commands come first: they were turned away before the walk.
	result.Steps = append(refused, result.Steps...)
	return result, nil
}

// omitScreenshotTempPath takes the temp file the screenshot was written to out
// of the result: BrowserBatch deletes it as it returns, so a path an agent
// read there names a picture that is already gone — linked in an answer, it
// drew "Image unavailable". The picture itself goes back as its own image
// block. Only the step BrowserBatch appended is touched: a screenshot the
// caller asked for at a path of its own keeps that path, which outlives the
// call.
func omitScreenshotTempPath(result *BrowserBatchResult, path string) {
	if result == nil || path == "" {
		return
	}
	const said = "the screenshot file"
	for i := range result.Steps {
		step := &result.Steps[i]
		if !isCommand(step.Command, browserCmdScreenshot) || !slices.Contains(step.Command, path) {
			continue
		}
		step.Command = slices.DeleteFunc(slices.Clone(step.Command), func(arg string) bool { return arg == path })
		step.Result = nil
		if step.Error != nil {
			text := strings.ReplaceAll(*step.Error, path, said)
			step.Error = &text
		}
	}
	result.Message = strings.ReplaceAll(result.Message, path, said)
}

// browserHTTPStatus finds the status a server-error step names.
var browserHTTPStatus = regexp.MustCompile(`HTTP (\d{3})`)

// pageVerdict says what a walk found of the page itself when it was no wedge:
// a slow page (its time to first byte, and that the walk went on within
// timeout) or a page whose server answered 5xx (what did not run, and where
// to look). "" otherwise.
func pageVerdict(steps []BrowserStepResult, timeout time.Duration) string {
	for _, s := range steps {
		if s.Error == nil {
			continue
		}
		switch s.ErrorKind {
		case ErrorKindServerError:
			status := "5xx"
			if m := browserHTTPStatus.FindStringSubmatch(*s.Error); m != nil {
				status = m[1]
			}
			return fmt.Sprintf("The page answered HTTP %s, a server error: the commands after it did not run; its screenshot, errors, console and network requests did. "+
				"Read the server's logs (zerops_logs, or zerops_dev_server action=logs hostname=\"<service>\" for a dev server), fix it, then call again.", status)
		case ErrorKindSlowPage:
			_, note, _ := strings.Cut(*s.Error, browserSlowPageMarker)
			return fmt.Sprintf("The page was slow, not Chrome: %s. The walk went on within timeoutSeconds (%d s); raise it for a page this slow, or call again once the server has warmed up.",
				strings.TrimSpace(note), int(timeout.Seconds()))
		}
	}
	return ""
}

// classifyRunResult inspects one walk's raw outcome and mutates result
// accordingly: fork exhaustion, a deadline, and any other failed walk are
// recorded in result.Message; stdout (when non-empty) is parsed into
// result.Steps with each step's ErrorKind classified; a CDP-wedge signal
// buried in step errors (even on a clean walk) asks for a recycle too.
//
// Returns done=true when the reporting fields must stay empty (every branch
// except "steps parsed cleanly, keep going"), and recycle=true when the
// browser must not be handed to the next call.
func classifyRunResult(result *BrowserBatchResult, stdout, stderr string, runErr error, timeout time.Duration) (done, recycle bool) {
	// Fork-exhaustion detection runs BEFORE exit-code checks — a step
	// sometimes answers after logging the error, sometimes not at all.
	// Check stderr only: stdout is JSON containing user-controlled text
	// (page titles, console logs) that could match a loose substring.
	if isForkExhausted(stderr) {
		result.Message = "Fork budget exhausted (agent-browser or Chrome could not spawn a process). " +
			"The browser is recycled. Inspect container task usage before retrying."
		if runErr != nil {
			result.Message += fmt.Sprintf("\nBatch error: %v", runErr)
		}
		return true, true
	}

	// Deadline: keep the steps that finished, so the agent sees how far the
	// page got, and recycle the browser the cut step may still be busy in.
	if runErr != nil && errors.Is(runErr, context.DeadlineExceeded) {
		if strings.TrimSpace(stdout) != "" && json.Unmarshal([]byte(stdout), &result.Steps) == nil {
			classifyStepErrors(result.Steps)
		}
		result.Message = fmt.Sprintf("agent-browser timed out after %s (%v); %d steps finished. The browser is recycled. "+
			"Retry with a shorter command sequence, or raise timeoutSeconds (max %ds).",
			timeout, runErr, len(result.Steps), int(browserMaxTimeout.Seconds()))
		return true, true
	}

	// Any other failed walk: record the raw error in Message BEFORE
	// attempting to parse stdout, so a subsequent parse failure does not
	// overwrite it.
	if runErr != nil {
		result.Message = fmt.Sprintf("agent-browser exited with error: %v", runErr)
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			result.Message += "\nstderr: " + trimmed
		}
	}

	// Parse structured output if any. On parse failure, APPEND to Message
	// rather than overwrite — preserves any existing error context.
	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &result.Steps); err != nil {
			parseMsg := fmt.Sprintf("failed to parse agent-browser --json output: %v\nraw output:\n%s", err, stdout)
			if result.Message == "" {
				result.Message = parseMsg
			} else {
				result.Message += "\n" + parseMsg
			}
			return true, runErr != nil
		}
		// Classify every step's error text into a coarse ErrorKind — this
		// runs regardless of overall run success, since Steps is preserved
		// for diagnosis on both success and failure paths below.
		classifyStepErrors(result.Steps)
	}

	// CDP-wedge signals buried in per-step errors (even on a clean walk)
	// indicate Chrome hung behind a stuck CDP connection. The daemon is
	// alive; Chrome isn't responding. The next call must get a fresh one.
	if sig, hit := scanStepsForCDPWedge(result.Steps); hit {
		result.Message = fmt.Sprintf(
			"Chrome wedged behind CDP (signal: %s). The browser is recycled. "+
				"Retry with a new batch.",
			sig,
		)
		if runErr != nil {
			result.Message += fmt.Sprintf("\nBatch error: %v", runErr)
		}
		// Intentionally do not populate ErrorsOutput / ConsoleOutput —
		// the partial walk must not look like a successful one.
		return true, true
	}

	return runErr != nil, runErr != nil
}

// classifyStepErrors stamps ErrorKind on every step that carries an
// Error, using classifyStepError. A kind the walk already stamped
// (ErrorKindScriptTimeout) stands: the text alone would say wedge.
func classifyStepErrors(steps []BrowserStepResult) {
	for i := range steps {
		if steps[i].Error != nil && steps[i].ErrorKind == "" {
			steps[i].ErrorKind = classifyStepError(*steps[i].Error)
		}
	}
}

// populateBrowserBatchOutputs fills ErrorsOutput/ConsoleOutput/
// NetworkOutput/Screenshot from a successful run's Steps. Tail is
// always errors, console, network requests (from
// buildCanonicalBatch), with an optional screenshot step one further
// back when requested — indexed from the end so an arbitrary number of
// caller commands before it doesn't shift the offsets. Callers must
// only invoke this when runErr == nil: on a non-zero exit these fields
// must stay unpopulated so a partial walk never looks like a successful
// one.
func populateBrowserBatchOutputs(result *BrowserBatchResult, screenshotRequested bool, screenshotPath string) {
	if n := len(result.Steps); n >= 3 {
		if isCommand(result.Steps[n-3].Command, browserCmdErrors) {
			result.ErrorsOutput = result.Steps[n-3].Result
		}
		if isCommand(result.Steps[n-2].Command, browserCmdConsole) {
			result.ConsoleOutput = result.Steps[n-2].Result
		}
		if isCommand(result.Steps[n-1].Command, browserCmdNetwork) {
			result.NetworkOutput = parseNetworkRequests(result.Steps[n-1].Result)
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
	if n < 4 || !isCommand(result.Steps[n-4].Command, browserCmdScreenshot) {
		return
	}
	data, readErr := os.ReadFile(screenshotPath)
	if readErr != nil {
		return
	}
	shot := &BrowserScreenshotResult{PNG: data}
	if cfg, cfgErr := png.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
		shot.Width, shot.Height = cfg.Width, cfg.Height
	}
	if shot.Height > browserScreenshotMaxHeight {
		if err := cutScreenshot(shot); err != nil {
			result.Message = fmt.Sprintf("The screenshot is taller than %d px and could not be cut to it: %v", browserScreenshotMaxHeight, err)
			return
		}
	}
	result.Screenshot = shot
}

// cutScreenshot keeps the top browserScreenshotMaxHeight px of a taller
// capture and records how tall the capture was in CapturedHeight.
func cutScreenshot(shot *BrowserScreenshotResult) error {
	img, err := png.Decode(bytes.NewReader(shot.PNG))
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	sub, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	})
	if !ok {
		return fmt.Errorf("cut screenshot: %T cannot be cut", img)
	}
	b := img.Bounds()
	var buf bytes.Buffer
	if err := png.Encode(&buf, sub.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+browserScreenshotMaxHeight))); err != nil {
		return fmt.Errorf("encode screenshot: %w", err)
	}
	shot.PNG = buf.Bytes()
	shot.CapturedHeight = shot.Height
	shot.Height = browserScreenshotMaxHeight
	return nil
}

// scanStepsForCDPWedge returns the first matching signal substring and
// true if any step's error field contains one of the CDP-wedge markers
// in cdpWedgeSignals. The "" sentinel in the return means "no match". An
// eval the walk found hung on a live page (ErrorKindScriptTimeout), a slow
// page (ErrorKindSlowPage) and a server-error page (ErrorKindServerError)
// are no wedge.
func scanStepsForCDPWedge(steps []BrowserStepResult) (string, bool) {
	for _, s := range steps {
		if s.Error == nil || s.ErrorKind == ErrorKindScriptTimeout || s.ErrorKind == ErrorKindSlowPage || s.ErrorKind == ErrorKindServerError {
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

// buildCanonicalBatch assembles [open url] + the first-paint wait +
// the caller's other commands + an optional [screenshot [--full] screenshotPath] +
// [errors] [console] [network requests]. Any open/close/tab/window in
// the caller's commands does not run and comes back in refused instead, as a
// failed invalid-command step naming why and what to use (refusedCommands) —
// the canonical open is the only navigation the tool owns, a close would end
// the warm browser, and tab and window could close or switch its base tab.
// screenshotPath == "" omits the
// screenshot step entirely; fullPage captures the whole page, not the
// viewport. A leading [set viewport w h] runs before the open, so the page
// loads at that size instead of reflowing from the default width; without
// one, browserDefaultViewport runs there, so no call inherits the viewport an
// earlier call set. The steps come back in the order they ran, the caller's
// commands in the order sent.
func buildCanonicalBatch(url string, commands [][]string, screenshotPath string, fullPage bool) (batch [][]string, refused []BrowserStepResult) {
	inner := make([][]string, 0, len(commands))
	for _, cmd := range commands {
		if len(cmd) == 0 {
			continue
		}
		if why, ok := refusedCommands[cmd[0]]; ok {
			refused = append(refused, BrowserStepResult{Command: cmd, Error: &why, ErrorKind: ErrorKindInvalidCommand})
			continue
		}
		inner = append(inner, cmd)
	}
	batch = make([][]string, 0, len(inner)+7)
	if len(inner) > 0 && isSetViewport(inner[0]) {
		batch, inner = append(batch, inner[0]), inner[1:]
	} else {
		batch = append(batch, browserDefaultViewport)
	}
	batch = append(batch, []string{browserCmdOpen, url}, browserPaintWait)
	batch = append(batch, inner...)
	if screenshotPath != "" {
		shot := []string{browserCmdScreenshot}
		if fullPage {
			shot = append(shot, browserFlagFull)
		}
		batch = append(batch, append(shot, screenshotPath))
	}
	batch = append(batch,
		[]string{browserCmdErrors},
		[]string{browserCmdConsole},
		[]string{browserCmdNetwork, browserSubcmdRequests, browserFlagStatus, browserStatusRange4xx5xx},
	)
	return batch, refused
}

// refusedCommands maps each caller command the tool does not run to the
// error its refused step carries: why, and what to use instead.
var refusedCommands = map[string]string{
	browserCmdOpen:   "not run: the call opens its url itself; follow a link with click or find, or pass another page as url in a new call",
	browserCmdClose:  "not run: the tool closes the call's tabs itself and keeps the shared browser",
	browserCmdTab:    "not run: the tool owns the call's tab; for a clean context pass isolation \"fresh-context\"",
	browserCmdWindow: "not run: the tool owns the call's tab; for a clean context pass isolation \"fresh-context\"",
}

// evalValuesAsText turns each answered eval step's result into its value as
// JSON text: agent-browser answers {origin, result: <value>} (0.39.1, undefined
// as null), and the value's shape is whatever the script returned. Text longer
// than browserEvalTextCap keeps its start and says how long it was. A script
// that threw is already a failed step carrying the page's error.
func evalValuesAsText(steps []BrowserStepResult) {
	for i := range steps {
		step := &steps[i]
		if !isCommand(step.Command, browserCmdEval) || !step.Success {
			continue
		}
		var data struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(step.Result, &data) != nil || data.Result == nil {
			continue
		}
		var value bytes.Buffer
		if json.Compact(&value, data.Result) != nil {
			continue
		}
		text := value.String()
		if len(text) > browserEvalTextCap {
			kept := strings.ToValidUTF8(text[:browserEvalTextCap], "")
			text = fmt.Sprintf("%s… [cut: the value's JSON is %d bytes; the first %d are shown]", kept, len(text), len(kept))
		}
		quoted, err := json.Marshal(text)
		if err != nil {
			continue
		}
		step.Result = quoted
	}
}

// browserDefaultViewport is the size a fresh Chrome opens at (measured
// 2026-10-16 with agent-browser 0.38.2 on a Zerops container: a new browser
// that set no viewport loaded the page at innerWidth 1280 and screenshot it
// 1280×577). The warm Chrome keeps a set viewport for later calls, so a call
// whose commands do not set one first runs this before the open.
var browserDefaultViewport = []string{browserCmdSet, browserSubcmdViewport, "1280", "577"}

// isSetViewport reports whether a command is [set viewport ...].
func isSetViewport(cmd []string) bool {
	return len(cmd) > 1 && cmd[0] == browserCmdSet && cmd[1] == browserSubcmdViewport
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
