package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleHQGitPush is a git-push of a wired pair to this Mate's HQ. HQ takes
// the Mate's push only on the branch of its own open change, so the push is
// a delivery of the committed work — no commit of its own: `main` taken in
// (a landing of this Mate's own change absorbed first, so the change it
// pushes to never shows a false conflict), the change opened when the
// checkout is ahead of `main`, and HEAD pushed to its branch. A REAL conflict
// stops it: pushing on top of a checkout the sync left mid-way is never
// right. Nothing builds from the change. A push that does not reach HQ is
// the tool's error, and a line on stderr.
func handleHQGitPush(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	projectID, stateDir, hostname, workingDir, remote, dirtyWarn string,
	recordAttempt func(string, topology.FailureClass),
) *mcp.CallToolResult {
	meta, _ := workflow.FindServiceMeta(stateDir, hostname)
	hqc, enrolled := openHQ(httpClient)
	if !enrolled || !hqPairWired(meta) {
		recordAttempt("the pair has no repository in HQ", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("git-push from %s did not run: it has no repository in this Mate's HQ yet.", hostname),
			"Deploy the pair directly (strategy \"ssh\") to run the code; the push goes to its change once HQ gives the pair its repository.",
		), WithRecoveryStatus())
	}
	// The push paces and bounds its own tries (deliveryRetry), so no call
	// waits HQ out beneath them.
	hqc = deliveryClient(hqc)
	failed := func(pe *platform.PlatformError) *mcp.CallToolResult {
		logDeliveryFailure(hostname, pe.Message)
		return convertError(pe, WithRecoveryStatus())
	}

	// What became of the change on record is news independent of this
	// push's own outcome, and folded into every answer from here.
	var (
		learned string
		landed  []string
	)
	if state, err := hqc.Self(ctx); err == nil {
		landed = landedHeads(state, meta.HQ.Repo)
		learned = hqLearnLanding(stateDir, meta, state)
		deliveryLanding(stateDir, meta, state)
	}
	withLearned := func(msg string) string {
		if learned == "" {
			return msg
		}
		return msg + " " + sentenceOf(learned)
	}

	landedCommit, landedHead := landingOf(meta)
	output, tries, err := gitAgainstHQ(ctx, sshDeployer, hqc, hostname, ops.BuildDeliverySyncCommand(workingDir, landedCommit, landedHead))
	if refusal := hqPushSyncRefusal(string(output), hostname, landedCommit); refusal != nil {
		recordAttempt("hq landing absorb: "+refusal.Message, topology.FailureClassConfig)
		refusal.Message = withLearned(refusal.Message)
		return failed(refusal)
	}
	if base := ops.DeliveryFreshBase(string(output)); base != "" {
		learned = withLearned("The next change starts from main at " + base + "; prior history is kept under refs/zcp/landed/" + landedCommit)
	}
	_, found := ops.DeliveryAhead(string(output))
	if err != nil || !found {
		detail := gitPushErrorDetail(err, output)
		if gitHQUnavailable(err, output) {
			recordAttempt("HQ unavailable: "+detail, topology.FailureClassNetwork)
			return failed(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				withLearned(fmt.Sprintf("git-push from %s has not reached HQ: %s.", hostname,
					hqNotAnsweringLine(hqc.Address(), fmt.Sprintf("taking %q in", hqBase), tries, gitNotServingWords(err, output)))),
				hqNotAnswering(hostname, "pushing again"),
			))
		}
		recordAttempt("taking main in failed: "+detail, topology.FailureClassNetwork)
		return failed(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			withLearned(fmt.Sprintf("git-push from %s has not reached HQ: taking %q in failed (%s).", hostname, hqBase, detail)),
			"Fix the cause named above, then push again.",
		))
	}

	shipped := shipChange(ctx, sshDeployer, stateDir, hqc, meta, landed)
	if shipped.ref == nil && !shipped.upToDate {
		recordAttempt("change not shipped: "+shipped.line, topology.FailureClassNetwork)
		next := "Fix the cause named above, then push again."
		if shipped.unreachable {
			next = hqNotAnswering(hostname, "pushing again")
		}
		return failed(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			withLearned(fmt.Sprintf("git-push from %s has not reached HQ: %s.", hostname, shipped.line)),
			next,
		))
	}

	result := &ops.GitPushResult{Status: "PUSHED", RemoteURL: remote}
	switch {
	case shipped.upToDate:
		result.Status = statusNothingToPush
		result.Message = fmt.Sprintf("Nothing to push from %s — %s", hostname, shipped.line)
	case shipped.unchanged:
		result.Status, result.Branch = statusNothingToPush, shipped.ref.Branch
		result.Message = fmt.Sprintf("Nothing to push from %s — change #%d in HQ already carries this HEAD", hostname, shipped.ref.Number)
	default:
		result.Branch = shipped.ref.Branch
		result.Message = fmt.Sprintf("Code pushed from %s to change #%d in HQ (branch: %s)", hostname, shipped.ref.Number, shipped.ref.Branch)
	}
	result.NextActions = hqPushNextActions(shipped.ref, hostname)

	var warnings []string
	if dirtyWarn != "" {
		warnings = append(warnings, dirtyWarn)
	}
	if learned != "" {
		warnings = append(warnings, learned)
	}
	return jsonResult(deployGitPushResponse{
		GitPushResult:    result,
		PullRequest:      shipped.ref,
		Warnings:         warnings,
		WorkSessionState: sessionAnnotations(stateDir),
		Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
	})
}

// hqPushSyncRefusal is the refusal of a push whose sync stopped by name — a
// checkout not clean enough to take this Mate's own landed change in, or a
// real conflict — nil for anything else.
func hqPushSyncRefusal(output, hostname, landedCommit string) *platform.PlatformError {
	switch {
	case ops.AbsorbDirty(output):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s has not reached HQ: uncommitted changes in %s's checkout block taking in this Mate's own landed change.", hostname, hostname),
			fmt.Sprintf("Commit the changes in %s's checkout, then push again.", hostname),
		)
	case ops.AbsorbConflict(output) != "":
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s has not reached HQ: a real conflict inside absorbing this Mate's own earlier change — %s changes the same lines (%s).", hostname, hqBase, ops.AbsorbConflict(output)),
			manualAbsorbSequence(hostname, landedCommit, true)+", then push again.",
		)
	case ops.DeliveryConflict(output) != "" && ops.AbsorbUnprovable(output) && landedCommit != "":
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s has not reached HQ: %s has moved on and this Mate's work changes the same lines (%s) — this may be this Mate's own squashed change, which this container's git could not prove safe to fold in automatically.", hostname, hqBase, ops.DeliveryConflict(output)),
			manualAbsorbSequence(hostname, landedCommit, false)+", then push again.",
		)
	case ops.DeliveryConflict(output) != "":
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s has not reached HQ: %s has moved on and this Mate's work changes the same lines (%s).", hostname, hqBase, ops.DeliveryConflict(output)),
			fmt.Sprintf("In %s's checkout run `git fetch origin && git merge origin/%s`, resolve it, then push again.", hostname, hqBase),
		)
	}
	return nil
}
