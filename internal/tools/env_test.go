// Tests for: env.go — zerops_env MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestEnvTool_Get(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithServiceEnv("svc-1", []platform.EnvVar{{Key: "PORT", Content: "3000"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EnvGetResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if er.Scope != "service" {
		t.Errorf("scope = %q, want %q", er.Scope, "service")
	}
}

func TestEnvTool_Set(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

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
}

func TestEnvTool_Delete(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithServiceEnv("svc-1", []platform.EnvVar{{ID: "env-1", Key: "OLD_VAR", Content: "old"}})

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
}

func TestEnvTool_ProjectScope(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProjectEnv([]platform.EnvVar{{Key: "APP_ENV", Content: "prod"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "project": true,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var er ops.EnvGetResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &er); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if er.Scope != "project" {
		t.Errorf("scope = %q, want %q", er.Scope, "project")
	}
}

func TestEnvTool_MissingScope(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_env", map[string]any{"action": "get"})

	if !result.IsError {
		t.Error("expected IsError when neither serviceHostname nor project is set")
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
}
