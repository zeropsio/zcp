// Tests for: tools/workflow_parts_plan.go — a bootstrap plan over a mounted
// repo takes its stages from the repo's parts.
package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestBootstrapPlan_MountedRepoParts_ReadBeforeThePlanIsAccepted: a mounted
// repo's parts are read before the bootstrap plan is accepted. A standard
// pair over a repo whose zerops.yml declares several parts has one stage per
// part, each with its part's setup; a plan with a generic stage is refused
// with the plan zcp made from the repo, on the classic and the adopt route,
// before any service is created. A single-part repo, a dev-only plan and a
// plan outside a container stay as they are.
func TestBootstrapPlan_MountedRepoParts_ReadBeforeThePlanIsAccepted(t *testing.T) {
	t.Parallel()
	webAPI := "@@yaml\nzerops:\n  - setup: base\n  - setup: dev\n    extends: base\n  - setup: web\n    extends: base\n  - setup: api\n    extends: base\n@@declaration\n@@markers\npnpm-workspace.yaml\n@@packages\napps/web/package.json\napps/api/package.json\n"
	onePart := "@@yaml\nzerops:\n  - setup: dev\n  - setup: prod\n@@declaration\n@@markers\n@@packages\n"
	pair := func(stage workflow.RuntimeTarget) []workflow.BootstrapTarget {
		stage.DevHostname, stage.Type, stage.IsExisting = "appdev", "ubuntu/nodejs@22", true
		if stage.BootstrapMode == "" {
			stage.BootstrapMode = topology.PlanModeStandard
		}
		return []workflow.BootstrapTarget{{Runtime: stage}}
	}
	generic := pair(workflow.RuntimeTarget{ExplicitStage: "appstage"})
	perPart := pair(workflow.RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}})
	devOnly := pair(workflow.RuntimeTarget{BootstrapMode: topology.PlanModeDev})
	container := runtime.Info{InContainer: true}

	tests := []struct {
		name        string
		route       workflow.BootstrapRoute
		repo        string
		plan        []workflow.BootstrapTarget
		rt          runtime.Info
		wantRefused bool
	}{
		{name: "classic, a generic stage over web and api", route: workflow.BootstrapRouteClassic, repo: webAPI, plan: generic, rt: container, wantRefused: true},
		{name: "adopt, a generic stage over web and api", route: workflow.BootstrapRouteAdopt, repo: webAPI, plan: generic, rt: container, wantRefused: true},
		{name: "classic, one stage per part", route: workflow.BootstrapRouteClassic, repo: webAPI, plan: perPart, rt: container},
		{name: "classic, a single-part repo", route: workflow.BootstrapRouteClassic, repo: onePart, plan: generic, rt: container},
		{name: "classic, dev only", route: workflow.BootstrapRouteClassic, repo: webAPI, plan: devOnly, rt: container},
		{name: "classic, outside a container", route: workflow.BootstrapRouteClassic, repo: webAPI, plan: generic, rt: runtime.Info{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, engine := submitPlanOverRepo(t, tt.route, "appdev", tt.repo, tt.plan, tt.rt)
			refused := strings.Contains(text, "declares the parts")
			if refused != tt.wantRefused {
				t.Fatalf("refused for its parts = %v, want %v:\n%s", refused, tt.wantRefused, text)
			}
			if !tt.wantRefused {
				return
			}
			for _, want := range []string{
				`\"web\"`, `\"api\"`, "appstage",
				`{\"hostname\":\"webstage\",\"setupName\":\"web\"}`,
				`{\"hostname\":\"apistage\",\"setupName\":\"api\"}`,
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %s:\n%s", want, text)
				}
			}
			if strings.Contains(text, "not found in project") {
				t.Errorf("the parts are named before the stage's absence:\n%s", text)
			}
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			if state.Bootstrap.Plan != nil {
				t.Errorf("a refused plan is not recorded: %+v", state.Bootstrap.Plan)
			}
		})
	}
}

