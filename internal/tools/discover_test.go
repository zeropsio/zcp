// Tests for: discover.go — zerops_discover MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestDiscoverTool_Basic(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@20"}},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDiscover(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_discover", nil)

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &dr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if dr.Project.Name != "myproject" {
		t.Errorf("project name = %q, want %q", dr.Project.Name, "myproject")
	}
	if len(dr.Services) != 1 {
		t.Fatalf("services count = %d, want 1", len(dr.Services))
	}
}

func TestDiscoverTool_WithService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@20"}},
			{ID: "svc-2", Name: "db", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDiscover(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_discover", map[string]any{"service": "api"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &dr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(dr.Services) != 1 {
		t.Fatalf("services count = %d, want 1", len(dr.Services))
	}
	if dr.Services[0].Hostname != "api" {
		t.Errorf("hostname = %q, want %q", dr.Services[0].Hostname, "api")
	}
}

func TestDiscoverTool_WithEnvs(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@20"}},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{{Key: "PORT", Content: "3000"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDiscover(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_discover", map[string]any{"service": "api", "includeEnvs": true})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &dr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(dr.Services[0].Envs) == 0 {
		t.Error("expected envs to be populated")
	}
}

func TestDiscoverTool_ServiceNotFound(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDiscover(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_discover", map[string]any{"service": "nonexistent"})

	if !result.IsError {
		t.Error("expected IsError for nonexistent service")
	}
}

func TestDiscoverTool_Error(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithError("GetProject", platform.NewPlatformError(platform.ErrAPIError, "API error", ""))

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDiscover(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_discover", nil)

	if !result.IsError {
		t.Error("expected IsError for API error")
	}
}
