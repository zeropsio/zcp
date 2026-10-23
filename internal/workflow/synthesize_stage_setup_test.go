package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestSynthesize_PromoteStage_NamesTheRepoOwnSetup: the next deploy step names
// the repo's own setup. Every promote-to-stage command a develop session is
// handed cross-deploys each stage of the pair with that stage's setup — every
// part of a multi-part pair in one zerops_deploy_batch, so the stages build in
// parallel, the dev half's own commands once — and a stage whose setup is not
// recorded keeps `setup="prod"`.
func TestSynthesize_PromoteStage_NamesTheRepoOwnSetup(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	pair := func(deployed bool, closeMode topology.CloseDeployMode, stages ...topology.StageRef) []ServiceSnapshot {
		dev := ServiceSnapshot{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
			SetupName: "dev", StageHostname: stages[0].Hostname, StageSetupName: stages[0].SetupName,
			CloseDeployMode: closeMode, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, Deployed: deployed}
		if len(stages) > 1 {
			dev.Stages = stages
		}
		out := make([]ServiceSnapshot, 0, 1+len(stages))
		out = append(out, dev)
		for _, s := range stages {
			out = append(out, ServiceSnapshot{Hostname: s.Hostname, TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
				SetupName: s.SetupName, CloseDeployMode: closeMode, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true})
		}
		return out
	}
	webAPI := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
	cadences := []struct {
		name      string
		deployed  bool
		closeMode topology.CloseDeployMode
		atom      string
	}{
		{name: "first deploy", closeMode: topology.CloseModeUnset, atom: "develop-first-deploy-promote-stage"},
		{name: "close-mode unset", deployed: true, closeMode: topology.CloseModeUnset, atom: "develop-standard-unset-promote-stage"},
		{name: "close-mode auto", deployed: true, closeMode: topology.CloseModeAuto, atom: "develop-close-mode-auto-standard"},
	}
	shapes := []struct {
		name   string
		stages []topology.StageRef
		want   []string
		absent []string
	}{
		{
			name:   "two parts",
			stages: webAPI,
			want: []string{
				`zerops_deploy_batch targets=[{"sourceService":"appdev","targetService":"webstage","setup":"web"},{"sourceService":"appdev","targetService":"apistage","setup":"api"}]` + "\n" +
					`zerops_verify serviceHostname="webstage"` + "\n" + `zerops_verify serviceHostname="apistage"`,
			},
			absent: []string{`setup="prod"`, `"setup":"prod"`, `zerops_deploy sourceService=`},
		},
		{
			name:   "one stage without a recorded setup",
			stages: []topology.StageRef{{Hostname: "appstage"}},
			want:   []string{`zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"` + "\n" + `zerops_verify serviceHostname="appstage"`},
		},
		{
			name:   "one stage with its own setup",
			stages: []topology.StageRef{{Hostname: "appstage", SetupName: "app"}},
			want:   []string{`zerops_deploy sourceService="appdev" targetService="appstage" setup="app"`},
			absent: []string{`setup="prod"`},
		},
	}
	for _, c := range cadences {
		for _, s := range shapes {
			t.Run(c.name+", "+s.name, func(t *testing.T) {
				t.Parallel()
				env := StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: pair(c.deployed, c.closeMode, s.stages...)}
				out, err := Synthesize(env, corpus)
				if err != nil {
					t.Fatalf("Synthesize: %v", err)
				}
				var body string
				for _, m := range out {
					if m.AtomID == c.atom {
						body = m.Body
					}
				}
				if body == "" {
					t.Fatalf("%s did not render", c.atom)
				}
				for _, want := range s.want {
					if !strings.Contains(body, want) {
						t.Errorf("%s misses %q:\n%s", c.atom, want, body)
					}
				}
				for _, absent := range s.absent {
					if strings.Contains(body, absent) {
						t.Errorf("%s still says %q:\n%s", c.atom, absent, body)
					}
				}
				if n := strings.Count(body, `zerops_deploy targetService="appdev"`); n > 1 {
					t.Errorf("%s repeats the dev half's deploy %d times:\n%s", c.atom, n, body)
				}
			})
		}
	}
}

