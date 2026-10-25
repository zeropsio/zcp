package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The monorepo contract's shared fixtures (docs/spec-mate.md §2.8a.7). zcp
// owns them; Mate keeps a hashed byte copy and decodes the same files. Each
// shape below is the contract's: strict decoding refuses a key the contract
// does not name, so a fixture cannot drift from §2.8a unnoticed. A lane that
// starts emitting a shape renders its case through the real handler instead.

const mateContractDir = "testdata/mate-contract"

// contractStatusRunning is the supervisor's running status (§2.8a.2).
const contractStatusRunning = "running"

type contractStage struct {
	Hostname string `json:"hostname"`
	Setup    string `json:"setup,omitempty"`
}

type contractVerify struct {
	OK         bool      `json:"ok"`
	HTTPStatus int       `json:"httpStatus,omitempty"`
	At         time.Time `json:"at"`
}

type contractExit struct {
	Code   *int      `json:"code,omitempty"`
	Signal string    `json:"signal,omitempty"`
	At     time.Time `json:"at"`
}

type contractDevServer struct {
	Name     string          `json:"name"`
	Port     int             `json:"port,omitempty"`
	URL      string          `json:"url,omitempty"`
	Status   string          `json:"status"`
	ReadAt   time.Time       `json:"readAt"`
	Verify   *contractVerify `json:"verify,omitempty"`
	LastExit *contractExit   `json:"lastExit,omitempty"`
}

type contractSnapshot struct {
	workflow.ServiceSnapshot
	Stages     []contractStage     `json:"stages,omitempty"`
	DevServers []contractDevServer `json:"devServers,omitempty"`
}

type contractEnvelope struct {
	workflow.StateEnvelope
	Services []contractSnapshot `json:"services"`
}

type contractDevServerResult struct {
	ops.DevServerResult
	Name     string            `json:"name,omitempty"`
	Envelope *contractEnvelope `json:"envelope,omitempty"`
}

type contractDeclaration struct {
	Version int `json:"version"`
	Servers []struct {
		Name    string `json:"name"`
		Port    int    `json:"port,omitempty"`
		Command string `json:"command"`
		Cwd     string `json:"cwd,omitempty"`
		Service string `json:"service,omitempty"`
		// Autostart is the server's first desired state; absent means true.
		Autostart *bool `json:"autostart,omitempty"`
		Watch     *struct {
			Paths  []string `json:"paths"`
			Ignore []string `json:"ignore,omitempty"`
		} `json:"watch,omitempty"`
	} `json:"servers"`
}

type contractReceiptPart struct {
	Setup           string    `json:"setup"`
	PreviewHostname string    `json:"previewHostname"`
	Deploy          string    `json:"deploy"`
	DeployedAt      time.Time `json:"deployedAt"`
	WorkspaceClean  bool      `json:"workspaceClean"`
	DeployedCommit  string    `json:"deployedCommit,omitempty"`
	Verified        bool      `json:"verified"`
}

type contractChange struct {
	hq.Change
	Receipt *struct {
		At    time.Time             `json:"at"`
		Parts []contractReceiptPart `json:"parts"`
	} `json:"receipt"`
}

// readContractFixture decodes one fixture strictly into out.
func readContractFixture(t *testing.T, name string, out any) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(mateContractDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("%s does not match the contract shape: %v", name, err)
	}
	return raw
}

