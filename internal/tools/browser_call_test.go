package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// The call a zerops_browser request publishes is the client's own tool-call
// id, verbatim, as Claude Code sends it in _meta (recorded from Claude Code
// 2.1.326: {"claudecode/toolUseId":"toolu_…","progressToken":2}), with the
// client's name from initialize; a progress token, a value that is no
// string or an id past the bound is no id, so none is published.
// witness: internal/tools/browser.go | if id, ok := meta[key].(string); ok && id != "" && len(id) <= browserCallIDMax { → if id, ok := meta[key].(string); ok && id != "" {
func TestBrowserCall_ClientsOwnToolCallID_IsTheCallVerbatim(t *testing.T) {
	t.Parallel()
	claudeCode := &mcp.InitializeParams{ClientInfo: &mcp.Implementation{Name: "claude-code", Version: "2.1.326"}}
	tests := []struct {
		name string
		meta mcp.Meta
		init *mcp.InitializeParams
		want ops.BrowserCall
	}{
		{"claude code's tool_use id", mcp.Meta{"claudecode/toolUseId": "toolu_019MULyG55DPdWbaXBcYaPxi", "progressToken": 2},
			claudeCode, ops.BrowserCall{ID: "toolu_019MULyG55DPdWbaXBcYaPxi", Client: "claude-code"}},
		{"a progress token alone", mcp.Meta{"progressToken": "p-1"}, claudeCode, ops.BrowserCall{Client: "claude-code"}},
		{"an id that is no string", mcp.Meta{"claudecode/toolUseId": 7}, claudeCode, ops.BrowserCall{Client: "claude-code"}},
		{"an id past the bound", mcp.Meta{"claudecode/toolUseId": "toolu_" + strings.Repeat("x", browserCallIDMax)}, claudeCode, ops.BrowserCall{Client: "claude-code"}},
		{"no meta, no initialize", nil, nil, ops.BrowserCall{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := browserCall(tt.meta, tt.init); got != tt.want {
				t.Errorf("browserCall = %+v, want %+v", got, tt.want)
			}
		})
	}
}
