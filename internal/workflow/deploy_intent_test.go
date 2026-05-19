package workflow

import (
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestResolve_Table pins the (mode, closeMode, gitPushState, deployed) →
// DeployIntent matrix that every downstream consumer reads from. Build_plan,
// auto-close gate (Phase 2), build-integration handlers (Phase 3), and
// verify/record-deploy (Phase 4) all key off these fields, so a regression
// here ripples everywhere.
func TestResolve_Table(t *testing.T) {
	t.Parallel()

	stagePairServices := []ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, StageHostname: "appstage", Deployed: true},
		{Hostname: "appstage", Mode: topology.ModeStage, Deployed: false},
	}

	cases := []struct {
		name     string
		target   ServiceSnapshot
		services []ServiceSnapshot
		want     DeployIntent
	}{
		{
			name: "simple/auto/deployed → direct self-deploy",
			target: ServiceSnapshot{
				Hostname:        "api",
				Mode:            topology.ModeSimple,
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        true,
			},
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "api",
				BuildTarget:   "api",
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "api"},
				EventsService: "api",
				VerifyTarget:  "api",
			},
		},
		{
			name: "standard dev half/auto/deployed → direct self-deploy, BuildTarget=self",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        true,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "appdev",
				BuildTarget:   "appdev", // direct delivery: dev deploys to self
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "appdev"},
				EventsService: "appdev",
				VerifyTarget:  "appdev",
			},
		},
		{
			name: "standard stage half/auto/!deployed → direct cross-deploy",
			target: ServiceSnapshot{
				Hostname:        "appstage",
				Mode:            topology.ModeStage,
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        false,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:          DeployDeliveryDirect,
				PushSource:        "appdev",
				BuildTarget:       "appstage",
				BuildSetup:        "prod",
				DeployTool:        "zerops_deploy",
				DeployArgs:        map[string]string{"sourceService": "appdev", "targetService": "appstage", "setup": "prod"},
				EventsService:     "appstage",
				VerifyTarget:      "appstage",
				FirstDeployBypass: true, // !Deployed
			},
		},
		{
			name: "standard stage half, paired dev missing → empty PushSource, falls back to self-deploy args",
			target: ServiceSnapshot{
				Hostname:        "appstage",
				Mode:            topology.ModeStage,
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        false,
			},
			services: []ServiceSnapshot{
				// no dev half with StageHostname=appstage
				{Hostname: "appstage", Mode: topology.ModeStage, Deployed: false},
			},
			want: DeployIntent{
				Delivery:          DeployDeliveryDirect,
				PushSource:        "",
				BuildTarget:       "appstage",
				BuildSetup:        "prod",
				DeployTool:        "zerops_deploy",
				DeployArgs:        map[string]string{"targetService": "appstage"}, // self-deploy fallback
				EventsService:     "appstage",
				VerifyTarget:      "appstage",
				FirstDeployBypass: true,
			},
		},
		{
			name: "local-stage/auto/deployed → direct, BuildTarget=self (no git-push)",
			target: ServiceSnapshot{
				Hostname:        "myproject",
				Mode:            topology.ModeLocalStage,
				StageHostname:   "stagehost",
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        true,
			},
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "myproject",
				BuildTarget:   "myproject",
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "myproject"},
				EventsService: "myproject",
				VerifyTarget:  "myproject",
			},
		},
		{
			name: "local-only/manual → manual delivery, empty args",
			target: ServiceSnapshot{
				Hostname:        "myproject",
				Mode:            topology.ModeLocalOnly,
				CloseDeployMode: topology.CloseModeManual,
				Deployed:        true,
			},
			want: DeployIntent{
				Delivery:    DeployDeliveryManual,
				PushSource:  "myproject",
				BuildTarget: "", // no Zerops runtime linked
			},
		},
		{
			name: "git-push/configured/deployed → git-push delivery, BuildTarget=stage with setup=prod",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeGitPush,
				GitPushState:    topology.GitPushConfigured,
				Deployed:        true,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:           DeployDeliveryGitPush,
				PushSource:         "appdev",
				BuildTarget:        "appstage",
				BuildSetup:         "prod",
				DeployTool:         "zerops_deploy",
				DeployArgs:         map[string]string{"targetService": "appdev", "strategy": "git-push"},
				EventsService:      "appstage",
				RecordDeployTarget: "appstage",
				VerifyTarget:       "appstage",
				RequiresAsyncAck:   true,
			},
		},
		{
			name: "git-push/unconfigured → direct fallback (capability gap), BuildTarget=self",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeGitPush,
				GitPushState:    topology.GitPushUnconfigured,
				Deployed:        true,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "appdev",
				BuildTarget:   "appdev",
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "appdev"},
				EventsService: "appdev",
				VerifyTarget:  "appdev",
			},
		},
		{
			name: "git-push/!deployed → first-deploy bypass → direct, BuildTarget=self",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeGitPush,
				GitPushState:    topology.GitPushConfigured,
				Deployed:        false,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:          DeployDeliveryDirect,
				PushSource:        "appdev",
				BuildTarget:       "appdev",
				DeployTool:        "zerops_deploy",
				DeployArgs:        map[string]string{"targetService": "appdev"},
				EventsService:     "appdev",
				VerifyTarget:      "appdev",
				FirstDeployBypass: true,
			},
		},
		{
			name: "unset close-mode → direct (pre-strategy-review), BuildTarget=self",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeUnset,
				Deployed:        true,
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "appdev",
				BuildTarget:   "appdev",
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "appdev"},
				EventsService: "appdev",
				VerifyTarget:  "appdev",
			},
		},
		{
			name: "ModeDev (legacy)/auto → direct self-deploy",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeDev,
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        true,
			},
			want: DeployIntent{
				Delivery:      DeployDeliveryDirect,
				PushSource:    "appdev",
				BuildTarget:   "appdev",
				DeployTool:    "zerops_deploy",
				DeployArgs:    map[string]string{"targetService": "appdev"},
				EventsService: "appdev",
				VerifyTarget:  "appdev",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(c.target, c.services)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Resolve(%s) mismatch:\n  got:  %+v\n  want: %+v", c.target.Hostname, got, c.want)
			}
		})
	}
}

