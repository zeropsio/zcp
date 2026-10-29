// Tests for: tools/hq_recipe.go — the person's choice of what serves only
// their Mate, recorded at their word by zerops_workflow action="group-recipe"
// mateOnly={...} and honoured by every recipe pass after (docs/spec-mate.md
// §2.8a.5, §10.10).
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/workflow"
)

// chooseMateOnly is the agent's group-recipe mateOnly={...} over the lab,
// through the workflow tool's own dispatch.
func (l *hqLab) chooseMateOnly(choice map[string]bool) *mcp.CallToolResult {
	l.t.Helper()
	engine := workflow.NewEngine(l.stateDir, workflow.EnvContainer, nil)
	input := WorkflowInput{Action: "group-recipe", MateOnly: choice}
	result, _, _ := handleWorkflowAction(l.t.Context(), labMate, engine, mateOwnRecipeClient(), l.hq.srv.Client(), l.schemas, nil, input, l.stateDir, "zcp", nil, nil, l.rt, "")
	return result
}

// TestGroupRecipe_TheMateOnlyChoice_IsKeptAndEveryPassHonoursIt: the person
// says the owner's cloud agent — which zcp's address rule keeps on the AI
// Agent tier, its server the Mate's own dev address — belongs to the app.
// The call records it and proposes the recipe with the agent and the volume
// it keeps its home on in Stage and Small Production, and a later pass
// composes the same, pushing nothing new.
func TestGroupRecipe_TheMateOnlyChoice_IsKeptAndEveryPassHonoursIt(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": mateOwnZeropsYAML})

	result := lab.chooseMateOnly(map[string]bool{"agentrunner": false})
	text := extractText(result)
	if result.IsError || !strings.Contains(text, `"agentrunner"`) {
		t.Fatalf("the choice was not recorded: %s", text)
	}
	outcome := lab.proposeRecipe(mateOwnRecipeClient())
	if outcome.Change == 0 {
		t.Fatalf("no change proposed: %+v", outcome)
	}
	if outcome.Committed {
		t.Errorf("a later pass pushed again, though the choice already composed the change: %+v", outcome)
	}
	branch := lab.hq.recipeFiles(recipeBranch(outcome.Change))
	for _, path := range []string{recipeAgentTier, recipeStageTier, recipeProdTier} {
		for _, host := range []string{"agentrunner", "agenthome", "db"} {
			if !bundle.TierListsService(branch[path], host) {
				t.Errorf("%s lacks %s, which the person said belongs to the app:\n%s", path, host, branch[path])
			}
		}
	}
	if strings.Contains(strings.Join(outcome.Warnings, "\n"), `"agentrunner" stays on the AI Agent tier only`) {
		t.Errorf("the pass still keeps agentrunner on the AI Agent tier: %v", outcome.Warnings)
	}
}

// TestGroupRecipe_TheMateOnlyChoice_RefusesADevHalf: a pair's dev half is on
// the AI Agent tier alone already, and a hostname the project does not run
// names nothing; neither choice is recorded, and each refusal says why.
func TestGroupRecipe_TheMateOnlyChoice_RefusesADevHalf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		choice map[string]bool
		want   string
	}{
		{name: "a dev half", choice: map[string]bool{"appdev": true}, want: "dev half"},
		{name: "a hostname the project does not run", choice: map[string]bool{"ghost": true}, want: `no service \"ghost\" runs`},
		{name: "a managed service", choice: map[string]bool{"db": true}, want: "managed service"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newRecipeLab(t)
			lab.hq.seed(hq.RecipeRepo)
			result := lab.chooseMateOnly(tt.choice)
			if text := extractText(result); !result.IsError || !strings.Contains(text, tt.want) {
				t.Fatalf("not refused naming %s: %s", tt.want, text)
			}
		})
	}
}
