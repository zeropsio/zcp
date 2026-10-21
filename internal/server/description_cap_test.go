// Tests for: every server mode's tools/list and initialize payload against
// Claude Code's description cap.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf16"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// claudeCodeDescriptionCap is the length, in UTF-16 code units, past which
// Claude Code cuts an MCP tool description or the server instructions and
// appends "… [truncated]" (its default for a tool that is not deferred).
const claudeCodeDescriptionCap = 4096

// TestServer_EveryToolDescriptionReachesClaudeCodeWhole: every tool zcp
// advertises, and the instructions it sends at initialize, reach Claude Code
// whole — none is longer than Claude Code's description cap — in every server
// mode zcp builds.
//
// Non-parallel: overrides ops' package-level browser runner, t.Chdir and
// t.Setenv.
func TestServer_EveryToolDescriptionReachesClaudeCodeWhole(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ZCP_RECIPE_MOUNT_ROOT", t.TempDir())
	restore := ops.OverrideBrowserRunnerForTest(&stubBrowserRunner{})
	t.Cleanup(restore)

	// A Mate whose person has a Chief offers to ask it.
	chiefHQ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mate/chief" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chief":{"ownerUserId":"owner","projectId":"chief"}}`))
	}))
	t.Cleanup(chiefHQ.Close)
	t.Setenv("HOME", t.TempDir())
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: chiefHQ.URL, ProjectID: "p1", Credential: "fixture"}); err != nil {
		t.Fatal(err)
	}

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	project := func(rt runtime.Info, ssh ops.SSHDeployer) *Server {
		mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil)
		return New(context.Background(), mock, &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"},
			store, platform.NewMockLogFetcher(), ssh, nil, rt, nil)
	}
	org := func(rt runtime.Info) *Server {
		return NewOrg(context.Background(), emptyOrg{}, "localhost", store, platform.NewMockLogFetcher(), rt, nil)
	}
	container := runtime.Info{InContainer: true, ServiceID: "s1"}
	mate := runtime.Info{InContainer: true, ServiceID: "s1", MateEnabled: true}

	tests := []struct {
		name  string
		build func() *Server
		// lists names tools the mode must advertise, so the cap is checked
		// on the mode's own surface, not on a smaller one.
		lists []string
	}{
		{name: "local", build: func() *Server { return project(runtime.Info{}, nil) },
			lists: []string{"zerops_workflow", "zerops_deploy"}},
		{name: "local authoring", build: func() *Server { return project(runtime.Info{Authoring: true}, nil) },
			lists: []string{"zerops_recipe", "zerops_port"}},
		{name: "container", build: func() *Server { return project(container, nopStandupSSH{}) },
			lists: []string{"zerops_browser", "zerops_dev_server", "zerops_deploy_batch"}},
		{name: "a Mate's container", build: func() *Server { return project(mate, nopStandupSSH{}) },
			lists: []string{"zerops_browser", "zerops_publish_page", "zerops_observe", "zerops_propose_ask_chief"}},
		{name: "the Chief, local", build: func() *Server { return org(runtime.Info{Chief: true}) },
			lists: []string{"zerops_org_overview", "zerops_knowledge"}},
		{name: "the Chief in a Mate's container", build: func() *Server {
			return org(runtime.Info{Chief: true, InContainer: true, ServiceID: "s1", MateEnabled: true})
		},
			lists: []string{"zerops_org_overview", "zerops_publish_page"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			if _, err := tt.build().MCPServer().Connect(ctx, st, nil); err != nil {
				t.Fatalf("server connect: %v", err)
			}
			session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatalf("client connect: %v", err)
			}
			defer session.Close()

			if n := utf16Len(session.InitializeResult().Instructions); n > claudeCodeDescriptionCap {
				t.Errorf("server instructions are %d UTF-16 units, over Claude Code's %d", n, claudeCodeDescriptionCap)
			}
			result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatalf("list tools: %v", err)
			}
			listed := make(map[string]bool, len(result.Tools))
			for _, tool := range result.Tools {
				listed[tool.Name] = true
				if n := utf16Len(tool.Description); n > claudeCodeDescriptionCap {
					t.Errorf("%s description is %d UTF-16 units, %d over Claude Code's %d",
						tool.Name, n, n-claudeCodeDescriptionCap, claudeCodeDescriptionCap)
				}
			}
			for _, name := range tt.lists {
				if !listed[name] {
					t.Errorf("mode does not advertise %s", name)
				}
			}
		})
	}
}

// utf16Len is s's length in UTF-16 code units, the unit Claude Code's cap
// counts in.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
