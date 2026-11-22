package ops

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// answer is one scripted agent-browser --json reply.
type answer struct {
	stdout, stderr string
	err            error
}

// scriptedSteps answers each step from script by its command name and
// records the commands it saw.
func scriptedSteps(script map[string]answer, seen *[][]string) browserStepExec {
	return func(_ context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		a, ok := script[args[0]]
		if !ok {
			return `{"success":true,"data":{"ok":true},"error":null}`, "", nil
		}
		return a.stdout, a.stderr, a.err
	}
}

// TestRunBrowserSteps_EachStepIsItsOwnAnswer pins that a walk is the steps
// run one by one, each answer kept as the step's result — a failed step is
// reported in its place and the walk goes on to the next one.
func TestRunBrowserSteps_EachStepIsItsOwnAnswer(t *testing.T) {
	var seen [][]string
	run := scriptedSteps(map[string]answer{
		"open":  {stdout: `{"success":true,"data":{"url":"https://example.com","title":"Example"},"error":null}`},
		"click": {stdout: `{"success":false,"data":null,"error":"no element @e9"}`, err: errors.New("exit status 1")},
	}, &seen)
	steps := [][]string{{"open", "https://example.com"}, {"click", "@e9"}, {"errors"}}

	stdout, _, truncated, err := runBrowserSteps(context.Background(), steps, run)
	if err != nil {
		t.Fatalf("a failed step is not a failed walk: %v", err)
	}
	if truncated {
		t.Error("nothing was cut")
	}
	if !slices.EqualFunc(seen, steps, slices.Equal) {
		t.Errorf("ran %v, want %v", seen, steps)
	}
	var got []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("walk output is not the step list: %v\n%s", err, stdout)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 steps, got %d: %s", len(got), stdout)
	}
	if !got[0].Success || !strings.Contains(string(got[0].Result), `"title":"Example"`) {
		t.Errorf("open step lost its answer: %+v", got[0])
	}
	if got[1].Success || got[1].Error == nil || *got[1].Error != "no element @e9" {
		t.Errorf("click step must carry its own error: %+v", got[1])
	}
	if !slices.Equal(got[2].Command, []string{"errors"}) || !got[2].Success {
		t.Errorf("the walk must go on after a failed step: %+v", got[2])
	}
}

// TestRunBrowserSteps_DeadlineKeepsFinishedSteps pins that a walk cut by
// its deadline returns the steps that finished and the deadline itself,
// and sends no step after it.
func TestRunBrowserSteps_DeadlineKeepsFinishedSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen [][]string
	run := func(c context.Context, args []string) (string, string, error) {
		seen = append(seen, args)
		if args[0] == "wait" {
			cancel() // the deadline lands while this step runs
			<-c.Done()
			return "", "", c.Err()
		}
		return `{"success":true,"data":{},"error":null}`, "", nil
	}
	steps := [][]string{{"open", "https://example.com"}, {"click", "@e1"}, {"wait", "60000"}, {"errors"}}

	stdout, _, _, err := runBrowserSteps(ctx, steps, run)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want the context error, got %v", err)
	}
	var got []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("finished steps lost: %v\n%s", err, stdout)
	}
	if len(got) != 2 || !slices.Equal(got[1].Command, []string{"click", "@e1"}) {
		t.Errorf("want open and click kept, got %+v", got)
	}
	if len(seen) != 3 {
		t.Errorf("no step may be sent after the deadline, sent %v", seen)
	}
}

// TestRunBrowserSteps_UnansweredStepEndsTheWalk pins that a step with no
// JSON answer (the CLI crashed, or could not reach the browser) ends the walk
// with an error naming the step, so the browser is recycled.
func TestRunBrowserSteps_UnansweredStepEndsTheWalk(t *testing.T) {
	var seen [][]string
	run := scriptedSteps(map[string]answer{
		"click": {stderr: "Failed to create socket directory: Permission denied", err: errors.New("exit status 1")},
	}, &seen)
	stdout, stderr, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "u"}, {"click", "@e1"}, {"errors"}}, run)
	if err == nil || !strings.Contains(err.Error(), "click") {
		t.Fatalf("want an error naming the unanswered step, got %v", err)
	}
	if !strings.Contains(stderr, "socket directory") {
		t.Errorf("stderr of the step must be kept: %q", stderr)
	}
	var got []BrowserStepResult
	if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != 1 {
		t.Errorf("want only the answered open step, got %s (%v)", stdout, jerr)
	}
	if len(seen) != 2 {
		t.Errorf("the walk must stop at the unanswered step, sent %v", seen)
	}
}

// TestRunBrowserSteps_WedgedStepEndsTheWalk pins that a step answering with a
// CDP wedge ends the walk there: every later step that needs the page would
// wait out the same 30 s CDP timeout on a page that spins forever.
func TestRunBrowserSteps_WedgedStepEndsTheWalk(t *testing.T) {
	var seen [][]string
	run := scriptedSteps(map[string]answer{
		"wait": {stdout: `{"success":false,"data":null,"error":"CDP command timed out: Runtime.evaluate"}`},
	}, &seen)
	stdout, _, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "u"}, {"wait", "--fn", "x"}, {"get", "title"}, {"errors"}}, run)
	if err != nil {
		t.Fatalf("a wedge is an answer, not a walk error: %v", err)
	}
	var got []BrowserStepResult
	if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != 2 {
		t.Errorf("want open and the wedged wait, got %s (%v)", stdout, jerr)
	}
	if len(seen) != 2 {
		t.Errorf("the walk must stop at the wedged step, sent %v", seen)
	}
}

