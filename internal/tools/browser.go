package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// RegisterBrowser registers the zerops_browser tool. Only called by server.go
// when running inside the ZCP container (where agent-browser is installed).
func RegisterBrowser(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_browser",
		Description: "Drive Chrome via agent-browser in ONE bounded batch (ZCP-container only). " +
			"The tool manages lifecycle: you pass url + inner commands, the tool auto-wraps " +
			"[open url] + your commands + [errors] + [console] + [close]. Use this for recipe close-step " +
			"browser verification — one call per subdomain (appstage, then appdev). " +
			"Before calling: stop background dev processes on every dev container (pkill -f 'nest start' etc) " +
			"— they compete for the fork budget and crash Chrome. " +
			"On fork exhaustion, context timeout, daemon crash, OR a CDP-wedge signal in per-step errors " +
			"(\"CDP command timed out\", \"Target closed\", \"Protocol error\") the tool auto-runs a full reset: " +
			"pidfile-based process-group SIGKILL reaps Chrome + helpers, then pkill --exact fallback against " +
			"chrome/chromium/chromium-browser/google-chrome/headless_shell reaps any escapees. " +
			"forkRecoveryAttempted=true is set and the message names the triggering signal. " +
			"If a prior call returned forkRecoveryAttempted=true and the immediate retry still wedges, " +
			"pass forceReset=true on the NEXT call to fully reset the daemon + Chrome state BEFORE the batch starts. " +
			"NEVER run raw `pkill -f chrome` from Bash — that pattern matches code-server's --no-chrome CLI arg " +
			"and has killed the user's editor in past runs (v27 incident). The tool's --exact fallback is safe; " +
			"raw `pkill -f` is not. " +
			"Inner command vocabulary (inside commands[]): [\"snapshot\",\"-i\",\"-c\"], [\"click\",\"@e1\"], " +
			"[\"fill\",\"@e2\",\"text\"], [\"find\",\"role\",\"button\",\"Submit\",\"click\"], [\"get\",\"text\",\"<sel>\"], " +
			"[\"get\",\"count\",\"<sel>\"], [\"is\",\"visible\",\"<sel>\"], [\"wait\",\"500\"]. " +
			"Do NOT pass [\"open\",...] or [\"close\"] in commands — both are stripped. " +
			"Do NOT use [\"eval\",...] — dedicated commands produce structured output. " +
			"Returns: steps[], errorsOutput (from final [errors] step), consoleOutput (from final [console] step), " +
			"durationMs, forkRecoveryAttempted, message.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Drive browser via agent-browser",
			IdempotentHint:  false,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ops.BrowserBatchInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.BrowserBatch(ctx, input)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
