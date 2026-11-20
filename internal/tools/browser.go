package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// BrowserInput is the tools-layer input for zerops_browser. It mirrors
// ops.BrowserBatchInput; browserInputSchema publishes isolation's levels as
// an enum.
type BrowserInput struct {
	URL            string     `json:"url"                      jsonschema:"The page URL to open. Required."`
	Commands       [][]string `json:"commands,omitempty"       jsonschema:"Inner agent-browser commands run between the auto-prepended [open url] + first-paint wait and the auto-appended [screenshot?]/[errors]/[console]/[network requests]. A leading [\"set\",\"viewport\",\"<w>\",\"<h>\"] runs before the open, so the page loads at that size; a call without one loads at 1280×577, the size a fresh Chrome opens at, whatever an earlier call set. Each element is one command as a string array. A popup or new tab the page opens becomes the active tab, focused, after the step that opened it; when it closes, the tab before it is active again. [\"tab\"] lists the call's tabs, [\"tab\",\"t2\"] switches to one; tabs open before the call are out of reach."`
	TimeoutSeconds int        `json:"timeoutSeconds,omitempty" jsonschema:"Bounds the whole call, including the wait for the browser and a slow page's load. Default 120, max 300."`
	Isolation      string     `json:"isolation,omitempty"      jsonschema:"What the page remembers from earlier calls. tab (default): cookies, login, storage, HTTP cache and service workers carry over; cookies, login and storage also across browser restarts. fresh-context: none of them, for the first check after a deploy or a signed-out view; it starts and stays signed out, stops Chrome as the call ends, and leaves the kept sign-in alone. new-browser: a fresh Chrome that keeps the sign-in. To forget a sign-in, pass the signed-in page as url in a tab call and run [\"cookies\",\"clear\"] and [\"storage\",\"local\",\"clear\"] before the call leaves it; the next browser starts signed out there."`
	Screenshot     bool       `json:"screenshot,omitempty"     jsonschema:"Capture a screenshot after your commands run, before errors/console/network requests. Returned as an image content block alongside the text result. A screenshot is not redacted: a password field shows dots, so never screenshot a {{env:KEY}} value typed into a visible field."`
	FullPage       bool       `json:"fullPage,omitempty"       jsonschema:"Capture the whole page instead of the viewport, as one image cut to its top 8000 px (screenshot.capturedHeight then says how tall the capture was). The capture does not scroll: content that loads when scrolled into view stays blank unless you scroll to it or wait for it first. Implies screenshot."`
}

// browserInputSchema derives the published schema from BrowserInput, lists
// isolation's three levels and says, where commands are written, what a
// `{{env:KEY}}` in one becomes (docs/spec-mate.md §5.8a) — by where zcp
// runs: only a Mate has a vault of its own for the value to come from.
func browserInputSchema(inMate bool) *jsonschema.Schema {
	s, err := jsonschema.For[BrowserInput](nil)
	if err != nil || s == nil {
		return nil
	}
	if prop, ok := s.Properties["isolation"]; ok {
		prop.Enum = []any{ops.BrowserIsolationTab, ops.BrowserIsolationFreshContext, ops.BrowserIsolationNewBrowser}
	}
	if prop, ok := s.Properties["commands"]; ok {
		source := "a variable of this service"
		if inMate {
			source = "a test sign-in from your own vault (zerops_env mate=true)"
		}
		prop.Description += " In any argument, whole or inside it, {{env:KEY}} is replaced with the value this container's env holds under KEY — " + source +
			" — and comes back as the placeholder. Not hidden: a value under six characters, one the page changed beyond trimming and escaping (base64, a hash)," +
			" or one the page kept (storage, a cookie) that a later call reads without naming its KEY, even after a browser restart." +
			" A KEY the env does not hold, or a value over 4096 bytes, fails the call before the browser opens." +
			" In JavaScript (an eval's source, a wait's --fn) write it inside a quoted string, '{{env:KEY}}', or the call is refused: the value is escaped to be exactly that string."
	}
	return s
}

