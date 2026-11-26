package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// tabsBrowser answers a call's walk as agent-browser 0.39.1 does for its tabs:
// `tab list` lists them in the order they opened, `tab t<N>` makes one
// active, and a page's own effects — opening a popup, a popup closing itself —
// come from on, keyed by the command, with the active tab when it ran. It
// records each command and the tab it ran in.
type tabsBrowser struct {
	tabs   []string
	active string
	on     map[string]func(b *tabsBrowser) (ok bool, err string)
	ran    []tabsBrowserStep
}

type tabsBrowserStep struct {
	command []string
	in      string
}

func (b *tabsBrowser) run(_ context.Context, args []string) (string, string, error) {
	b.ran = append(b.ran, tabsBrowserStep{command: args, in: b.active})
	switch {
	case slices.Equal(args, []string{"tab", "list"}):
		tabs := make([]string, 0, len(b.tabs))
		for _, id := range b.tabs {
			tabs = append(tabs, fmt.Sprintf(`{"tabId":%q,"targetId":%q,"active":%t,"url":"https://%s.example.invalid/"}`, id, id, id == b.active, id))
		}
		return `{"success":true,"data":{"tabs":[` + strings.Join(tabs, ",") + `]},"error":null}`, "", nil
	case len(args) == 2 && args[0] == "tab":
		if !slices.Contains(b.tabs, args[1]) {
			return `{"success":false,"data":null,"error":"Tab ` + args[1] + ` not found"}`, "", nil
		}
		b.active = args[1]
		return `{"success":true,"data":{"tabId":"` + args[1] + `"},"error":null}`, "", nil
	}
	if effect, ok := b.on[strings.Join(args, " ")]; ok {
		if ok, why := effect(b); !ok {
			return `{"success":false,"data":null,"error":"` + why + `"}`, "", nil
		}
	}
	return `{"success":true,"data":{},"error":null}`, "", nil
}

// opens is a page effect that opens tab and, as agent-browser does for a
// discovered page, leaves the active tab as it was.
func opens(tab string) func(b *tabsBrowser) (bool, string) {
	return func(b *tabsBrowser) (bool, string) {
		b.tabs = append(b.tabs, tab)
		return true, ""
	}
}

// closes is a page effect that closes tab, after which agent-browser makes the
// base tab active.
func closes(tab string) func(b *tabsBrowser) (bool, string) {
	return func(b *tabsBrowser) (bool, string) {
		b.tabs = slices.DeleteFunc(b.tabs, func(t string) bool { return t == tab })
		b.active = b.tabs[0]
		return true, ""
	}
}

// ranIn answers the tab the walk's step cmd ran in, "" when it never ran.
func (b *tabsBrowser) ranIn(cmd ...string) string {
	for _, s := range b.ran {
		if slices.Equal(s.command, cmd) {
			return s.in
		}
	}
	return ""
}

func walkCallSteps(t *testing.T, b *tabsBrowser, steps ...[]string) []BrowserStepResult {
	t.Helper()
	out, _, _, err := runCallSteps(t.Context(), steps, b.run, nil)
	if err != nil {
		t.Fatalf("runCallSteps: %v", err)
	}
	var results []BrowserStepResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("decode the walk's steps: %v", err)
	}
	return results
}

// A popup the page opens — a sign-in provider's — is where the call's next
// step runs, after it was brought to front and held focus, so a page that arms
// its Approve only after it has had focus for a while is armed; the result
// holds only the caller's steps, each as written.
// witness: internal/ops/browser_tabs.go | 	case len(follow) > 0: → 	case false:
func TestRunCallSteps_APopupThePageOpensIsWhereTheNextStepRuns(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
	}}
	results := walkCallSteps(t, b, []string{"click", "#signin"}, []string{"wait", "--text", "Approve"})

	if in := b.ranIn("wait", "--text", "Approve"); in != "t3" {
		t.Errorf("the step after the popup opened ran in %q, want the popup t3", in)
	}
	focused := slices.IndexFunc(b.ran, func(s tabsBrowserStep) bool { return slices.Equal(s.command, browserFocusHold) && s.in == "t3" })
	waited := slices.IndexFunc(b.ran, func(s tabsBrowserStep) bool { return s.command[0] == "wait" && s.command[1] == "--text" })
	if focused < 0 || focused > waited {
		t.Errorf("the popup must hold focus before the next step runs: %v", b.ran)
	}
	if len(results) != 2 || !slices.Equal(results[0].Command, []string{"click", "#signin"}) || !slices.Equal(results[1].Command, []string{"wait", "--text", "Approve"}) {
		t.Errorf("the result must hold the caller's two steps as written: %+v", results)
	}
}

