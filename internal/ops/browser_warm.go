package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// the last one ended cleanly ("idle"); its mtime is the last call's end.
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
)

// browserPaintWait waits for the page's first contentful paint: it returns
// at once on a page that has painted and fails as a timeout step after 5 s
// on one that never does, while the walk goes on.
var browserPaintWait = []string{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"}

// browserEnv is the whole environment of the warm browser's service and of
// every step sent to it. agent-browser restarts a daemon whose settings
// differ from the client's, so both sides get exactly this, never an
// inherited AGENT_BROWSER_* variable.
func browserEnv(home, path string) []string {
	return []string{
		"HOME=" + home,
		"PATH=" + path,
		"TMPDIR=" + browserTmpDir,
		"AGENT_BROWSER_SOCKET_DIR=" + browserRuntimeDir,
		"AGENT_BROWSER_SESSION=default",
		"AGENT_BROWSER_IDLE_TIMEOUT_MS=" + strconv.FormatInt(browserIdleTimeout.Milliseconds(), 10),
	}
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
}

// reusable reports whether a call with the given budget may run on the
// warm browser: it is running, the last call in it ended cleanly, and
// neither its idle exit nor its max age can cut the call.
func (w warmBrowser) reusable(budget time.Duration) bool {
	return w.Active && w.Marker == browserMarkerIdle &&
		w.IdleFor < browserIdleTimeout-browserBoundMargin &&
		w.Age+budget < browserMaxAge
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
// success:false is kept in its place and the walk goes on; a step with no
// answer, or the end of ctx, ends the walk with an error, keeping the steps
// before it.
func runBrowserSteps(ctx context.Context, steps [][]string, run browserStepExec) (string, string, bool, error) {
	results := make([]BrowserStepResult, 0, len(steps))
	stderr := &capBuffer{cap: browserOutputCap}
	truncated := false
	var walkErr error
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			walkErr = err
			break
		}
		out, serr, runErr := run(ctx, step)
		_, _ = stderr.Write([]byte(serr))
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
				walkErr = fmt.Errorf("step %q got no answer: %w", strings.Join(step, " "), errors.Join(runErr, err))
			}
			break
		}
		if string(answer.Data) == "null" {
			answer.Data = nil
		}
		results = append(results, BrowserStepResult{Command: step, Success: answer.Success, Result: answer.Data, Error: answer.Error})
	}
	out, err := json.Marshal(results)
	if err != nil {
		return "", stderr.String(), truncated, fmt.Errorf("marshal steps: %w", err)
	}
	return string(out), stderr.String(), truncated || stderr.truncated, walkErr
}
