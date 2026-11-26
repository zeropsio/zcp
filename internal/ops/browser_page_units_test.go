package ops

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// non-parallel: these cases replace the DevTools dialer and page probe.
// witness: internal/ops/browser_warm.go | 				if !callerStep && (last.Error == nil || !strings.HasPrefix(*last.Error, browserNavigationNoAnswer)) { → 				if !callerStep {
// witness: internal/ops/browser_cdp.go | 		if errors.Is(nctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil { → 		if errors.Is(nctx.Err(), context.DeadlineExceeded) && false {
func TestBrowserPage_BPOpenbound_AnUncommittedOpenIsDiagnosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"slow page", 200, ErrorKindSlowPage},
		{"server error", 503, ErrorKindServerError},
		{"server does not answer", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				chrome := newFakeChrome().dialedBy(t)
				chrome.pages["t2"] = "about:blank"
				chrome.stall["https://example.invalid"] = true
				t.Cleanup(OverrideBrowserPageProbeForTest(func(ctx context.Context, _ string) (int, time.Duration, error) {
					if tc.status == 0 {
						<-ctx.Done()
						return 0, 0, ctx.Err()
					}
					return tc.status, 200 * time.Millisecond, nil
				}))
				b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
				run := func(ctx context.Context, args []string) (string, string, error) {
					if slices.Equal(args, []string{"get", "cdp-url"}) {
						return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1/devtools/browser/test"}}`, "", nil
					}
					return b.run(ctx, args)
				}
				steps, caller, _ := buildCanonicalBatch("https://example.invalid/", [][]string{{"get", "title"}}, "page.png", false)
				ctx, cancel := context.WithTimeout(t.Context(), 95*time.Second)
				defer cancel()
				started := time.Now()
				out, _, _, err := runCallSteps(ctx, steps, run, caller)
				if err != nil {
					t.Fatalf("walk: %v", err)
				}
				var got []BrowserStepResult
				if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != len(steps) {
					t.Fatalf("steps = %s (%v), want the completed walk", out, err)
				}
				if got[1].Success || got[1].ErrorKind != tc.want || got[1].Error == nil || !strings.Contains(*got[1].Error, browserNoAnswer) {
					t.Errorf("open = %+v, want unanswered %s", got[1], tc.want)
				}
				wantElapsed := 30 * time.Second
				if tc.status == 0 {
					wantElapsed += 30 * time.Second
					if got[3].Success || got[3].Error == nil || !strings.Contains(*got[3].Error, "open") {
						t.Errorf("caller step = %+v, want skip naming the uncommitted open", got[3])
					}
					var result BrowserBatchResult
					_, recycled := classifyRunResult(&result, out, "", nil, 95*time.Second)
					if !recycled || result.Steps[1].ErrorKind != ErrorKindWedge {
						t.Errorf("unanswered open = %+v, recycled = %v; want wedge and recycle", result.Steps[1], recycled)
					}
				}
				if elapsed := time.Since(started); elapsed != wantElapsed {
					t.Errorf("elapsed = %s, want %s", elapsed, wantElapsed)
				}
				if tc.status == 200 {
					verdict := pageVerdict(got)
					if !strings.Contains(verdict, "did not commit") || !strings.Contains(verdict, "HTTP 200") || strings.Contains(verdict, "agent-browser") {
						t.Errorf("slow-open verdict = %q, want direct navigation and probe status", verdict)
					}
				}
				for _, step := range [][]string{{"screenshot", "page.png"}, {"errors"}, {"console"}, {"network", "requests", "--status", "400-599"}} {
					if b.ranIn(step...) != "t2" {
						t.Errorf("diagnostic %v did not run on the page", step)
					}
				}
			})
		})
	}
	t.Run("call deadline", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			chrome := newFakeChrome().dialedBy(t)
			chrome.pages["t2"] = "about:blank"
			chrome.stall["https://example.invalid"] = true
			b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
			run := func(ctx context.Context, args []string) (string, string, error) {
				if slices.Equal(args, []string{"get", "cdp-url"}) {
					return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1/devtools/browser/test"}}`, "", nil
				}
				return b.run(ctx, args)
			}
			steps, caller, _ := buildCanonicalBatch("https://example.invalid/", nil, "", false)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, _, _, err := runCallSteps(ctx, steps, run, caller)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("walk error = %v, want the call deadline", err)
			}
		})
	})
}

