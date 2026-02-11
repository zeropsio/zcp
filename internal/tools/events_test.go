// Tests for: events.go — zerops_events MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestEventsTool_Basic(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api"},
		}).
		WithProcessEvents([]platform.ProcessEvent{
			{ID: "p-1", ActionName: "serviceStackStart", Status: "FINISHED", Created: "2025-01-01T00:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_events", nil)

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EventsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if er.Summary.Total == 0 {
		t.Error("expected at least one event")
	}
}

func TestEventsTool_WithService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api"},
			{ID: "svc-2", Name: "db"},
		}).
		WithProcessEvents([]platform.ProcessEvent{
			{ID: "p-1", ActionName: "serviceStackStart", Status: "FINISHED", Created: "2025-01-01T00:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
			{ID: "p-2", ActionName: "serviceStackStart", Status: "FINISHED", Created: "2025-01-01T01:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "db"}}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_events", map[string]any{"serviceHostname": "api"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EventsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	for _, e := range er.Events {
		if e.Service != "api" {
			t.Errorf("expected only api events, got service=%q", e.Service)
		}
	}
}

func TestEventsTool_WithLimit(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{}).
		WithProcessEvents([]platform.ProcessEvent{}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_events", map[string]any{"limit": 5})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestEventsTool_Error(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithError("SearchProcesses", platform.NewPlatformError(platform.ErrAPIError, "API error", ""))

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_events", nil)

	if !result.IsError {
		t.Error("expected IsError for API error")
	}
}