// RegisterBrowser registers the zerops_browser tool. Only called by server.go
// when running inside the ZCP container (where agent-browser is installed).
// stateDir is where a screenshot is kept as one of the Mate's pictures; ""
// keeps none. inMate says zcp serves a Mate here, whose own vault a
// `{{env:KEY}}` value comes from: the words about it follow.
func RegisterBrowser(srv *mcp.Server, stateDir string, inMate bool) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_browser",
		Description: "Drive Chrome in ONE bounded batch, lifecycle included: [open url] + your commands + [screenshot] + errors, console and network requests. " +
			"The call already waits for the page's first paint; after a click, scroll, reload or navigation, wait for what should appear — " +
			"[\"wait\",\"<sel>\"] or [\"wait\",\"--text\",\"<text>\"] — never for a fixed time. " +
			"Only wait polls (25 s, or [\"wait\",\"<sel>\",\"--timeout\",\"<ms>\"] up to 120000; on a dev server, text that has not come by then: read zerops_dev_server action=logs hostname=\"<host>\", then wait again); click, fill, check, find, get and is look once " +
			"and fail at once on an element that is not there yet. " +
			"One warm Chrome serves every call, one at a time (waiting within timeoutSeconds); a call's tabs close when it ends. A call that times out, is cancelled " +
			"or wedges Chrome returns its finished steps and recycles the browser. " +
			"isolation: \"tab\" (default) keeps cookies, storage, HTTP cache and service workers from earlier calls, and the sign-in across browser restarts. " +
			"\"fresh-context\" starts with none of them — only for the first check after a deploy that changed sign-in, storage, " +
			"a service worker or long-cached assets, or for a signed-out view; it stops Chrome as the call ends, so the next call starts a cold one. " +
			"Every other call stays on \"tab\". \"new-browser\" restarts Chrome first, keeping the sign-in, for a page that misbehaves only in the warm browser. " +
			"freshBrowser: true means Chrome was started for the call (always after new-browser); " +
			"browserRecycled: true means the call ended unclean and the browser was stopped (not set by a fresh-context call's clean stop). " +
			"Each step's errorKind: selector-not-found, not-editable, timeout, wedge, invalid-command " +
			"(fix the command, not the page), stale-ref, " +
			"script-timeout (your eval's promise never settled, e.g. awaiting a looping animation's finished; the page is alive), " +
			"slow-page, server-error (the message says what to do) or other. " +
			"commands[] vocabulary (<sel> is a CSS selector): " +
			"[\"find\",\"role\",\"button\",\"click\",\"--name\",\"Submit\"], [\"find\",\"label\",\"<text>\",\"check\"], " +
			"[\"find\",\"label\",\"<text>\",\"fill\",\"<value>\"], [\"find\",\"text\",\"<text>\",\"click\"], " +
			"[\"click\",\"<sel>\"], [\"fill\",\"<sel>\",\"<value>\"], [\"check\",\"<sel>\"], " +
			"[\"get\",\"text\",\"<sel>\"], [\"get\",\"value\",\"<sel>\"], [\"get\",\"count\",\"<sel>\"], " +
			"[\"is\",\"visible\",\"<sel>\"], [\"is\",\"enabled\",\"<sel>\"], [\"is\",\"checked\",\"<sel>\"], " +
			"[\"wait\",\"<sel>\"], [\"wait\",\"--text\",\"<text>\"], [\"snapshot\",\"-i\",\"-c\"], " +
			"[\"set\",\"viewport\",\"<w>\",\"<h>\"], [\"set\",\"device\",\"<name>\"], [\"set\",\"media\",\"dark|light\"], " +
			"[\"eval\",\"<js>\"] (value as JSON text; only for what get/is/find cannot read, e.g. localStorage). " +
			"find's action follows its value and is only click, fill, check, hover or text; a role's name goes in --name after the action, " +
			"never as a positional; find cannot get or is — read state with get/is on a CSS selector (a checkbox: [\"is\",\"checked\",\"<sel>\"]). " +
			"--name matches a substring (\"Sign in\" hits \"Sign in with passkey\"); --exact matches the whole name. " +
			"An @eN ref exists only after a snapshot earlier in the SAME call and dies with the call's tab: never reuse one from an earlier call " +
			"or guess one; prefer find role|label|text or CSS. " +
			"open, close, window and tab new/close never run (invalid-command steps); popups: see commands. " +
			"{{env:KEY}} in an argument: see commands. " +
			"screenshot=true captures the viewport after your commands run, fullPage=true for the whole page as one image — an image block beside the result, " +
			"kept as the picture screenshot.picture names: ![what it shows](shot-3) in a describe-change description shows it to the person. " +
			"Returns: steps[], errorsOutput, consoleOutput (warnings and errors; [\"console\"] in commands gives every level), " +
			"networkOutput (4xx/5xx; a request with no status — failed or in flight — is not listed), " +
			"durationMs, freshBrowser, browserRecycled, failedSteps, message, keptSignIn; outputs past 20k chars are cut; fullResult names a file with all of it. " +
			"To say how a call ran, report the result's durationMs, freshBrowser and browserRecycled; a script that summarises results keeps them.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Drive browser via agent-browser",
			IdempotentHint:  false,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		},
		InputSchema: browserInputSchema(inMate),
	}, func(ctx context.Context, req *mcp.CallToolRequest, input BrowserInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.BrowserBatch(ctx, ops.BrowserBatchInput{
			URL:            input.URL,
			Commands:       input.Commands,
			TimeoutSeconds: input.TimeoutSeconds,
			Isolation:      input.Isolation,
			Screenshot:     input.Screenshot,
			FullPage:       input.FullPage,
			EnvStorePath:   mate.LiveEnvStorePath,
			InMate:         inMate,
			Call:           requestBrowserCall(req),
		})
		if err != nil {
			return convertError(err), nil, nil
		}
		keepBrowserPicture(stateDir, result)
		return browserToolResult(result), nil, nil
	})
}

