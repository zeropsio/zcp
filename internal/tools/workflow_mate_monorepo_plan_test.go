// Tests for: tools/hq_one_repository.go — a Mate's fresh classic plan never
// splits one repository into a pair per part: several dev/stage pairs are
// refused before anything is recorded, steering to one dev service with a
// stage per part and pool-migrate, unless the plan says its runtimes are
// separate repositories (docs/spec-mate.md §10.10 "One repository per
// monorepo").
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestBootstrapPlan_InAMate_SeveralPairs_RefusedForOnePairWithAStagePerPart:
// a classic plan pairs dev services while their checkouts are still empty,
// so no root commit tells the parts of one repository apart; in a Mate every
// pair asks HQ for a repository of its own, so a plan of several dev/stage
// pairs is refused before anything is recorded, with the plan made from it —
// the first dev service holding every part's stage, each with its setup —
// and pool-migrate after. separateRepositories=true takes the plan as it is;
// one pair with a stage per part, and every plan outside a Mate, stay as
// they are.
func TestBootstrapPlan_InAMate_SeveralPairs_RefusedForOnePairWithAStagePerPart(t *testing.T) {
	t.Parallel()
	pair := func(dev, stage, typ string) workflow.BootstrapTarget {
		return workflow.BootstrapTarget{
			Runtime: workflow.RuntimeTarget{DevHostname: dev, ExplicitStage: stage, Type: typ, BootstrapMode: topology.PlanModeStandard},
		}
	}
	web := pair("webdev", "webstage", "nodejs@22")
	web.Dependencies = []workflow.Dependency{{Hostname: "db", Type: "postgresql@17", Resolution: workflow.ResolutionCreate}}
	api := pair("apidev", "apistage", "nodejs@22")
	api.Dependencies = []workflow.Dependency{{Hostname: "db", Type: "postgresql@17", Resolution: workflow.ResolutionShared}, {Hostname: "cache", Type: "valkey@7.2", Resolution: workflow.ResolutionCreate}}
	onePair := workflow.BootstrapTarget{Runtime: workflow.RuntimeTarget{
		DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard,
		Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
	}}
	mate := runtime.Info{InContainer: true, MateEnabled: true}

	tests := []struct {
		name        string
		plan        []workflow.BootstrapTarget
		separate    bool
		rt          runtime.Info
		wantRefused bool
	}{
		{name: "two pairs in a Mate", plan: []workflow.BootstrapTarget{web, api}, rt: mate, wantRefused: true},
		{name: "two pairs in a Mate, separate repositories", plan: []workflow.BootstrapTarget{web, api}, separate: true, rt: mate},
		{name: "one pair with a stage per part in a Mate", plan: []workflow.BootstrapTarget{onePair}, rt: mate},
		{name: "two pairs outside a Mate", plan: []workflow.BootstrapTarget{web, api}, rt: runtime.Info{InContainer: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			if _, err := engine.BootstrapStartWithRoute("proj-1", "build web and api", workflow.BootstrapRouteClassic, ""); err != nil {
				t.Fatalf("BootstrapStartWithRoute: %v", err)
			}
			input := WorkflowInput{Step: workflow.StepDiscover, Plan: tt.plan}
			if tt.separate {
				input.SeparateRepositories = FlexBool(true)
			}

			result, _, err := handleBootstrapComplete(context.Background(), engine, platform.NewMock(), nil, nil,
				input, nil, "proj-1", dir, nil, nil, tt.rt)
			if err != nil {
				t.Fatalf("handleBootstrapComplete: %v", err)
			}
			text := extractText(result)
			state, err := engine.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}

			if !tt.wantRefused {
				if result.IsError {
					t.Fatalf("the plan was refused:\n%s", text)
				}
				if state.Bootstrap.Plan == nil {
					t.Fatalf("the plan was not recorded:\n%s", text)
				}
				return
			}
			if !result.IsError {
				t.Fatalf("several pairs in a Mate were taken:\n%s", text)
			}
			if state.Bootstrap.Plan != nil {
				t.Errorf("a refused plan is recorded: %+v", state.Bootstrap.Plan)
			}
			var wire struct {
				Error      string `json:"error"`
				Suggestion string `json:"suggestion"`
			}
			if err := json.Unmarshal([]byte(text), &wire); err != nil {
				t.Fatalf("refusal is not the typed error: %v\n%s", err, text)
			}
			for _, want := range []string{`"webdev"`, `"apidev"`, "one repository", "separateRepositories"} {
				if !strings.Contains(wire.Error+wire.Suggestion, want) {
					t.Errorf("the refusal misses %s:\n%s", want, text)
				}
			}
			if !strings.Contains(wire.Suggestion, `zerops_workflow workflow="pool-migrate" service="webdev"`) {
				t.Errorf("the refusal names no pool-migrate of the dev service it keeps:\n%s", wire.Suggestion)
			}
			_, planText, ok := strings.Cut(wire.Suggestion, "plan=")
			if !ok {
				t.Fatalf("the refusal carries no plan made from it:\n%s", wire.Suggestion)
			}
			var fixed []workflow.BootstrapTarget
			if err := json.NewDecoder(strings.NewReader(planText)).Decode(&fixed); err != nil {
				t.Fatalf("the plan made from it does not decode: %v\n%s", err, planText)
			}
			if len(fixed) != 1 || fixed[0].Runtime.DevHostname != "webdev" || fixed[0].Runtime.ExplicitStage != "" {
				t.Fatalf("the plan made from it = %+v, want one pair keyed by webdev, its stages in stages", fixed)
			}
			wantStages := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
			if got := fixed[0].Runtime.Stages; len(got) != 2 || got[0] != wantStages[0] || got[1] != wantStages[1] {
				t.Errorf("stages = %+v, want %+v", got, wantStages)
			}
			deps := map[string]string{}
			for _, d := range fixed[0].Dependencies {
				deps[d.Hostname] = d.Resolution
			}
			if len(deps) != 2 || deps["db"] != workflow.ResolutionCreate || deps["cache"] != workflow.ResolutionCreate {
				t.Errorf("dependencies = %+v, want db and cache, each created once", fixed[0].Dependencies)
			}
			// The plan made from it is one zcp takes.
			again, _, err := handleBootstrapComplete(context.Background(), engine, platform.NewMock(), nil, nil,
				WorkflowInput{Step: workflow.StepDiscover, Plan: fixed}, nil, "proj-1", dir, nil, nil, tt.rt)
			if err != nil || again.IsError {
				t.Errorf("the plan made from the refusal is refused: %v\n%s", err, extractText(again))
			}
		})
	}
}