// non-parallel: these cases replace the DevTools dialer.
// witness: internal/ops/browser_cdp.go | 			if ctx.Err() != nil { → 			if true {
func TestBrowserPage_BPObserver_AFailedObserverDoesNotStopTheWalk(t *testing.T) {
	for _, tc := range []struct {
		name            string
		failure         string
		navigateWithCDP bool
	}{
		{"tab list unavailable", "tabs", false},
		{"tab target unavailable", "target", false},
		{"CDP URL unavailable", "url", false},
		{"DevTools connection unavailable", "dial", false},
		{"target attach refused", "attach", false},
		{"runtime observation refused", "Runtime.enable", true},
		{"network observation refused", "Network.enable", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				chrome := newFakeChrome().dialedBy(t)
				if tc.failure != "attach" {
					chrome.pages["t2"] = "about:blank"
				}
				if tc.failure == "dial" {
					browserCDPDial = func(context.Context, string) (browserCDPConn, error) { return nil, errors.New("connection refused") }
				}
				chrome.refuseMethod = tc.failure
				b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
				run := func(ctx context.Context, args []string) (string, string, error) {
					if slices.Equal(args, []string{"tab", "list"}) && tc.failure == "tabs" {
						return `{"success":false,"error":"tabs unavailable"}`, "", nil
					}
					if slices.Equal(args, []string{"tab", "list"}) && tc.failure == "target" {
						return `{"success":true,"data":{"tabs":[{"tabId":"t2","active":true}]}}`, "", nil
					}
					if slices.Equal(args, []string{"get", "cdp-url"}) {
						if tc.failure == "url" {
							return `{"success":false,"error":"CDP URL unavailable"}`, "", nil
						}
						return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1/devtools/browser/test"}}`, "", nil
					}
					return b.run(ctx, args)
				}
				steps, caller, _ := buildCanonicalBatch("https://example.invalid/", [][]string{{"wait", "--text", "Ready"}}, "page.png", false)
				out, _, _, err := runCallSteps(t.Context(), steps, run, caller)
				if err != nil {
					t.Fatalf("walk: %v", err)
				}
				var got []BrowserStepResult
				if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != len(steps) {
					t.Fatalf("steps = %s (%v), want the completed walk", out, err)
				}
				for _, step := range got {
					if !step.Success {
						t.Errorf("step did not succeed: %+v", step)
					}
				}
				if viaCDP := b.ranIn("open", "https://example.invalid/") == ""; viaCDP != tc.navigateWithCDP {
					t.Errorf("navigation via CDP = %v, want %v", viaCDP, tc.navigateWithCDP)
				}
			})
		})
	}
}

// witness: internal/ops/browser.go | if mark == devsup.PageMarkHold { → if mark == devsup.PageMarkFinal {
func TestBrowserPage_BPDevsup_TheSupervisorsPageFailsTheCall(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		mark, says    string
		failure, wait bool
	}{
		{"hold", "web is starting.", true, true},
		{"final", "web exited with code 1.", true, false},
		{"", "Internal Server Error", false, false},
	} {
		got, ok := devsupPageFailure(tt.mark, tt.says)
		if ok != tt.failure {
			t.Errorf("mark %q: failure = %v, want %v", tt.mark, ok, tt.failure)
			continue
		}
		if !ok {
			continue
		}
		if got.Code != platform.ErrPrerequisiteMissing || !strings.Contains(got.Message, strconv.Quote(tt.says)) {
			t.Errorf("mark %q: code/message = %q/%q, want prerequisite and quoted page text", tt.mark, got.Code, got.Message)
		}
		if !strings.Contains(got.Suggestion, "zerops_dev_server action=status") || strings.Contains(got.Suggestion, "waitSeconds") != tt.wait {
			t.Errorf("mark %q: suggestion = %q, want status and waitSeconds = %v", tt.mark, got.Suggestion, tt.wait)
		}
		if !tt.wait && (!strings.Contains(got.Suggestion, "exit") || !strings.Contains(got.Suggestion, "log")) {
			t.Errorf("mark %q: suggestion = %q, want exit and log", tt.mark, got.Suggestion)
		}
	}
}

