// Tests for: tools/deploy_ssh.go — zerops_deploy SSH mode MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type stubSSH struct {
	output []byte
	err    error
}

func (s *stubSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	return s.output, s.err
}
func (s *stubSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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

// Plan 2: after a successful deploy on a dev/stage/simple/standard/local-stage
// runtime whose subdomain is currently off, the handler auto-enables the L7
// route before returning. Result payload exposes SubdomainAccessEnabled +
// SubdomainURL; agent never calls zerops_subdomain explicitly.
func TestDeployTool_SSHMode_AutoEnablesSubdomain(t *testing.T) {
	// t.Parallel omitted — OverrideHTTPReadyConfigForTest mutates a
	// package-level config; parallel tests would clobber each other's
	// interval/timeout values even though the mutex keeps the race
	// detector green.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	// Pre-flight reads zerops.yaml from projectRoot (two levels up from
	// stateDir). Structure paths so preflight finds a minimal valid file.
	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "app",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	minimalYaml := "zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      ports:\n        - port: 3000\n          httpSupport: true\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "zerops.yaml"), []byte(minimalYaml), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				SubdomainAccess: false,
				Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			SubdomainAccess: false,
			Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	text := getTextContent(t, result)
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v (raw: %s)", err, text)
	}
	if parsed.Status != statusDeployed {
		t.Fatalf("status = %s, want DEPLOYED (raw: %s)", parsed.Status, text)
	}
	if !parsed.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true after auto-enable, got false")
	}
	if parsed.SubdomainURL == "" {
		t.Error("SubdomainURL: want non-empty, got empty")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1, got %d", mock.CallCounts["EnableSubdomainAccess"])
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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

func TestDeployTool_ActiveDeploy_WithBuildWarnings(t *testing.T) {
	t.Parallel()

	buildSvcID := "build-svc-42"
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
				Build: &platform.BuildInfo{
					ServiceStackID: &buildSvcID,
				},
			},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Severity: "Warning", Facility: "local0", Tag: "zbuilder@av-1", Message: "WARN: deployFiles paths not found: dist"},
	})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, logFetcher, runtime.Info{}, "", testDeployEngine(t))

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
	if len(parsed.BuildLogs) != 1 {
		t.Fatalf("expected 1 build warning line, got %d", len(parsed.BuildLogs))
	}
	if parsed.BuildLogs[0] != "WARN: deployFiles paths not found: dist" {
		t.Errorf("buildLogs[0] = %q, want warning about deployFiles", parsed.BuildLogs[0])
	}
	if parsed.BuildLogsSource != buildContainerSource {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, buildContainerSource)
	}
}

func TestDeployTool_ActiveDeploy_NoBuildWarnings(t *testing.T) {
	t.Parallel()

	buildSvcID := "build-svc-43"
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
				Build: &platform.BuildInfo{
					ServiceStackID: &buildSvcID,
				},
			},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, logFetcher, runtime.Info{}, "", testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want DEPLOYED", parsed.Status)
	}
	if len(parsed.BuildLogs) != 0 {
		t.Errorf("expected no buildLogs for clean deploy, got %d", len(parsed.BuildLogs))
	}
	if parsed.BuildLogsSource != "" {
		t.Errorf("buildLogsSource should be empty for clean deploy, got %q", parsed.BuildLogsSource)
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "npm error code ERESOLVE"},
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "Build command failed with exit code 1"},
	})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, logFetcher, runtime.Info{}, "", testDeployEngine(t))

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
func (s *stubSSHWithReadiness) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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

