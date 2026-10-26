// Tests for: integration — org mode (docs/spec-mate.md §2.11, §3.11) through
// the full MCP server and the real org reader, against a fake HQ overview
// that records every request.

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// fakeHQOverview answers HQ's Chief overview for a two-project organization
// and records every request it gets.
func fakeHQOverview(t *testing.T) (*httptest.Server, func() []string) {
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
	medusa := service("sMedusa", "pProd", "medusa", "nodejs@22", "ACTIVE")
	medusa["ports"] = []any{map[string]any{"port": 9000, "protocol": "tcp", "scheme": "http", "httpRouting": true}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+"|"+string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var out any
		switch r.URL.Path {
		case "/api/chief/zerops/project/search":
			out = map[string]any{"items": []any{
				project("pProd", "Contoso - production"),
				project("pRossy", "Northwind Shop - Rossy", "mate"),
			}}
		case "/api/chief/zerops/service-stack/search":
			out = map[string]any{"items": []any{
				medusa,
				service("sZcp", "pRossy", "zcp", "zcp@1", "ACTIVE"),
			}}
		case "/api/chief/members":
			out = map[string]any{"members": []any{
				map[string]any{"userId": "u1", "fullName": "Dana", "role": "ADMIN", "status": "ACTIVE"},
			}}
		default:
			w.WriteHeader(http.StatusForbidden)
			out = map[string]any{"code": "forbidden", "reason": "not_chief_project"}
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

// TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite runs the Chief's tools the
// way the model calls them: it sees the organization, proposes a restart, a
// message to a Mate, a domain and access, and HQ receives only the overview's
// reads, each presenting the project's key.
func TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite(t *testing.T) {
	// Non-parallel: t.Chdir (NewOrg derives its state dir from the cwd).
	t.Chdir(t.TempDir())
	hqServer, seen := fakeHQOverview(t)
	ctx := context.Background()

	org, err := platform.OpenOrgReader(platform.OrgAccess{Origin: hqServer.URL, Key: "project-key"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.NewOrg(ctx, org, store, runtime.Info{Chief: true}, nil)
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
		{tool: "zerops_org_overview", args: map[string]any{}, want: `"role":"ADMIN"`},
		{tool: "zerops_org_services", args: map[string]any{"project": "Contoso - production"}, want: `"hostname":"medusa"`},
		{tool: "zerops_org_propose_service", args: map[string]any{"action": "restart", "project": "Contoso - production", "service": "medusa", "reason": "stuck"}, want: `"summary":"Restart medusa in Contoso - production"`},
		{tool: "zerops_org_propose_message", args: map[string]any{"to": "Rossy", "words": "debug medusa", "reason": "Rossy's code"}, want: `"to":{"projectId":"pRossy","mateName":"Rossy"}`},
		{tool: "zerops_org_propose_domain", args: map[string]any{"project": "Contoso - production", "service": "medusa", "domains": []string{"contoso.com", "www.contoso.com"}, "port": 9000, "reason": "its own name"}, want: `"summary":"Put Contoso - production on contoso.com and www.contoso.com"`},
		{tool: "zerops_org_propose_access", args: map[string]any{"project": "Contoso - production", "people": []map[string]any{{"person": "Dana", "level": "read"}}, "reason": "she reviews the shop"}, want: `"summary":"Access to Contoso - production: Dana read only"`},
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

	reqs := seen()
	if len(reqs) == 0 {
		t.Fatal("org mode read nothing")
	}
	reads := platform.OrgReads()
	allowed := make([]string, 0, len(reads))
	for _, read := range reads {
		allowed = append(allowed, read.Method+" "+read.Path)
	}
	for _, req := range reqs {
		call, rest, _ := strings.Cut(req, " ")
		path, rest, _ := strings.Cut(rest, " ")
		authorization, _, _ := strings.Cut(rest, "|")
		if !slices.Contains(allowed, call+" "+path) {
			t.Errorf("org mode sent %s %s, outside the overview (%v)", call, path, allowed)
		}
		if authorization != "Project project-key" {
			t.Errorf("%s %s carried Authorization %q, want the project's key", call, path, authorization)
		}
	}
}
