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
	medusa := service("sMedusa", "pProd", "medusa", "nodejs@22", "ACTIVE")
	medusa["ports"] = []any{map[string]any{"port": 9000, "protocol": "tcp", "scheme": "http", "httpRouting": true}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+"|"+string(body))
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
				medusa,
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
// restart, a message to a Mate and a domain, and the Zerops API receives reads only —
// through the region's API, or through ZCP_ORG_API (HQ's Chief proxy), where
// every read lands under its path with the credential as the bearer and the
// token's org is never resolved.
func TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite(t *testing.T) {
	doors := []struct {
		name   string
		orgAPI string // appended to the fake API's URL; empty reads the region's API
		orgID  string
	}{
		{name: "the region's API"},
		{name: "ZCP_ORG_API", orgAPI: "/api/chief/zerops", orgID: "org1"},
	}
	for _, door := range doors {
		t.Run(door.name, func(t *testing.T) {
			// Non-parallel: t.Chdir (NewOrg derives its state dir from the cwd).
			t.Chdir(t.TempDir())
			api, seen := fakeOrgAPI(t)
			ctx := context.Background()

			access := platform.OrgAccess{Token: "org-token", APIHost: api.URL, OrgID: door.orgID}
			if door.orgAPI != "" {
				access.APIHost = "api.region.invalid"
				access.OrgAPI = api.URL + door.orgAPI
			}
			org, err := platform.OpenOrgReader(ctx, access)
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
				method, rest, _ := strings.Cut(req, " ")
				path, rest, _ := strings.Cut(rest, " ")
				bearer, _, _ := strings.Cut(rest, "|")
				if method != http.MethodGet && (method != http.MethodPost || !strings.HasSuffix(path, "/search")) {
					t.Errorf("org mode sent a write: %s %s", method, path)
				}
				if bearer != "Bearer org-token" {
					t.Errorf("%s %s carried Authorization %q", method, path, bearer)
				}
				if door.orgAPI == "" {
					continue
				}
				if !strings.HasPrefix(path, door.orgAPI+"/") {
					t.Errorf("%s %s did not go to ZCP_ORG_API", method, path)
				}
				if strings.HasSuffix(path, "/user/info") {
					t.Errorf("org mode resolved the token's org through ZCP_ORG_API: %s", path)
				}
			}
		})
	}
}
