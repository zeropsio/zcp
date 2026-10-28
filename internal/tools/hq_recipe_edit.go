package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// A tier on main of the recipe repository is the group's, and every recipe
// call zcp makes proposes only what it composes from this Mate's project:
// the tiers main lacks, its own tier anew, one host's scale with the tier's
// floors applied, a part or a managed service. None of them can make a
// tier say anything the Mate does not run — production leaner than the
// floors, a service's settings, an address of its own — and HQ takes a
// Mate's push in the recipe repository only on a branch a change of its own
// binds (flip.studio, 2026-10-25: Small Production merged with HA PostgreSQL
// and two dedicated-CPU containers, and the Mate's correction was refused on
// the merged change's branch, `change_closed`, and on a branch no change
// binds, `unknown_change`, while scaling= found nothing to propose).
//
// So the agent opens a change of its own there: `zerops_workflow
// action="group-recipe" branch=<branch> title=<title>` opens the Mate's
// change in the recipe repository on that branch, and the agent pushes its
// edit of the tier to it as plain git. zcp writes nothing on it: its title
// is none of zcp's, so a recipe pass and a targeted proposal find it as the
// Mate's other recipe change and leave it as it is (writeRecipeChange), and
// a tier it lands is the group's — no squash of a change so titled reads as
// zcp's (recipeOwnSubject), and an edit of a marked tier leaves the mark
// broken — so no pass proposes zcp's composition over it again. While a
// recipe change of this Mate is open, the edit goes on its branch, as this
// Mate keeps one recipe proposal open at a time: once the agent's commit is
// on it, it is the agent's (hq_recipe_respect.go).

// recipeEditSteer is what a recipe call that proposes nothing over the tiers
// on main says of everything no proposal of zcp's can write.
const recipeEditSteer = ` To change anything else in a tier on main — production leaner than its floors, a service's settings, an address of its own — open a change of your own with zerops_workflow action="group-recipe" branch="<a branch of yours>" title="<what it does>" and push your edit of the tier to it; zcp leaves that change, and the tier it lands, as yours.`

// recipeOwnTitle is a title zcp gives its own recipe proposals: the
// additive one's, a part's or a managed service's, a scale's.
var recipeOwnTitle = regexp.MustCompile(`^(?:` + recipeOwnTitles() + `)$`)

// recipeEditResult is group-recipe's answer to the agent's own tier edit.
type recipeEditResult struct {
	GroupRepo string `json:"groupRepo"`
	RepoURL   string `json:"repoUrl,omitempty"`
	Change    int    `json:"change"`
	ChangeURL string `json:"changeUrl"`
	Branch    string `json:"branch"`
	Created   bool   `json:"created"`
	Message   string `json:"message"`
}

// handleGroupRecipeEdit is `zerops_workflow action="group-recipe"
// branch=<branch> title=<title>`: the Mate's change in the recipe repository
// on the agent's branch, for an edit of a tier main carries that no
// proposal of zcp's can make. It opens the change, or answers the Mate's
// recipe change already open, and says where to push. It runs under the
// recipe pass lock, so no pass opens zcp's own change beside it.
func handleGroupRecipeEdit(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, branch, title string,
) (*mcp.CallToolResult, any, error) {
	result, refusal := groupRecipeEdit(ctx, httpClient, rt, stateDir, branch, title)
	if refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	return jsonResult(result), nil, nil
}

