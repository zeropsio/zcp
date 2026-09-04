package tools

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// BrowserInput is the tools-layer input for zerops_browser. It mirrors
// ops.BrowserBatchInput but types ForceReset as FlexBool so a stringified
// boolean ("true") from some agents unmarshals cleanly instead of being
// rejected at the schema layer (B4). browserInputSchema publishes it as
// oneOf[boolean,string].
type BrowserInput struct {
	URL            string     `json:"url"                      jsonschema:"The page URL to open. Required."`
	Commands       [][]string `json:"commands,omitempty"       jsonschema:"Inner agent-browser commands run between the auto-prepended [open url] and the auto-appended [screenshot?]/[errors]/[console]/[network requests]/[close]. Each element is one command as a string array."`
	TimeoutSeconds int        `json:"timeoutSeconds,omitempty" jsonschema:"Bounds the whole batch. Default 120, max 300."`
	ForceReset     FlexBool   `json:"forceReset,omitempty"     jsonschema:"Run a full daemon + Chrome reset BEFORE the batch. Use after a prior call returned forkRecoveryAttempted=true and the retry still wedges."`
	Screenshot     bool       `json:"screenshot,omitempty"     jsonschema:"Capture a screenshot after your commands run, before errors/console/network requests. Returned as an image content block alongside the text result."`
}

// browserInputSchema derives the published schema from BrowserInput and
// replaces the FlexBool forceReset with the oneOf[boolean,string] shape.
func browserInputSchema() *jsonschema.Schema {
	s, err := jsonschema.For[BrowserInput](nil)
	if err != nil || s == nil {
		return nil
	}
	patchFlexBoolProperty(s, "forceReset")
	return s
}

// RegisterBrowser registers the zerops_browser tool. Only called by server.go
// when running inside the ZCP container (where agent-browser is installed).
func RegisterBrowser(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_browser",
		Description: "Drive Chrome via agent-browser in ONE bounded batch (ZCP-container only). " +
			"The tool manages lifecycle: you pass url + inner commands, the tool auto-wraps " +
			"[open url] + your commands + [screenshot? if requested] + [errors] + [console] + " +
			"[network requests] + [close]. Use this for recipe close-step " +
			"browser verification — one call per subdomain (appstage, then appdev). " +
			"Before calling: stop background dev processes on every dev container (pkill -f 'nest start' etc) " +
			"— they compete for the fork budget and crash Chrome. " +
			"On fork exhaustion, context timeout, daemon crash, OR a CDP-wedge signal in per-step errors " +
			"(\"CDP command timed out\", \"Target closed\", \"Protocol error\") the tool auto-runs a full reset: " +
			"pidfile-based process-group SIGKILL reaps Chrome + helpers, then pkill --exact fallback against " +
			"chrome/chromium/chromium-browser/google-chrome/headless_shell reaps any escapees. " +
			"forkRecoveryAttempted=true is set and the message names the triggering signal. " +
			"Each step's errorKind classifies the failure (selector-not-found, not-editable, timeout, " +
			"wedge, other) so you can decide retry vs. adjust selector vs. give up. " +
			"If a prior call returned forkRecoveryAttempted=true and the immediate retry still wedges, " +
			"pass forceReset=true on the NEXT call to fully reset the daemon + Chrome state BEFORE the batch starts. " +
			"NEVER run raw `pkill -f chrome` from Bash — that pattern matches code-server's --no-chrome CLI arg " +
			"and has killed the user's editor in past runs (v27 incident). The tool's --exact fallback is safe; " +
			"raw `pkill -f` is not. " +
			"Inner command vocabulary (inside commands[]): [\"snapshot\",\"-i\",\"-c\"], [\"click\",\"@e1\"], " +
			"[\"fill\",\"@e2\",\"text\"], [\"find\",\"role\",\"button\",\"Submit\",\"click\"], [\"get\",\"text\",\"<sel>\"], " +
			"[\"get\",\"count\",\"<sel>\"], [\"is\",\"visible\",\"<sel>\"], [\"wait\",\"500\"], " +
			"[\"set\",\"viewport\",\"<w>\",\"<h>\"], [\"set\",\"device\",\"<name>\"], [\"set\",\"media\",\"dark|light\"]. " +
			"Do NOT pass [\"open\",...] or [\"close\"] in commands — both are stripped. " +
			"Do NOT use [\"eval\",...] — it is stripped; dedicated commands produce structured output. " +
			"Pass screenshot=true to capture a screenshot after your commands run — returned as " +
			"an image content block alongside the text result, not inlined into it. " +
			"Returns: steps[] (each with errorKind on failure), errorsOutput (from [errors]), " +
			"consoleOutput (from [console]), networkOutput (4xx/5xx requests from [network requests], " +
			"always populated — no flag needed; a request that failed at the network layer or is still " +
			"in flight has no status and is not reported), durationMs, forkRecoveryAttempted, message.",
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
			ForceReset:     input.ForceReset.Bool(),
			Screenshot:     input.Screenshot,
		})
		if err != nil {
			return convertError(err), nil, nil
		}
		return browserToolResult(result), nil, nil
	})
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
