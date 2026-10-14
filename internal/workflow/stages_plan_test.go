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

// A stage belongs to one pair: a plan naming a stage another pair's meta
// already records is refused before import (spec-mate §2.8a.4); the pair
// itself naming its own stages again is not.
func TestBootstrapCompletePlan_StageAnotherPairRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runtime RuntimeTarget
		wantErr string
	}{
		{
			name:    "another dev half claiming one of the pair's stages is refused",
			runtime: RuntimeTarget{DevHostname: "otherdev", Type: "nodejs@22", BootstrapMode: "standard", Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "apistage"}}},
			wantErr: `stage "apistage" is already a stage of pair "appdev"`,
		},
		{
			name:    "the pair's own dev half naming its stages again proceeds",
			runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", IsExisting: true, Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "apistage"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pair := NewServiceMeta("appdev", topology.PlanModeStandard)
			pair.Stages = []topology.StageRef{{Hostname: "apistage", SetupName: "api"}}
			pair.BootstrapSession = "earlier"
			pair.BootstrappedAt = "2026-10-14T00:00:00Z"
			if err := WriteServiceMeta(dir, pair); err != nil {
				t.Fatal(err)
			}
			eng := NewEngine(dir, EnvContainer, nil)
			if _, err := eng.BootstrapStart("proj-1", "two parts"); err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}
			_, err := eng.BootstrapCompletePlan([]BootstrapTarget{{Runtime: tt.runtime}}, nil, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("BootstrapCompletePlan: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
