package tools

import (
	"context"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// RegisterMcpPropose registers zerops_propose_mcp: a Mate proposes an MCP
// server for its agents (docs/spec-mate.md §2.11, mcp.add) and the person
// adds it on a card, typing any key there — never through the agent. A
// Mate's client draws the card, so it is Mate-gated (§2.0).
func RegisterMcpPropose(srv *mcp.Server, projectID string, rt runtime.Info) {
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_propose_mcp",
		Description: "Propose connecting an MCP server (Linear, GitHub, Sentry, a command) for this Mate's agents: its address or command, how it signs in, and the keys it needs by name only. Runs nothing: the person adds it on a card and types any key there, never in chat.",
		Annotations: orgPropose("Propose an MCP server"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in ops.McpProposalInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeMcp(projectID, rt.ProjectName, uuid.NewString, in))
	})
}