// groupRecipeEdit is handleGroupRecipeEdit's answer, or why nothing was
// opened.
func groupRecipeEdit(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir, branch, title string) (recipeEditResult, *platform.PlatformError) {
	none := recipeEditResult{}
	refuse := func(code, msg, suggestion string) *platform.PlatformError {
		return platform.NewPlatformError(code, "Nothing was opened in the recipe repository: "+msg, suggestion)
	}
	if !rt.InContainer || !rt.MateEnabled {
		return none, refuse(platform.ErrPrerequisiteMissing, "the group recipe is a Mate's, and there is no Mate environment here.", "")
	}
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return none, refuse(platform.ErrPrerequisiteMissing, "this Mate is not linked to its HQ yet, so it has no recipe repository.", "")
	}
	branch, refusal := recipeEditBranch(branch)
	if refusal != nil {
		return none, refusal
	}
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return none, refuse(platform.ErrInvalidParameter, `a tier edit needs the change's title: pass title="…".`,
			"Say what the edit does in a few words, e.g. \"Lean production at its own domain\".")
	case recipeOwnTitle.MatchString(title):
		return none, refuse(platform.ErrInvalidParameter, fmt.Sprintf("%q is a title zcp gives its own recipe proposals, and zcp would read this change, and the tier it lands, as its own.", title),
			"Give the change a title of your own that says what the edit does.")
	}
	if refusal := describeTitleRefusal(title); refusal != nil {
		return none, refusal
	}
	release, err := holdRecipePass(ctx, stateDir)
	if err != nil {
		return none, refuse(platform.ErrAPIError, recipePassHeldLine(err)+".", "Call again once it finished.")
	}
	defer release()

	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	state, err := hqc.Once().Self(callCtx)
	cancel()
	if err != nil {
		return none, refuse(platform.ErrAPIError, fmt.Sprintf("HQ did not answer with this Mate's state (%v).", err), "Call again; nothing changed.")
	}
	if state.AppID == nil || *state.AppID == "" {
		return none, refuse(platform.ErrPrerequisiteMissing, "this Mate is in no application in HQ yet, so it has no recipe repository.", "")
	}
	appID := *state.AppID
	result := recipeEditResult{GroupRepo: hq.RecipeRepo}
	if url, err := hqc.RepoURL(appID, hq.RecipeRepo); err == nil {
		result.RepoURL = url
	}

	// The Mate keeps one recipe proposal open at a time: the edit goes on
	// the one open.
	if open := openRecipeChange(state); open != nil {
		result.Change, result.Branch = open.Number, hqc.BranchOf(open.Number, open.Branch)
		result.ChangeURL = hqc.ChangeURL(appID, hq.RecipeRepo, open.Number)
		result.Message = recipeEditOnOpen(result, titleOf(*open), branch)
		return result, nil
	}
	callCtx, cancel = context.WithTimeout(ctx, hqCallTimeout)
	opened, err := hqc.OpenChange(callCtx, hq.RecipeRepo, title, branch)
	cancel()
	var refused *hq.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Reason == "branch_taken":
		return none, refuse(platform.ErrInvalidParameter, fmt.Sprintf("another open change holds the branch %q in %q.", branch, hq.RecipeRepo),
			"Name a branch of your own under another name.")
	case errors.As(err, &refused) && refused.Reason == "protected_ref":
		return none, branchNotOwn(branch)
	case errors.As(err, &refused) && refused.Reason == "invalid_ref":
		return none, notABranchName(branch)
	case err != nil:
		return none, refuse(platform.ErrAPIError, fmt.Sprintf("HQ did not open a change in %q (%v).", hq.RecipeRepo, err), "Call again; nothing changed.")
	}
	number := opened.Change.Number
	result.Change, result.Created = number, opened.Created
	result.Branch = hqc.BranchOf(number, opened.Change.Branch)
	result.ChangeURL = hqc.ChangeURL(appID, hq.RecipeRepo, number)
	if !opened.Created {
		result.Message = recipeEditOnOpen(result, opened.Change.Title, branch)
		return result, nil
	}
	push, bound := result.Branch, ""
	if result.Branch != branch {
		push = branch + ":" + result.Branch
		bound = fmt.Sprintf(" (this HQ binds a change to a branch of its own, not %q)", branch)
	}
	result.Message = fmt.Sprintf("Change #%d (%s) is open in the recipe repository %q for the branch %q%s. In your own shell, where git asks `zcp hq git-credential`: %s, edit the tier's import.yaml, commit, and `git push origin %s` — never force-push, never main. The change is yours: zcp pushes nothing onto it, its recipe passes and scale or service proposals wait while it is open (this Mate keeps one recipe proposal open at a time), and a tier it lands is the group's, which no pass proposes zcp's composition over again. %s.",
		number, result.ChangeURL, hq.RecipeRepo, result.Branch, bound, recipeEditClone(result.RepoURL, branch), push, recipeLanding)
	return result, nil
}

// recipeEditBranch is the agent's branch for a tier edit, as its short name:
// one of its own, never main, a tag, a full ref or HQ's mate/ namespace.
func recipeEditBranch(input string) (string, *platform.PlatformError) {
	name, refusal := changeBranchOf(input)
	if refusal != nil {
		return "", refusal
	}
	if name == "mate" || strings.HasPrefix(name, "mate/") {
		return "", branchNotOwn(name)
	}
	return name, nil
}

// recipeEditClone is the clone that cuts branch from main of the recipe
// repository at url; the repository named when its address is unknown.
func recipeEditClone(url, branch string) string {
	if url == "" {
		return fmt.Sprintf("clone the recipe repository %q and cut %s from origin/main", hq.RecipeRepo, branch)
	}
	return fmt.Sprintf("`git -c credential.helper= -c 'credential.helper=!zcp hq git-credential' clone %s group && cd group && git checkout -b %s origin/main`", url, branch)
}

// recipeEditOnOpen is the answer when the Mate's recipe change result names
// is open already, titled title ("" unknown): the edit goes on its branch.
func recipeEditOnOpen(result recipeEditResult, title, branch string) string {
	named := ""
	if title != "" {
		named = fmt.Sprintf(" (%q)", title)
	}
	if result.Branch == branch {
		return fmt.Sprintf("Change #%d%s on %q is already open in the recipe repository %q (%s): push your edit to it (`git push origin %s`), never force-pushing. Once a commit of yours is on it, zcp pushes nothing more onto it.",
			result.Change, named, branch, hq.RecipeRepo, result.ChangeURL, branch)
	}
	return fmt.Sprintf("Change #%d%s is this Mate's open recipe change in %q (%s), on the branch %q, and this Mate keeps one recipe proposal open at a time, so nothing else was opened. Put your edit on its branch: fetch it (`git fetch origin %s && git checkout -b %s FETCH_HEAD`), commit, and `git push origin %s:%s` — never force-push. Once a commit of yours is on it the change is yours, and zcp pushes nothing more onto it; or call again once #%d is merged or closed.",
		result.Change, named, hq.RecipeRepo, result.ChangeURL, result.Branch, result.Branch, branch, branch, result.Branch, result.Change)
}
