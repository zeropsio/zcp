// Tests for: import.go — zerops_import MCP tool handler.

package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestImportTool_Content(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: "RUNNING"},
				}},
			},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", nil)

	yaml := "services:\n  - hostname: api\n    type: nodejs@20\n"
	result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestImportTool_MissingContentAndFile(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_import", nil)

	if !result.IsError {
		t.Error("expected IsError when no content or filePath is provided")
	}
}
