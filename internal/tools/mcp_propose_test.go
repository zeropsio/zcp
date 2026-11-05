// Tests for: internal/tools/mcp_propose.go — a Mate proposes connecting an
// MCP server for its agents; nothing runs from here and no key passes
// through the agent.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestMcpPropose_OverMCP_ProposesAndRunsNothing: the tool answers an mcp.add
// proposal from this Mate's own project, and refuses one that carries a key.
func TestMcpPropose_OverMCP_ProposesAndRunsNothing(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterMcpPropose(srv, "P_MATE", runtime.Info{InContainer: true, MateEnabled: true, ProjectName: "Shop - Juno"})
	result := callTool(t, srv, "zerops_propose_mcp", map[string]any{
		"name": "linear", "title": "Linear", "why": "to read the issues you link",
		"transport": "http", "url": "https://mcp.linear.app/mcp", "auth": "oauth",
	})
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError || !strings.Contains(text, `"kind":"mcp.add"`) || !strings.Contains(text, `"projectId":"P_MATE"`) {
		t.Fatalf("result = %s, want an mcp.add proposal from P_MATE", text)
	}
	refused := callTool(t, srv, "zerops_propose_mcp", map[string]any{
		"name": "linear", "why": "w", "transport": "http", "url": "https://mcp.linear.app/mcp?token=abc", "auth": "none",
	})
	if !refused.IsError || !strings.Contains(refused.Content[0].(*mcp.TextContent).Text, "secret") {
		t.Errorf("a key in the address is not refused: %+v", refused.Content)
	}
}
