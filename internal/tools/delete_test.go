// Tests for: delete.go — zerops_delete MCP tool handler.

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeleteTool_Confirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirmHostname": "api",
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
	RegisterDelete(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirmHostname": "wrong",
	})

	if !result.IsError {
		t.Error("expected IsError when confirm=false")
	}
}

func TestDeleteTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "", "confirmHostname": "",
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}

func TestDeleteTool_CleansUpServiceMeta(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()

	// Write a service meta file that should be cleaned up after delete.
	meta := &workflow.ServiceMeta{
		Hostname:         "api",
		Type:             "nodejs@22",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Verify the meta file exists.
	metaPath := filepath.Join(stateDir, "services", "api.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("expected service meta to exist before delete")
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", stateDir)

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirmHostname": "api",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Service meta file should be removed after successful delete.
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("expected service meta to be deleted after service deletion")
	}
}

func TestDeleteTool_NoStateDir_StillSucceeds(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirmHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}
