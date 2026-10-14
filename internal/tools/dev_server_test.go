// Tests for: dev_server.go, dev_server_migrate.go — the zerops_dev_server
// handler on top of a dev service's supervisor: the S7 start-success
// public-access hook (docs/spec-workflows.md §8 O3 PA-1's second hook), the
// move of a dev setup onto the supervisor, and the supervisor as the listener
// fact after a deploy.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const testDevsupURL = "https://github.com/zeropsio/zcp/releases/download/v9.300.0/zcp-devsup-linux-amd64"

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
	c := devservertest.NewSupervised("")

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})

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
		t.Fatalf("expected Running=true for the supervised start")
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
	RegisterDevServer(srv2, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})
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

// TestDevServer_RunningOnlyWhereChecked: a log read checks nothing, so its
// answer says nothing of whether the server runs — `running` is left out,
// never a `false` the Mate would read as "it stopped". An action that does
// check says it.
func TestDevServer_RunningOnlyWhereChecked(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		wantRunning bool
	}{
		{name: "a log read", action: "logs"},
		{name: "a start that came up", action: "start", wantRunning: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE",
					Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}}}).
				WithService(&platform.ServiceStack{ID: "svc-1", Name: "appdev", ProjectID: "proj-1"})
			c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"npm run start:dev","cwd":"."}]}`)
			c.Logs = map[string][]string{"appdev": {"line 1", "line 2"}}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})
			result := callTool(t, srv, "zerops_dev_server", map[string]any{
				"action": tt.action, "hostname": "appdev", "command": "npm run start:dev", "port": 3000,
			})
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			var answer map[string]any
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &answer); err != nil {
				t.Fatalf("parse result: %v", err)
			}
			running, said := answer["running"]
			if said != tt.wantRunning || (said && running != true) {
				t.Errorf("running = %v (said %v), want said=%v", running, said, tt.wantRunning)
			}
		})
	}
}

// todaysDevSetup is a dev setup from before the supervisor: the dev server
// was started over SSH and run.start kept the container idle.
const todaysDevSetup = `zerops:
  - setup: dev
    build:
      base: bun@1.3
      deployFiles: ./
    run:
      base: bun@1.3
      ports:
        - port: 3000
          httpSupport: true
      start: zsc noop --silent
`

// migrationToolServer registers zerops_dev_server for appdev, a dev service
// whose deploys the platform mock reports with buildStatus.
func migrationToolServer(t *testing.T, c *devservertest.Container, buildStatus string) (*mcp.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1",
		BootstrappedAt: "2026-10-14", PrimarySetupName: "dev",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: buildStatus, Sequence: 2},
		})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{
		Client: mock, ProjectID: "proj-1", SSH: c, StateDir: dir,
		AuthInfo:   &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		ZcpVersion: "v9.300.0",
	})
	return srv, dir
}

type devServerAnswer struct {
	Running bool   `json:"running"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

func callDevServer(t *testing.T, srv *mcp.Server, args map[string]any) devServerAnswer {
	t.Helper()
	result := callTool(t, srv, "zerops_dev_server", args)
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("zerops_dev_server %v: %s", args, text)
	}
	var a devServerAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		t.Fatalf("parse %s: %v", text, err)
	}
	return a
}

// TestDevServer_FirstStartWithoutSupervisor_MigratesIt (orchestrator decision
// 2026-10-14 23:05, replacing TEST-PLAN E1 #13): a dev service without the
// supervisor is moved onto it by its first dev-server start — the dev setup
// gains the two lines, the server is declared, the dev service is redeployed
// once from inside and the server runs under the supervisor; one sentence
// says what changed. Nothing runs on the old SSH path, and the next start
// redeploys nothing.
func TestDevServer_FirstStartWithoutSupervisor_MigratesIt(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup}
	srv, _ := migrationToolServer(t, c, statusActive)

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000})
	if !a.Running || a.URL != "http://appdev:3000/" {
		t.Fatalf("after the move the server is not up: %+v", a)
	}
	if !strings.HasPrefix(a.Message, `zcp moved appdev onto the dev-server supervisor: setup "dev" in zerops.yml now installs and runs zcp-devsup`) ||
		strings.Count(a.Message, "zcp moved") != 1 {
		t.Errorf("message = %q, want one sentence saying what changed, then the start's own answer", a.Message)
	}
	if c.Deploys != 1 {
		t.Errorf("redeploys = %d, want exactly one", c.Deploys)
	}
	if !strings.Contains(c.YAML, "start: zcp-devsup run") || !strings.Contains(c.YAML, testDevsupURL) || !strings.Contains(c.YAML, "httpSupport: true") {
		t.Errorf("zerops.yml after the move:\n%s", c.YAML)
	}
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || len(d.Servers) != 1 || d.Servers[0].Name != "appdev" || d.Servers[0].Command != "bun --watch index.ts" || d.Servers[0].Port != 3000 {
		t.Errorf("declaration = %s (%v), want appdev's one server", c.Declaration, err)
	}
	if c.Status("appdev") != devsup.StatusRunning {
		t.Errorf("supervisor status = %q, want running", c.Status("appdev"))
	}
	for _, cmd := range c.Sent() {
		if strings.Contains(cmd, "setsid") || strings.Contains(cmd, "nohup") || strings.Contains(cmd, "pkill") {
			t.Errorf("the old SSH start ran: %q", cmd)
		}
	}

	again := callDevServer(t, srv, map[string]any{"action": "restart", "hostname": "appdev"})
	if !again.Running || strings.Contains(again.Message, "zcp moved") || c.Deploys != 1 {
		t.Errorf("a later start moved the dev setup again (deploys %d): %+v", c.Deploys, again)
	}
}