// browserCallIDKeys are the `_meta` keys under which a client sends its own
// id of the tool call, by client: Claude Code's tool_use id ("toolu_…",
// recorded from Claude Code 2.1.326). Codex, Cursor, OpenCode, Grok and
// Antigravity are not known to send one.
var browserCallIDKeys = []string{"claudecode/toolUseId"}

// browserCallIDMax bounds an id zcp publishes; a longer one is no tool-call
// id it publishes, never one it cuts.
const browserCallIDMax = 256

// requestBrowserCall is the call a zerops_browser request runs for: the
// client's own tool-call id and its name from initialize (browserCall).
func requestBrowserCall(req *mcp.CallToolRequest) ops.BrowserCall {
	if req == nil || req.Params == nil {
		return ops.BrowserCall{}
	}
	var init *mcp.InitializeParams
	if req.Session != nil {
		init = req.Session.InitializeParams()
	}
	call := browserCall(req.Params.Meta, init)
	if call.ID == "" {
		keys := slices.Sorted(maps.Keys(req.Params.Meta))
		fmt.Fprintf(os.Stderr, "zerops_browser: client %q sent no tool-call id; its _meta keys: %v\n", call.Client, keys)
	}
	return call
}

// browserCall reads the client's own tool-call id from a tools/call
// request's _meta, verbatim, and the client's name: an id under a key
// browserCallIDKeys does not name, or one that is no string, empty or past
// browserCallIDMax, is none. zcp never makes one up.
func browserCall(meta mcp.Meta, init *mcp.InitializeParams) ops.BrowserCall {
	var call ops.BrowserCall
	if init != nil && init.ClientInfo != nil {
		call.Client = init.ClientInfo.Name
	}
	for _, key := range browserCallIDKeys {
		if id, ok := meta[key].(string); ok && id != "" && len(id) <= browserCallIDMax {
			call.ID = id
			break
		}
	}
	return call
}

