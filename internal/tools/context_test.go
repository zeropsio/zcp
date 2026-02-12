// Tests for: context.go — zerops_context MCP tool handler.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestContextTool_StaticOnly(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv, nil, nil)

	result := callTool(t, srv, "zerops_context", nil)

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if text == "" {
		t.Error("expected non-empty context text")
	}
	if !strings.Contains(text, "Zerops") {
		t.Errorf("expected context to mention Zerops, got: %s", text[:min(100, len(text))])
	}
}

func TestContextTool_WithDynamicStacks(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
			},
		},
	})
	cache := ops.NewStackTypeCache(0)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv, mock, cache)

	result := callTool(t, srv, "zerops_context", nil)

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Service Stacks (live)") {
		t.Error("expected dynamic service stacks section")
	}
	if !strings.Contains(text, "nodejs@22") {
		t.Error("expected nodejs@22 in dynamic section")
	}
}

func TestContextTool_APIErrorGraceful(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("ListServiceStackTypes", &platform.PlatformError{
		Code:    "API_ERROR",
		Message: "service unavailable",
	})
	cache := ops.NewStackTypeCache(0)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv, mock, cache)

	result := callTool(t, srv, "zerops_context", nil)

	if result.IsError {
		t.Error("API error should not make tool return error")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Zerops") {
		t.Error("should still return static context on API error")
	}
}