// non-parallel: the refusal case replaces the browser runner and scaler.
// witness: internal/ops/browser_warm.go | 		if callerFailed && callerStep { → 		if callerFailed && false {
// witness: internal/ops/browser.go | 		if why, ok := refusedCommand(cmd); ok { → 		if why, ok := refusedCommand(cmd); ok && false {
func TestBrowserPage_BPSkip_AFailedStepSkipsLaterCallerSteps(t *testing.T) {
	for _, tc := range []struct {
		name, errorText string
		first           []string
		recycle         bool
	}{
		{"missing element", "no element #missing", []string{"click", "#missing"}, false},
		{"hung script on a live page", "CDP command timed out", []string{"eval", "hang()"}, false},
		{"wedged browser", "CDP command timed out", []string{"click", "#stuck"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
				strings.Join(tc.first, " "): func(*tabsBrowser) (bool, string) { return false, tc.errorText },
			}}
			steps := [][]string{browserDefaultViewport, {"open", "https://example.invalid"}, browserPaintWait, tc.first, {"wait", "#next", "--timeout", "120000"}, {"console"}, {"screenshot", "page.png"}, {"errors"}, {"console"}, {"network", "requests", "--status", "400-599"}}
			out, _, _, err := walkBrowserSteps(t.Context(), steps, b.run, false, []bool{false, false, false, true, true, true, false, false, false, false})
			if err != nil {
				t.Fatalf("walk: %v", err)
			}
			var got []BrowserStepResult
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("decode steps: %v", err)
			}
			if len(got) != len(steps) {
				t.Fatalf("steps = %+v, want each step reported", got)
			}
			for _, at := range []int{4, 5} {
				if got[at].Success || got[at].Error == nil || *got[at].Error != "not run: an earlier caller step failed" {
					t.Errorf("step %d = %+v, want a skip because an earlier step failed", at, got[at])
				}
			}
			if b.ranIn("wait", "#next", "--timeout", "120000") != "" {
				t.Error("the wait after a failed click ran")
			}
			for _, at := range []int{6, 7, 8, 9} {
				if !got[at].Success {
					t.Errorf("diagnostic step %d did not run: %+v", at, got[at])
				}
			}
			var result BrowserBatchResult
			_, recycled := classifyRunResult(&result, out, "", nil, time.Second)
			if recycled != tc.recycle {
				t.Errorf("recycled = %v, want %v", recycled, tc.recycle)
			}
		})
	}
	t.Run("refused command", func(t *testing.T) {
		b := &tabsBrowser{tabs: []string{"t1"}, active: "t1"}
		t.Cleanup(OverrideBrowserRunnerForTest(&walkRunner{exec: b.run}))
		t.Cleanup(OverrideBrowserScaleForTest(func(context.Context) error { return nil }))
		result, err := BrowserBatch(t.Context(), BrowserBatchInput{URL: "https://example.invalid/", Commands: [][]string{{"open", "https://other.invalid/"}, {"get", "title"}}})
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
		if len(result.Steps) == 0 || !slices.Equal(result.Steps[0].Command, []string{"open", "https://other.invalid/"}) || result.Steps[0].ErrorKind != ErrorKindInvalidCommand {
			t.Errorf("first result = %+v, want the refused open", result.Steps)
		}
		if b.ranIn("get", "title") != "t1" {
			t.Error("the command after the refusal did not run")
		}
	})
}

// non-parallel: the existing fake DevTools connection replaces browserCDPDial.
// witness: internal/ops/browser_warm.go | 			if followTabs && w.page.session != nil && isCommand(step, browserCmdOpen) && len(step) > 1 { → 			if false {
// witness: internal/ops/browser_warm.go | 		result.Success, result.Error, result.Result = true, nil, slices.Clone(browserNoPaint) → 		result.Success, result.Error, result.Result = false, nil, slices.Clone(browserNoPaint)
func TestBrowserPage_BPPaint_AWalkDoesNotWaitForLoad(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		paint     bool
	}{
		{"page paints while load never ends", "https://app.example.invalid/", true},
		{"page never paints", "https://app.example.invalid/", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				chrome := newFakeChrome().dialedBy(t)
				chrome.pages["t2"] = "about:blank"
				b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
				run := func(ctx context.Context, args []string) (string, string, error) {
					if slices.Equal(args, []string{"get", "cdp-url"}) {
						return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1/devtools/browser/test"}}`, "", nil
					}
					if isCommand(args, "open") {
						<-ctx.Done() // agent-browser waits for a load that never fires
						return "", "", ctx.Err()
					}
					if slices.Equal(args, browserPaintWait) && !tc.paint {
						return `{"success":false,"error":"Wait timed out after 5000ms"}`, "", nil
					}
					return b.run(ctx, args)
				}
				steps, caller, _ := buildCanonicalBatch(tc.url, [][]string{{"get", "title"}}, "", false)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				out, _, _, err := runCallSteps(ctx, steps, run, caller)
				if err != nil {
					t.Fatalf("painted page was not walked: %v", err)
				}
				var got []BrowserStepResult
				if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != len(steps) {
					t.Fatalf("steps = %s (%v), want the completed walk", out, err)
				}
				if !got[1].Success || !got[2].Success || !got[3].Success {
					t.Errorf("open, paint and caller step = %+v, want the walk to continue", got[1:4])
				}
				if !tc.paint && (got[2].Error != nil || string(got[2].Result) != string(browserNoPaint)) {
					t.Errorf("the paint timeout result = %s, error = %v; want %s and no error", got[2].Result, got[2].Error, browserNoPaint)
				}
			})
		})
	}
}