// keepBrowserPicture keeps a captured screenshot as one of the Mate's pictures
// and names it in the result, so a change's description can show it
// (workflow.KeepPicture). Best-effort: a picture that could not be kept is
// still returned as the image block, only unnamed.
func keepBrowserPicture(stateDir string, result *ops.BrowserBatchResult) {
	if stateDir == "" || result == nil || result.Screenshot == nil || len(result.Screenshot.PNG) == 0 {
		return
	}
	shot := result.Screenshot
	pic, err := workflow.KeepPicture(stateDir, shot.PNG, shot.Width, shot.Height)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zerops_browser: the screenshot could not be kept as a picture: %v\n", err)
		return
	}
	shot.Picture = pic.ID
	shot.KeptFor = pictureKeepWords()
}

// browserToolResult builds the zerops_browser CallToolResult: the JSON
// text (as before) plus, when a screenshot was captured, one appended
// image content block. Kept as its own function so it's unit-testable
// without a full MCP session. structuredContent (the handler's 2nd
// return value) stays nil regardless — spec-mate.md §1.6: the SDK
// marshals a non-nil typed output into structuredContent and Claude Code
// replaces the model-facing text with it, stripping every guidance atom.
func browserToolResult(result *ops.BrowserBatchResult) *mcp.CallToolResult {
	tr := jsonResult(shortBrowserResult(result))
	if result.Screenshot != nil && len(result.Screenshot.PNG) > 0 {
		tr.Content = append(tr.Content, &mcp.ImageContent{
			Data:     result.Screenshot.PNG,
			MIMEType: "image/png",
		})
	}
	return tr
}

// browserTextCap bounds the result text the model reads: one page snapshot or
// network tail can run to hundreds of thousands of chars.
const browserTextCap = 20000

// shortBrowserResult is the result as the model reads it. Within
// browserTextCap it is the whole result; past it, the whole result goes to a
// temp file the reply names (fullResult) when it can be written and each step
// result, errors and console output keeps its head as a string, the network
// tail its first requests — the longest outputs cut first, as little as fits.
func shortBrowserResult(result *ops.BrowserBatchResult) any {
	whole, err := json.Marshal(result)
	if err != nil || len(whole) <= browserTextCap {
		return result
	}
	short := *result
	short.FullResult = saveBrowserResult(whole)
	note := "… (cut)"
	if short.FullResult != "" {
		note = "… (cut; the whole result is in fullResult)"
	}
	for limit := browserTextCap; ; limit /= 2 {
		short.Steps = make([]ops.BrowserStepResult, len(result.Steps))
		for i, step := range result.Steps {
			step.Result = cutBrowserOutput(step.Result, limit, note)
			short.Steps[i] = step
		}
		short.ErrorsOutput = cutBrowserOutput(result.ErrorsOutput, limit, note)
		short.ConsoleOutput = cutBrowserOutput(result.ConsoleOutput, limit, note)
		short.NetworkOutput = result.NetworkOutput[:min(len(result.NetworkOutput), limit/100)]
		if b, err := json.Marshal(short); limit == 0 || (err == nil && len(b) <= browserTextCap) {
			return short
		}
	}
}

// saveBrowserResult writes a cut result whole to this process's one file in
// the temp dir, replacing the previous cut result so they never pile up. It
// answers the file, "" when it could not be written. The rename replaces the
// name itself, never following what an existing name points at.
func saveBrowserResult(whole []byte) string {
	f, err := os.CreateTemp("", "zerops-browser-*.tmp")
	if err != nil {
		return ""
	}
	_, werr := f.Write(whole)
	cerr := f.Close()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("zerops-browser-%d.json", os.Getpid()))
	if werr != nil || cerr != nil || os.Rename(f.Name(), path) != nil {
		_ = os.Remove(f.Name())
		return ""
	}
	return path
}

// cutBrowserOutput keeps an output within limit chars: its head, as a JSON
// string ending in note, when it is longer; nothing at limit 0.
func cutBrowserOutput(raw json.RawMessage, limit int, note string) json.RawMessage {
	if len(raw) <= limit {
		return raw
	}
	if limit == 0 {
		return nil
	}
	head := []rune(string(raw[:limit]))
	cut, err := json.Marshal(string(head[:len(head)-1]) + note)
	if err != nil {
		return nil
	}
	return cut
}