// TestBootstrapPlan_RepoSetups_RefusesOnlyAStageMatchingNoPart: the plan gate
// refuses only a stage that matches no part of the repo — one naming no setup
// or a setup the repo declares that is no part of it. A pair whose stages each
// name a part is accepted even when another setup has no stage: an app with
// environment setups and a recipe pair keep working as before the gate.
func TestBootstrapPlan_RepoSetups_RefusesOnlyAStageMatchingNoPart(t *testing.T) {
	t.Parallel()
	environments := "@@yaml\nzerops:\n  - setup: dev\n  - setup: stage\n  - setup: prod\n@@declaration\n@@markers\n@@packages\n"
	recipe := "@@yaml\nzerops:\n  - setup: appdev\n  - setup: appstage\n@@declaration\n@@markers\n@@packages\n"
	webAPI := "@@yaml\nzerops:\n  - setup: base\n  - setup: dev\n    extends: base\n  - setup: web\n    extends: base\n  - setup: api\n    extends: base\n@@declaration\n@@markers\npnpm-workspace.yaml\n@@packages\napps/web/package.json\napps/api/package.json\n"
	webAPIOverAdmin := strings.ReplaceAll(webAPI, "base", "admin")
	pair := func(dev string, r workflow.RuntimeTarget) []workflow.BootstrapTarget {
		r.DevHostname, r.Type, r.IsExisting, r.BootstrapMode = dev, "ubuntu/nodejs@22", true, topology.PlanModeStandard
		return []workflow.BootstrapTarget{{Runtime: r}}
	}
	container := runtime.Info{InContainer: true}

	tests := []struct {
		name        string
		dev         string
		repo        string
		plan        []workflow.BootstrapTarget
		wantRefused []string
	}{
		{name: "a single app with dev, stage and prod setups is accepted with one stage naming one of them, prod", dev: "appdev", repo: environments,
			plan: pair("appdev", workflow.RuntimeTarget{ExplicitStage: "appprod", StageSetupName: "prod"})},
		{name: "a single app with dev, stage and prod setups is accepted with one stage naming one of them, stage", dev: "appdev", repo: environments,
			plan: pair("appdev", workflow.RuntimeTarget{ExplicitStage: "appstage", StageSetupName: "stage"})},
		{name: "a single app with dev, stage and prod setups is accepted with one stage naming no setup", dev: "appdev", repo: environments,
			plan: pair("appdev", workflow.RuntimeTarget{ExplicitStage: "appstage"})},
		{name: "a recipe pair whose stage names its setup is accepted", dev: "nodedev", repo: recipe,
			plan: pair("nodedev", workflow.RuntimeTarget{ExplicitStage: "nodestage", StageSetupName: "appstage"})},
		{name: "a recipe pair whose stage names its setup is accepted, with the dev's setup known", dev: "nodedev", repo: recipe,
			plan: pair("nodedev", workflow.RuntimeTarget{PrimarySetupName: "appdev", ExplicitStage: "nodestage", StageSetupName: "appstage"})},
		{name: "a stage that names no setup over a repo of several parts is refused with the plan built from the repo", dev: "appdev", repo: webAPI,
			plan:        pair("appdev", workflow.RuntimeTarget{ExplicitStage: "appstage"}),
			wantRefused: []string{`stage \"appstage\" names no part`, `{\"hostname\":\"webstage\",\"setupName\":\"web\"}`, `{\"hostname\":\"apistage\",\"setupName\":\"api\"}`}},
		{name: "a stage naming a setup that is no part of the repo is refused with the plan built from the repo", dev: "appdev", repo: webAPIOverAdmin,
			plan:        pair("appdev", workflow.RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "adminstage", SetupName: "admin"}}}),
			wantRefused: []string{`stage \"adminstage\" names the setup \"admin\", no part of the repo`, `{\"hostname\":\"webstage\",\"setupName\":\"web\"}`, `{\"hostname\":\"apistage\",\"setupName\":\"api\"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, _ := submitPlanOverRepo(t, workflow.BootstrapRouteClassic, tt.dev, tt.repo, tt.plan, container)
			refused := strings.Contains(text, "declares the parts")
			if refused != (len(tt.wantRefused) > 0) {
				t.Fatalf("refused for its parts = %v, want %v:\n%s", refused, len(tt.wantRefused) > 0, text)
			}
			for _, want := range tt.wantRefused {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %s:\n%s", want, text)
				}
			}
			if _, proposed, _ := strings.Cut(text, "plan="); strings.Contains(proposed, "adminstage") {
				t.Errorf("the plan built from the repo keeps a stage that matches no part:\n%s", text)
			}
		})
	}
}

// TestBootstrapPlan_AddingAPartToAMultiPartRepo_KeepsItsStage: a part the
// agent adds to a repo of several parts has its stage planned before develop
// writes the part's setup into the repo's zerops.yml. A stage whose setup the
// file does not declare yet is accepted on the classic and the adopt route
// and is kept in a plan made from the repo; a stage naming a setup the file
// declares that is no part — the dev setup, a base only extended — is
// refused.
func TestBootstrapPlan_AddingAPartToAMultiPartRepo_KeepsItsStage(t *testing.T) {
	t.Parallel()
	webAPI := "@@yaml\nzerops:\n  - setup: base\n  - setup: dev\n    extends: base\n  - setup: web\n    extends: base\n  - setup: api\n    extends: base\n@@declaration\n@@markers\n@@packages\n"
	pair := func(stages ...topology.StageRef) []workflow.BootstrapTarget {
		return []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{
			DevHostname: "appdev", Type: "ubuntu/nodejs@22", IsExisting: true,
			BootstrapMode: topology.PlanModeStandard, Stages: stages,
		}}}
	}
	web, api := topology.StageRef{Hostname: "webstage", SetupName: "web"}, topology.StageRef{Hostname: "apistage", SetupName: "api"}
	worker := topology.StageRef{Hostname: "workerstage", SetupName: "worker"}
	container := runtime.Info{InContainer: true}

	for _, route := range []workflow.BootstrapRoute{workflow.BootstrapRouteClassic, workflow.BootstrapRouteAdopt} {
		t.Run(string(route)+", the new part's stage is accepted", func(t *testing.T) {
			t.Parallel()
			text, engine := submitPlanOverRepo(t, route, "appdev", webAPI, pair(web, api, worker), container, "webstage", "apistage", "workerstage")
			if strings.Contains(text, "declares the parts") {
				t.Fatalf("the plan adding the worker is refused for its parts:\n%s", text)
			}
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			if state.Bootstrap.Plan == nil || len(state.Bootstrap.Plan.Targets) != 1 {
				t.Fatalf("the plan is not recorded with its one runtime:\n%s", text)
			}
			if got := state.Bootstrap.Plan.Targets[0].Runtime.StageRefs(); !slices.Equal(got, []topology.StageRef{web, api, worker}) {
				t.Errorf("recorded stages = %+v, want webstage(web), apistage(api), workerstage(worker)", got)
			}
		})
		t.Run(string(route)+", a refused plan keeps the new part's stage", func(t *testing.T) {
			t.Parallel()
			devStage := topology.StageRef{Hostname: "devstage", SetupName: "dev"}
			baseStage := topology.StageRef{Hostname: "basestage", SetupName: "base"}
			text, _ := submitPlanOverRepo(t, route, "appdev", webAPI, pair(web, devStage, baseStage, worker), container, "webstage", "workerstage")
			for _, want := range []string{
				`stage \"devstage\" names the setup \"dev\", no part of the repo`,
				`stage \"basestage\" names the setup \"base\", no part of the repo`,
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %s:\n%s", want, text)
				}
			}
			_, proposed, _ := strings.Cut(text, "plan=")
			want := `[{\"hostname\":\"webstage\",\"setupName\":\"web\"},{\"hostname\":\"apistage\",\"setupName\":\"api\"},{\"hostname\":\"workerstage\",\"setupName\":\"worker\"}]`
			if !strings.Contains(proposed, want) {
				t.Errorf("the plan made from the repo is not %s:\n%s", want, text)
			}
		})
	}
}

