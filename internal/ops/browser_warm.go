package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
)

// The warm browser: one transient systemd service holds agent-browser's
// daemon and its Chrome between calls. systemd bounds its tasks, CPU and age
// and kills its whole cgroup on stop; the daemon exits by itself when idle.
// Its socket lives in the service's RuntimeDirectory, so once the service is
// gone no client can respawn a daemon outside it.
const (
	browserUnit       = "zcp-browser.service"
	browserRuntimeDir = "/run/zcp-browser"
	// browserTmpDir holds Chrome's profile; the service removes it on stop.
	browserTmpDir = "/tmp/zcp-browser"
	// browserMarkerPath says whether a call is in the browser ("busy") or
	// the last one ended cleanly ("idle", with the service's CPU time then);
	// its mtime is the last call's end.
	// It lives in the RuntimeDirectory, so it dies with the service.
	browserMarkerPath = browserRuntimeDir + "/zcp-call"
	browserMarkerIdle = "idle"
	browserMarkerBusy = "busy"

	// browserIdleTimeout is agent-browser's own idle exit for the daemon.
	browserIdleTimeout = 20 * time.Minute
	// browserMaxAge is the service's RuntimeMaxSec.
	browserMaxAge = time.Hour
	// browserBoundMargin keeps a call from starting on a browser about to
	// hit its idle exit.
	browserBoundMargin = 30 * time.Second
	browserTasksMax    = 1000
	// browserCPUPercentPerCPU caps the browser at 75% of the container's
	// CPUs, so a runaway page always leaves the agent some.
	browserCPUPercentPerCPU = 75
	// browserMemoryPercent caps the browser at 75% of the container's memory
	// at its start, so a page that grows without end is killed with it.
	browserMemoryPercent = 75
)