// A popup that closes itself returns the call to the tab it was opened from —
// a popup the first popup opened returns to that one, not to the call's own
// tab — whatever tab agent-browser made active.
// witness: internal/ops/browser_tabs.go | 		if opener := t.returns[t.active]; slices.Contains(listed, opener) { → 		if opener := t.own; slices.Contains(listed, opener) {
func TestRunCallSteps_AClosedPopupReturnsToTheTabItWasOpenedFrom(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		"click #signin":   opens("t3"),
		"click #continue": opens("t4"),
		"click #approve":  closes("t4"),
	}}
	walkCallSteps(t, b, []string{"click", "#signin"}, []string{"click", "#continue"}, []string{"click", "#approve"}, []string{"get", "text", "body"})

	if in := b.ranIn("get", "text", "body"); in != "t3" {
		t.Errorf("after the second popup closed the call is in %q, want t3, the popup it was opened from", in)
	}
}

// A step that runs in a popup closing itself under it — the step after the
// one that finished it — ran in no page: it runs once more in the tab the
// call is back in, and the result holds that run only.
// witness: internal/ops/browser_warm.go | 			if answer.Success || attempt > 0 || tabs == nil || isReportingCommand(step) { → 			if true {
func TestRunCallSteps_AStepAClosingPopupRanInRunsAgainInTheTabBeforeIt(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
	b.on = map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
		"wait --text signed in": func(b *tabsBrowser) (bool, string) {
			if b.active == "t3" {
				closes("t3")(b)
				return false, "CDP error (Runtime.evaluate): Session with given id not found."
			}
			return true, ""
		},
	}
	results := walkCallSteps(t, b, []string{"click", "#signin"}, []string{"wait", "--text", "signed in"})

	if len(results) != 2 || !results[1].Success {
		t.Fatalf("the wait must come back once, answered in the opener: %+v", results)
	}
	var waitedIn []string
	for _, s := range b.ran {
		if slices.Equal(s.command, []string{"wait", "--text", "signed in"}) {
			waitedIn = append(waitedIn, s.in)
		}
	}
	if !slices.Equal(waitedIn, []string{"t3", "t2"}) {
		t.Errorf("the wait ran in %v, want the closing popup t3, then the opener t2", waitedIn)
	}
}

// A tab open before the call — the warm browser's base tab, another call's
// leftover — is never reached: a switch to it does not run and says why.
// witness: internal/ops/browser_tabs.go | 		if !t.isCalls(tab) { → 		if false {
func TestRunCallSteps_ATabOpenBeforeTheCallIsNeverReached(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2", "t3"}, active: "t3"}
	results := walkCallSteps(t, b, []string{"tab", "t1"}, []string{"get", "url"})

	if slices.ContainsFunc(b.ran, func(s tabsBrowserStep) bool { return slices.Equal(s.command, []string{"tab", "t1"}) }) {
		t.Errorf("the switch to the base tab ran: %v", b.ran)
	}
	if in := b.ranIn("get", "url"); in != "t3" {
		t.Errorf("the call left its own tab for %q", in)
	}
	if results[0].Success || results[0].ErrorKind != ErrorKindInvalidCommand {
		t.Errorf("the refused switch must be a failed invalid-command step: %+v", results[0])
	}
}

// The call's tab list names only its own tab and the tabs its pages opened,
// never a tab open before the call.
// witness: internal/ops/browser_tabs.go | 			if t.isCalls(tabs[i].TabID) { → 			if true {
func TestRunCallSteps_TheTabListNamesOnlyTheCallsTabs(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2", "t3"}, active: "t3", on: map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t4"),
	}}
	results := walkCallSteps(t, b, []string{"click", "#signin"}, []string{"tab"})

	var listed struct {
		Tabs []browserTabEntry `json:"tabs"`
	}
	if err := json.Unmarshal(results[1].Result, &listed); err != nil {
		t.Fatalf("decode the tab list: %v (%s)", err, results[1].Result)
	}
	ids := make([]string, 0, len(listed.Tabs))
	for _, tab := range listed.Tabs {
		ids = append(ids, tab.TabID)
	}
	if !slices.Equal(ids, []string{"t3", "t4"}) {
		t.Errorf("the tab list names %v, want the call's own t3 and its popup t4", ids)
	}
}

