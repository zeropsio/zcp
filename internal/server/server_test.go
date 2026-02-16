// Tests for: server package — MCP server setup and tool registration.
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

func TestServer_AllToolsRegistered(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	// Mount tool is now always registered (nil mounter returns error at call time).
	srv := New(mock, authInfo, store, logFetcher, nil, nil, nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	_, err = srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	// With nil deployers, zerops_deploy should NOT be registered.
	// Mount tool IS registered even with nil mounter (returns error at call time).
	expectedTools := []string{
		"zerops_context", "zerops_workflow", "zerops_discover", "zerops_knowledge",
		"zerops_logs", "zerops_events", "zerops_process",
		"zerops_manage", "zerops_scale", "zerops_env", "zerops_import", "zerops_delete", "zerops_subdomain",
		"zerops_mount",
	}

	if len(result.Tools) != len(expectedTools) {
		names := make([]string, 0, len(result.Tools))
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("expected %d tools, got %d: %v", len(expectedTools), len(result.Tools), names)
	}

	toolMap := make(map[string]bool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = true
	}
	for _, name := range expectedTools {
		if !toolMap[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
	if toolMap["zerops_deploy"] {
		t.Error("zerops_deploy should NOT be registered when deployers are nil")
	}
}

func TestServer_Instructions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rt   runtime.Info
		want string
		miss string
	}{
		{
			name: "in container with service name",
			rt:   runtime.Info{InContainer: true, ServiceName: "zcpx", ServiceID: "abc", ProjectID: "def"},
			want: "zcpx",
		},
		{
			name: "in container without service name",
			rt:   runtime.Info{InContainer: true, ServiceID: "abc"},
			miss: "running inside",
		},
		{
			name: "local dev (no context)",
			rt:   runtime.Info{},
			miss: "running inside",
		},
		{
			name: "base instructions always included",
			rt:   runtime.Info{InContainer: true, ServiceName: "myservice"},
			want: "zerops_workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inst := BuildInstructions(tt.rt)
			if tt.want != "" && !strings.Contains(inst, tt.want) {
				t.Errorf("Instructions should contain %q, got: %s", tt.want, inst)
			}
			if tt.miss != "" && strings.Contains(inst, tt.miss) {
				t.Errorf("Instructions should NOT contain %q, got: %s", tt.miss, inst)
			}
		})
	}
}

func TestServer_Instructions_ReasonableLength(t *testing.T) {
	t.Parallel()
	words := strings.Fields(baseInstructions)
	if len(words) < 10 || len(words) > 80 {
		t.Errorf("baseInstructions has %d words, expected 10-80", len(words))
	}
}

func TestServer_Connect(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	srv := New(mock, authInfo, store, logFetcher, nil, nil, nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	// Verify connection is alive by pinging.
	if err := session.Ping(ctx, nil); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}