// browserPaintWait waits for the page's first contentful paint: it returns
// at once on a page that has painted and gives up after 5 s on one that never
// does, while the walk goes on. Its timeout is reported in the step's result.
var browserPaintWait = []string{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"}

// browserWaitTimedOut is agent-browser's error for a wait whose --timeout ran
// out (agent-browser 0.39.1).
const browserWaitTimedOut = "Wait timed out"

// browserNoPaint is the result of a paint wait that saw no paint.
var browserNoPaint = json.RawMessage(`"no first paint within 5 s; the walk went on"`)

// browserEnv is the whole environment of the warm browser's service and of
// every step sent to it. agent-browser restarts a daemon whose settings
// differ from the client's, so both sides get exactly this, never an
// inherited AGENT_BROWSER_* variable. It names no agent-browser session, so
// agent-browser itself never restores or saves a sign-in: zcp does
// (browser_signin.go).
func browserEnv(home, path string) []string {
	return []string{
		"HOME=" + home,
		"PATH=" + path,
		"TMPDIR=" + browserTmpDir,
		"AGENT_BROWSER_SOCKET_DIR=" + browserRuntimeDir,
		"AGENT_BROWSER_SESSION=default",
		"AGENT_BROWSER_IDLE_TIMEOUT_MS=" + strconv.FormatInt(browserIdleTimeout.Milliseconds(), 10),
		// No page sits in Chrome's back/forward cache, a renderer apiece: a
		// call's tabs close when it ends, so the cache only ever held the
		// kept sign-in's stub pages, for minutes, in the idle browser. The
		// same args on every command keep agent-browser's launch hash, so
		// it never relaunches Chrome for them.
		"AGENT_BROWSER_ARGS=" + browserChromeArgs,
	}
}

// browserChromeArgs are the Chrome flags the warm browser runs with.
const browserChromeArgs = "--disable-back-forward-cache"

// browserUnitArgs are the sudo arguments that start the warm browser: the
// daemon forks out of `agent-browser open about:blank` and its pid file
// makes it the service's main process. containerMemory is the container's
// memory ceiling in bytes, 0 when unknown. OOMScoreAdjust makes Chrome, not
// the agent, the first to go when the whole container runs out.
func browserUnitArgs(binary string, env []string, uid, cpus int, containerMemory int64) []string {
	args := make([]string, 0, 20+len(env))
	args = append(args,
		"-n", "systemd-run", "--quiet", "--collect",
		"--unit="+browserUnit,
		"--uid="+strconv.Itoa(uid),
		"--service-type=forking",
		"--property=PIDFile="+browserRuntimeDir+"/default.pid",
		"--property=RuntimeDirectory=zcp-browser",
		"--property=KillMode=control-group",
		"--property=KillSignal=SIGKILL",
		"--property=TasksMax="+strconv.Itoa(browserTasksMax),
		"--property=CPUQuota="+strconv.Itoa(browserCPUPercentPerCPU*max(cpus, 1))+"%",
		"--property=RuntimeMaxSec="+strconv.Itoa(int(browserMaxAge.Seconds())),
		"--property=ExecStopPost=/usr/bin/rm -rf "+browserTmpDir,
		"--property=OOMScoreAdjust=1000",
		// Every file it writes, the kept sign-in's cookies among them, is
		// its user's alone.
		"--property=UMask=0077",
	)
	if containerMemory > 0 {
		args = append(args, "--property=MemoryMax="+strconv.FormatInt(containerMemory*browserMemoryPercent/100, 10))
	}
	for _, kv := range env {
		args = append(args, "--setenv="+kv)
	}
	return append(args, binary, browserCmdOpen, "about:blank")
}

// warmBrowser is what a call knows about the browser before it starts.
type warmBrowser struct {
	Active  bool
	Age     time.Duration
	Marker  string
	IdleFor time.Duration
	// CPU is the service's CPU time so far, -1 when unknown; IdleCPU is the
	// part of it used since the last call ended, -1 when unknown.
	CPU     time.Duration
	IdleCPU time.Duration
}

// reusable reports whether a call at the given isolation and budget may run
// on the warm browser: it did not ask for a new browser, the browser is
// running, the last call in it ended cleanly, and neither its idle exit nor
// its max age can cut the call.
func (w warmBrowser) reusable(isolation string, budget time.Duration) bool {
	return isolation != BrowserIsolationNewBrowser &&
		w.Active && w.Marker == browserMarkerIdle &&
		w.IdleFor < browserIdleTimeout-browserBoundMargin &&
		w.Age+budget < browserMaxAge && w.quietWhileIdle()
}

// quietWhileIdle reports whether the browser used at most a tenth of a CPU
// while no call was in it. One that used more may still run a page: a popup
// agent-browser never tracked, or a hung renderer Chrome kept after its tab
// closed — or it was only closing a heavy page, which settleWhileIdle tells
// apart. A gap under a second is too short to judge.
func (w warmBrowser) quietWhileIdle() bool {
	return w.IdleCPU < 0 || w.IdleFor < time.Second || w.IdleCPU <= w.IdleFor/10
}

// reusableButBusy reports whether the warm browser would be reusable but for
// the CPU it used since the last call ended.
func (w warmBrowser) reusableButBusy(isolation string, budget time.Duration) bool {
	busy := w
	busy.IdleCPU = -1
	return busy.reusable(isolation, budget) && !w.quietWhileIdle()
}

// Closing a heavy page — a WebGL game, a large app — keeps Chrome busy for
// seconds after a call's tabs close (4.7 s of CPU in the 6 s after a three.js
// example on a Mate, then nothing), so a call that follows within that time
// would see a browser that used more than a tenth of a CPU and recycle it,
// paying a launch and a restore. settleWhileIdle watches the service's CPU in
// windows of browserSettleWindow, up to browserSettleBound: a browser whose
// use in one window falls to a tenth of a CPU has settled and is kept; one
// still above it at the bound runs a page of its own and is recycled.
const (
	browserSettleWindow = 250 * time.Millisecond
	browserSettleBound  = 4 * time.Second
)

// settleWhileIdle reports whether the browser's CPU use fell to a tenth of a
// CPU within bound, read through cpuOf in windows of window; false when a
// read fails or ctx ends.
func settleWhileIdle(ctx context.Context, cpuOf func(context.Context) (time.Duration, error), window, bound time.Duration) bool {
	before, err := cpuOf(ctx)
	if err != nil || before < 0 {
		return false
	}
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(window):
		}
		now, err := cpuOf(ctx)
		if err != nil || now < 0 {
			return false
		}
		if now-before <= window/10 {
			return true
		}
		before = now
	}
	return false
}

