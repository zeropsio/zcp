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

	srv := New(mock, authInfo, store, logFetcher, nil, nil, nil, nil)

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
	expectedTools := []string{
		"zerops_context", "zerops_workflow", "zerops_discover", "zerops_knowledge",
		"zerops_logs", "zerops_events", "zerops_process",
		"zerops_manage", "zerops_scale", "zerops_env", "zerops_import", "zerops_delete", "zerops_subdomain",
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

// TestServer_Instructions — NOT parallel because subtests use t.Setenv.
func TestServer_Instructions(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "contains zerops_workflow",
			check: func(t *testing.T) {
				t.Helper()
				inst := BuildInstructions()
				if !strings.Contains(inst, "zerops_workflow") {
					t.Error("Instructions should reference zerops_workflow")
				}
			},
		},
		{
			name: "reasonable length",
			check: func(t *testing.T) {
				t.Helper()
				words := strings.Fields(baseInstructions)
				if len(words) < 10 || len(words) > 80 {
					t.Errorf("baseInstructions has %d words, expected 10-80", len(words))
				}
			},
		},
		{
			name: "mentions Zerops",
			check: func(t *testing.T) {
				t.Helper()
				inst := BuildInstructions()
				if !strings.Contains(inst, "Zerops") {
					t.Error("Instructions should mention Zerops")
				}
			},
		},
		{
			name: "includes service name when env set",
			check: func(t *testing.T) {
				t.Helper()
				t.Setenv("ZEROPS_StackName", "myservice")
				inst := BuildInstructions()
				if !strings.Contains(inst, "myservice") {
					t.Error("Instructions should include service name from ZEROPS_StackName")
				}
			},
		},
		{
			name: "no service name when env empty",
			check: func(t *testing.T) {
				t.Helper()
				t.Setenv("ZEROPS_StackName", "")
				inst := BuildInstructions()
				if strings.Contains(inst, "running inside") {
					t.Error("Instructions should not mention service when ZEROPS_StackName is empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t)
		})
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

	srv := New(mock, authInfo, store, logFetcher, nil, nil, nil, nil)

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