// TestDevServer_MigrationRedeployFails: when the redeploy that brings up the
// supervisor fails, the answer says so with what is written and how to finish.
func TestDevServer_MigrationRedeployFails(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yaml", YAML: todaysDevSetup}
	srv, _ := migrationToolServer(t, c, statusBuildFailed)

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun index.ts", "port": 3000})
	if a.Running || a.Reason != reasonMigrationDeployFailed {
		t.Fatalf("answer = %+v, want %s", a, reasonMigrationDeployFailed)
	}
	if !strings.Contains(a.Message, "zerops_deploy targetService=appdev finishes the move") || !strings.Contains(c.YAML, "start: zcp-devsup run") {
		t.Errorf("message = %q, want the written files and the way to finish", a.Message)
	}
}

// TestDevServer_MigrationNeedsADevSetup: a zerops.yaml whose dev setup zcp
// cannot tell is left as it is and nothing is redeployed.
func TestDevServer_MigrationNeedsADevSetup(t *testing.T) {
	t.Parallel()
	yml := "zerops:\n  - setup: web\n  - setup: api\n"
	c := &devservertest.Container{YAMLFile: "zerops.yaml", YAML: yml}
	srv, dir := migrationToolServer(t, c, statusActive)
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "s", BootstrappedAt: "2026-10-14"}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "start", "hostname": "appdev", "command": "bun index.ts", "port": 3000})
	if !result.IsError || !strings.Contains(getTextContent(t, result), "cannot tell the dev setup") {
		t.Fatalf("answer = %s, want the dev setup named", getTextContent(t, result))
	}
	if c.YAML != yml || c.Deploys != 0 {
		t.Errorf("zerops.yaml changed or a redeploy ran (%d)", c.Deploys)
	}
}

// TestDeployListener_SupervisorReportsListener (TEST-PLAN E1 #12): a
// preview's subdomain is enabled after its first deploy when the supervisor
// reports a listener — a declared server with a port that runs — and only
// then; a dev setup without the supervisor keeps the runtime's static class.
func TestDeployListener_SupervisorReportsListener(t *testing.T) {
	t.Parallel()
	web := `{"version":1,"servers":[{"name":"app","port":3000,"command":"bun index.ts","cwd":"."}]}`
	tests := []struct {
		name       string
		container  func() *devservertest.Container
		wantEnable int
	}{
		{name: "a declared server runs on its port", container: func() *devservertest.Container { return devservertest.NewSupervised(web) }, wantEnable: 1},
		{name: "the declared server crashed", container: func() *devservertest.Container {
			c := devservertest.NewSupervised("")
			c.Declaration, c.Crash = web, map[string]bool{"app": true}
			c.Redeploy()
			return c
		}},
		{name: "only a worker without a port", container: func() *devservertest.Container {
			return devservertest.NewSupervised(`{"version":1,"servers":[{"name":"worker","command":"bun worker.ts","cwd":"."}]}`)
		}},
		{name: "no supervisor", container: func() *devservertest.Container { return &devservertest.Container{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeMeta(t, dir, topology.PlanModeDev)
			mock := autoEnableTestMockWithType(t, false, "nodejs@22")
			result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
			c := tt.container()
			ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result, deployListener(context.Background(), c, "app", result)...)
			if got := mock.CallCounts["EnableSubdomainAccess"]; got != tt.wantEnable {
				t.Errorf("EnableSubdomainAccess calls = %d, want %d", got, tt.wantEnable)
			}
			for _, cmd := range c.Sent() {
				if strings.Contains(cmd, "zsc noop") {
					t.Errorf("listener read from zsc noop: %q", cmd)
				}
			}
		})
	}
}

// recordingUnits records the unit prefixes removed.
type recordingUnits struct {
	removed []string
	err     error
}

func (u *recordingUnits) RemoveUnits(_ context.Context, prefix string) error {
	u.removed = append(u.removed, prefix)
	return u.err
}

// TestRetireDevServerKeeper (TEST-PLAN E1 #14): an upgraded zcp removes what
// the deleted keeper left on its container — the devservers- units and
// dev-servers.json in the state directory — and nothing else.
func TestRetireDevServerKeeper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kept := filepath.Join(dir, "dev-servers.json")
	other := filepath.Join(dir, "services.json")
	for _, f := range []string{kept, other} {
		if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	units := &recordingUnits{}
	if err := RetireDevServerKeeper(context.Background(), units, dir); err != nil {
		t.Fatalf("RetireDevServerKeeper: %v", err)
	}
	if _, err := os.Stat(kept); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dev-servers.json is still in the state directory (%v)", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another state file went with it: %v", err)
	}
	if !slices.Equal(units.removed, []string{"devservers-"}) {
		t.Errorf("units removed = %q, want the keeper's devservers- units", units.removed)
	}
	// A second run finds nothing left and is no error.
	if err := RetireDevServerKeeper(context.Background(), units, dir); err != nil {
		t.Errorf("second run: %v", err)
	}
	units.err = errors.New("systemctl: boom")
	if err := RetireDevServerKeeper(context.Background(), units, dir); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a unit removal failure is not reported: %v", err)
	}
}
