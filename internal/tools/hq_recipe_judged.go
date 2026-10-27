package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Core lands the Mate's import-files change in the recipe repository by
// itself, judging it once, on its push (reconcileGroupRecipe). What it judged
// is in the Mate's state: how the change merges with main, and Core's refusal
// to land it (hq.MateChange.Verdict). A Mate's HQ pass answers it, so the
// change never waits for a person on something zcp can do itself:
//
//   - in conflict with main — a sibling's tier landed first — the recipe
//     reconcile runs again: the branch takes main in (a commit with main as a
//     second parent, only what main lacks added), and Core lands it, or
//     closes it as empty once main carries every tier;
//   - refused head_moved — a push arrived while Core judged — the change is
//     pushed once more, its tree unchanged, to be judged again; once per
//     change, so a refusal that persists never loops;
//   - refused for anything else — recipe_changes_files, files_past_bound, a
//     git failure — it is a person's to land or close: zcp never pushes it
//     again for it, and the pass names it.

// recipeJudgedDir keeps, per change, that zcp pushed it again for a
// head_moved refusal: <stateDir>/hq-recipe-repushed/<appID>-<number>.
const recipeJudgedDir = "hq-recipe-repushed"

// recipeVerdictHeadMoved is Core's refusal of a recipe change whose head a
// push moved while it judged: judged again on the next push.
const recipeVerdictHeadMoved = "head_moved"

// recipeChangeTurn is a Mate pass's turn for its own open import-files change
// (hq.RecipeProposalTitle in hq.RecipeRepo) as state names it; one line for
// what it did or what waits for a person, none for a change in step with
// main that Core refused nothing of. recipeFollows: the recipe reconcile runs
// right after this pass (bootstrap and adopt), so a conflict is left to it.
func recipeChangeTurn(ctx context.Context, client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string, hqc hq.Client, state hq.MateState, recipeFollows bool) []string {
	open := openRecipeChange(state)
	if !rt.MateEnabled || state.AppID == nil || open == nil || open.Title == nil || *open.Title != hq.RecipeProposalTitle {
		return nil
	}
	app := *state.AppID
	where := hqc.ChangeURL(app, hq.RecipeRepo, open.Number)
	switch {
	case open.Mergeability == hq.MergeConflict:
		if recipeFollows {
			return nil
		}
		line := reconcileGroupRecipe(ctx, client, httpClient, rt, stateDir)
		if line == "" {
			line = "it already holds main"
		}
		return []string{fmt.Sprintf("the group's import files change #%d (%s) is in conflict with main — a tier landed there first — so zcp composed it again from main: %s", open.Number, where, line)}
	case open.Verdict == "":
		return nil
	case open.Verdict != recipeVerdictHeadMoved:
		return []string{fmt.Sprintf("Core did not land the group's import files change #%d (%s): %s — it waits for a person, who lands or closes it in HQ; zcp pushes nothing more for it", open.Number, where, open.Verdict)}
	case recipeRepushed(stateDir, app, open.Number):
		return []string{fmt.Sprintf("Core still did not land the group's import files change #%d (%s): %s, after zcp pushed it again — it waits for a person, who lands or closes it in HQ", open.Number, where, open.Verdict)}
	}
	if err := repushRecipeChange(ctx, hqc, app, open); err != nil {
		return []string{fmt.Sprintf("Core did not land the group's import files change #%d (%s): %s, and pushing it again to be judged again failed (%v) — retrying on the next pass", open.Number, where, open.Verdict, err)}
	}
	markRecipeRepushed(stateDir, app, open.Number)
	return []string{fmt.Sprintf("Core did not land the group's import files change #%d (%s): %s — a push arrived while it judged — so zcp pushed it once more, unchanged, to be judged again", open.Number, where, open.Verdict)}
}

// repushRecipeChange pushes change, unchanged, once more onto its branch: a
// commit of its head's tree on its head, which Core judges as a push of its
// own.
func repushRecipeChange(ctx context.Context, hqc hq.Client, appID string, change *hq.MateChange) error {
	scratch, err := hqc.OpenScratch(ctx, appID, hq.RecipeRepo)
	if err != nil {
		return fmt.Errorf("prepare the recipe's scratch repository: %w", err)
	}
	defer scratch.Close()
	branch := hqc.BranchOf(change.Number, change.Branch)
	head, found, err := scratch.Fetch(ctx, branch)
	switch {
	case err != nil:
		return fmt.Errorf("read the change's branch: %w", err)
	case !found:
		return fmt.Errorf("the change's branch %q holds nothing", branch)
	}
	tree, err := scratch.Tree(ctx, head)
	if err != nil {
		return fmt.Errorf("read the change's tree: %w", err)
	}
	commit, err := scratch.Commit(ctx, hq.CommitSpec{Tree: tree, Parents: []string{head},
		Message: "recipe: judge the proposal again", Name: ops.DeployGitIdentity.Name, Email: ops.DeployGitIdentity.Email})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if err := scratch.Push(ctx, commit, branch); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

// recipeRepushedPath is where zcp keeps that it pushed change number of
// appID's recipe repository again.
func recipeRepushedPath(stateDir, appID string, number int) string {
	return filepath.Join(stateDir, recipeJudgedDir, appID+"-"+strconv.Itoa(number))
}

// recipeRepushed reports whether zcp already pushed change number again.
func recipeRepushed(stateDir, appID string, number int) bool {
	_, err := os.Stat(recipeRepushedPath(stateDir, appID, number))
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

// markRecipeRepushed keeps, best-effort, that zcp pushed change number again.
func markRecipeRepushed(stateDir, appID string, number int) {
	path := recipeRepushedPath(stateDir, appID, number)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = os.WriteFile(path, nil, 0o600)
	}
}