// TestDeployTool_SelfDeploy_NeutralMessage pins the honest post-deploy
// message contract (DS-01). For a self-deploy to a dynamic runtime, the
// message reports what the platform told us (deploy succeeded, new
// container replaced old) and points the agent at the next right tool.
// It does NOT assert process liveness the code did not check. Dev-server
// lifecycle guidance is owned by atoms (prescribing zerops_dev_server)
// and by zerops_verify — not by free-text assertions in deploy_poll.go.
// See plans/dev-server-canonical-primitive.md invariant DS-01.
func TestDeployTool_SelfDeploy_NeutralMessage(t *testing.T) {
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
	if !strings.Contains(parsed.Message, "Successfully deployed") {
		t.Errorf("message should report deploy success, got: %s", parsed.Message)
	}
	forbidden := []string{"NOT running", "idle start", "auto-start", "Built-in webserver"}
	for _, phrase := range forbidden {
		if strings.Contains(parsed.Message, phrase) {
			t.Errorf("message must not assert runtime state (%q), got: %s", phrase, parsed.Message)
		}
		if strings.Contains(parsed.NextActions, phrase) {
			t.Errorf("nextActions must not assert runtime state (%q), got: %s", phrase, parsed.NextActions)
		}
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
	// Cross-deploy message is neutral — the self-deploy addendum (new
	// container, SSH sessions dead) does not apply.
	if strings.Contains(parsed.Message, "NOT running") {
		t.Errorf("message must not assert runtime state, got: %s", parsed.Message)
	}
	if strings.Contains(parsed.NextActions, "NOT running") {
		t.Errorf("nextActions must not assert runtime state, got: %s", parsed.NextActions)
	}
	if strings.Contains(parsed.Message, "New container replaced old") {
		t.Errorf("cross-deploy must not carry self-deploy SSH-sessions-dead addendum, got: %s", parsed.Message)
	}
}

func TestDeployTool_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &stubSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "prepare command failed"},
	})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, logFetcher, runtime.Info{}, "", testDeployEngine(t))

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
	if !strings.Contains(parsed.Suggestion, "RUNTIME PREPARE failed") {
		t.Errorf("suggestion should indicate prepare phase failed (not build), got: %s", parsed.Suggestion)
	}
	if !strings.Contains(parsed.Suggestion, "prepareCommands") {
		t.Errorf("suggestion should direct agent to prepareCommands, got: %s", parsed.Suggestion)
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
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

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

func TestDeployTool_DescriptionByEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		rtInfo       runtime.Info
		wantContains string
		wantAbsent   string
	}{
		{
			name:         "container_omit_workingDir",
			rtInfo:       runtime.Info{InContainer: true, ServiceName: "zcpx"},
			wantContains: "Omit workingDir",
		},
		{
			name:         "local_defaults",
			rtInfo:       runtime.Info{},
			wantContains: "defaults to",
			wantAbsent:   "Omit workingDir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			ssh := &stubSSH{}
			authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, tt.rtInfo, "", testDeployEngine(t))

			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			ss, err := srv.Connect(ctx, st, nil)
			if err != nil {
				t.Fatalf("server connect: %v", err)
			}
			defer ss.Close()

			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
			session, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatalf("client connect: %v", err)
			}
			defer session.Close()

			result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatalf("list tools: %v", err)
			}

			var desc string
			for _, tool := range result.Tools {
				if tool.Name == "zerops_deploy" {
					desc = tool.Description
					break
				}
			}
			if desc == "" {
				t.Fatal("zerops_deploy not found in tool list")
			}
			if tt.wantContains != "" && !strings.Contains(desc, tt.wantContains) {
				t.Errorf("description = %q, want to contain %q", desc, tt.wantContains)
			}
			if tt.wantAbsent != "" && strings.Contains(desc, tt.wantAbsent) {
				t.Errorf("description = %q, should NOT contain %q", desc, tt.wantAbsent)
			}
		})
	}
}

// testDeployEngine creates an Engine for deploy tests.
func testDeployEngine(t *testing.T) *workflow.Engine {
	t.Helper()
	dir := t.TempDir()
	return workflow.NewEngine(dir, workflow.EnvContainer, nil)
}

// setupAdoptedService writes a ServiceMeta so the service is "known" to ZCP.
func setupAdoptedService(t *testing.T, stateDir, hostname, stageHostname string) {
	t.Helper()
	mode := topology.PlanModeSimple
	if stageHostname != "" {
		mode = topology.PlanModeStandard
	}
	meta := &workflow.ServiceMeta{
		Hostname:         hostname,
		Mode:             mode,
		StageHostname:    stageHostname,
		BootstrapSession: "test-session",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// setupDeployedService writes a ServiceMeta with FirstDeployedAt stamped so
// deploy-state derivation (compute_envelope.DeriveDeployed) reports
// Deployed=true for hostname. Under plan A.3 the stamp is driven by
// RecordDeployAttempt-on-success and adoption-at-ACTIVE; tests needing the
// bit set up front use this helper and plant the timestamp directly.
func setupDeployedService(t *testing.T, stateDir, hostname, stageHostname string) {
	t.Helper()
	setupAdoptedService(t, stateDir, hostname, stageHostname)
	meta, err := workflow.ReadServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		t.Fatalf("ReadServiceMeta after setup: %v", err)
	}
	meta.FirstDeployedAt = "2026-04-19T10:00:00Z"
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta stamp: %v", err)
	}
}

func TestDeployTool_AdoptionGate_BlocksUnadoptedService(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	// Create services/ dir (bootstrap ran for another service) but NOT for "docs".
	setupAdoptedService(t, stateDir, "other", "")

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "docs"}})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "docs",
	})

	if !result.IsError {
		t.Fatal("expected IsError for unadopted service, got success")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not adopted") {
		t.Errorf("error should mention 'not adopted', got: %s", text)
	}
	if !strings.Contains(text, "bootstrap") {
		t.Errorf("error should suggest bootstrap, got: %s", text)
	}
}