// A caller's open, close, window, new tab or closed tab is refused as
// agent-browser would read it — past its global flags — so a flag before it
// does not let it run; a tab list or switch goes to the walk.
// witness: internal/ops/browser.go | 	why, ok := refusedCommands[cmd[own[0]]] → 	why, ok := refusedCommands[cmd[0]]
func TestBuildCanonicalBatch_ACommandIsReadPastItsGlobalFlags(t *testing.T) {
	batch, _, refused := buildCanonicalBatch("https://app.example.invalid/", [][]string{
		{"--json", "window", "new"},
		{"--headed", "tab", "new"},
		{"-q", "open", "https://other.example.invalid/"},
		{"tab", "close", "t1"},
		{"tab", "t3"},
		{"tab"},
	}, "", false)

	if len(refused) != 4 {
		t.Errorf("refused %d commands, want the window, the new tab, the open and the close: %+v", len(refused), refused)
	}
	for _, kept := range [][]string{{"tab", "t3"}, {"tab"}} {
		if !slices.ContainsFunc(batch, func(c []string) bool { return slices.Equal(c, kept) }) {
			t.Errorf("%v must reach the walk: %v", kept, batch)
		}
	}
}

// A step that may act — a key, a click, a fill — and failed in a popup that
// closed under it keeps its one answer, which says so: its own effect may be
// what closed the popup (a submit that posts back), and run again in the
// opener it would act on another site's page.
// witness: internal/ops/browser_warm.go | 			if closedUnder = tabs.closedUnder(ctx, run); !closedUnder || !browserReadOnly(step) { → 			if closedUnder = tabs.closedUnder(ctx, run); !closedUnder {
func TestRunCallSteps_AStepThatMayActRunsOnceWhenItsPopupClosesUnderIt(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
	b.on = map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
		"press Enter": func(b *tabsBrowser) (bool, string) {
			if b.active == "t3" {
				closes("t3")(b)
				return false, "CDP error (Input.dispatchKeyEvent): Session with given id not found."
			}
			return true, ""
		},
	}
	results := walkCallSteps(t, b, []string{"click", "#signin"}, []string{"press", "Enter"})

	var pressedIn []string
	for _, s := range b.ran {
		if slices.Equal(s.command, []string{"press", "Enter"}) {
			pressedIn = append(pressedIn, s.in)
		}
	}
	if !slices.Equal(pressedIn, []string{"t3"}) {
		t.Errorf("Enter was pressed in %v, want only the popup t3", pressedIn)
	}
	if results[1].Success || results[1].Error == nil || !strings.Contains(*results[1].Error, "Session with given id not found") {
		t.Errorf("the step must keep the answer it got: %+v", results[1])
	}
}

