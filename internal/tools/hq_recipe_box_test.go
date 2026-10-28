// Tests for: tools/hq_recipe_inputs.go — the Zerops Box the group recipe
// carries for the runtimes that read it, read off the Mate's live project.
package tools

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// noolaRunnerYAML is the runner's part of Noola's zerops.yaml (fxck/noola):
// both setups reach the project's Box — the workspace API at its address on
// the project network, its key by reference.
const noolaRunnerYAML = `zerops:
  - setup: runnerdev
    build:
      base: go@1.22
      buildCommands:
        - go build -o runner ./runner
      deployFiles: ./
    run:
      base: go@1.22
      envVariables:
        NATS_PORT: ${broker_port}
        BOX_API_URL: http://box:8080
        BOX_API_KEY: ${box_ZBOX_WORKSPACE_API_KEY}
      start: zsc noop
  - setup: runnerprod
    build:
      base: go@1.22
      buildCommands:
        - go build -o runner ./runner
      deployFiles: ./runner
    run:
      base: go@1.22
      envVariables:
        NATS_PORT: ${broker_port}
        BOX_API_URL: http://box:8080
        BOX_API_KEY: ${box_ZBOX_WORKSPACE_API_KEY}
      start: ./runner
`

// liveBox is a Zerops Box of a Mate's project: the platform reports it in
// the category of a database.
func liveBox(id, name string) platform.ServiceStack {
	box := liveRuntime(id, name, "ubuntu/zbox@1")
	box.ServiceStackTypeInfo.ServiceStackTypeCategoryName = "STANDARD"
	return box
}

// noolaRunnerProject is the Noola Mate's project as far as the runner goes:
// its pair, the broker it reads, the Box it runs jobs on, and zcp — and
// whatever else a test runs beside them.
func noolaRunnerProject(extra ...platform.ServiceStack) *platform.Mock {
	broker := platform.ServiceStack{ID: "svc-broker", Name: "broker", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nats@2.12", ServiceStackTypeCategoryName: "USER"}}
	services := append([]platform.ServiceStack{
		liveRuntime("svc-runnerdev", "runnerdev", "go@1.22"),
		liveRuntime("svc-runnerstage", "runnerstage", "go@1.22"),
		liveRuntime("svc-zcp", "zcp", "zcp@1"),
		broker,
	}, extra...)
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "noola-mate", Status: "ACTIVE", Mode: "LIGHT"}).
		WithServicesDirect(services).
		WithServices(services)
}

// writeNoolaRunnerMeta seeds the runner pair the repository pass wired, both
// halves deployed.
func writeNoolaRunnerMeta(t *testing.T, stateDir string) []*workflow.ServiceMeta {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "runnerdev",
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "runnerstage", SetupName: "runnerprod"}},
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-20",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        testHQAddress + "/git/" + testHQOrg + "/" + labApp + "/runnerdev.git",
		PrimarySetupName: "runnerdev",
		HQ:               &workflow.HQRepoRef{AppID: labApp, Repo: "runnerdev"},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	return metas
}

// tierServiceTypes reads a tier's services as hostname → type.
func tierServiceTypes(t *testing.T, body string) map[string]string {
	t.Helper()
	var doc struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
			Type     string `yaml:"type"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("tier does not parse: %v\n%s", err, body)
	}
	out := map[string]string{}
	for _, svc := range doc.Services {
		out[svc.Hostname] = svc.Type
	}
	return out
}

// Noola's runner (live 2026-10-28): its setups name the project's Box, and
// the recipe zcp composed carried the runner in every tier and the Box in
// none, so a project born from any tier got a runner with nothing to run a
// job on. Read off the live project and composed, every tier that carries
// the runner carries the Box — keyed on the Box's type, written as a Box
// import takes it. Spec: spec-workflows.md §8 PS-3.
func TestGroupRecipe_NoolaRunner_EveryTierCarriesTheBox(t *testing.T) {
	metas := writeNoolaRunnerMeta(t, t.TempDir())
	readMain := func(_ context.Context, repo string) string {
		if repo == "runnerdev" {
			return noolaRunnerYAML
		}
		return ""
	}
	inputs, readWarnings, err := composeGroupRecipeInputs(context.Background(), noolaRunnerProject(liveBox("svc-box", "box")),
		"p1", "noola", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, readMain)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v", err)
	}
	layout, warnings, err := bundle.BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	want := map[string]string{"0 — AI Agent": "runnerstage", "3 — Stage": "runner", "4 — Small Production": "runner"}
	for _, tier := range layout.Tiers {
		dir := fmt.Sprintf("%d — %s", tier.Index, tier.Title)
		services := tierServiceTypes(t, tier.ImportYAML)
		if _, ok := services[want[dir]]; !ok {
			t.Errorf("%s does not carry the runner %q:\n%s", dir, want[dir], tier.ImportYAML)
		}
		if got := services["box"]; got != "ubuntu/zbox@1" {
			t.Errorf("%s: box = %q, want the Box (ubuntu/zbox@1) the runner reads:\n%s", dir, got, tier.ImportYAML)
		}
		delete(want, dir)
	}
	if len(want) > 0 {
		t.Errorf("tiers not composed: %v", want)
	}
	if all := append(readWarnings, warnings...); warningsHave(all, `"box"`) {
		t.Errorf("warnings %v speak of the Box as left out", all)
	}
}

// A Box is known by its type, never by its hostname: in the Zerops Apps
// Mate `box` is an ordinary alpine/static runtime, which the recipe carries
// as what it is — here a utility built from a public repository — and a Box
// is a Box under any name.
func TestComposeGroupRecipeInputs_BoxKeyedOnType_NotHostname(t *testing.T) {
	metas := writeNoolaRunnerMeta(t, t.TempDir())
	static := publicBuildRuntime("svc-box", "box", "alpine/static", "https://github.com/zerops-recipe-apps/box-app")
	inputs, _, err := composeGroupRecipeInputs(context.Background(), noolaRunnerProject(static, liveBox("svc-sandbox", "sandbox")),
		"p1", "noola", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, nil)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v", err)
	}
	var boxes, utilities []string
	for _, b := range inputs.Boxes {
		boxes = append(boxes, b.Hostname)
	}
	for _, u := range inputs.Utilities {
		utilities = append(utilities, u.Hostname)
	}
	if !slices.Equal(boxes, []string{"sandbox"}) {
		t.Errorf("boxes = %v, want [sandbox] — the service of the Box type, whatever its name", boxes)
	}
	if !slices.Equal(utilities, []string{"box"}) {
		t.Errorf("utilities = %v, want [box] — an alpine/static runtime named box is no Box", utilities)
	}
}
