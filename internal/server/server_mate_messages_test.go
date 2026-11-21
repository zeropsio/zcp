package server

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// witness: internal/tools/mate_messages.go | if _, ok := openHQ(httpClient, rt); !ok { → if _, ok := openHQ(httpClient, rt); ok && !ok {
func TestServer_MateMessageTools_OnlyWhereAMateReachesItsHQ(t *testing.T) {
	// Non-parallel: hqtest.Link sets the process environment and t.Chdir
	// rebases the state dir the servers derive.
	t.Chdir(t.TempDir())
	mate := runtime.Info{InContainer: true, ServiceID: "s1", MateEnabled: true}
	tests := []struct {
		name   string
		rt     runtime.Info
		chief  bool
		linked bool
		want   bool
	}{
		{name: "a Mate linked to its HQ", rt: mate, linked: true, want: true},
		{name: "the Chief linked to its HQ", rt: runtime.Info{Chief: true, InContainer: true, ServiceID: "s1", MateEnabled: true}, chief: true, linked: true, want: true},
		{name: "a Mate that cannot address its HQ", rt: mate},
		{name: "a container without the flag", rt: runtime.Info{InContainer: true, ServiceID: "s1"}, linked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if tt.linked {
				hqtest.Link(t, "http://hq.invalid", "p1", "fixture")
			} else {
				hqtest.Link(t, "", "", "")
			}
			var names []string
			if tt.chief {
				names = listOrgServerTools(t, tt.rt)
			} else {
				names = listProjectServerTools(t, tt.rt)
			}
			for _, tool := range []string{"zerops_list_mates", "zerops_message_mate"} {
				if got := slices.Contains(names, tool); got != tt.want {
					t.Errorf("%s registered = %v, want %v", tool, got, tt.want)
				}
			}
		})
	}
}

func listProjectServerTools(t *testing.T, rt runtime.Info) []string {
	t.Helper()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil)
	srv := New(context.Background(), mock, &auth.Info{ProjectID: "p1", Key: func() string { return "test" }, APIHost: "localhost"},
		store, platform.NewMockLogFetcher(), nopStandupSSH{}, nil, rt, nil)
	t.Cleanup(srv.Stop)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}
