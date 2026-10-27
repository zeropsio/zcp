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

// changeCloseResult is close-change's answer.
type changeCloseResult struct {
	Service        string `json:"service,omitempty"`
	Branch         string `json:"branch,omitempty"`
	PullRequest    int    `json:"pullRequest,omitempty"`
	PullRequestURL string `json:"pullRequestUrl,omitempty"`
	Closed         bool   `json:"closed"`
	Message        string `json:"message"`
}

// handleCloseChange is zerops_workflow action="close-change": the Mate closes
// its own open change without merging it — abandoned work, a subagent's
// change nobody will review — named by its branch, or by a lease of it; never
// inferred, not even as the only open one. Its branch stays, and so do the
// leases on it. Only an HQ that keeps an open change per branch lets a Mate
// close its own; on any other — one that keeps one per repository, or one
// with no close for a Mate — a person closes it in its review, and the answer
// says so with its link. Nothing closes on idleness: this is the agent's act,
// asked for.
func handleCloseChange(
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
			"close-change closes a change in this Mate's HQ, and this Mate is not linked to one yet.",
			"Without HQ there is no change to close.",
		), WithRecoveryStatus()), nil, nil
	}
	target, err := resolveChangeTarget(stateDir, hqc.Address(), changeAsk{Service: input.Service, Branch: input.Branch, Verb: changeVerbClose})
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	if target.Branch == "" {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			`close-change closes the change of one branch: pass branch="<branch>", or service="<lease alias>" for the branch a lease holds.`,
			`zerops_workflow action="status" lists each repository's open changes by number and branch.`), WithRecoveryStatus()), nil, nil
	}
	result := changeCloseResult{Service: strings.TrimSpace(input.Service), Branch: target.Branch}

	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	state, err := hqc.Once().Self(callCtx)
	cancel()
	if err != nil {
		return convertError(platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Nothing was closed: whether a change of this Mate is open on %q in %q is unknown — HQ at %s did not tell (%s).", target.Branch, target.Repo, hqc.Address(), hqNotServingWords(err)),
			`Call zerops_workflow action="close-change" again once HQ answers.`), WithRecoveryStatus()), nil, nil
	}
	kept := changesIn(hqChanges(stateDir), target.AppID, target.Repo)
	if learned := learnDescribedLandings(stateDir, hqc.Address(), target, kept, state); learned != "" {
		result.Message = learned + " Nothing else was closed."
		return jsonResult(result), nil, nil
	}
	pick, err := target.pick(&state, kept, changeVerbClose)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	if pick.Number == 0 {
		result.Message = fmt.Sprintf("No change of this Mate is open on the branch %q in %q, so nothing was closed. A Mate closes only its own change; another's is its Mate's, or a person's to close.", target.Branch, target.Repo)
		return jsonResult(result), nil, nil
	}
	result.PullRequest = pick.Number
	result.PullRequestURL = hqc.ChangeURL(target.AppID, target.Repo, pick.Number)
	if !state.OpenPerBranch {
		result.Message = personClosesWords(pick.Number, target.Branch, result.PullRequestURL, "this HQ keeps one open change per repository and closes none for a Mate")
		return jsonResult(result), nil, nil
	}

	release, refusal := holdChange(ctx, stateDir, target, pick.Number, fmt.Sprintf("Nothing was closed: change #%d in %q", pick.Number, target.Repo), "close-change")
	if refusal != nil {
		return refusal, nil, nil
	}
	defer release()
	callCtx, cancel = context.WithTimeout(ctx, hqCallTimeout)
	_, err = hqc.CloseChange(callCtx, target.Repo, pick.Number)
	cancel()
	var refused *hq.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Reason == "change_not_open":
		_ = workflow.ForgetHQChange(stateDir, target.AppID, target.Repo, pick.Number)
		result.Message = fmt.Sprintf("Change #%d on %q is no longer open — it merged or closed meanwhile — so nothing was closed; the next pass says what became of it.", pick.Number, target.Branch)
		return jsonResult(result), nil, nil
	case errors.As(err, &refused) && (refused.Status == http.StatusNotFound && refused.Reason != "change_not_found" || refused.Status == http.StatusMethodNotAllowed):
		result.Message = personClosesWords(pick.Number, target.Branch, result.PullRequestURL, "this HQ has no close for a Mate yet")
		return jsonResult(result), nil, nil
	case errors.As(err, &refused) && refused.Status == http.StatusForbidden:
		result.Message = personClosesWords(pick.Number, target.Branch, result.PullRequestURL, fmt.Sprintf("HQ refused this Mate the close (%s)", hqRefusalWords(refused)))
		return jsonResult(result), nil, nil
	case err != nil:
		return convertError(platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Nothing was closed: HQ did not close change #%d in %q (%v).", pick.Number, target.Repo, err),
			`Call zerops_workflow action="close-change" again; the change and its branch stay as they are.`), WithRecoveryStatus()), nil, nil
	}
	_ = workflow.ForgetHQChange(stateDir, target.AppID, target.Repo, pick.Number)
	result.Closed = true
	result.Message = fmt.Sprintf("Change #%d on %q is closed without merging, by this Mate — the person sees it closed in the change's conversation. Its branch stays in HQ, so the work is not lost: opening the branch's change again opens a new number.", pick.Number, target.Branch)
	if held := leasesOnBranch(stateDir, hqc.Address(), target.AppID, target.Repo, target.Branch); len(held) > 0 {
		result.Message += fmt.Sprintf(" The leases %s still hold the branch: release them when you are done with it (`%s`).",
			strings.Join(held, ", "), ops.LeaseReleaseBranchCall(target.Branch, "", false))
	}
	return jsonResult(result), nil, nil
}

// personClosesWords is close-change's answer where HQ closes no change for a
// Mate: a person closes it in its review, by its link; nothing changed here.
func personClosesWords(number int, branch, url, why string) string {
	return fmt.Sprintf("Nothing was closed: %s, so a person closes change #%d on %q in its review (%s). Ask them, saying why the work is abandoned; its branch stays either way.",
		why, number, branch, url)
}
