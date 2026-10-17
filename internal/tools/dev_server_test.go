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
	"github.com/zeropsio/zcp/internal/runtime"
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
		AuthInfo: &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		Devsup:   DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }},
	})
	return srv, dir
}

type devServerAnswer struct {
	Running bool   `json:"running"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	URL     string `json:"url"`

	MigratedToSupervisor *bool `json:"migratedToSupervisor"`
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
	if a.MigratedToSupervisor == nil || !*a.MigratedToSupervisor {
		t.Errorf("a migrating start carries migratedToSupervisor=true as data, got %v", a.MigratedToSupervisor)
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
	if !again.Running || strings.Contains(again.Message, "zcp moved") || again.MigratedToSupervisor != nil || c.Deploys != 1 {
		t.Errorf("a later start moved the dev setup again (deploys %d): %+v", c.Deploys, again)
	}
}

// A start on a dev service whose supervisor runs but does not answer in time
// is reported unresponsive and never moves the dev setup: no zerops.yaml
// rewrite, no redeploy while the stalled supervisor lives.
func TestDevServer_AStartOnASupervisorThatDoesNotAnswerIsReportedUnresponsiveAndDoesNotRedeploy(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"next dev","cwd":"."}]}`)
	c.Unresponsive = true
	yaml := c.YAML
	srv, _ := migrationToolServer(t, c, statusActive)

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev"})
	if a.Reason != "supervisor_unresponsive" || a.Running || a.MigratedToSupervisor != nil {
		t.Errorf("start = %+v, want supervisor_unresponsive with no move", a)
	}
	if c.Deploys != 0 || c.YAML != yaml {
		t.Errorf("a start on a stalled supervisor redeployed %d time(s) or rewrote zerops.yaml:\n%s", c.Deploys, c.YAML)
	}
}

// A dev-server call on a dev container that does not answer over SSH in time
// answers not answering with its recovery, never an UNKNOWN error.
func TestDevServer_ACallOnAContainerThatDoesNotAnswerSSHAnswersNotAnsweringNeverUnknown(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"next dev","cwd":"."}]}`)
	c.Silent = true
	srv, _ := migrationToolServer(t, c, statusActive)

	a := callDevServer(t, srv, map[string]any{"action": "status", "hostname": "appdev"})
	if a.Reason != "supervisor_unresponsive" || !strings.Contains(a.Message, "appdev did not answer over SSH in time") ||
		!strings.Contains(a.Message, "zerops_scale") || !strings.Contains(a.Message, "zerops_manage action=restart") {
		t.Errorf("status = %+v, want supervisor_unresponsive naming the host, zerops_scale and a restart", a)
	}
}

// The move's final start passes the start's memory rule: when the server the
// redeploy brought up has ended, a single-part app on a nearly full 0.5 GiB
// grant has no server running, so its start reaches the supervisor and answers
// how the server ended, never a memory refusal.
func TestDevServer_MigrationsFinalStartWithNoServerRunning_IsNotRefusedOnMemory(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup,
		MemMaxB: 512 << 20, MemCurrentB: 400 << 20, Crash: map[string]bool{"appdev": true}}
	srv, _ := migrationToolServer(t, c, statusActive)

	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000})
	if text := getTextContent(t, result); strings.Contains(text, "Not starting") {
		t.Fatalf("the move's final start was refused on memory: %s", text)
	}
	finalStart := slices.ContainsFunc(c.Sent(), func(cmd string) bool { return strings.Contains(cmd, "ctl 'start' 'appdev'") })
	if c.Deploys != 1 || !finalStart {
		t.Errorf("the final start must reach the supervisor after the one redeploy (deploys %d)", c.Deploys)
	}
}

// variantBDevMeta is webdev's pair recorded as variant B (docs/spec-mate.md
// §2.8a.4a): its repository on the code volume vol at /var/www/ws, which the
// further dev service apidev (setup dev-api) mounts too.
func variantBDevMeta() *workflow.ServiceMeta {
	return &workflow.ServiceMeta{
		Hostname: "webdev", Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}}, Mode: topology.PlanModeStandard,
		BootstrapSession: "sess1", BootstrappedAt: "2026-10-15", PrimarySetupName: "dev",
		CodeVolume: &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
			DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}},
	}
}

// variantBDevSetup is apidev's dev setup before the supervisor.
const variantBDevSetup = `zerops:
  - setup: dev-api
    build:
      base: go@1
      deployFiles: [.]
    run:
      base: go@1
      volume:
        hostname: vol
        mountPath: /var/www/ws
      ports:
        - port: 8080
          httpSupport: true
      start: zsc noop --silent
