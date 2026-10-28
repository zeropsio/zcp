// Tests for: tools/hq_recipe_edit.go — the Mate's agent corrects a tier on
// main of the recipe repository with a change of its own: zcp opens it on the
// agent's branch and leaves it, and the files it lands, as the group's
// (docs/spec-mate.md §10.10, D37).
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// recipeEditAnswer is group-recipe's answer to the agent's own tier edit.
type recipeEditAnswer struct {
	Change    int    `json:"change"`
	ChangeURL string `json:"changeUrl"`
	Branch    string `json:"branch"`
	Created   bool   `json:"created"`
	Message   string `json:"message"`
}

// editRecipe is the agent's zerops_workflow action="group-recipe"
// branch=<branch> title=<title> over the lab: its answer (nil when it names
// no change), the text, and whether it is a refusal.
func (l *hqLab) editRecipe(client platform.Client, branch, title string) (*recipeEditAnswer, string, bool) {
	l.t.Helper()
	result, _, _ := handleGroupRecipeAction(l.t.Context(), client, l.hq.srv.Client(), l.rt, l.stateDir,
		WorkflowInput{Action: "group-recipe", Branch: branch, Title: title})
	text := getTextContent(l.t, result)
	if result.IsError {
		return nil, text, true
	}
	var answer recipeEditAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil || answer.Change == 0 {
		return nil, text, false
	}
	return &answer, text, false
}

// pushRecipeBranch is the agent's own push of branch in the recipe
// repository: path edited over main, one commit under the robot identity
// zcp's own commits carry too. It answers the commit.
func (f *fakeHQ) pushRecipeBranch(branch, path string, edit func(string) string) string {
	f.t.Helper()
	body := f.recipeFiles("main")[path]
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := f.repoDir(f.appID, hq.RecipeRepo)
	main := f.git(f.t.Context(), dir, "rev-parse", "refs/heads/main")
	tree := f.recipeTreeWith(main, path, edit(body))
	commit := f.git(f.t.Context(), dir, "-c", "user.name="+ops.DeployGitIdentity.Name, "-c", "user.email="+ops.DeployGitIdentity.Email,
		"commit-tree", tree, "-p", main, "-m", "recipe: lean production")
	f.git(f.t.Context(), dir, "update-ref", "refs/heads/"+branch, commit)
	f.writeRefs()
	return commit
}

// leanProduction is flip.studio's correction of its Small Production tier
// (2026-10-25): shared CPU where zcp's floor wrote dedicated.
func leanProduction(body string) string {
	return strings.ReplaceAll(body, "cpuMode: DEDICATED", "cpuMode: SHARED")
}

// TestGroupRecipeEdit_AMergedTierCorrectedOnTheMatesOwnChange_ZcpLeavesItAlone
// is flip.studio's Small Production (2026-10-25): zcp drafted it with the
// production floors, a person merged it, and the Mate's correction had no way
// in — a push to the merged change's branch was refused (change_closed), one
// to a branch no change binds too (unknown_change), and group-recipe
// scaling= proposed nothing, since it proposes the Mate's own scale with the
// floors applied. The agent opens a change of its own in the recipe
// repository on its branch; zcp pushes nothing onto it, a scaling proposal
// waits for it, and once it lands no pass proposes over the tier it edited.
func TestGroupRecipeEdit_AMergedTierCorrectedOnTheMatesOwnChange_ZcpLeavesItAlone(t *testing.T) {
	t.Parallel()
	const title = "Lean production at s.flip.studio"
	for _, perBranch := range []bool{true, false} {
		t.Run(fmt.Sprintf("HQ keeps an open change per branch: %v", perBranch), func(t *testing.T) {
			t.Parallel()
			lab := newScalingLab(t)
			if perBranch {
				lab.hq.keepOpenPerBranch()
			}
			client := scalingRecipeClient(1, 0.25)
			if !strings.Contains(lab.hq.recipeFiles("main")[recipeProdTier], "cpuMode: DEDICATED") {
				t.Fatalf("the merged Small Production carries no dedicated CPU:\n%s", lab.hq.recipeFiles("main")[recipeProdTier])
			}

			answer, text, _ := lab.editRecipe(client, "recipe/lean-production", title)

			if answer == nil || answer.Change != 2 || !answer.Created {
				t.Fatalf("answer = %s, want change #2 opened in the recipe repository", text)
			}
			change := lab.hq.changeIn(hq.RecipeRepo, 2)
			if change == nil || change.Title != title || change.State != hq.ChangeOpen {
				t.Fatalf("change #2 = %+v, want open and titled %q", change, title)
			}
			wantBranch := map[bool]string{true: "recipe/lean-production", false: recipeBranch(2)}[perBranch]
			if answer.Branch != wantBranch || !strings.Contains(answer.Message, wantBranch) {
				t.Errorf("branch = %q, message %q, want the change's branch %q named", answer.Branch, answer.Message, wantBranch)
			}
			if answer.ChangeURL != lab.hq.srv.URL+"/changes/"+labApp+"/"+hq.RecipeRepo+"/2" {
				t.Errorf("changeUrl = %q", answer.ChangeURL)
			}
			repoURL, err := hq.RepoURLAt(lab.rt.HQ.Origin, labOrg, labApp, hq.RecipeRepo)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(answer.Message, repoURL) {
				t.Errorf("message %q does not say where the recipe repository is (%s)", answer.Message, repoURL)
			}

			head := lab.hq.pushRecipeBranch(wantBranch, recipeProdTier, leanProduction)

			if outcome := lab.proposeRecipe(client); outcome.Committed {
				t.Errorf("a recipe pass pushed onto the Mate's own change: %+v", outcome)
			}
			if scaled, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search"); scaled != nil {
				t.Errorf("a scaling proposal went ahead beside the Mate's own change: %s", text)
			}
			if got := lab.hq.recipeHead(wantBranch); got != head {
				t.Fatalf("the Mate's branch moved from %s to %s", head, got)
			}

			lab.hq.mergeChange(hq.RecipeRepo, 2)
			landed := lab.hq.recipeFiles("main")[recipeProdTier]
			if !strings.Contains(landed, "cpuMode: SHARED") || strings.Contains(landed, "cpuMode: DEDICATED") {
				t.Fatalf("the correction did not land:\n%s", landed)
			}
			if outcome := lab.proposeRecipe(client); outcome.Committed || outcome.Created {
				t.Errorf("a pass proposed over the tier the Mate corrected: %+v", outcome)
			}
			if got := lab.hq.recipeFiles("main")[recipeProdTier]; got != landed {
				t.Errorf("main's Small Production changed after the correction landed:\n%s", got)
			}
		})
	}
}

