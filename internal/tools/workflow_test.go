// Tests for: workflow.go — zerops_workflow MCP tool handler.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkflowTool_Catalog(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv)

	result := callTool(t, srv, "zerops_workflow", nil)

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Available") {
		t.Errorf("expected catalog listing, got: %s", text[:min(100, len(text))])
	}
}

func TestWorkflowTool_Specific(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv)

	// "bootstrap" is one of the known workflows.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "bootstrap"})

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if text == "" {
		t.Error("expected non-empty workflow content")
	}
}

func TestWorkflowTool_NotFound(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "nonexistent_workflow"})

	if !result.IsError {
		t.Error("expected IsError for unknown workflow")
	}
}
