package bundle

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/schema"
)

// One repository, one dev service, its parts as the pair's stages
// (ServiceMeta.Stages, spec-mate §2.8a.4): every tier composed from it is
// pinned byte for byte. The one-part golden was written by the composer
// before a pair held more than one stage, so it is the single-part
// invariant: an app with one part composes as it always did.

const onePartYAML = `zerops:
  - setup: dev
    run:
      base: bun@1.3
      start: zsc noop --silent
  - setup: prod
    build:
      base: bun@1.3
      buildCommands:
        - bun install
      deployFiles: ./
    run:
      base: bun@1.3
      ports:
        - port: 3000
          httpSupport: true
      start: bun index.ts
`

func onePartInputs() GroupRecipeInputs {
	return GroupRecipeInputs{
		Name:            "mono",
		Title:           "mono",
		MateProjectName: "mono-mate",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "APP_URL", Value: "https://appstage-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
		},
		Runtimes: []GroupRuntime{{
			DevHostname: "appdev", ServiceType: "bun@1.3",
			RepoURL:   "https://hq.example.com/git/app-1/appdev",
			SetupName: "dev", ZeropsYAMLBody: onePartYAML,
			Stages: []GroupStage{{
				Hostname: "appstage", SetupName: "prod",
				ServiceEnvs: []ProjectEnvVar{{Key: "LOG_LEVEL", Value: "info"}},
			}},
			SubdomainEnabled: true,
			Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 0.5, MaxRAM: 2},
			ServiceEnvs:      []ProjectEnvVar{{Key: "LOG_LEVEL", Value: "debug"}},
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@17", Profile: "oltp-hobby"},
		},
	}
}