`

// TestDevServer_FirstStartOnAVariantBDevService_RunsTheSupervisorOnTheCodeVolume
// (docs/spec-mate.md §2.8a.2): moving a variant-B dev service onto the
// supervisor edits the dev setup where its repository lives, on the code
// volume, makes run.start `zcp-devsup run --root <mountPath>` so the
// supervisor reads the shared declaration, and redeploys from there.
func TestDevServer_FirstStartOnAVariantBDevService_RunsTheSupervisorOnTheCodeVolume(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: variantBDevSetup}
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, variantBDevMeta()); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "apidev", ProjectID: "proj-1", Status: serviceStatusRunning,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
			{ID: "svc-vol", Name: "vol", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1", ServiceStackTypeCategoryName: "STANDARD"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-api", Status: statusActive, Sequence: 2}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{
		Client: mock, ProjectID: "proj-1", SSH: c, StateDir: dir,
		AuthInfo: &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		Devsup:   DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }},
	})

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "apidev", "command": "go run ./apps/api", "port": 8080})
	if a.Reason == reasonMigrationDeployFailed || c.Deploys != 1 {
		t.Errorf("the redeploy did not run once (%d): %+v", c.Deploys, a)
	}
	if !strings.HasPrefix(a.Message, `zcp moved apidev onto the dev-server supervisor: setup "dev-api"`) {
		t.Errorf("message = %q", a.Message)
	}
	if !strings.Contains(c.YAML, "start: zcp-devsup run --root /var/www/ws\n") || !strings.Contains(c.YAML, testDevsupURL) {
		t.Errorf("zerops.yml after the move:\n%s", c.YAML)
	}
	var read, wrote, pushed bool
	for _, cmd := range c.Sent() {
		read = read || (strings.Contains(cmd, "@@zcp-file") && strings.Contains(cmd, "'/var/www/ws'/\"$f\""))
		wrote = wrote || strings.Contains(cmd, "'/var/www/ws/zerops.yml.zcp-tmp'")
		pushed = pushed || (strings.Contains(cmd, "zcli push") && strings.HasPrefix(cmd, "cd '/var/www/ws' &&"))
	}
	if !read || !wrote || !pushed {
		t.Errorf("the dev setup is read %v, written %v and redeployed %v on the code volume, want all three:\n%s", read, wrote, pushed, strings.Join(c.Sent(), "\n"))
	}
}

// TestDevServer_DeclarationOfAVariantBDevService_LivesOnTheCodeVolume
// (docs/spec-mate.md §2.8a.1): the file the supervisor reads is the volume's
// .zcp/dev-servers.json, so the declaration zcp writes for a variant-B dev
// service goes there, never to /var/www/.zcp.
func TestDevServer_DeclarationOfAVariantBDevService_LivesOnTheCodeVolume(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: variantBDevSetup}
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, variantBDevMeta()); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "apidev", ProjectID: "proj-1", Status: serviceStatusRunning,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
			{ID: "svc-vol", Name: "vol", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1", ServiceStackTypeCategoryName: "STANDARD"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-api", Status: statusActive, Sequence: 2}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{
		Client: mock, ProjectID: "proj-1", SSH: c, StateDir: dir,
		AuthInfo: &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		Devsup:   DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }},
	})

	callDevServer(t, srv, map[string]any{"action": "start", "hostname": "apidev", "command": "go run ./apps/api", "port": 8080, "workDir": "/var/www/ws/apps/api"})

	var onVolume, onVarWww bool
	for _, cmd := range c.Sent() {
		onVolume = onVolume || strings.Contains(cmd, "'/var/www/ws/.zcp/dev-servers.json.tmp'")
		onVarWww = onVarWww || strings.Contains(cmd, "'/var/www/.zcp")
	}
	if !onVolume || onVarWww {
		t.Errorf("declaration written on the volume %v, on /var/www/.zcp %v:\n%s", onVolume, onVarWww, strings.Join(c.Sent(), "\n"))
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

// TestDevServer_MigrationWaitsForDirtyCrewCopies (orchestrator decision C,
// review hole 6): the move onto the supervisor never redeploys while a crew
// copy holds uncommitted or untracked work — one sentence names the copies
// and what to do, and nothing changes.
func TestDevServer_MigrationWaitsForDirtyCrewCopies(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup, DirtyCrew: []string{"ada", "bob"}}
	srv, _ := migrationToolServer(t, c, statusActive)

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000})
	if a.Running || a.Reason != reasonCrewWorkUnsaved {
		t.Fatalf("answer = %+v, want %s", a, reasonCrewWorkUnsaved)
	}
	if !strings.Contains(a.Message, ".crew/ada, .crew/bob") || !strings.Contains(a.Message, "commit it in each copy") || strings.Count(a.Message, ". ") != 0 {
		t.Errorf("message = %q, want one sentence naming the dirty copies and what to do", a.Message)
	}
	if c.Deploys != 0 || c.YAML != todaysDevSetup || c.Declaration != "" {
		t.Errorf("something changed: deploys %d, declaration %q, zerops.yml:\n%s", c.Deploys, c.Declaration, c.YAML)
	}

	c.DirtyCrew = nil
	if a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000}); !a.Running || c.Deploys != 1 {
		t.Errorf("with the crew work committed the move did not run (deploys %d): %+v", c.Deploys, a)
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
			ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result, deployListener(context.Background(), c, dir, "app", result)...)
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

// TestDeploy_KeptDevServer_ComesBackUnderTheSupervisor (orchestrator
// decision B, review hole 4b): the dev server an older zcp's keeper kept on a
// dev service still comes back after zcp deploys it. The deploy of a dev
// service with a keeper record and no supervisor carries the move — the two
// lines, the kept server declared from the record — so the supervisor starts
// it with no extra redeploy; the record goes only after that, and a failed
// deploy keeps it.
func TestDeploy_KeptDevServer_ComesBackUnderTheSupervisor(t *testing.T) {
	t.Parallel()
	const record = `{
  "appdev": {"hostname": "appdev", "command": "bun --watch index.ts", "port": 3000, "healthPath": "/health", "container": "c-1", "startedAt": "2026-10-13T10:00:00Z"},
  "apidev": {"hostname": "apidev", "command": "go run .", "port": 8080, "container": "c-2", "startedAt": "2026-10-13T10:00:00Z"}
}`
	for _, tc := range []struct{ tool, build string }{
		{"zerops_deploy", statusActive}, {"zerops_deploy", statusBuildFailed}, {"zerops_deploy_batch", statusActive},
	} {
		build := tc.build
		t.Run(tc.tool+" "+build, func(t *testing.T) {
			t.Parallel()
			// The batch's pre-flight reads the dev setup from the source mount
			// beside the state directory.
			root := t.TempDir()
			dir := filepath.Join(root, ".zcp", "state")
			if err := os.MkdirAll(filepath.Join(root, "appdev"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "appdev", "zerops.yml"), []byte(todaysDevSetup), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1",
				BootstrappedAt: "2026-10-13", PrimarySetupName: "dev", FirstDeployedAt: "2026-10-13T09:00:00Z",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			recordPath := filepath.Join(dir, "dev-servers.json")
			if err := os.WriteFile(recordPath, []byte(record), 0o600); err != nil {
				t.Fatal(err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}}}).
				WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: build, Sequence: 2}})
			c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
			src := DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }}
			args := map[string]any{"targetService": "appdev", "setup": "dev"}
			if tc.tool == "zerops_deploy" {
				RegisterDeploySSH(srv, mock, okHTTP, "proj-1", c, authInfo, nil, runtime.Info{}, dir, testDeployEngine(t), nil, src)
			} else {
				RegisterDeployBatch(srv, mock, okHTTP, "proj-1", c, authInfo, nil, runtime.Info{InContainer: true}, dir, testDeployEngine(t), nil, src)
				args = map[string]any{"targets": []any{args}}
			}

			text := getTextContent(t, callTool(t, srv, tc.tool, args))
			if c.Deploys != 1 {
				t.Fatalf("deploys = %d, want the one asked for: %s", c.Deploys, text)
			}
			d, err := devsup.ParseDeclaration([]byte(c.Declaration))
			if err != nil || len(d.Servers) != 1 || d.Servers[0].Name != "appdev" || d.Servers[0].Command != "bun --watch index.ts" || d.Servers[0].Port != 3000 || d.Servers[0].Cwd != "." {
				t.Fatalf("declaration = %s (%v), want the kept server", c.Declaration, err)
			}
			if !strings.Contains(c.YAML, "start: zcp-devsup run") || !strings.Contains(c.YAML, testDevsupURL) {
				t.Errorf("the deploy did not carry the supervisor lines:\n%s", c.YAML)
			}
			after, _ := os.ReadFile(recordPath)
			if build == statusBuildFailed {
				if string(after) != record {
					t.Errorf("a failed deploy dropped the keeper record:\n%s", after)
				}
				return
			}
			if c.Status("appdev") != devsup.StatusRunning {
				t.Errorf("kept server after the deploy = %q, want running under the supervisor", c.Status("appdev"))
			}
			if !strings.Contains(text, `declared \"appdev\", the dev server zcp kept there`) {
				t.Errorf("the deploy does not say what changed: %s", text)
			}
			if strings.Contains(string(after), `"appdev"`) || !strings.Contains(string(after), `"apidev"`) {
				t.Errorf("record after the deploy = %s, want appdev's entry gone and apidev's kept", after)
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

// TestRetireDevServerKeeper (TEST-PLAN E1 #14, orchestrator decision B): an
// upgraded zcp removes the deleted keeper's devservers- units and nothing
// else; its dev-servers.json record stays until each host's deploy moves the
// kept server onto the supervisor.
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
	if err := RetireDevServerKeeper(context.Background(), units); err != nil {
		t.Fatalf("RetireDevServerKeeper: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("startup removed the keeper record of hosts not on the supervisor yet: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another state file went with it: %v", err)
	}
	if !slices.Equal(units.removed, []string{"devservers-"}) {
		t.Errorf("units removed = %q, want the keeper's devservers- units", units.removed)
	}
	// A second run finds nothing left and is no error.
	if err := RetireDevServerKeeper(context.Background(), units); err != nil {
		t.Errorf("second run: %v", err)
	}
	units.err = errors.New("systemctl: boom")
	if err := RetireDevServerKeeper(context.Background(), units); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a unit removal failure is not reported: %v", err)
	}
}