func TestDeployTool_AdoptionGate_AllowsAdoptedService(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	setupAdoptedService(t, stateDir, "appdev", "appstage")

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
	})

	// Adoption gate must let an adopted service through. Downstream may
	// still fail on preflight (no zerops.yaml in the test setup), so we
	// only assert the failure isn't the gate itself.
	text := getTextContent(t, result)
	if strings.Contains(text, "not adopted") || strings.Contains(text, platform.ErrPrerequisiteMissing) {
		t.Errorf("adoption gate blocked an adopted service: %s", text)
	}
}

func TestDeployTool_AdoptionGate_AllowsStageHostname(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	setupAdoptedService(t, stateDir, "appdev", "appstage")

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev"},
			{ID: "svc-2", Name: "appstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "appdev",
		"targetService": "appstage",
	})

	// Adoption gate must let a stage hostname pass when its dev pair was
	// adopted. Downstream may still fail on preflight (no zerops.yaml);
	// we only check the failure isn't the gate.
	text := getTextContent(t, result)
	if strings.Contains(text, "not adopted") || strings.Contains(text, platform.ErrPrerequisiteMissing) {
		t.Errorf("adoption gate blocked stage hostname: %s", text)
	}
}

func TestDeployTool_AdoptionGate_GitPush_BlocksUnadopted(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	// Create services/ dir (bootstrap ran) but NOT for "docs".
	setupAdoptedService(t, stateDir, "other", "")

	mock := platform.NewMock()
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "docs",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})

	if !result.IsError {
		t.Fatal("expected IsError for unadopted git-push, got success")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not adopted") {
		t.Errorf("error should mention 'not adopted', got: %s", text)
	}
}

// stubSSHWithCommands dispatches on command content for fine-grained test control.
// Pre-flight order in handleGitPush: committed-code check → GIT_TOKEN check → push.
type stubSSHWithCommands struct {
	committedOutput []byte // output for committed-code check command ("1" = has commits, "0" = no)
	committedErr    error
	tokenOutput     []byte // output for GIT_TOKEN check command ("1" = set, "0" = missing)
	tokenErr        error
	pushOutput      []byte // output for the actual push command
	pushErr         error

	committedCalls int // committed-code check invocation counter
	tokenCalls     int // GIT_TOKEN check invocation counter
	pushCalls      int // push invocation counter
}

func (s *stubSSHWithCommands) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	// Committed-code pre-flight: looks at HEAD.
	if strings.Contains(command, "rev-parse HEAD") && !strings.Contains(command, "netrc") {
		s.committedCalls++
		out := s.committedOutput
		if out == nil {
			// Default: repo has commits. Tests that want "no commits" override explicitly.
			out = []byte("1")
		}
		return out, s.committedErr
	}
	// GIT_TOKEN pre-flight (test -n ... && echo 1 || echo 0).
	if strings.Contains(command, "GIT_TOKEN") && !strings.Contains(command, "netrc") {
		s.tokenCalls++
		return s.tokenOutput, s.tokenErr
	}
	s.pushCalls++
	return s.pushOutput, s.pushErr
}
func (s *stubSSHWithCommands) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return s.pushOutput, s.pushErr
}

func TestDeployTool_GitPush_MissingGitToken_ReturnsPrerequisites(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	setupDeployedService(t, stateDir, "appdev", "")

	mock := platform.NewMock()
	// GIT_TOKEN check returns "0" — token not set.
	ssh := &stubSSHWithCommands{
		tokenOutput: []byte("0"),
		pushOutput:  []byte("ok"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})

	// Should NOT be an error — it's a structured "prerequisites missing" response.
	text := getTextContent(t, result)

	wantParts := []string{
		"GIT_TOKEN_MISSING",       // uses platform error constant
		"action=\\\"strategy\\\"", // short pointer to central deploy-config action (JSON-escaped)
		"push-git",                // the strategy being configured
		"appdev",                  // target service hostname filled into the pointer
	}
	for _, part := range wantParts {
		if !strings.Contains(text, part) {
			t.Errorf("git-push prerequisite response should contain %q, got:\n%s", part, text)
		}
	}
}

