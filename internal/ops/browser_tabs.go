package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// A call's tabs (docs/spec-mate.md §2.6b, "A call's popups"): the call's own
// tab, which the setup opened for it, and every tab its pages open during the
// call — a popup, a target=_blank link — are the call's. The walk follows them
// the way a person's eyes do: a tab the page opens becomes the active one,
// focused, after the step that opened it, and when the active popup closes the
// tab that was active before it is active again. The caller may also list the
// call's tabs and switch between them. A tab open before the call — the warm
// browser's base tab, or anything else — is never reached.

// browserTabForm is what a caller's tab command asks for.
type browserTabForm int

const (
	// browserTabRefused is a tab command the tool does not run.
	browserTabRefused browserTabForm = iota
	// browserTabList is ["tab"] or ["tab","list"]: the call's tabs.
	browserTabList
	// browserTabSwitch is ["tab","t<N>"]: make one of the call's tabs active.
	browserTabSwitch
)

// browserTabID is a tab's id as agent-browser's tab list names it.
var browserTabID = regexp.MustCompile(`^t[1-9][0-9]*$`)

// readBrowserTabCommand reads a caller's command as agent-browser does (past
// its global flags, browserCommandArgs) and, for a tab command, answers what
// it asks for and, for a switch, the tab it names. isTab is false for every
// other command. Only the list and the switch by a tab's id run: a new tab is
// the call's own or one its page opens, the tool closes them as the call ends,
// and a label or target id names no tab the list shows.
func readBrowserTabCommand(cmd []string) (form browserTabForm, tab string, isTab bool) {
	own, _ := browserCommandArgs(cmd)
	if len(own) == 0 || cmd[own[0]] != browserCmdTab {
		return browserTabRefused, "", false
	}
	args := make([]string, 0, len(own)-1)
	for _, at := range own[1:] {
		args = append(args, cmd[at])
	}
	switch {
	case len(args) == 0, len(args) == 1 && args[0] == browserSubcmdTabList:
		return browserTabList, "", true
	case len(args) == 1 && browserTabID.MatchString(args[0]):
		return browserTabSwitch, args[0], true
	}
	return browserTabRefused, "", true
}

// browserTabRefusal is the error of a caller's tab command that does not run.
func browserTabRefusal(cmd []string) string {
	own, _ := browserCommandArgs(cmd)
	sub := ""
	if len(own) > 1 {
		sub = cmd[own[1]]
	}
	switch sub {
	case "new":
		return "not run: the call has its own tab, and a tab its page opens is followed; for a clean context pass isolation \"fresh-context\""
	case browserCmdClose:
		return "not run: the tool closes the call's tabs as the call ends; a popup that closes itself returns the call to the tab before it"
	}
	return `not run: only ["tab"], which lists the call's tabs, and ["tab","t<N>"] with an id from that list run`
}

const (
	browserCmdGet        = "get"
	browserCmdSnapshot   = "snapshot"
	browserSubcmdTabList = "list"
	// browserTabStepTimeout bounds each step the walk takes to follow a tab:
	// the switch, the paint wait (5 s of its own) and the focus hold (3 s).
	// Each is bounded by the call's own deadline as well.
	browserTabStepTimeout = 8 * time.Second
	// browserTabSettleLeast is the least time the call must have left for the
	// walk to wait for a followed tab's first paint and hold focus on it (5 s
	// and 3 s at most); with less, it switches only, so the call does not run
	// past its deadline, lose its browser and skip the sign-in's save.
	browserTabSettleLeast = 10 * time.Second
)

// browserFocusHold focuses the active tab's window and answers once the page
// has held focus for 900 ms, or after 3 s with whether it has it: a page that
// arms a control only after it has had focus for a while (a sign-in
// provider's Approve) is armed by the time the next step runs.
var browserFocusHold = []string{browserCmdEval, "new Promise(r=>{window.focus();let held=0;" +
	"const t=setInterval(()=>{held=document.hasFocus()?held+50:0;if(held>=900){clearInterval(t);r(true)}},50);" +
	"setTimeout(()=>{clearInterval(t);r(document.hasFocus())},3000)})"}

