package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The monorepo contract's shared fixtures (docs/spec-mate.md §2.8a.7). zcp
// owns them; Mate keeps a hashed byte copy and decodes the same files. Each
// shape below is the contract's: strict decoding refuses a key the contract
// does not name, so a fixture cannot drift from §2.8a unnoticed. A lane that
// starts emitting a shape renders its case through the real handler instead.

const mateContractDir = "testdata/mate-contract"

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
		Watch   *struct {
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

func TestMateContractFixtures_Declaration_FollowsTheRules(t *testing.T) {
	t.Parallel()
	var decl contractDeclaration
	readContractFixture(t, "dev-servers.declaration.json", &decl)
	if decl.Version != 1 {
		t.Errorf("version = %d, want 1", decl.Version)
	}
	nameRe := regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	names, ports := map[string]bool{}, map[int]bool{}
	got := make([]string, 0, len(decl.Servers))
	for _, s := range decl.Servers {
		got = append(got, s.Name)
		if !nameRe.MatchString(s.Name) || names[s.Name] {
			t.Errorf("name %q must match %s and be unique", s.Name, nameRe)
		}
		names[s.Name] = true
		if s.Port != 0 {
			if ports[s.Port] || s.Port > 65535 {
				t.Errorf("port %d must be unique and valid", s.Port)
			}
			ports[s.Port] = true
		}
		if s.Command == "" {
			t.Errorf("%s: command is required", s.Name)
		}
		if clean := filepath.Clean(s.Cwd); filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			t.Errorf("%s: cwd %q leaves the repo", s.Name, s.Cwd)
		}
	}
	if want := []string{"web", "api", "worker"}; !reflect.DeepEqual(got, want) {
		t.Errorf("servers = %v, want %v", got, want)
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
