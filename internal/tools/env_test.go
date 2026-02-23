// Tests for: env.go — zerops_env MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestEnvTool_GetAction_ReturnsError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithServiceEnv("svc-1", []platform.EnvVar{{Key: "PORT", Content: "3000"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "api",
	})

	if !result.IsError {
		t.Fatal("expected IsError for get action")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "zerops_discover") {
		t.Errorf("error should mention zerops_discover, got: %s", text)
	}
	if !strings.Contains(text, "includeEnvs") {
		t.Errorf("error should mention includeEnvs, got: %s", text)
	}
}

func TestEnvTool_Set_PollsToFinished(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-envset-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":          "set",
		"serviceHostname": "api",
		"variables":       []any{"PORT=8080"},
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	proc, ok := parsed["process"].(map[string]any)
	if !ok {
		t.Fatalf("expected process in result, got: %v", parsed)
	}
	if proc["status"] != statusFinished {
		t.Errorf("process status = %v, want FINISHED", proc["status"])
	}
}

func TestEnvTool_Delete_PollsToFinished(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithServiceEnv("svc-1", []platform.EnvVar{{ID: "env-1", Key: "OLD_VAR", Content: "old"}}).
		WithProcess(&platform.Process{
			ID:     "proc-envdel-env-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":          "delete",
		"serviceHostname": "api",
		"variables":       []any{"OLD_VAR"},
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	proc, ok := parsed["process"].(map[string]any)
	if !ok {
		t.Fatalf("expected process in result, got: %v", parsed)
	}
	if proc["status"] != statusFinished {
		t.Errorf("process status = %v, want FINISHED", proc["status"])
	}
}

func TestEnvTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "", "serviceHostname": "api",
	})

	if !result.IsError {
		t.Error("expected IsError for empty action")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "set or delete") {
		t.Errorf("error should suggest 'set or delete', got: %s", text)
	}
}

func TestEnvTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "wipe", "serviceHostname": "api",
	})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "set or delete") {
		t.Errorf("error should suggest 'set or delete', got: %s", text)
	}
}