// TestGroupRecipeEdit_TheMatesRecipeChangeOpen_GoesOnIt: this Mate keeps one
// recipe proposal open at a time, so while zcp's own proposal is open the
// agent's edit goes on its branch, and nothing else is opened; a commit of
// the agent's there makes the change its own (hq_recipe_respect.go).
func TestGroupRecipeEdit_TheMatesRecipeChangeOpen_GoesOnIt(t *testing.T) {
	t.Parallel()
	lab := newScalingLab(t)
	client := scalingRecipeClient(1, 0.25)
	if scaled, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search"); scaled == nil || scaled.Change != 2 {
		t.Fatalf("scaling proposal = %s", text)
	}
	opens := lab.hq.callCount("POST /api/mate/changes")

	answer, text, _ := lab.editRecipe(client, "recipe/lean-production", "Lean production")

	if answer == nil || answer.Change != 2 || answer.Created {
		t.Fatalf("answer = %s, want the open change #2", text)
	}
	if answer.Branch != recipeBranch(2) || !strings.Contains(answer.Message, recipeBranch(2)) {
		t.Errorf("branch = %q, message %q, want change #2's branch", answer.Branch, answer.Message)
	}
	if !strings.Contains(answer.Message, scalingTitle) {
		t.Errorf("message %q does not name the open change's title", answer.Message)
	}
	if asked := lab.hq.callCount("POST /api/mate/changes") - opens; asked != 0 {
		t.Errorf("HQ was asked to open a change %d times", asked)
	}
	if n := lab.hq.openCount(hq.RecipeRepo); n != 1 {
		t.Errorf("%d recipe changes open, want 1", n)
	}
}

// TestGroupRecipeEdit_Refusals: the agent's tier edit names a branch of its
// own and its own title. main, a tag and HQ's mate/ namespace are HQ's; a
// title zcp gives its own proposals would make zcp read the change, and the
// tier it lands, as its own. Each refusal opens nothing.
func TestGroupRecipeEdit_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, branch, title, want string
	}{
		{name: "no title", branch: "recipe/lean", want: "title"},
		{name: "main", branch: "main", title: "Lean production", want: "main"},
		{name: "HQ's mate/ namespace", branch: "mate/" + labMate + "/2", title: "Lean production", want: "mate/"},
		{name: "a tag", branch: "refs/tags/v1", title: "Lean production", want: "refs/tags/v1"},
		{name: "zcp's additive title", branch: "recipe/lean", title: hq.RecipeProposalTitle, want: "zcp"},
		{name: "zcp's scale title", branch: "recipe/lean", title: "Mate: db's scale in the group recipe", want: "zcp"},
		{name: "zcp's part title", branch: "recipe/lean", title: "Mate: api in the group recipe", want: "zcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newScalingLab(t)
			opens := lab.hq.callCount("POST /api/mate/changes")

			answer, text, refused := lab.editRecipe(scalingRecipeClient(1, 0.25), tt.branch, tt.title)

			if answer != nil || !refused || !strings.Contains(text, tt.want) {
				t.Errorf("answer = %s, want a refusal naming %q", text, tt.want)
			}
			if asked := lab.hq.callCount("POST /api/mate/changes") - opens; asked != 0 {
				t.Errorf("HQ was asked to open a change %d times", asked)
			}
		})
	}
}
