package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// changeOpenResult is open-change's answer.
type changeOpenResult struct {
	Service        string `json:"service"`
	Branch         string `json:"branch,omitempty"`
	PullRequest    int    `json:"pullRequest"`
	PullRequestURL string `json:"pullRequestUrl"`
	Created        bool   `json:"created"`
	Message        string `json:"message"`
}

// handleOpenChange is zerops_workflow action="open-change": the agent opens
// the change that takes a branch of its own to `main`, in its repository in
// HQ — named by the branch, and by a pair or a lease alias (which names the
// branch too) where the Mate works in several repositories. HQ binds the
// change to the branch, which may be pushed before or after; the change
// ledger keeps it, and describe-change then describes it by its branch. One
// change per branch: HQ answers the change already open on the branch, and a
// change a person handed the Mate on it is answered without asking HQ to open
// one. An HQ that keeps one open change per repository (no openPerBranch in
// the Mate's state) answers its open one whatever the branch: zcp keeps that
// change on its own branch, records nothing for the branch asked for, and
// says the work goes on the open one. In a repository whose code is kept on
// GitHub, the branch is GitHub's and the change's pull request is HQ's to
// open there; the person merges it in Mate.
func handleOpenChange(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"open-change opens a change in this Mate's HQ, and this Mate is not enrolled with one yet.",
			"Say what the work does in your answer instead.",
		), WithRecoveryStatus()), nil, nil
	}
	if strings.TrimSpace(input.Branch) == "" && strings.TrimSpace(input.Service) == "" {
		_, refusal := changeBranchOf(input.Branch)
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			`open-change needs the change's title: pass title="…".`,
			"Say what this repository's change does in a few words."), WithRecoveryStatus()), nil, nil
	}
	if refusal := describeTitleRefusal(title); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	_ = hqChanges(stateDir)
	target, err := resolveChangeTarget(stateDir, hqc.Address(), changeAsk{Service: input.Service, Branch: input.Branch, Verb: changeVerbOpen})
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	branch := target.Branch
	result := changeOpenResult{Service: strings.TrimSpace(input.Service)}
	if result.Service == "" && target.Pair != nil {
		result.Service = target.Pair.Hostname
	}

	// The Mate's own state says how the repository's code is kept, whether
	// HQ keeps an open change per branch, and names the changes handed to
	// the Mate there: one on this branch is its change, and HQ is not asked
	// to open another. A state that cannot be read leaves all of it to HQ's
	// answer.
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	state, stateErr := hqc.Once().Self(callCtx)
	cancel()
	origin := ""
	var handed []hq.MateChange
	if stateErr == nil {
		if target.Pair != nil {
			recordHQOrigin(stateDir, target.Pair, state)
		}
		if kept, ok := hqOriginIn(state, &workflow.HQRepoRef{AppID: target.AppID, Repo: target.Repo}); ok {
			origin = kept.Mode
		}
		handed = handedChangesIn(state, target.AppID, target.Repo)
		for _, h := range handed {
			held := hqc.BranchOf(h.Number, h.Branch)
			// On an HQ that keeps one open change per repository the handed
			// change is the Mate's one, whatever branch is named.
			if held != branch && state.OpenPerBranch {
				continue
			}
			recordHQChange(stateDir, target.AppID, target.Repo, h.Number, held, titleOf(h), workflow.HQChangeFromHanded)
			result.Branch, result.PullRequest = held, h.Number
			result.PullRequestURL = hqc.ChangeURL(target.AppID, target.Repo, h.Number)
			result.Message = sentenceOf(handedWork(h, target.Repo, held, !state.OpenPerBranch))
			if held == branch {
				result.Message += fmt.Sprintf(" It is the change of %q, so nothing else was opened.", branch)
			} else {
				result.Message += " Nothing else was opened: a Mate has one open change per repository."
			}
			result.Message += ` Write what it does and how you checked it with zerops_workflow action="describe-change"; the person reviews it by that.`
			return jsonResult(result), nil, nil
		}
	} else if target.Pair != nil && target.Pair.HQ.Origin != nil {
		origin = target.Pair.HQ.Origin.Mode
	}

	callCtx, cancel = context.WithTimeout(ctx, hqCallTimeout)
	opened, err := hqc.OpenChange(callCtx, target.Repo, title, branch)
	cancel()
	// HQ refuses a branch as {code, reason}: the reason names which refusal.
	var refused *hq.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Reason == "branch_taken":
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Nothing was opened: another open change holds the branch %q in %q.", branch, target.Repo),
			"Push this work to a branch of your own under another name and open its change from that."), WithRecoveryStatus()), nil, nil
	case errors.As(err, &refused) && refused.Reason == "protected_ref":
		return convertError(branchNotOwn(branch), WithRecoveryStatus()), nil, nil
	case errors.As(err, &refused) && refused.Reason == "invalid_ref":
		return convertError(notABranchName(branch), WithRecoveryStatus()), nil, nil
	case errors.As(err, &refused) && refused.Reason == "too_many_open":
		return convertError(tooManyOpen(target, branch, state, stateErr), WithRecoveryStatus()), nil, nil
	case err != nil:
		return convertError(platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Nothing was opened: HQ did not open a change in %q (%v).", target.Repo, err),
			`Call zerops_workflow action="open-change" again; the branch you pushed stays as it is.`), WithRecoveryStatus()), nil, nil
	}
	number := opened.Change.Number
	result.PullRequest, result.Created = number, opened.Created
	result.PullRequestURL = hqc.ChangeURL(target.AppID, target.Repo, number)
	// The change's branch is the one HQ names. An HQ from before changes
	// were bound to a branch they name ignores the one asked for and binds
	// a change it opens to the Mate's own (ChangeBranch); an open change it
	// answers without naming one is never on the one asked for, and stays
	// unnamed.
	held := strings.TrimPrefix(opened.Change.Branch, "refs/heads/")
	switch {
	case held != "":
		result.Branch = held
	case opened.Created:
		result.Branch = hqc.ChangeBranch(number)
	}
	// The ledger keeps the change on the branch HQ names — never on the one
	// asked for when HQ answered another.
	recordHQChange(stateDir, target.AppID, target.Repo, number, result.Branch, titleIfCreated(opened), workflow.HQChangeFromOpen)
	if !opened.Created {
		result.Message = answeredOpenChange(hqc, target.Repo, branch, held, number)
		return jsonResult(result), nil, nil
	}
	handedNote := handedElsewhere(hqc, handed, target.Repo)
	if origin == hq.OriginOnGitHub {
		result.Message = fmt.Sprintf("Change #%d is open in %q for the branch %q, whose code is kept on GitHub: HQ opens its pull request on GitHub from that branch, so open none of your own, and the person merges it in Mate. Push the work there (`git push origin %s`, origin being GitHub) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
			number, target.Repo, result.Branch, result.Branch) + handedNote
		return jsonResult(result), nil, nil
	}
	push, bound := result.Branch, ""
	if result.Branch != branch {
		push = branch + ":" + result.Branch
		bound = fmt.Sprintf(", not %q — this HQ binds a change to a branch of its own, as HQ did before changes were bound to the branch they name —", branch)
	}
	result.Message = fmt.Sprintf("Change #%d is open in %q for the branch %q%s: push the work there (`git push origin %s`) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
		number, target.Repo, result.Branch, bound, push) + handedNote
	return jsonResult(result), nil, nil
}

