// Tests for: integration — org mode (docs/spec-mate.md §2.11) through the
// full MCP server and the real read-only org reader, against a fake Zerops
// API that records every request.

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// fakeOrgAPI answers the searches org mode makes for a two-project
// organization and records every request it gets.
func fakeOrgAPI(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	project := func(id, name string, tags ...string) map[string]any {
		return map[string]any{"id": id, "clientId": "org1", "name": name, "status": "ACTIVE", "tagList": tags}
	}
	service := func(id, projectID, name, typ, status string) map[string]any {
		return map[string]any{
			"id": id, "clientId": "org1", "projectId": projectID, "name": name, "status": status,
			"serviceStackTypeInfo": map[string]any{"serviceStackTypeVersionName": typ, "serviceStackTypeCategory": "USER"},
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var out any
		switch {
		case strings.HasSuffix(r.URL.Path, "/user/info"):
			out = map[string]any{"id": "u1", "clientUserList": []any{map[string]any{"id": "cu1", "clientId": "org1"}}}
		case strings.HasSuffix(r.URL.Path, "/project/search"):
			out = map[string]any{"items": []any{
				project("pProd", "Contoso - production"),
				project("pRossy", "Northwind Shop - Rossy", "mate"),
			}}
		case strings.HasSuffix(r.URL.Path, "/service-stack/search"):
			out = map[string]any{"items": []any{
				service("sMedusa", "pProd", "medusa", "nodejs@22", "ACTIVE"),
				service("sZcp", "pRossy", "zcp", "zcp@1", "ACTIVE"),
			}}
		case strings.HasSuffix(r.URL.Path, "/search"):
			out = map[string]any{"items": []any{}}
		default:
			out = map[string]any{"items": []any{}, "list": []any{}}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite runs the Chief's
// tools the way the model calls them: it sees the organization, proposes a
// restart and a message to a Mate, and the Zerops API receives reads only.
func TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite(t *testing.T) {
	// Non-parallel: t.Chdir (NewOrg derives its state dir from the cwd).
	t.Chdir(t.TempDir())
	api, seen := fakeOrgAPI(t)
	ctx := context.Background()

	org, err := platform.OpenOrgReader(ctx, "org-token", api.URL, "")
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.NewOrg(ctx, org, api.URL, store, platform.NewMockLogFetcher(), runtime.Info{Chief: true}, nil)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	calls := []struct {
		tool string
		args map[string]any
		want string
	}{
		{tool: "zerops_org_overview", args: map[string]any{}, want: `"mate":"Rossy"`},
		{tool: "zerops_org_failures", args: map[string]any{}, want: `"scanned"`},
		{tool: "zerops_org_propose_service", args: map[string]any{"action": "restart", "project": "Contoso - production", "service": "medusa", "reason": "stuck"}, want: `"summary":"Restart medusa in Contoso - production"`},
		{tool: "zerops_org_propose_message", args: map[string]any{"to": "Rossy", "words": "debug medusa", "reason": "Rossy's code"}, want: `"to":{"projectId":"pRossy","mateName":"Rossy"}`},
	}
	for _, c := range calls {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if res.IsError || !strings.Contains(text, c.want) {
			t.Errorf("%s = %s, want it to carry %s", c.tool, text, c.want)
		}
	}

	for _, req := range seen() {
		method, rest, _ := strings.Cut(req, " ")
		path, _, _ := strings.Cut(rest, " ")
		if method != http.MethodGet && (method != http.MethodPost || !strings.HasSuffix(path, "/search")) {
			t.Errorf("org mode sent a write: %s %s", method, path)
		}
	}
}
