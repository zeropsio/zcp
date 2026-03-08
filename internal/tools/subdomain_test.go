// Tests for: subdomain.go — zerops_subdomain MCP tool handler.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestSubdomainTool_EnableReturnsUrls(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(sr.SubdomainUrls) != 1 {
		t.Fatalf("expected 1 subdomain URL, got %d: %v", len(sr.SubdomainUrls), sr.SubdomainUrls)
	}
	want := "https://app-abc1-3000.prg1.zerops.app"
	if sr.SubdomainUrls[0] != want {
		t.Errorf("SubdomainUrls[0] = %q, want %q", sr.SubdomainUrls[0], want)
	}
}

func TestSubdomainTool_EnableReturnsUrls_BarePrefix(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1", // bare prefix — no domain suffix
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "env-1", Key: "zeropsSubdomain", Content: "https://app-abc1-3000.prg1.zerops.app"},
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(sr.SubdomainUrls) != 1 {
		t.Fatalf("expected 1 subdomain URL, got %d: %v", len(sr.SubdomainUrls), sr.SubdomainUrls)
	}
	want := "https://app-abc1-3000.prg1.zerops.app"
	if sr.SubdomainUrls[0] != want {
		t.Errorf("SubdomainUrls[0] = %q, want %q", sr.SubdomainUrls[0], want)
	}
}

func TestSubdomainTool_Enable_PollsProcess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

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
	if sr.Process == nil {
		t.Fatal("expected process in result")
	}
	if sr.Process.Status != statusFinished {
		t.Errorf("process status = %q, want %q (tool must poll to completion)", sr.Process.Status, statusFinished)
	}
}

func TestSubdomainTool_Disable_PollsProcess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-disable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "disable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Process == nil {
		t.Fatal("expected process in result")
	}
	if sr.Process.Status != statusFinished {
		t.Errorf("process status = %q, want %q (tool must poll to completion)", sr.Process.Status, statusFinished)
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

func TestSubdomainTool_Enable_FailedProcess_TreatedAsAlreadyEnabled(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFailed, // API sometimes returns FAILED instead of error
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Status != "already_enabled" {
		t.Errorf("status = %q, want %q", sr.Status, "already_enabled")
	}
	if sr.Process != nil {
		t.Error("process should be nil for already_enabled")
	}
	if len(sr.SubdomainUrls) == 0 {
		t.Error("expected subdomainUrls to be populated")
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
