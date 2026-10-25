// Tests for: BrowserBatch — canonical agent-browser walk on the warm browser.
//
// These tests lock the contract the recipe workflow depends on:
//   - Tool auto-prepends ["open", url] and the first-paint wait, and
//     auto-appends ["errors"], ["console"], ["network requests"] — the
//     agent never manages lifecycle directly.
//   - Any "open", "close", "tab" or "window" the agent passes in Commands
//     does not run; eval runs and returns its value as text.
//   - Fork exhaustion (stderr only), a deadline, a cancel, a crash or a CDP
//     wedge release the browser for a recycle and say so in the result;
//     a clean call hands it back warm.
//   - A timed-out walk keeps the steps that finished.
//   - On a failed run, ErrorsOutput/ConsoleOutput are NOT populated from
//     partial steps — those fields must only reflect a clean walk.
//   - Calls wait for the container's browser lock until their deadline.
//
// These tests do NOT run in parallel. BrowserBatch uses a package-level
// browserRun global overridden via OverrideBrowserRunnerForTest; running
// tests in parallel would race the override.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBrowserRunner captures calls and returns scripted results.
type fakeBrowserRunner struct {
	lookPathErr error
	acquireErr  error
	fresh       bool // Acquire started the browser for the call

	lastIsolation string
	lastStdin     string
	lastTimeout   time.Duration
	runStdout     string
	runStderr     string
	runTruncated  bool
	runErr        error

	mu       sync.Mutex
	releases []bool // the reuse verdict of every release, in order
}

func (f *fakeBrowserRunner) LookPath() (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return "/usr/local/bin/agent-browser", nil
}

func (f *fakeBrowserRunner) Acquire(_ context.Context, isolation string) (func(reuse bool) error, bool, error) {
	if f.acquireErr != nil {
		return nil, false, f.acquireErr
	}
	f.lastIsolation = isolation
	return func(reuse bool) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.releases = append(f.releases, reuse)
		return nil
	}, f.fresh, nil
}

func (f *fakeBrowserRunner) Run(_ context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	f.lastStdin = stdin
	f.lastTimeout = timeout
	return f.runStdout, f.runStderr, f.runTruncated, f.runErr
}

// recycles counts the releases that asked for a fresh browser.
func (f *fakeBrowserRunner) recycles() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, reuse := range f.releases {
		if !reuse {
			n++
		}
	}
	return n
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

// TestBrowserBatch_PreScalesRAMBeforeRun pins the zerops_browser RAM-headroom
// fix: before launching agent-browser (Chrome spikes 300-700 MB and thrashes
// the 2 GiB agent container's cgroup ceiling, wedging every CDP command),
// BrowserBatch first calls browserScale to grant headroom. The scale MUST run
// BEFORE the runner — proven here by capturing the runner's stdin at
// scale-time (still empty) vs after the batch (populated).
func TestBrowserBatch_PreScalesRAMBeforeRun(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: "[]"}
	defer OverrideBrowserRunnerForTest(fake)()

	scaleCalled := false
	stdinAtScale := "sentinel"
	defer OverrideBrowserScaleForTest(func(context.Context) error {
		scaleCalled = true
		stdinAtScale = fake.lastStdin
		return nil
	})()

	if _, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !scaleCalled {
		t.Fatal("expected browserScale to be called before launching agent-browser")
	}
	if stdinAtScale != "" {
		t.Errorf("scale must run BEFORE the agent-browser Run; runner already had stdin %q at scale-time", stdinAtScale)
	}
	if fake.lastStdin == "" {
		t.Error("expected the runner to run after the scale")
	}
}

