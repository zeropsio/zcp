package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
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
	browserIdleTimeout = 5 * time.Minute
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

	// browserSession is agent-browser's session: the daemon's socket, pid
	// and stream files are named after it.
	browserSession = "default"
	// browserSignIn names the kept sign-in, agent-browser's restore key: a
	// browser that keeps it loads and saves <browserSignIn>-<browserSession>.json
	// in browserStateDir (docs/spec-mate.md §2.6c).
	browserSignIn     = "zcp"
	browserSignInFile = browserSignIn + "-" + browserSession + ".json"
	// browserStateTmp is agent-browser's own directory for a save's
	// candidate, inside the state directory.
	browserStateTmp = ".tmp"
	// browserStateExpireDays: a starting browser removes a kept sign-in no
	// browser saved for a week.
	browserStateExpireDays = 7
	// browserSignInSaveTimeout bounds the save at a call's end, inside the
	// hand-back's own bound.
	browserSignInSaveTimeout = 5 * time.Second
)

// browserPaintWait waits for the page's first contentful paint: it returns
// at once on a page that has painted and fails as a timeout step after 5 s
// on one that never does, while the walk goes on.
var browserPaintWait = []string{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"}

// browserEnv is the whole environment of the warm browser's service and of
// every step sent to it. agent-browser restarts a daemon whose settings
// differ from the client's, so both sides get exactly this, never an
// inherited AGENT_BROWSER_* variable. keep names the kept sign-in: such a
// browser loads it as it starts and saves it as its daemon exits idle,
// never on a timer — every clean call ends with zcp's own save
// (handBackBrowser) — and drops one unsaved for browserStateExpireDays.
func browserEnv(home, path string, keep bool) []string {
	env := []string{
		"HOME=" + home,
		"PATH=" + path,
		"TMPDIR=" + browserTmpDir,
		"AGENT_BROWSER_SOCKET_DIR=" + browserRuntimeDir,
		"AGENT_BROWSER_SESSION=" + browserSession,
		"AGENT_BROWSER_IDLE_TIMEOUT_MS=" + strconv.FormatInt(browserIdleTimeout.Milliseconds(), 10),
	}
	if keep {
		env = append(env,
			"AGENT_BROWSER_SESSION_NAME="+browserSignIn,
			"AGENT_BROWSER_RESTORE_SAVE=auto",
			"AGENT_BROWSER_AUTOSAVE_INTERVAL_MS=0",
			"AGENT_BROWSER_STATE_EXPIRE_DAYS="+strconv.Itoa(browserStateExpireDays),
		)
	}
	return env
}

// browserKeepsSignIn reports whether a call's browser keeps the sign-in
// across browser restarts. A fresh-context call's never does: agent-browser
// saves the cookies of the active tab's context, so a save while its fresh
// context is active would write that empty context over the sign-in. Nor
// does any browser whose home would put the saved cookies outside a
// private home: a relative one, /, or the workspace, which holds git
// checkouts and is mounted into dev services.
func browserKeepsSignIn(isolation, home string) bool {
	if isolation == BrowserIsolationFreshContext || !filepath.IsAbs(home) {
		return false
	}
	home = filepath.Clean(home)
	return home != "/" && home != mountBase && !strings.HasPrefix(home, mountBase+"/")
}

// browserStateDir is where agent-browser keeps a named session's state
// under the browser's home.
func browserStateDir(home string) string {
	return filepath.Join(home, ".agent-browser", "sessions")
}

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
// on the warm browser: it is a tab call (a new browser asks for its own, and
// a fresh context gets one that keeps no sign-in), the browser is running,
// the last call in it ended cleanly, and neither its idle exit nor its max
// age can cut the call.
func (w warmBrowser) reusable(isolation string, budget time.Duration) bool {
	return isolation == BrowserIsolationTab &&
		w.Active && w.Marker == browserMarkerIdle &&
		w.IdleFor < browserIdleTimeout-browserBoundMargin &&
		w.Age+budget < browserMaxAge && w.quietWhileIdle()
}

// quietWhileIdle reports whether the browser used at most a tenth of a CPU
// while no call was in it. One that used more still runs a page: a popup
// agent-browser never tracked, or a hung renderer Chrome kept after its tab
// closed. A gap under a second is too short to judge.
func (w warmBrowser) quietWhileIdle() bool {
	return w.IdleCPU < 0 || w.IdleFor < time.Second || w.IdleCPU <= w.IdleFor/10
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
// does not), closes the call's tabs, and only then marks the browser idle,
// so a call that follows finds the save done. The save runs while the
// call's own tab, a live page of the default context, is active: on the
// base tab's about:blank agent-browser reads every origin's storage through
// a temporary tab, which now and then stalled the save past 30 s (measured
// with agent-browser 0.39.1: 2 of 29 saves on the base tab, none of 40 on
// the call's tab). A save that fails is logged and hands the browser back
// all the same: the last good save stands, and the browser saves again
// when its daemon exits idle.
func handBackBrowser(ctx context.Context, run browserStepExec, stateDir string, markIdle func(context.Context) error) error {
	if stateDir != "" {
		sctx, cancel := context.WithTimeout(ctx, browserSignInSaveTimeout)
		err := saveBrowserSignIn(sctx, run, stateDir)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "zerops_browser: keep the sign-in: %v\n", err)
		}
	}
	if err := closeCallTabs(ctx, run); err != nil {
		return err
	}
	return markIdle(ctx)
}

