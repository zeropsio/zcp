// Tests for: tools/workflow_bootstrap.go — a bootstrap plan never takes a
// Swarm pool as a dependency of a runtime, on any route (spec-workflows.md §8
// PS-2, PS-5).
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

// submitPlanBesidePool opens a bootstrap session on route in a project running
// `app` (an app adopted earlier in dev mode) and the Swarm pool `sandbox`, and
// submits plan at the discover step through the zerops_workflow handler.
func submitPlanBesidePool(t *testing.T, route workflow.BootstrapRoute, plan []workflow.BootstrapTarget) (refused bool, text string, engine *workflow.Engine) {
	t.Helper()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{Hostname: "app", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-25"}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	engine = workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("proj-1", "switch app to simple mode", route, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-sandbox", Name: "sandbox", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
	})
	input := WorkflowInput{Step: workflow.StepDiscover, Plan: plan}
	result, _, err := handleBootstrapComplete(context.Background(), engine, mock, nil, nil, input, nil, "proj-1", dir, nil, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("handleBootstrapComplete: %v", err)
	}
	return result.IsError, extractText(result), engine
}

// TestBootstrapPlan_PoolDependency_RefusedOnEveryRoute: A13's plan — the
// adopted app switched to simple mode with the project's pool as an EXISTS
// dependency — is refused on the classic and the adopt route before the plan
// is taken, naming the pool and the way forward. The same plan with the pool
// left out is taken on both, with no dependency stored.
func TestBootstrapPlan_PoolDependency_RefusedOnEveryRoute(t *testing.T) {
	t.Parallel()
	app := workflow.RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple, IsExisting: true}
	withPool := []workflow.BootstrapTarget{{Runtime: app, Dependencies: []workflow.Dependency{{Hostname: "sandbox", Type: "swarm@1", Resolution: "EXISTS"}}}}
	poolLeftOut := []workflow.BootstrapTarget{{Runtime: app}}

	for _, route := range []workflow.BootstrapRoute{workflow.BootstrapRouteClassic, workflow.BootstrapRouteAdopt} {
		t.Run(string(route)+", the pool as a dependency", func(t *testing.T) {
			t.Parallel()
			refused, text, engine := submitPlanBesidePool(t, route, withPool)
			if !refused {
				t.Fatalf("a plan naming the pool as a dependency must be refused:\n%s", text)
			}
			for _, want := range []string{"sandbox", "Swarm pool", "never a dependency of a runtime", "leave it out of the plan"} {
				if !strings.Contains(text, want) {
					t.Errorf("refusal missing %q:\n%s", want, text)
				}
			}
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			if state.Bootstrap.Plan != nil || state.Bootstrap.CurrentStepName() != workflow.StepDiscover {
				t.Errorf("a refused plan leaves the session at discover with no plan; step %q plan %+v", state.Bootstrap.CurrentStepName(), state.Bootstrap.Plan)
			}
		})
		t.Run(string(route)+", the pool left out", func(t *testing.T) {
			t.Parallel()
			refused, text, engine := submitPlanBesidePool(t, route, poolLeftOut)
			if refused {
				t.Fatalf("the mode switch with the pool left out must be taken:\n%s", text)
			}
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			plan := state.Bootstrap.Plan
			if plan == nil || len(plan.Targets) != 1 || plan.Targets[0].Runtime.BootstrapMode != topology.PlanModeSimple || len(plan.Targets[0].Dependencies) != 0 {
				t.Errorf("plan not stored as the app in simple mode with no dependency: %+v", plan)
			}
		})
	}
}
