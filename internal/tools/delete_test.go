// Tests for: delete.go — zerops_delete MCP tool handler.

package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeleteTool_Confirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirm": true,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestDeleteTool_NotConfirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api", "confirm": false,
	})

	if !result.IsError {
		t.Error("expected IsError when confirm=false")
	}
}

func TestDeleteTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "", "confirm": true,
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}