// singlePairTodayEnvelope is today's envelope for one bun pair, as
// ComputeEnvelope projects it from a ServiceMeta with one stage.
func singlePairTodayEnvelope() workflow.StateEnvelope {
	at := time.Date(2026, 10, 14, 21, 4, 0, 0, time.UTC)
	pairDims := func(s workflow.ServiceSnapshot) workflow.ServiceSnapshot {
		s.TypeVersion = "bun@1.3"
		s.RuntimeClass = topology.RuntimeDynamic
		s.Status = platform.ServiceStatusActive
		s.Bootstrapped = true
		s.Deployed = true
		s.CloseDeployMode = topology.CloseModeAuto
		s.GitPushState = topology.GitPushConfigured
		s.BuildIntegration = topology.BuildIntegrationNone
		return s
	}
	return workflow.StateEnvelope{
		Phase:       workflow.PhaseDevelopActive,
		Environment: workflow.EnvContainer,
		SelfService: &workflow.SelfService{Hostname: "zcp"},
		Project:     workflow.ProjectSummary{ID: "proj-weather", Name: "weather"},
		Services: []workflow.ServiceSnapshot{
			pairDims(workflow.ServiceSnapshot{
				Hostname:       "appdev",
				Mode:           topology.ModeStandard,
				StageHostname:  "appstage",
				SetupName:      "dev",
				StageSetupName: "prod",
			}),
			pairDims(workflow.ServiceSnapshot{
				Hostname:  "appstage",
				Mode:      topology.ModeStage,
				SetupName: "prod",
			}),
		},
		Generated: at,
	}
}

