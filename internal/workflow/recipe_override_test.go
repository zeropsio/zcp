package workflow

import (
	"strings"
	"testing"
)

// recipeDotnetShape is a minimal dotnet-hello-world-style recipe: one runtime
// pair (appdev / appstage) plus a managed postgres dependency. Every F6
// rewrite scenario exercises this shape or a trimmed variant.
const recipeDotnetShape = `project:
  name: dotnet-hello-world-agent
services:
  - hostname: appdev
    type: dotnet@9
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/dotnet-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
  - hostname: appstage
    type: dotnet@9
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/dotnet-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
    verticalAutoscaling:
      minRam: 0.25
`

// recipeDevOnly is a dev-mode recipe with a single runtime service (no stage).
const recipeDevOnly = `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/nodejs-hello-world-app
    enableSubdomainAccess: true
`

func TestRewriteRecipeImportYAML(t *testing.T) {
	t.Parallel()

	planMatching := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname:   "appdev",
				ExplicitStage: "appstage",
				Type:          "dotnet@9",
				BootstrapMode: PlanModeStandard,
			},
			Dependencies: []Dependency{{
				Hostname: "db", Type: "postgresql@16", Mode: ModeNonHA, Resolution: ResolutionCreate,
			}},
		}},
	}

	planRenamedRuntime := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname:   "uploaddev",
				ExplicitStage: "uploadstage",
				Type:          "dotnet@9",
				BootstrapMode: PlanModeStandard,
			},
			Dependencies: []Dependency{{
				Hostname: "db", Type: "postgresql@16", Mode: ModeNonHA, Resolution: ResolutionCreate,
			}},
		}},
	}

	planAdoptDb := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname:   "uploaddev",
				ExplicitStage: "uploadstage",
				Type:          "dotnet@9",
				BootstrapMode: PlanModeStandard,
			},
			Dependencies: []Dependency{{
				Hostname: "db", Type: "postgresql@16", Mode: ModeNonHA, Resolution: ResolutionExists,
			}},
		}},
	}

	planManagedRename := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname:   "uploaddev",
				ExplicitStage: "uploadstage",
				Type:          "dotnet@9",
				BootstrapMode: PlanModeStandard,
			},
			Dependencies: []Dependency{{
				Hostname: "mydb", Type: "postgresql@16", Mode: ModeNonHA, Resolution: ResolutionCreate,
			}},
		}},
	}

	planTypeMismatch := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname: "xdev", ExplicitStage: "xstage",
				Type: "nodejs@22", BootstrapMode: PlanModeStandard,
			},
		}},
	}

	planDevModeOnly := &ServicePlan{
		Targets: []BootstrapTarget{{
			Runtime: RuntimeTarget{
				DevHostname: "workdev", Type: "nodejs@22", BootstrapMode: PlanModeDev,
			},
		}},
	}

	type predicate func(t *testing.T, out string)

	containsAll := func(needles ...string) predicate {
		return func(t *testing.T, out string) {
			t.Helper()
			for _, n := range needles {
				if !strings.Contains(out, n) {
					t.Errorf("output missing substring %q. Output:\n%s", n, out)
				}
			}
		}
	}
	containsNone := func(needles ...string) predicate {
		return func(t *testing.T, out string) {
			t.Helper()
			for _, n := range needles {
				if strings.Contains(out, n) {
					t.Errorf("output must NOT contain substring %q. Output:\n%s", n, out)
				}
			}
		}
	}
	both := func(ps ...predicate) predicate {
		return func(t *testing.T, out string) {
			t.Helper()
			for _, p := range ps {
				p(t, out)
			}
		}
	}

	cases := []struct {
		name    string
		recipe  string
		plan    *ServicePlan
		check   predicate
		wantErr string // substring match in error message
	}{
		{
			name:   "nil plan returns recipe verbatim",
			recipe: recipeDotnetShape,
			plan:   nil,
			check: func(t *testing.T, out string) {
				t.Helper()
				if out != recipeDotnetShape {
					t.Errorf("want verbatim recipe, got diff.\nwant:\n%s\ngot:\n%s", recipeDotnetShape, out)
				}
			},
		},
		{
			name:   "empty plan targets returns recipe verbatim",
			recipe: recipeDotnetShape,
			plan:   &ServicePlan{},
			check: func(t *testing.T, out string) {
				t.Helper()
				if out != recipeDotnetShape {
					t.Errorf("want verbatim recipe, got diff")
				}
			},
		},
		{
			name:   "plan matches recipe hostnames — all hostnames preserved",
			recipe: recipeDotnetShape,
			plan:   planMatching,
			check:  containsAll("hostname: appdev", "hostname: appstage", "hostname: db"),
		},
		{
			name:   "runtime rename — dev and stage renamed, managed kept",
			recipe: recipeDotnetShape,
			plan:   planRenamedRuntime,
			check: both(
				containsAll("hostname: uploaddev", "hostname: uploadstage", "hostname: db"),
				containsNone("hostname: appdev", "hostname: appstage"),
			),
		},
		{
			name:   "EXISTS dep drops managed service entry from output",
			recipe: recipeDotnetShape,
			plan:   planAdoptDb,
			check: both(
				containsAll("hostname: uploaddev", "hostname: uploadstage"),
				containsNone("hostname: db", "type: postgresql@16"),
			),
		},
		{
			name:    "managed rename rejected",
			recipe:  recipeDotnetShape,
			plan:    planManagedRename,
			wantErr: "managed service",
		},
		{
			name:    "runtime type mismatch — no recipe service matches plan target type",
			recipe:  recipeDotnetShape,
			plan:    planTypeMismatch,
			wantErr: "no recipe service matches",
		},
		{
			name:    "malformed YAML — parse error",
			recipe:  "project:\n  : invalid: [\n",
			plan:    &ServicePlan{Targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "a", Type: "dotnet@9"}}}},
			wantErr: "parse",
		},
		{
			name:    "recipe without services section — error",
			recipe:  "project:\n  name: empty\n",
			plan:    &ServicePlan{Targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "a", Type: "dotnet@9"}}}},
			wantErr: "services",
		},
		{
			name:   "preserves non-hostname fields",
			recipe: recipeDotnetShape,
			plan:   planRenamedRuntime,
			check: containsAll(
				"type: dotnet@9",
				"zeropsSetup: dev",
				"zeropsSetup: prod",
				"buildFromGit: https://github.com/zerops-recipe-apps/dotnet-hello-world-app",
				"enableSubdomainAccess: true",
				"minRam: 0.5",
				"type: postgresql@16",
				"mode: NON_HA",
				"priority: 10",
			),
		},
		{
			name:   "dev-mode recipe — single runtime service renamed",
			recipe: recipeDevOnly,
			plan:   planDevModeOnly,
			check: both(
				containsAll("hostname: workdev", "zeropsSetup: dev"),
				containsNone("hostname: appdev"),
			),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := RewriteRecipeImportYAML(tc.recipe, tc.plan)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil.\nOutput:\n%s", tc.wantErr, out)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}
}
