package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// partsRepos answers the parts-signal read per hostname and records who was read.
type partsRepos struct {
	mu   sync.Mutex
	out  map[string]string
	read []string
}

func (p *partsRepos) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	if !strings.Contains(command, "@@yaml") {
		return nil, nil
	}
	p.mu.Lock()
	p.read = append(p.read, host)
	p.mu.Unlock()
	return []byte(p.out[host]), nil
}

func (p *partsRepos) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return p.ExecSSH(ctx, hostname, command)
}

func (p *partsRepos) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestAttachPartsSignal: in a container, every dev half's repo is read for the
// multi-part signal — never a stage, a managed service or anything in local
// mode — and only a multi-part repo marks its dev half.
func TestAttachPartsSignal(t *testing.T) {
	t.Parallel()
	threeParts := "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n@@declaration\n@@markers\n@@packages\n"
	onePart := "@@yaml\nzerops:\n  - setup: dev\n  - setup: prod\n@@declaration\n@@markers\n@@packages\n"
	services := func() []workflow.ServiceSnapshot {
		return []workflow.ServiceSnapshot{
			{Hostname: "monodev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev"},
			{Hostname: "webstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
			{Hostname: "appdev", Mode: topology.ModeDev, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev"},
			{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
		}
	}
	repos := &partsRepos{out: map[string]string{"monodev": threeParts, "appdev": onePart, "webstage": threeParts}}
	got := services()
	attachPartsSignal(context.Background(), got, repos, runtime.Info{InContainer: true}, nil)
	want := map[string]bool{"monodev": true, "webstage": false, "appdev": false, "db": false}
	for _, s := range got {
		if s.MultiPart != want[s.Hostname] {
			t.Errorf("%s: MultiPart = %v, want %v", s.Hostname, s.MultiPart, want[s.Hostname])
		}
	}
	if len(repos.read) != 2 {
		t.Errorf("read %v, want the two dev halves only", repos.read)
	}

	local := &partsRepos{out: repos.out}
	got = services()
	attachPartsSignal(context.Background(), got, local, runtime.Info{}, nil)
	if len(local.read) != 0 || got[0].MultiPart {
		t.Errorf("local mode read %v", local.read)
	}
}

// TestAttachPartsSignal_DeclaredBasesReachThePairStages: a dev half whose
// repo's zerops.yaml declares its bases marks itself and every stage of its
// pair, since the stages deploy setups of that same zerops.yaml; a repo that
// declares none marks nothing.
func TestAttachPartsSignal_DeclaredBasesReachThePairStages(t *testing.T) {
	t.Parallel()
	withBases := "@@yaml\nzerops:\n  - setup: base\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n  - setup: dev\n    extends: base\n  - setup: web\n    extends: base\n  - setup: api\n    extends: base\n@@declaration\n@@markers\n@@packages\n"
	noBases := "@@yaml\nzerops:\n  - setup: dev\n    run:\n      start: zsc noop\n@@declaration\n@@markers\n@@packages\n"
	got := []workflow.ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev",
			Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}},
		{Hostname: "webstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "apistage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "otherdev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev", StageHostname: "otherstage"},
		{Hostname: "otherstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
	}
	repos := &partsRepos{out: map[string]string{"appdev": withBases, "otherdev": noBases}}
	attachPartsSignal(context.Background(), got, repos, runtime.Info{InContainer: true}, nil)
	want := map[string]bool{"appdev": true, "webstage": true, "apistage": true, "otherdev": false, "otherstage": false}
	for _, s := range got {
		if s.DeclaresBase != want[s.Hostname] {
			t.Errorf("%s: DeclaresBase = %v, want %v", s.Hostname, s.DeclaresBase, want[s.Hostname])
		}
	}
}

// TestMultiPartRouteNote: the route menu names the monorepo playbook only for
// an existing runtime service whose repo is multi-part.
func TestMultiPartRouteNote(t *testing.T) {
	t.Parallel()
	stack := func(name, typ string) platform.ServiceStack {
		return platform.ServiceStack{Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	existing := []platform.ServiceStack{stack("appdev", "nodejs@22"), stack("db", "postgresql@17")}
	multi := "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n@@declaration\n@@markers\n@@packages\n"
	single := "@@yaml\nzerops:\n  - setup: dev\n@@declaration\n@@markers\n@@packages\n"
	rt := runtime.Info{InContainer: true}

	got := multiPartRouteNote(context.Background(), existing, &partsRepos{out: map[string]string{"appdev": multi}}, rt, nil)
	if !strings.Contains(got, "`appdev`") || !strings.Contains(got, `uri="zerops://playbooks/monorepo"`) {
		t.Errorf("multi-part note = %q", got)
	}
	repos := &partsRepos{out: map[string]string{"appdev": single, "db": multi}}
	if got := multiPartRouteNote(context.Background(), existing, repos, rt, nil); got != "" {
		t.Errorf("single-part note = %q, want none", got)
	}
	if len(repos.read) != 1 {
		t.Errorf("read %v, want appdev only", repos.read)
	}
}

// TestMultiPartRouteNote_AStageIsNeverNamedAsARepo: a service a pair records as
// its stage holds what its setup deployed, not a repo — a stage that deployed
// the whole monorepo is neither read nor named, only the pair's dev half is.
func TestMultiPartRouteNote_AStageIsNeverNamedAsARepo(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "documensodev",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "documensostage", SetupName: "prod"}, {Hostname: "docsstage", SetupName: "docs"}},
	}); err != nil {
		t.Fatal(err)
	}
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	stack := func(name string) platform.ServiceStack {
		return platform.ServiceStack{Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@24"}}
	}
	existing := []platform.ServiceStack{stack("documensodev"), stack("documensostage"), stack("docsstage")}
	multi := "@@yaml\nzerops:\n  - setup: dev\n  - setup: prod\n  - setup: docs\n@@declaration\n@@markers\n@@packages\n"
	repos := &partsRepos{out: map[string]string{"documensodev": multi, "documensostage": multi, "docsstage": multi}}

	got := multiPartRouteNote(context.Background(), existing, repos, runtime.Info{InContainer: true}, engine)
	if !strings.Contains(got, "The repo of `documensodev` holds") {
		t.Errorf("note = %q, want it to name documensodev alone", got)
	}
	if strings.Contains(got, "stage`") {
		t.Errorf("note names a stage: %q", got)
	}
	if len(repos.read) != 1 || repos.read[0] != "documensodev" {
		t.Errorf("read %v, want documensodev only", repos.read)
	}
}

