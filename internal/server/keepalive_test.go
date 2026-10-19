// Tests for: server package — keep-alive progress on long tool calls.
package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// beatLog records the progress notifications a client receives.
type beatLog struct {
	mu    sync.Mutex
	times []time.Time
	token []any
}

func (b *beatLog) handle(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.times = append(b.times, time.Now())
	b.token = append(b.token, req.Params.ProgressToken)
}

func (b *beatLog) snapshot() ([]time.Time, []any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]time.Time(nil), b.times...), append([]any(nil), b.token...)
}

// connectSilentTool serves one tool that stays silent for `silent`, behind
// keepAlive(interval, gap), and returns a client session whose progress
// notifications land in the returned log.
func connectSilentTool(t *testing.T, silent, interval, gap time.Duration) (*mcp.ClientSession, *beatLog) {
	t.Helper()
	ctx := t.Context()
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp", Version: "test"}, nil)
	srv.AddReceivingMiddleware(keepAlive(interval, gap))
	mcp.AddTool(srv, &mcp.Tool{Name: "silent"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		select {
		case <-time.After(silent):
		case <-ctx.Done():
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil, nil
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	log := &beatLog{}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: log.handle,
	})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, log
}

// A client with a request timeout (OpenCode's MCP SDK default is 60 s) resets
// it on each progress notification. A deploy goes quiet for minutes outside
// its build poll (the zcli push, HQ delivery), so every tool call that asked
// for progress hears from zcp at least once per interval while it runs.
func TestKeepAlive_SilentToolCall_SendsProgressEachInterval(t *testing.T) {
	t.Parallel()
	const interval = 20 * time.Millisecond
	session, log := connectSilentTool(t, 7*interval, interval, 0)

	params := &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}
	params.SetProgressToken("deploy-1")
	result, err := session.CallTool(t.Context(), params)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool returned an error result: %+v", result.Content)
	}

	times, tokens := log.snapshot()
	if len(times) < 3 {
		t.Fatalf("got %d progress notifications over a %s silent call, want at least 3", len(times), 7*interval)
	}
	for i, tok := range tokens {
		if tok != "deploy-1" {
			t.Errorf("notification %d carried token %v, want deploy-1", i, tok)
		}
	}
}

// A client that sent no progress token cannot route a notification; zcp sends
// none (the SDK would reject an unknown token).
func TestKeepAlive_NoProgressToken_SendsNothing(t *testing.T) {
	t.Parallel()
	const interval = 20 * time.Millisecond
	session, log := connectSilentTool(t, 5*interval, interval, 0)

	if _, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if times, _ := log.snapshot(); len(times) != 0 {
		t.Fatalf("got %d progress notifications without a progress token, want 0", len(times))
	}
}

// A progress notification and its call's response in one stdin chunk tear
// down Claude Code's MCP transport (see pollProcess). The response therefore
// trails the last keep-alive by at least the gap.
func TestKeepAlive_ResponseAfterBeat_WaitsTheGap(t *testing.T) {
	t.Parallel()
	const (
		interval = 40 * time.Millisecond
		gap      = 60 * time.Millisecond
	)
	// The tool ends a few ms after the second beat.
	session, log := connectSilentTool(t, 2*interval+5*time.Millisecond, interval, gap)

	params := &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}
	params.SetProgressToken("deploy-2")
	if _, err := session.CallTool(t.Context(), params); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	answered := time.Now()

	times, _ := log.snapshot()
	if len(times) == 0 {
		t.Fatal("no keep-alive was sent; the test's timing assumption broke")
	}
	if trail := answered.Sub(times[len(times)-1]); trail < gap-10*time.Millisecond {
		t.Fatalf("response came %s after the last keep-alive, want at least %s", trail, gap)
	}
}

// postBuildContainer is a dev container whose supervisor answers slowly once a
// redeploy replaced it, as a container just booted does; started is when that
// slow answer began.
type postBuildContainer struct {
	*devservertest.Container
	slow    time.Duration
	mu      sync.Mutex
	pushed  bool
	started time.Time
}

func (c *postBuildContainer) ExecSSH(ctx context.Context, host, cmd string) ([]byte, error) {
	c.mu.Lock()
	c.pushed = c.pushed || strings.Contains(cmd, "zcli push")
	slow := c.pushed && c.started.IsZero() && strings.Contains(cmd, "@@zcp-devsup ctl")
	if slow {
		c.started = time.Now()
	}
	c.mu.Unlock()
	if slow {
		time.Sleep(c.slow)
	}
	return c.Container.ExecSSH(ctx, host, cmd)
}

// A zerops_dev_server start that moves the dev setup onto the supervisor
// waits on the new container after its build is live, outside the build
// poll's own progress; a client that asked for progress hears from zcp each
// interval through that wait too.
func TestKeepAlive_DevServerStartAfterTheBuild_SendsProgressEachInterval(t *testing.T) {
	t.Parallel()
	const interval = 20 * time.Millisecond
	ctx := t.Context()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1", BootstrappedAt: "2026-10-19", PrimarySetupName: "dev",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: platform.ServiceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: platform.ServiceStatusActive, Sequence: 2}})
	c := &postBuildContainer{slow: 7 * interval, Container: &devservertest.Container{YAMLFile: "zerops.yaml", YAML: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: zsc noop --silent
`}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp", Version: "test"}, nil)
	srv.AddReceivingMiddleware(keepAlive(interval, 0))
	tools.RegisterDevServer(srv, tools.DevServerDeps{
		Client: mock, ProjectID: "proj-1", SSH: c, StateDir: dir,
		AuthInfo: &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		Devsup: tools.DevsupSource{Latest: func(context.Context) (string, error) {
			return "https://github.com/zeropsio/zcp/releases/download/v9.300.0/zcp-devsup-linux-amd64", nil
		}},
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	log := &beatLog{}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, &mcp.ClientOptions{ProgressNotificationHandler: log.handle}).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	params := &mcp.CallToolParams{Name: "zerops_dev_server", Arguments: map[string]any{"action": "start", "hostname": "appdev", "command": "node server.mjs", "port": 3000}}
	params.SetProgressToken("start-1")
	result, err := session.CallTool(ctx, params)
	if err != nil || result.IsError {
		t.Fatalf("CallTool: %v %+v", err, result)
	}
	if c.Deploys != 1 {
		t.Fatalf("deploys = %d, want the one redeploy of the move", c.Deploys)
	}
	c.mu.Lock()
	began := c.started
	c.mu.Unlock()
	times, _ := log.snapshot()
	during := 0
	for _, at := range times {
		if at.After(began) && at.Before(began.Add(c.slow)) {
			during++
		}
	}
	if began.IsZero() || during < 3 {
		t.Errorf("%d progress notifications during the %s wait after the build, want at least 3", during, c.slow)
	}
}
