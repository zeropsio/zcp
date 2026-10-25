// Tests for: manage.go — zerops_manage MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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
	if _, ok := parsed["nextActions"]; !ok {
		t.Error("expected nextActions in response")
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

// TestManageTool_Box_RestartStop_WarnSandboxesLoseMemory — a Zerops Box is
// one VM: a restart or a stop kills its running sandboxes, which come back
// PARKED without their memory, while a reload restarts only the orchestrator
// and keeps them (zerops-docs zbox/workspace-service.mdx, Service lifecycle).
// A restart or stop of a Box says so and points at the reload; a reload and
// every other service's restart keep their plain next action
// (spec-workflows.md §8 PS-8).
func TestManageTool_Box_RestartStop_WarnSandboxesLoseMemory(t *testing.T) {
	t.Parallel()
	box := platform.ServiceStack{ID: "svc-box", Name: "box", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"}}
	api := platform.ServiceStack{ID: "svc-api", Name: "api", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}
	tests := []struct {
		name     string
		action   string
		host     string
		wantWarn bool
	}{
		{"restart of a Box warns", "restart", "box", true},
		{"stop of a Box warns", "stop", "box", true},
		{"reload of a Box keeps the plain next action", "reload", "box", false},
		{"restart of a runtime keeps the plain next action", "restart", "api", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{box, api}).
				WithProcess(&platform.Process{ID: "proc-" + tt.action + "-svc-" + tt.host, Status: statusFinished})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterManage(srv, mock, "proj-1")

			result := callTool(t, srv, "zerops_manage", map[string]any{"action": tt.action, "serviceHostname": tt.host})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("unexpected IsError: %s", text)
			}
			var parsed struct {
				NextActions string `json:"nextActions"`
			}
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				t.Fatalf("parse: %v", err)
			}
			warns := []string{"Zerops Box", "PARKED", "without their memory", "park", `zerops_manage action=reload serviceHostname="box"`, "keeps"}
			for _, want := range warns {
				if got := strings.Contains(parsed.NextActions, want); got != tt.wantWarn {
					t.Errorf("nextActions contains %q = %v, want %v: %s", want, got, tt.wantWarn, parsed.NextActions)
				}
			}
		})
	}
}

// TestManageTool_DescriptionKeepsTheReloadRules: the description says what
// reload is for and what it keeps — a runtime its boot env, PHP-FPM its boot
// config — beside a Zerops Box's opposite rule, for every user.
func TestManageTool_DescriptionKeepsTheReloadRules(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterManage(srv, platform.NewMock(), "proj-1")
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	listed, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("list tools: %v (%d)", err, len(listed.Tools))
	}
	tool := listed.Tools[0]
	for _, want := range []string{"restart, NOT reload", "PHP-FPM keeps its boot config", "reload (~4s) is for graceful config/code reloads",
		"A Zerops Box is the opposite"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("zerops_manage's description lacks %q: %s", want, tool.Description)
		}
	}
}