// settleScript is an eval whose promise never settles on a page with a
// looping animation: its finished never resolves.
const settleScript = "Promise.all(document.getAnimations().map(a => a.finished)).then(() => 'settled')"

// evalTimedOut is agent-browser's answer to an evaluate past its CDP timeout.
const evalTimedOut = `{"success":false,"data":null,"error":"CDP command timed out: Runtime.evaluate"}`

// probedSteps answers the caller's settleScript with the CDP timeout, any
// other eval (the liveness probe) with probe, and every other step with
// success; it records the commands it saw and fails the test when the probe
// runs without a short bound of its own.
func probedSteps(t *testing.T, probe answer, seen *[][]string) browserStepExec {
	t.Helper()
	return func(ctx context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		switch {
		case args[0] == "eval" && len(args) > 1 && args[1] == settleScript:
			return evalTimedOut, "", nil
		case args[0] == "eval":
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
				t.Errorf("the liveness probe must run under a short bound of its own, deadline %v %v", deadline, ok)
			}
			return probe.stdout, probe.stderr, probe.err
		}
		return `{"success":true,"data":{"ok":true},"error":null}`, "", nil
	}
}

// TestRunBrowserSteps_EvalNeverSettledOnALivePage_WalkGoesOn pins that an
// eval which ran into agent-browser's CDP timeout while the page still
// answers a trivial evaluate is the script's own hang, not the page's: the
// step says script-timeout, the probe is no step of the walk, and the
// remaining commands and the canned tail run.
func TestRunBrowserSteps_EvalNeverSettledOnALivePage_WalkGoesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var seen [][]string
		run := probedSteps(t, answer{stdout: `{"success":true,"data":{"result":1},"error":null}`}, &seen)
		steps := [][]string{{"open", "u"}, {"eval", settleScript}, {"get", "title"}, {"errors"}, {"console"}}
		stdout, _, _, err := runBrowserSteps(context.Background(), steps, run)
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		var got []BrowserStepResult
		if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil {
			t.Fatalf("steps: %v", jerr)
		}
		if len(got) != len(steps) {
			t.Fatalf("every step must answer in its place and the probe in none, got %s", stdout)
		}
		if got[1].ErrorKind != "script-timeout" {
			t.Errorf("the eval that never settled must say script-timeout, got %q", got[1].ErrorKind)
		}
		if got[1].Error == nil || *got[1].Error != "CDP command timed out: Runtime.evaluate" {
			t.Errorf("the step keeps agent-browser's own words, got %v", got[1].Error)
		}
		if len(seen) != len(steps)+1 || !isCommand(seen[2], "eval") || seen[2][len(seen[2])-1] == settleScript {
			t.Errorf("one probe must run right after the timed-out eval, then the rest of the walk; sent %v", seen)
		}
	})
}