// browserTabEntry is one tab of agent-browser's tab list.
type browserTabEntry struct {
	TabID    string `json:"tabId"`
	TargetID string `json:"targetId"`
	Active   bool   `json:"active"`
	URL      string `json:"url"`
}

// browserCallTabs is what a walk knows of the call's tabs.
type browserCallTabs struct {
	// own is the call's own tab, active when the walk began.
	own       string
	ownTarget string
	// before are the tabs open when the walk began, own among them.
	before []string
	// opened are the tabs opened during the call and still open, in the
	// order seen; pending are those seen while the call's own page loaded,
	// followed once its load is checked.
	opened, pending []string
	// active is the tab the walk keeps active; returns is the tab that was
	// active when each opened tab was seen — the one it returns to when it
	// closes, while that one is open.
	active  string
	returns map[string]string
	// lost says a tab list failed twice while a tab the call opened was
	// active: where the next step would run is unknown, so no caller step
	// runs any more.
	lost bool
	// note is what the walk says of its last follow, for the step after
	// which it ran: a follow cut short for the call's deadline.
	note   string
	stderr *capBuffer
}

// startBrowserCallTabs reads the tabs as a call's walk begins; the active one
// is the call's own. nil when the list names no active tab: the walk then
// runs without following tabs.
func startBrowserCallTabs(ctx context.Context, run browserStepExec, stderr *capBuffer) *browserCallTabs {
	t := &browserCallTabs{returns: map[string]string{}, stderr: stderr}
	_, tabs, ok := t.list(ctx, run)
	if !ok {
		return nil
	}
	for _, tab := range tabs {
		t.before = append(t.before, tab.TabID)
		if tab.Active {
			t.own = tab.TabID
			t.ownTarget = tab.TargetID
		}
	}
	if t.own == "" {
		return nil
	}
	t.active = t.own
	return t
}

