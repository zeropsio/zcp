// Tests for: tools/hq_recipe_judged.go — a Mate's HQ pass answers what Core
// judged of the Mate's own import-files change: in conflict with main, it is
// composed again from main; refused head_moved, it is pushed once more to be
// judged again; refused for anything else, a person takes it, and the pass
// says so (docs/spec-mate.md §10.10, the group's recipe).
package tools

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// judgedRecipeLab is the recipe lab with the Mate's import-files change open
// as change #1 in the recipe repository, and main then moved by a sibling's
// tier landing first.
func judgedRecipeLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newRecipeLab(t)
	lab.hq.keepOpenPerBranch()
	lab.hq.seed(hq.RecipeRepo)
	if outcome := lab.proposeRecipe(lab.mock); !outcome.Created || outcome.Change != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{"3 — Stage/import.yaml": "project:\n  name: sibling stage\nservices:\n  - hostname: app\n    zeropsSetup: prod\n"})
	return lab
}

// judge is Core's verdict on the recipe repository's change number: how it
// merges with main now, and its refusal to land it ("" for none).
func (f *fakeHQ) judge(number int, mergeability, verdict string) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	change := f.find(f.appID, hq.RecipeRepo, number, hq.ChangeOpen)
	if change == nil {
		f.t.Fatalf("no open change #%d in the recipe repository", number)
	}
	change.Mergeability = mergeability
	f.verdicts[hq.RecipeRepo+"#"+strconv.Itoa(number)] = verdict
}

// TestHQPass_RecipeChangeJudged: the pass reads Core's verdict on the Mate's
// own import-files change. In conflict with main — a sibling's tier landed
// first — the recipe reconcile runs again: the branch takes main in, so Core
// lands it or closes it as empty. Refused head_moved (a push arrived while
// Core judged), it is pushed once more, its tree the same, to be judged
// again — once: a second pass that still reads head_moved pushes nothing and
// names the change for a person. Refused for anything else
// (recipe_changes_environments, files_past_bound, a git failure) it is a
// person's: never pushed again, named in the pass's report. Clean and
// unrefused, the change is left as it is.
func TestHQPass_RecipeChangeJudged(t *testing.T) {
	requireGitForLab(t)
	t.Parallel()
	tests := []struct {
		name         string
		mergeability string
		verdict      string
		wantPushes   int
		wantMain     bool
		wantLine     []string
		wantAgain    []string
	}{
		{name: "conflict", mergeability: hq.MergeConflict, wantPushes: 1, wantMain: true,
			wantLine: []string{"change #1", "conflict", "main"}},
		{name: "clean", mergeability: hq.MergeClean},
		{name: "head_moved", mergeability: hq.MergeClean, verdict: "head_moved", wantPushes: 1,
			wantLine: []string{"change #1", "head_moved", "pushed"}, wantAgain: []string{"change #1", "head_moved", "person"}},
		{name: "recipe_changes_environments", mergeability: hq.MergeClean, verdict: "recipe_changes_environments",
			wantLine: []string{"change #1", "recipe_changes_environments", "person"}, wantAgain: []string{"recipe_changes_environments"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := judgedRecipeLab(t)
			lab.hq.judge(1, tt.mergeability, tt.verdict)
			before := lab.hq.recipeHead(recipeBranch(1))

			report := lab.passReport()
			after := lab.hq.recipeHead(recipeBranch(1))
			again := lab.passReport()
			last := lab.hq.recipeHead(recipeBranch(1))

			pushes := 0
			if after != before {
				pushes++
			}
			if last != after {
				pushes++
			}
			if pushes != tt.wantPushes {
				t.Errorf("pushes = %d, want %d:\n%s\n---\n%s", pushes, tt.wantPushes, report, again)
			}
			if tt.wantMain && !lab.hq.recipeHolds(lab.hq.recipeHead("main"), recipeBranch(1)) {
				t.Errorf("the branch did not take main in:\n%s", report)
			}
			if tt.verdict == "head_moved" && after != before {
				if lab.hq.git(t.Context(), lab.hq.repoDir(labApp, hq.RecipeRepo), "rev-parse", before+"^{tree}") !=
					lab.hq.git(t.Context(), lab.hq.repoDir(labApp, hq.RecipeRepo), "rev-parse", after+"^{tree}") {
					t.Error("the push for a head_moved verdict changed what the change holds")
				}
			}
			for _, want := range tt.wantLine {
				if !strings.Contains(report, want) {
					t.Errorf("the pass lacks %q:\n%s", want, report)
				}
			}
			for _, want := range tt.wantAgain {
				if !strings.Contains(again, want) {
					t.Errorf("the next pass lacks %q:\n%s", want, again)
				}
			}
			if tt.wantLine == nil && strings.Contains(report, "change #1") {
				t.Errorf("a clean, unrefused change is worth no words:\n%s", report)
			}
		})
	}
}