// assertGroupGolden composes inputs and compares every tier with
// testdata/group_recipe/<prefix>-<tier slug>.yaml, each valid against the
// import schema's structure.
func assertGroupGolden(t *testing.T, inputs GroupRecipeInputs, prefix string) {
	t.Helper()
	layout, _, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if len(layout.Tiers) != 3 {
		t.Fatalf("tiers = %d, want 3", len(layout.Tiers))
	}
	for _, tier := range layout.Tiers {
		golden := filepath.Join("testdata", "group_recipe", prefix+"-"+tier.Slug+".yaml")
		if *updateGroupGolden {
			if err := os.WriteFile(golden, []byte(tier.ImportYAML), 0o600); err != nil {
				t.Fatalf("write %s: %v", golden, err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read %s: %v (run with -update-group-golden to write it)", golden, err)
		}
		if tier.ImportYAML != string(want) {
			t.Errorf("tier %q differs from %s:\n--- got\n%s\n--- want\n%s", tier.Title, golden, tier.ImportYAML, want)
		}
		if errs := schema.ValidateImportYAMLStructure(tier.ImportYAML); len(errs) > 0 {
			t.Errorf("tier %q fails the import schema: %v", tier.Title, errs)
		}
	}
}

// A pair with one stage composes the same stage and production tier as
// before Stages[].
func TestBuildGroupRecipe_OnePart_ComposesAsBeforeStages(t *testing.T) {
	t.Parallel()
	assertGroupGolden(t, onePartInputs(), "one-part")
}

const threePartsYAML = `zerops:
  - setup: dev
    run:
      base: nodejs@22
      start: zsc noop --silent
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm --filter web build
      deployFiles: apps/web/dist
    run:
      base: nodejs@22
      start: node apps/web/server.js
  - setup: api
    build:
      base: nodejs@22
      deployFiles: apps/api
    run:
      base: nodejs@22
      start: node apps/api/index.js
  - setup: worker
    build:
      base: nodejs@22
      deployFiles: apps/worker
    run:
      base: nodejs@22
      start: node apps/worker/index.js
`

// threePartsInputs is one repository with three parts — web, api and a
// worker — as one dev service and three stages; the web reaches the api
// through a project variable naming its stage.
func threePartsInputs() GroupRecipeInputs {
	return GroupRecipeInputs{
		Name:            "mono",
		Title:           "mono",
		MateProjectName: "mono-mate",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "API_URL", Value: "http://apistage:8080"},
			{Key: "WEB_URL", Value: "https://webstage-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
		},
		Runtimes: []GroupRuntime{{
			DevHostname: "appdev", ServiceType: "nodejs@22",
			RepoURL:   "https://hq.example.com/git/app-1/appdev",
			SetupName: "dev", ZeropsYAMLBody: threePartsYAML,
			Stages: []GroupStage{
				{Hostname: "webstage", SetupName: "web"},
				{Hostname: "apistage", SetupName: "api", ServiceEnvs: []ProjectEnvVar{{Key: "API_PORT", Value: "8080"}}},
				{Hostname: "workerstage", SetupName: "worker"},
			},
			SubdomainEnabled: true,
			Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 1, MaxRAM: 4},
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@17", Profile: "oltp-hobby"},
		},
	}
}

// tierRuntimes reads a composed tier's runtimes built from a repository as
// hostname → zeropsSetup.
func tierRuntimes(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, held := range HeldRuntimes(body) {
		out[held.Hostname] = held.SetupName
	}
	return out
}

// The composer emits one stage and one production runtime per Stages[] entry.
func TestBuildGroupRecipe_ThreeParts_OneRuntimePerStage(t *testing.T) {
	t.Parallel()
	assertGroupGolden(t, threePartsInputs(), "three-parts")

	layout, _, err := BuildGroupRecipe(threePartsInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	want := map[string]map[string]string{
		"AI Agent":         {"appdev": "dev", "webstage": "web", "apistage": "api", "workerstage": "worker"},
		"Stage":            {"web": "web", "api": "api", "worker": "worker"},
		"Small Production": {"web": "web", "api": "api", "worker": "worker"},
	}
	for _, tier := range layout.Tiers {
		if got := tierRuntimes(t, tier.ImportYAML); !maps.Equal(got, want[tier.Title]) {
			t.Errorf("tier %q runtimes = %v, want %v", tier.Title, got, want[tier.Title])
		}
	}
}

// A new service's hostname is its part's — the stem of the part's stage
// (`webstage` → `web`), for every part alike — and an existing tier hostname
// is never recomputed.
func TestBuildGroupRecipe_PartHostnames_HeldByTheTier(t *testing.T) {
	t.Parallel()
	repo := "https://hq.example.com/git/app-1/appdev"
	tests := []struct {
		name string
		held []HeldRuntime
		want map[string]string
	}{
		{
			name: "no tier holds a part yet",
			want: map[string]string{"web": "web", "api": "api", "worker": "worker"},
		},
		{
			name: "a person renamed the web and the api in the tier",
			held: []HeldRuntime{
				{Hostname: "frontend", RepoURL: repo, SetupName: "web"},
				{Hostname: "backend", RepoURL: repo, SetupName: "api"},
			},
			want: map[string]string{"frontend": "web", "backend": "api", "worker": "worker"},
		},
		{
			name: "a tier holds a part of another repository",
			held: []HeldRuntime{{Hostname: "frontend", RepoURL: "https://hq.example.com/git/app-1/other", SetupName: "web"}},
			want: map[string]string{"web": "web", "api": "api", "worker": "worker"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := threePartsInputs()
			inputs.Held = tt.held
			layout, _, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				if tier.Title == "AI Agent" {
					continue
				}
				if got := tierRuntimes(t, tier.ImportYAML); !maps.Equal(got, tt.want) {
					t.Errorf("tier %q runtimes = %v, want %v", tier.Title, got, tt.want)
				}
			}
			// A value naming a stage names the runtime that holds its part.
			stage := tierDocBody(t, layout, "Stage")
			wantAPI := "API_URL: http://" + map[bool]string{true: "backend", false: "api"}[len(tt.held) == 2] + ":8080"
			if !strings.Contains(stage, wantAPI) {
				t.Errorf("Stage tier does not carry %q:\n%s", wantAPI, stage)
			}
		})
	}
}

// A part whose name another service of the group environments already has
// is left out of them and said — never renamed on a guess.
func TestBuildGroupRecipe_PartHostnameUnusable_LeftOutAndSaid(t *testing.T) {
	t.Parallel()
	inputs := threePartsInputs()
	inputs.ManagedServices = append(inputs.ManagedServices, ManagedServiceEntry{Hostname: "worker", Type: "valkey@7.2"})
	layout, warnings, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	stage := tierDocBody(t, layout, "Stage")
	if got := tierRuntimes(t, stage); !maps.Equal(got, map[string]string{"web": "web", "api": "api"}) {
		t.Errorf("Stage runtimes = %v, want web and api only", got)
	}
	if !warningsContain(warnings, `part "worker"`) {
		t.Errorf("warnings %v do not name part \"worker\"", warnings)
	}
}

func tierDocBody(t *testing.T, layout recipe.Layout, title string) string {
	t.Helper()
	for _, tier := range layout.Tiers {
		if tier.Title == title {
			return tier.ImportYAML
		}
	}
	t.Fatalf("no tier %q", title)
	return ""
}
