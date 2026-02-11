// Tests for: validate.go — zerops_validate MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

func TestValidateTool_Content(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterValidate(srv)

	yamlContent := "zerops:\n  - run:\n      base: nodejs@20\n"
	result := callTool(t, srv, "zerops_validate", map[string]any{"content": yamlContent})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var vr ops.ValidateResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if !vr.Valid {
		t.Error("expected valid=true for valid zerops.yml")
	}
}

func TestValidateTool_InvalidYAML(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterValidate(srv)

	result := callTool(t, srv, "zerops_validate", map[string]any{"content": "invalid: [yaml: bad"})

	if !result.IsError {
		t.Error("expected IsError for invalid YAML")
	}
}

func TestValidateTool_NoInput(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterValidate(srv)

	result := callTool(t, srv, "zerops_validate", nil)

	if !result.IsError {
		t.Error("expected IsError when no content or filePath provided")
	}
}
