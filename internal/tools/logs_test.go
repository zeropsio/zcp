// Tests for: logs.go — zerops_logs MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// recentTS returns a timestamp inside parseSince's default 1h window.
func recentTS(offsetSeconds int) string {
	return time.Now().UTC().Add(time.Duration(offsetSeconds) * time.Second).Format(time.RFC3339Nano)
}

func TestLogsTool_Basic(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: recentTS(-60), Severity: "info", Facility: "local0", Message: "started"},
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
		{Timestamp: recentTS(-60), Severity: "Error", Facility: "local0", Message: "crash"},
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

// TestLogsTool_SearchFiltersEntries — Phase 4: `search` must actually narrow
// the returned entries. The Zerops log backend silently ignores `search=`
// (verified live 2026-04-23); the tool advertises search via its JSON schema
// and now honours that promise via a client-side substring filter.
func TestLogsTool_SearchFiltersEntries(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: recentTS(-90), Severity: "info", Facility: "local0", Message: "matched: connection opened"},
		{Timestamp: recentTS(-60), Severity: "info", Facility: "local0", Message: "unrelated: keepalive"},
		{Timestamp: recentTS(-30), Severity: "info", Facility: "local0", Message: "matched: connection closed"},
	})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLogs(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_logs", map[string]any{
		"serviceHostname": "api",
		"search":          "matched:",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var lr ops.LogsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &lr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(lr.Entries) != 2 {
		t.Errorf("search-filtered entries = %d, want 2 (backend ignores `search=`; tool must filter client-side)", len(lr.Entries))
	}
	for _, e := range lr.Entries {
		if !strings.Contains(e.Message, "matched:") {
			t.Errorf("entry message %q does not match search %q", e.Message, "matched:")
		}
	}
}

// TestLogsTool_DefaultsToApplicationFacility — Phase 4: the tool's default
// scope is application logs. Daemon-facility noise (sshfs mount errors,
// systemd warnings) is excluded unless the caller explicitly widens scope.
// Without this, `zerops_logs` for a freshly-deployed service surfaces sshfs
// mount messages that the agent misinterprets as application errors.
func TestLogsTool_DefaultsToApplicationFacility(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: recentTS(-120), Severity: "info", Facility: "local0", Tag: "zerops@setup", Message: "application log"},
		{Timestamp: recentTS(-60), Severity: "info", Facility: "daemon", Tag: "sshfs", Message: "sshfs: bad mount point"},
	})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLogs(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_logs", map[string]any{"serviceHostname": "api"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var lr ops.LogsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &lr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(lr.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 (daemon noise must be filtered)", len(lr.Entries))
	}
	if lr.Entries[0].Message != "application log" {
		t.Errorf("entry message = %q, want %q — daemon noise leaked", lr.Entries[0].Message, "application log")
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
