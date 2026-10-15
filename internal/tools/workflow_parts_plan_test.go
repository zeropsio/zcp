// Tests for: tools/workflow_parts_plan.go — a bootstrap plan over a mounted
// repo takes its stages from the repo's parts.
package tools

import (
	"context"
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
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			if _, err := engine.BootstrapStartWithRoute("proj-1", "develop the repo on appdev", tt.route, ""); err != nil {
				t.Fatalf("BootstrapStartWithRoute: %v", err)
			}
			mock := platform.NewMock().WithServices([]platform.ServiceStack{{
				ID: "svc-appdev", Name: "appdev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/nodejs@22"},
			}})
			repos := &partsRepos{out: map[string]string{"appdev": tt.repo}}
			input := WorkflowInput{Step: workflow.StepDiscover, Plan: tt.plan}
			result, _, err := handleBootstrapComplete(context.Background(), engine, mock, nil, nil, input, nil, "proj-1", dir, nil, repos, tt.rt)
			if err != nil {
				t.Fatalf("handleBootstrapComplete: %v", err)
			}
			text := extractText(result)
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