// TestRunBrowserSteps_EvalTimeoutOnAHungPage_EndsTheWalk pins that when the
// probe after a timed-out eval gets no answer either, the page (or Chrome)
// hung: the walk ends there as a wedge, as it does today.
func TestRunBrowserSteps_EvalTimeoutOnAHungPage_EndsTheWalk(t *testing.T) {
	tests := []struct {
		name  string
		probe answer
	}{
		{"the probe runs into the CDP timeout too", answer{stdout: evalTimedOut}},
		{"the probe gets no answer", answer{err: context.DeadlineExceeded}},
		{"the probe answers a failure", answer{stdout: `{"success":false,"data":null,"error":"Execution context was destroyed"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var seen [][]string
				run := probedSteps(t, tt.probe, &seen)
				stdout, _, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "u"}, {"eval", settleScript}, {"get", "title"}, {"errors"}}, run)
				if err != nil {
					t.Fatalf("a wedge is an answer, not a walk error: %v", err)
				}
				var got []BrowserStepResult
				if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != 2 {
					t.Fatalf("want open and the timed-out eval, got %s (%v)", stdout, jerr)
				}
				if got[1].ErrorKind == "script-timeout" {
					t.Error("an eval on a page that does not answer is no script-timeout")
				}
				if len(seen) != 3 {
					t.Errorf("the walk must probe once, then stop; sent %v", seen)
				}
			})
		})
	}
}

// TestRunBrowserSteps_OversizedAnswerIsCut pins that one step's answer past
// the output cap becomes a failed step and flags the walk truncated, while
// the steps after it still run.
func TestRunBrowserSteps_OversizedAnswerIsCut(t *testing.T) {
	var seen [][]string
	huge := `{"success":true,"data":{"text":"` + strings.Repeat("x", browserOutputCap) + `"},"error":null}`
	run := scriptedSteps(map[string]answer{"get": {stdout: huge}}, &seen)
	stdout, _, truncated, err := runBrowserSteps(context.Background(), [][]string{{"get", "text", "body"}, {"errors"}}, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Error("want the walk flagged truncated")
	}
	var got []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || len(got) != 2 {
		t.Fatalf("want 2 steps, got %s (%v)", stdout[:min(len(stdout), 200)], err)
	}
	if got[0].Success || got[0].Error == nil {
		t.Errorf("the cut step must be a failed step: %+v", got[0])
	}
}

// TestWarmBrowser_ReusableOnlyInsideItsBounds pins when a call may reuse the
// warm browser instead of starting a fresh one.
func TestWarmBrowser_ReusableOnlyInsideItsBounds(t *testing.T) {
	budget := 2 * time.Minute
	idle := warmBrowser{Active: true, Age: 10 * time.Minute, Marker: browserMarkerIdle, IdleFor: time.Minute, IdleCPU: time.Second}
	tests := []struct {
		name string
		w    func(warmBrowser) warmBrowser
		want bool
	}{
		{"idle after a clean call", func(w warmBrowser) warmBrowser { return w }, true},
		{"not running", func(w warmBrowser) warmBrowser { w.Active = false; return w }, false},
		{"a call died in it", func(w warmBrowser) warmBrowser { w.Marker = browserMarkerBusy; return w }, false},
		{"started by nobody known", func(w warmBrowser) warmBrowser { w.Marker = ""; return w }, false},
		{"about to hit its idle exit", func(w warmBrowser) warmBrowser { w.IdleFor = browserIdleTimeout - 10*time.Second; return w }, false},
		{"the call could outlive its max age", func(w warmBrowser) warmBrowser { w.Age = browserMaxAge - budget + time.Second; return w }, false},
		{"a page kept running while no call was in it", func(w warmBrowser) warmBrowser { w.IdleCPU = 30 * time.Second; return w }, false},
		{"a gap too short to judge its CPU", func(w warmBrowser) warmBrowser {
			w.IdleFor, w.IdleCPU = 500*time.Millisecond, 400*time.Millisecond
			return w
		}, true},
		{"its CPU use is unknown", func(w warmBrowser) warmBrowser { w.IdleCPU = -1; return w }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w(idle).reusable(BrowserIsolationTab, budget); got != tt.want {
				t.Errorf("reusable = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("a call that asked for a new browser", func(t *testing.T) {
		if idle.reusable(BrowserIsolationNewBrowser, budget) {
			t.Error("a new-browser call must start a fresh browser, even beside an idle one")
		}
	})
}

// TestRunBrowserSteps_StepsCarryNoAgentBrowserLifecycle pins that a step's
// result drops the lifecycle agent-browser adds to every answer: it says
// how that step's client reached the browser — reused, on every step after
// the browser's own start — never whether the call's Chrome is fresh, so an
// agent reading reused: true after a new-browser call was misled.
func TestRunBrowserSteps_StepsCarryNoAgentBrowserLifecycle(t *testing.T) {
	var seen [][]string
	run := scriptedSteps(map[string]answer{
		"open": {stdout: `{"success":true,"data":{"lifecycle":{"effectiveLaunch":{"browserLaunched":true,"engine":"chrome","launchHash":11677035664275010974},` +
			`"launched":false,"relaunchedBrowser":false,"reused":true},"title":"Example","url":"https://example.com/"},"error":null}`},
	}, &seen)
	stdout, _, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "https://example.com"}}, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || len(got) != 1 {
		t.Fatalf("want the open step, got %s (%v)", stdout, err)
	}
	var result map[string]any
	if err := json.Unmarshal(got[0].Result, &result); err != nil {
		t.Fatalf("open result is not an object: %v\n%s", err, got[0].Result)
	}
	if _, ok := result["lifecycle"]; ok || strings.Contains(stdout, "reused") {
		t.Errorf("the step must carry no agent-browser lifecycle: %s", stdout)
	}
	if result["title"] != "Example" || result["url"] != "https://example.com/" {
		t.Errorf("the step lost its answer: %s", got[0].Result)
	}
}

// TestBrowserMarker_CarriesTheCPUUsedWhenTheCallEnded pins the marker a
// clean call leaves: idle with the service's CPU time at that moment, so the
// next call can tell how much the browser used while no call was in it.
func TestBrowserMarker_CarriesTheCPUUsedWhenTheCallEnded(t *testing.T) {
	state, cpu := parseBrowserMarker(browserIdleMarker(1500 * time.Millisecond))
	if state != browserMarkerIdle || cpu != 1500*time.Millisecond {
		t.Errorf("idle marker read back as %q %v", state, cpu)
	}
	for _, raw := range []string{"busy\n", "idle\n", "idle x\n", ""} {
		if _, cpu := parseBrowserMarker(raw); cpu != -1 {
			t.Errorf("marker %q must carry no CPU, got %v", raw, cpu)
		}
	}
	if state, _ := parseBrowserMarker("busy\n"); state != browserMarkerBusy {
		t.Errorf("busy marker read back as %q", state)
	}
}

// TestBrowserEnv_IdenticalForTheBrowserAndEveryStep pins that the env is
// built from nothing but its arguments: agent-browser restarts a daemon whose
// settings differ from the client's, so an inherited AGENT_BROWSER_* variable
// must never reach either side.
func TestBrowserEnv_IdenticalForTheBrowserAndEveryStep(t *testing.T) {
	t.Setenv("AGENT_BROWSER_DEFAULT_TIMEOUT", "9000")
	env := browserEnv("/home/zerops", "/usr/bin")
	want := []string{
		"HOME=/home/zerops",
		"PATH=/usr/bin",
		"TMPDIR=" + browserTmpDir,
		"AGENT_BROWSER_SOCKET_DIR=" + browserRuntimeDir,
		"AGENT_BROWSER_SESSION=default",
		"AGENT_BROWSER_IDLE_TIMEOUT_MS=1200000",
		"AGENT_BROWSER_ARGS=--disable-back-forward-cache",
	}
	if !slices.Equal(env, want) {
		t.Errorf("env = %v, want %v", env, want)
	}
	args := browserUnitArgs("/usr/bin/agent-browser", env, 1000, 2, 0)
	for _, kv := range env {
		if !slices.Contains(args, "--setenv="+kv) {
			t.Errorf("the browser unit misses %q: %v", kv, args)
		}
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--setenv=") && !slices.Contains(env, strings.TrimPrefix(a, "--setenv=")) {
			t.Errorf("the browser unit sets %q, which the steps do not", a)
		}
	}
}

// TestBrowserEnv_TheWarmBrowserKeepsNoPageInItsBackForwardCache: the warm
// browser and every step run Chrome with its back/forward cache off, so the
// stub pages the kept sign-in's restore and save walk the base tab through
// pin no renderer while the browser idles. With the cache on, a Mate's idle
// browser held up to 15 renderers against 11 before the stub pages (Chrome
// 157, 2026-11-21), and resetting the base tab's history after them was
// refused there ("Not attached to an active page").
// witness: internal/ops/browser_warm.go | const browserChromeArgs = "--disable-back-forward-cache" → const browserChromeArgs = ""
func TestBrowserEnv_TheWarmBrowserKeepsNoPageInItsBackForwardCache(t *testing.T) {
	t.Parallel()
	env := browserEnv("/home/zerops", "/usr/local/bin:/usr/bin")
	if !slices.Contains(env, "AGENT_BROWSER_ARGS=--disable-back-forward-cache") {
		t.Errorf("env = %v, want Chrome started with --disable-back-forward-cache", env)
	}
	if !slices.Contains(browserUnitArgs("/usr/bin/agent-browser", env, 1000, 2, 0), "--setenv=AGENT_BROWSER_ARGS=--disable-back-forward-cache") {
		t.Error("the browser unit must start Chrome with the back/forward cache off too")
	}
}

// TestBrowserUnitArgs_BoundEverythingTheBrowserRuns pins the systemd bounds
// of the warm browser: its own cgroup killed whole, a task and CPU ceiling,
// a max age, and its profile removed on every stop.
func TestBrowserUnitArgs_BoundEverythingTheBrowserRuns(t *testing.T) {
	args := browserUnitArgs("/usr/bin/agent-browser", browserEnv("/home/zerops", "/usr/bin"), 1000, 2, 4<<30)
	for _, want := range []string{
		"-n", "systemd-run",
		"--unit=" + browserUnit,
		"--uid=1000",
		"--service-type=forking",
		"--property=PIDFile=" + browserRuntimeDir + "/default.pid",
		"--property=RuntimeDirectory=zcp-browser",
		"--property=KillMode=control-group",
		"--property=KillSignal=SIGKILL",
		"--property=TasksMax=1000",
		"--property=CPUQuota=150%",
		"--property=MemoryMax=3221225472",
		"--property=OOMScoreAdjust=1000",
		"--property=RuntimeMaxSec=3600",
		"--property=ExecStopPost=/usr/bin/rm -rf " + browserTmpDir,
	} {
		if !slices.Contains(args, want) {
			t.Errorf("unit args miss %q: %v", want, args)
		}
	}
	if !slices.Equal(args[len(args)-3:], []string{"/usr/bin/agent-browser", "open", "about:blank"}) {
		t.Errorf("the unit must start the daemon through agent-browser open about:blank: %v", args)
	}
	if slices.ContainsFunc(args, func(a string) bool { return a == "--wait" || a == "--pipe" }) {
		t.Errorf("the unit outlives the call; no --wait/--pipe: %v", args)
	}
	unbounded := browserUnitArgs("/usr/bin/agent-browser", nil, 1000, 2, 0)
	if slices.ContainsFunc(unbounded, func(a string) bool { return strings.HasPrefix(a, "--property=MemoryMax=") }) {
		t.Errorf("an unknown container ceiling sets no MemoryMax: %v", unbounded)
	}
}

// TestReleaseBrowser_StopGetsItsOwnBoundAfterAHungHandBack pins that a
// hand-back stuck on a hung tab until its bound still leaves the stop a whole
// bound of its own, so a spinning page never keeps Chrome up.
func TestReleaseBrowser_StopGetsItsOwnBoundAfterAHungHandBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hung := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		stopCtxErr := errors.New("stop never ran")
		stop := func(ctx context.Context) error { stopCtxErr = ctx.Err(); return nil }
		err := releaseBrowser(context.Background(), true, 50*time.Millisecond, hung, stop)
		if stopCtxErr != nil {
			t.Errorf("the stop must start with its bound left, got %v", stopCtxErr)
		}
		if err == nil {
			t.Error("a failed hand-back must be reported")
		}
	})
}

// TestBrowserSetupSteps_OneStepPerIsolation pins how each isolation opens
// the call's page: a new tab shares the warm browser's history, a fresh
// context shares none; the logs of earlier calls are cleared either way.
func TestBrowserSetupSteps_OneStepPerIsolation(t *testing.T) {
	clears := [][]string{{"console", "--clear"}, {"errors", "--clear"}, {"network", "requests", "--clear"}}
	for isolation, open := range map[string][]string{
		BrowserIsolationTab:          {"tab", "new"},
		BrowserIsolationFreshContext: {"window", "new"},
		BrowserIsolationNewBrowser:   {"tab", "new"},
	} {
		got := browserSetupSteps(isolation)
		want := append(slices.Clone(clears), open)
		if !slices.EqualFunc(got, want, slices.Equal) {
			t.Errorf("%s: setup = %v, want %v", isolation, got, want)
		}
	}
}

// TestBrowserCleanupSteps_CloseEveryTabButTheBase pins the end of a call:
// switch to the base tab first (closing beside a hung active tab stalls),
// then close every other tab, popups included.
func TestBrowserCleanupSteps_CloseEveryTabButTheBase(t *testing.T) {
	list := `{"success":true,"data":{"tabs":[{"tabId":"t1","url":"about:blank"},{"tabId":"t4","url":"https://example.com"},{"tabId":"t5","url":"https://example.com/popup"}]},"error":null}`
	got, err := browserCleanupSteps(list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"tab", "t1"}, {"tab", "close", "t4"}, {"tab", "close", "t5"}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("cleanup = %v, want %v", got, want)
	}
	if _, err := browserCleanupSteps(`{"success":false,"error":"daemon gone"}`); err == nil {
		t.Error("an unreadable tab list must fail the cleanup, so the browser is recycled")
	}
}

// noResponse is agent-browser's answer to a step the daemon took and never
// answered within the client's own wait — on 2026-10-27 the open of a Vite dev
// server whose first request took 30 s.
const noResponse = `{"success":false,"data":null,"error":"Failed to read: Resource temporarily unavailable (os error 11) (the command reached the daemon but no response came back; it may or may not have run, so it was not retried)"}`

// pageProbeAnswer scripts the plain request a walk sends the page's server
// when its open got no answer.
func pageProbeAnswer(t *testing.T, status int, firstByte time.Duration, err error) *[]string {
	t.Helper()
	var asked []string
	t.Cleanup(OverrideBrowserPageProbeForTest(func(_ context.Context, url string) (int, time.Duration, error) {
		asked = append(asked, url)
		return status, firstByte, err
	}))
	return &asked
}

// TestRunBrowserSteps_OpenWithNoAnswer_ServerAnswers_IsASlowPage pins that an
// open the daemon never answered is no Chrome wedge when a plain request to
// the page gets an answer: the step says slow-page with the time to first
// byte, and the walk goes on within the call's own timeout, so the caller's
// waits give the page its time. On 2026-10-27 two such opens recycled Chrome
// although the server was only slow.
func TestRunBrowserSteps_OpenWithNoAnswer_ServerAnswers_IsASlowPage(t *testing.T) {
	asked := pageProbeAnswer(t, 200, 30200*time.Millisecond, nil)
	var seen [][]string
	run := scriptedSteps(map[string]answer{"open": {stdout: noResponse}}, &seen)
	steps := [][]string{{"open", "https://webdev-1df2-5173.prg1.zerops.app/"}, browserPaintWait, {"wait", "--text", "Sign in"}, {"errors"}}
	stdout, _, _, err := runBrowserSteps(context.Background(), steps, run)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var got []BrowserStepResult
	if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != len(steps) {
		t.Fatalf("every step must answer, the walk going on after the slow open; got %s (%v)", stdout, jerr)
	}
	if got[0].ErrorKind != ErrorKindSlowPage || got[0].Error == nil ||
		!strings.Contains(*got[0].Error, "first byte in 30.2s") || !strings.Contains(*got[0].Error, "HTTP 200") {
		t.Errorf("open = %+v, want slow-page naming its time to first byte", got[0])
	}
	if len(*asked) != 1 || (*asked)[0] != steps[0][1] {
		t.Errorf("asked %v, want one plain request to the opened page", *asked)
	}
	if _, wedged := scanStepsForCDPWedge(got); wedged {
		t.Error("a slow page reads as a CDP wedge, which recycles the browser")
	}
}

// TestRunBrowserSteps_OpenWithNoAnswer_NoServer_StaysAWedge pins the other
// side: when the page's server does not answer either, the open stays a wedge
// and the walk ends there, as before.
func TestRunBrowserSteps_OpenWithNoAnswer_NoServer_StaysAWedge(t *testing.T) {
	pageProbeAnswer(t, 0, 0, errors.New("dial tcp: connection refused"))
	var seen [][]string
	run := scriptedSteps(map[string]answer{"open": {stdout: noResponse}}, &seen)
	stdout, _, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "https://x.zerops.app/"}, browserPaintWait, {"errors"}}, run)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var got []BrowserStepResult
	if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != 1 {
		t.Fatalf("want the walk ended at the open, got %s (%v)", stdout, jerr)
	}
	if got[0].ErrorKind == ErrorKindSlowPage {
		t.Errorf("open = %+v, want no slow-page without a server answer", got[0])
	}
}

// TestRunBrowserSteps_ServerErrorPage_SkipsTheCallersCommands pins that a page
// whose server answered 5xx ends the caller's commands early: each comes back
// as a server-error step that did not run, while the screenshot and the canned
// tail still run, so the agent sees the error page and its network line. On
// 2026-10-27 a wait for text on a 502 page waited out its whole 60 s.
func TestRunBrowserSteps_ServerErrorPage_SkipsTheCallersCommands(t *testing.T) {
	tail := [][]string{{"screenshot", "/tmp/shot.png"}, {"errors"}, {"console"}, {"network", "requests", "--status", "400-599"}}
	tests := []struct {
		name  string
		open  answer
		probe int // the plain request's status, when the open got no answer
	}{
		{name: "the document answered 502", open: answer{stdout: `{"success":true,"data":{"url":"u","title":"502"},"error":null}`}},
		{name: "the open got no answer and the server answers 503", open: answer{stdout: noResponse}, probe: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pageProbeAnswer(t, tt.probe, 2*time.Second, nil)
			var seen [][]string
			run := func(_ context.Context, args []string) (string, string, error) {
				seen = append(seen, args)
				switch {
				case args[0] == "open":
					return tt.open.stdout, "", nil
				case args[0] == "eval" && slices.Equal(args, browserDocumentStatusProbe):
					return `{"success":true,"data":{"origin":"u","result":502},"error":null}`, "", nil
				}
				return `{"success":true,"data":{"ok":true},"error":null}`, "", nil
			}
			steps := append([][]string{{"open", "https://webdev-1df2-2222.prg1.zerops.app/"}, browserPaintWait,
				{"wait", "--text", "Pricing", "--timeout", "60000"}, {"click", "#buy"}}, tail...)
			stdout, _, _, err := runBrowserSteps(context.Background(), steps, run)
			if err != nil {
				t.Fatalf("walk: %v", err)
			}
			var got []BrowserStepResult
			if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != len(steps) {
				t.Fatalf("every step must come back in its place, got %s (%v)", stdout, jerr)
			}
			for _, i := range []int{2, 3} {
				if got[i].Success || got[i].ErrorKind != ErrorKindServerError || got[i].Error == nil || !strings.Contains(*got[i].Error, "HTTP 50") {
					t.Errorf("step %v = %+v, want a server-error step that did not run", steps[i], got[i])
				}
				if slices.ContainsFunc(seen, func(s []string) bool { return slices.Equal(s, steps[i]) }) {
					t.Errorf("step %v ran on a server-error page", steps[i])
				}
			}
			for _, cmd := range tail {
				if !slices.ContainsFunc(seen, func(s []string) bool { return slices.Equal(s, cmd) }) {
					t.Errorf("the tail step %v did not run", cmd)
				}
			}
		})
	}
}

// TestRunBrowserSteps_HealthyPage_RunsEveryCommand pins that the document's
// status read after the first paint is no step of the walk and changes
// nothing on a page that answered 200.
func TestRunBrowserSteps_HealthyPage_RunsEveryCommand(t *testing.T) {
	var seen [][]string
	run := func(_ context.Context, args []string) (string, string, error) {
		seen = append(seen, args)
		if args[0] == "eval" && slices.Equal(args, browserDocumentStatusProbe) {
			return `{"success":true,"data":{"origin":"u","result":200},"error":null}`, "", nil
		}
		return `{"success":true,"data":{"ok":true},"error":null}`, "", nil
	}
	steps := [][]string{{"open", "u"}, browserPaintWait, {"wait", "--text", "Pricing"}, {"errors"}}
	stdout, _, _, err := runBrowserSteps(context.Background(), steps, run)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var got []BrowserStepResult
	if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil || len(got) != len(steps) {
		t.Fatalf("want every step, got %s (%v)", stdout, jerr)
	}
	for _, s := range got {
		if !s.Success {
			t.Errorf("step %v failed on a healthy page: %+v", s.Command, s)
		}
	}
	if len(seen) != len(steps)+1 {
		t.Errorf("want each step plus one status read, sent %v", seen)
	}
}

// Every file the browser writes, the kept sign-in's cookies among them, is
// its user's alone.
// witness: internal/ops/browser_warm.go | 		"--property=UMask=0077", →
func TestBrowserUnitArgs_FilesTheBrowserWritesArePrivate(t *testing.T) {
	args := browserUnitArgs("/usr/local/bin/agent-browser", browserEnv("/home/agent", "/usr/bin"), 2000, 4, 0)
	if !slices.Contains(args, "--property=UMask=0077") {
		t.Errorf("the browser unit must make its files private: %v", args)
	}
}

// A clean call hands the browser back with its sign-in saved — before its
// tabs close, and before the browser is marked idle — so a recycle right
// after it, a page timeout in the next call, still finds the sign-in kept.
// witness: internal/ops/browser_warm.go | 	if stateDir != "" { → 	if false {
func TestHandBackBrowser_SavesTheSignInBeforeTheBrowserGoesIdle(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.cookies = []map[string]any{{"name": "sid", "value": "abc123", "domain": "127.0.0.1", "path": "/"}}
	dir := readyStateDir(t)
	var seen [][]string
	keptWhenIdle := ""
	_, err := handBackBrowser(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18731/"), dir, presenceIn(t, browserPresence{CookiesHeld: true}), "t2", func(context.Context) error {
		keptWhenIdle = keptState(t, dir)
		return nil
	})
	if err != nil {
		t.Fatalf("handBackBrowser: %v", err)
	}
	if !strings.Contains(keptWhenIdle, `"abc123"`) {
		t.Errorf("the browser went idle before its sign-in was kept: %q", keptWhenIdle)
	}
	closed := slices.IndexFunc(seen, func(a []string) bool { return slices.Equal(a, []string{"tab", "close", "t2"}) })
	saved := slices.IndexFunc(seen, func(a []string) bool { return slices.Equal(a, []string{"get", "cdp-url"}) })
	if saved < 0 || closed < saved {
		t.Errorf("the save must run before the call's tabs close: %v", seen)
	}
}

// A save that fails neither fails the hand-back nor recycles the browser:
// it goes back warm, and the last good save stands.
// witness: internal/ops/browser_warm.go | 			logBrowserSignIn(err) → 			return "", err
func TestHandBackBrowser_AFailedSaveStillHandsTheBrowserBack(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	unreached := func(ctx context.Context, args []string) (string, string, error) {
		if slices.Equal(args, []string{"get", "cdp-url"}) {
			return `{"success":false,"data":null,"error":"Browser not launched"}`, "", errors.New("exit status 1")
		}
		return browserOn(new([][]string), "t2 http://127.0.0.1:18731/")(ctx, args)
	}
	idle := false
	_, err := handBackBrowser(t.Context(), unreached, dir, presenceIn(t, browserPresence{CookiesHeld: true}), "t2", func(context.Context) error {
		idle = true
		return nil
	})
	if err != nil || !idle || keptState(t, dir) != signedInState {
		t.Errorf("a failed save must still hand the browser back idle, the last save standing: err=%v idle=%v", err, idle)
	}
}

// A save whose connection finds another Chrome than the one agent-browser
// listed — relaunched empty between the two — tells the call its sign-in
// was not kept.
// witness: internal/ops/browser_signin.go | 		return fmt.Errorf("%w: %w", errBrowserRelaunched, err) → 		return err
func TestHandBackBrowser_AnotherChromeThanTheListedOneSaysTheSignInWasNotKept(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	delete(chrome.pages, baseTarget)
	dir := readyStateDir(t)
	writeKept(t, dir)
	note, err := handBackBrowser(t.Context(), browserOn(new([][]string), "t3 http://127.0.0.1:18731/"), dir,
		presenceIn(t, browserPresence{CookiesHeld: true}), "t3", func(context.Context) error { return nil })
	if err != nil || note != browserSignInLostInTheCall {
		t.Errorf("another Chrome: note=%q err=%v", note, err)
	}
}

// A browser zcp recycles — after a timeout, a wedge, a spent fork budget — is
// stopped without the hand-back, so nothing of it is saved: the last good
// save stands.
// witness: internal/ops/browser_warm.go | 	if reuse { → 	if true {
func TestReleaseBrowser_ARecycledBrowserIsNeverHandedBack(t *testing.T) {
	handedBack, stopped := false, false
	err := releaseBrowser(t.Context(), false, time.Second,
		func(context.Context) error { handedBack = true; return nil },
		func(context.Context) error { stopped = true; return nil })
	if err != nil || handedBack || !stopped {
		t.Errorf("a recycle must stop the browser without handing it back: err=%v handedBack=%v stopped=%v", err, handedBack, stopped)
	}
}

// A save that finds Chrome relaunched empty during the call — its base tab
// another one — saves nothing over the kept sign-in, says so in the call's
// result, and leaves the browser holding nothing, so no later call reuses it.
// witness: internal/ops/browser_signin.go | 	if base != presence.BaseTarget { → 	if base == "" {
func TestHandBackBrowser_AChromeRelaunchedDuringTheCallSavesNothing(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18731"}, BaseTarget: "A7E47918804A4FDA"})
	newFakeChrome().dialedBy(t)
	note, err := handBackBrowser(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18731/"), dir, presence, "t2", func(context.Context) error { return nil })
	if err != nil || note == "" {
		t.Errorf("a relaunched Chrome: note=%q err=%v", note, err)
	}
	if keptState(t, dir) != signedInState {
		t.Errorf("the kept sign-in must stay as it was: %s", keptState(t, dir))
	}
	if p := presenceAfter(t, presence); p.CookiesHeld || len(p.Opened) != 0 {
		t.Errorf("the browser must be left holding nothing: %+v", p)
	}
}

// cpuReadings answers a CPU time per read from list, the last one ever after.
func cpuReadings(list ...time.Duration) func(context.Context) (time.Duration, error) {
	i := 0
	return func(context.Context) (time.Duration, error) {
		v := list[min(i, len(list)-1)]
		i++
		return v, nil
	}
}

// TestSettleWhileIdle_AHeavyPageClosingIsWaitedOutAndTheBrowserKept: a browser
// that is only closing a heavy page — Chrome burned 4.7 s of CPU in the 6 s
// after a three.js example on a Mate, then nothing — is waited out and kept,
// so a call that follows within seconds pays no launch and no restore; one
// still busy when the bound runs out runs a page of its own and is recycled
// (2026-11-22, two Mates relaunching Chrome on back-to-back calls).
// witness: internal/ops/browser_warm.go | if now-before <= window/10 { → if false {
func TestSettleWhileIdle_AHeavyPageClosingIsWaitedOutAndTheBrowserKept(t *testing.T) {
	t.Parallel()
	settling := cpuReadings(0, 500*time.Millisecond, time.Second, 1300*time.Millisecond, 1300*time.Millisecond)
	if !settleWhileIdle(t.Context(), settling, 5*time.Millisecond, time.Second) {
		t.Error("a browser whose CPU use fell to nothing must be kept")
	}
}

// TestSettleWhileIdle_ABrowserStillBusyAtTheBoundIsRecycled: a browser whose
// CPU use stays above a tenth of a CPU until the bound — a popup agent-browser
// never tracked, a renderer left spinning — is not handed on.
// witness: internal/ops/browser_warm.go | if now-before <= window/10 { → if true {
func TestSettleWhileIdle_ABrowserStillBusyAtTheBoundIsRecycled(t *testing.T) {
	t.Parallel()
	spent := time.Duration(0)
	spinning := func(context.Context) (time.Duration, error) {
		spent += 100 * time.Millisecond
		return spent, nil
	}
	if settleWhileIdle(t.Context(), spinning, 5*time.Millisecond, 50*time.Millisecond) {
		t.Error("a browser still busy at the bound must be recycled")
	}
	if settleWhileIdle(t.Context(), func(context.Context) (time.Duration, error) { return 0, errors.New("no systemd") }, 5*time.Millisecond, 50*time.Millisecond) {
		t.Error("a browser whose CPU cannot be read must not count as settled")
	}
}

// TestWarmBrowser_ReusableButBusyOnlyForTheIdleCPU: only a browser that is
// reusable in every other way but used CPU since the last call is waited out;
// one a call cannot reuse for any other reason is not.
// witness: internal/ops/browser_warm.go | busy.IdleCPU = -1 → busy.IdleCPU = w.IdleCPU
func TestWarmBrowser_ReusableButBusyOnlyForTheIdleCPU(t *testing.T) {
	t.Parallel()
	budget := 2 * time.Minute
	busy := warmBrowser{Active: true, Age: 10 * time.Minute, Marker: browserMarkerIdle, IdleFor: 8 * time.Second, IdleCPU: 5 * time.Second}
	if !busy.reusableButBusy(BrowserIsolationTab, budget) {
		t.Error("a clean, young browser that only used CPU since the last call must be waited out")
	}
	quiet := busy
	quiet.IdleCPU = 100 * time.Millisecond
	if quiet.reusableButBusy(BrowserIsolationTab, budget) {
		t.Error("a quiet browser is reused at once, not waited out")
	}
	died := busy
	died.Marker = browserMarkerBusy
	if died.reusableButBusy(BrowserIsolationTab, budget) || busy.reusableButBusy(BrowserIsolationNewBrowser, 30*time.Second) {
		t.Error("a browser a call died in, or a new-browser call, is never waited out")
	}
}

// TestBrowserFreshReason_SaysWhyACallStartsAFreshBrowser: each reason a call
// does not run on the warm browser is named, so a relaunch can be read from
// the container's system log; a reused browser names none.
// witness: internal/ops/browser_warm.go | return "it does not hold the kept sign-in" → return ""
func TestBrowserFreshReason_SaysWhyACallStartsAFreshBrowser(t *testing.T) {
	t.Parallel()
	budget := 2 * time.Minute
	w := warmBrowser{Active: true, Age: 10 * time.Minute, Marker: browserMarkerIdle, IdleFor: 8 * time.Second, IdleCPU: 100 * time.Millisecond}
	busy := w
	busy.IdleCPU = 5 * time.Second
	dead := w
	dead.Active = false
	for _, tt := range []struct {
		name, want string
		got        string
	}{
		{"reused", "", browserFreshReason(w, BrowserIsolationTab, budget, false, true)},
		{"new browser", "asked for a new browser", browserFreshReason(w, BrowserIsolationNewBrowser, budget, false, true)},
		{"none running", "no browser was running", browserFreshReason(dead, BrowserIsolationTab, budget, false, true)},
		{"still busy", "still busy after", browserFreshReason(busy, BrowserIsolationTab, budget, false, true)},
		{"busy but settled", "", browserFreshReason(busy, BrowserIsolationTab, budget, true, true)},
		{"no sign-in", "does not hold the kept sign-in", browserFreshReason(w, BrowserIsolationTab, budget, false, false)},
		{"too old for the call", "too old for the call's", browserFreshReason(w, BrowserIsolationTab, browserMaxAge, false, true)},
	} {
		if (tt.want == "") != (tt.got == "") || !strings.Contains(tt.got, tt.want) {
			t.Errorf("%s: reason %q, want one naming %q", tt.name, tt.got, tt.want)
		}
	}
}
