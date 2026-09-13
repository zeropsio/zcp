// Tests for: dev_server.go — zerops_dev_server MCP tool handler, in
// particular the S7 start-success public-access hook (docs/spec-workflows.md
// §8 O3 PA-1's second hook): after a passing health probe, ensurePublicAccess
// runs with listener forced true.

package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// scriptSSH is a scripted ops.SSHDeployer: every ExecSSH/ExecSSHBackground
// call returns the next (output, err) pair from the queue, or a zero-value
// response once drained. Mirrors internal/ops/dev_server_test.go's stub of
// the same name (unexported to its package, so this is a local copy).
type scriptSSH struct {
	queue []scriptStep
}

type scriptStep struct {
	output string
	err    error
}

func (s *scriptSSH) ExecSSH(_ context.Context, _ string, _ string) ([]byte, error) {
	return s.next()
}

func (s *scriptSSH) ExecSSHBackground(_ context.Context, _ string, _ string, _ time.Duration) ([]byte, error) {
	return s.next()
}

func (s *scriptSSH) next() ([]byte, error) {
	if len(s.queue) == 0 {
		return nil, nil
	}
	step := s.queue[0]
	s.queue = s.queue[1:]
	if step.err != nil {
		return []byte(step.output), step.err
	}
	return []byte(step.output), nil
}

// devServerStartSuccessSSH returns a scriptSSH scripted for a successful
// `start` action: background spawn ack, a passing health probe, a log tail.
func devServerStartSuccessSSH() *scriptSSH {
	return &scriptSSH{queue: []scriptStep{
		{output: "zcp-dev-server-spawned pid=1234"}, // bg spawn ack
		{output: "OK 200 123"},                      // health probe
		{output: "starting...\nok"},                 // log tail
	}}
}

// TestDevServerStart_Success_EnablesSubdomainOnce pins the S7 dev-server
// start-success hook: a passing health probe on a deferred-start dev-mode
// dynamic runtime (which never auto-enabled at deploy time — no listener
// existed then, per TestEnsurePublicAccess_DeferredStartNoListener_SkipsWithoutStamp
// in deploy_subdomain_test.go) forces listener=true and fires the one-time
// auto-enable. A second start on the same hostname must NOT enable again —
// the first call's stamp blocks it (PA-2).
func TestDevServerStart_Success_EnablesSubdomainOnce(t *testing.T) {
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeCategoryName: "USER",
					ServiceStackTypeVersionName:  "nodejs@22",
				}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "appdev", ProjectID: "proj-1",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeCategoryName: "USER",
				ServiceStackTypeVersionName:  "nodejs@22",
			},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, mock, okHTTP, "proj-1", devServerStartSuccessSSH(), dir)

	result := callTool(t, srv, "zerops_dev_server", map[string]any{
		"action": "start", "hostname": "appdev",
		"command": "npm run start:dev", "port": 3000,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var dr struct {
		Running bool `json:"running"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &dr); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if !dr.Running {
		t.Fatalf("expected Running=true for the scripted successful spawn")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 after first successful start, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}

	meta, err := workflow.FindServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if got := meta.PublicAccessFor("appdev").SubdomainEnabledByZcpAt; got == "" {
		t.Error("SubdomainEnabledByZcpAt: want non-empty stamp after start-success hook enables")
	}

	// Second start on the same hostname: the stamp from the first call
	// must block a second auto-enable (PA-2).
	srv2 := mcp.NewServer(&mcp.Implementation{Name: "test2", Version: "0.1"}, nil)
	RegisterDevServer(srv2, mock, okHTTP, "proj-1", devServerStartSuccessSSH(), dir)
	result2 := callTool(t, srv2, "zerops_dev_server", map[string]any{
		"action": "start", "hostname": "appdev",
		"command": "npm run start:dev", "port": 3000,
	})
	if result2.IsError {
		t.Fatalf("unexpected error on second start: %s", getTextContent(t, result2))
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls after second start: want still 1 (PA-2 — stamped, never re-enable), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}
}