// non-parallel: the existing fake DevTools connection replaces browserCDPDial.
// witness: internal/ops/browser_warm.go | 			if failure := w.page.session.failure(); failure != "" { → 			if failure := w.page.session.failure(); false {
// witness: internal/ops/browser_warm.go | 				return BrowserStepResult{Command: step, Error: &failure, ErrorKind: ErrorKindOther}, true, false → 				return BrowserStepResult{Command: step, Error: &failure}, true, false
func TestBrowserPage_BPFailed_AWaitReportsAnAlreadyFailedPage(t *testing.T) {
	for _, tc := range []struct {
		name, method, want string
		params             map[string]any
	}{
		{"uncaught exception", "Runtime.exceptionThrown", "Failed to fetch dynamically imported module", map[string]any{"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "Failed to fetch dynamically imported module"}}}},
		{"page error resembling a driver timeout", "Runtime.exceptionThrown", "CDP command timed out", map[string]any{"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "CDP command timed out"}}}},
		{"failed script", "Network.responseReceived", "HTTP 503", map[string]any{"type": "Script", "response": map[string]any{"status": 503, "url": "https://example.invalid/app.js"}}},
		{"failed document", "Network.responseReceived", "HTTP 502", map[string]any{"type": "Document", "response": map[string]any{"status": 502, "url": "https://example.invalid/"}}},
		{"other request", "Network.responseReceived", "", map[string]any{"type": "XHR", "response": map[string]any{"status": 503, "url": "https://example.invalid/api"}}},
		{"missing script", "Network.responseReceived", "", map[string]any{"type": "Script", "response": map[string]any{"status": 404, "url": "https://example.invalid/app.js"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				chrome := newFakeChrome().dialedBy(t)
				chrome.pages["t2"] = "about:blank"
				b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
				run := func(ctx context.Context, args []string) (string, string, error) {
					if slices.Equal(args, []string{"get", "cdp-url"}) {
						return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1/devtools/browser/test"}}`, "", nil
					}
					if slices.Equal(args, browserPaintWait) {
						chrome.send(map[string]any{"method": tc.method, "sessionId": "S1-t2", "params": tc.params})
						synctest.Wait()
					}
					return b.run(ctx, args)
				}
				steps, caller, _ := buildCanonicalBatch("https://example.invalid/", [][]string{{"wait", "--text", "Ready"}, {"click", "#next"}}, "", false)
				out, _, _, err := runCallSteps(t.Context(), steps, run, caller)
				if err != nil {
					t.Fatalf("walk: %v", err)
				}
				var got []BrowserStepResult
				if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != len(steps) {
					t.Fatalf("steps = %s (%v)", out, err)
				}
				if tc.want == "" {
					if !got[3].Success || !got[4].Success {
						t.Errorf("unrelated response stopped the walk: %+v", got[3:5])
					}
				} else if got[3].Success || got[3].Error == nil || !strings.Contains(*got[3].Error, tc.want) || !got[4].Success {
					t.Errorf("wait and next step = %+v, want failure naming %q and the next step run", got[3:5], tc.want)
				}
				if (b.ranIn("wait", "--text", "Ready") != "") != (tc.want == "") {
					t.Errorf("wait ran on page failure %q", tc.want)
				}
				var result BrowserBatchResult
				_, recycled := classifyRunResult(&result, out, "", nil, time.Second)
				if recycled {
					t.Error("a page-owned error recycled the browser")
				}
				for _, step := range got[len(got)-3:] {
					if !step.Success {
						t.Errorf("diagnostic did not run: %+v", step)
					}
				}
			})
		})
	}
}