// TestBrowserBatch_ScaleFailureDoesNotBlock pins that the RAM pre-scale is
// best-effort: a zsc failure (CLI absent, maxRam ceiling, hung) must NOT fail
// the browser call — agent-browser proceeds and degrades exactly as before.
func TestBrowserBatch_ScaleFailureDoesNotBlock(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: "[]"}
	defer OverrideBrowserRunnerForTest(fake)()
	defer OverrideBrowserScaleForTest(func(context.Context) error {
		return errors.New("zsc scale ram: boom")
	})()

	if _, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"}); err != nil {
		t.Fatalf("scale failure must not fail the batch: %v", err)
	}
	if fake.lastStdin == "" {
		t.Error("expected the runner to run despite the scale failure")
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

// TestBrowserBatch_BuildsCanonicalShape pins the walk: the default viewport,
// open, the wait for
// the page's first contentful paint, the caller's commands, then errors,
// console and failed network requests. No close: the call ends by closing
// its tabs, and the warm browser stays for the next call.
func TestBrowserBatch_BuildsCanonicalShape(t *testing.T) {
	batch := [][]string{
		{"set", "viewport", "1280", "577"},
		{"open", "https://example.com/app"},
		{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"},
		{"snapshot", "-i", "-c"},
		{"click", "@e1"},
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
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
	if len(got) != len(batch) {
		t.Fatalf("expected %d commands in batch, got %d: %v", len(batch), len(got), got)
	}
	for i, want := range batch {
		if !slices.Equal(got[i], want) {
			t.Errorf("batch[%d] = %v, want %v", i, got[i], want)
		}
	}

	if result.URL != "https://example.com/app" {
		t.Errorf("result.URL = %q", result.URL)
	}
	if len(result.Steps) != len(batch) {
		t.Errorf("expected %d parsed steps, got %d", len(batch), len(result.Steps))
	}
	if len(result.ErrorsOutput) == 0 {
		t.Errorf("ErrorsOutput should be populated from the [errors] step")
	}
	if len(result.ConsoleOutput) == 0 {
		t.Errorf("ConsoleOutput should be populated from the [console] step")
	}
	if result.BrowserRecycled {
		t.Errorf("BrowserRecycled should be false on success")
	}
}

// TestBrowserBatch_PageLoadsAtTheLeadingViewport: a page that reads
// matchMedia or measures only on load is captured at the default width when
// the viewport is set after the open, so a leading set viewport runs before
// it. The steps come back in the order they ran, the agent's commands in the
// order sent; a set viewport later in the list stays where it was sent.
func TestBrowserBatch_PageLoadsAtTheLeadingViewport(t *testing.T) {
	batch := [][]string{
		{"set", "viewport", "390", "844"},
		{"open", "https://example.com/app"},
		{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"},
		{"snapshot", "-i", "-c"},
		{"set", "viewport", "1440", "900"},
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
	}
	fake := &fakeBrowserRunner{runStdout: makeStdout(t, batch)}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com/app",
		Commands: [][]string{
			{"set", "viewport", "390", "844"},
			{"snapshot", "-i", "-c"},
			{"set", "viewport", "1440", "900"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseStdinBatch(t, fake.lastStdin)
	if len(got) != len(batch) {
		t.Fatalf("expected %d commands in batch, got %d: %v", len(batch), len(got), got)
	}
	for i, want := range batch {
		if !slices.Equal(got[i], want) {
			t.Errorf("batch[%d] = %v, want %v", i, got[i], want)
		}
	}
	if len(result.Steps) != len(batch) || !slices.Equal(result.Steps[0].Command, batch[0]) {
		t.Errorf("steps must come back in the order they ran, the viewport first: %+v", result.Steps)
	}
	if len(result.ConsoleOutput) == 0 {
		t.Errorf("ConsoleOutput should still be read from the [console] step")
	}
}

// TestBrowserBatch_ACallThatSetsNoViewportLoadsAtTheDefaultSize: the warm
// Chrome keeps a set viewport, so after a call at 390 px the next call that
// set none loaded at 390 too, and a desktop check after a mobile one came
// back mobile. A call whose commands do not start with a set viewport runs
// [set viewport 1280 577], the size a fresh Chrome opens at, before the open.
func TestBrowserBatch_ACallThatSetsNoViewportLoadsAtTheDefaultSize(t *testing.T) {
	tests := []struct {
		name     string
		commands [][]string
	}{
		{"no commands", nil},
		{"commands without a viewport", [][]string{{"get", "title"}}},
		{"a viewport after other commands", [][]string{{"get", "title"}, {"set", "viewport", "390", "844"}}},
		{"a viewport after a stripped open", [][]string{{"open", "https://example.com"}, {"set", "device", "iPhone 12"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBrowserRunner{runStdout: `[]`}
			defer OverrideBrowserRunnerForTest(fake)()

			if _, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Commands: tt.commands}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := parseStdinBatch(t, fake.lastStdin)
			if len(got) < 2 || !slices.Equal(got[0], []string{"set", "viewport", "1280", "577"}) || !slices.Equal(got[1], []string{"open", "https://example.com"}) {
				t.Errorf("the batch must start with [set viewport 1280 577] then the open, got: %v", got)
			}
		})
	}
}

func TestBrowserBatch_StripsAgentOpenAndClose(t *testing.T) {
	// Agent erroneously includes open/close in their Commands. Tool must
	// strip both: the canonical open is the only one, and a close would end
	// the warm browser every other call shares.
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
	if closeCount != 0 {
		t.Errorf("expected no close (the warm browser stays), got %d: %v", closeCount, got)
	}
}

// TestBrowserBatch_StripsTabAndWindowCommands pins that the tool owns the
// call's tabs and contexts: a caller's tab or window command could close or
// switch the warm browser's base tab, which would then survive every cleanup.
func TestBrowserBatch_StripsTabAndWindowCommands(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: `[]`}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com",
		Commands: [][]string{
			{"tab", "close", "t1"},
			{"snapshot"},
			{"tab", "t1"},
			{"window", "new"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, cmd := range parseStdinBatch(t, fake.lastStdin) {
		if cmd[0] == "tab" || cmd[0] == "window" {
			t.Errorf("caller %v must be stripped: %v", cmd, fake.lastStdin)
		}
	}
	if !strings.Contains(fake.lastStdin, `["snapshot"]`) {
		t.Errorf("other commands must stay: %s", fake.lastStdin)
	}
}

// TestBrowserBatch_AcceptsSetAndEvalCommands pins the S7 vocabulary
// extension: `set viewport`, `set device`, `set media` (agent-browser
// 0.35.1 `set --help`: "viewport <w> <h> [scale]", "device <name>",
// "media [dark|light] [reduced-motion]") pass through unchanged as inner
// commands, and so does `eval`: it reads what get/is/find cannot.
func TestBrowserBatch_AcceptsSetAndEvalCommands(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		wantN   int // occurrences of the exact command expected in the sent batch
	}{
		{"set viewport passes through", []string{"set", "viewport", "1920", "1080"}, 1},
		{"set device passes through", []string{"set", "device", "iPhone 12"}, 1},
		{"set media passes through", []string{"set", "media", "dark"}, 1},
		{"eval passes through", []string{"eval", "document.title"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBrowserRunner{runStdout: `[]`}
			defer OverrideBrowserRunnerForTest(fake)()

			_, err := BrowserBatch(context.Background(), BrowserBatchInput{
				URL:      "https://example.com",
				Commands: [][]string{tt.command},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := parseStdinBatch(t, fake.lastStdin)
			n := 0
			for _, cmd := range got {
				if slices.Equal(cmd, tt.command) {
					n++
				}
			}
			if n != tt.wantN {
				t.Errorf("command %v occurrences=%d, want %d; batch: %v", tt.command, n, tt.wantN, got)
			}
		})
	}
}

// TestBrowserBatch_StillStripsOpenCloseBesideEval guards the "still
// strips open, close" half of the S7 vocabulary extension in the same
// batch a caller mixes them with a legitimate set command and an eval,
// which both run.
func TestBrowserBatch_StillStripsOpenCloseBesideEval(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: `[]`}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com",
		Commands: [][]string{
			{"open", "https://example.com"},
			{"set", "viewport", "1920", "1080"},
			{"eval", "document.title"},
			{"close"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := parseStdinBatch(t, fake.lastStdin)
	var openN, closeN, evalN, setN int
	for _, cmd := range got {
		switch cmd[0] {
		case "open":
			openN++
		case "close":
			closeN++
		case "eval":
			evalN++
		case "set":
			setN++
		}
	}
	if openN != 1 {
		t.Errorf("open occurrences = %d, want 1", openN)
	}
	if closeN != 0 {
		t.Errorf("close occurrences = %d, want 0 (stripped)", closeN)
	}
	if evalN != 1 {
		t.Errorf("eval occurrences = %d, want 1 (runs)", evalN)
	}
	if setN != 1 {
		t.Errorf("set occurrences = %d, want 1 (passes through)", setN)
	}
}

// TestBrowserBatch_RefusedCommandsAreFailedSteps pins that a command the tool
// does not run is reported, not dropped without a word: each refused open,
// close, tab or window comes back first in steps, in the order sent, as
// a failed invalid-command step saying what to use instead — an agent whose
// command vanished spent a call finding out. The walk itself is unchanged.
func TestBrowserBatch_RefusedCommandsAreFailedSteps(t *testing.T) {
	batch := [][]string{
		{"set", "viewport", "1280", "577"},
		{"open", "https://example.com"},
		{"wait", "--fn", "performance.getEntriesByName('first-contentful-paint').length>0", "--timeout", "5000"},
		{"get", "text", ".error"},
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
	}
	fake := &fakeBrowserRunner{runStdout: makeStdout(t, batch)}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL: "https://example.com",
		Commands: [][]string{
			{"open", "https://example.com/other"},
			{"get", "text", ".error"},
			{"tab", "new"},
			{"window", "new"},
			{"close"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := parseStdinBatch(t, fake.lastStdin); len(got) != len(batch) {
		t.Errorf("refused commands must not run; batch: %v", got)
	}
	refused := []struct {
		command []string
		says    string // a word of the alternative the step names
	}{
		{[]string{"open", "https://example.com/other"}, "url"},
		{[]string{"tab", "new"}, "isolation"},
		{[]string{"window", "new"}, "isolation"},
		{[]string{"close"}, "closes"},
	}
	if len(result.Steps) != len(refused)+len(batch) {
		t.Fatalf("steps = %d, want %d refused + %d run: %+v", len(result.Steps), len(refused), len(batch), result.Steps)
	}
	for i, want := range refused {
		step := result.Steps[i]
		if !slices.Equal(step.Command, want.command) {
			t.Errorf("steps[%d].command = %v, want %v", i, step.Command, want.command)
		}
		if step.Success {
			t.Errorf("steps[%d] %v: success = true, want false", i, want.command)
		}
		if step.ErrorKind != ErrorKindInvalidCommand {
			t.Errorf("steps[%d] %v: errorKind = %q, want %q", i, want.command, step.ErrorKind, ErrorKindInvalidCommand)
		}
		if step.Error == nil || !strings.Contains(*step.Error, want.says) {
			t.Errorf("steps[%d] %v: error must name the alternative (%q), got %v", i, want.command, want.says, step.Error)
		}
	}
	if !slices.Equal(result.Steps[len(refused)].Command, batch[0]) {
		t.Errorf("the run's steps must follow the refused ones, got %v", result.Steps[len(refused)].Command)
	}
	if len(result.ErrorsOutput) == 0 || len(result.ConsoleOutput) == 0 {
		t.Errorf("the reporting tail must still be read: errors=%s console=%s", result.ErrorsOutput, result.ConsoleOutput)
	}
}

// TestBrowserBatch_EvalReturnsItsValueAsText pins that an eval runs in its
// place among the caller's commands and its step's result is the value as
// JSON text — what get/is/find cannot read (localStorage, app state). A long
// value is cut and says so; a script that throws is a failed step carrying
// the page's error. Answers are agent-browser 0.39.1's, measured: data is
// {lifecycle, origin, result: <value>}, undefined comes back as null.
func TestBrowserBatch_EvalReturnsItsValueAsText(t *testing.T) {
	long := strings.Repeat("x", 50000)
	value := func(v string) string {
		return `{"success":true,"data":{"lifecycle":{"launched":false,"reused":true},"origin":"https://example.com","result":` + v + `},"error":null}`
	}
	answers := map[string]string{
		`localStorage.getItem("k")`: value(`"v"`),
		`({a:1,b:[1,2]})`:           value(`{"a":1,"b":[1,2]}`),
		`undefined`:                 value(`null`),
		`"x".repeat(50000)`:         value(`"` + long + `"`),
		`throw new Error("boom")`:   `{"success":false,"data":null,"error":"Evaluation error: Error: boom\n    at <anonymous>:1:7"}`,
	}
	commands := [][]string{
		{"eval", `localStorage.getItem("k")`},
		{"get", "text", "h1"},
		{"eval", `({a:1,b:[1,2]})`},
		{"eval", `undefined`},
		{"eval", `"x".repeat(50000)`},
		{"eval", `throw new Error("boom")`},
	}
	var seen [][]string
	batch, _ := buildCanonicalBatch("https://example.com", commands, "", false)
	stdout, _, _, err := runBrowserSteps(context.Background(), batch, func(_ context.Context, args []string) (string, string, error) {
		seen = append(seen, args)
		if args[0] == "eval" {
			return answers[args[1]], "", nil
		}
		return `{"success":true,"data":{"ok":true},"error":null}`, "", nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	fake := &fakeBrowserRunner{runStdout: stdout}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Commands: commands})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seen) < 3+len(commands) || !slices.EqualFunc(seen[3:3+len(commands)], commands, slices.Equal) {
		t.Fatalf("the caller's commands must run in order after open and the paint wait, ran %v", seen)
	}
	steps := map[string]BrowserStepResult{}
	for _, step := range result.Steps {
		if isCommand(step.Command, "eval") {
			steps[step.Command[1]] = step
		}
	}
	text := func(script string) string {
		t.Helper()
		step := steps[script]
		if !step.Success || step.Error != nil {
			t.Fatalf("eval %s: want a successful step, got %+v", script, step)
		}
		var s string
		if err := json.Unmarshal(step.Result, &s); err != nil {
			t.Fatalf("eval %s: result must be text, got %s", script, step.Result)
		}
		return s
	}
	for script, want := range map[string]string{
		`localStorage.getItem("k")`: `"v"`,
		`({a:1,b:[1,2]})`:           `{"a":1,"b":[1,2]}`,
		`undefined`:                 `null`,
	} {
		if got := text(script); got != want {
			t.Errorf("eval %s: result = %q, want %q", script, got, want)
		}
	}

	cut := text(`"x".repeat(50000)`)
	if !strings.HasPrefix(cut, `"`+strings.Repeat("x", 9000)) || len(cut) > 10200 {
		t.Errorf("a long value must keep its start and be cut near 10000 bytes, got %d bytes", len(cut))
	}
	if !strings.Contains(cut, "cut") || !strings.Contains(cut, "50002") {
		t.Errorf("a cut value must say it was cut and how long it was, ends %q", cut[len(cut)-120:])
	}

	thrown := steps[`throw new Error("boom")`]
	if thrown.Success || thrown.Error == nil || !strings.Contains(*thrown.Error, "Error: boom") {
		t.Errorf("a script that throws must be a failed step with the page's error, got %+v", thrown)
	}
	if len(result.ErrorsOutput) == 0 || result.BrowserRecycled {
		t.Errorf("a thrown eval fails its step, not the walk: errors=%s recycled=%v", result.ErrorsOutput, result.BrowserRecycled)
	}
}

// TestBrowserBatch_WrapsScreenshotBeforeErrorsConsoleWhenRequested pins
// the S7 screenshot batch shape: when Screenshot is requested, the walk
// becomes default viewport, open, paint wait, commands, screenshot <tmpfile>, errors,
// console, network requests — screenshot slots in right after
// the caller's commands, before the reporting tail. agent-browser
// 0.35.1 `screenshot --help`: "Usage: agent-browser screenshot
// [selector] [path]"; the annotate flag is not honoured inside `batch` (measured 2026-09-04),
// so the plain [screenshot <path>] form is emitted.
func TestBrowserBatch_WrapsScreenshotBeforeErrorsConsoleWhenRequested(t *testing.T) {
	fake := &fakeScreenshotRunner{png: testPNG(t, 10, 6)}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL:        "https://example.com",
		Commands:   [][]string{{"snapshot", "-i"}},
		Screenshot: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := parseStdinBatch(t, fake.lastStdin)
	if len(got) != 8 {
		t.Fatalf("expected 8 commands (default viewport, open, paint wait, snapshot, screenshot, errors, console, network requests), got %d: %v", len(got), got)
	}
	wantPrefix := [][]string{
		{"set", "viewport", "1280", "577"},
		{"open", "https://example.com"},
		browserPaintWait,
		{"snapshot", "-i"},
	}
	for i, w := range wantPrefix {
		if !slices.Equal(got[i], w) {
			t.Errorf("batch[%d] = %v, want %v", i, got[i], w)
		}
	}
	if len(got[4]) != 2 || got[4][0] != "screenshot" || got[4][1] == "" {
		t.Errorf("batch[4] must be [screenshot <path>], got: %v", got[4])
	}
	wantTail := [][]string{
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
	}
	for i, w := range wantTail {
		if !slices.Equal(got[5+i], w) {
			t.Errorf("batch[%d] = %v, want %v", 5+i, got[5+i], w)
		}
	}
}

// TestBrowserBatch_ReturnsScreenshotBytesAndDeletesTempFile pins the S7
// screenshot capture: BrowserBatch reads the PNG the "screenshot"
// command wrote to the temp path it generated, decodes width/height
// from the PNG itself (not from agent-browser's JSON, whose shape for
// this step was not observed live), and removes the temp file
// afterward regardless of outcome.
func TestBrowserBatch_ReturnsScreenshotBytesAndDeletesTempFile(t *testing.T) {
	png := testPNG(t, 12, 8)
	fake := &fakeScreenshotRunner{png: png}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL:        "https://example.com",
		Screenshot: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Screenshot == nil {
		t.Fatal("expected result.Screenshot to be populated")
	}
	if !slices.Equal(result.Screenshot.PNG, png) {
		t.Error("result.Screenshot.PNG does not match the bytes written to the temp file")
	}
	if result.Screenshot.Width != 12 || result.Screenshot.Height != 8 {
		t.Errorf("result.Screenshot dimensions = %dx%d, want 12x8", result.Screenshot.Width, result.Screenshot.Height)
	}
	if fake.capturedPath == "" {
		t.Fatal("fake runner never captured the screenshot path from stdin")
	}
	if _, statErr := os.Stat(fake.capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("expected temp screenshot file %q to be deleted, stat err: %v", fake.capturedPath, statErr)
	}
}

// TestBrowserBatch_FullPageScreenshotIsOneImageOfTheWholePage pins the
// full-page option: the screenshot step asks agent-browser for the whole page
// (agent-browser 0.38.2 `screenshot --help`: "--full, -f  Capture full page
// (not just viewport)") and the call returns that one image. An agent that
// could not ask for it scrolled and took eleven viewport screenshots instead.
func TestBrowserBatch_FullPageScreenshotIsOneImageOfTheWholePage(t *testing.T) {
	shot := testPNG(t, 12, 300)
	fake := &fakeScreenshotRunner{png: shot}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL:      "https://example.com",
		FullPage: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenshots [][]string
	for _, cmd := range parseStdinBatch(t, fake.lastStdin) {
		if cmd[0] == "screenshot" {
			screenshots = append(screenshots, cmd)
		}
	}
	if want := [][]string{{"screenshot", "--full", fake.capturedPath}}; !slices.EqualFunc(screenshots, want, slices.Equal[[]string]) {
		t.Fatalf("screenshot steps = %v, want %v", screenshots, want)
	}
	if result.Screenshot == nil {
		t.Fatal("a full-page call returned no screenshot")
	}
	if !slices.Equal(result.Screenshot.PNG, shot) {
		t.Error("the returned image is not the one agent-browser wrote")
	}
	if result.Screenshot.Width != 12 || result.Screenshot.Height != 300 || result.Screenshot.CapturedHeight != 0 {
		t.Errorf("screenshot = %dx%d (captured %d), want 12x300 uncut",
			result.Screenshot.Width, result.Screenshot.Height, result.Screenshot.CapturedHeight)
	}
	for _, step := range result.Steps {
		if step.Command[0] == "screenshot" && !slices.Equal(step.Command, []string{"screenshot", "--full"}) {
			t.Errorf("the result's screenshot step = %v, want [screenshot --full]", step.Command)
		}
	}
}

// TestBrowserBatch_ScreenshotTallerThanTheCapKeepsItsTop pins the bound on a
// returned image: one taller than browserScreenshotMaxHeight keeps its top
// that tall and says how tall the capture was. A full page of
// docs.zerops.io/references/import at 1365 px wide is 1365×10870 px (2.6 MB,
// agent-browser 0.38.2, 2026-10-16), and the Claude API refuses an image
// with a side over 8000 px.
func TestBrowserBatch_ScreenshotTallerThanTheCapKeepsItsTop(t *testing.T) {
	const tall = browserScreenshotMaxHeight + 50
	red := color.RGBA{R: 255, A: 255}
	green := color.RGBA{G: 255, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 3, tall))
	img.Set(0, 0, red)
	img.Set(0, browserScreenshotMaxHeight-1, green)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	fake := &fakeScreenshotRunner{png: buf.Bytes()}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", FullPage: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	shot := result.Screenshot
	if shot == nil {
		t.Fatal("a tall page returned no screenshot")
	}
	if shot.Width != 3 || shot.Height != browserScreenshotMaxHeight || shot.CapturedHeight != tall {
		t.Fatalf("screenshot = %dx%d (captured %d), want 3x%d (captured %d)",
			shot.Width, shot.Height, shot.CapturedHeight, browserScreenshotMaxHeight, tall)
	}
	got, err := png.Decode(bytes.NewReader(shot.PNG))
	if err != nil {
		t.Fatalf("the returned image is not a PNG: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 3 || b.Dy() != browserScreenshotMaxHeight {
		t.Errorf("the returned image is %dx%d, want 3x%d", b.Dx(), b.Dy(), browserScreenshotMaxHeight)
	}
	if c := color.RGBAModel.Convert(got.At(0, 0)); c != red {
		t.Errorf("top pixel = %v, want the page's top (red)", c)
	}
	if c := color.RGBAModel.Convert(got.At(0, browserScreenshotMaxHeight-1)); c != green {
		t.Errorf("last kept pixel = %v, want the page's row %d (green)", c, browserScreenshotMaxHeight-1)
	}
}

// TestBrowserBatch_NoScreenshotWhenNotRequested guards the default path:
// no screenshot step is added and result.Screenshot stays nil when the
// caller does not ask for one.
func TestBrowserBatch_NoScreenshotWhenNotRequested(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: `[]`}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Screenshot != nil {
		t.Errorf("expected no Screenshot, got %+v", result.Screenshot)
	}
	got := parseStdinBatch(t, fake.lastStdin)
	for _, cmd := range got {
		if cmd[0] == "screenshot" {
			t.Errorf("did not request a screenshot but batch contains a screenshot step: %v", got)
		}
	}
}

// TestBrowserBatch_ScreenshotTempPath_NotInResult pins that the temp file the
// screenshot is written to — deleted as BrowserBatch returns — is named
// nowhere in the result. An agent that read the path there linked it in its
// answer, and a Mate showed "Image unavailable" for a picture already gone.
// The picture goes back as its own image block; a screenshot the caller asked
// for at a path of its own keeps that path, which outlives the call.
func TestBrowserBatch_ScreenshotTempPath_NotInResult(t *testing.T) {
	const ownPath = "/home/zerops/shots/front.png"
	tests := []struct {
		name string
		// How agent-browser answers the screenshot step at path.
		answer func(path string) (result any, errText string)
	}{
		{
			name:   "the step echoes the path in its result",
			answer: func(path string) (any, string) { return map[string]any{"path": path}, "" },
		},
		{
			name: "the step fails naming the path",
			answer: func(path string) (any, string) {
				return nil, "failed to write " + path + ": no space left on device"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEchoScreenshotRunner{png: testPNG(t, 4, 4), answer: tt.answer}
			defer OverrideBrowserRunnerForTest(fake)()

			result, err := BrowserBatch(context.Background(), BrowserBatchInput{
				URL:        "https://example.com",
				Commands:   [][]string{{"screenshot", ownPath}},
				Screenshot: true,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fake.tempPath == "" {
				t.Fatal("the batch never carried the temp screenshot step")
			}
			out, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			if strings.Contains(string(out), fake.tempPath) {
				t.Errorf("result names the temp screenshot file %q: %s", fake.tempPath, out)
			}
			if !strings.Contains(string(out), ownPath) {
				t.Errorf("result lost the caller's own screenshot path %q: %s", ownPath, out)
			}
		})
	}
}

// fakeEchoScreenshotRunner writes the PNG wherever the batch asks for a
// screenshot, and answers each screenshot step as `answer` says for the temp
// one (the path BrowserBatch generated) — a path agent-browser may echo back.
type fakeEchoScreenshotRunner struct {
	fakeBrowserRunner
	png      []byte
	answer   func(path string) (result any, errText string)
	tempPath string
}

func (f *fakeEchoScreenshotRunner) Run(_ context.Context, stdin string, _ time.Duration) (string, string, bool, error) {
	var batch [][]string
	if err := json.Unmarshal([]byte(stdin), &batch); err != nil {
		return "", "", false, fmt.Errorf("fakeEchoScreenshotRunner: parse stdin: %w", err)
	}
	out := make([]map[string]any, 0, len(batch))
	for _, cmd := range batch {
		step := map[string]any{"command": cmd, "success": true, "result": map[string]any{"ok": true}}
		switch {
		case cmd[0] == "errors":
			step["result"] = map[string]any{"errors": []any{}}
		case cmd[0] == "console":
			step["result"] = map[string]any{"logs": []any{}}
		case cmd[0] == "screenshot" && len(cmd) == 2 && strings.Contains(cmd[1], "zcp-browser-screenshot-"):
			f.tempPath = cmd[1]
			if err := os.WriteFile(cmd[1], f.png, 0o600); err != nil {
				return "", "", false, fmt.Errorf("fakeEchoScreenshotRunner: write PNG: %w", err)
			}
			result, errText := f.answer(cmd[1])
			step["result"] = result
			if errText != "" {
				step["success"] = false
				step["error"] = errText
			}
		case cmd[0] == "screenshot" && len(cmd) == 2:
			step["result"] = map[string]any{"path": cmd[1]}
		}
		out = append(out, step)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", "", false, err
	}
	return string(b), "", false, nil
}

// testPNG encodes a minimal valid PNG of the given dimensions for tests
// that need real, decodable image bytes.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

// fakeScreenshotRunner simulates agent-browser writing a PNG to the path
// BrowserBatch put in the "screenshot" command's stdin, since the real
// production writer is the external agent-browser process, not Go code
// under test. It inspects lastStdin (set by the embedded fakeBrowserRunner's
// Run) to find that path.
type fakeScreenshotRunner struct {
	fakeBrowserRunner
	png          []byte
	capturedPath string
}

func (f *fakeScreenshotRunner) Run(ctx context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	var batch [][]string
	if err := json.Unmarshal([]byte(stdin), &batch); err != nil {
		return "", "", false, fmt.Errorf("fakeScreenshotRunner: parse stdin: %w", err)
	}
	for _, cmd := range batch {
		if len(cmd) >= 2 && cmd[0] == "screenshot" {
			f.capturedPath = cmd[len(cmd)-1]
			if err := os.WriteFile(f.capturedPath, f.png, 0o600); err != nil {
				return "", "", false, fmt.Errorf("fakeScreenshotRunner: write PNG: %w", err)
			}
		}
	}
	stdout, stderr, truncated, err := f.fakeBrowserRunner.Run(ctx, stdin, timeout)
	if stdout == "" {
		// Default canned output when the embedded fake wasn't pre-seeded.
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
			out = append(out, map[string]any{"command": cmd, "success": true, "result": res})
		}
		b, mErr := json.Marshal(out)
		if mErr != nil {
			return "", "", false, mErr
		}
		stdout = string(b)
	}
	return stdout, stderr, truncated, err
}

// TestBrowserBatch_AlwaysAppendsNetworkRequestsToTail pins the S7 tail
// extension: every batch ends errors, console, network requests
// — the outcome statement says
// zerops_browser "always reports failed network requests next to errors
// and console". The tail step is bound to 4xx/5xx at the source
// (`network requests --status 400-599`) rather than requesting the full
// log: measured on z3-eval, agent-browser 0.35.1's `network requests
// --json` runs ~1 KB/request (headers/responseHeaders/postData
// included) — nytimes.com (471 reqs/845 KB) and amazon.com (614
// reqs/1.33 MB) both blow the batch's 1 MiB stdout cap unfiltered,
// which would truncate the WHOLE batch output, not just the network
// tail. `--status <code>` accepts "200", "2xx", or a "400-499"-style
// range per `--help`.
func TestBrowserBatch_AlwaysAppendsNetworkRequestsToTail(t *testing.T) {
	fake := &fakeBrowserRunner{runStdout: `[]`}
	defer OverrideBrowserRunnerForTest(fake)()

	_, err := BrowserBatch(context.Background(), BrowserBatchInput{
		URL:      "https://example.com",
		Commands: [][]string{{"snapshot", "-i"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := parseStdinBatch(t, fake.lastStdin)
	if len(got) != 7 {
		t.Fatalf("expected 7 commands (default viewport, open, paint wait, snapshot, errors, console, network requests), got %d: %v", len(got), got)
	}
	want := [][]string{
		{"set", "viewport", "1280", "577"},
		{"open", "https://example.com"},
		browserPaintWait,
		{"snapshot", "-i"},
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
	}
	for i, w := range want {
		if !slices.Equal(got[i], w) {
			t.Errorf("batch[%d] = %v, want %v", i, got[i], w)
		}
	}
}

// TestBrowserBatch_ParsesNetworkOutputFilteredToFailures pins the S7
// NetworkOutput extraction, bound at the source by the follow-up fix:
// the auto-appended tail step is `network requests --status 400-599`
// (agent-browser 0.35.1 `--help`: "--status <code>  Filter by status
// (200, 2xx, 400-499)"), so the step result should already be 4xx/5xx
// only; parseNetworkRequests keeps its own `Status >= 400` filter as a
// second line of defense. Measured live on z3-eval with --status
// applied: entries carry no "failure" field, and a request that failed
// at the network layer or is still in flight has no "status" key either
// — indistinguishable from each other, and not reported by this filter.
// A clean 200 response (which --status should already exclude, but the
// Go-side filter must still reject if the CLI ever returns one) is
// dropped here too.
func TestBrowserBatch_ParsesNetworkOutputFilteredToFailures(t *testing.T) {
	networkResult := `{"requests":[` +
		`{"url":"https://example.com/","method":"GET","status":200},` +
		`{"url":"https://example.com/api","method":"GET","status":404,"statusText":"Not Found"},` +
		`{"url":"https://example.com/broken","method":"POST","status":500}` +
		`]}`
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": true, "result": map[string]any{}},
		{"command": []string{"errors"}, "success": true, "result": map[string]any{"errors": []any{}}},
		{"command": []string{"console"}, "success": true, "result": map[string]any{"logs": []any{}}},
		{"command": []string{"network", "requests", "--status", "400-599"}, "success": true, "result": json.RawMessage(networkResult)},
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
	if len(result.NetworkOutput) != 2 {
		t.Fatalf("expected 2 4xx/5xx requests, got %d: %+v", len(result.NetworkOutput), result.NetworkOutput)
	}
	urls := make([]string, len(result.NetworkOutput))
	for i, r := range result.NetworkOutput {
		urls[i] = r.URL
	}
	wantURLs := []string{"https://example.com/api", "https://example.com/broken"}
	for _, w := range wantURLs {
		if !slices.Contains(urls, w) {
			t.Errorf("expected NetworkOutput to contain %q; got %v", w, urls)
		}
	}
	if slices.Contains(urls, "https://example.com/") {
		t.Error("clean 200 request must be filtered out of NetworkOutput")
	}
}

// TestClassifyStepError_Table pins the S7 per-step ErrorKind
// classification: agent-browser's own error text (the step JSON's
// "error" field — see makeStdout / BrowserStepResult.Error, not raw
// process stderr, since agent-browser's --json output surfaces per-step
// failures there) is mapped to a coarse kind so callers can branch
// without re-parsing free text. Wedge signals take priority over the
// generic "timeout" match — "CDP command timed out" must classify as
// wedge, not timeout, because it is also a cdpWedgeSignals entry that
// recycles the browser.
func TestClassifyStepError_Table(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"CDP timeout is wedge not timeout", "CDP command timed out: DOM.enable", ErrorKindWedge},
		{"target closed is wedge", "Target closed", ErrorKindWedge},
		{"protocol error is wedge", "Protocol error (Page.navigate): unknown", ErrorKindWedge},
		{"a daemon that never answers is wedge", "Failed to read: Resource temporarily unavailable (os error 11) (the command reached the daemon but no response came back; it may or may not have run, so it was not retried)", ErrorKindWedge},
		{"selector not found", `no element matching selector "#missing"`, ErrorKindSelectorNotFound},
		{"no such element", "no such element: @e9", ErrorKindSelectorNotFound},
		{"not editable", "Element is not editable", ErrorKindNotEditable},
		{"generic timeout", "Timeout 25000ms exceeded waiting for element to be visible", ErrorKindTimeout},
		{"wait timeout", "Wait timed out after 25000ms", ErrorKindTimeout},
		{"selector missing in the DOM", "Element not found: #late. Verify the selector, role, or name is correct and the element exists in the DOM.", ErrorKindSelectorNotFound},
		{"find with an action it lacks is invalid-command", "Unknown action 'get' for find. Valid actions: click, fill, check, hover, text.", "invalid-command"},
		{"find with the name as a positional is invalid-command", "Unknown action 'Order' for find. Valid actions: click, fill, check, hover, text.", "invalid-command"},
		{"unknown command is invalid-command", "Unknown command: foo", "invalid-command"},
		{"unknown get or is subcommand is invalid-command", "Unknown subcommand: foo\nValid options: visible, enabled, checked", "invalid-command"},
		{"ref outside its snapshot's tab is stale-ref", "Unknown ref: e99", "stale-ref"},
		{"unrecognized falls to other", "some unexpected agent-browser failure", ErrorKindOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyStepError(tt.msg); got != tt.want {
				t.Errorf("classifyStepError(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

// TestBrowserBatch_PopulatesStepErrorKind guards the wiring: BrowserBatch
// must stamp ErrorKind on every parsed step that carries an Error, using
// the same classification classifyStepError pins, not just leave the
// field unset.
func TestBrowserBatch_PopulatesStepErrorKind(t *testing.T) {
	errMsg := "Timeout 25000ms exceeded waiting for element to be visible"
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": true, "result": map[string]any{}},
		{"command": []string{"click", "@e1"}, "success": false, "error": errMsg},
		{"command": []string{"errors"}, "success": true, "result": map[string]any{"errors": []any{}}},
		{"command": []string{"console"}, "success": true, "result": map[string]any{"logs": []any{}}},
		{"command": []string{"network", "requests"}, "success": true, "result": map[string]any{"requests": []any{}}},
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
	if len(result.Steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(result.Steps))
	}
	if got := result.Steps[1].ErrorKind; got != ErrorKindTimeout {
		t.Errorf("Steps[1].ErrorKind = %q, want %q", got, ErrorKindTimeout)
	}
	if got := result.Steps[0].ErrorKind; got != "" {
		t.Errorf("Steps[0] (no error) ErrorKind = %q, want empty", got)
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
	if !result.BrowserRecycled {
		t.Error("expected BrowserRecycled=true")
	}
	if fake.recycles() != 1 {
		t.Errorf("expected 1 recovery call, got %d", fake.recycles())
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
	if !result.BrowserRecycled {
		t.Error("pthread_create should trigger recovery")
	}
}

// TestBrowserBatch_ForkSignatureOnlyInStdoutNoRecovery guards the stderr-only
// detection policy: agent-browser's own JSON stdout may legitimately contain
// text like "resource temporarily unavailable" (page title, console log from
// the browsed site) and MUST NOT spuriously trigger owned cleanup.
func TestBrowserBatch_ForkSignatureOnlyInStdoutNoRecovery(t *testing.T) {
	// A tiny valid JSON array with a result containing the matching text.
	stdoutWithMatch := `[{"command":["open","https://example.com"],"success":true,"result":{"title":"Resource temporarily unavailable - my-app"}},{"command":["errors"],"success":true,"result":{"errors":[]}},{"command":["console"],"success":true,"result":{"logs":[]}}]`
	fake := &fakeBrowserRunner{
		runStdout: stdoutWithMatch,
		runStderr: "", // clean stderr — the only place we check
	}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BrowserRecycled {
		t.Error("fork signature in stdout must NOT trigger recovery — stderr is the only trusted source")
	}
	if fake.recycles() != 0 {
		t.Errorf("expected 0 recovery calls, got %d", fake.recycles())
	}
}

func TestBrowserBatch_ContextDeadlineTriggersRecovery(t *testing.T) {
	fake := &fakeBrowserRunner{runErr: context.DeadlineExceeded}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.BrowserRecycled {
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
	if !result.BrowserRecycled {
		t.Error("daemon crash with no stdout should trigger recovery")
	}
	if fake.recycles() != 1 {
		t.Errorf("expected 1 recovery call, got %d", fake.recycles())
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
// happen to be the canonical errors/console/network sequence.
func TestBrowserBatch_ErrorsOutputNotPopulatedOnFailedRun(t *testing.T) {
	// Full valid JSON with canonical tail, but runErr is set.
	batch := [][]string{
		{"open", "https://example.com"},
		{"errors"},
		{"console"},
		{"network", "requests", "--status", "400-599"},
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

// TestBrowserBatch_IsolationReachesTheBrowser pins the one isolation
// choice: the default is a tab in the warm browser, and fresh-context and
// new-browser are handed to the runner as asked. Anything else is refused
// before the browser is touched.
func TestBrowserBatch_IsolationReachesTheBrowser(t *testing.T) {
	tests := []struct {
		name, isolation, want string
	}{
		{"default is a tab", "", BrowserIsolationTab},
		{"tab", "tab", BrowserIsolationTab},
		{"fresh context", "fresh-context", BrowserIsolationFreshContext},
		{"new browser", "new-browser", BrowserIsolationNewBrowser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBrowserRunner{runStdout: `[]`}
			defer OverrideBrowserRunnerForTest(fake)()
			if _, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Isolation: tt.isolation}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fake.lastIsolation != tt.want {
				t.Errorf("runner got isolation %q, want %q", fake.lastIsolation, tt.want)
			}
		})
	}
	t.Run("unknown isolation is refused", func(t *testing.T) {
		fake := &fakeBrowserRunner{runStdout: `[]`}
		defer OverrideBrowserRunnerForTest(fake)()
		_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Isolation: "incognito"})
		if err == nil || !strings.Contains(err.Error(), "isolation") {
			t.Fatalf("want an isolation error, got %v", err)
		}
		if fake.lastStdin != "" || fake.lastIsolation != "" {
			t.Error("a refused isolation must not reach the browser")
		}
	})
}

// TestBrowserBatch_WarmBrowserStaysOnlyAfterACleanCall pins when the warm
// browser is handed back for the next call: a walk that ended on its own,
// even with a failed step, keeps it; a deadline, a cancel, fork exhaustion,
// a crash or a CDP wedge recycle it, so the next call starts a clean Chrome.
func TestBrowserBatch_WarmBrowserStaysOnlyAfterACleanCall(t *testing.T) {
	failedClick := `[{"command":["open","https://example.com"],"success":true,"result":{}},` +
		`{"command":["click","@e9"],"success":false,"error":"no element @e9"},` +
		`{"command":["errors"],"success":true,"result":{"errors":[]}},` +
		`{"command":["console"],"success":true,"result":{"logs":[]}},` +
		`{"command":["network","requests","--status","400-599"],"success":true,"result":{"requests":[]}}]`
	wedged := `[{"command":["open","https://example.com"],"success":false,"error":"CDP command timed out: Page.navigate"}]`
	tests := []struct {
		name   string
		fake   *fakeBrowserRunner
		cancel bool
		reuse  bool
	}{
		{"clean walk keeps it", &fakeBrowserRunner{runStdout: `[]`}, false, true},
		{"failed step keeps it", &fakeBrowserRunner{runStdout: failedClick}, false, true},
		{"deadline recycles", &fakeBrowserRunner{runErr: context.DeadlineExceeded}, false, false},
		{"cancel recycles", &fakeBrowserRunner{runStdout: `[]`, runErr: context.Canceled}, true, false},
		{"fork exhaustion recycles", &fakeBrowserRunner{runStderr: "fork failed: resource temporarily unavailable", runErr: errors.New("exit status 1")}, false, false},
		{"crash recycles", &fakeBrowserRunner{runErr: errors.New("signal: killed")}, false, false},
		{"wedge recycles", &fakeBrowserRunner{runStdout: wedged}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer OverrideBrowserRunnerForTest(tt.fake)()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			result, err := BrowserBatch(ctx, BrowserBatchInput{URL: "https://example.com"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(tt.fake.releases, []bool{tt.reuse}) {
				t.Errorf("releases = %v, want exactly [%v]", tt.fake.releases, tt.reuse)
			}
			if result.BrowserRecycled == tt.reuse {
				t.Errorf("BrowserRecycled = %v, want %v", result.BrowserRecycled, !tt.reuse)
			}
		})
	}
}

// TestBrowserBatch_FreshContextEndsWithItsBrowser pins that a fresh-context
// call leaves nothing behind: agent-browser cannot dispose a browser context,
// so the browser holding it is stopped when the call ends, while a clean call
// is still not reported as recycled.
func TestBrowserBatch_FreshContextEndsWithItsBrowser(t *testing.T) {
	for isolation, reuse := range map[string]bool{
		BrowserIsolationTab:          true,
		BrowserIsolationFreshContext: false,
		BrowserIsolationNewBrowser:   true,
	} {
		fake := &fakeBrowserRunner{runStdout: `[]`}
		restore := OverrideBrowserRunnerForTest(fake)
		result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Isolation: isolation})
		restore()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", isolation, err)
		}
		if !slices.Equal(fake.releases, []bool{reuse}) {
			t.Errorf("%s: releases = %v, want exactly [%v]", isolation, fake.releases, reuse)
		}
		if result.BrowserRecycled {
			t.Errorf("%s: a clean call must not report browserRecycled", isolation)
		}
	}
}

// TestBrowserBatch_SaysWhenTheCallRanInAFreshChrome pins that the result
// tells the agent whether its call ran in a Chrome started for it — always
// after new-browser — apart from browserRecycled, which only says the
// browser ended with the call. Nothing in a fresh call's result says reused.
func TestBrowserBatch_SaysWhenTheCallRanInAFreshChrome(t *testing.T) {
	lifecycle := func(launched bool) string {
		return fmt.Sprintf(`"lifecycle":{"effectiveLaunch":{"browserLaunched":true,"engine":"chrome","launchHash":11677035664275010974},"launched":%v,"reused":true}`, launched)
	}
	run := scriptedSteps(map[string]answer{
		"open": {stdout: `{"success":true,"data":{` + lifecycle(false) + `,"title":"Example","url":"https://example.com/"},"error":null}`},
		"get":  {stdout: `{"success":true,"data":{` + lifecycle(false) + `,"title":"Example"},"error":null}`},
	}, new([][]string))
	stdout, _, _, err := runBrowserSteps(context.Background(), [][]string{{"open", "https://example.com"}, {"get", "title"}, {"errors"}}, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tests := []struct {
		name, isolation string
		fresh           bool
	}{
		{"new-browser starts Chrome for the call", BrowserIsolationNewBrowser, true},
		{"a tab beside no warm browser starts one", BrowserIsolationTab, true},
		{"a tab in the warm browser is not fresh", BrowserIsolationTab, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBrowserRunner{runStdout: stdout, fresh: tt.fresh}
			defer OverrideBrowserRunnerForTest(fake)()
			result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Isolation: tt.isolation})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			wire, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			if got := strings.Contains(string(wire), `"freshBrowser":true`); got != tt.fresh {
				t.Errorf("freshBrowser in result = %v, want %v: %s", got, tt.fresh, wire)
			}
			if strings.Contains(string(wire), "reused") {
				t.Errorf("the result must not say reused: %s", wire)
			}
			if strings.Contains(string(wire), "browserRecycled") {
				t.Errorf("a clean call must not report browserRecycled: %s", wire)
			}
		})
	}
}

// TestBrowserBatch_TimeoutKeepsFinishedSteps pins that a walk cut by its
// deadline still returns the steps that finished, so the agent sees how far
// the page got, while the reporting fields stay empty.
func TestBrowserBatch_TimeoutKeepsFinishedSteps(t *testing.T) {
	finished := `[{"command":["open","https://example.com"],"success":true,"result":{"url":"https://example.com"}},` +
		`{"command":["click","@e1"],"success":true,"result":{}}]`
	fake := &fakeBrowserRunner{runStdout: finished, runErr: context.DeadlineExceeded}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Commands: [][]string{{"click", "@e1"}, {"wait", "60000"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Steps) != 2 || !isCommand(result.Steps[1].Command, "click") {
		t.Fatalf("finished steps lost on timeout: %+v", result.Steps)
	}
	if !strings.Contains(result.Message, "timed out") {
		t.Errorf("message should say the walk timed out: %q", result.Message)
	}
	if len(result.ErrorsOutput) != 0 || len(result.ConsoleOutput) != 0 {
		t.Error("a timed-out walk must not populate the reporting fields")
	}
}

// TestBrowserBatch_AcquireFailureIsAnError pins that a browser that could
// not be readied (busy past the deadline, or failed to start) fails the call
// before any step runs.
func TestBrowserBatch_AcquireFailureIsAnError(t *testing.T) {
	fake := &fakeBrowserRunner{acquireErr: errors.New("the browser is busy with another call")}
	defer OverrideBrowserRunnerForTest(fake)()
	_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("want the acquire error, got %v", err)
	}
	if fake.lastStdin != "" {
		t.Error("no step may run without the browser")
	}
}

// TestBrowserBatch_CDPTimeoutInStepsTriggersRecovery: daemon returned
// exit 0 with a step error matching a CDP-wedge signal. Tool must
// recycle the browser, set BrowserRecycled, and surface a message
// naming the triggering signal — the next call starts a clean Chrome.
func TestBrowserBatch_CDPTimeoutInStepsTriggersRecovery(t *testing.T) {
	errMsg := "CDP command timed out: DOM.enable"
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": false, "error": errMsg},
		{"command": []string{"errors"}, "success": true, "result": map[string]any{"errors": []any{}}},
		{"command": []string{"console"}, "success": true, "result": map[string]any{"logs": []any{}}},
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
	if !result.BrowserRecycled {
		t.Error("CDP-timeout in step errors must recycle the browser")
	}
	if fake.recycles() != 1 {
		t.Errorf("expected 1 recycle, got %d", fake.recycles())
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

// TestBrowserBatch_EvalNeverSettledOnALivePage_KeepsTheBrowser pins that an
// eval whose promise never settled on a page that still answered is no
// wedge: the result carries no "Chrome wedged" message, the browser is handed
// back warm, the canned tail is read, and the step says script-timeout.
func TestBrowserBatch_EvalNeverSettledOnALivePage_KeepsTheBrowser(t *testing.T) {
	commands := [][]string{{"eval", settleScript}, {"get", "title"}}
	batch, _ := buildCanonicalBatch("https://example.com", commands, "", false)
	var seen [][]string
	stdout, _, _, err := runBrowserSteps(context.Background(), batch, probedSteps(t, answer{stdout: `{"success":true,"data":{"result":1},"error":null}`}, &seen))
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	fake := &fakeBrowserRunner{runStdout: stdout}
	defer OverrideBrowserRunnerForTest(fake)()

	result, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com", Commands: commands})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.Message, "Chrome wedged") {
		t.Errorf("a script's own hang is no wedge; message %q", result.Message)
	}
	if result.BrowserRecycled || fake.recycles() != 0 {
		t.Errorf("the browser must be handed back warm, recycled=%v recycles=%d", result.BrowserRecycled, fake.recycles())
	}
	if len(result.ErrorsOutput) == 0 {
		t.Error("the canned tail must be read: errorsOutput is empty")
	}
	var kind string
	for _, step := range result.Steps {
		if isCommand(step.Command, "eval") {
			kind = step.ErrorKind
		}
	}
	if kind != "script-timeout" {
		t.Errorf("the eval must say script-timeout, got %q", kind)
	}
}

// TestBrowserBatch_TargetClosedAlsoTriggersRecovery: the wedge
// detection fires on "Target closed" too, not only CDP timeout —
// every known-wedge signature in cdpWedgeSignals is load-bearing.
func TestBrowserBatch_TargetClosedAlsoTriggersRecovery(t *testing.T) {
	errMsg := "Target closed"
	steps := []map[string]any{
		{"command": []string{"open", "https://example.com"}, "success": false, "error": errMsg},
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
	if !result.BrowserRecycled {
		t.Error("'Target closed' in step errors must recycle the browser")
	}
}
