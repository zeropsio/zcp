// Tests for: tools/hq_recipe.go — the branch of the Mate's recipe change is
// zcp's only while it holds what zcp last pushed there: a commit the Mate's
// agent or a person adds makes the change theirs, and zcp pushes nothing over
// it (docs/spec-mate.md §10.10, D36, D37).
package tools

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// theMatesWords is the Mate's agent's own edit of a tier, as flip.studio's
// was: stage and production without the owner's cloud agent.
func theMatesWords(body string) string {
	return strings.Replace(body, "project:\n", "# Without the owner's cloud agent.\nproject:\n", 1)
}

// editRecipeChange is the Mate's agent's own commit on its recipe change
// number: path edited over the change's head and pushed as plain git does,
// under the robot identity zcp's own commits carry too. It answers the
// commit.
func (f *fakeHQ) editRecipeChange(number int, path string, edit func(string) string, subject string) string {
	f.t.Helper()
	branch := recipeBranch(number)
	body := f.recipeFiles(branch)[path]
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := f.repoDir(f.appID, hq.RecipeRepo)
	head := f.git(f.t.Context(), dir, "rev-parse", "refs/heads/"+branch)
	tree := f.recipeTreeWith(head, path, edit(body))
	commit := f.git(f.t.Context(), dir, "-c", "user.name="+ops.DeployGitIdentity.Name, "-c", "user.email="+ops.DeployGitIdentity.Email,
		"commit-tree", tree, "-p", head, "-m", subject)
	f.git(f.t.Context(), dir, "update-ref", "refs/heads/"+branch, commit)
	f.writeRefs()
	return commit
}

// TestGroupRecipe_AChangeSomeoneElseEdited_IsNeverPushedOver is flip.studio's
// change #2 (2026-10-28): zcp proposed its own tiers anew, the Mate's agent
// pushed its own edit onto the change's branch, and every pass after pushed
// zcp's composition over it again. A recipe change whose branch holds a
// commit zcp did not write is the Mate's or a person's: a pass pushes nothing
// onto it — the project unchanged or changed since, the additive proposal or
// a tier proposed anew, a change zcp pushed before it kept any record — and
// says so, naming the change.
func TestGroupRecipe_AChangeSomeoneElseEdited_IsNeverPushedOver(t *testing.T) {
	t.Parallel()
	cache := platform.ServiceStack{ID: "svc-cache", Name: "cache", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}}
	tests := []struct {
		name string
		// propose leaves the change zcp proposed open, and answers its
		// number, the tier the Mate edits and the project of the next pass.
		propose func(*hqLab) (int, string, platform.Client)
	}{
		{
			name: "the additive proposal, the project unchanged",
			propose: func(lab *hqLab) (int, string, platform.Client) {
				return lab.mustPropose(lab.mock), recipeStageTier, lab.mock
			},
		},
		{
			name: "the additive proposal, the project changed since",
			propose: func(lab *hqLab) (int, string, platform.Client) {
				return lab.mustPropose(lab.mock), recipeStageTier, recipeReconcileClient(cache)
			},
		},
		{
			name: "its own tier proposed anew",
			propose: func(lab *hqLab) (int, string, platform.Client) {
				lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(lab.t, lab, labMate))
				lab.moveLabPairOntoPool()
				return lab.mustPropose(lab.mock), recipeAgentTier, lab.mock
			},
		},
		{
			name: "a change zcp pushed before it kept a record",
			propose: func(lab *hqLab) (int, string, platform.Client) {
				lab.hq.unbornRecipe()
				lab.mustPropose(lab.mock)
				tree := lab.hq.git(lab.t.Context(), lab.hq.repoDir(labApp, hq.RecipeRepo), "rev-parse", recipeBranch(1)+"^{tree}")
				lab.hq.mergeChange(hq.RecipeRepo, 1)
				lab.hq.racedRecipeDuplicate(2, tree)
				return 2, recipeStageTier, lab.mock
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newRecipeLab(t)
			number, tier, next := tt.propose(lab)
			edited := lab.hq.editRecipeChange(number, tier, theMatesWords, "recipe: stage and production without the owner's cloud agent")
			words := lab.hq.recipeFiles(recipeBranch(number))[tier]

			for pass := 1; pass <= 2; pass++ {
				outcome := lab.proposeRecipe(next)

				if head := lab.hq.recipeHead(recipeBranch(number)); head != edited {
					t.Fatalf("pass %d moved change #%d off the Mate's commit %s to %s: %+v", pass, number, edited, head, outcome)
				}
				if outcome.Committed || outcome.Created || outcome.Change != number {
					t.Errorf("pass %d: %+v, want nothing pushed and change #%d named", pass, outcome, number)
				}
				for _, want := range []string{"change #" + strconv.Itoa(number), "did not write", "pushes nothing"} {
					if !strings.Contains(outcome.Line, want) {
						t.Errorf("pass %d says %q, want it to say %q", pass, outcome.Line, want)
					}
				}
			}
			if got := lab.hq.recipeFiles(recipeBranch(number))[tier]; got != words {
				t.Errorf("the Mate's edit of %s did not stand:\n%s", tier, got)
			}
		})
	}
}

// TestGroupRecipeScaling_AChangeSomeoneElseEdited_IsNeverPushedOver: the
// agent's own scaling proposal is the Mate's change too; once its branch
// holds a commit zcp did not write, asking again pushes nothing over it and
// says why.
func TestGroupRecipeScaling_AChangeSomeoneElseEdited_IsNeverPushedOver(t *testing.T) {
	t.Parallel()
	lab := newScalingLab(t)
	if answer, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search"); answer == nil || answer.Change != 2 {
		t.Fatalf("scaling proposal = %s", text)
	}
	edited := lab.hq.editRecipeChange(2, recipeProdTier, theMatesWords, "recipe: production in our own words")

	answer, text := lab.proposeScaling(scalingRecipeClient(8, 1), "search")

	if answer != nil {
		t.Errorf("asking again over the Mate's edit answered %s, want it refused", text)
	}
	if head := lab.hq.recipeHead(recipeBranch(2)); head != edited {
		t.Errorf("asking again moved change #2 off the Mate's commit %s to %s", edited, head)
	}
	for _, want := range []string{"change #2", "did not write"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer %s does not say %q", text, want)
		}
	}
}

// mustPropose is one pass of the recipe reconcile that opens the Mate's
// recipe change; it answers its number.
func (l *hqLab) mustPropose(client platform.Client) int {
	l.t.Helper()
	outcome := l.proposeRecipe(client)
	if !outcome.Created || outcome.Change == 0 {
		l.t.Fatalf("the proposal: %+v", outcome)
	}
	return outcome.Change
}
