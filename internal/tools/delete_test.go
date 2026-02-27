// Tests for: delete.go — zerops_delete MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeleteTool_Confirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirm": true,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Verify delete now polls to completion (status=FINISHED).
	text := getTextContent(t, result)
	var proc platform.Process
	if err := json.Unmarshal([]byte(text), &proc); err != nil {
		t.Fatalf("parse delete result: %v", err)
	}
	if proc.Status != "FINISHED" {
		t.Errorf("status = %q, want %q", proc.Status, "FINISHED")
	}
}

func TestDeleteTool_NotConfirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirm": false,
	})

	if !result.IsError {
		t.Error("expected IsError when confirm=false")
	}
}

func TestDeleteTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "", "confirm": true,
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}
