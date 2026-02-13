// Tests for: context.go — zerops_context MCP tool handler.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/update"
)

func TestContextTool_StaticOnly(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterContext(srv, nil, nil, nil)

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
	RegisterContext(srv, mock, cache, nil)

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
	RegisterContext(srv, mock, cache, nil)

	result := callTool(t, srv, "zerops_context", nil)

	if result.IsError {
		t.Error("API error should not make tool return error")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Zerops") {
		t.Error("should still return static context on API error")
	}
}

func TestContextTool_UpdateNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		updateInfo *update.Info
		wantNote   bool
	}{
		{
			name:       "nil update info",
			updateInfo: nil,
			wantNote:   false,
		},
		{
			name:       "no update available",
			updateInfo: &update.Info{Available: false},
			wantNote:   false,
		},
		{
			name: "update available",
			updateInfo: &update.Info{
				Available:      true,
				CurrentVersion: "0.1.0",
				LatestVersion:  "v0.2.0",
			},
			wantNote: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterContext(srv, nil, nil, tt.updateInfo)

			result := callTool(t, srv, "zerops_context", nil)

			text := getTextContent(t, result)
			hasNote := strings.Contains(text, "ZCP update available")
			if hasNote != tt.wantNote {
				t.Errorf("update notification present = %v, want %v", hasNote, tt.wantNote)
			}
			if tt.wantNote {
				if !strings.Contains(text, "0.1.0") || !strings.Contains(text, "v0.2.0") {
					t.Error("update notification should contain version numbers")
				}
			}
		})
	}
}
