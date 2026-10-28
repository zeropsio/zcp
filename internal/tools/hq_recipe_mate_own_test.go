// Tests for: tools/hq_recipe.go with ops/bundle — what serves a Mate itself
// stays on the AI Agent tier its project is made from, never in Stage or
// Small Production (docs/spec-mate.md §2.8a.5, §10.10).
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// mateOwnZeropsYAML is the lab pair's zerops.yaml on main of its repository
// in HQ: its dev and stage setups, the database its run reads.
const mateOwnZeropsYAML = `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: npm run dev
      envVariables:
        DATABASE_URL: ${db_connectionString}
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: npm start
      envVariables:
        DATABASE_URL: ${db_connectionString}
`

// mateOwnRecipeClient is the recipe lab's project as flip.studio's ran on
// 2026-10-28: the pair and its database, the owner's cloud agent — built
// from a public repository, its server the Mate's own dev address, its
// token one zcp can only generate — and the volume it keeps its home on.
func mateOwnRecipeClient() *platform.Mock {
	runner := publicBuildRuntime("svc-agentrunner", "agentrunner", "ubuntu@24.04", "https://github.com/acme/agent-runner")
	home := platform.ServiceStack{ID: "svc-agenthome", Name: "agenthome", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage", ServiceStackTypeCategoryName: "USER"}}
	services := []platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
		runner, home,
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: labMate, Name: "acme-mate-1", Status: "ACTIVE", SubdomainHost: "3901.prg1.zerops.app"}).
		WithServicesDirect(services).
		WithServices(services).
		WithServiceEnv("svc-agentrunner", []platform.ServiceEnvVar{
			{Key: "FLIP_SERVER", Content: "https://appdev-3901-5195.prg1.zerops.app", Type: platform.ServiceEnvUser},
			{Key: "CLAUDE_CODE_OAUTH_TOKEN", Content: "sk-ant-oat01-Xq3vR8pLm2", Type: platform.ServiceEnvUser, Sensitive: true},
		})
}

// TestGroupRecipe_TheMatesOwnServices_StayOnTheAIAgentTier is flip.studio's
// group recipe (2026-10-28): the owner's cloud agent and its home came out
// in Stage and Small Production, its server the Mate's own dev address and
// its token a generated string. A runtime wired to an address of the Mate's
// own project serves that Mate, and a volume nothing a group environment
// runs uses is its own: both stay on the AI Agent tier, the pair's part and
// its database reach every tier, and the pass says what it left out.
func TestGroupRecipe_TheMatesOwnServices_StayOnTheAIAgentTier(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": mateOwnZeropsYAML})

	outcome := lab.proposeRecipe(mateOwnRecipeClient())

	if !outcome.Created || outcome.Change == 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	branch := lab.hq.recipeFiles(recipeBranch(outcome.Change))
	for _, path := range []string{recipeAgentTier, recipeStageTier, recipeProdTier} {
		agent := path == recipeAgentTier
		for _, host := range []string{"agentrunner", "agenthome"} {
			if carried := bundle.TierListsService(branch[path], host); carried != agent {
				t.Errorf("%s carries %s = %v, want it on the AI Agent tier alone:\n%s", path, host, carried, branch[path])
			}
		}
		if !bundle.TierListsService(branch[path], "db") {
			t.Errorf("%s lacks the database the part reads:\n%s", path, branch[path])
		}
		if !agent && strings.Contains(branch[path], "-3901-") {
			t.Errorf("%s names an address of the Mate's own project:\n%s", path, branch[path])
		}
	}
	said := strings.Join(outcome.Warnings, "\n")
	for _, want := range []string{`"agentrunner" stays on the AI Agent tier only`, `"agenthome" stays on the AI Agent tier only`} {
		if !strings.Contains(said, want) {
			t.Errorf("the pass does not say %q:\n%s", want, said)
		}
	}
}
