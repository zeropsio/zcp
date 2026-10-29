package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// changeOpenResult is open-change's answer.
type changeOpenResult struct {
	Service string `json:"service"`
	// Parts are the parts whose dev pools lease the change's branch when
	// they are several, and so name no one service (changeService).
	Parts          []string `json:"parts,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	PullRequest    int      `json:"pullRequest"`
	PullRequestURL string   `json:"pullRequestUrl"`
	Created        bool     `json:"created"`
	Message        string   `json:"message"`
	// Landed says what became of the ledger's other changes in the
	// repository that the Mate's state names settled since the last pass.
	Landed string `json:"landed,omitempty"`
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
	rt runtime.Info,
	stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"open-change opens a change in this Mate's HQ, and this Mate is not linked to one yet.",
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
	route := changeRoute(stateDir, target)
	var result changeOpenResult
	result.Service, result.Parts = changeService(stateDir, hqc.Address(), target, input.Service, branch)

	// The Mate's own state says how the repository's code is kept, whether
	// HQ keeps an open change per branch, and names the changes handed to
	// the Mate there: one on this branch is its change, and HQ is not asked
	// to open another. A state that cannot be read leaves all of it to HQ's
	// answer. It also says what became of the changes the ledger keeps in
	// the repository as it is read (keptBefore — one recorded after the read
	// is not judged by it): an answer forgets each settled one and says it
	// once (learnSettled), so a describe naming no branch never meets a
	// change merged since the last pass.
	keptBefore := changesIn(hqChanges(stateDir), target.AppID, target.Repo)
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	state, stateErr := hqc.Once().Self(callCtx)
	cancel()
	learnSettled := func() {
		if stateErr == nil {
			result.Landed = learnRepoLandings(ctx, stateDir, hqc.Address(), target.AppID, keptBefore, state)
		}
	}
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
			result.Message = sentenceOf(handedWork(route, h, target.Repo, held, !state.OpenPerBranch))
			if held == branch {
				result.Message += fmt.Sprintf(" It is the change of %q, so nothing else was opened.", branch)
			} else {
				result.Message += " Nothing else was opened: a Mate has one open change per repository."
			}
			result.Message += ` Write what it does and how you checked it with zerops_workflow action="describe-change"; the person reviews it by that.`
			learnSettled()
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
		found, refusal := openUnanswered(ctx, hqc, target, branch, err, state, stateErr)
		if refusal != nil {
			return convertError(refusal, WithRecoveryStatus()), nil, nil
		}
		opened = found
	}
	learnSettled()
	number := opened.Change.Number
	// An open whose answer was lost and the Mate's state answered says so
	// first (openUnanswered).
	lostNote := ""
	if err != nil {
		lostNote = fmt.Sprintf("HQ's answer to the open never arrived (%s), and the Mate's state shows change #%d open in %q: it is the change this open answers, and nothing else was opened. ",
			hqNotServingWords(err), number, target.Repo)
	}
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
		result.Message = lostNote + answeredOpenChange(route, hqc, target.Repo, branch, held, number)
		return jsonResult(result), nil, nil
	}
	handedNote := handedElsewhere(hqc, handed, target.Repo)
	if origin == hq.OriginOnGitHub {
		result.Message = lostNote + fmt.Sprintf("Change #%d is open in %q for the branch %q, whose code is kept on GitHub: HQ opens its pull request on GitHub from that branch, so open none of your own, and the person merges it in Mate. Push the work there (%s, origin being GitHub) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
			number, target.Repo, result.Branch, route.git("push origin "+result.Branch)) + handedNote
		return jsonResult(result), nil, nil
	}
	push, bound := result.Branch, ""
	if result.Branch != branch {
		push = branch + ":" + result.Branch
		bound = fmt.Sprintf(", not %q — this HQ binds a change to a branch of its own, as HQ did before changes were bound to the branch they name —", branch)
	}
	result.Message = lostNote + fmt.Sprintf("Change #%d is open in %q for the branch %q%s: push the work there (%s) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
		number, target.Repo, result.Branch, bound, route.git("push origin "+push)) + handedNote
	return jsonResult(result), nil, nil
}

// openUnanswered decides an open HQ did not answer. HQ's own refusal, and a
// try that surely never reached it (hqNotReached), opened nothing. Any other
// failure — the answer timed out or the connection dropped once HQ had the
// request, a balancer's 502 or 504, an HQ 500, an answer that did not parse
// — cannot tell an open HQ made from one it did not, and opening is no
// idempotent write: Filip's open on 2026-10-26 met "context deadline
// exceeded" and said "Nothing was opened" while HQ had opened change #2. So
// it reads the Mate's state before it answers (check-before-mutate,
// spec-workflows.md §8 O3) and answers the change open for branch in the
// repository as HQ answers an open (openedIn), created when the state read
// before the open (before, unread when beforeErr is set) did not hold it.
// Without it, whether a change was opened is unknown and the refusal says
// so — never that nothing was opened — and that a call again is safe: HQ
// answers the open change on a branch rather than opening a second one.
func openUnanswered(ctx context.Context, hqc hq.Client, target changeTarget, branch string, openErr error, before hq.MateState, beforeErr error) (hq.OpenedChange, *platform.PlatformError) {
	var refused *hq.RefusedError
	if (errors.As(openErr, &refused) && refused.Status < http.StatusInternalServerError) || hqNotReached(openErr) {
		return hq.OpenedChange{}, platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Nothing was opened: HQ did not open a change in %q (%v).", target.Repo, openErr),
			`Call zerops_workflow action="open-change" again; the branch you pushed stays as it is.`)
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	after, err := hqc.Once().Self(callCtx)
	cancel()
	again := `Call zerops_workflow action="open-change" again: HQ answers the change open on the branch if this open went through and opens it if not — never a second one. The branch you pushed stays as it is.`
	if err != nil {
		return hq.OpenedChange{}, platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Whether a change was opened in %q is unknown: HQ's answer to the open never arrived (%s), nor the Mate's state after it (%s).",
				target.Repo, hqNotServingWords(openErr), hqNotServingWords(err)), again)
	}
	change, ok := openedIn(hqc, after, target, branch)
	if !ok {
		return hq.OpenedChange{}, platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Whether a change was opened in %q is unknown: HQ's answer to the open never arrived (%s), and the Mate's state shows no change open for %q yet — the open may still land.",
				target.Repo, hqNotServingWords(openErr), branch), again)
	}
	created := false
	if beforeErr == nil {
		prior, held := openedIn(hqc, before, target, branch)
		created = !held || prior.Number != change.Number
	}
	return hq.OpenedChange{Change: change, Created: created}, nil
}

// openedIn is the change state holds that HQ answers an open for branch in
// target's repository with: on an HQ that keeps an open change per branch,
// the Mate's open change on that branch; on one that keeps one per
// repository, its open one there whatever the branch (a handed one is
// answered before HQ is asked to open anything).
func openedIn(hqc hq.Client, state hq.MateState, target changeTarget, branch string) (hq.Change, bool) {
	if state.AppID == nil || *state.AppID != target.AppID {
		return hq.Change{}, false
	}
	for _, c := range state.Changes {
		if c.Repo != target.Repo || c.State != hq.ChangeOpen || c.Handed {
			continue
		}
		held := hqc.BranchOf(c.Number, strings.TrimPrefix(c.Branch, "refs/heads/"))
		if state.OpenPerBranch && held != branch {
			continue
		}
		change := hq.Change{AppID: target.AppID, Repo: c.Repo, Number: c.Number, State: c.State, Head: c.Head, Branch: c.Branch}
		if c.Title != nil {
			change.Title = *c.Title
		}
		return change, true
	}
	return hq.Change{}, false
}

// learnRepoLandings learns, by state, what became of kept — the ledger's
// changes of appID's repository — each held with no wait, forgetting each no
// longer open and saying it once (learnChangeLanding); "" when every one is
// open, or state is of another application. A change another step holds (a
// describe) is left to it.
func learnRepoLandings(ctx context.Context, stateDir, address, appID string, kept []workflow.HQChange, state hq.MateState) string {
	if state.AppID == nil || *state.AppID != appID {
		return ""
	}
	var learned []string
	for _, c := range kept {
		release, err := workflow.HoldHQChange(ctx, stateDir, c.AppID, c.Repo, c.Number, 0)
		if err != nil {
			continue
		}
		line := learnChangeLanding(stateDir, address, c, state)
		release()
		if line != "" {
			learned = append(learned, sentenceOf(line))
		}
	}
	return strings.Join(learned, " ")
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
// none), which this work goes onto; git run where route says.
func answeredOpenChange(route repoRoute, hqc hq.Client, repo, branch, held string, number int) string {
	switch held {
	case branch:
		return fmt.Sprintf("Change #%d on %q is already open: it stays; push to it (%s), never force-pushing, and describe it again with zerops_workflow action=\"describe-change\" branch=%q once the push is ready — a push that moves it makes it a draft until then.",
			number, branch, route.git("push origin "+branch), branch)
	case "":
		return fmt.Sprintf("Change #%d is already open in %q, and a Mate has one open change per repository: it stays. Push this work to its branch, or open its change once #%d is merged or closed.",
			number, repo, number)
	}
	// Work cut from main is no fast-forward of the open change's branch: it
	// goes onto that branch, or HQ refuses its push.
	return fmt.Sprintf("Change #%d is already open in %q on the branch %q, and a Mate has one open change per repository: it stays, and this work goes on its branch — %s, bring this work onto it (%s) and push it (%s), never force-pushing; or open this work's change once #%d is merged or closed.",
		number, repo, held, takeChangeBranch(route, hqc, held), route.git("merge "+branch), route.git("push origin "+held), number)
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
		`Ask the person to merge or close one of them in its review, or close one this Mate abandoned (zerops_workflow action="close-change" branch="<its branch>"); then open this one again — the branch you pushed stays as it is.`)
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
