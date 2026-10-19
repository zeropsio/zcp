package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// BrowserInput is the tools-layer input for zerops_browser. It mirrors
// ops.BrowserBatchInput; browserInputSchema publishes isolation's levels as
// an enum.
type BrowserInput struct {
	URL            string     `json:"url"                      jsonschema:"The page URL to open. Required."`
	Commands       [][]string `json:"commands,omitempty"       jsonschema:"Inner agent-browser commands run between the auto-prepended [open url] + first-paint wait and the auto-appended [screenshot?]/[errors]/[console]/[network requests]. A leading [\"set\",\"viewport\",\"<w>\",\"<h>\"] runs before the open, so the page loads at that size; a call without one loads at 1280×577, the size a fresh Chrome opens at, whatever an earlier call set. Each element is one command as a string array."`
	TimeoutSeconds int        `json:"timeoutSeconds,omitempty" jsonschema:"Bounds the whole call, including the wait for the browser. Default 120, max 300."`
	Isolation      string     `json:"isolation,omitempty"      jsonschema:"What the page remembers from earlier calls. tab (default): cookies, login, storage, HTTP cache and service workers carry over. fresh-context: none of them, for the first check after a deploy; it stops Chrome as the call ends. new-browser: also a fresh Chrome."`
	Screenshot     bool       `json:"screenshot,omitempty"     jsonschema:"Capture a screenshot after your commands run, before errors/console/network requests. Returned as an image content block alongside the text result."`
	FullPage       bool       `json:"fullPage,omitempty"       jsonschema:"Capture the whole page instead of the viewport, as one image cut to its top 8000 px (screenshot.capturedHeight then says how tall the capture was). The capture does not scroll: content that loads when scrolled into view stays blank unless you scroll to it or wait for it first. Implies screenshot."`
}

// browserInputSchema derives the published schema from BrowserInput and
// lists isolation's three levels.
func browserInputSchema() *jsonschema.Schema {
	s, err := jsonschema.For[BrowserInput](nil)
	if err != nil || s == nil {
		return nil
	}
	if prop, ok := s.Properties["isolation"]; ok {
		prop.Enum = []any{ops.BrowserIsolationTab, ops.BrowserIsolationFreshContext, ops.BrowserIsolationNewBrowser}
	}
	return s
}

// RegisterBrowser registers the zerops_browser tool. Only called by server.go
// when running inside the ZCP container (where agent-browser is installed).
// stateDir is where a screenshot is kept as one of the Mate's pictures; ""
// keeps none.
func RegisterBrowser(srv *mcp.Server, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_browser",
		Description: "Drive Chrome via agent-browser in ONE bounded batch (ZCP-container only). " +
			"The tool manages lifecycle: it runs [open url] + " +
			"your commands + [screenshot if requested] + [errors] + [console] + [network requests]. " +
			"The call already waits for the page's first paint; after a click, scroll, reload or navigation, wait for what should appear — " +
			"[\"wait\",\"<sel>\"] or [\"wait\",\"--text\",\"<text>\"] — never for a fixed time. " +
			"Only wait polls (up to 25 s, or [\"wait\",\"<sel>\",\"--timeout\",\"<ms>\"]); click, fill, check, find, get and is look once " +
			"and fail at once on an element that is not there yet. " +
			"Recipe close-step verification: one call per subdomain (appstage, then appdev). " +
			"One warm Chrome serves every call: calls take turns (waiting for the browser " +
			"within timeoutSeconds) and each call's tabs close when it ends. A call that times out, is cancelled " +
			"or wedges Chrome returns its finished steps and recycles the browser (browserRecycled). " +
			"isolation: \"tab\" (default) keeps cookies, login, storage, HTTP cache and service workers from earlier calls — " +
			"right for re-checking your change after a deploy. " +
			"\"fresh-context\" starts with none of them — only for the first check after a deploy that changed sign-in, storage, " +
			"a service worker or long-cached assets, or for a signed-out view; it stops Chrome as the call ends, so the next call starts a cold one. " +
			"Every other call stays on \"tab\". \"new-browser\" also restarts Chrome first, for a page that misbehaves only in the warm browser. " +
			"freshBrowser: true means the call ran in a Chrome started for it (always after new-browser); " +
			"browserRecycled: true means the call ended unclean and the browser was stopped (a fresh-context call stops it cleanly, without it). " +
			"Each step's errorKind: selector-not-found, not-editable, timeout, wedge, invalid-command " +
			"(an unknown command, subcommand or find action: fix the command, not the page), " +
			"stale-ref (an @eN ref no snapshot in this call's tab made) or other failures. " +
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
			"open, close, tab and window never run; each returns a failed invalid-command step. " +
			"Pass screenshot=true to capture the viewport after your commands run, fullPage=true for the whole page as one image " +
			"(cut to its top 8000 px; screenshot.capturedHeight gives its full height) — returned as " +
			"an image block beside the text result and kept as a picture the result names " +
			"(screenshot.picture, e.g. \"shot-3\") for screenshot.keptFor: write ![what it shows](shot-3) in a " +
			"zerops_workflow action=\"describe-change\" description to show it to the person. " +
			"Returns: steps[], errorsOutput, consoleOutput, " +
			"networkOutput (4xx/5xx from [network requests], always on; " +
			"a request with no status — failed at the network layer or still in flight — is not listed), " +
			"durationMs, freshBrowser, browserRecycled, message. " +
			"To say how a call ran, report the result's durationMs, freshBrowser and browserRecycled; a script that summarises results keeps them.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Drive browser via agent-browser",
			IdempotentHint:  false,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		},
		InputSchema: browserInputSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input BrowserInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.BrowserBatch(ctx, ops.BrowserBatchInput{
			URL:            input.URL,
			Commands:       input.Commands,
			TimeoutSeconds: input.TimeoutSeconds,
			Isolation:      input.Isolation,
			Screenshot:     input.Screenshot,
			FullPage:       input.FullPage,
		})
		if err != nil {
			return convertError(err), nil, nil
		}
		keepBrowserPicture(stateDir, result)
		return browserToolResult(result), nil, nil
	})
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
	tr := jsonResult(result)
	if result.Screenshot != nil && len(result.Screenshot.PNG) > 0 {
		tr.Content = append(tr.Content, &mcp.ImageContent{
			Data:     result.Screenshot.PNG,
			MIMEType: "image/png",
		})
	}
	return tr
}
