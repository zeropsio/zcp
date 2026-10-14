package workflow

import (
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// One commit per level, all parts (spec-mate §2.8a): a push from a pair's dev
// half rebuilds every stage, each with its own setup.

func gitPushThreeStagePair() *ServiceMeta {
	m := threeStagePair()
	m.CloseDeployMode = topology.CloseModeAuto
	m.GitPushState = topology.GitPushConfigured
	m.FirstDeployedAt = "2026-10-14T00:00:00Z"
	return m
}

func TestResolve_GitPushFromAPairWithThreeStages_BuildsEveryStage(t *testing.T) {
	t.Parallel()
	meta := gitPushThreeStagePair()
	snaps := SnapshotsFromMetas([]*ServiceMeta{meta})
	intent := Resolve(snaps[0], snaps)
	want := []topology.StageRef{
		{Hostname: "webstage", SetupName: "web"},
		{Hostname: "apistage", SetupName: "api"},
		{Hostname: "workerstage", SetupName: RecipeSetupProd},
	}
	if !slices.Equal(intent.Builds(), want) {
		t.Errorf("Builds() = %+v, want %+v", intent.Builds(), want)
	}
	if intent.BuildTarget != "webstage" || intent.BuildSetup != "web" {
		t.Errorf("BuildTarget/BuildSetup = %q/%q, want the first stage webstage/web", intent.BuildTarget, intent.BuildSetup)
	}
}

func TestResolve_SelfDeployAndOneStage_BuildsIsTheOneBuildTarget(t *testing.T) {
	t.Parallel()
	one := &ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard,
		Stages:          []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushConfigured,
		BootstrapSession: "s", BootstrappedAt: "t", FirstDeployedAt: "t",
	}
	simple := &ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple, BootstrapSession: "s", BootstrappedAt: "t", FirstDeployedAt: "t"}
	tests := []struct {
		name string
		meta *ServiceMeta
		want []topology.StageRef
	}{
		{"a one-stage pair under git-push builds its stage", one, []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}},
		{"a simple service builds itself", simple, []topology.StageRef{{Hostname: "app"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snaps := SnapshotsFromMetas([]*ServiceMeta{tt.meta})
			if got := Resolve(snaps[0], snaps).Builds(); !slices.Equal(got, tt.want) {
				t.Errorf("Builds() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolvedDeployTargets_GitPushFromAPairWithThreeStages_GatesOnEveryStage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, gitPushThreeStagePair()); err != nil {
		t.Fatal(err)
	}
	got := ResolvedDeployTargets(dir, &WorkSession{Services: []string{"appdev"}})
	if want := []string{"webstage", "apistage", "workerstage"}; !slices.Equal(got, want) {
		t.Errorf("ResolvedDeployTargets = %v, want %v", got, want)
	}
}