// list runs agent-browser's tab list: each tab's entry as it came, and parsed.
func (t *browserCallTabs) list(ctx context.Context, run browserStepExec) ([]json.RawMessage, []browserTabEntry, bool) {
	out, ok := t.housekeep(ctx, run, []string{browserCmdTab, browserSubcmdTabList})
	if !ok {
		return nil, nil, false
	}
	var answer struct {
		Data struct {
			Tabs []json.RawMessage `json:"tabs"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(out), &answer) != nil {
		return nil, nil, false
	}
	tabs := make([]browserTabEntry, 0, len(answer.Data.Tabs))
	for _, raw := range answer.Data.Tabs {
		var tab browserTabEntry
		if json.Unmarshal(raw, &tab) != nil || tab.TabID == "" {
			return nil, nil, false
		}
		tabs = append(tabs, tab)
	}
	return answer.Data.Tabs, tabs, true
}

// housekeep runs one step the walk takes for itself, bounded by
// browserTabStepTimeout and the call's deadline; it is no step of the result.
// ok says it answered success.
func (t *browserCallTabs) housekeep(ctx context.Context, run browserStepExec, step []string) (string, bool) {
	sctx, cancel := context.WithTimeout(ctx, browserTabStepTimeout)
	defer cancel()
	out, serr, err := run(sctx, step)
	_, _ = t.stderr.Write([]byte(serr))
	var answer struct {
		Success bool `json:"success"`
	}
	if err != nil && strings.TrimSpace(out) == "" {
		return "", false
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &answer) != nil || !answer.Success {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// isCalls reports whether tab is one of the call's tabs: its own or one
// opened during it.
func (t *browserCallTabs) isCalls(tab string) bool {
	return tab == t.own || slices.Contains(t.opened, tab)
}

// listAfterStep reads the tabs after a step. A list that fails while a tab
// the call opened is active is asked once more — agent-browser may have
// fallen back to the base tab when that tab closed — and one that fails
// again marks the walk lost (browserCallTabs.lost).
func (t *browserCallTabs) listAfterStep(ctx context.Context, run browserStepExec) ([]browserTabEntry, bool) {
	_, tabs, ok := t.list(ctx, run)
	if !ok && t.active != t.own {
		_, tabs, ok = t.list(ctx, run)
		t.lost = !ok
	}
	return tabs, ok
}

// sync reads the tabs after a step and keeps the walk on what a person would
// look at: a tab opened since becomes active, focused, after its first paint
// (the last one when several opened); when the active tab closed, the tab
// that was active when it was seen is active again while it is open, and the
// call's own tab otherwise; else the walk's active tab stays active, whatever
// agent-browser made active. closed says the active tab was gone.
func (t *browserCallTabs) sync(ctx context.Context, run browserStepExec) (closed bool) {
	return t.read(ctx, run, false)
}

// hold reads the tabs while the call's own page loads: a tab its page opened
// is recorded and followed only once the load is checked (sync), and the
// call's own tab is kept active, so the first-paint wait and the document's
// status are the page's own.
func (t *browserCallTabs) hold(ctx context.Context, run browserStepExec) {
	t.read(ctx, run, true)
}

func (t *browserCallTabs) read(ctx context.Context, run browserStepExec, loading bool) (closed bool) {
	t.note = ""
	tabs, ok := t.listAfterStep(ctx, run)
	if !ok {
		return false
	}
	listed := make([]string, 0, len(tabs))
	nowActive := ""
	var fresh []string
	for _, tab := range tabs {
		listed = append(listed, tab.TabID)
		if tab.Active {
			nowActive = tab.TabID
		}
		if !slices.Contains(t.before, tab.TabID) && !slices.Contains(t.opened, tab.TabID) {
			fresh = append(fresh, tab.TabID)
		}
	}
	closed = !slices.Contains(listed, t.active)
	from := t.active
	if closed {
		from = t.own
		if opener := t.returns[t.active]; slices.Contains(listed, opener) {
			from = opener
		}
	}
	for _, tab := range fresh {
		t.opened = append(t.opened, tab)
		t.returns[tab] = from
	}
	t.opened = slices.DeleteFunc(t.opened, func(tab string) bool { return !slices.Contains(listed, tab) })
	if loading {
		t.pending = append(t.pending, fresh...)
		if nowActive != t.own {
			t.switchTo(ctx, run, t.own, false, false)
		}
		return closed
	}
	follow := append(slices.Clone(t.pending), fresh...)
	t.pending = nil
	follow = slices.DeleteFunc(follow, func(tab string) bool { return !slices.Contains(listed, tab) })
	switch {
	case len(follow) > 0:
		t.switchTo(ctx, run, follow[len(follow)-1], true, true)
	case closed:
		t.switchTo(ctx, run, from, false, true)
	case nowActive != t.active:
		t.switchTo(ctx, run, t.active, false, true)
	}
	return closed
}

// switchTo makes tab active, which brings it to front; settle then — for a
// tab just opened — waits for its first paint and holds focus on it
// (browserFocusHold). A hold that fails on a page that moved on by itself
// (its script redirected it) is tried once more after that page's first
// paint. With under browserTabSettleLeast of the call left, the walk only
// switches and says so in note. A switch that fails leaves the active tab as
// it was.
func (t *browserCallTabs) switchTo(ctx context.Context, run browserStepExec, tab string, fresh, settle bool) {
	if _, ok := t.housekeep(ctx, run, []string{browserCmdTab, tab}); !ok {
		return
	}
	t.active = tab
	if !settle {
		return
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < browserTabSettleLeast {
		t.note = fmt.Sprintf("the call's tab %s became active without waiting for its first paint or holding focus on it: under %s of the call's time was left", tab, browserTabSettleLeast)
		return
	}
	if fresh {
		t.housekeep(ctx, run, browserPaintWait)
	}
	for attempt := range 2 {
		if _, held := t.housekeep(ctx, run, browserFocusHold); held || attempt == 1 {
			break
		}
		t.housekeep(ctx, run, browserPaintWait)
	}
}

// closedUnder reports, for a step that failed, whether it ran in a tab the
// call opened that closed under it — a popup that closes itself once it is
// done; the call is then back in the tab before it (sync).
func (t *browserCallTabs) closedUnder(ctx context.Context, run browserStepExec) bool {
	return t.active != t.own && t.sync(ctx, run)
}

// command answers a caller's tab command: the list holds the call's tabs
// only; a switch reaches only one of them, and the tab it makes active is
// focused as one the walk follows.
func (t *browserCallTabs) command(ctx context.Context, step []string, run browserStepExec) BrowserStepResult {
	form, tab, _ := readBrowserTabCommand(step)
	refuse := func(why string) BrowserStepResult {
		return BrowserStepResult{Command: step, Error: &why, ErrorKind: ErrorKindInvalidCommand}
	}
	switch form {
	case browserTabList:
		t.sync(ctx, run)
		raw, tabs, ok := t.list(ctx, run)
		if !ok {
			msg := "the tab list did not answer"
			return BrowserStepResult{Command: step, Error: &msg}
		}
		calls := make([]json.RawMessage, 0, len(raw))
		for i := range tabs {
			if t.isCalls(tabs[i].TabID) {
				calls = append(calls, raw[i])
			}
		}
		data, err := json.Marshal(map[string]any{"tabs": calls})
		if err != nil {
			msg := fmt.Sprintf("the tab list: %v", err)
			return BrowserStepResult{Command: step, Error: &msg}
		}
		return BrowserStepResult{Command: step, Success: true, Result: withoutLifecycle(data)}
	case browserTabSwitch:
		t.sync(ctx, run)
		if !t.isCalls(tab) {
			return refuse(fmt.Sprintf(`not run: %s is not one of this call's tabs; ["tab"] lists them — the call's own tab and the tabs its pages opened`, tab))
		}
		sctx, cancel := context.WithTimeout(ctx, browserTabStepTimeout)
		out, serr, _ := run(sctx, []string{browserCmdTab, tab})
		cancel()
		_, _ = t.stderr.Write([]byte(serr))
		var answer struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
			Error   *string         `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &answer) != nil {
			msg := "the switch did not answer"
			return BrowserStepResult{Command: step, Error: &msg}
		}
		if answer.Success {
			t.active = tab
			t.housekeep(ctx, run, browserFocusHold)
		}
		return BrowserStepResult{Command: step, Success: answer.Success, Result: withoutLifecycle(answer.Data), Error: answer.Error}
	case browserTabRefused:
	}
	return refuse(browserTabRefusal(step))
}

