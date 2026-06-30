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
			ActionName: "stack.start",
			Status:     serviceStatusRunning,
			Created:    "2025-01-01T00:00:00Z",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var pr ops.ProcessStatusResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &pr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if pr.Status != serviceStatusRunning {
		t.Errorf("status = %q, want %q", pr.Status, serviceStatusRunning)
	}
}

func TestProcessTool_Cancel(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProcess(&platform.Process{
			ID:         "proc-1",
			ActionName: "stack.start",
			Status:     serviceStatusRunning,
			Created:    "2025-01-01T00:00:00Z",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

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
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": ""})

	if !result.IsError {
		t.Error("expected IsError for missing process ID")
	}
}

func TestProcessTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1", "action": "explode"})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}

// TestProcessTool_WaitProcess wires action="wait" with a single processId
// through the handler: the process is terminal, so the wait settles and reports
// its final status.
func TestProcessTool_WaitProcess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProcess(&platform.Process{ID: "proc-1", ActionName: "stack.build", Status: platform.ProcessStatusFinished})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"processId": "proc-1", "action": "wait"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var wr ops.WaitResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wr); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !wr.Settled || len(wr.Processes) != 1 || wr.Processes[0].Status != platform.ProcessStatusFinished {
		t.Errorf("want settled with one FINISHED process; got %+v", wr)
	}
}

// TestProcessTool_WaitService wires action="wait" service=<host> through the
// handler: a busy service drains to settled across activity rounds.
func TestProcessTool_WaitService(t *testing.T) {
	t.Parallel()
	const svcID = "svc1"
	buildRunning := []platform.Process{{
		ID: "build-1", ActionName: "stack.build", Status: platform.ProcessStatusRunning,
		ServiceStacks: []platform.ServiceStackRef{{ID: svcID, Name: "appdev"}},
		Created:       "2026-06-30T10:00:00Z",
		AppVersion:    &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding},
	}}
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: svcID, Name: "appdev"}}).
		WithProjectProcessesSequence([][]platform.Process{buildRunning, {}}).
		WithProcess(&platform.Process{ID: "build-1", ActionName: "stack.build", Status: platform.ProcessStatusRunning}).
		WithProcessScenario("build-1", platform.ProcessScenario{InitialStatus: platform.ProcessStatusRunning, Transitions: []platform.ProcessTransition{{AtCall: 1, Status: platform.ProcessStatusFinished}}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"service": "appdev", "action": "wait"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var wr ops.WaitResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wr); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !wr.Settled {
		t.Errorf("want settled service-wait; got %+v", wr)
	}
}

// TestProcessTool_WaitUnknownService surfaces an INVALID_PARAMETER for a
// hostname not in the project.
func TestProcessTool_WaitUnknownService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: "svc1", Name: "appdev"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterProcess(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_process", map[string]any{"service": "ghost", "action": "wait"})
	if !result.IsError {
		t.Error("expected IsError for unknown service hostname")
	}
}