// TestBootstrapPlan_RefusedPlan_ImportsEveryProposedStageNotLive: a plan
// pairing an existing dev service is attached to existing services only, so a
// plan refused for its parts names the import of every proposed stage that is
// not live before the plan is re-submitted, on the classic and the adopt
// route; a refusal whose proposed stages are all live names no import.
func TestBootstrapPlan_RefusedPlan_ImportsEveryProposedStageNotLive(t *testing.T) {
	t.Parallel()
	webAPI := "@@yaml\nzerops:\n  - setup: base\n  - setup: dev\n    extends: base\n  - setup: web\n    extends: base\n  - setup: api\n    extends: base\n@@declaration\n@@markers\npnpm-workspace.yaml\n@@packages\n"
	plan := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{
		DevHostname: "appdev", Type: "ubuntu/nodejs@22", IsExisting: true,
		BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage",
	}}}
	container := runtime.Info{InContainer: true}

	tests := []struct {
		name       string
		route      workflow.BootstrapRoute
		live       []string
		wantImport []string
	}{
		{name: "classic, neither proposed stage is live", route: workflow.BootstrapRouteClassic, live: []string{"appstage"}, wantImport: []string{"webstage", "apistage"}},
		{name: "adopt, neither proposed stage is live", route: workflow.BootstrapRouteAdopt, live: []string{"appstage"}, wantImport: []string{"webstage", "apistage"}},
		{name: "classic, only the api stage is not live", route: workflow.BootstrapRouteClassic, live: []string{"appstage", "webstage"}, wantImport: []string{"apistage"}},
		{name: "classic, every proposed stage is live", route: workflow.BootstrapRouteClassic, live: []string{"webstage", "apistage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, _ := submitPlanOverRepo(t, tt.route, "appdev", webAPI, plan, container, tt.live...)
			if !strings.Contains(text, "declares the parts") {
				t.Fatalf("the plan is not refused for its parts:\n%s", text)
			}
			suggestion, _, _ := strings.Cut(text, "plan=")
			imports := strings.Contains(suggestion, "zerops_import")
			if imports != (len(tt.wantImport) > 0) {
				t.Fatalf("the refusal names an import = %v, want %v:\n%s", imports, len(tt.wantImport) > 0, text)
			}
			named, _, _ := strings.Cut(suggestion, "e-submit the plan")
			for _, host := range []string{"webstage", "apistage"} {
				if want := slices.Contains(tt.wantImport, host); strings.Contains(named, `\"`+host+`\"`) != want {
					t.Errorf("the import names %s = %v, want %v:\n%s", host, !want, want, text)
				}
			}
		})
	}
}

// submitPlanOverRepo submits plan on route over the repo of the existing
// dev service dev, beside the existing services stages, and returns the
// response text.
func submitPlanOverRepo(t *testing.T, route workflow.BootstrapRoute, dev, repo string, plan []workflow.BootstrapTarget, rt runtime.Info, stages ...string) (string, *workflow.Engine) {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("proj-1", "develop the repo on "+dev, route, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}
	services := make([]platform.ServiceStack, 0, 1+len(stages))
	for _, host := range append([]string{dev}, stages...) {
		services = append(services, platform.ServiceStack{
			ID: "svc-" + host, Name: host, Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/nodejs@22"},
		})
	}
	mock := platform.NewMock().WithServices(services)
	repos := &partsRepos{out: map[string]string{dev: repo}}
	input := WorkflowInput{Step: workflow.StepDiscover, Plan: plan}
	result, _, err := handleBootstrapComplete(context.Background(), engine, mock, nil, nil, input, nil, "proj-1", dir, nil, repos, rt)
	if err != nil {
		t.Fatalf("handleBootstrapComplete: %v", err)
	}
	return extractText(result), engine
}
