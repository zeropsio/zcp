// Tests for: context.go — zerops_context MCP tool handler.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestContextTool_Success(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv)

	result := callTool(t, srv, "zerops_context", nil)

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if text == "" {
		t.Error("expected non-empty context text")
	}
}

func TestContextTool_NonEmpty(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv)

	result := callTool(t, srv, "zerops_context", nil)

	text := getTextContent(t, result)
	if !strings.Contains(text, "Zerops") {
		t.Errorf("expected context to mention Zerops, got: %s", text[:min(100, len(text))])
	}
}
