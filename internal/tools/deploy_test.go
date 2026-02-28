// Tests for: tools/deploy.go — zerops_deploy MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
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
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{
				ID:             "av-1",
				ProjectID:      "proj-1",
				ServiceStackID: "svc-2",
				Status:         statusActive,
				Sequence:       1,
			},
		})
	ssh := &stubSSH{output: []byte("ok")}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

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
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if parsed.BuildStatus != statusActive {
		t.Errorf("buildStatus = %s, want ACTIVE", parsed.BuildStatus)
	}
	if parsed.MonitorHint != "" {
		t.Errorf("monitorHint should be empty after successful deploy, got %q", parsed.MonitorHint)
	}
}

func TestDeployTool_LocalMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{
				ID:             "av-1",
				ProjectID:      "proj-1",
				ServiceStackID: "svc-1",
				Status:         statusActive,
				Sequence:       1,
			},
		})
	ssh := &stubSSH{}
	local := &stubLocal{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

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
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if parsed.BuildStatus != statusActive {
		t.Errorf("buildStatus = %s, want ACTIVE", parsed.BuildStatus)
	}
	if parsed.MonitorHint != "" {
		t.Errorf("monitorHint should be empty after successful deploy, got %q", parsed.MonitorHint)
	}
}

func TestDeployTool_BuildFailed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{
				ID:             "av-1",
				ProjectID:      "proj-1",
				ServiceStackID: "svc-1",
				Status:         statusBuildFailed,
				Sequence:       1,
			},
		})
	ssh := &stubSSH{}
	local := &stubLocal{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

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
	if parsed.Status != statusBuildFailed {
		t.Errorf("status = %s, want BUILD_FAILED", parsed.Status)
	}
	if parsed.BuildStatus != statusBuildFailed {
		t.Errorf("buildStatus = %s, want BUILD_FAILED", parsed.BuildStatus)
	}
	if parsed.Suggestion == "" {
		t.Error("expected non-empty suggestion for build failure")
	}
}

func TestDeployTool_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &stubSSH{}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{})

	if !result.IsError {
		t.Error("expected IsError for no params")
	}
}

func TestDeployTool_SSHMode_Exit255PollsSuccessfully(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{
				ID:             "av-1",
				ProjectID:      "proj-1",
				ServiceStackID: "svc-2",
				Status:         statusActive,
				Sequence:       1,
			},
		})
	// SSH returns exit 255 but output indicates build was triggered.
	ssh := &stubSSH{
		output: []byte("BUILD ARTEFACTS READY TO DEPLOY\nConnection closed.\n"),
		err:    fmt.Errorf("ssh builder: process exited with status 255"),
	}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

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
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if parsed.BuildStatus != statusActive {
		t.Errorf("buildStatus = %s, want ACTIVE", parsed.BuildStatus)
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
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})

	if !result.IsError {
		t.Error("expected IsError for SSH failure")
	}
}

func TestDeployTool_NoWorkflowSession_Blocked(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	ssh := &stubSSH{}
	local := &stubLocal{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	engine := workflow.NewEngine(t.TempDir())

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, engine)

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "app"})
	if !result.IsError {
		t.Error("expected IsError when no workflow session")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "WORKFLOW_REQUIRED") {
		t.Errorf("expected WORKFLOW_REQUIRED, got: %s", text)
	}
}

func TestDeployTool_WithWorkflowSession_Succeeds(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{}
	local := &stubLocal{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	dir := t.TempDir()
	engine := workflow.NewEngine(dir)
	if _, err := engine.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, local, authInfo, engine)

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "app"})
	if result.IsError {
		t.Errorf("unexpected IsError with active session: %s", getTextContent(t, result))
	}
}
