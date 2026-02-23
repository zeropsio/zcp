// Tests for: manage.go — zerops_manage MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestManageTool_Start(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-start-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "start", "serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_Stop(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-stop-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "stop", "serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_Restart(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-restart-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "restart", "serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_MissingAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	// SDK schema validation rejects missing required "action" field.
	err := callToolMayError(t, srv, "zerops_manage", map[string]any{
		"serviceHostname": "api",
	})
	if err == nil {
		t.Error("expected error for missing action")
	}
}

func TestManageTool_MissingService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	// SDK schema validation rejects missing required "serviceHostname" field.
	err := callToolMayError(t, srv, "zerops_manage", map[string]any{
		"action": "start",
	})
	if err == nil {
		t.Error("expected error for missing service hostname")
	}
}

func TestManageTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "", "serviceHostname": "api",
	})

	if !result.IsError {
		t.Error("expected IsError for empty action")
	}
}

func TestManageTool_EmptyServiceHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "start", "serviceHostname": "",
	})

	if !result.IsError {
		t.Error("expected IsError for empty serviceHostname")
	}
}

func TestManageTool_Reload(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-reload-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "reload", "serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_ConnectStorage_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev"},
			{ID: "svc-2", Name: "storage"},
		}).
		WithProcess(&platform.Process{
			ID:     "proc-connect-storage-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "connect-storage", "serviceHostname": "appdev", "storageHostname": "storage",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_ConnectStorage_MissingStorageHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "connect-storage", "serviceHostname": "appdev", "storageHostname": "",
	})

	if !result.IsError {
		t.Error("expected IsError for missing storageHostname")
	}
}

func TestManageTool_DisconnectStorage_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev"},
			{ID: "svc-2", Name: "storage"},
		}).
		WithProcess(&platform.Process{
			ID:     "proc-disconnect-storage-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "disconnect-storage", "serviceHostname": "appdev", "storageHostname": "storage",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["status"] != statusFinished {
		t.Errorf("status = %v, want FINISHED", parsed["status"])
	}
}

func TestManageTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_manage", map[string]any{
		"action": "explode", "serviceHostname": "api",
	})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}
