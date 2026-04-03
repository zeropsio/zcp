// Tests for: tool annotations — verify all tools have correct metadata.
package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// nopMounter satisfies ops.Mounter for annotation tests (never called).
type nopMounter struct{}

var _ ops.Mounter = (*nopMounter)(nil)

func (*nopMounter) CheckMount(_ context.Context, _ string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (*nopMounter) Mount(_ context.Context, _, _ string) error           { return nil }
func (*nopMounter) Unmount(_ context.Context, _, _ string) error         { return nil }
func (*nopMounter) ForceUnmount(_ context.Context, _, _ string) error    { return nil }
func (*nopMounter) IsWritable(_ context.Context, _ string) (bool, error) { return false, nil }
func (*nopMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (*nopMounter) HasUnit(_ context.Context, _ string) (bool, error) { return false, nil }
func (*nopMounter) CleanupUnit(_ context.Context, _ string) error     { return nil }

// nopSSH satisfies ops.SSHDeployer for annotation tests (never called).
type nopSSH struct{}

func (*nopSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) { return nil, nil }

func TestAnnotations_AllToolsHaveTitleAndAnnotations(t *testing.T) {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	toolMap := make(map[string]*mcp.Tool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = tool
	}

	tests := []struct {
		name        string
		title       string
		readOnly    bool
		idempotent  bool
		destructive *bool
		openWorld   *bool
	}{
		// Read-only tools
		{name: "zerops_workflow", title: "Workflow orchestration", openWorld: boolPtr(false)},
		{name: "zerops_discover", title: "Discover project and services", readOnly: true, idempotent: true},
		{name: "zerops_knowledge", title: "Zerops knowledge access", readOnly: true, idempotent: true},
		{name: "zerops_logs", title: "Fetch service logs", readOnly: true, idempotent: true},
		{name: "zerops_events", title: "Fetch project activity timeline", readOnly: true, idempotent: true},
		{name: "zerops_verify", title: "Verify service health", readOnly: true, idempotent: true},

		// Mutating tools
		{name: "zerops_process", title: "Check or cancel async process", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_manage", title: "Manage service lifecycle", idempotent: true, destructive: boolPtr(true)},
		{name: "zerops_scale", title: "Scale a service", idempotent: true, destructive: boolPtr(true)},
		{name: "zerops_delete", title: "Delete a service", destructive: boolPtr(true)},
		{name: "zerops_subdomain", title: "Enable or disable subdomain", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_deploy", title: "Deploy code to a service", destructive: boolPtr(true)},
		{name: "zerops_env", title: "Manage environment variables", destructive: boolPtr(true)},
		{name: "zerops_import", title: "Import services from YAML", destructive: boolPtr(true)},
		{name: "zerops_mount", title: "Mount/unmount service filesystems", idempotent: true, destructive: boolPtr(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tool, ok := toolMap[tt.name]
			if !ok {
				t.Fatalf("tool %s not found", tt.name)
			}

			// All tools must have non-empty description.
			if tool.Description == "" {
				t.Errorf("tool %s has empty description", tt.name)
			}

			if tool.Annotations == nil {
				t.Fatalf("tool %s has nil annotations", tt.name)
			}

			ann := tool.Annotations

			if ann.Title != tt.title {
				t.Errorf("tool %s: Title = %q, want %q", tt.name, ann.Title, tt.title)
			}
			if ann.ReadOnlyHint != tt.readOnly {
				t.Errorf("tool %s: ReadOnlyHint = %v, want %v", tt.name, ann.ReadOnlyHint, tt.readOnly)
			}
			if ann.IdempotentHint != tt.idempotent {
				t.Errorf("tool %s: IdempotentHint = %v, want %v", tt.name, ann.IdempotentHint, tt.idempotent)
			}
			if !equalBoolPtr(ann.DestructiveHint, tt.destructive) {
				t.Errorf("tool %s: DestructiveHint = %v, want %v", tt.name, ptrStr(ann.DestructiveHint), ptrStr(tt.destructive))
			}
			if !equalBoolPtr(ann.OpenWorldHint, tt.openWorld) {
				t.Errorf("tool %s: OpenWorldHint = %v, want %v", tt.name, ptrStr(ann.OpenWorldHint), ptrStr(tt.openWorld))
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func equalBoolPtr(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrStr(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	if *p {
		return "true"
	}
	return "false"
}

func TestAnnotations_DescriptionWordCount(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t)

	const maxWords = 60

	// Tools subject to the 60-word cap (excludes workflow, delete, logs, events, mount,
	// scale which were not part of the trim plan).
	trimmedTools := []string{
		"zerops_discover",
		"zerops_deploy",
		"zerops_import",
		"zerops_manage",
		"zerops_env",
		"zerops_subdomain",
		"zerops_knowledge",
		"zerops_verify",
		"zerops_process",
	}

	for _, name := range trimmedTools {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, ok := toolMap[name]
			if !ok {
				t.Fatalf("tool %s not found", name)
			}
			words := strings.Fields(tool.Description)
			if len(words) > maxWords {
				t.Errorf("tool %s: description has %d words (max %d):\n%s",
					name, len(words), maxWords, tool.Description)
			}
		})
	}
}

func TestAnnotations_DescriptionKeywords(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t)

	tests := []struct {
		name     string
		keywords []string
	}{
		{name: "zerops_discover", keywords: []string{"env var", "includeEnvs"}},
		{name: "zerops_deploy", keywords: []string{"SSH", "zerops.yaml", "deployFiles"}},
		{name: "zerops_import", keywords: []string{"workflow", "YAML"}},
		{name: "zerops_manage", keywords: []string{"reload", "restart", "connect-storage", "/mnt/"}},
		{name: "zerops_env", keywords: []string{"set", "delete", "reload"}},
		{name: "zerops_subdomain", keywords: []string{"enable", "disable", "subdomain"}},
		{name: "zerops_knowledge", keywords: []string{"briefing", "scope", "query", "recipe"}},
		{name: "zerops_verify", keywords: []string{"health", "pass", "fail", "info"}},
		{name: "zerops_process", keywords: []string{"cancel", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool, ok := toolMap[tt.name]
			if !ok {
				t.Fatalf("tool %s not found", tt.name)
			}
			desc := strings.ToLower(tool.Description)
			for _, kw := range tt.keywords {
				if !strings.Contains(desc, strings.ToLower(kw)) {
					t.Errorf("tool %s: description missing keyword %q:\n%s",
						tt.name, kw, tool.Description)
				}
			}
		})
	}
}

// listAllTools creates a test MCP server and returns all registered tools by name.
func listAllTools(t *testing.T) map[string]*mcp.Tool {
	t.Helper()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	toolMap := make(map[string]*mcp.Tool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = tool
	}
	return toolMap
}

func TestAnnotations_DeleteToolRequiresExplicitApproval(t *testing.T) {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var deleteTool *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "zerops_delete" {
			deleteTool = tool
			break
		}
	}
	if deleteTool == nil {
		t.Fatal("zerops_delete tool not found")
	}

	// Delete tool description must require explicit user approval.
	if !strings.Contains(deleteTool.Description, "explicit user approval") {
		t.Errorf("zerops_delete description should contain 'explicit user approval', got: %s", deleteTool.Description)
	}
}
