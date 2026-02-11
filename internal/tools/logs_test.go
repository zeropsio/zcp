// Tests for: logs.go — zerops_logs MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestLogsTool_Basic(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: "2025-01-01T00:00:00Z", Severity: "info", Message: "started"},
	})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLogs(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_logs", map[string]any{"serviceHostname": "api"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var lr ops.LogsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &lr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(lr.Entries) != 1 {
		t.Errorf("entries count = %d, want 1", len(lr.Entries))
	}
}

func TestLogsTool_WithFilters(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: "2025-01-01T00:00:00Z", Severity: "error", Message: "crash"},
	})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLogs(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_logs", map[string]any{
		"serviceHostname": "api",
		"severity":        "error",
		"since":           "1h",
		"limit":           10,
		"search":          "crash",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestLogsTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLogs(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_logs", map[string]any{"serviceHostname": ""})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}
