// Tests for: tools/hq_recipe_edit.go — the Mate's agent corrects a tier on
// main of the recipe repository with a change of its own: zcp opens it on the
// agent's branch and leaves it, and the files it lands, as the group's
// (docs/spec-mate.md §10.10, D37).
package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
		WorkflowInput{Action: "group-recipe", Branch: branch, Title: title}, l.schemas)
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

// matePush is the agent's own push of branch to the recipe repository, as
// plain git in its shell makes it: path edited over base (main, or a branch
// the repository holds), one commit under the robot identity zcp's own
// commits carry too, pushed over HQ's git with the Mate's key, so HQ's ref
// rules decide it (the push hook, newFakeHQ). It answers the commit, git's
// output, and an error when HQ refused it.
func (f *fakeHQ) matePush(base, branch, path string, edit func(string) string) (commit, out string, err error) {
	f.t.Helper()
	work := filepath.Join(f.t.TempDir(), "recipe")
	f.git(f.t.Context(), "", "clone", "-q", f.repoDir(labApp, hq.RecipeRepo), work)
	f.git(f.t.Context(), work, "checkout", "-q", "-B", "work", "origin/"+base)
	file := filepath.Join(work, path)
	body, readErr := os.ReadFile(file)
	if readErr != nil {
		f.t.Fatal(readErr)
	}
	if writeErr := os.WriteFile(file, []byte(edit(string(body))), 0o600); writeErr != nil {
		f.t.Fatal(writeErr)
	}
	f.git(f.t.Context(), work, "add", "-A")
	f.git(f.t.Context(), work, "-c", "user.name="+ops.DeployGitIdentity.Name, "-c", "user.email="+ops.DeployGitIdentity.Email,
		"commit", "-qm", "recipe: lean production")
	commit = f.git(f.t.Context(), work, "rev-parse", "HEAD")
	cmd := exec.CommandContext(f.t.Context(), "git", "-C", work, "-c", "http.sslCAInfo="+f.caFile,
		"-c", "http.extraHeader=Authorization: Project "+labKey,
		"push", f.srv.URL+"/git/"+labOrg+"/"+labApp+"/"+hq.RecipeRepo+".git", "HEAD:refs/heads/"+branch)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	output, err := cmd.CombinedOutput()
	return commit, string(output), err
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
	tests := []struct {
		name string
		// perBranch: HQ keeps an open change per branch; unnamed: HQ from
		// before changes were bound to the branch they name binds its own.
		perBranch, unnamed bool
		wantBranch         string
	}{
		{name: "HQ keeping an open change per branch", perBranch: true, wantBranch: "recipe/lean-production"},
		{name: "HQ keeping one open change per repository", wantBranch: "recipe/lean-production"},
		{name: "HQ binding a change to a branch of its own", unnamed: true, wantBranch: recipeBranch(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newScalingLab(t)
			if tt.perBranch {
				lab.hq.keepOpenPerBranch()
			}
			lab.hq.mu.Lock()
			lab.hq.unnamedBranches = tt.unnamed
			lab.hq.mu.Unlock()
			client := scalingRecipeClient(1, 0.25)
			if !strings.Contains(lab.hq.recipeFiles("main")[recipeProdTier], "cpuMode: DEDICATED") {
				t.Fatalf("the merged Small Production carries no dedicated CPU:\n%s", lab.hq.recipeFiles("main")[recipeProdTier])
			}
			// flip.studio's two pushes, as HQ refused them: onto the merged
			// change's branch, and to a mate/ branch no change binds.
			for _, push := range []struct{ base, ref, reason string }{
				{recipeBranch(1), recipeBranch(1), "change_closed"},
				{"main", recipeBranch(2), "unknown_change"},
			} {
				if _, out, err := lab.hq.matePush(push.base, push.ref, recipeProdTier, leanProduction); err == nil || !strings.Contains(out, push.reason) {
					t.Fatalf("push to %s: err %v, output %s — want HQ to refuse it (%s)", push.ref, err, out, push.reason)
				}
			}

			answer, text, _ := lab.editRecipe(client, "recipe/lean-production", title)

			if answer == nil || answer.Change != 2 || !answer.Created {
				t.Fatalf("answer = %s, want change #2 opened in the recipe repository", text)
			}
			change := lab.hq.changeIn(hq.RecipeRepo, 2)
			if change == nil || change.Title != title || change.State != hq.ChangeOpen {
				t.Fatalf("change #2 = %+v, want open and titled %q", change, title)
			}
			wantBranch := tt.wantBranch
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
			// HQ judges a recipe change on a push to its branch
			// (mate-hq apps/hq/src/refMoves/recipes.ts): a branch pushed
			// before the change opened waits for one more push.
			if !strings.Contains(answer.Message, "judges it on each push") {
				t.Errorf("message %q does not say HQ judges the change on a push to it", answer.Message)
			}

			head, out, err := lab.hq.matePush("main", wantBranch, recipeProdTier, leanProduction)
			if err != nil {
				t.Fatalf("HQ refused the agent's push to %s: %v\n%s", wantBranch, err, out)
			}

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
			// The project changes shape: zcp proposes its own tiers anew, and
			// never the one the Mate corrected.
			grown := recipeReconcileClient(
				platform.ServiceStack{ID: "svc-search", Name: "search", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch:single@1.44", ServiceStackTypeCategoryName: "USER"},
					CustomAutoscaling:    &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 1, MaxRAM: 48, MinFreeRAMGB: 0.25}},
				platform.ServiceStack{ID: "svc-cache", Name: "cache", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}})
			outcome := lab.proposeRecipe(grown)
			if !slices.Contains(outcome.Replaced, "3 — Stage") {
				t.Fatalf("the grown project proposed none of zcp's own tiers anew: %+v", outcome)
			}
			if slices.Contains(outcome.Replaced, "4 — Small Production") {
				t.Errorf("zcp proposed the Small Production the Mate corrected anew: %+v", outcome)
			}
			if outcome.Change != 0 && outcome.Branch != "" {
				if got := lab.hq.recipeFiles(outcome.Branch)[recipeProdTier]; got != landed {
					t.Errorf("change #%d writes over the Small Production the Mate corrected:\n%s", outcome.Change, got)
				}
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

// TestGroupRecipe_NothingToPropose_NamesTheTierEdit: every recipe call that
// finds nothing to propose over the tiers on main says how the agent changes
// what no proposal of zcp's can — the call that opens a change of its own.
// flip.studio's agent, told "nothing to propose" by scaling=, found no way
// to a correction (2026-10-25).
func TestGroupRecipe_NothingToPropose_NamesTheTierEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input WorkflowInput
	}{
		{name: "the recipe outright", input: WorkflowInput{Action: "group-recipe"}},
		{name: "a scale", input: WorkflowInput{Action: "group-recipe", Scaling: "search"}},
		{name: "a managed service main carries", input: WorkflowInput{Action: "group-recipe", Service: "db"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newScalingLab(t)

			result, _, _ := handleGroupRecipeAction(t.Context(), scalingRecipeClient(1, 0.25), lab.hq.srv.Client(), lab.rt, lab.stateDir, tt.input, lab.schemas)
			text := getTextContent(t, result)

			if result.IsError || !strings.Contains(text, `action=\"group-recipe\" branch=`) {
				t.Errorf("answer = %s, want it to name the tier edit call", text)
			}
		})
	}
}
