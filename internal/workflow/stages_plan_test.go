package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const threePartRecipe = `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
    buildFromGit: https://github.com/acme/mono
  - hostname: webstage
    type: static
    zeropsSetup: web
    buildFromGit: https://github.com/acme/mono
  - hostname: apistage
    type: go@1
    zeropsSetup: api
    buildFromGit: https://github.com/acme/mono
  - hostname: db
    type: postgresql@17
    mode: NON_HA
`

func TestDeriveRecipePlan_SecondStageOfTheSameRepo_JoinsThePair(t *testing.T) {
	t.Parallel()
	shape, err := ParseRecipeImportShape(threePartRecipe)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if shape.Mode() != topology.PlanModeStandard {
		t.Errorf("Mode() = %q, want standard (one pair)", shape.Mode())
	}
	targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want one pair", targets)
	}
	rt := targets[0].Runtime
	if rt.DevHostname != "appdev" || rt.BootstrapMode != topology.PlanModeStandard || rt.PrimarySetupName != "dev" {
		t.Errorf("pair = %+v, want appdev standard with setup dev", rt)
	}
	want := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
	if got := rt.StageRefs(); !slices.Equal(got, want) {
		t.Errorf("StageRefs() = %+v, want %+v", got, want)
	}
	if len(targets[0].Dependencies) != 1 || targets[0].Dependencies[0].Hostname != "db" {
		t.Errorf("dependencies = %+v, want db on the pair", targets[0].Dependencies)
	}
	if _, err := ValidateBootstrapTargets(targets, nil, nil); err != nil {
		t.Errorf("derived plan does not validate: %v", err)
	}
}

func TestDeriveRecipePlan_SingleStagePair_KeepsTodaysPlan(t *testing.T) {
	t.Parallel()
	shape, err := ParseRecipeImportShape(`services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
    buildFromGit: https://github.com/acme/app
  - hostname: appstage
    type: nodejs@22
    zeropsSetup: prod
    buildFromGit: https://github.com/acme/app
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	rt := targets[0].Runtime
	if rt.ExplicitStage != "appstage" || rt.StageSetupName != "prod" || rt.Stages != nil {
		t.Errorf("pair = %+v, want today's stageHostname/stageSetupName spelling", rt)
	}
	if got, want := rt.StageRefs(), []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}; !slices.Equal(got, want) {
		t.Errorf("StageRefs() = %+v, want %+v", got, want)
	}
}

func TestValidateBootstrapTargets_PairStages(t *testing.T) {
	t.Parallel()
	pair := func(rt RuntimeTarget) []BootstrapTarget {
		rt.DevHostname = "appdev"
		rt.Type = "nodejs@22"
		rt.BootstrapMode = topology.PlanModeStandard
		return []BootstrapTarget{{Runtime: rt}}
	}
	tests := []struct {
		name    string
		targets []BootstrapTarget
		live    []platform.ServiceStack
		wantErr string
	}{
		{
			name:    "a pair with three distinct stages validates",
			targets: pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage"}, {Hostname: "workerstage"}}}),
		},
		{
			name:    "a duplicate stage hostname in a pair is refused before import",
			targets: pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "webstage"}}}),
			wantErr: `duplicate stage hostname "webstage"`,
		},
		{
			name:    "a stage named like its dev half is refused",
			targets: pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "appdev"}}}),
			wantErr: `duplicate stage hostname "appdev"`,
		},
		{
			name: "a stage hostname another target already plans is refused",
			targets: append(pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "apistage"}}}),
				BootstrapTarget{Runtime: RuntimeTarget{DevHostname: "apistage", Type: "go@1", BootstrapMode: topology.PlanModeSimple}}),
			wantErr: `"apistage" is planned twice`,
		},
		{
			name:    "an invalid stage hostname is refused",
			targets: pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "web_stage"}}}),
			wantErr: `stageHostname "web_stage"`,
		},
		{
			name:    "stageHostname and stages together are refused",
			targets: pair(RuntimeTarget{ExplicitStage: "appstage", Stages: []topology.StageRef{{Hostname: "webstage"}}}),
			wantErr: "either stageHostname or stages",
		},
		{
			name:    "a second stage colliding with a live service is refused for a new pair",
			targets: pair(RuntimeTarget{Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "apistage"}}}),
			live:    []platform.ServiceStack{{Name: "apistage"}},
			wantErr: "apistage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateBootstrapTargets(tt.targets, nil, tt.live)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
