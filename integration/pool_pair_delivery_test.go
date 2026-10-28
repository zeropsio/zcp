// Tests for: integration — the delivery and launch readers of a pair keyed by
// its dev pool through the full MCP server (docs/spec-workflows.md §13 PM-10,
// §10): git-push refuses it by name before any read, launch and release read
// it from its origin and never the pool, build-integration and a stage deploy
// from a lease never name the pool as a push source, and develop's scope never
// takes the pool.

package integration_test

import (
	"context"
	"os"
	"os/exec"
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
	return poolDeliverySessionAt(t, "https://github.com/acme/app.git")
}

// poolDeliverySessionAt is poolDeliverySession with the pair's origin at
// remoteURL.
func poolDeliverySessionAt(t *testing.T, remoteURL string) (*mcp.ClientSession, *platform.Mock, *poolPairHostSSH) {
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
		RemoteURL:        remoteURL,
		TrackedRef:       "main",
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
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "GIT_TOKEN", Content: poolDeliveryGitToken, Sensitive: true}}).
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

// TestPoolPair_Integration_ReleaseTagsTheOriginWithoutSSH: a release of the
// pool-keyed pair tags the pair's origin at its tracked tip, from zcp's own
// scratch, with no ssh call to the pool, a lease or the stage.
func TestPoolPair_Integration_ReleaseTagsTheOriginWithoutSSH(t *testing.T) {
	t.Parallel()
	origin := poolDeliveryOrigin(t)
	session, _, ssh := poolDeliverySessionAt(t, origin)

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "release", "service": "appstage", "releaseVersion": "v1.0.1",
	})

	text := getTextContent(t, res)
	if res.IsError || !strings.Contains(text, `"status":"released"`) {
		t.Fatalf("release of the pool-keyed pair did not tag its origin: %s", text)
	}
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "--verify", "-q", "v1.0.1^{commit}") //nolint:gosec // G204: a test's own git
	cmd.Dir = strings.TrimPrefix(origin, "file://")
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) == "" {
		t.Errorf("the origin holds no v1.0.1: %v", err)
	}
	if strings.Contains(text, poolDeliveryGitToken) {
		t.Errorf("the answer carries the pool's GIT_TOKEN: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("release reached %v over ssh", ssh.hosts)
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

// poolDeliveryGitToken is the pool's GIT_TOKEN as the env API answers it.
const poolDeliveryGitToken = "integration-pool-token-canary-77e2"

// poolDeliveryOrigin makes the pair's origin: a bare repository whose main
// holds a zerops.yaml with the stage's setup. It answers its file:// URL.
func poolDeliveryOrigin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...) //nolint:gosec // G204: a test's own git
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(work, "init", "-q", "-b", "main")
	yaml := "zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n      deployFiles: ./\n    run:\n      base: nodejs@22\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join(work, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	run(work, "add", "-A")
	run(work, "commit", "-q", "-m", "app")
	run(root, "clone", "-q", "--bare", work, filepath.Join(root, "origin"))
	return "file://" + filepath.Join(root, "origin")
}

// TestPoolPair_Integration_LaunchReadsTheOriginNeverThePool: launch-production
// on the stage of the pool-keyed pair reads the pair's origin, passes the
// source-control gate and reaches nothing over ssh; the pool's GIT_TOKEN
// stays out of the answer.
func TestPoolPair_Integration_LaunchReadsTheOriginNeverThePool(t *testing.T) {
	t.Parallel()
	session, _, ssh := poolDeliverySessionAt(t, poolDeliveryOrigin(t))

	res := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "launch-production",
		"productionProjectName": "app-prod", "targetService": "appstage", "region": "eu-central",
		"skipBuildIntegration": []string{"apppool"},
	})

	text := getTextContent(t, res)
	if strings.Contains(text, "source-control-required") || !strings.Contains(text, "classify-prompt") {
		t.Fatalf("launch did not pass the source-control gate on the origin: %s", text)
	}
	if strings.Contains(text, poolDeliveryGitToken) {
		t.Errorf("the answer carries the pool's GIT_TOKEN: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("launch reached %v over ssh", ssh.hosts)
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