// saveBrowserSignIn saves the browser's default context into stateDir. It
// saves nothing after a restore that failed, as agent-browser's own auto
// policy does, so a file the browser could not load is never replaced by a
// browser that started without it. agent-browser has no command that saves
// the named session at once, so its state save writes a candidate in the
// directory's own temporary folder and zcp renames it over the kept file
// once it is a state agent-browser can load. It saves the context of the
// active tab, which must be the default context's.
func saveBrowserSignIn(ctx context.Context, run browserStepExec, stateDir string) error {
	out, _, err := run(ctx, []string{"session", "info"})
	if err != nil {
		return fmt.Errorf("session info: %w", err)
	}
	var info struct {
		Data struct {
			Runtime struct {
				RestoreStatus string `json:"restoreStatus"`
			} `json:"runtime"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &info); err != nil {
		return fmt.Errorf("session info: %w", err)
	}
	// loaded: the kept file is in the browser; missing: there was none.
	if status := info.Data.Runtime.RestoreStatus; status != "loaded" && status != "missing" {
		return nil
	}
	candidate := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := walkBrowser(ctx, [][]string{{"state", "save", candidate}}, run); err != nil {
		return err
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return fmt.Errorf("read the saved state: %w", err)
	}
	if !validBrowserState(data) {
		_ = os.Remove(candidate)
		return errors.New("the saved state is not one agent-browser can load")
	}
	if err := os.Rename(candidate, filepath.Join(stateDir, browserSignInFile)); err != nil {
		return fmt.Errorf("keep the saved state: %w", err)
	}
	return nil
}

// validBrowserState reports whether data is a state agent-browser 0.39.1
// loads: its cookies and origins, each cookie with its name, value, domain
// and path, each origin with its origin and local storage, every field of
// the type agent-browser reads.
func validBrowserState(data []byte) bool {
	type entry struct {
		Name  *string `json:"name"`
		Value *string `json:"value"`
	}
	var state struct {
		Cookies *[]struct {
			Name     *string `json:"name"`
			Value    *string `json:"value"`
			Domain   *string `json:"domain"`
			Path     *string `json:"path"`
			Expires  float64 `json:"expires"`
			Size     int64   `json:"size"`
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
			Session  bool    `json:"session"`
			SameSite *string `json:"sameSite"`
		} `json:"cookies"`
		Origins *[]struct {
			Origin         *string  `json:"origin"`
			LocalStorage   *[]entry `json:"localStorage"`
			SessionStorage []entry  `json:"sessionStorage"`
		} `json:"origins"`
	}
	if json.Unmarshal(data, &state) != nil || state.Cookies == nil || state.Origins == nil {
		return false
	}
	for _, c := range *state.Cookies {
		if c.Name == nil || c.Value == nil || c.Domain == nil || c.Path == nil {
			return false
		}
	}
	for _, o := range *state.Origins {
		if o.Origin == nil || o.LocalStorage == nil {
			return false
		}
		for _, e := range append(slices.Clone(*o.LocalStorage), o.SessionStorage...) {
			if e.Name == nil || e.Value == nil {
				return false
			}
		}
	}
	return true
}

// readyBrowserState readies stateDir before a browser that keeps the
// sign-in starts: the directory and its temporary folder exist, are no
// symlink and are their user's alone (0700, every file in them 0600, a
// looser mode tightened), and a kept file agent-browser could not load is
// removed — after a failed load its auto policy would skip every save that
// follows.
func readyBrowserState(stateDir string) error {
	tmp := filepath.Join(stateDir, browserStateTmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return fmt.Errorf("browser state directory: %w", err)
	}
	for _, dir := range []string{stateDir, tmp} {
		if err := tightenMode(dir, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("browser state directory: %w", err)
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				if err := tightenMode(filepath.Join(dir, e.Name()), 0o600); err != nil {
					return err
				}
			}
		}
	}
	file := filepath.Join(stateDir, browserSignInFile)
	data, err := os.ReadFile(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("browser state: %w", err)
	case !validBrowserState(data):
		if err := os.Remove(file); err != nil {
			return fmt.Errorf("remove a browser state agent-browser cannot load: %w", err)
		}
	}
	return nil
}

// tightenMode refuses a symlink at path and takes from its mode every
// permission bit outside mode.
func tightenMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("browser state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("browser state: %s is a symlink", path)
	}
	if info.Mode().Perm()&^mode == 0 {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("browser state: %w", err)
	}
	return nil
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
	results := make([]BrowserStepResult, 0, len(steps))
	stderr := &capBuffer{cap: browserOutputCap}
	truncated := false
	var walkErr error
	var page browserPage
walk:
	for at, step := range steps {
		if err := ctx.Err(); err != nil {
			walkErr = err
			break
		}
		// A page whose server answered 5xx runs none of the caller's
		// commands: each would wait or fail on the error page. The
		// screenshot and the canned tail still run, to show it.
		if page.serverStatus >= 500 && !isReportingCommand(step) {
			msg := page.notRun()
			results = append(results, BrowserStepResult{Command: step, Error: &msg, ErrorKind: ErrorKindServerError})
			continue
		}
		if isCommand(step, browserCmdOpen) && len(step) > 1 {
			page.url = step[1]
		}
		out, serr, runErr := run(ctx, step)
		_, _ = stderr.Write([]byte(serr))
		// A step's own capture keeps browserOutputCap bytes of its stderr
		// (browserHost.step): one that filled it may have been cut there.
		if len(serr) >= browserOutputCap {
			truncated = true
		}
		if len(out) > browserOutputCap {
			truncated = true
			msg := fmt.Sprintf("the answer exceeded %d bytes and was cut", browserOutputCap)
			results = append(results, BrowserStepResult{Command: step, Error: &msg})
			continue
		}
		var answer struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
			Error   *string         `json:"error"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				walkErr = ctxErr
			} else {
				walkErr = &browserStepUnansweredError{step: at, command: step, err: errors.Join(runErr, err)}
			}
			break
		}
		if string(answer.Data) == "null" {
			answer.Data = nil
		}
		results = append(results, BrowserStepResult{Command: step, Success: answer.Success, Result: withoutLifecycle(answer.Data), Error: answer.Error})
		last := &results[len(results)-1]
		// A wedged page would make every later step wait out the same CDP
		// timeout; the browser is recycled for it anyway. An eval whose
		// script hung on a live page, or a step the daemon did not answer
		// while the page's server does, is no wedge: the walk goes on.
		if _, wedged := scanStepsForCDPWedge(results[len(results)-1:]); wedged {
			switch {
			case evalOnLivePage(ctx, *last, run):
				last.ErrorKind = ErrorKindScriptTimeout
			case page.answeredSlowly(ctx, last):
			default:
				break walk
			}
		}
		// The document's own status, read once the page painted (or gave up
		// painting): a 5xx page ends the caller's commands here.
		if slices.Equal(step, browserPaintWait) && page.serverStatus == 0 {
			page.serverStatus = documentStatus(ctx, run)
		}
	}
	out, err := json.Marshal(results)
	if err != nil {
		return "", stderr.String(), truncated, fmt.Errorf("marshal steps: %w", err)
	}
	return string(out), stderr.String(), truncated || stderr.truncated, walkErr
}

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
}

// answeredSlowly stamps a step the daemon did not answer, and reports whether
// the walk goes on. The page's open is checked with one plain request to its
// URL: a server that answers makes the open slow-page, naming its time to
// first byte — the page is slow, not Chrome, and the walk goes on within the
// call's own timeout, so the caller's waits give it its time — or
// server-error when it answered 5xx. A later step the daemon does not answer
// on a slow page is slow-page too. Anything else stays a wedge.
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
	note := fmt.Sprintf("a plain request to it got its first byte after %s (HTTP %d)", firstByte.Round(100*time.Millisecond), status)
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