// TestResolve_BuildPlanParity asserts that for every (Mode, closeMode=auto)
// combination the existing deployActionFor exercises in build_plan_test.go,
// the resolver emits identical DeployArgs. Phase 1 stop-gate: any Args
// divergence here means build_plan tests will break when deployActionFor
// switches to Resolve.
func TestResolve_BuildPlanParity(t *testing.T) {
	t.Parallel()

	services := []ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, StageHostname: "appstage", Deployed: true},
		{Hostname: "appstage", Mode: topology.ModeStage, Deployed: false},
	}

	// Mirrors TestBuildPlan_DevelopActiveDeployPending_StageHalf_CrossDeploy
	stage := services[1]
	stage.CloseDeployMode = topology.CloseModeAuto
	intent := Resolve(stage, services)
	if intent.DeployArgs["sourceService"] != "appdev" ||
		intent.DeployArgs["targetService"] != "appstage" ||
		intent.DeployArgs["setup"] != "prod" {
		t.Errorf("stage-half cross-deploy args mismatch: %+v", intent.DeployArgs)
	}

	// Mirrors TestBuildPlan_DevelopActiveDeployPending_DevHalf_SelfDeploy
	dev := services[0]
	dev.Deployed = false
	dev.CloseDeployMode = topology.CloseModeAuto
	intent = Resolve(dev, services)
	if intent.DeployArgs["targetService"] != "appdev" {
		t.Errorf("dev-half self-deploy target = %q, want appdev", intent.DeployArgs["targetService"])
	}
	if _, ok := intent.DeployArgs["sourceService"]; ok {
		t.Errorf("dev-half self-deploy should not carry sourceService, got: %+v", intent.DeployArgs)
	}
	if _, ok := intent.DeployArgs["setup"]; ok {
		t.Errorf("dev-half self-deploy should not carry setup, got: %+v", intent.DeployArgs)
	}
}
