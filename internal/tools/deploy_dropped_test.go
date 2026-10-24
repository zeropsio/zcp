// Tests for: tools/deploy_ssh.go — a deploy whose caller drops mid-build.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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
// is held there until the caller's drop has reached the handler; every later
// call honours the context it gets, as a real API client does. enabled is the
// receipt of the first EnableSubdomainAccess call.
type dropClient struct {
	*platform.Mock
	polls     atomic.Int32
	drop      func()
	dropped   <-chan struct{}
	enables   atomic.Int32
	enabled   chan struct{}
	enableOne sync.Once
}

func (c *dropClient) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.polls.Add(1) == 1 {
		c.drop()
		<-c.dropped
		return []platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: "BUILDING", Sequence: 1}}, nil
	}
	return c.Mock.SearchAppVersions(ctx, projectID, limit)
}

func (c *dropClient) EnableSubdomainAccess(ctx context.Context, serviceID string) (*platform.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.enables.Add(1)
	c.enableOne.Do(func() { close(c.enabled) })
	return c.Mock.EnableSubdomainAccess(ctx, serviceID)
}

// watchToolCall reports when the server-side tools/call context is cancelled
// (the caller's drop has reached the handler) and when the handler returns.
func watchToolCall(srv *mcp.Server) (dropped, returned <-chan struct{}) {
	droppedCh, returnedCh := make(chan struct{}), make(chan struct{})
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			go func() {
				<-ctx.Done()
				close(droppedCh)
			}()
			defer close(returnedCh)
			return next(ctx, method, req)
		}
	})
	return droppedCh, returnedCh
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
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	dropped, returned := watchToolCall(srv)
	client := &dropClient{Mock: mock, drop: drop, dropped: dropped, enabled: make(chan struct{})}
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

	select { // the receipt of the call, or the deploy ended without it
	case <-client.enabled:
	case <-returned:
	}
	<-returned
	if got := client.enables.Load(); got != 1 {
		t.Fatalf("EnableSubdomainAccess calls after the caller dropped: want 1, got %d", got)
	}
}