// TestDevServer_PartsEnvelopeOnlyBehindTheGate (TEST-PLAN E1 #16): until
// every mate zcp drives accepts per-server state, the result is today's —
// no envelope, no devServers[].
func TestDevServer_PartsEnvelopeOnlyBehindTheGate(t *testing.T) {
	t.Parallel()
	for _, carries := range []bool{false, true} {
		mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}}})
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
		RegisterDevServer(srv, DevServerDeps{Client: mock, ProjectID: "proj-1", StateDir: t.TempDir(), CarriesParts: carries,
			SSH: devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun index.ts","cwd":"."}]}`)})
		text := getTextContent(t, callTool(t, srv, "zerops_dev_server", map[string]any{"action": "status", "hostname": "appdev"}))
		if got := strings.Contains(text, `"devServers"`); got != carries {
			t.Errorf("CarriesParts=%v: result carries devServers = %v\n%s", carries, got, text)
		}
		if !carries && strings.Contains(text, `"envelope"`) {
			t.Errorf("an envelope rides the result before the gate opens:\n%s", text)
		}
	}
}

// TestDeploy_NewVariantBDevSetup_RunsTheSupervisorOnTheCodeVolume
// (docs/spec-mate.md §2.8a.2): the first deploy of a variant-B dev service
// runs from the code volume, so the supervisor's lines go into the dev setup
// there, and run.start is `zcp-devsup run --root <mountPath>`.
func TestDeploy_NewVariantBDevSetup_RunsTheSupervisorOnTheCodeVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, variantBDevMeta()); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "apidev", ProjectID: "proj-1", Status: serviceStatusRunning,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
			{ID: "svc-vol", Name: "vol", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1", ServiceStackTypeCategoryName: "STANDARD"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-api", Status: statusActive, Sequence: 1}})
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: variantBDevSetup}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", c, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		nil, runtime.Info{}, dir, testDeployEngine(t), nil, DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }})

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "apidev", "setup": "dev-api"})
	if result.IsError {
		t.Fatalf("zerops_deploy: %s", getTextContent(t, result))
	}
	if !strings.Contains(c.YAML, "start: zcp-devsup run --root /var/www/ws\n") || !strings.Contains(c.YAML, testDevsupURL) {
		t.Errorf("zerops.yml after the first deploy:\n%s", c.YAML)
	}
	var read, wrote bool
	for _, cmd := range c.Sent() {
		read = read || (strings.Contains(cmd, "@@zcp-file") && strings.Contains(cmd, "'/var/www/ws'/\"$f\""))
		wrote = wrote || strings.Contains(cmd, "'/var/www/ws/zerops.yml.zcp-tmp'")
	}
	if !read || !wrote {
		t.Errorf("the dev setup is read %v and written %v on the code volume, want both:\n%s", read, wrote, strings.Join(c.Sent(), "\n"))
	}
}

