// Tests for: scale.go — zerops_scale MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestScaleTool_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "api",
		"cpuMode":         "SHARED",
		"minCpu":          1,
		"maxCpu":          4,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["serviceHostname"] != "api" {
		t.Errorf("serviceHostname = %q, want %q", parsed["serviceHostname"], "api")
	}
}

func TestScaleTool_WithDisk(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "db"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "db",
		"minDisk":         5.0,
		"maxDisk":         20.0,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestScaleTool_MissingService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1")

	// SDK schema validation rejects missing required "serviceHostname" field.
	err := callToolMayError(t, srv, "zerops_scale", map[string]any{
		"minCpu": 1,
	})
	if err == nil {
		t.Error("expected error for missing service hostname")
	}
}

func TestScaleTool_EmptyServiceHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "",
		"minCpu":          1,
	})

	if !result.IsError {
		t.Error("expected IsError for empty serviceHostname")
	}
}