// browserFreshReason says why a call does not run on the warm browser; ""
// when it does. settled is settleWhileIdle's verdict for a browser that was
// reusable but busy, and signInHeld whether the warm browser holds the kept
// sign-in the call needs.
func browserFreshReason(w warmBrowser, isolation string, budget time.Duration, settled, signInHeld bool) string {
	switch {
	case isolation == BrowserIsolationNewBrowser:
		return "the call asked for a new browser"
	case !w.Active:
		return "no browser was running"
	case w.Marker != browserMarkerIdle:
		return fmt.Sprintf("the last call did not end cleanly (marker %q)", w.Marker)
	case w.IdleFor >= browserIdleTimeout-browserBoundMargin:
		return fmt.Sprintf("it idled %s, past its idle exit", w.IdleFor.Round(time.Second))
	case w.Age+budget >= browserMaxAge:
		return fmt.Sprintf("it is %s old, too old for the call's %s", w.Age.Round(time.Second), budget.Round(time.Second))
	case !w.quietWhileIdle() && !settled:
		return fmt.Sprintf("it used %s of CPU in %s idle and was still busy after %s", w.IdleCPU.Round(time.Millisecond), w.IdleFor.Round(100*time.Millisecond), browserSettleBound)
	case !signInHeld:
		return "it does not hold the kept sign-in"
	}
	return ""
}

// browserIdleMarker is the marker a clean call leaves: idle, with the
// service's CPU time when the call ended.
func browserIdleMarker(cpu time.Duration) string {
	return browserMarkerIdle + " " + strconv.FormatInt(cpu.Nanoseconds(), 10)
}

// parseBrowserMarker reads a marker back: its state and the CPU time it
// carries, -1 when it carries none.
func parseBrowserMarker(raw string) (string, time.Duration) {
	state, cpu, _ := strings.Cut(strings.TrimSpace(raw), " ")
	nsec, err := strconv.ParseInt(cpu, 10, 64)
	if err != nil {
		return state, -1
	}
	return state, time.Duration(nsec)
}

// browserSetupSteps ready the browser for one call: clear the logs earlier
// calls left (they are daemon-wide), then open the call's own tab — in the
// warm default context, or in a fresh browser context.
func browserSetupSteps(isolation string) [][]string {
	steps := [][]string{
		{browserCmdConsole, "--clear"},
		{browserCmdErrors, "--clear"},
		{browserCmdNetwork, browserSubcmdRequests, "--clear"},
	}
	if isolation == BrowserIsolationFreshContext {
		return append(steps, []string{"window", "new"})
	}
	return append(steps, []string{"tab", "new"})
}