func TestMateContractFixtures_SinglePairToday_IsTheEnvelopeZcpMarshalsToday(t *testing.T) {
	t.Parallel()
	var env workflow.StateEnvelope
	raw := readContractFixture(t, "envelope.single-pair.today.json", &env)
	want, err := json.Marshal(singlePairTodayEnvelope())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got bytes.Buffer
	if err := json.Compact(&got, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got.String() != string(want) {
		t.Errorf("fixture differs from today's wire bytes:\n got %s\nwant %s", got.String(), want)
	}
}

func TestMateContractFixtures_SinglePair_StagesMeanTodaysPair(t *testing.T) {
	t.Parallel()
	var today workflow.StateEnvelope
	readContractFixture(t, "envelope.single-pair.today.json", &today)
	var env contractEnvelope
	raw := readContractFixture(t, "envelope.single-pair.json", &env)

	for _, legacy := range []string{`"stageHostname"`, `"stageSetupName"`} {
		if bytes.Contains(raw, []byte(legacy)) {
			t.Errorf("after the field order's step 3 zcp no longer emits %s", legacy)
		}
	}
	if len(env.Services) != len(today.Services) {
		t.Fatalf("services: got %d, today %d", len(env.Services), len(today.Services))
	}
	for i, svc := range env.Services {
		was := today.Services[i]
		var stages []contractStage
		if was.StageHostname != "" {
			stages = []contractStage{{Hostname: was.StageHostname, Setup: was.StageSetupName}}
		}
		if !reflect.DeepEqual(svc.Stages, stages) {
			t.Errorf("%s stages = %+v, today's pair is %+v", svc.Hostname, svc.Stages, stages)
		}
		was.StageHostname, was.StageSetupName = "", ""
		if !reflect.DeepEqual(svc.ServiceSnapshot, was) {
			t.Errorf("%s: every other key keeps today's value:\n got %+v\nwant %+v", svc.Hostname, svc.ServiceSnapshot, was)
		}
	}
	dev := env.Services[0]
	if len(dev.DevServers) != 1 || dev.DevServers[0].Name != dev.Hostname || dev.DevServers[0].Port != 3000 || dev.DevServers[0].Status != "running" {
		t.Errorf("a one-server dev service reports one dev server under its hostname, got %+v", dev.DevServers)
	}
	if env.Services[1].DevServers != nil || env.Services[1].Stages != nil {
		t.Errorf("only the pair's dev snapshot carries stages and dev servers, got %+v", env.Services[1])
	}
}

func TestMateContractFixtures_ThreeParts_CarriesEveryPreviewAndServer(t *testing.T) {
	t.Parallel()
	var env contractEnvelope
	raw := readContractFixture(t, "envelope.three-parts.json", &env)
	if bytes.Contains(raw, []byte(`"stageHostname"`)) || bytes.Contains(raw, []byte(`"stageSetupName"`)) {
		t.Error("a three-part envelope carries stages[] only")
	}
	hosts := map[string]bool{}
	for _, s := range env.Services {
		hosts[s.Hostname] = true
	}
	dev := env.Services[0]
	wantStages := []contractStage{{"webstage", "web"}, {"apistage", "api"}, {"workerstage", "worker"}}
	if !reflect.DeepEqual(dev.Stages, wantStages) {
		t.Errorf("stages = %+v, want %+v", dev.Stages, wantStages)
	}
	for _, s := range dev.Stages {
		if !hosts[s.Hostname] {
			t.Errorf("stage %s has no snapshot of its own", s.Hostname)
		}
	}
	read := time.Date(2026, 10, 14, 21, 4, 0, 0, time.UTC)
	type row struct {
		name, status string
		port         int
	}
	got := make([]row, 0, len(dev.DevServers))
	for _, d := range dev.DevServers {
		got = append(got, row{d.Name, d.Status, d.Port})
		if !d.ReadAt.Equal(read) {
			t.Errorf("%s readAt = %s, want %s", d.Name, d.ReadAt, read)
		}
	}
	want := []row{{"web", "running", 3000}, {"api", "running", 8080}, {"worker", "crashed", 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dev servers = %+v, want %+v", got, want)
	}
	worker := dev.DevServers[2]
	if worker.LastExit == nil || worker.LastExit.Code == nil || *worker.LastExit.Code != 1 || worker.URL != "" {
		t.Errorf("the crashed portless worker carries its exit and no URL, got %+v", worker)
	}
	if web := dev.DevServers[0]; web.Verify == nil || !web.Verify.OK || web.Verify.HTTPStatus != 200 || !strings.HasPrefix(web.URL, "https://appdev-") {
		t.Errorf("web carries its probe and public URL, got %+v", web)
	}
}

func TestMateContractFixtures_DevServerResults_KeepTodaysFieldsAndAddTheEnvelope(t *testing.T) {
	t.Parallel()
	var one contractDevServerResult
	readContractFixture(t, "dev-server.one-server.json", &one)
	if one.Name != "" || one.Hostname != "appdev" || !one.Running || one.Port != 3000 {
		t.Errorf("one server: today's fields, no name, got %+v", one)
	}
	if one.Envelope == nil || len(one.Envelope.Services[0].DevServers) != 1 || one.Envelope.Services[0].DevServers[0].Name != "appdev" {
		t.Errorf("one server: the envelope reports it under the hostname, got %+v", one.Envelope)
	}

	var three contractDevServerResult
	readContractFixture(t, "dev-server.three-servers-one-crashed.json", &three)
	if three.Name != "worker" || three.Running || three.Action != "status" {
		t.Errorf("three servers: the result names the server it reports, got %+v", three)
	}
	if three.Envelope == nil || len(three.Envelope.Services[0].DevServers) != 3 {
		t.Fatalf("three servers: the envelope carries all of them, got %+v", three.Envelope)
	}
}

func TestMateContractFixtures_DevServerResults_RunningIsTheReportedServersStatus(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"dev-server.one-server.json", "dev-server.three-servers-one-crashed.json", "dev-server.running-http-404.json"} {
		var res contractDevServerResult
		readContractFixture(t, name, &res)
		if res.Envelope == nil {
			t.Fatalf("%s: no envelope", name)
		}
		reported := res.Name
		if reported == "" {
			reported = res.Hostname
		}
		var entry *contractDevServer
		dev := contractService(t, res.Envelope, res.Hostname)
		for i, d := range dev.DevServers {
			if d.Name == reported {
				entry = &dev.DevServers[i]
			}
		}
		if entry == nil {
			t.Fatalf("%s: the envelope carries no dev server named %q", name, reported)
		}
		if running := entry.Status == contractStatusRunning; res.Running != running {
			t.Errorf("%s: running = %v, but %s's status is %q", name, res.Running, reported, entry.Status)
		}
	}
}

func TestMateContractFixtures_Declaration_FollowsTheRules(t *testing.T) {
	t.Parallel()
	// The shape is strict here; the rules are the supervisor's own parser.
	raw := readContractFixture(t, "dev-servers.declaration.json", &contractDeclaration{})
	decl, err := devsup.ParseDeclaration(raw)
	if err != nil {
		t.Fatalf("the supervisor refuses the declaration: %v", err)
	}
	if decl.Version != 1 {
		t.Errorf("version = %d, want 1", decl.Version)
	}
	got := make([]string, 0, len(decl.Servers))
	for _, s := range decl.Servers {
		got = append(got, s.Name)
	}
	if want := []string{"web", "api", "worker", "admin"}; !reflect.DeepEqual(got, want) {
		t.Errorf("servers = %v, want %v", got, want)
	}
}

func TestMateContractFixtures_Declaration_AutostartFalseStartsStopped(t *testing.T) {
	t.Parallel()
	var decl contractDeclaration
	readContractFixture(t, "dev-servers.declaration.json", &decl)
	var started, stopped []string
	for _, s := range decl.Servers {
		if s.Autostart == nil || *s.Autostart {
			started = append(started, s.Name)
		} else {
			stopped = append(stopped, s.Name)
		}
	}
	if want := []string{"web", "api", "worker"}; !reflect.DeepEqual(started, want) {
		t.Errorf("first desired running = %v, want %v", started, want)
	}
	if want := []string{"admin"}; !reflect.DeepEqual(stopped, want) {
		t.Errorf("first desired stopped = %v, want %v", stopped, want)
	}
}

func TestMateContractFixtures_Receipt_CommitOnlyFromACleanTree(t *testing.T) {
	t.Parallel()
	var change contractChange
	readContractFixture(t, "hq-change.receipt.json", &change)
	if change.Receipt == nil || change.Head == nil {
		t.Fatal("the change carries a head and a receipt")
	}
	setups := make([]string, 0, len(change.Receipt.Parts))
	for _, p := range change.Receipt.Parts {
		setups = append(setups, p.Setup+"/"+p.PreviewHostname+"/"+p.Deploy)
		if p.DeployedCommit != "" && (p.Deploy != "ok" || !p.WorkspaceClean) {
			t.Errorf("%s: a commit is written only for an ok deploy from a clean tree", p.Setup)
		}
	}
	want := []string{"web/webstage/ok", "api/apistage/ok", "worker/workerstage/failed"}
	if !reflect.DeepEqual(setups, want) {
		t.Errorf("parts = %v, want %v", setups, want)
	}
	if web := change.Receipt.Parts[0]; web.DeployedCommit == "" || web.DeployedCommit == *change.Head {
		t.Errorf("the fixture shows a preview older than the change head, got %q vs %q", web.DeployedCommit, *change.Head)
	}
}

// TestMateContractFixtures_DevServerResults_RenderedByTheHandler (TEST-PLAN
// E1 #15/#16, zcp side): with the parts envelope on, zerops_dev_server
// renders each dev-server fixture — its own fields, the dev service's
// stages[] and devServers[] — through the real handler; the read times are
// the call's own, so they are checked against the call instead.
func TestMateContractFixtures_DevServerResults_RenderedByTheHandler(t *testing.T) {
	t.Parallel()
	crashedAt := time.Date(2026, 10, 14, 21, 3, 10, 0, time.UTC)
	tests := []struct {
		fixture string
		decl    string
		stages  []topology.StageRef
		args    map[string]any
		setup   func(c *devservertest.Container)
	}{
		{
			fixture: "dev-server.one-server.json",
			decl:    `{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun --hot index.ts","cwd":"."}]}`,
			stages:  []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
			args:    map[string]any{"action": "status", "hostname": "appdev"},
		},
		{
			fixture: "dev-server.three-servers-one-crashed.json",
			decl: `{"version":1,"servers":[{"name":"web","port":3000,"command":"bun --hot index.ts","cwd":"apps/web"},` +
				`{"name":"api","port":8080,"command":"go run .","cwd":"apps/api"},` +
				`{"name":"worker","command":"bun worker.ts","cwd":"apps/worker"}]}`,
			stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}},
			args:   map[string]any{"action": "status", "hostname": "appdev", "name": "worker"},
			setup: func(c *devservertest.Container) {
				c.Servers["worker"].Status = "crashed"
				c.Servers["worker"].Pid = 0
				c.Servers["worker"].Exit = &devsup.Exit{Code: 1, At: crashedAt}
				c.Logs = map[string][]string{"worker": {"Error: QUEUE_URL is not set"}}
			},
		},
		{
			fixture: "dev-server.running-http-404.json",
			decl: `{"version":1,"servers":[{"name":"web","port":3000,"command":"bun --hot index.ts","cwd":"apps/web"},` +
				`{"name":"api","port":8080,"command":"go run .","cwd":"apps/api"}]}`,
			stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
			args:   map[string]any{"action": "status", "hostname": "appdev", "name": "api"},
			setup: func(c *devservertest.Container) {
				c.HTTP = map[int]int{8080: 404}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()
			var want contractDevServerResult
			readContractFixture(t, tt.fixture, &want)

			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: tt.stages, PrimarySetupName: "dev",
				BootstrapSession: "s", BootstrappedAt: "2026-10-14",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			services := make([]platform.ServiceStack, 0, 1+len(tt.stages))
			services = append(services, platform.ServiceStack{ID: "svc-dev", Name: "appdev", ProjectID: "proj-weather", Status: serviceStatusRunning, SubdomainAccess: true,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3", ServiceStackTypeCategoryName: "USER"},
				Ports:                []platform.Port{{Port: 3000, HTTPSupport: true}, {Port: 8080, HTTPSupport: true}}})
			for _, st := range tt.stages {
				services = append(services, platform.ServiceStack{ID: "svc-" + st.Hostname, Name: st.Hostname, ProjectID: "proj-weather", Status: serviceStatusRunning,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3", ServiceStackTypeCategoryName: "USER"}})
			}
			mock := platform.NewMock().WithServices(services).
				WithProject(&platform.Project{ID: "proj-weather", Name: "weather", SubdomainHost: "1df2.prg1.zerops.app"})
			c := devservertest.NewSupervised(tt.decl)
			if tt.setup != nil {
				tt.setup(c)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDevServer(srv, DevServerDeps{Client: mock, ProjectID: "proj-weather", SSH: c, StateDir: dir,
				RtInfo: runtime.Info{InContainer: true, ServiceName: "zcp"}, CarriesParts: true})

			before := time.Now().UTC()
			result := callTool(t, srv, "zerops_dev_server", tt.args)
			after := time.Now().UTC()
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("zerops_dev_server: %s", text)
			}
			var got contractDevServerResult
			dec := json.NewDecoder(strings.NewReader(text))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("the handler's result does not match the contract shape: %v\n%s", err, text)
			}
			if got.Envelope == nil {
				t.Fatalf("no envelope with the parts envelope on:\n%s", text)
			}
			if !reflect.DeepEqual(got.DevServerResult, want.DevServerResult) || got.Name != want.Name {
				t.Errorf("result fields\n got %+v name=%q\nwant %+v name=%q", got.DevServerResult, got.Name, want.DevServerResult, want.Name)
			}
			dev := contractService(t, got.Envelope, "appdev")
			if dev.StageHostname != "" || dev.StageSetupName != "" {
				t.Errorf("the dev half still carries today's stage keys: %+v", dev.ServiceSnapshot)
			}
			wantDev := contractService(t, want.Envelope, "appdev")
			if !reflect.DeepEqual(dev.Stages, wantDev.Stages) {
				t.Errorf("stages = %+v, want %+v", dev.Stages, wantDev.Stages)
			}
			for i := range dev.DevServers {
				d := &dev.DevServers[i]
				if d.ReadAt.Before(before.Truncate(time.Second)) || d.ReadAt.After(after) {
					t.Errorf("%s read at %v, outside the call [%v, %v]", d.Name, d.ReadAt, before, after)
				}
				d.ReadAt = wantDev.DevServers[0].ReadAt
				if d.Verify != nil {
					d.Verify.At = d.ReadAt
				}
			}
			if !reflect.DeepEqual(dev.DevServers, wantDev.DevServers) {
				g, _ := json.Marshal(dev.DevServers)
				w, _ := json.Marshal(wantDev.DevServers)
				t.Errorf("devServers\n got %s\nwant %s", g, w)
			}
		})
	}
}

