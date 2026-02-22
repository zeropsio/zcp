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
	"github.com/zeropsio/zcp/internal/workflow"
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
	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, nil, nil, runtime.Info{})

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
		"zerops_workflow", "zerops_discover", "zerops_knowledge",
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
			want: "ZCP manages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inst := BuildInstructions(context.Background(), nil, "", tt.rt)
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
	// baseInstructions is short; routing is in routingInstructions.
	// Check combined constant length is reasonable.
	combined := baseInstructions + routingInstructions
	words := strings.Fields(combined)
	if len(words) < 20 || len(words) > 150 {
		t.Errorf("base+routing instructions has %d words, expected 20-150", len(words))
	}
}

func TestServer_RoutingInstructions_TrackedMode(t *testing.T) {
	t.Parallel()
	// All routing entries must use tracked mode syntax (action="start"), not legacy workflow="name".
	if !strings.Contains(routingInstructions, `action="start"`) {
		t.Error("routingInstructions should use tracked mode syntax")
	}
	// Legacy bare workflow= format should not appear in routing.
	// We check that there's no "workflow=\"bootstrap\" (REQUIRED" which was the old format.
	if strings.Contains(routingInstructions, `workflow="bootstrap" (REQUIRED`) {
		t.Error("routingInstructions should not use legacy workflow= format")
	}
}

func TestBuildInstructions_WithServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	})

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{})

	for _, want := range []string{"appdev", "appstage", "db", "nodejs@22", "postgresql@16", "RUNNING", string(workflow.StateConformant)} {
		if !strings.Contains(inst, want) {
			t.Errorf("instructions should contain %q", want)
		}
	}
	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should contain base instructions")
	}
	// Conformant project should recommend deploy with tracked mode syntax.
	if !strings.Contains(inst, `action="start"`) {
		t.Error("conformant project should use tracked mode syntax")
	}
}

func TestBuildInstructions_FiltersSystemServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "core", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "core", ServiceStackTypeCategoryName: "CORE"}},
		{Name: "buildappdevv123", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "ubuntu-build@1", ServiceStackTypeCategoryName: "BUILD"}},
		{Name: "api", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}},
	})

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{})

	// User services should appear.
	if !strings.Contains(inst, "api") {
		t.Error("instructions should contain user service 'api'")
	}
	if !strings.Contains(inst, "db") {
		t.Error("instructions should contain user service 'db'")
	}
	// System services should NOT appear.
	if strings.Contains(inst, "core") {
		t.Error("instructions should not contain system service 'core'")
	}
	if strings.Contains(inst, "buildappdevv123") {
		t.Error("instructions should not contain system service 'buildappdevv123'")
	}
}

func TestBuildInstructions_FreshProject(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices(nil)

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{})

	if !strings.Contains(inst, "empty") {
		t.Error("instructions should mention empty project")
	}
	if !strings.Contains(inst, "bootstrap") {
		t.Error("instructions should recommend bootstrap")
	}
	if !strings.Contains(inst, `action="start"`) {
		t.Error("empty project directive should use tracked mode syntax")
	}
	if !strings.Contains(inst, `mode="full"`) {
		t.Error("empty project directive should specify mode")
	}
}

func TestBuildInstructions_APIFailure(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("ListServices", platform.NewPlatformError(
		platform.ErrAPIError, "connection refused", "",
	))

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{})

	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should still contain base instructions on API failure")
	}
}

func TestBuildInstructions_NilClient(t *testing.T) {
	t.Parallel()

	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{})

	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should contain base instructions with nil client")
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

	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, nil, nil, runtime.Info{})

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