// browserCleanupSteps end a call from its `tab list` answer: switch to the
// base tab first (closing beside a hung active tab stalls), then close every
// other tab, popups included.
func browserCleanupSteps(tabList string) ([][]string, error) {
	var answer struct {
		Success bool `json:"success"`
		Data    struct {
			Tabs []struct {
				TabID string `json:"tabId"`
			} `json:"tabs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(tabList), &answer); err != nil {
		return nil, fmt.Errorf("tab list: %w", err)
	}
	if !answer.Success || len(answer.Data.Tabs) == 0 {
		return nil, errors.New("tab list: no tabs")
	}
	steps := [][]string{{"tab", answer.Data.Tabs[0].TabID}}
	for _, tab := range answer.Data.Tabs[1:] {
		steps = append(steps, []string{"tab", "close", tab.TabID})
	}
	return steps, nil
}

// walkBrowser runs housekeeping steps and fails unless every one succeeded.
func walkBrowser(ctx context.Context, steps [][]string, run browserStepExec) error {
	stdout, stderr, _, err := runBrowserSteps(ctx, steps, run)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
	}
	var results []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return fmt.Errorf("browser steps: %w", err)
	}
	for _, r := range results {
		if !r.Success {
			reason := "failed"
			if r.Error != nil {
				reason = *r.Error
			}
			return fmt.Errorf("browser step %q: %s", strings.Join(r.Command, " "), reason)
		}
	}
	return nil
}

// closeCallTabs closes every tab but the base one.
func closeCallTabs(ctx context.Context, run browserStepExec) error {
	list, _, err := run(ctx, []string{"tab", "list"})
	if err != nil {
		return fmt.Errorf("tab list: %w", err)
	}
	steps, err := browserCleanupSteps(list)
	if err != nil {
		return err
	}
	return walkBrowser(ctx, steps, run)
}

// handBackBrowser readies the browser for the next call after a clean one:
// it saves the sign-in into stateDir when the browser keeps one ("" when it
// does not; presencePath is the browser's presence, browserPresence, and
// ownTab the call's own tab), closes the call's tabs, and only then marks the
// browser idle, so a call that follows finds the save done. The save has its
// own bound; one that fails or runs out is logged and hands the browser back
// all the same: the last good save stands, and the browser is not restarted.
// note is what the call's result says when the save found Chrome relaunched
// empty during the call.
func handBackBrowser(ctx context.Context, run browserStepExec, stateDir, presencePath, ownTab string, markIdle func(context.Context) error) (note string, err error) {
	if stateDir != "" {
		sctx, cancel := context.WithTimeout(ctx, browserSignInSaveTimeout)
		err := saveBrowserSignIn(sctx, run, stateDir, presencePath, ownTab, time.Now())
		cancel()
		if err != nil {
			logBrowserSignIn(err)
		}
		if errors.Is(err, errBrowserRelaunched) {
			note = browserSignInLostInTheCall
		}
	}
	if err := closeCallTabs(ctx, run); err != nil {
		return note, err
	}
	return note, markIdle(ctx)
}

// releaseBrowser ends a call: handBack readies the browser for the next call
// after a clean one, and stop ends it otherwise or when handBack fails. It
// runs after a cancel or deadline too, so each gets its own bound: a hand-back
// that spent its bound on a hung tab still leaves the stop a whole one.
func releaseBrowser(ctx context.Context, reuse bool, bound time.Duration, handBack, stop func(context.Context) error) error {
	bounded := func(f func(context.Context) error) error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bound)
		defer cancel()
		return f(rctx)
	}
	if reuse {
		err := bounded(handBack)
		if err == nil {
			return nil
		}
		return errors.Join(fmt.Errorf("hand the browser back: %w", err), bounded(stop))
	}
	return bounded(stop)
}

// browserStepExec runs one agent-browser command and returns its --json
// answer.
type browserStepExec func(ctx context.Context, args []string) (stdout, stderr string, err error)

// runBrowserSteps walks steps one at a time and answers with the JSON
// []BrowserStepResult of the steps that got an answer. A step that answered
// success:false is kept in its place and the walk goes on, unless it names a
// CDP wedge, which ends the walk there — save an eval that ran into the CDP
// timeout on a page that still answers (evalOnLivePage), which is the
// script's own hang: it says ErrorKindScriptTimeout and the walk goes on. A
// step with no answer, or the end of ctx, ends the walk with an error,
// keeping the steps before it.
func runBrowserSteps(ctx context.Context, steps [][]string, run browserStepExec) (string, string, bool, error) {
	return walkBrowserSteps(ctx, steps, run, false, nil)
}

// runCallSteps walks a call's batch as runBrowserSteps does, in the call's
// tabs (browser_tabs.go): while the call's own page loads, a tab it opens is
// recorded and the call's tab kept active for the first-paint wait and the
// document's status; after them and after each caller step, a tab its page
// opened becomes active and focused, and a closed popup hands back to the tab
// before it; a read-only step that failed in a popup that closed under it
// runs once more in that tab, any other keeps its one answer and says so; a
// tab list that fails twice after a popup was active runs no caller step
// more; and a caller's tab command lists or switches the call's tabs only.
// Those steps of its own are none of the result's.
func runCallSteps(ctx context.Context, steps [][]string, run browserStepExec, caller []bool) (string, string, bool, error) {
	return walkBrowserSteps(ctx, steps, run, true, caller)
}

func walkBrowserSteps(ctx context.Context, steps [][]string, run browserStepExec, followTabs bool, caller []bool) (string, string, bool, error) {
	w := browserWalk{run: run, stderr: &capBuffer{cap: browserOutputCap}, followTabs: followTabs}
	defer func() {
		if w.page.session != nil {
			w.page.session.cdp.close()
		}
	}()
	if followTabs {
		w.tabs = startBrowserCallTabs(ctx, run, w.stderr)
	}
	tabs := w.tabs
	results := make([]BrowserStepResult, 0, len(steps))
	var walkErr error
	// loading lasts until the first-paint wait has run and the document's
	// status is read: the call's own page's load.
	loading := followTabs
	callerFailed := false
	var skipReason string
	rememberFailure := func(step BrowserStepResult) {
		if !step.Success {
			callerFailed = true
			skipReason = "not run: an earlier caller step failed"
		}
	}
walk:
	for at, step := range steps {
		if walkErr = ctx.Err(); walkErr != nil {
			break
		}
		callerStep := at < len(caller) && caller[at]
		if callerFailed && callerStep {
			results = append(results, BrowserStepResult{Command: step, Error: &skipReason})
			continue
		}
		if own, ok, ran := w.ownStep(ctx, step, callerStep); ok {
			results = append(results, own)
			if callerStep && ran {
				rememberFailure(own)
			}
			continue
		}
		if isCommand(step, browserCmdOpen) && len(step) > 1 {
			w.page.url = step[1]
		}
		if followTabs && isCommand(step, browserCmdOpen) {
			if tabs != nil {
				// The observer is optional. If it cannot be prepared, let
				// agent-browser open the page as it did before this session existed.
				setupCtx, cancel := context.WithTimeout(ctx, browserLivenessTimeout)
				w.page.session, _ = openBrowserPage(setupCtx, run, tabs.ownTarget)
				cancel()
			}
			if walkErr = ctx.Err(); walkErr != nil {
				break
			}
		}
		var answer browserAnswer
		var cut bool
		closedUnder := false
		for attempt := 0; ; attempt++ {
			if followTabs && w.page.session != nil && isCommand(step, browserCmdOpen) && len(step) > 1 {
				answer, walkErr = w.page.session.navigate(ctx, step[1])
			} else {
				answer, cut, walkErr = w.ask(ctx, at, step)
			}
			if walkErr != nil {
				break walk
			}
			if cut {
				break
			}
			// A popup that closes itself once it is done can close under the
			// step after the one that finished it. A step that only reads runs
			// once more in the tab the call is back in; one that may have acted
			// — its own effect may be what closed the popup — keeps its answer.
			if answer.Success || attempt > 0 || tabs == nil || isReportingCommand(step) {
				break
			}
			if closedUnder = tabs.closedUnder(ctx, run); !closedUnder || !browserReadOnly(step) {
				break
			}
		}
		if cut {
			msg := fmt.Sprintf("the answer exceeded %d bytes and was cut", browserOutputCap)
			results = append(results, BrowserStepResult{Command: step, Error: &msg})
			if callerStep {
				rememberFailure(results[len(results)-1])
			}
			continue
		}
		results = append(results, answer.stepResult(step, closedUnder))
		last := &results[len(results)-1]
		if callerStep {
			rememberFailure(*last)
		}
		// A wedged page would make every later step wait out the same CDP
		// timeout; the browser is recycled for it anyway. A step whose popup
		// closed under it got the closed page's answer ("Target closed"); the
		// tab list after it answered, so Chrome is no wedge.
		if _, wedged := scanStepsForCDPWedge(results[len(results)-1:]); wedged && !closedUnder {
			if !w.outlivesWedge(ctx, last) {
				if !callerStep && (last.Error == nil || !strings.HasPrefix(*last.Error, browserNavigationNoAnswer)) {
					break
				}
				if !callerStep {
					// The unanswered open still permits paint and closing reports.
					callerFailed = true
					skipReason = "not run: the open did not commit"
				}
			}
		}
		// While the call's own page loads — the viewport, the open and the
		// paint wait the walk itself puts first — a tab the page opens is
		// held back, so the paint wait and the status are the page's own.
		loading = loading && (isSetViewport(step) || isCommand(step, browserCmdOpen) || slices.Equal(step, browserPaintWait))
		if tabs != nil && loading {
			tabs.hold(ctx, run)
		}
		loading = w.settle(ctx, step, last, loading)
		if w.page.devsup {
			break
		}
	}
	out, err := json.Marshal(results)
	if err != nil {
		return "", w.stderr.String(), w.truncated, fmt.Errorf("marshal steps: %w", err)
	}
	return string(out), w.stderr.String(), w.truncated || w.stderr.truncated, walkErr
}

// browserWalk is what one walk of steps keeps between them: how it runs a
// step, the stderr it gathered and whether any was cut, the page it opened,
// and the call's tabs when it follows them (nil otherwise, or when they could
// not be read).
type browserWalk struct {
	run        browserStepExec
	stderr     *capBuffer
	truncated  bool
	page       browserPage
	tabs       *browserCallTabs
	followTabs bool
}

// browserAnswer is agent-browser's --json answer to one step.
type browserAnswer struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *string         `json:"error"`
}

// ownStep answers a step the walk does not hand agent-browser as written,
// and reports whether it did and whether the caller's command ran. A page
// whose server answered 5xx runs none of the caller's commands: each would
// wait or fail on the error page; the
// screenshot and the canned tail still run, to show it. A walk that lost
// track of its tabs runs no step that acts on a page: it could run in the
// base tab; the canned tail still runs, the screenshot would show an unknown
// tab. A caller's tab command lists or switches the call's tabs only.
func (w *browserWalk) ownStep(ctx context.Context, step []string, caller bool) (BrowserStepResult, bool, bool) {
	switch {
	case w.page.serverStatus >= 500 && !isReportingCommand(step):
		msg := w.page.notRun()
		return BrowserStepResult{Command: step, Error: &msg, ErrorKind: ErrorKindServerError}, true, false
	case w.tabs != nil && w.tabs.lost && (!isReportingCommand(step) || isCommand(step, browserCmdScreenshot)):
		msg := browserTabsLost
		return BrowserStepResult{Command: step, Error: &msg, ErrorKind: ErrorKindOther}, true, false
	}
	if caller {
		own, _ := browserCommandArgs(step)
		if len(own) > 0 && step[own[0]] == browserCmdWait && w.page.session != nil {
			if failure := w.page.session.failure(); failure != "" {
				return BrowserStepResult{Command: step, Error: &failure, ErrorKind: ErrorKindOther}, true, false
			}
		}
	}
	if _, _, isTab := readBrowserTabCommand(step); !isTab || !w.followTabs {
		return BrowserStepResult{}, false, false
	}
	if w.tabs == nil {
		msg := "not run: the call's tabs could not be read, so no tab command runs in it"
		return BrowserStepResult{Command: step, Error: &msg, ErrorKind: ErrorKindInvalidCommand}, true, false
	}
	result := w.tabs.command(ctx, step, w.run)
	return result, true, result.ErrorKind != ErrorKindInvalidCommand
}

// ask runs step once, step number at of the batch, and reads its answer. cut
// reports an answer longer than browserOutputCap, which is not read; an
// answer that is no JSON ends the walk with the step unanswered, or with
// ctx's end when that came first.
func (w *browserWalk) ask(ctx context.Context, at int, step []string) (answer browserAnswer, cut bool, err error) {
	out, serr, runErr := w.run(ctx, step)
	_, _ = w.stderr.Write([]byte(serr))
	// A step's own capture keeps browserOutputCap bytes of its stderr
	// (browserHost.step): one that filled it may have been cut there.
	if len(serr) >= browserOutputCap {
		w.truncated = true
	}
	if len(out) > browserOutputCap {
		w.truncated = true
		return answer, true, nil
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); jsonErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return answer, false, ctxErr
		}
		return answer, false, &browserStepUnansweredError{step: at, command: step, err: errors.Join(runErr, jsonErr)}
	}
	return answer, false, nil
}

// stepResult is the result the answer gives step. A step that may have acted
// and failed in a popup that closed under it says it is not run again. The
// paint wait is zcp's own: a page that did not paint within it is no failed
// step; any other failure of it stays what it is.
func (a browserAnswer) stepResult(step []string, closedUnder bool) BrowserStepResult {
	data := a.Data
	if string(data) == "null" {
		data = nil
	}
	if browserNetworkRequests(step) {
		data = redactBrowserNetworkHeaders(data)
	}
	errText := a.Error
	if closedUnder && !a.Success && errText != nil && !browserReadOnly(step) {
		text := *errText + "; " + browserClosedUnderNote
		errText = &text
	}
	result := BrowserStepResult{Command: step, Success: a.Success, Result: withoutLifecycle(data), Error: errText}
	if slices.Equal(step, browserPaintWait) && !result.Success && result.Error != nil && strings.Contains(*result.Error, browserWaitTimedOut) {
		result.Success, result.Error, result.Result = true, nil, slices.Clone(browserNoPaint)
	}
	return result
}

// outlivesWedge reports whether the walk goes on after last named a CDP
// wedge. An eval whose script hung on a live page, or a step the daemon did
// not answer while the page's server does, is no wedge.
func (w *browserWalk) outlivesWedge(ctx context.Context, last *BrowserStepResult) bool {
	if evalOnLivePage(ctx, *last, w.run) {
		last.ErrorKind = ErrorKindScriptTimeout
		return true
	}
	return w.page.answeredSlowly(ctx, last)
}

// settle ends a step and answers whether the call's own page still loads.
// The document's own status is read once the page painted (or gave up
// painting): a 5xx page ends the caller's commands. Once the page has loaded,
// a tab a caller's step opened becomes the call's, and last carries what the
// tabs noted.
func (w *browserWalk) settle(ctx context.Context, step []string, last *BrowserStepResult, loading bool) bool {
	paintWait := slices.Equal(step, browserPaintWait)
	if paintWait && w.page.serverStatus == 0 {
		w.page.serverStatus = documentStatus(ctx, w.run)
		if w.page.serverStatus >= 500 {
			mark, says := devsupDocument(ctx, w.run)
			if _, ok := devsupPageFailure(mark, says); ok {
				if words, err := json.Marshal(devsupPageWords{Says: says}); err == nil {
					// Page text rides Result, which the env redactor reads; Note
					// carries only the supervisor's fixed mark.
					last.Note = devsupPageNotePrefix + mark
					last.Result = words
					w.page.devsup = true
					return false
				}
			}
		}
	}
	loading = loading && !paintWait
	if w.tabs != nil && !loading && !isReportingCommand(step) {
		w.tabs.sync(ctx, w.run)
		if w.tabs.note != "" {
			last.Note = w.tabs.note
		}
	}
	return loading
}

// browserClosedUnderNote ends the error of a step that may have acted and
// failed in a popup that closed under it: it is not run again.
const browserClosedUnderNote = "the popup it ran in closed under it, so the call is back in the tab before it; a step that may have acted is not run again"

// browserStepUnansweredError is the error of a walk that ended on a step with no
// answer: the step's place in the batch and its command. A caller that ran
// the batch with other words than it was given names the step by its place
// (browserEnvUse.takeBack).
type browserStepUnansweredError struct {
	step    int
	command []string
	err     error
}

func (e *browserStepUnansweredError) Error() string {
	return fmt.Sprintf("step %q got no answer: %v", strings.Join(e.command, " "), e.err)
}

func (e *browserStepUnansweredError) Unwrap() error { return e.err }

// browserNoAnswer is agent-browser's word for a step the daemon took and did
// not answer within its client's own wait (at least 30 s): the step may still
// be running — an open of a page whose server is slow.
const browserNoAnswer = "no response came back"

// browserNavigationNoAnswer distinguishes a direct CDP navigation bound from
// the agent-browser client's own unanswered-command text.
const browserNavigationNoAnswer = "navigation did not commit: " + browserNoAnswer

// browserPageProbeTimeout bounds the plain request a walk sends a page whose
// open got no answer.
const browserPageProbeTimeout = 30 * time.Second

// browserDocumentStatusProbe reads the HTTP status of the page's own document
// from the navigation timing entry (0 when the browser has none). It is no
// step of the walk.
var browserDocumentStatusProbe = []string{browserCmdEval, "(performance.getEntriesByType('navigation')[0]||{}).responseStatus||0"}

// browserPage is what a walk learned of the page it opened: its URL, whether
// its open went unanswered while its server answered (slow), and the HTTP
// status its server answered (0 while unknown).
type browserPage struct {
	url          string
	slow         bool
	serverStatus int
	devsup       bool
	session      *browserPageSession
}

const devsupPageNotePrefix = "zcp-devsup page: "

type devsupPageWords struct {
	Says string `json:"says"`
}

// devsupDocument reads the mark and the main text together, once. An
// unanswered or unmarked page remains an ordinary server-error page.
func devsupDocument(ctx context.Context, run browserStepExec) (string, string) {
	pctx, cancel := context.WithTimeout(ctx, browserLivenessTimeout)
	defer cancel()
	script := fmt.Sprintf(`(()=>{const m=document.querySelector('meta[name="%s"]');const main=document.querySelector('main');return {mark:m?m.content:'',says:main?main.innerText:''}})()`, devsup.PageMarkName)
	out, _, err := run(pctx, []string{browserCmdEval, script})
	if err != nil {
		return "", ""
	}
	var answer struct {
		Success bool `json:"success"`
		Data    struct {
			Result struct {
				Mark string `json:"mark"`
				Says string `json:"says"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &answer) != nil || !answer.Success {
		return "", ""
	}
	return answer.Data.Result.Mark, answer.Data.Result.Says
}

// answeredSlowly stamps a step the daemon did not answer, and reports whether
// the walk goes on. The page's open is checked with one plain request to its
// URL, sent after the open went unanswered: a server that answers makes the
// open slow-page, noting that request's time to first byte and status — all
// the walk measured — or server-error when it
// answered 5xx. A later step the daemon does not answer on a slow page is
// slow-page too. Anything else stays a wedge.
func (p *browserPage) answeredSlowly(ctx context.Context, step *BrowserStepResult) bool {
	if step.Error == nil || !strings.Contains(*step.Error, browserNoAnswer) {
		return false
	}
	if !isCommand(step.Command, browserCmdOpen) {
		if !p.slow {
			return false
		}
		step.ErrorKind = ErrorKindSlowPage
		*step.Error += "; slow page: it is still loading"
		return true
	}
	pctx, cancel := context.WithTimeout(ctx, browserPageProbeTimeout)
	defer cancel()
	status, firstByte, err := browserPageProbe(pctx, p.url)
	if err != nil || status <= 0 {
		return false
	}
	note := fmt.Sprintf("a plain request sent after the open got its first byte in %s (HTTP %d)", firstByte.Round(100*time.Millisecond), status)
	if status >= 500 {
		p.serverStatus = status
		step.ErrorKind = ErrorKindServerError
		*step.Error += "; server error: " + note
		return true
	}
	p.slow = true
	step.ErrorKind = ErrorKindSlowPage
	*step.Error += "; " + browserSlowPageMarker + note
	return true
}

// browserSlowPageMarker heads the note a slow-page open's error carries.
const browserSlowPageMarker = "slow page: "

// notRun is the error of a caller's command a server-error page did not run.
func (p *browserPage) notRun() string {
	return fmt.Sprintf("not run: the page answered HTTP %d, a server error", p.serverStatus)
}

// isReportingCommand reports whether a step only reports on the page — the
// screenshot and the canned tail — and so runs on a server-error page too.
func isReportingCommand(cmd []string) bool {
	return isCommand(cmd, browserCmdScreenshot) || isCommand(cmd, browserCmdErrors) ||
		isCommand(cmd, browserCmdConsole) || isCommand(cmd, browserCmdNetwork)
}

// documentStatus is the page document's HTTP status as the browser got it
// (browserDocumentStatusProbe, bounded by browserLivenessTimeout); 0 when it
// cannot tell.
func documentStatus(ctx context.Context, run browserStepExec) int {
	pctx, cancel := context.WithTimeout(ctx, browserLivenessTimeout)
	defer cancel()
	out, _, err := run(pctx, browserDocumentStatusProbe)
	if err != nil {
		return 0
	}
	var answer struct {
		Success bool `json:"success"`
		Data    struct {
			Result json.Number `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &answer) != nil || !answer.Success {
		return 0
	}
	status, err := answer.Data.Result.Int64()
	if err != nil {
		return 0
	}
	return int(status)
}

// browserPageProbe sends a page's server one plain request and answers its
// status and time to first byte. Tests override it
// (OverrideBrowserPageProbeForTest).
var browserPageProbe = httpPageProbe

// OverrideBrowserPageProbeForTest replaces the plain request a walk sends a
// page whose open got no answer. Returns restore.
func OverrideBrowserPageProbeForTest(fn func(ctx context.Context, url string) (int, time.Duration, error)) func() {
	old := browserPageProbe
	browserPageProbe = fn
	return func() { browserPageProbe = old }
}

// httpPageProbe GETs an http(s) url and answers the status and the time to
// the response's first byte; the body is never read.
func httpPageProbe(ctx context.Context, url string) (int, time.Duration, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return 0, 0, fmt.Errorf("probe page: %q is no http(s) URL", url)
	}
	start := time.Now()
	var firstByte time.Duration
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Since(start) }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("probe page: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("probe page: %w", err)
	}
	_ = resp.Body.Close()
	if firstByte == 0 {
		firstByte = time.Since(start)
	}
	return resp.StatusCode, firstByte, nil
}

// withoutLifecycle drops the lifecycle agent-browser adds to every answer. It
// says how that step's client reached the browser — reused on every step
// after the browser's own start — not whether the call's Chrome is fresh;
// BrowserBatchResult.FreshBrowser says that. Data that is not an object, or
// has no lifecycle, is kept as it came.
// browserLivenessProbe is the trivial evaluate asked of a page after an eval
// ran into the CDP timeout: a page whose main thread and CDP connection
// still work answers it at once.
var browserLivenessProbe = []string{browserCmdEval, "1"}

// browserLivenessTimeout bounds that probe: well under agent-browser's 30 s
// CDP timeout, so a hung page costs the call only a few seconds more.
const browserLivenessTimeout = 5 * time.Second

// evalOnLivePage reports whether step is an eval that ran into the CDP
// timeout while the page still answers a trivial evaluate under
// browserLivenessTimeout — the script's promise never settled (a wait on a
// looping animation's finished), the page is alive. Any other wedge, or a
// probe that times out, gets no answer or answers a failure, is not.
func evalOnLivePage(ctx context.Context, step BrowserStepResult, run browserStepExec) bool {
	if !isCommand(step.Command, browserCmdEval) || step.Error == nil || !strings.Contains(*step.Error, cdpCommandTimedOut) {
		return false
	}
	pctx, cancel := context.WithTimeout(ctx, browserLivenessTimeout)
	defer cancel()
	out, _, err := run(pctx, browserLivenessProbe)
	if err != nil {
		return false
	}
	var answer struct {
		Success bool `json:"success"`
	}
	return json.Unmarshal([]byte(strings.TrimSpace(out)), &answer) == nil && answer.Success
}

func withoutLifecycle(data json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	if _, ok := fields["lifecycle"]; !ok {
		return data
	}
	delete(fields, "lifecycle")
	out, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return out
}