// TestDeploy_NewDevSetup_GetsTheSupervisorLines (docs/spec-mate.md §2.8a.2,
// orchestrator decision for mono-z-devtool-2): the first deploy of a dev
// service writes the supervisor's two lines into its dev setup when a
// published release ships zcp-devsup or ZCP_DEVSUP_URL names one, so its
// first dev-server start redeploys nothing. Without either, or for a stage or
// an already deployed dev service, the dev setup keeps today's shape and the
// first start migrates it.
func TestDeploy_NewDevSetup_GetsTheSupervisorLines(t *testing.T) {
	t.Parallel()
	const override = "https://storage.example.com/devsup/zcp-devsup-linux-amd64"
	tests := []struct {
		name     string
		latest   string
		override string
		deployed bool
		target   string
		wantURL  string
	}{
		{name: "a published release ships it", latest: testDevsupURL, target: "appdev", wantURL: testDevsupURL},
		{name: "ZCP_DEVSUP_URL names a build", override: override, target: "appdev", wantURL: override},
		{name: "no release ships it yet", target: "appdev"},
		{name: "the dev service was deployed before", latest: testDevsupURL, deployed: true, target: "appdev"},
		{name: "the stage of the pair", latest: testDevsupURL, target: "appstage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			meta := &workflow.ServiceMeta{
				Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}, Mode: topology.PlanModeStandard,
				BootstrapSession: "sess1", BootstrappedAt: "2026-10-14", PrimarySetupName: "dev",
			}
			if tt.deployed {
				meta.FirstDeployedAt = "2026-10-14T20:00:00Z"
			}
			if err := workflow.WriteServiceMeta(dir, meta); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
						ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}},
					{ID: "svc-2", Name: "appstage", ProjectID: "proj-1", Status: serviceStatusRunning,
						ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}},
				}).
				WithAppVersionEvents([]platform.AppVersionEvent{
					{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
					{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
				})
			c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", c, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
				nil, runtime.Info{}, dir, testDeployEngine(t), nil, DevsupSource{
					Latest:   func(context.Context) (string, error) { return tt.latest, nil },
					Override: tt.override,
				})

			result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": tt.target, "setup": "dev"})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("zerops_deploy: %s", text)
			}
			if c.Deploys != 1 {
				t.Errorf("deploys = %d, want 1", c.Deploys)
			}
			supervised := strings.Contains(c.YAML, "start: zcp-devsup run")
			if tt.wantURL == "" {
				if c.YAML != todaysDevSetup {
					t.Errorf("the dev setup changed:\n%s", c.YAML)
				}
				return
			}
			if !supervised || !strings.Contains(c.YAML, tt.wantURL) || !strings.Contains(c.YAML, "httpSupport: true") {
				t.Errorf("zerops.yml after the first deploy:\n%s", c.YAML)
			}
			if !strings.Contains(text, `setup \"dev\" in zerops.yml now installs and runs zcp-devsup`) {
				t.Errorf("the deploy does not say what changed: %s", text)
			}
			RegisterDevServer(srv, DevServerDeps{Client: mock, ProjectID: "proj-1", SSH: c, StateDir: dir,
				AuthInfo: &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}})
			a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000})
			if !a.Running || c.Deploys != 1 || strings.Contains(a.Message, "zcp moved") {
				t.Errorf("first start after the first deploy: %+v, deploys = %d; want it running with no redeploy", a, c.Deploys)
			}
		})
	}
}

