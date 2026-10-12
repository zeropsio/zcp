// Tests for: integration — zerops_propose_ask_chief through the full MCP
// server in a Mate's container: the Mate asks its person's Chief, and nothing
// is sent (docs/spec-mate.md §2.11).

package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

func TestAskChief_OverMCP_ProposesFromThisProjectAndSendsNothing(t *testing.T) {
	// Non-parallel: t.Chdir rebases the state dir server.New derives.
	t.Chdir(t.TempDir())
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "Contoso - Rossy"}).
		WithServices(nil)
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.New(context.Background(), mock,
		&auth.Info{ProjectID: "proj-1", ProjectName: "Contoso - Rossy", Token: "test-token", APIHost: "localhost"},
		store, platform.NewMockLogFetcher(), &mockSSHDeployer{output: []byte("ok")}, nil,
		runtime.Info{InContainer: true, ServiceID: "s-zcp", ServiceName: "zcp", ProjectID: "proj-1", MateEnabled: true}, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	before := mockCalls(mock)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "zerops_propose_ask_chief", Arguments: map[string]any{
		"words":           "Is production's medusa failing too?",
		"attachmentTitle": "my failing test",
		"attachmentText":  "GET /cart 502",
		"reason":          "production is outside this project",
	}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", getTextContent(t, result))
	}
	var got ops.OrgProposalResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &got); err != nil {
		t.Fatalf("result is not one JSON document: %v", err)
	}
	var params ops.ChiefMessageParams
	if err := json.Unmarshal(got.Proposal.Params, &params); err != nil {
		t.Fatalf("params: %v", err)
	}
	p := got.Proposal
	if p.ID == "" || p.Kind != "chief.message" || p.ProjectID != "proj-1" || p.ProjectName != "Contoso - Rossy" ||
		p.Summary != "Ask your Chief: Is production's medusa failing too?" || p.Reason != "production is outside this project" ||
		params.Words != "Is production's medusa failing too?" || params.Attachment == nil || params.Attachment.Text != "GET /cart 502" {
		t.Errorf("proposal = %+v, params = %+v", p, params)
	}
	if got.Note != "Nothing has been sent. The person approves or declines it on the card." {
		t.Errorf("note = %q", got.Note)
	}
	if after := mockCalls(mock); after != before {
		t.Errorf("asking the Chief called the platform: %d calls before, %d after", before, after)
	}
}

// mockCalls is how many platform calls the mock has answered.
func mockCalls(m *platform.Mock) int {
	n := 0
	for _, c := range m.CallCounts {
		n += c
	}
	return n
}
