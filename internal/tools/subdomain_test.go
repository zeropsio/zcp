// Tests for: subdomain.go — zerops_subdomain MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestSubdomainTool_Enable(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Action != "enable" {
		t.Errorf("action = %q, want %q", sr.Action, "enable")
	}
}

func TestSubdomainTool_Disable(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "disable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestSubdomainTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "toggle",
	})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}

func TestSubdomainTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "", "action": "enable",
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}

func TestSubdomainTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "",
	})

	if !result.IsError {
		t.Error("expected IsError for empty action")
	}
}
