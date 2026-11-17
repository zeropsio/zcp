// Tests for: integration — org mode (docs/spec-mate.md §2.11, OM-4) through
// the full MCP server and the real HQ client, against a fake HQ that answers
// the Chief read and records every request.

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
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// fakeHQChief answers HQ's Chief read for a two-project organization as the
// Chief's person reaches it, and records every request it gets.
func fakeHQChief(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	project := func(id, name string, tags ...string) map[string]any {
		return map[string]any{"id": id, "name": name, "status": "ACTIVE", "tags": append([]string{}, tags...)}
	}
	service := func(id, projectID, name, typ, status string, ports ...map[string]any) map[string]any {
		return map[string]any{"id": id, "projectId": projectID, "name": name, "status": status, "type": typ, "category": "USER", "ports": append([]map[string]any{}, ports...)}
	}
	web := map[string]any{"port": 9000, "protocol": "tcp", "scheme": "http", "portRouting": false, "httpRouting": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+"|"+string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var out any
		switch r.Method + " " + r.URL.Path + " " + string(body) {
		case `POST /api/mate/chief/read {"read":"projects"}`:
			out = map[string]any{"projects": []any{
				project("pChief", "Chief - Dana", "chief"),
				project("pProd", "Contoso - production"),
				project("pRossy", "Northwind Shop - Rossy", "mate"),
			}}
		case `POST /api/mate/chief/read {"read":"services"}`:
			out = map[string]any{"services": []any{
				service("sMedusa", "pProd", "medusa", "nodejs@22", "ACTIVE", web),
				service("sZcp", "pRossy", "zcp", "zcp@1", "ACTIVE"),
			}}
		case `POST /api/mate/chief/read {"read":"members"}`:
			out = map[string]any{"members": []any{
				map[string]any{"userId": "u1", "fullName": "Dana", "role": "ADMIN", "status": "ACTIVE"},
			}}
		default:
			w.WriteHeader(http.StatusBadRequest)
			out = map[string]any{"code": "invalid"}
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
// message to a Mate, a domain and access, and HQ receives only the Chief
// read of the allowlist's lists, each presenting the project's key; no
// Zerops client exists.
func TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite(t *testing.T) {
	// Non-parallel: t.Chdir (NewOrg derives its state dir from the cwd).
	t.Chdir(t.TempDir())
	hqServer, seen := fakeHQChief(t)
	ctx := context.Background()

	org, err := hq.Open(hqServer.Client(), hq.Identity{Origin: hqServer.URL, Key: "project-key", ProjectID: "pChief"})
	if err != nil {
		t.Fatalf("hq.Open: %v", err)
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
	for _, req := range reqs {
		call, rest, _ := strings.Cut(req, " ")
		path, rest, _ := strings.Cut(rest, " ")
		authorization, _, _ := strings.Cut(rest, "|")
		if call+" "+path != "POST /api/mate/chief/read" {
			t.Errorf("org mode sent %s %s, not the Chief read", call, path)
		}
		if authorization != "Project project-key" {
			t.Errorf("%s %s carried Authorization %q, want the project's key", call, path, authorization)
		}
	}
}
