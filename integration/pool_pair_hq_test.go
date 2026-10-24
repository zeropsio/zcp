// Tests for: integration — a pair keyed by its dev pool through the full MCP
// server: git-push setup refuses it before any ssh call, and status reads no
// checkout origin on the pool (docs/spec-workflows.md §13 PM-10, §8 E8).

package integration_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolPairHostSSH records every host it is sent a command for and answers
// nothing.
type poolPairHostSSH struct {
	mu    sync.Mutex
	hosts []string
}

func (s *poolPairHostSSH) ExecSSH(_ context.Context, hostname, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = append(s.hosts, hostname)
	return nil, nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *poolPairHostSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *poolPairHostSSH) ExecSSHBackground(_ context.Context, hostname, _ string, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = append(s.hosts, hostname)
	return nil, nil
}

func (s *poolPairHostSSH) reached(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.hosts, host)
}

// poolPairSession serves zerops_workflow over a project whose wired
// appdev/appstage pair a pool migration moved onto its dev pool apppool.
func poolPairSession(t *testing.T) (*mcp.ClientSession, *platform.Mock, *poolPairHostSSH) {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-01T00:00:00Z",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://hq.example/git/app-1/appdev.git",
		PrimarySetupName: "dev",
		HQ:               &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"},
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	if _, err := workflow.MigratePairToPool(stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "pool"}); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	services := []platform.ServiceStack{
		poolMigrateIntService("apppool", "svc-pool", "swarm@1"),
		poolMigrateIntService("appstage", "svc-stage", "nodejs@22"),
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj", Status: "ACTIVE"}).
		WithServices(services).WithServicesDirect(services)
	ssh := &poolPairHostSSH{}

	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	tools.RegisterWorkflow(srv, mock, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, ssh, runtime.Info{InContainer: true, ServiceName: "zcp", ProjectID: "proj-1"}, "")

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, mock, ssh
}

// TestPoolPair_Integration_GitPushSetupRefusesThePool: git-push setup on the
// pool that keys a pair is refused by name, pointing at a lease of the pool,
// before any ssh call or env write.
func TestPoolPair_Integration_GitPushSetupRefusesThePool(t *testing.T) {
	t.Parallel()
	session, mock, ssh := poolPairSession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "git-push-setup", "service": "apppool",
		"remoteUrl": "https://github.com/acme/app.git", "gitToken": "ghp_pooltoken",
	})

	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("git-push-setup on the pool was not refused: %s", text)
	}
	if !strings.Contains(text, "apppool") || !strings.Contains(text, "zerops_lease") {
		t.Errorf("the refusal does not name the pool and its lease: %s", text)
	}
	if len(ssh.hosts) != 0 || mock.CallCount("CreateServiceEnvVar") != 0 {
		t.Errorf("the refusal came after ssh %v or an env write (%d)", ssh.hosts, mock.CallCount("CreateServiceEnvVar"))
	}
	if strings.Contains(text, "ghp_pooltoken") {
		t.Errorf("the token reached the result: %s", text)
	}
}

// TestPoolPair_Integration_StatusReadsNoCheckoutOnThePool: status reads each
// wired pair's checkout origin on its dev half, never on a pool keying one.
func TestPoolPair_Integration_StatusReadsNoCheckoutOnThePool(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolPairSession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{"action": "status"})

	if res.IsError {
		t.Fatalf("status: %s", getTextContent(t, res))
	}
	if ssh.reached("apppool") {
		t.Errorf("status reached the pool over ssh: %v", ssh.hosts)
	}
}
