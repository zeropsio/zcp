package ops

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
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
		"AGENT_BROWSER_IDLE_TIMEOUT_MS=300000",
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
