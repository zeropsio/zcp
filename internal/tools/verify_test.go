// Tests for: zerops_verify — MCP tool handler.
package tools

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestVerifyTool_RuntimeHealthy(t *testing.T) {
	t.Parallel()

	// No log access configured → log checks skip (not fail).
	// No subdomain → HTTP checks skip. Service running → pass.
	// All pass/skip → healthy.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
	// service_running=pass, log checks=skip (no log access), HTTP=skip (no subdomain) → healthy.
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", vr.Status)
	}
}

func TestVerifyTool_ManagedHealthy(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "db"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "managed" {
		t.Errorf("Type = %q, want managed", vr.Type)
	}
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", vr.Status)
	}
	if len(vr.Checks) != 1 {
		t.Errorf("Checks count = %d, want 1", len(vr.Checks))
	}
}

func TestVerifyTool_RuntimeActive(t *testing.T) {
	t.Parallel()

	// ACTIVE is the real status returned by Zerops API for running services.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusActive},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy (ACTIVE should be accepted)", vr.Status)
	}
	// service_running must pass for ACTIVE status.
	for _, c := range vr.Checks {
		if c.Name == "service_running" && c.Status != "pass" {
			t.Errorf("service_running = %q, want pass for ACTIVE status", c.Status)
		}
	}
}

func TestVerifyTool_NotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "nonexistent"})

	if !result.IsError {
		t.Error("expected IsError for nonexistent service")
	}
}

func TestVerifyTool_GracefulLogError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning},
		}).
		WithError("GetProjectLog", fmt.Errorf("log backend down"))
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s — log errors should be graceful", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	// Log checks should be skip, not fail or crash.
	for _, c := range vr.Checks {
		if c.Name == "error_logs" || c.Name == "startup_detected" {
			if c.Status != "skip" {
				t.Errorf("Check %q: status = %q, want skip", c.Name, c.Status)
			}
		}
	}
}

func TestVerifyTool_BatchMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	// Call with empty serviceHostname → batch mode.
	result := callTool(t, srv, "zerops_verify", map[string]any{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyAllResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(vr.Services) != 2 {
		t.Errorf("Services count = %d, want 2", len(vr.Services))
	}
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", vr.Status)
	}
}

func TestVerifyTool_SingleMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1")

	// Call with serviceHostname → single mode, returns VerifyResult.
	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Hostname != "app" {
		t.Errorf("Hostname = %q, want app", vr.Hostname)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
}
