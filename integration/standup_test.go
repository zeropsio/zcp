// Tests for: integration — zerops_standup through the full MCP server, in a
// Mate's container, against a mock platform and a fake Gitea.

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestStandup_OverMCP_ABotInNoGroupIsRefusedAndNothingIsTouched calls the
// stand-up the way the model does on a Mate's first turn. The bot the broker
// made belongs to no group yet, so there is no recipe to read: the answer is
// a refusal that names the adopt route, and no service is adopted.
func TestStandup_OverMCP_ABotInNoGroupIsRefusedAndNothingIsTouched(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and t.Setenv (the Git variables,
	// read from the process env where no live env store exists).
	dir := t.TempDir()
	t.Chdir(dir)
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"mate-p1"}`))
		case "/api/v1/user/orgs":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gitea.Close()
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", "bot-token")

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "beviro-wren"}).
		WithServices([]platform.ServiceStack{{ID: "s-appdev", Name: "appdev", Status: "ACTIVE"}})
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.New(context.Background(), mock,
		&auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1"},
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

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "zerops_standup", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call zerops_standup: %v", err)
	}
	var text strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if !result.IsError {
		t.Fatalf("want a refusal, got: %s", text.String())
	}
	for _, want := range []string{"PREREQUISITE_MISSING", "belongs to no group", `route=\"adopt\"`} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("refusal does not say %q: %s", want, text.String())
		}
	}
	if result.StructuredContent != nil {
		t.Error("a tool result carries no structuredContent (Claude Code would replace the text with it)")
	}
	if metas, _ := workflow.ListServiceMetas(dir + "/.zcp/state"); len(metas) != 0 {
		t.Errorf("a refusal touches nothing, but %d services were adopted", len(metas))
	}
}
