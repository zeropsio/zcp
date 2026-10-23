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
//
//nolint:maintidx // table-driven: the maintainability index is dominated by the case-table data (Halstead volume), not control-flow complexity — the test body is a single linear loop.
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
				PushSetup:     "dev",
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
				PushSetup:     "dev",
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
				PushSetup:          "dev",
				BuildSetup:         "prod",
				DeployTool:         "zerops_deploy",
				DeployArgs:         map[string]string{"targetService": "appdev", "strategy": "git-push", "setup": "dev"},
				EventsService:      "appstage",
				RecordDeployTarget: "appstage",
				VerifyTarget:       "appstage",
				RequiresAsyncAck:   true,
			},
		},
		{
			// WF-1: a renamed setup (set-default-setup → snapshot SetupName/
			// StageSetupName) MUST flow into PushSetup + BuildSetup + the
			// committed-CI DeployArgs — Resolve used to hardcode dev/prod and
			// ignore the fields, baking a wrong `--setup prod` into CI.
			name: "git-push with renamed setups → snapshot SetupName/StageSetupName win over dev/prod",
			target: ServiceSnapshot{
				Hostname:        "appdev",
				Mode:            topology.ModeStandard,
				StageHostname:   "appstage",
				CloseDeployMode: topology.CloseModeGitPush,
				GitPushState:    topology.GitPushConfigured,
				Deployed:        true,
				SetupName:       "web",     // dev-half canonical setup
				StageSetupName:  "release", // stage-half canonical setup
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:           DeployDeliveryGitPush,
				PushSource:         "appdev",
				BuildTarget:        "appstage",
				PushSetup:          "web",     // not "dev"
				BuildSetup:         "release", // not "prod"
				DeployTool:         "zerops_deploy",
				DeployArgs:         map[string]string{"targetService": "appdev", "strategy": "git-push", "setup": "web"},
				EventsService:      "appstage",
				RecordDeployTarget: "appstage",
				VerifyTarget:       "appstage",
				RequiresAsyncAck:   true,
			},
		},
		{
			// WF-1: stage-half direct cross-deploy honors the stage's
			// canonical setup name (snapshot SetupName via SetupNameFor(stage)).
			name: "stage half with renamed setup → snapshot SetupName wins over prod",
			target: ServiceSnapshot{
				Hostname:        "appstage",
				Mode:            topology.ModeStage,
				CloseDeployMode: topology.CloseModeAuto,
				Deployed:        false,
				SetupName:       "release", // stage-half canonical setup
			},
			services: stagePairServices,
			want: DeployIntent{
				Delivery:          DeployDeliveryDirect,
				PushSource:        "appdev",
				BuildTarget:       "appstage",
				BuildSetup:        "release", // not "prod"
				DeployTool:        "zerops_deploy",
				DeployArgs:        map[string]string{"sourceService": "appdev", "targetService": "appstage", "setup": "release"},
				EventsService:     "appstage",
				VerifyTarget:      "appstage",
				FirstDeployBypass: true,
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
				PushSetup:     "dev",
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
				PushSetup:         "dev",
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
				PushSetup:     "dev",
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

// poolDevPairServices is a pair keyed by its dev pool as the envelope carries
// it: the pool half marked and modeless, the stage half in stage mode.
func poolDevPairServices(closeMode topology.CloseDeployMode, gitPush topology.GitPushState) []ServiceSnapshot {
	return []ServiceSnapshot{
		{
			Hostname: "apppool", RuntimeClass: topology.RuntimePool, DevIsPool: true, Bootstrapped: true, Deployed: true,
			CloseDeployMode: closeMode, GitPushState: gitPush, StageHostname: "appstage",
			Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "web"}},
		},
		{
			Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: true,
			CloseDeployMode: closeMode, GitPushState: gitPush, SetupName: "web",
		},
	}
}

