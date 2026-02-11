// Tests for: tools/deploy.go — zerops_deploy MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

type stubSSH struct {
	output []byte
	err    error
}

func (s *stubSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	return s.output, s.err
}

type stubLocal struct {
	output []byte
	err    error
}

func (s *stubLocal) ExecZcli(_ context.Context, _ ...string) ([]byte, error) {
	return s.output, s.err
}

func TestDeployTool_SSHMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &stubSSH{output: []byte("ok")}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", parsed.Mode)
	}
}

func TestDeployTool_LocalMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &stubSSH{}
	local := &stubLocal{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Mode != "local" {
		t.Errorf("mode = %s, want local", parsed.Mode)
	}
}

func TestDeployTool_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &stubSSH{}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo)

	result := callTool(t, srv, "zerops_deploy", map[string]any{})

	if !result.IsError {
		t.Error("expected IsError for no params")
	}
}

func TestDeployTool_Error(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &stubSSH{err: fmt.Errorf("ssh failed")}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})

	if !result.IsError {
		t.Error("expected IsError for SSH failure")
	}
}
