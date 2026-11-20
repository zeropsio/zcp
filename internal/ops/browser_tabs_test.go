package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
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
			tabs = append(tabs, fmt.Sprintf(`{"tabId":%q,"active":%t,"url":"https://%s.example.invalid/"}`, id, id == b.active, id))
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
	out, _, _, err := runCallSteps(t.Context(), steps, b.run)
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
// witness: internal/ops/browser_tabs.go | 	case len(fresh) > 0: → 	case false:
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
// witness: internal/ops/browser_tabs.go | 		from = t.returnTo(listed) → 		from = t.own
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
// witness: internal/ops/browser_warm.go | 			if answer.Success || attempt > 0 || tabs == nil || isReportingCommand(step) || !tabs.closedUnder(ctx, run) { → 			if true {
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

// A walk that ends with a popup still open ends on the call's own tab, so the
// hand-back saves the sign-in from it and never reads the popup's site.
// witness: internal/ops/browser_tabs.go | 	if t.active != t.own { → 	if false {
func TestRunCallSteps_ACleanWalkEndsOnTheCallsOwnTab(t *testing.T) {
	b := &tabsBrowser{tabs: []string{"t1", "t2"}, active: "t2", on: map[string]func(*tabsBrowser) (bool, string){
		"click #signin": opens("t3"),
	}}
	walkCallSteps(t, b, []string{"click", "#signin"}, []string{"get", "url"})

	if b.active != "t2" {
		t.Errorf("the walk ended on %q, want the call's own t2", b.active)
	}
}

// A caller's open, close, window, new tab or closed tab is refused as
// agent-browser would read it — past its global flags — so a flag before it
// does not let it run; a tab list or switch goes to the walk.
// witness: internal/ops/browser.go | 	why, ok := refusedCommands[cmd[own[0]]] → 	why, ok := refusedCommands[cmd[0]]
func TestBuildCanonicalBatch_ACommandIsReadPastItsGlobalFlags(t *testing.T) {
	batch, refused := buildCanonicalBatch("https://app.example.invalid/", [][]string{
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