// TestMultiPartRouteNote_PoolAndBoxAreNeverRead: a read by service hostname
// reaches a random pool container, and a Box holds no repo of the app — neither
// is read for a parts signal nor named (PS-1, PS-3).
func TestMultiPartRouteNote_PoolAndBoxAreNeverRead(t *testing.T) {
	t.Parallel()
	stack := func(name, typ string) platform.ServiceStack {
		return platform.ServiceStack{Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	existing := []platform.ServiceStack{stack("appdev", "nodejs@22"), stack("swarmpool", "swarm@1"), stack("box", "ubuntu/zbox@1")}
	multi := "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n@@declaration\n@@markers\n@@packages\n"
	repos := &partsRepos{out: map[string]string{"appdev": multi, "swarmpool": multi, "box": multi}}

	got := multiPartRouteNote(context.Background(), existing, repos, runtime.Info{InContainer: true}, nil)
	if !strings.Contains(got, "`appdev`") || strings.Contains(got, "swarmpool") || strings.Contains(got, "`box`") {
		t.Errorf("note = %q, want it to name appdev alone", got)
	}
	if len(repos.read) != 1 || repos.read[0] != "appdev" {
		t.Errorf("read %v, want appdev only", repos.read)
	}
}

// TestAttachPartsSignal_ZeropsYamlReachesThePairStages: a dev half whose repo
// carries its zerops.yaml marks itself and every stage of its pair — the
// stages deploy setups of that same file — so a first deploy of it gets no
// guidance that scaffolds a zerops.yaml or writes the app; a repo without one
// marks nothing.
func TestAttachPartsSignal_ZeropsYamlReachesThePairStages(t *testing.T) {
	t.Parallel()
	withFile := "@@file\nzerops.yml\n@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n@@declaration\n@@markers\n@@packages\n"
	noFile := "@@file\n@@yaml\n@@declaration\n@@markers\n@@packages\n"
	got := []workflow.ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev",
			Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}}},
		{Hostname: "webstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "newdev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev", StageHostname: "newstage"},
		{Hostname: "newstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
	}
	repos := &partsRepos{out: map[string]string{"appdev": withFile, "newdev": noFile}}
	attachPartsSignal(context.Background(), got, repos, runtime.Info{InContainer: true}, nil)
	want := map[string]bool{"appdev": true, "webstage": true, "newdev": false, "newstage": false}
	for _, s := range got {
		if s.HasZeropsYAML != want[s.Hostname] {
			t.Errorf("%s: HasZeropsYAML = %v, want %v", s.Hostname, s.HasZeropsYAML, want[s.Hostname])
		}
	}
}