func TestDeployTool_GitPush_WithGitToken_Succeeds(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	setupDeployedService(t, stateDir, "appdev", "")

	mock := platform.NewMock()
	// GIT_TOKEN check returns "1" — token is set.
	ssh := &stubSSHWithCommands{
		tokenOutput: []byte("1"),
		pushOutput:  []byte("ok"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1", Email: "test@test.com", FullName: "Test"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})

	if result.IsError {
		t.Fatalf("expected success when GIT_TOKEN exists, got error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "PUSHED") && !strings.Contains(text, "NOTHING_TO_PUSH") {
		t.Errorf("expected push result status, got: %s", text)
	}
}

// TestDeployTool_GitPush_NoCommittedCode_Refuses pins the committed-code
// guard in handleGitPush: git-push requires an actual commit at workingDir
// so the push has something to transmit. If the working dir isn't a git
// repo, or the repo has no commits, the tool refuses up front instead of
// silently auto-committing (which used to hide bugs) or pushing an empty
// state.
func TestDeployTool_GitPush_NoCommittedCode_Refuses(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	// Any adopted service — deploy-state no longer matters for the git-push
	// precondition (FirstDeployedAt is dropped; see plan phase A.3).
	setupAdoptedService(t, stateDir, "appdev", "")

	mock := platform.NewMock()
	// Committed-code pre-flight returns "0" — no commits on container.
	ssh := &stubSSHWithCommands{committedOutput: []byte("0"), tokenOutput: []byte("1"), pushOutput: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})
	if !result.IsError {
		t.Fatalf("expected error for git-push without committed code, got:\n%s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, needle := range []string{"committed code", "commit"} {
		if !strings.Contains(text, needle) {
			t.Errorf("response missing %q. Got:\n%s", needle, text)
		}
	}
	// No downstream calls should have happened — guard runs first.
	if ssh.tokenCalls != 0 {
		t.Errorf("GIT_TOKEN check fired despite committed-code guard refusing (%d calls)", ssh.tokenCalls)
	}
	if ssh.pushCalls != 0 {
		t.Errorf("push fired despite committed-code guard refusing (%d calls)", ssh.pushCalls)
	}
}

// TestDeployTool_GitPush_AdoptedNeverDeployed_Proceeds locks in the
// FirstDeployedAt decoupling: a service adopted by ZCP but never deployed
// through a ZCP verify cycle (legacy state or fresh adopt) must still be
// able to git-push as long as the container has committed code. The old
// gate (meta.IsDeployed) blocked this class of users — most visibly
// during export of an already-running service whose meta ZCP never stamped.
func TestDeployTool_GitPush_AdoptedNeverDeployed_Proceeds(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	// Adopted, no FirstDeployedAt. Under the old gate this would fail;
	// under the new model the gate looks at the repo, not the meta.
	setupAdoptedService(t, stateDir, "appdev", "")

	mock := platform.NewMock()
	ssh := &stubSSHWithCommands{
		committedOutput: []byte("1"), // repo has commits
		tokenOutput:     []byte("1"), // GIT_TOKEN present
		pushOutput:      []byte("ok"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1", Email: "t@t.com", FullName: "Test"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})
	if result.IsError {
		t.Fatalf("expected success for adopted-never-deployed + committed code, got:\n%s", getTextContent(t, result))
	}
	if ssh.committedCalls != 1 {
		t.Errorf("committed-code pre-flight must run exactly once, got %d calls", ssh.committedCalls)
	}
	if ssh.tokenCalls != 1 {
		t.Errorf("GIT_TOKEN pre-flight must run after committed-code check, got %d calls", ssh.tokenCalls)
	}
	if ssh.pushCalls != 1 {
		t.Errorf("push must fire after both pre-flights pass, got %d calls", ssh.pushCalls)
	}
}

func TestDeployTool_AdoptionGate_EmptyStateDir_Skips(t *testing.T) {
	t.Parallel()

	// Empty stateDir = no check, allows deploy (backward compat).
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("expected success with empty stateDir, got error: %s", getTextContent(t, result))
	}
}

func TestDeployTool_AdoptionGate_NoServicesDir_Skips(t *testing.T) {
	t.Parallel()

	// stateDir exists but has no services/ subdirectory (no bootstrap ever ran).
	// Gate should not activate — allows deploy for fresh projects.
	stateDir := t.TempDir()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	if result.IsError {
		t.Errorf("expected success with no services dir, got error: %s", getTextContent(t, result))
	}
}

func TestDeployTool_PreFlight_BlocksWithoutZeropsYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := dir

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	// Write ServiceMeta so pre-flight activates, but no zerops.yaml.
	meta := &workflow.ServiceMeta{
		Hostname:       "app",
		Mode:           "simple",
		BootstrappedAt: "2026-01-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, eng)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	// Pre-flight blocks (not IsError — returns structured result).
	text := getTextContent(t, result)
	if !strings.Contains(text, "zerops_yml_exists") && !strings.Contains(text, "zerops.yaml") {
		t.Errorf("expected pre-flight to mention zerops.yaml, got: %s", text)
	}
}