// titleIfCreated is the title a change opened with, "" for one HQ answered
// open: an open change keeps its own title.
func titleIfCreated(opened hq.OpenedChange) string {
	if !opened.Created {
		return ""
	}
	return opened.Change.Title
}

// answeredOpenChange is open-change's words for a change HQ answered open
// rather than opened: the one on the branch asked for, which stays and takes
// the work by a push — or, from an HQ that keeps one open change per
// repository, its open one on another branch (held, "" when that HQ names
// none), which this work goes onto.
func answeredOpenChange(hqc hq.Client, repo, branch, held string, number int) string {
	switch held {
	case branch:
		return fmt.Sprintf("Change #%d on %q is already open: it stays; push to it (`git push origin %s`), never force-pushing, and describe it again with zerops_workflow action=\"describe-change\" branch=%q once the push is ready — a push that moves it makes it a draft until then.",
			number, branch, branch, branch)
	case "":
		return fmt.Sprintf("Change #%d is already open in %q, and a Mate has one open change per repository: it stays. Push this work to its branch, or open its change once #%d is merged or closed.",
			number, repo, number)
	}
	// Work cut from main is no fast-forward of the open change's branch: it
	// goes onto that branch, or HQ refuses its push.
	return fmt.Sprintf("Change #%d is already open in %q on the branch %q, and a Mate has one open change per repository: it stays, and this work goes on its branch — %s, bring this work onto it (`git merge %s`) and push it (`git push origin %s`), never force-pushing; or open this work's change once #%d is merged or closed.",
		number, repo, held, takeChangeBranch(hqc, held), branch, held, number)
}