// browserTabsLost is the error of each caller step a lost walk did not run
// (browserCallTabs.lost).
const browserTabsLost = "not run: the tab list did not answer after a tab the call opened was active, so where this step would run is unknown"

// browserReadOnly reports whether a command only reads the page, so that one
// that failed in a popup closing under it may run again in the tab the call
// is back in: agent-browser 0.39.1's (cli/src/commands.rs) wait for a time, a
// selector, text, a URL or a load state — not --fn, which runs the page's
// script, nor --download, which writes a file; get text, html, value, attr,
// url, title, count, box or styles; is visible, enabled or checked; and
// snapshot. Every other command may act — a click, a key, a fill, a script
// — and runs once.
func browserReadOnly(cmd []string) bool {
	own, _ := browserCommandArgs(cmd)
	if len(own) == 0 {
		return false
	}
	args := make([]string, 0, len(own))
	for _, at := range own {
		args = append(args, cmd[at])
	}
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	switch args[0] {
	case browserCmdWait:
		return !slices.ContainsFunc(args, func(a string) bool {
			return slices.Contains([]string{"--fn", "-f", "--download", "-d"}, a)
		})
	case browserCmdGet:
		return slices.Contains([]string{"text", "html", "value", "attr", "url", "title", "count", "box", "styles"}, sub)
	case "is":
		return slices.Contains([]string{"visible", "enabled", "checked"}, sub)
	case browserCmdSnapshot:
		return true
	}
	return false
}