// Only a command that cannot act counts as read-only, so that only it runs
// again after its popup closed under it: a wait for a time, a selector, text,
// a URL or a load state, get, is and snapshot; never a wait on the page's own
// script, a click, a key, a fill or an eval.
// witness: internal/ops/browser_tabs.go | 			return slices.Contains([]string{"--fn", "-f", "--download", "-d"}, a) → 			return slices.Contains([]string{"--download", "-d"}, a)
func TestBrowserReadOnly_OnlyCommandsThatCannotAct(t *testing.T) {
	for _, tt := range []struct {
		cmd  []string
		want bool
	}{
		{[]string{"wait", "--text", "signed in"}, true},
		{[]string{"wait", "#done"}, true},
		{[]string{"wait", "--url", "**/home"}, true},
		{[]string{"get", "text", "#state"}, true},
		{[]string{"is", "enabled", "#approve"}, true},
		{[]string{"snapshot", "-i", "-c"}, true},
		{[]string{"wait", "--fn", "window.signIn()"}, false},
		{[]string{"wait", "-f", "window.signIn()"}, false},
		{[]string{"wait", "--download", "/tmp/file"}, false},
		{[]string{"click", "#approve"}, false},
		{[]string{"press", "Enter"}, false},
		{[]string{"fill", "#password", "{{env:PW}}"}, false},
		{[]string{"eval", "document.forms[0].submit()"}, false},
		{[]string{"find", "role", "button", "click", "--name", "Approve"}, false},
	} {
		if got := browserReadOnly(tt.cmd); got != tt.want {
			t.Errorf("browserReadOnly(%v) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

// A popup the call's page opens while it loads does not take over the load:
// the first-paint wait and the document's status are the call's own page's,
// and the popup is followed after them, before the first caller step.
// witness: internal/ops/browser_warm.go | 		if tabs != nil && loading { → 		if false {
func TestRunCallSteps_APopupOpenedDuringLoadLeavesThePagesOwnLoad(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		// agent-browser makes a popup it discovers the active tab.
		"open https://app.example.invalid/": func(b *tabsBrowser) (bool, string) {
			b.tabs = append(b.tabs, "t3")
			b.active = "t3"
			return true, ""
		},
	}}
	run := func(ctx context.Context, args []string) (string, string, error) {
		if slices.Equal(args, browserDocumentStatusProbe) {
			b.ran = append(b.ran, tabsBrowserStep{command: args, in: b.active})
			if b.active == "t3" {
				return `{"success":true,"data":{"result":503}}`, "", nil
			}
			return `{"success":true,"data":{"result":200}}`, "", nil
		}
		return b.run(ctx, args)
	}
	steps := [][]string{browserDefaultViewport, {"open", "https://app.example.invalid/"}, browserPaintWait, {"get", "title"}, {"errors"}}
	if _, _, _, err := runCallSteps(t.Context(), steps, run, nil); err != nil {
		t.Fatalf("runCallSteps: %v", err)
	}
	if in := b.ranIn(browserPaintWait...); in != "t2" {
		t.Errorf("the paint wait ran in %q, want the call's own t2", in)
	}
	if in := b.ranIn(browserDocumentStatusProbe...); in != "t2" {
		t.Errorf("the document's status was read in %q, want the call's own t2", in)
	}
	if in := b.ranIn("get", "title"); in != "t3" {
		t.Errorf("the first caller step ran in %q, want the popup t3", in)
	}
}

// A tab list that fails twice after a popup was active runs no caller step
// more — it could run in the base tab agent-browser fell back to — and each
// says it did not run; the canned tail still runs.
// witness: internal/ops/browser_tabs.go | 		t.lost = !ok → 		t.lost = false
func TestRunCallSteps_ATabListLostAfterAPopupRunsNoStepBlind(t *testing.T) {
	failLists := 0
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
	b.on = map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
		"click #approve": func(b *tabsBrowser) (bool, string) {
			closes("t3")(b)
			failLists = 2
			return true, ""
		},
	}
	run := func(ctx context.Context, args []string) (string, string, error) {
		if failLists > 0 && slices.Equal(args, []string{"tab", "list"}) {
			failLists--
			return "", "", context.DeadlineExceeded
		}
		return b.run(ctx, args)
	}
	out, _, _, err := runCallSteps(t.Context(), [][]string{{"click", "#signin"}, {"click", "#approve"}, {"eval", "location.href"}, {"errors"}}, run, nil)
	if err != nil {
		t.Fatalf("runCallSteps: %v", err)
	}
	var results []BrowserStepResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatal(err)
	}
	if in := b.ranIn("eval", "location.href"); in != "" {
		t.Errorf("the eval ran blind in %q", in)
	}
	if len(results) != 4 || results[2].Success || !results[3].Success {
		t.Errorf("the eval must come back not run and the tail must run: %+v", results)
	}
}

// Every name agent-browser takes for open, close, window or a batch of
// commands of its own is refused, not only the first one documented.
// witness: internal/ops/browser.go | 	"goto":           browserRefusedOpen, → 	"go-to":          browserRefusedOpen,
func TestBuildCanonicalBatch_EveryNameOfARefusedCommandIsRefused(t *testing.T) {
	_, _, refused := buildCanonicalBatch("https://app.example.invalid/", [][]string{
		{"goto", "https://other.example.invalid/"},
		{"navigate", "https://other.example.invalid/"},
		{"quit"},
		{"exit"},
		{"batch", "open https://other.example.invalid/", "tab new"},
	}, "", false)
	if len(refused) != 5 {
		t.Errorf("refused %d of 5 commands: %+v", len(refused), refused)
	}
}