// TestDeploy_ADeployedDevSetupPinnedToAnOlderSupervisor_InstallsTheNewestInTheSameDeploy
// (docs/spec-mate.md §2.8a.2): a dev service whose dev setup an older zcp
// pinned to an older zcp-devsup gets its install line re-pinned to the newest
// release before a deploy the agent asked for, so that deploy installs it and
// nothing redeploys on its own. A pin that is already the newest stays as
// written and the deploy says nothing about it.
func TestDeploy_ADeployedDevSetupPinnedToAnOlderSupervisor_InstallsTheNewestInTheSameDeploy(t *testing.T) {
	t.Parallel()
	const older = "https://github.com/zeropsio/zcp/releases/download/v9.200.0/zcp-devsup-linux-amd64"
	tests := []struct {
		name    string
		pinned  string
		wantNew bool
	}{
		{name: "pinned to an older release", pinned: older, wantNew: true},
		{name: "pinned to the newest release", pinned: testDevsupURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1",
				BootstrappedAt: "2026-10-14", PrimarySetupName: "dev", FirstDeployedAt: "2026-10-14T20:00:00Z",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}}}).
				WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1}})
			pinnedSetup, _, _, err := ops.AdoptSupervisorYAML([]byte(todaysDevSetup), "dev", "appdev", tt.pinned, "")
			if err != nil {
				t.Fatalf("AdoptSupervisorYAML: %v", err)
			}
			c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: string(pinnedSetup)}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", c, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
				nil, runtime.Info{}, dir, testDeployEngine(t), nil, DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }})

			result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "setup": "dev"})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("zerops_deploy: %s", text)
			}
			if c.Deploys != 1 {
				t.Errorf("deploys = %d, want 1: the re-pin rides the deploy the agent asked for", c.Deploys)
			}
			if !tt.wantNew {
				if c.YAML != string(pinnedSetup) || strings.Contains(text, "re-pinned") {
					t.Errorf("a newest pin changed:\n%s\n%s", c.YAML, text)
				}
				return
			}
			want, _, _, err := ops.AdoptSupervisorYAML([]byte(todaysDevSetup), "dev", "appdev", testDevsupURL, "")
			if err != nil {
				t.Fatalf("AdoptSupervisorYAML: %v", err)
			}
			if c.YAML != string(want) {
				t.Errorf("zerops.yml after the deploy:\n%s\nwant the newest pin and nothing else changed:\n%s", c.YAML, want)
			}
			if !strings.Contains(text, `zcp re-pinned zcp-devsup in setup \"dev\" of zerops.yml from v9.200.0 to v9.300.0, so this deploy installs it`) {
				t.Errorf("the deploy does not say what changed: %s", text)
			}
		})
	}
}