// TestSynthesize_CloseModeAuto_MultiPartPairChecksItsDeclaredServers: on a
// pair with several parts the close step's one unnamed start overwrote the
// declared web server. The deploy starts every declared server, so a
// multi-part pair's close step checks them with action=status and never
// renders an unnamed start; a one-stage pair keeps its start.
func TestSynthesize_CloseModeAuto_MultiPartPairChecksItsDeclaredServers(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	snapshots := func(stages ...topology.StageRef) []ServiceSnapshot {
		dev := ServiceSnapshot{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
			SetupName: "dev", StageHostname: stages[0].Hostname, StageSetupName: stages[0].SetupName,
			CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, Deployed: true}
		if len(stages) > 1 {
			dev.Stages = stages
		}
		out := make([]ServiceSnapshot, 0, 1+len(stages))
		out = append(out, dev)
		for _, s := range stages {
			out = append(out, ServiceSnapshot{Hostname: s.Hostname, TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
				SetupName: s.SetupName, CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true})
		}
		return out
	}
	closeStep := func(t *testing.T, services []ServiceSnapshot) string {
		t.Helper()
		out, err := Synthesize(StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: services}, corpus)
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		for _, m := range out {
			if m.AtomID == "develop-close-mode-auto-standard" {
				return m.Body
			}
		}
		t.Fatal("develop-close-mode-auto-standard did not render")
		return ""
	}

	multi := closeStep(t, snapshots(topology.StageRef{Hostname: "webstage", SetupName: "web"}, topology.StageRef{Hostname: "apistage", SetupName: "api"}))
	if !strings.Contains(multi, `zerops_dev_server action=status hostname="appdev"`) {
		t.Errorf("multi-part close step misses the status check:\n%s", multi)
	}
	if strings.Contains(multi, "zerops_dev_server action=start hostname=") {
		t.Errorf("multi-part close step still renders an unnamed start:\n%s", multi)
	}

	single := closeStep(t, snapshots(topology.StageRef{Hostname: "appstage"}))
	if !strings.Contains(single, `zerops_dev_server action=start hostname="appdev" command=`) {
		t.Errorf("one-stage close step lost its start:\n%s", single)
	}
}

// TestBootstrapComplete_MultiPartPair_RecordsEachStagesSetup: the next deploy
// step names the repo's own setup from the moment bootstrap closes. A pair
// planned with several stages records each stage's setup — it is what tells
// the parts apart — so develop's first deploy of webstage builds `web`, not
// `prod`. A one-stage pair records only its stage, as today; its setup is
// resolved at its first deploy.
func TestBootstrapComplete_MultiPartPair_RecordsEachStagesSetup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rt   RuntimeTarget
		want map[string]string // stage → recorded setup
	}{
		{
			name: "two parts",
			rt:   RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}},
			want: map[string]string{"webstage": "web", "apistage": "api"},
		},
		{
			name: "one stage",
			rt:   RuntimeTarget{ExplicitStage: "appstage", StageSetupName: "prod"},
			want: map[string]string{"appstage": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvContainer, nil)
			if _, err := eng.BootstrapStartWithRoute("proj-1", "develop the repo on appdev", BootstrapRouteClassic, ""); err != nil {
				t.Fatalf("BootstrapStartWithRoute: %v", err)
			}
			rt := tt.rt
			rt.DevHostname, rt.Type, rt.BootstrapMode, rt.IsExisting = "appdev", "nodejs@22", topology.PlanModeStandard, true
			if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{{Runtime: rt}}, nil, nil); err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}
			// An adopted pair's bootstrap closes with its provision step.
			if _, err := eng.BootstrapComplete(context.Background(), "provision", "Attestation for provision step completed ok", nil); err != nil {
				t.Fatalf("BootstrapComplete(provision): %v", err)
			}
			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil || meta == nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			for stage, setup := range tt.want {
				if got := meta.SetupNameFor(stage); got != setup {
					t.Errorf("setup of %s = %q, want %q", stage, got, setup)
				}
			}
		})
	}
}
