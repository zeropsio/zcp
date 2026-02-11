// Tests for: process.go — zerops_process MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestProcessTool_Status(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProcess(&platform.Process{
			ID:         "proc-1",
			ActionName: "serviceStackStart",
			Status:     "RUNNING",
			Created:    "2025-01-01T00:00:00Z",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock)

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var pr ops.ProcessStatusResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &pr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if pr.Status != "RUNNING" {
		t.Errorf("status = %q, want %q", pr.Status, "RUNNING")
	}
}

func TestProcessTool_Cancel(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProcess(&platform.Process{
			ID:         "proc-1",
			ActionName: "serviceStackStart",
			Status:     "RUNNING",
			Created:    "2025-01-01T00:00:00Z",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock)

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1", "action": "cancel"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var pr ops.ProcessCancelResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &pr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if pr.Status != "CANCELED" {
		t.Errorf("status = %q, want %q", pr.Status, "CANCELED")
	}
}

func TestProcessTool_MissingID(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock)

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": ""})

	if !result.IsError {
		t.Error("expected IsError for missing process ID")
	}
}

func TestProcessTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock)

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1", "action": "explode"})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}
