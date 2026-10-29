// Tests for: tools/hq_recipe.go with ops/bundle — a value naming the public
// address of a service of the Mate's own project reaches every tier as that
// service's address in the environment the tier makes (docs/spec-mate.md
// §10.10).
package tools

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// subdomainZeropsYAML is the lab pair's zerops.yaml: its dev setup and its
// stage's, which serves HTTP on 3000.
const subdomainZeropsYAML = `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: npm run dev
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: npm start
`

// subdomainRecipeClient is the recipe lab's project as flip.studio's ran
// for its group change #2: the stage knows its own public address written
// out, and a runtime built from a public repository talks to the stage at
// that address.
func subdomainRecipeClient() *platform.Mock {
	runner := publicBuildRuntime("svc-agentrunner", "agentrunner", "ubuntu@24.04", "https://github.com/acme/agent-runner")
	services := []platform.ServiceStack{
		liveRuntime("svc-appdev", "appdev", "nodejs@22"),
		liveRuntime("svc-appstage", "appstage", "nodejs@22"),
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
		runner,
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: labMate, Name: "acme-mate-1", Status: "ACTIVE", SubdomainHost: "3901.prg1.zerops.app"}).
		WithServicesDirect(services).
		WithServices(services).
		WithServiceEnv("svc-appstage", []platform.ServiceEnvVar{
			{Key: "PUBLIC_URL", Content: "https://appstage-3901-3000.prg1.zerops.app", Type: platform.ServiceEnvUser},
		}).
		WithServiceEnv("svc-agentrunner", []platform.ServiceEnvVar{
			{Key: "APP_SERVER", Content: "https://appstage-3901-3000.prg1.zerops.app/api", Type: platform.ServiceEnvUser},
		})
}

// tierRuntimePriorities reads a tier's services as hostname → priority.
func tierRuntimePriorities(t *testing.T, body string) map[string]int {
	t.Helper()
	var doc struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
			Priority int    `yaml:"priority"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("tier does not parse: %v\n%s", err, body)
	}
	out := map[string]int{}
	for _, svc := range doc.Services {
		out[svc.Hostname] = svc.Priority
	}
	return out
}

// TestGroupRecipe_AnAddressOfTheMatesProject_IsTheEnvironmentsReference is
// flip.studio's group change #2 through the pass: a value holding the Mate's
// stage's address written out came out in Stage and Small Production renamed
// to a host that exists nowhere. Every tier now names the service in its own
// environment — the group environments' app, the AI Agent tier's appstage —
// with the project's own subdomain label, ${zeropsSubdomainHost}, the service's
// own address and another's alike, the path after it kept. No tier names the
// Mate's own project. The label exists from the project's creation, so the
// address needs no service created before its reader.
func TestGroupRecipe_AnAddressOfTheMatesProject_IsTheEnvironmentsReference(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": subdomainZeropsYAML})

	outcome := lab.proposeRecipe(subdomainRecipeClient())

	if !outcome.Created || outcome.Change == 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	branch := lab.hq.recipeFiles(recipeBranch(outcome.Change))
	tests := []struct {
		path, server, app string
	}{
		{recipeAgentTier, "APP_SERVER: https://appstage-${zeropsSubdomainHost}-3000.prg1.zerops.app/api", "appstage"},
		{recipeStageTier, "APP_SERVER: https://app-${zeropsSubdomainHost}-3000.prg1.zerops.app/api", "app"},
		{recipeProdTier, "APP_SERVER: https://app-${zeropsSubdomainHost}-3000.prg1.zerops.app/api", "app"},
	}
	for _, tt := range tests {
		body := branch[tt.path]
		if p := tierRuntimePriorities(t, body); p[tt.app] != p["agentrunner"] {
			t.Errorf("%s: %s at priority %d, agentrunner at %d, want the address to order neither", tt.path, tt.app, p[tt.app], p["agentrunner"])
		}
		for _, want := range []string{tt.server, "PUBLIC_URL: https://" + tt.app + "-${zeropsSubdomainHost}-3000.prg1.zerops.app"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not write %q:\n%s", tt.path, want, body)
			}
		}
		if strings.Contains(body, "-3901-") {
			t.Errorf("%s names an address of the Mate's own project:\n%s", tt.path, body)
		}
	}
}
