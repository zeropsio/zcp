// Tests for: tools/deploy_ssh.go — a deploy whose caller drops mid-build.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
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

// dropClient builds like the platform does: the first poll sees BUILDING and
// the caller drops right then; every later call honours the context it gets,
// as a real API client does.
type dropClient struct {
	*platform.Mock
	polls atomic.Int32
	drop  func()
}

func (c *dropClient) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.polls.Add(1) == 1 {
		c.drop()
		time.Sleep(200 * time.Millisecond) // the cancel reaches the handler
		return []platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: "BUILDING", Sequence: 1}}, nil
	}
	return c.Mock.SearchAppVersions(ctx, projectID, limit)
}

func (c *dropClient) EnableSubdomainAccess(ctx context.Context, serviceID string) (*platform.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Mock.EnableSubdomainAccess(ctx, serviceID)
}

func TestDeployTool_CallerDropsMidBuild_StillEnablesPublicAccess(t *testing.T) {
	// non-parallel: OverrideHTTPReadyConfigForTest mutates a package-level config.
	defer ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)()

	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "app", Mode: topology.PlanModeDev, BootstrapSession: "s", BootstrappedAt: "2026-04-22",
	}); err != nil {
		t.Fatal(err)
	}
	mountDir := filepath.Join(projectRoot, "app")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      ports:\n        - port: 3000\n          httpSupport: true\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := platform.ServiceStack{ID: "svc-1", Name: "app",
		Ports:                []platform.Port{{Port: 3000, Protocol: "tcp", HTTPSupport: true}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"}}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).WithService(&svc).
		WithProject(&platform.Project{ID: "proj-1", Name: "t", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1}}).
		WithProcess(&platform.Process{ID: "proc-subdomain-enable-svc-1", Status: statusFinished})

	callCtx, drop := context.WithCancel(context.Background())
	client := &dropClient{Mock: mock, drop: drop}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, client, okHTTP, "proj-1", &stubSSH{output: []byte("ok")},
		&auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}, nil, runtime.Info{}, stateDir, testDeployEngine(t), nil, DevsupSource{})

	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0.1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, _ = session.CallTool(callCtx, &mcp.CallToolParams{Name: "zerops_deploy", Arguments: map[string]any{"targetService": "app"}})

	deadline := time.Now().Add(5 * time.Second)
	for mock.CallCount("EnableSubdomainAccess") == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := mock.CallCount("EnableSubdomainAccess"); got != 1 {
		t.Fatalf("EnableSubdomainAccess calls after the caller dropped: want 1, got %d", got)
	}
}