// contractService is the envelope's snapshot of hostname.
func contractService(t *testing.T, env *contractEnvelope, hostname string) contractSnapshot {
	t.Helper()
	for _, s := range env.Services {
		if s.Hostname == hostname {
			return s
		}
	}
	t.Fatalf("the envelope has no %s", hostname)
	return contractSnapshot{}
}

// TestMateContractFixtures_PoolPair_StagesRideThePoolSnapshot pins the
// envelope of a pair keyed by its dev pool (docs/spec-workflows.md §13): the
// pool's snapshot keys the pair, so stages[] rides it; devServers[] never
// does (a pool's dev servers run in its leases and ride leases[]); and the
// bytes are what zcp computes from a dev/stage pair re-keyed onto its pool.
func TestMateContractFixtures_PoolPair_StagesRideThePoolSnapshot(t *testing.T) {
	t.Parallel()
	var fixture contractEnvelope
	raw := readContractFixture(t, "envelope.pool-pair.json", &fixture)

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard,
		Stages:          []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushConfigured, BuildIntegration: topology.BuildIntegrationNone,
		PrimarySetupName: "dev", BootstrappedAt: "2026-10-14T20:00:00Z", FirstDeployedAt: "2026-10-14T20:30:00Z",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if _, err := workflow.MigratePairToPool(dir, workflow.PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "pool"}); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "apppool", ProjectID: "proj-weather", Status: platform.ServiceStatusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-appstage", Name: "appstage", ProjectID: "proj-weather", Status: platform.ServiceStatusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithProject(&platform.Project{ID: "proj-weather", Name: "weather"})
	lease := workflow.LeaseSnapshot{Alias: "apppool-1", Pool: "apppool", Branch: "feat-x", ContainerHostname: "node-id-1.runtime.apppool.zerops", State: "held"}
	env, err := workflow.ComputeEnvelope(context.Background(), mock, dir, "proj-weather",
		runtime.Info{InContainer: true, ServiceName: "zcp"}, time.Date(2026, 10, 14, 21, 4, 0, 0, time.UTC),
		workflow.WithLeaseReader(func(context.Context) ([]workflow.LeaseSnapshot, error) { return []workflow.LeaseSnapshot{lease}, nil }))
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	want, err := json.Marshal(env.WithParts(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got bytes.Buffer
	if err := json.Compact(&got, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got.String() != string(want) {
		t.Errorf("fixture differs from the envelope zcp computes:\n got %s\nwant %s", got.String(), want)
	}

	if len(fixture.Services) != 2 {
		t.Fatalf("services = %d, want the pool and its stage", len(fixture.Services))
	}
	pool, stage := fixture.Services[0], fixture.Services[1]
	if pool.Hostname != "apppool" || pool.RuntimeClass != topology.RuntimePool || pool.Mode != "" {
		t.Errorf("services[0] = %+v, want the pool, modeless", pool.ServiceSnapshot)
	}
	if !reflect.DeepEqual(pool.Stages, []contractStage{{Hostname: "appstage", Setup: "prod"}}) || pool.DevServers != nil {
		t.Errorf("the pool carries stages %+v and devServers %+v, want [{appstage prod}] and none", pool.Stages, pool.DevServers)
	}
	if stage.Stages != nil || stage.Mode != topology.ModeStage {
		t.Errorf("the stage = %+v stages %+v, want a stage half carrying no stages", stage.ServiceSnapshot, stage.Stages)
	}
	if leases := fixture.LeaseList(); len(leases) != 1 || leases[0].Pool != "apppool" {
		t.Errorf("leases = %+v, want the pool's one lease", leases)
	}
}

// TestMateContractFixtures_LeaseStatus_RenderedByTheHandler pins the envelope a
// zerops_lease action=status result carries (docs/spec-mate.md §2.8a.3): the
// lease status read carries the activity and unsaved work it read; the lease
// of a stopped pool, which status did not read, carries activity with only
// acquiredAt and why in note, and no unsaved. The bytes are what the real
// handler renders, the read times being the call's own.
func TestMateContractFixtures_LeaseStatus_RenderedByTheHandler(t *testing.T) {
	t.Parallel()
	var fixture contractEnvelope
	raw := readContractFixture(t, "envelope.lease-status.json", &fixture)

	hs := httptest.NewServer(&fakeOrchestrator{reserved: true})
	t.Cleanup(hs.Close)
	stateDir := t.TempDir()
	writeContractLeaseRecord(t, stateDir, "devpool", "svc-pool", "c-abc", 3, "feat-a", "https://github.com/example/weather.git")
	writeContractLeaseRecord(t, stateDir, "stoppool", "svc-stop", "c-2", 2, "feat-b", "https://github.com/example/weather-admin")
	token := []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-weather", Name: "weather"}).
		WithServices([]platform.ServiceStack{swarmPoolService("svc-pool", "devpool", "ACTIVE"), swarmPoolService("svc-stop", "stoppool", "STOPPED")}).
		WithServiceEnv("svc-pool", token).WithServiceEnv("svc-stop", token)
	factory := func(baseURL, tok string) ops.SwarmAPI {
		if baseURL == "http://orch.devpool.zerops" {
			return platform.NewSwarmClient(hs.URL, tok, hs.Client())
		}
		return platform.NewSwarmClient(baseURL, tok, &http.Client{Transport: dnsFailTransport{}})
	}
	changed := time.Date(2026, 10, 23, 19, 12, 40, 0, time.UTC)
	ssh := &leaseStubSSH{fn: func(command string) ([]byte, error) {
		if strings.Contains(command, "@@zcp-lease activity") {
			return fmt.Appendf(nil, "@@zcp-lease activity %d \n@@zcp-lease status\n M src/App.tsx\n?? notes.txt\n@@zcp-lease unpushed\n2\n", changed.Unix()), nil
		}
		return nil, nil
	}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-weather", stateDir, factory, nil, ssh, runtime.Info{InContainer: true, ServiceName: "zcp"})

	before := time.Now().UTC()
	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	after := time.Now().UTC()
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status: %s", text)
	}
	var result struct {
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil || result.Envelope == nil {
		t.Fatalf("no envelope in the status result (%v):\n%s", err, text)
	}
	var got contractEnvelope
	dec := json.NewDecoder(bytes.NewReader(result.Envelope))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("the handler's envelope does not match the contract shape: %v\n%s", err, result.Envelope)
	}

	// The read times are the call's own: check them against the call, then
	// put the fixture's in their place.
	var times struct {
		Generated string `json:"generated"`
		Leases    []struct {
			Activity *struct {
				ReadAt string `json:"readAt"`
			} `json:"activity"`
		} `json:"leases"`
	}
	if err := json.Unmarshal(result.Envelope, &times); err != nil {
		t.Fatalf("times: %v", err)
	}
	if got.Generated.Before(before) || got.Generated.After(after) {
		t.Errorf("generated %v outside the call [%v, %v]", got.Generated, before, after)
	}
	rendered := bytes.Replace(result.Envelope, []byte(`"generated":"`+times.Generated+`"`),
		[]byte(`"generated":"`+fixture.Generated.Format(time.RFC3339Nano)+`"`), 1)
	for i, l := range times.Leases {
		if l.Activity == nil || l.Activity.ReadAt == "" {
			continue
		}
		readAt, err := time.Parse(time.RFC3339, l.Activity.ReadAt)
		if err != nil || readAt.Before(before.Truncate(time.Second)) || readAt.After(after) {
			t.Errorf("leases[%d] read at %q, outside the call [%v, %v]", i, l.Activity.ReadAt, before, after)
		}
		want := fixture.LeaseList()[i].Activity.ReadAt
		rendered = bytes.Replace(rendered, []byte(`"readAt":"`+l.Activity.ReadAt+`"`), []byte(`"readAt":"`+want+`"`), 1)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if compact.String() != string(rendered) {
		t.Errorf("fixture differs from the envelope zcp renders:\n got %s\nwant %s", compact.String(), rendered)
	}

	leases := fixture.LeaseList()
	if len(leases) != 2 || leases[0].Alias != "devpool-3" || leases[1].Alias != "stoppool-2" {
		t.Fatalf("leases = %+v, want devpool-3 and stoppool-2", leases)
	}
	// Each lease names its group's repository — canonical, credential-free —
	// beside its branch: a client binds and rebinds by both (§6.3).
	var repos struct {
		Leases []struct {
			Repo string `json:"repo"`
		} `json:"leases"`
	}
	if err := json.Unmarshal(raw, &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos.Leases) != 2 || repos.Leases[0].Repo != "https://github.com/example/weather" || repos.Leases[1].Repo != "https://github.com/example/weather-admin" {
		t.Errorf("leases' repos = %+v, want each record's repository in canonical form", repos.Leases)
	}
	read, unread := leases[0], leases[1]
	if read.Activity == nil || read.Activity.LastChangeAt != "2026-10-23T19:12:40Z" || read.Activity.AcquiredAt != read.AcquiredAt || read.Activity.Note != "" {
		t.Errorf("devpool-3 activity = %+v, want lastChangeAt 2026-10-23T19:12:40Z, its acquiredAt and no note", read.Activity)
	}
	if !reflect.DeepEqual(read.Unsaved, &topology.LeaseUnsaved{Files: []string{" M src/App.tsx", "?? notes.txt"}, FileCount: 2, UnpushedCommits: 2}) {
		t.Errorf("devpool-3 unsaved = %+v, want two files and two commits", read.Unsaved)
	}
	if !unread.Unverified || unread.Unsaved != nil || unread.Activity == nil ||
		unread.Activity.LastChangeAt != "" || unread.Activity.ReadAt != "" || unread.Activity.Note == "" {
		t.Errorf("stoppool-2 = %+v activity %+v, want unverified, no unsaved, activity with only why it was not read", unread, unread.Activity)
	}
}

// writeContractLeaseRecord is writeLeaseRecordFile for a lease of repo: the
// record keeps the repository's identity as an acquire writes it.
func writeContractLeaseRecord(t *testing.T, stateDir, pool, poolServiceID, containerID string, number int, branch, repo string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"poolHostname": pool, "poolServiceId": poolServiceID, "containerId": containerID,
		"containerNumber": number, "containerHostname": fmt.Sprintf("node-id-%d.runtime.%s.zerops", number, pool),
		"lease": leaseTestLease, "branch": branch, "repo": topology.CanonicalRepoURL(repo), "acquiredAt": "2026-10-23T18:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
