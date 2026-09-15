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
			{ID: "p-1", ActionName: "stack.start", Status: statusFinished, Created: "2025-01-01T00:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, nil, "proj-1")

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
			{ID: "p-1", ActionName: "stack.start", Status: statusFinished, Created: "2025-01-01T00:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
			{ID: "p-2", ActionName: "stack.start", Status: statusFinished, Created: "2025-01-01T01:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "db"}}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, nil, "proj-1")

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

// TestEvents_ServiceFilter_ListsAppVersions pins the GF-8 candidate
// source (docs/spec-workflows.md §8 R2 / §12.6 GF-8): filtering
// zerops_events by serviceHostname populates the `appVersions` section
// with the target's app-version history, ACTIVE/BACKUP flagged — the
// same rows ops.ReactivateAppVersion's "does not belong" refusal lists,
// reachable here WITHOUT an agent needing to probe zerops_deploy with a
// fake appVersion id first.
func TestEvents_ServiceFilter_ListsAppVersions(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage"},
		}).
		WithProcessEvents([]platform.ProcessEvent{}).
		WithAppVersionEvents([]platform.AppVersionEvent{}).
		WithServiceAppVersions("svc-1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "svc-1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "svc-1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, nil, "proj-1")

	result := callTool(t, srv, "zerops_events", map[string]any{"serviceHostname": "appstage"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EventsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(er.AppVersions) != 2 {
		t.Fatalf("len(AppVersions) = %d, want 2: %+v", len(er.AppVersions), er.AppVersions)
	}
	byID := make(map[string]ops.AppVersionCandidate, len(er.AppVersions))
	for _, c := range er.AppVersions {
		byID[c.ID] = c
	}
	if !byID["av-new"].Active {
		t.Errorf("av-new not marked Active: %+v", byID["av-new"])
	}
	if !byID["av-old"].Backup {
		t.Errorf("av-old not marked Backup: %+v", byID["av-old"])
	}
}

// TestEvents_NoServiceFilter_OmitsAppVersions pins that AppVersions stays
// empty for a project-wide (unfiltered) call — the section only makes
// sense once a caller has scoped to a single service.
func TestEvents_NoServiceFilter_OmitsAppVersions(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage"},
		}).
		WithProcessEvents([]platform.ProcessEvent{}).
		WithAppVersionEvents([]platform.AppVersionEvent{}).
		WithServiceAppVersions("svc-1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "svc-1", Status: platform.ServiceStatusActive, Sequence: 1},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, nil, "proj-1")

	result := callTool(t, srv, "zerops_events", nil)
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EventsResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(er.AppVersions) != 0 {
		t.Errorf("AppVersions = %+v, want empty for an unfiltered call", er.AppVersions)
	}
}

func TestEventsTool_WithLimit(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{}).
		WithProcessEvents([]platform.ProcessEvent{}).
		WithAppVersionEvents([]platform.AppVersionEvent{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEvents(srv, mock, nil, "proj-1")

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
	RegisterEvents(srv, mock, nil, "proj-1")

	result := callTool(t, srv, "zerops_events", nil)

	if !result.IsError {
		t.Error("expected IsError for API error")
	}
}