// A popup whose recorded opener closed returns the call to its own tab, not
// to a tab further up that the walk only guesses opened it.
// witness: internal/ops/browser_tabs.go | 		from = t.own → 		from = t.opened[0]
func TestRunCallSteps_APopupWhoseOpenerClosedReturnsToTheCallsOwnTab(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		"click #a": opens("t3"),
		"click #b": opens("t4"),
		"click #c": opens("t5"),
		"click #d": func(b *tabsBrowser) (bool, string) {
			b.tabs = slices.DeleteFunc(b.tabs, func(t string) bool { return t == "t4" })
			return true, ""
		},
		"click #e": closes("t5"),
	}}
	walkCallSteps(t, b, []string{"click", "#a"}, []string{"click", "#b"}, []string{"click", "#c"}, []string{"click", "#d"}, []string{"click", "#e"}, []string{"get", "url"})

	if in := b.ranIn("get", "url"); in != "t2" {
		t.Errorf("after t5 closed, its opener t4 gone, the call is in %q, want its own t2", in)
	}
}

// A focus hold that fails because the popup's page moved on by itself (its
// script redirected it, destroying the hold's context) is held once more on
// the next page, after its first paint.
// witness: internal/ops/browser_tabs.go | 	for attempt := range 2 { → 	for attempt := range 1 {
func TestRunCallSteps_AFocusHoldCutByARedirectIsHeldOnTheNextPage(t *testing.T) {
	holds := 0
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
		strings.Join(browserFocusHold, " "): func(*tabsBrowser) (bool, string) {
			holds++
			if holds == 1 {
				return false, "Execution context was destroyed."
			}
			return true, ""
		},
	}}
	walkCallSteps(t, b, []string{"click", "#signin"}, []string{"click", "#approve"})

	var held []string
	for _, s := range b.ran {
		if slices.Equal(s.command, browserFocusHold) {
			held = append(held, s.in)
		}
		if slices.Equal(s.command, []string{"click", "#approve"}) {
			break
		}
	}
	if !slices.Equal(held, []string{"t3", "t3"}) {
		t.Errorf("focus was held in %v before the next step, want twice in the popup t3", held)
	}
}

// With too little of the call left to wait for a popup's first paint and hold
// focus on it, the walk only makes it active and says so on the step after
// which it did, so following it never runs the call past its deadline.
// witness: internal/ops/browser_tabs.go | 	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < browserTabSettleLeast { → 	if false {
func TestRunCallSteps_AFollowWithLittleTimeLeftOnlySwitchesAndSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
			"click #signin": opens("t3"),
		}}
		out, _, _, err := runCallSteps(ctx, [][]string{{"click", "#signin"}, {"get", "url"}}, b.run, nil)
		if err != nil {
			t.Fatalf("runCallSteps: %v", err)
		}
		var results []BrowserStepResult
		if err := json.Unmarshal([]byte(out), &results); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(b.ran, func(s tabsBrowserStep) bool {
			return slices.Equal(s.command, browserFocusHold) || slices.Equal(s.command, browserPaintWait)
		}) {
			t.Errorf("the walk waited on the popup with 5 s of the call left: %v", b.ran)
		}
		if b.ranIn("get", "url") != "t3" || results[0].Note == "" {
			t.Errorf("the popup must still be followed, and the click's step must say it was not waited on: %+v", results)
		}
	})
}

// A step that failed with "Target closed" because its popup closed under it
// is no CDP wedge: the tab list after it answered, so the walk goes on and the
// browser is kept for the next call, its sign-in saved.
// witness: internal/ops/browser_warm.go | 		if _, wedged := scanStepsForCDPWedge(results[len(results)-1:]); wedged && !closedUnder { → 		if _, wedged := scanStepsForCDPWedge(results[len(results)-1:]); wedged {
func TestRunCallSteps_APopupClosingUnderAStepIsNoWedge(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2"}
	b.on = map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
		"click #approve": func(b *tabsBrowser) (bool, string) {
			closes("t3")(b)
			return false, "Target closed"
		},
	}
	results := walkCallSteps(t, b, []string{"click", "#signin"}, []string{"click", "#approve"}, []string{"get", "text", "#state"})

	if len(results) != 3 || b.ranIn("get", "text", "#state") != "t2" {
		t.Errorf("the walk must go on in the opener after its popup closed under a step: %+v", results)
	}
}
