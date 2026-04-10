// Tests for: env.go — zerops_env MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestEnvTool_GetAction_Success is the new happy path for `get` — the
// action is now first-class, delegating to the same discover path that
// zerops_discover uses. Before the v7 post-mortem fix, agents tried
// `get` five times in a row because it's the natural action name for
// "read env vars"; exposing it here eliminates that whole failure mode.
func TestEnvTool_GetAction_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{Key: "hostname", Content: "db"},
			{Key: "port", Content: "5432"},
			{Key: "user", Content: "dbuser"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "db",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	services, ok := parsed["services"].([]any)
	if !ok || len(services) == 0 {
		t.Fatalf("expected services[] in result, got: %v", parsed)
	}
	first, _ := services[0].(map[string]any)
	envs, ok := first["envs"].([]any)
	if !ok {
		t.Fatalf("expected envs[] on first service, got: %v", first)
	}
	if len(envs) == 0 {
		t.Error("get returned zero env vars — expected hostname/port/user")
	}
}

// TestEnvTool_GetAction_RequiresTarget verifies the handler's own
// guard: get without serviceHostname AND without project=true is a
// user error that must come back with an actionable suggestion. This
// is the one get-action failure that cannot be caught at the schema
// layer (both fields are optional — one or the other has to be set).
func TestEnvTool_GetAction_RequiresTarget(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{"action": "get"})

	if !result.IsError {
		t.Fatal("expected IsError when get has neither serviceHostname nor project=true")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "serviceHostname") {
		t.Errorf("error should name the missing parameter, got: %s", text)
	}
	if !strings.Contains(text, "zerops_discover") {
		t.Errorf("error should point at zerops_discover for bulk reads, got: %s", text)
	}
}

// TestEnvTool_GetAction_StringifiedBoolProject verifies the same
// FlexBool acceptance we tested for discover: an agent sending
// `project: "true"` (stringified) must succeed, not bounce off the
// schema. This is the direct regression for LOG.txt line 65 where a
// stringified `project` argument failed with a non-actionable MCP
// schema error.
func TestEnvTool_GetAction_StringifiedBoolProject(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":  "get",
		"project": "true",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError with stringified project=\"true\": %s", getTextContent(t, result))
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
	RegisterEnv(srv, mock, "proj-1", "")

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
	RegisterEnv(srv, mock, "proj-1", "")

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

// TestEnvTool_EmptyAction: the schema enum rejects an empty action
// at the protocol layer before it reaches the handler. This is the
// preferred form of early-exit for a required field — the agent
// sees a crisp "enum: does not equal any of [get set delete
// generate-dotenv]" error with the valid set included.
func TestEnvTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	err := callToolMayError(t, srv, "zerops_env", map[string]any{
		"action": "", "serviceHostname": "api",
	})
	if err == nil {
		t.Fatal("expected schema rejection for empty action")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("error should reference the action field, got: %v", err)
	}
}

// TestEnvTool_InvalidAction: same mechanism as EmptyAction — the
// enum-based schema rejects unknown actions and includes the full
// valid-action list in the error. `wipe` is the standin for any
// bogus action an agent might try (e.g. the old "get" attempt from
// LOG.txt is now a valid action, so we cannot use it here).
func TestEnvTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	err := callToolMayError(t, srv, "zerops_env", map[string]any{
		"action": "wipe", "serviceHostname": "api",
	})
	if err == nil {
		t.Fatal("expected schema rejection for invalid action 'wipe'")
	}
	msg := err.Error()
	if !strings.Contains(msg, "wipe") {
		t.Errorf("error should echo the bad value, got: %v", err)
	}
	// The enum list must be in the error so the agent can recover
	// without a second lookup.
	for _, wanted := range []string{"get", "set", "delete", "generate-dotenv"} {
		if !strings.Contains(msg, wanted) {
			t.Errorf("error should list valid action %q, got: %v", wanted, err)
		}
	}
}
