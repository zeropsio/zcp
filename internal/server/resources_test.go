// Tests for: MCP resource templates — zerops://docs/{+path} knowledge base resources.
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

// testResourceServer creates an MCP client session backed by a server with a
// 2-doc mock knowledge store. Mirrors the helper pattern from server_test.go.
func testResourceServer(t *testing.T) *mcp.ClientSession {
	t.Helper()

	docs := map[string]*knowledge.Document{
		"zerops://docs/services/postgresql": {
			Path:        "embed/services/postgresql.md",
			URI:         "zerops://docs/services/postgresql",
			Title:       "PostgreSQL on Zerops",
			Keywords:    []string{"postgresql", "postgres"},
			Content:     "# PostgreSQL on Zerops\n\nManaged PostgreSQL service.",
			Description: "Managed PostgreSQL service.",
		},
		"zerops://docs/services/nodejs": {
			Path:        "embed/services/nodejs.md",
			URI:         "zerops://docs/services/nodejs",
			Title:       "Node.js on Zerops",
			Keywords:    []string{"nodejs", "node"},
			Content:     "# Node.js on Zerops\n\nRuntime for Node.js applications.",
			Description: "Runtime for Node.js applications.",
		},
	}

	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
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
	t.Cleanup(func() { session.Close() })
	return session
}

func TestResources_ListTemplates_Registered(t *testing.T) {
	t.Parallel()
	session := testResourceServer(t)
	ctx := context.Background()

	result, err := session.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}

	var found bool
	for _, tmpl := range result.ResourceTemplates {
		if tmpl.Name == "zerops-docs" {
			found = true
			if tmpl.URITemplate != "zerops://docs/{+path}" {
				t.Errorf("URITemplate = %q, want %q", tmpl.URITemplate, "zerops://docs/{+path}")
			}
			if tmpl.MIMEType != "text/markdown" {
				t.Errorf("MIMEType = %q, want %q", tmpl.MIMEType, "text/markdown")
			}
			break
		}
	}
	if !found {
		t.Error("zerops-docs resource template not found")
	}
}

func TestResources_ReadDoc_Success(t *testing.T) {
	t.Parallel()
	session := testResourceServer(t)
	ctx := context.Background()

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "zerops://docs/services/postgresql",
	})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("contents length = %d, want 1", len(result.Contents))
	}

	c := result.Contents[0]
	if c.MIMEType != "text/markdown" {
		t.Errorf("MIMEType = %q, want %q", c.MIMEType, "text/markdown")
	}
	if c.Text == "" {
		t.Error("text is empty")
	}
	if c.URI != "zerops://docs/services/postgresql" {
		t.Errorf("URI = %q, want %q", c.URI, "zerops://docs/services/postgresql")
	}
}

func TestResources_ReadDoc_ContentMatches(t *testing.T) {
	t.Parallel()
	session := testResourceServer(t)
	ctx := context.Background()

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "zerops://docs/services/postgresql",
	})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}

	want := "# PostgreSQL on Zerops\n\nManaged PostgreSQL service."
	if result.Contents[0].Text != want {
		t.Errorf("text = %q, want %q", result.Contents[0].Text, want)
	}
}

func TestResources_ReadDoc_NotFound(t *testing.T) {
	t.Parallel()
	session := testResourceServer(t)
	ctx := context.Background()

	_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "zerops://docs/nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent resource, got nil")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "resource") {
		t.Errorf("error = %q, want it to mention 'not found'", err.Error())
	}
}
