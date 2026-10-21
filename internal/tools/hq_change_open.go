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
)

// changeOpenResult is open-change's answer.
type changeOpenResult struct {
	Service        string `json:"service"`
	Branch         string `json:"branch"`
	PullRequest    int    `json:"pullRequest"`
	PullRequestURL string `json:"pullRequestUrl"`
	Created        bool   `json:"created"`
	Message        string `json:"message"`
}

// handleOpenChange is zerops_workflow action="open-change": the agent opens
// the change that takes a branch of its own to `main`, in the pair's
// repository in HQ. HQ binds the change to the branch, which may be pushed
// before or after; the pair records the change, which describe-change then
// describes. A Mate has one open change per repository: HQ answers the open
// one rather than opening a second.
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
	branch, refusal := changeBranchOf(input.Branch)
	if refusal != nil {
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
	meta, held := pairOfChange(stateDir, input.Service, "open")
	if held != nil {
		return held, nil, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	opened, err := hqc.OpenChange(callCtx, meta.HQ.Repo, title, branch)
	cancel()
	var refused *hq.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Code == "branch_taken":
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Nothing was opened: another open change holds the branch %q in %q.", branch, meta.HQ.Repo),
			"Push this work to a branch of your own under another name and open its change from that."), WithRecoveryStatus()), nil, nil
	case errors.As(err, &refused) && refused.Code == "protected_ref":
		return convertError(branchNotOwn(branch), WithRecoveryStatus()), nil, nil
	case err != nil:
		return convertError(platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Nothing was opened: HQ did not open a change in %q (%v).", meta.HQ.Repo, err),
			`Call zerops_workflow action="open-change" again; the branch you pushed stays as it is.`), WithRecoveryStatus()), nil, nil
	}
	number := opened.Change.Number
	recordChange(stateDir, meta, number)
	result := changeOpenResult{
		Service: meta.Hostname, Branch: branch, PullRequest: number,
		PullRequestURL: hqc.ChangeURL(meta.HQ.AppID, meta.HQ.Repo, number), Created: opened.Created,
	}
	if held := strings.TrimPrefix(opened.Change.Branch, "refs/heads/"); held != "" {
		result.Branch = held
	}
	if !opened.Created {
		result.Message = fmt.Sprintf("Change #%d is already open in %q on the branch %q, and a Mate has one open change per repository: it stays. Push this work to %q, or open its change once #%d is merged or closed.",
			number, meta.HQ.Repo, result.Branch, result.Branch, number)
		return jsonResult(result), nil, nil
	}
	result.Message = fmt.Sprintf("Change #%d is open in %q for the branch %q: push the work there (`git push origin %s`) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
		number, meta.HQ.Repo, result.Branch, result.Branch)
	return jsonResult(result), nil, nil
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
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is not a branch name git takes.", name),
			"Name the branch with letters, digits, - _ . and /, as you pushed it.")
	}
	if name == hqBase {
		return "", branchNotOwn(name)
	}
	return name, nil
}

// branchNotOwn refuses a ref HQ keeps for itself.
func branchNotOwn(ref string) *platform.PlatformError {
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("Nothing was opened: %q is HQ's — `main` moves only by a merge, and tags are HQ's.", ref),
		"Cut a branch of your own from main, push it, and open its change from that.")
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