// TestDeployIntent_StageOfPoolDevPair_SourceIsLease pins the stage's deploy
// on a pair whose dev half is a pool: its source is a held lease of that pool
// (a cross-deploy from the alias), and with none held it names the pool to
// lease from and renders no deploy — never the pool as the source (PS-3). A
// git-push pair's stage deploys the same way: a git-push from a lease is
// refused (DP-6), so a cross-deploy from it is the stage's deploy.
func TestDeployIntent_StageOfPoolDevPair_SourceIsLease(t *testing.T) {
	t.Parallel()
	held := LeaseSnapshot{Alias: "apppool-4", Pool: "apppool", Branch: "feat-x", State: "held"}
	cases := []struct {
		name    string
		mode    topology.CloseDeployMode
		gitPush topology.GitPushState
		leases  []LeaseSnapshot
		want    DeployIntent
	}{
		{"a held lease is the source", topology.CloseModeAuto, topology.GitPushUnconfigured,
			[]LeaseSnapshot{{Alias: "otherpool-1", Pool: "otherpool", State: "held"}, {Alias: "apppool-2", Pool: "apppool", State: "lost"}, held},
			DeployIntent{
				Delivery: DeployDeliveryDirect, PushSource: "apppool-4", SourcePool: "apppool",
				BuildTarget: "appstage", BuildSetup: "web", DeployTool: "zerops_deploy",
				DeployArgs:    map[string]string{"sourceService": "apppool-4", "targetService": "appstage", "setup": "web"},
				EventsService: "appstage", VerifyTarget: "appstage",
			}},
		{"no held lease: lease first, no deploy", topology.CloseModeAuto, topology.GitPushUnconfigured,
			[]LeaseSnapshot{{Alias: "apppool-2", Pool: "apppool", State: "lost"}},
			DeployIntent{
				Delivery: DeployDeliveryDirect, SourcePool: "apppool",
				BuildTarget: "appstage", BuildSetup: "web",
				EventsService: "appstage", VerifyTarget: "appstage",
			}},
		{"git-push pair: still a cross-deploy from the lease", topology.CloseModeAuto, topology.GitPushConfigured,
			[]LeaseSnapshot{held},
			DeployIntent{
				Delivery: DeployDeliveryDirect, PushSource: "apppool-4", SourcePool: "apppool",
				BuildTarget: "appstage", BuildSetup: "web", DeployTool: "zerops_deploy",
				DeployArgs:    map[string]string{"sourceService": "apppool-4", "targetService": "appstage", "setup": "web"},
				EventsService: "appstage", VerifyTarget: "appstage",
			}},
		{"manual close mode: the user's deploy", topology.CloseModeManual, topology.GitPushUnconfigured,
			[]LeaseSnapshot{held},
			DeployIntent{
				Delivery: DeployDeliveryManual, PushSource: "apppool-4", SourcePool: "apppool",
				BuildTarget: "appstage", BuildSetup: "web",
				EventsService: "appstage", VerifyTarget: "appstage",
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			services := poolDevPairServices(tc.mode, tc.gitPush)
			got := ResolveWithLeases(services[1], services, tc.leases)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("intent:\n got %+v\nwant %+v", got, tc.want)
			}
			if got.PushSource == "apppool" || got.DeployArgs["sourceService"] == "apppool" || got.DeployArgs["targetService"] == "apppool" {
				t.Errorf("the pool itself is a deploy endpoint: %+v", got)
			}
		})
	}

	// Without the leases (the readers that resolve from metas alone) the stage
	// still builds on itself and names its pool.
	services := poolDevPairServices(topology.CloseModeAuto, topology.GitPushConfigured)
	if got := Resolve(services[1], services); got.PushSource != "" || got.SourcePool != "apppool" || got.BuildTarget != "appstage" || got.Delivery != DeployDeliveryDirect {
		t.Errorf("Resolve without leases = %+v, want a lease first from apppool, building appstage", got)
	}
}

// TestDeployIntent_PoolDevHalf_BuildsNothing pins that the pool half of such a
// pair is never a push source nor a build target, whatever its delivery: a
// deploy of the part runs from a lease to its stage.
func TestDeployIntent_PoolDevHalf_BuildsNothing(t *testing.T) {
	t.Parallel()
	for _, gitPush := range []topology.GitPushState{topology.GitPushUnconfigured, topology.GitPushConfigured} {
		services := poolDevPairServices(topology.CloseModeAuto, gitPush)
		got := ResolveWithLeases(services[0], services, []LeaseSnapshot{{Alias: "apppool-4", Pool: "apppool", State: "held"}})
		if got.PushSource != "" || got.BuildTarget != "" || got.DeployTool != "" || got.DeployArgs != nil || len(got.Builds()) != 0 || got.SourcePool != "apppool" {
			t.Errorf("gitPush=%s: pool half intent = %+v, want nothing built or pushed from the pool", gitPush, got)
		}
	}
}
