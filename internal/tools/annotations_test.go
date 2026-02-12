// Tests for: tool annotations — verify all tools have correct metadata.
package tools_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/server"
)

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

	srv := server.New(mock, authInfo, store, logFetcher, nil, nil)

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
		name           string
		readOnly       bool
		idempotent     bool
		destructive    *bool
		openWorld      *bool
		hasAnnotations bool
	}{
		// Read-only tools
		{name: "zerops_context", readOnly: true, idempotent: true, openWorld: boolPtr(false), hasAnnotations: true},
		{name: "zerops_workflow", readOnly: true, idempotent: true, openWorld: boolPtr(false), hasAnnotations: true},
		{name: "zerops_discover", readOnly: true, idempotent: true, hasAnnotations: true},
		{name: "zerops_knowledge", readOnly: true, idempotent: true, hasAnnotations: true},
		{name: "zerops_logs", readOnly: true, idempotent: true, hasAnnotations: true},
		{name: "zerops_events", readOnly: true, idempotent: true, hasAnnotations: true},
		{name: "zerops_process", readOnly: true, idempotent: true, hasAnnotations: true},

		// Destructive tools
		{name: "zerops_manage", destructive: boolPtr(true), hasAnnotations: true},
		{name: "zerops_scale", destructive: boolPtr(true), hasAnnotations: true},
		{name: "zerops_delete", destructive: boolPtr(true), hasAnnotations: true},

		// Idempotent tools
		{name: "zerops_subdomain", idempotent: true, hasAnnotations: true},

		// Tools without annotations
		{name: "zerops_deploy", hasAnnotations: false},
		{name: "zerops_env", hasAnnotations: false},
		{name: "zerops_import", hasAnnotations: false},
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

			if !tt.hasAnnotations {
				if tool.Annotations != nil {
					t.Errorf("tool %s should have no annotations, got %+v", tt.name, tool.Annotations)
				}
				return
			}

			if tool.Annotations == nil {
				t.Fatalf("tool %s has nil annotations", tt.name)
			}

			ann := tool.Annotations

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
