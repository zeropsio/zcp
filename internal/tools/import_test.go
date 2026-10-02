// Tests for: import.go — zerops_import MCP tool handler.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestImportTool_Content(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{
			ID:         "p-1",
			ActionName: "serviceStackImport",
			Status:     statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@20\n"
	result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.ImportResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Summary == "" {
		t.Error("expected non-empty summary after polling")
	}
	if len(parsed.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(parsed.Processes))
	}
	if parsed.Processes[0].Status != statusFinished {
		t.Errorf("process status = %s, want FINISHED", parsed.Processes[0].Status)
	}
}

func TestImportTool_MissingContentAndFile(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_import", nil)

	if !result.IsError {
		t.Error("expected IsError when no content or filePath is provided")
	}
}

func TestImportTool_PollMultipleSuccess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
				{ID: "svc-2", Name: "db", Processes: []platform.Process{
					{ID: "p-2", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
			},
		}).
		WithProcess(&platform.Process{
			ID:     "p-1",
			Status: statusFinished,
		}).
		WithProcess(&platform.Process{
			ID:     "p-2",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@20\n  - hostname: db\n    type: postgresql@16\n"
	result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.ImportResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Summary != "All 2 processes completed successfully" {
		t.Errorf("summary = %q, want 'All 2 processes completed successfully'", parsed.Summary)
	}
	for i, p := range parsed.Processes {
		if p.Status != statusFinished {
			t.Errorf("process[%d] status = %s, want FINISHED", i, p.Status)
		}
	}
}

func TestImportTool_PollPartialFailure(t *testing.T) {
	t.Parallel()
	failReason := "service type not found"
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
				{ID: "svc-2", Name: "db", Processes: []platform.Process{
					{ID: "p-2", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
			},
		}).
		WithProcess(&platform.Process{
			ID:     "p-1",
			Status: statusFinished,
		}).
		WithProcess(&platform.Process{
			ID:         "p-2",
			Status:     statusFailed,
			FailReason: &failReason,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@20\n  - hostname: db\n    type: postgresql@16\n"
	result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.ImportResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Summary != "1/2 processes completed, 1 failed" {
		t.Errorf("summary = %q, want '1/2 processes completed, 1 failed'", parsed.Summary)
	}
	if parsed.Processes[1].Status != statusFailed {
		t.Errorf("process[1] status = %s, want FAILED", parsed.Processes[1].Status)
	}
	if parsed.Processes[1].FailReason == nil || *parsed.Processes[1].FailReason != failReason {
		t.Errorf("process[1] failReason = %v, want %q", parsed.Processes[1].FailReason, failReason)
	}
}

func TestImportTool_NoWorkflowSession_Blocked(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", engine, stateDir, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_import", map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@20\n"})
	if !result.IsError {
		t.Error("expected IsError when no workflow context")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "WORKFLOW_REQUIRED") {
		t.Errorf("expected WORKFLOW_REQUIRED, got: %s", text)
	}
}

func TestImportTool_WithWorkflowSession_Succeeds(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", Status: statusFinished})

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", engine, dir, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_import", map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@20\n"})
	if result.IsError {
		t.Errorf("unexpected IsError with active session: %s", getTextContent(t, result))
	}
}

func TestImportTool_WithWorkSession_Succeeds(t *testing.T) {
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", Status: statusFinished})

	stateDir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", "container", "test", []string{"appdev"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("save work session: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", nil, stateDir, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_import", map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@20\n"})
	if result.IsError {
		t.Errorf("unexpected IsError with develop marker: %s", getTextContent(t, result))
	}
}

// TestImportTool_RefusesAnOpenMate: in a Mate made by the new press (its zcp
// carries MATE_SETUP_RUNTIMES) whose project is not closed off yet, runtimes
// would read the zcp service's variables (the Mate's key among them), and
// closing it off after they exist restarts them — so the import refuses,
// naming what closes it. A Mate closed off, an older Mate (no plan), a
// project that does not say, and a container that is no Mate import as
// before.
func TestImportTool_RefusesAnOpenMate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mate   bool
		plan   bool
		closed bool
		askErr error  // HQ not answering
		want   string // what the refusal says; "" imports
	}{
		{"an open new-flow Mate", true, true, false, nil, "not closed off yet; Finish setup"},
		{"a new-flow Mate closed off", true, true, true, nil, ""},
		{"a new-flow Mate HQ cannot be asked about", true, true, false, errors.New("unreachable"), "Could not ask HQ"},
		{"an open Mate made before the new press", true, false, false, nil, ""},
		{"an open project outside a Mate", false, true, false, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithImportResult(&platform.ImportResult{ProjectID: "proj-1", ServiceStacks: []platform.ImportedServiceStack{{ID: "svc-1", Name: "api"}}})
			env := map[string]string{"PATH": "/usr/bin"}
			if tt.plan {
				env["MATE_SETUP_RUNTIMES"] = "c2VydmljZXM6IFtd"
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			closedOff := func(context.Context) (bool, error) { return tt.closed, tt.askErr }
			registerImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{MateEnabled: tt.mate}, writeLiveEnvFile(t, env), closedOff)
			result := callTool(t, srv, "zerops_import", map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@20\n"})
			text := getTextContent(t, result)
			if tt.want == "" {
				if result.IsError {
					t.Errorf("refused: %s", text)
				}
				return
			}
			if !result.IsError || !strings.Contains(text, tt.want) {
				t.Errorf("want a refusal saying %q, got: %s", tt.want, text)
			}
		})
	}
}
