// Tests for: tools/deploy.go — zerops_deploy MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

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
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

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
	if !parsed.SSHReady {
		t.Error("expected SSHReady=true after successful deploy with SSH deployer")
	}
}

func TestDeployTool_SelfDeploy_TargetOnly(t *testing.T) {
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
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

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
	if parsed.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", parsed.Mode)
	}
	if parsed.SourceService != "app" {
		t.Errorf("sourceService = %s, want app (auto-inferred)", parsed.SourceService)
	}
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if !parsed.SSHReady {
		t.Error("expected SSHReady=true after successful self-deploy with SSH deployer")
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
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

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
	if !strings.Contains(parsed.Suggestion, "build logs unavailable") {
		t.Errorf("suggestion should mention 'build logs unavailable' (no logFetcher), got: %s", parsed.Suggestion)
	}
	if len(parsed.BuildLogs) != 0 {
		t.Errorf("expected empty buildLogs without logFetcher, got %d entries", len(parsed.BuildLogs))
	}
}

func TestDeployTool_BuildFailed_WithBuildLogs(t *testing.T) {
	t.Parallel()

	buildSvcID := "build-svc-99"
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
				Build: &platform.BuildInfo{
					ServiceStackID: &buildSvcID,
				},
			},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Message: "npm error code ERESOLVE"},
		{Message: "Build command failed with exit code 1"},
	})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), logFetcher)

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
	if len(parsed.BuildLogs) != 2 {
		t.Fatalf("expected 2 build log lines, got %d", len(parsed.BuildLogs))
	}
	if parsed.BuildLogs[0] != "npm error code ERESOLVE" {
		t.Errorf("buildLogs[0] = %q, want %q", parsed.BuildLogs[0], "npm error code ERESOLVE")
	}
	if parsed.BuildLogsSource != buildContainerSource {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, buildContainerSource)
	}
	if !strings.Contains(parsed.Suggestion, "buildLogs") {
		t.Errorf("suggestion should mention buildLogs, got: %s", parsed.Suggestion)
	}
}

type stubSSHWithReadiness struct {
	deployOutput []byte
	deployErr    error
	readyErr     error
}

func (s *stubSSHWithReadiness) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	if command == "true" {
		return nil, s.readyErr
	}
	return s.deployOutput, s.deployErr
}

func TestDeployTool_SSHReadinessTimeout(t *testing.T) {
	restore := ops.OverrideSSHReadyConfigForTest(time.Millisecond, 10*time.Millisecond)
	defer restore()

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
	ssh := &stubSSHWithReadiness{
		deployOutput: []byte("ok"),
		readyErr:     fmt.Errorf("connection refused"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

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
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if parsed.SSHReady {
		t.Error("expected SSHReady=false when SSH readiness times out")
	}
	if len(parsed.Warnings) == 0 {
		t.Error("expected warning about SSH readiness timeout")
	}
	foundWarning := false
	for _, w := range parsed.Warnings {
		if strings.Contains(w, "SSH not ready") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected warning containing 'SSH not ready', got: %v", parsed.Warnings)
	}
}

func TestDeployTool_SelfDeploy_DevAwareResponse(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-1",
				Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.TargetServiceType != "nodejs@22" {
		t.Errorf("targetServiceType = %q, want %q", parsed.TargetServiceType, "nodejs@22")
	}
	if !strings.Contains(parsed.NextActions, "NOT running") {
		t.Errorf("nextActions should warn about server NOT running for self-deploy dynamic runtime, got: %s", parsed.NextActions)
	}
	if !strings.Contains(parsed.Message, "NOT running") {
		t.Errorf("message should indicate server NOT running for self-deploy dynamic runtime, got: %s", parsed.Message)
	}
}

func TestDeployTool_CrossDeploy_StandardResponse(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-1",
				Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
			{
				ID:   "svc-2",
				Name: "appstage",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "appdev",
		"targetService": "appstage",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.TargetServiceType != "nodejs@22" {
		t.Errorf("targetServiceType = %q, want %q", parsed.TargetServiceType, "nodejs@22")
	}
	if strings.Contains(parsed.NextActions, "NOT running") {
		t.Errorf("cross-deploy nextActions should NOT warn about server, got: %s", parsed.NextActions)
	}
}

func TestDeployTool_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &stubSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

	// targetService is required — SDK validates and returns error for missing field.
	err := callToolMayError(t, srv, "zerops_deploy", map[string]any{})
	if err == nil {
		t.Error("expected error for missing required targetService")
	}
}

func TestDeployTool_SSHMode_Exit255PollsSuccessfully(t *testing.T) {
	restore := ops.OverrideSSHReadyConfigForTest(time.Millisecond, 10*time.Millisecond)
	defer restore()

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
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

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
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})

	if !result.IsError {
		t.Error("expected IsError for SSH failure")
	}
}

func TestDeployTool_PreparingRuntimeFailed(t *testing.T) {
	t.Parallel()

	buildSvcID := "build-svc-99"
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{
				ID:             "av-1",
				ProjectID:      "proj-1",
				ServiceStackID: "svc-1",
				Status:         "PREPARING_RUNTIME_FAILED",
				Sequence:       1,
				Build: &platform.BuildInfo{
					ServiceStackID: &buildSvcID,
				},
			},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Message: "prepare command failed"},
	})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), logFetcher)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("unexpected IsError — failed deploy is a valid response: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != "PREPARING_RUNTIME_FAILED" {
		t.Errorf("status = %s, want PREPARING_RUNTIME_FAILED", parsed.Status)
	}
	if parsed.BuildStatus != "PREPARING_RUNTIME_FAILED" {
		t.Errorf("buildStatus = %s, want PREPARING_RUNTIME_FAILED", parsed.BuildStatus)
	}
	if len(parsed.BuildLogs) == 0 {
		t.Error("expected buildLogs to be populated for PREPARING_RUNTIME_FAILED")
	}
	if parsed.BuildLogsSource != buildContainerSource {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, buildContainerSource)
	}
	if parsed.Suggestion == "" {
		t.Error("expected non-empty suggestion")
	}
	if !strings.Contains(parsed.Suggestion, "PREPARING_RUNTIME_FAILED") {
		t.Errorf("suggestion should mention the status, got: %s", parsed.Suggestion)
	}
}

func TestDeployTool_UnknownBuildStatus_TreatedAsFailure(t *testing.T) {
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
				Status:         "SOME_FUTURE_STATUS",
				Sequence:       1,
			},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, testEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("unexpected IsError — unknown build status is a valid response: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != "SOME_FUTURE_STATUS" {
		t.Errorf("status = %s, want SOME_FUTURE_STATUS", parsed.Status)
	}
	if parsed.Suggestion == "" {
		t.Error("expected non-empty suggestion for unknown status")
	}
	if !strings.Contains(parsed.Suggestion, "SOME_FUTURE_STATUS") {
		t.Errorf("suggestion should mention the status, got: %s", parsed.Suggestion)
	}
}

func TestDeployTool_NoWorkflowSession_Blocked(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	ssh := &stubSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	engine := workflow.NewEngine(t.TempDir())

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, engine, nil)

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
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	dir := t.TempDir()
	engine := workflow.NewEngine(dir)
	if _, err := engine.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploy(srv, mock, "proj-1", ssh, authInfo, engine, nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "app"})
	if result.IsError {
		t.Errorf("unexpected IsError with active session: %s", getTextContent(t, result))
	}
}
