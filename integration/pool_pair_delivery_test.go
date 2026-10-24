// Tests for: integration — the delivery and launch readers of a pair keyed by
// its dev pool through the full MCP server (docs/spec-workflows.md §13 PM-10,
// §10): release, git-push and launch refuse it by name before any read,
// build-integration and a stage deploy from a lease never name the pool as a
// push source, and develop's scope never takes the pool.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolDeliverySession serves zerops_workflow and zerops_deploy over a project
// whose appdev/appstage pair — kept on GitHub, git-push configured, an
// Actions integration, deployed — a pool migration moved onto its dev pool
// apppool, with lease apppool-3 held.
func poolDeliverySession(t *testing.T) (*mcp.ClientSession, *platform.Mock, *poolPairHostSSH) {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-01T00:00:00Z",
		FirstDeployedAt:  "2026-10-01T01:00:00Z",
		CloseDeployMode:  topology.CloseModeAuto,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/acme/app.git",
		BuildIntegration: topology.BuildIntegrationActions,
		PrimarySetupName: "dev",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	if _, err := workflow.MigratePairToPool(stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "pool"}); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	leases := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"apppool","poolServiceId":"svc-pool","containerId":"c-3","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.apppool.zerops","lease":"lease-secret","branch":"main","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(leases, "apppool-3.json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	services := []platform.ServiceStack{
		poolMigrateIntService("apppool", "svc-pool", "swarm@1"),
		poolMigrateIntService("appstage", "svc-stage", "nodejs@22"),
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "app", Status: "ACTIVE"}).
		WithServices(services).WithServicesDirect(services).
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "LOG_LEVEL", Content: "info"}})
	ssh := &poolPairHostSSH{}
	rt := runtime.Info{InContainer: true, ServiceName: "zcp", ProjectID: "proj-1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	tools.RegisterWorkflow(srv, mock, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, ssh, rt, "")
	authInfo := &auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1", ClientID: "client-1", ProjectName: "app"}
	tools.RegisterDeploySSH(srv, mock, nopHTTPDoer{}, "proj-1", ssh, authInfo, defaultLogFetcher(), rt, stateDir, engine, nil, tools.DevsupSource{})

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
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
	return session, mock, ssh
}

// TestPoolPair_Integration_ReleaseRefusedWithoutSSH: a release of the
// pool-keyed pair is refused by name, pointing at a lease of the pool,
// before any ssh call.
func TestPoolPair_Integration_ReleaseRefusedWithoutSSH(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "release", "service": "appstage", "releaseVersion": "v1.0.1",
	})

	text := getTextContent(t, res)
	if !res.IsError || !strings.Contains(text, "apppool") || !strings.Contains(text, "zerops_lease") {
		t.Fatalf("release was not refused naming the pool and its lease: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("the refusal came after ssh %v", ssh.hosts)
	}
}

// TestPoolPair_Integration_GitPushRefusedBeforeSSH: zerops_deploy
// strategy=git-push on the stage of the pool-keyed pair is refused by name,
// pointing at a lease, before any ssh call.
func TestPoolPair_Integration_GitPushRefusedBeforeSSH(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_deploy", map[string]any{
		"targetService": "appstage", "strategy": "git-push",
	})

	text := getTextContent(t, res)
	if !res.IsError || !strings.Contains(text, "apppool") || !strings.Contains(text, "zerops_lease") {
		t.Fatalf("git-push was not refused naming the pool and its lease: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("the refusal came after ssh %v", ssh.hosts)
	}
}

// TestPoolPair_Integration_StageDeployFromLeaseNotRedirected: a deploy of the
// stage from a lease of its dev pool is the stage's delivery — it is never
// redirected to a git-push from the pool.
func TestPoolPair_Integration_StageDeployFromLeaseNotRedirected(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_deploy", map[string]any{
		"sourceService": "apppool-3", "targetService": "appstage",
	})

	text := getTextContent(t, res)
	if strings.Contains(text, "push-delivery-required") {
		t.Fatalf("the stage deploy from a lease was redirected to a push: %s", text)
	}
	if ssh.reached("apppool") {
		t.Errorf("the deploy reached the pool over ssh: %v", ssh.hosts)
	}
}

// TestPoolPair_Integration_BuildIntegrationReadsNoPool: wiring Actions for
// the pool-keyed pair targets the stage and reaches the pool over no ssh.
func TestPoolPair_Integration_BuildIntegrationReadsNoPool(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "build-integration", "service": "appstage", "integration": "actions",
	})

	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("build-integration: %s", text)
	}
	if !strings.Contains(text, `"buildTarget":"appstage"`) || strings.Contains(text, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null apppool") {
		t.Errorf("build-integration does not target the stage, or reads the token over ssh from the pool: %s", text)
	}
	if ssh.reached("apppool") {
		t.Errorf("build-integration reached the pool over ssh: %v", ssh.hosts)
	}
}

// TestPoolPair_Integration_LaunchRefusedBeforeAnyRead: launch-production on
// the stage of the pool-keyed pair answers the pool-dev-pair blocker before
// any read of the source.
func TestPoolPair_Integration_LaunchRefusedBeforeAnyRead(t *testing.T) {
	t.Parallel()
	session, mock, ssh := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "launch-production",
		"productionProjectName": "app-prod", "targetService": "appstage", "region": "eu-central",
	})

	text := getTextContent(t, res)
	if !strings.Contains(text, "pool-dev-pair-apppool") || !strings.Contains(text, "source-control-required") {
		t.Fatalf("launch did not answer the pool-dev-pair blocker: %s", text)
	}
	if len(ssh.hosts) != 0 || mock.CallCount("GetProjectEnv") != 0 {
		t.Errorf("launch read the source before refusing: ssh %v, %d env reads", ssh.hosts, mock.CallCount("GetProjectEnv"))
	}
}

// TestPoolPair_Integration_DevelopScopeRefusesThePool: develop's scope never
// takes the pool keying the pair; the refusal names the stage to scope.
func TestPoolPair_Integration_DevelopScopeRefusesThePool(t *testing.T) {
	t.Parallel()
	session, _, _ := poolDeliverySession(t)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "develop", "intent": "fix the header", "scope": []string{"apppool"},
	})

	text := getTextContent(t, res)
	if !res.IsError || !strings.Contains(text, "appstage") || !strings.Contains(text, "zerops_lease") {
		t.Fatalf("develop took the pool into its scope, or the refusal names no stage and lease: %s", text)
	}
}
