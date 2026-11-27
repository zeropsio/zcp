package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// PD-CHOICE — A pool stage deploy uses the sole verified held lease, or leaves the choice open and lists it.
// witness: internal/workflow/deploy_intent.go | if len(held) == 1 { → if len(held) > 0 {
func TestPoolDeploy_PDCHOICE_RequiresAnUnambiguousLease(t *testing.T) {
	t.Parallel()
	services := []ServiceSnapshot{
		{Hostname: "apppool", RuntimeClass: topology.RuntimePool, DevIsPool: true, Bootstrapped: true, Deployed: true, StageHostname: "appstage", Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "web"}}},
		{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto, SetupName: "web"},
	}
	for _, tc := range []struct {
		name        string
		leases      []LeaseSnapshot
		wantSource  string
		wantChoices []LeaseSnapshot
		wantTool    string
		wantArgs    map[string]string
	}{
		{
			name:     "no held leases",
			wantTool: "zerops_lease",
			wantArgs: map[string]string{"action": "acquire", "pool": "apppool"},
		},
		{
			name: "no verified held lease",
			leases: []LeaseSnapshot{
				{Alias: "apppool-1", Pool: "apppool", State: "held", Unverified: true},
				{Alias: "apppool-2", Pool: "apppool", State: "lost"},
				{Alias: "other-1", Pool: "other", State: "held"},
			},
			wantTool: "zerops_lease",
			wantArgs: map[string]string{"action": "acquire", "pool": "apppool"},
		},
		{
			name: "one verified held lease",
			leases: []LeaseSnapshot{
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
				{Alias: "apppool-5", Pool: "apppool", State: "lost"},
				{Alias: "other-1", Pool: "other", State: "held"},
			},
			wantSource: "apppool-4",
			wantTool:   "zerops_deploy",
			wantArgs:   map[string]string{"sourceService": "apppool-4", "targetService": "appstage", "setup": "web"},
		},
		{
			name: "two branches",
			leases: []LeaseSnapshot{
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
				{Alias: "apppool-7", Pool: "apppool", Branch: "feat/b", State: "held"},
				{Alias: "apppool-8", Pool: "apppool", State: "held", Unverified: true},
				{Alias: "other-1", Pool: "other", State: "held"},
			},
			wantChoices: []LeaseSnapshot{
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
				{Alias: "apppool-7", Pool: "apppool", Branch: "feat/b", State: "held"},
				{Alias: "apppool-8", Pool: "apppool", State: "held", Unverified: true},
			},
		},
		{
			name: "same branches in reverse order",
			leases: []LeaseSnapshot{
				{Alias: "apppool-7", Pool: "apppool", Branch: "feat/b", State: "held"},
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
			},
			wantChoices: []LeaseSnapshot{
				{Alias: "apppool-7", Pool: "apppool", Branch: "feat/b", State: "held"},
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
			},
		},
		{
			name: "another held lease is unverified",
			leases: []LeaseSnapshot{
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
				{Alias: "apppool-8", Pool: "apppool", Branch: "feat/b", State: "held", Unverified: true},
			},
			wantChoices: []LeaseSnapshot{
				{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"},
				{Alias: "apppool-8", Pool: "apppool", Branch: "feat/b", State: "held", Unverified: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent := ResolveWithLeases(services[1], services, tc.leases)
			if intent.PushSource != tc.wantSource || !reflect.DeepEqual(intent.SourceChoices, tc.wantChoices) {
				t.Errorf("source = %q, choices = %v; want %q, %v", intent.PushSource, intent.SourceChoices, tc.wantSource, tc.wantChoices)
			}
			env := StateEnvelope{
				Phase: PhaseDevelopActive, Services: services, Leases: &tc.leases,
				WorkSession: &WorkSessionSummary{Intent: "check the change", Services: []string{"appstage"}},
			}
			plan := BuildPlan(env)
			if plan.Primary.Tool != tc.wantTool || !reflect.DeepEqual(plan.Primary.Args, tc.wantArgs) {
				t.Errorf("plan source = %q %v; want %q %v", plan.Primary.Tool, plan.Primary.Args, tc.wantTool, tc.wantArgs)
			}
			if len(tc.wantChoices) == 0 {
				return
			}
			answer := RenderStatus(Response{Envelope: env, Plan: &plan})
			var blocker string
			for line := range strings.SplitSeq(answer, "\n") {
				if strings.Contains(line, "Auto-close blocked") {
					blocker = line
				}
			}
			for _, lease := range tc.wantChoices {
				if !strings.Contains(blocker, lease.Alias) {
					t.Errorf("answer does not list %q in the source choices: %s", lease.Alias, blocker)
				}
				if lease.Unverified && !strings.Contains(blocker, lease.Alias+" (unverified:") {
					t.Errorf("answer does not mark %q unverified: %s", lease.Alias, blocker)
				}
			}
			if strings.Contains(blocker, "other-1") || strings.Contains(answer, "zerops_lease") {
				t.Errorf("answer offers an ineligible source or an unnecessary lease: %s", answer)
			}
		})
	}
}

// witness: internal/workflow/render.go | if env.Mate { → if !env.Mate {
func TestPoolDeploy_PDMATE_LeavesStageCheckToReviewBuild(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		env  StateEnvelope
	}{
		{
			name: "Mate with a held branch lease",
			env: StateEnvelope{
				Phase: PhaseDevelopActive, Mate: true,
				Services: []ServiceSnapshot{
					{Hostname: "apppool", RuntimeClass: topology.RuntimePool, DevIsPool: true, Bootstrapped: true, Deployed: true, StageHostname: "appstage", Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "web"}}},
					{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto, SetupName: "web"},
				},
				Leases:      &[]LeaseSnapshot{{Alias: "apppool-4", Pool: "apppool", Branch: "feat/a", State: "held"}},
				WorkSession: &WorkSessionSummary{Intent: "deploy the requested branch", Services: []string{"appstage"}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := poolStageDeployNext("appstage", tc.env); got != "" {
				t.Errorf("Mate pool-stage hint = %q, want none", got)
			}
			plan := BuildPlan(tc.env)
			if plan.Primary.Tool != "zerops_deploy" || plan.Primary.Args["sourceService"] != "apppool-4" || plan.Primary.Args["targetService"] != "appstage" {
				t.Errorf("requested deploy plan = %+v", plan.Primary)
			}
			out := RenderStatus(Response{Envelope: tc.env, Plan: &plan})
			var blocker string
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, "Auto-close blocked") {
					blocker = line
				}
			}
			if !strings.Contains(blocker, "pending appstage.") || strings.Contains(blocker, "zerops_deploy") || strings.Contains(blocker, "stage check") {
				t.Errorf("Mate blocker = %q, want the pending stage without a deploy suggestion", blocker)
			}
			if !strings.Contains(out, `sourceService="apppool-4"`) || !strings.Contains(out, `targetService="appstage"`) {
				t.Errorf("rendered plan does not name the requested deploy's source and target: %s", out)
			}
		})
	}
}
