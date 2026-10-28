// Tests for: tools/hq_recipe.go with ops/bundle — a value naming the public
// address of a service of the Mate's own project reaches every tier as a
// reference to that service's address in the environment the tier makes
// (docs/spec-mate.md §10.10).
package tools

import (
	"strings"
	"testing"

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

// TestGroupRecipe_AnAddressOfTheMatesProject_IsTheEnvironmentsReference is
// flip.studio's group change #2 through the pass: a value holding the Mate's
// stage's address written out came out in Stage and Small Production renamed
// to a host that exists nowhere. Every tier now names the service in its own
// environment: another service by ${<host>_zeropsSubdomain} — the group
// environments' app, the AI Agent tier's appstage — and the service's own
// address by ${zeropsSubdomain}, the path after it kept. No tier names the
// Mate's own project.
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
		path, server string
	}{
		{recipeAgentTier, "APP_SERVER: ${appstage_zeropsSubdomain}/api"},
		{recipeStageTier, "APP_SERVER: ${app_zeropsSubdomain}/api"},
		{recipeProdTier, "APP_SERVER: ${app_zeropsSubdomain}/api"},
	}
	for _, tt := range tests {
		body := branch[tt.path]
		for _, want := range []string{tt.server, "PUBLIC_URL: ${zeropsSubdomain}"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not write %q:\n%s", tt.path, want, body)
			}
		}
		if strings.Contains(body, "-3901-") {
			t.Errorf("%s names an address of the Mate's own project:\n%s", tt.path, body)
		}
	}
}