// handedElsewhere names the changes handed to the Mate in repo, each on a
// branch of its own, which stay open beside the one just opened; "" for none.
func handedElsewhere(hqc hq.Client, handed []hq.MateChange, repo string) string {
	if len(handed) == 0 {
		return ""
	}
	named := make([]string, 0, len(handed))
	for _, h := range handed {
		named = append(named, fmt.Sprintf("#%d on %q", h.Number, hqc.BranchOf(h.Number, h.Branch)))
	}
	return fmt.Sprintf(" Handed to this Mate from a Mate that is gone, %s in %q stays open too, its work on its own branch.", strings.Join(named, " and "), repo)
}

// tooManyOpen refuses an open past as many open changes as HQ keeps the Mate
// in the repository, naming them from the Mate's state when it was read.
func tooManyOpen(target changeTarget, branch string, state hq.MateState, stateErr error) *platform.PlatformError {
	var named []string
	if stateErr == nil {
		open := target
		open.Branch = ""
		for _, c := range open.openCandidates(&state, nil) {
			name := fmt.Sprintf("#%d on %q", c.Number, c.Branch)
			if c.Title != "" {
				name += fmt.Sprintf(" (%q)", c.Title)
			}
			named = append(named, name)
		}
	}
	held := "as many open changes as HQ keeps a Mate there"
	if len(named) > 0 {
		held += ": " + strings.Join(named, ", ")
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Nothing was opened for %q: this Mate already holds, in %q, %s.", branch, target.Repo, held),
		"Ask the person to merge or close one of them in its review; then open this one again — the branch you pushed stays as it is.")
}

// changeBranchOf is the branch a change is opened from, as its short name:
// refs/heads/ dropped. It refuses no branch, one git cannot name, and one HQ
// keeps for itself — `main` and the tags.
func changeBranchOf(input string) (string, *platform.PlatformError) {
	name := strings.TrimSpace(input)
	if name == "" {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			`open-change opens the change of a branch of your own: pass branch="<branch>".`,
			"Cut a branch from main, commit, push it, and name it here.")
	}
	if strings.HasPrefix(name, "refs/") && !strings.HasPrefix(name, "refs/heads/") {
		return "", branchNotOwn(name)
	}
	name = strings.TrimPrefix(name, "refs/heads/")
	if !gitBranchName(name) {
		return "", notABranchName(name)
	}
	if name == hqBase {
		return "", branchNotOwn(name)
	}
	return name, nil
}

// branchNotOwn refuses a ref HQ keeps for itself.
func branchNotOwn(ref string) *platform.PlatformError {
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("Nothing was opened: %q is HQ's — `main` moves only by a merge, and tags and the mate/ branches are HQ's.", ref),
		"Cut a branch of your own from main, push it, and open its change from that.")
}

// notABranchName refuses a name git, or HQ's git, does not take as a branch.
func notABranchName(name string) *platform.PlatformError {
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("Nothing was opened: %q is not a branch name git takes.", name),
		"Name the branch with letters, digits, - _ . and /, as you pushed it.")
}

// gitBranchName reports whether git takes name as a branch name
// (git-check-ref-format): no control character, space or ~^:?*[\, no "..",
// "@{" or "//", and no part starting or ending in a dot or ending in .lock.
func gitBranchName(name string) bool {
	if name == "" || name == "@" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.ContainsAny(name, " ~^:?*[\\\x7f") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
