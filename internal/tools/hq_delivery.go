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
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate enrolled with its HQ delivers through changes, and only a dev/stage
// pair can: the dev half is the checkout that pushes, the stage half the
// verified basis a production is promoted from. The rules below were each
// measured missing on 2026-09-17, when "create a todo app" on a fresh Mate
// got bootstrapMode simple — one service, nothing pushed, no change, and the
// person expanding it by hand:
//
//   - the classic route plans every runtime of such a Mate as a standard pair;
//   - a deploy onto a wired pair's stage half delivers it: commit, change,
//     push, with nothing asked of the agent or the person;
//   - a push to HQ is watched for no build and offers no integration, and a
//     wired pair's direct deploys are never redirected.

// hqPairPlanError refuses a classic-route plan that gives a Mate delivering
// through HQ a runtime with no stage half. Nil when the Mate does not deliver
// through HQ, or every runtime is a standard pair.
func hqPairPlanError(plan []workflow.BootstrapTarget, wired bool) *platform.PlatformError {
	if !wired {
		return nil
	}
	for _, target := range plan {
		rt := target.Runtime
		if rt.EffectiveMode() == topology.PlanModeStandard && rt.StageHostname() != "" {
			continue
		}
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Plan validation failed: target %q: this Mate delivers through its HQ, where code lands through changes, so every runtime is a dev/stage pair — bootstrapMode %q with no stage half cannot be delivered to its repository or promoted to stage and production", rt.DevHostname, rt.EffectiveMode()),
			fmt.Sprintf("Re-submit the plan with runtime.bootstrapMode=\"standard\" and an explicit stageHostname for %q (for example devHostname %q, stageHostname %q).", rt.DevHostname, rt.DevHostname, stageNameFor(rt.DevHostname)),
		)
	}
	return nil
}

// hqLaunchProductionRefusal refuses the whole launch-production workflow in a
// Mate that delivers through HQ: launch-production creates and imports its
// OWN production project on a user-owned remote (workflow_launch_production.go)
// — it has no case for an application's production, which the person adds
// from the projects page. wired is the caller's own hqWired() read — no
// second detector. nil when the Mate does not deliver through HQ, so the
// classic route is untouched.
func hqLaunchProductionRefusal(ctx context.Context, httpClient ops.HTTPDoer, stateDir string, wired bool) *mcp.CallToolResult {
	if !wired {
		return nil
	}
	return launchFailedResponse(nil, topology.BlockerCategoryOther, "wired_mate_production_is_the_groups",
		"This Mate delivers through its HQ, where an application's production is a project the person adds "+
			"from the projects page — launch-production creates its own production project outside the "+
			"application and has no case for that. "+hqLaunchProductionNextStep(ctx, httpClient, stateDir))
}

// hqLaunchProductionNextStep says what is true of THIS Mate's own pairs rather
// than a lecture. A pair's recorded change number is only cleared when a pass
// reads HQ, and that pass is backoff-gated — so a change the person already
// merged can still be sitting here as "open" the moment this runs. The
// Mate's own state is read fresh first, so "merge #N first" is never said
// about work that already landed. Three states, never collapsed into
// "nothing open means merged":
//   - a change still open after the fresh read → name it, tell the person to
//     merge it;
//   - a merge in HQ, even after its local landing was absorbed → the code
//     is on main; point to the projects page;
//   - no change at all, or one closed without merging → nothing of this
//     pair's work has reached main; deliver through the stage half first.
func hqLaunchProductionNextStep(ctx context.Context, httpClient ops.HTTPDoer, stateDir string) string {
	var open []string
	merged := false
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		self := func() (hq.MateState, error) { return hq.MateState{}, hq.ErrNotEnrolled }
		if hqc, enrolled := openHQ(httpClient); enrolled {
			self = lazySelf(ctx, hqc)
		}
		for _, m := range metas {
			if !hqPairWired(m) {
				continue
			}
			state, err := self()
			if err != nil {
				return fmt.Sprintf("This Mate could not read its changes from HQ (%v), so whether its work reached main is unknown. Ask again once HQ can be reached; production is added and released from Mate's projects page.", err)
			}
			if state.AppID == nil || *state.AppID != m.HQ.AppID {
				continue
			}
			hqLearnLanding(stateDir, m, state)
			// HQ keeps the merged history after a delivery clears Landed.
			// Read it there, rather than keeping a second last-merge record.
			for _, change := range state.Changes {
				if change.Repo == m.HQ.Repo && change.State == hq.ChangeMerged {
					merged = true
				}
			}
			if number := openChangeIn(state, m.HQ.Repo); number != 0 {
				open = append(open, fmt.Sprintf("%s's change #%d on %q", m.Hostname, number, m.HQ.Repo))
			}
		}
	}
	if len(open) > 0 {
		return "Tell the person to merge " + strings.Join(open, " and ") +
			" first — once it lands, production is added and released from the project on Mate's projects page."
	}
	if merged {
		return "Tell the person: production is added and released from the project on Mate's projects page."
	}
	return "Tell the person: none of this Mate's work has reached its repository's main yet — deliver it by deploying the pair's stage half first, then production is added and released from the project on Mate's projects page."
}

// stageNameFor suggests the stage half's hostname: appdev → appstage,
// todoapp → todoappstage.
func stageNameFor(devHostname string) string {
	if strings.HasSuffix(devHostname, "dev") && len(devHostname) > len("dev") {
		return strings.TrimSuffix(devHostname, "dev") + "stage"
	}
	return devHostname + "stage"
}

// hqDelivery is what a wired pair's delivery did, in the words the agent
// passes on.
type hqDelivery struct {
	Change *changeRef
	Line   string
	// shipped: the delivery went as far as shipping its change, which the
	// group's recipe follows.
	shipped bool
	// failed: the work did not reach HQ.
	failed bool
}

// notDelivered is a delivery whose work did not reach HQ, said in line.
func notDelivered(line string) *hqDelivery {
	return &hqDelivery{Line: line, failed: true}
}

// deliverHQPair is how a wired pair's work reaches its application without
// anyone saying how (the owner, 2026-09-17, of a prompt that had to name a
// git-push deploy: "no person is ever going to say this"). A deploy onto the
// pair's stage half is the moment the work is shippable, so zcp commits the
// dev half's tree as it was deployed, takes `main` in, opens or finds the
// change when there is something to deliver, pushes its branch, and proposes
// whatever tiers of the group's recipe its repo still lacks. The dev half's
// deploys are the loop and deliver nothing.
//
// Nil when there is nothing to deliver: a Mate not enrolled with an HQ, no
// wired pair behind the target, not its stage half, not in a container.
// Otherwise a line for the deploy's next actions, a failed delivery included
// — the deploy itself succeeded either way, and a failed delivery is also a
// line on stderr.
func deliverHQPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir, target string,
) *hqDelivery {
	delivery := deliverHeldHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, target)
	if delivery != nil && delivery.failed {
		logDeliveryFailure(target, delivery.Line)
	}
	// The recipe reads the project and HQ, never the checkout, so it runs
	// once the delivery has let go of it — and not after a failed one, which
	// would only ask HQ again; the next pass or delivery proposes it.
	if delivery != nil && delivery.shipped && !delivery.failed {
		if line := reconcileGroupRecipe(ctx, client, httpClient, rt, stateDir); line != "" {
			delivery.Line += " The group's recipe: " + line
		}
	}
	return delivery
}

// deliverHeldHQPair is the delivery itself, holding the pair's checkout.
func deliverHeldHQPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir, target string,
) (delivery *hqDelivery) {
	if sshDeployer == nil || !rt.InContainer {
		return nil
	}
	delivers := func(m *workflow.ServiceMeta) bool {
		return hqPairOnItsBranch(m) && m.StageHostname != "" && target == m.StageHostname
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if !delivers(meta) {
		return nil
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return nil
	}
	// The delivery paces and bounds its own tries (deliveryRetry), so no
	// call waits HQ out beneath them.
	hqc = deliveryClient(hqc)
	release, err := holdPairCheckout(ctx, stateDir, meta.Hostname)
	if err != nil {
		return notDelivered(fmt.Sprintf(
			"%s runs, but its code has not reached HQ: %s. Nothing of it was committed or pushed; the next stage deploy delivers it.",
			target, pairHeldReason(meta.Hostname, err)))
	}
	defer release()
	// A stage deploy is a delivery whether or not a pass has run since the
	// last merge — the passes are backoff-gated — so the Mate's own state is
	// read fresh here. What it says about the OLD change precedes whatever
	// this delivery itself reports, success or failure: nothing else would
	// ever say it once the number is off the pair.
	var news []string
	defer func() {
		if delivery != nil && len(news) > 0 {
			if delivery.Change != nil {
				for i, note := range news {
					news[i] = changeOutcomeAfterOpen(note)
				}
			}
			delivery.Line = strings.TrimSpace(sentenceOf(strings.Join(news, "; ")) + " " + delivery.Line)
		}
	}()

	// Whoever held the checkout may have moved the record: a delivery it
	// finished, a change it learned of.
	meta, _ = workflow.FindServiceMeta(stateDir, target)
	if !delivers(meta) {
		return nil
	}
	if state, err := hqc.Self(ctx); err == nil {
		switch {
		case state.AppID != nil && *state.AppID != meta.HQ.AppID:
			attempt := rewireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, meta, meta.HQ.Repo)
			if !attempt.wired {
				return notDelivered(fmt.Sprintf(
					"%s runs, but its code has not reached HQ: HQ holds this Mate in another application now, and wiring %s's repository there did not complete — %s",
					target, meta.Hostname, attempt.line))
			}
			news = append(news, attempt.line)
			if meta, _ = workflow.FindServiceMeta(stateDir, target); !hqPairWired(meta) {
				return nil
			}
		default:
			if note := hqLearnLanding(stateDir, meta, state); note != "" {
				news = append(news, note)
			}
			deliveryLanding(stateDir, meta, state)
		}
	}

	repo := meta.HQ.Repo
	landedCommit, landedHead := landingOf(meta)
	if err := hqEnsurePushCredential(ctx, client, sshDeployer, rt.ProjectID, stateDir, hqc, meta); err != nil {
		var (
			notAnswering *hqNotAnsweringError
			refused      *hqCredentialRefusedError
		)
		switch {
		case errors.As(err, &notAnswering):
			return notDelivered(hqUnreachableDelivery(target, meta.Hostname, notAnswering.line))
		case errors.As(err, &refused):
			return notDelivered(fmt.Sprintf(
				"%s runs, but its code has not reached its repository %q in HQ: %v. %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue.",
				target, repo, err, meta.Hostname))
		}
		return notDelivered(fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: %v. Fix the cause, then deploy %s again — the change follows that deploy.",
			target, repo, err, target))
	}

	title := changeTitle(stateDir, meta)
	output, tries, err := gitAgainstHQ(ctx, sshDeployer, meta.Hostname,
		ops.BuildDeliveryCommand(hqPairWorkingDir, title, landedCommit, landedHead))
	if line := deliveryRefusalLine(string(output), target, repo, meta.Hostname, landedCommit); line != "" {
		return notDelivered(line)
	}
	if base := ops.DeliveryFreshBase(string(output)); base != "" {
		news = append(news, "The next change starts from main at "+base+"; prior history is kept under refs/zcp/landed/"+landedCommit)
	}
	_, found := ops.DeliveryAhead(string(output))
	if err != nil || !found {
		if ops.GitRemoteUnavailable(string(output)) {
			return notDelivered(hqUnreachableDelivery(target, meta.Hostname,
				hqNotAnsweringLine(hqc.Address(), fmt.Sprintf("taking %q in", hqBase), tries, gitNotServingWords(err, output))))
		}
		if cls := classifyTransportError(err, deployStrategyGitPush); cls != nil && cls.Category == topology.FailureClassCredential {
			hqMarkPushRefused(stateDir, meta)
			return notDelivered(fmt.Sprintf(
				"%s runs, but its code has not reached its repository %q in HQ: HQ refused %s's credential (%s). %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue.",
				target, repo, meta.Hostname, gitPushErrorDetail(err, output), meta.Hostname))
		}
		return notDelivered(fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: committing it and taking %q in failed (%s). Fix the cause, then deploy %s again — the change follows that deploy.",
			target, repo, hqBase, gitPushErrorDetail(err, output), target))
	}

	shipped := shipChange(ctx, sshDeployer, stateDir, hqc, meta, title)
	result := &hqDelivery{Change: shipped.ref}
	switch {
	case shipped.ref != nil:
		result.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of its repository %q in HQ, and change #%d (%s) carries it to %q. %s Tell the person that link — the code lands on %q when they merge it. %s",
			meta.Hostname, shipped.ref.Branch, repo, shipped.ref.Number, shipped.ref.URL, hqBase, describeLine(shipped.ref, meta.Hostname), hqBase, hqMergeAnyMoment(hqBase))
	case shipped.upToDate:
		result.Line = fmt.Sprintf(
			"%s runs; nothing to deliver: main already has this. No change was opened or updated in its repository %q in HQ.",
			target, repo)
	case shipped.unreachable:
		result.Line, result.failed = hqUnreachableDelivery(target, meta.Hostname, shipped.line), true
	default:
		result.Line, result.failed = fmt.Sprintf("%s runs, but its code has not reached its repository %q in HQ: %s. Fix the cause, then deploy %s again — the change follows that deploy.",
			target, repo, shipped.line, target), true
	}
	result.shipped = true
	return result
}

// hqUnreachableDelivery is a stage deploy's line for a delivery HQ could not
// serve after its tries, why naming the step (hqNotAnsweringLine).
func hqUnreachableDelivery(target, hostname, why string) string {
	return fmt.Sprintf("%s runs, but its code has not reached HQ: %s. %s",
		target, why, hqNotAnswering(hostname, "deploying "+target+" again"))
}

// deliveryRefusalLine is the line for a delivery its git refused by name — a
// checkout not clean enough to take this Mate's own landed change in, a real
// conflict, an unignored dependency directory — "" for anything else.
func deliveryRefusalLine(output, target, repo, hostname, landedCommit string) string {
	switch {
	case ops.AbsorbDirty(output):
		return fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: uncommitted changes in %s's checkout block taking in this Mate's own landed change. Commit them, then deploy %s again — the change follows that deploy.",
			target, repo, hostname, target)
	case ops.AbsorbConflict(output) != "":
		return fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: a real conflict inside absorbing this Mate's own earlier change — %s changes the same lines (%s). %s, then deploy %s again — the change follows that deploy.",
			target, repo, hqBase, ops.AbsorbConflict(output), manualAbsorbSequence(hostname, landedCommit, true), target)
	case ops.DeliveryConflict(output) != "" && ops.AbsorbUnprovable(output) && landedCommit != "":
		return fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: %s has moved on and this Mate's work changes the same lines (%s) — this may be this Mate's own squashed change, which this container's git could not prove safe to fold in automatically. %s, then deploy %s again — the change follows that deploy.",
			target, repo, hqBase, ops.DeliveryConflict(output), manualAbsorbSequence(hostname, landedCommit, false), target)
	case ops.DeliveryConflict(output) != "":
		return fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: %s has moved on and this Mate's work changes the same lines (%s). In %s's checkout run `git fetch origin && git merge origin/%s`, resolve it, then deploy %s again — the change follows that deploy.",
			target, repo, hqBase, ops.DeliveryConflict(output), hostname, hqBase, target)
	case ops.DeliveryUnignored(output) != "":
		return fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: %s in %s is not ignored and would be committed. Add a .gitignore that ignores it, then deploy %s again — the change follows that deploy.",
			target, repo, ops.DeliveryUnignored(output), hostname, target)
	}
	return ""
}

// hqPairOnItsBranch reports whether a pair is wired through: its repository
// in HQ, the Mate's branch checked out, and a state that lets it push.
func hqPairOnItsBranch(m *workflow.ServiceMeta) bool {
	return hqPairWired(m) && m.HQ.Branch != "" && hqPairPushes(m.GitPushState)
}

// hqPairPushes reports whether a wired pair's state lets it push: set up, or
// marked by a refused credential — which the push credential step checks
// again, so the mark heals by itself (hqEnsurePushCredential).
func hqPairPushes(state topology.GitPushState) bool {
	return state == topology.GitPushConfigured || state == topology.GitPushBroken
}

// manualAbsorbSequence is the recovery a REAL conflict inside absorbing a
// landed change needs — never the plain fetch+merge advice, which would
// recreate the very conflict it is meant to resolve: `main` still does not
// contain S's content until this sequence lands it. hostname is the checkout
// to run it in; landedCommit is S.
//
// proven distinguishes the two shapes that reach this. An absorb conflict
// already PROVED tree(S) == a mechanical merge of S^1 and H before the S^1
// merge itself hit a real conflict against unrelated later work — so
// resolving it by hand leaves S's own content intact, and `git merge -s ours
// S` (record S merged without touching the tree) is sound. An unprovable
// landing never established that: `-s ours` there would silently discard
// whatever S's real content was. The advice is a plain `git merge S`
// instead — once its own S^1 merge lands, merge-base(HEAD, S) is exactly
// S^1, so this is a REAL three-way merge, and any conflicts it raises are
// resolved on their own merits.
func manualAbsorbSequence(hostname, landedCommit string, proven bool) string {
	absorbStep := fmt.Sprintf("`git merge -s ours %s`", landedCommit)
	if !proven {
		absorbStep = fmt.Sprintf("`git merge %s` — never `-s ours` here, nothing proved its content is already accounted for — and resolve any conflicts on their own merits", landedCommit)
	}
	return fmt.Sprintf(
		"In %s's checkout run `git merge %s^1`, resolve it and commit, then %s, then `git fetch origin && git merge origin/%s`",
		hostname, landedCommit, absorbStep, hqBase)
}

// hqRemoteOfThisMate reports whether a remote is a repository on this Mate's
// HQ, where nothing builds from a Mate's change.
func hqRemoteOfThisMate(remoteURL string) bool {
	return ops.IsHQRemote(remoteURL, hqAddress())
}

// hqPushNextActions answers a push from hostname to HQ. Nothing builds from
// a change's branch: the Mate's own services change only through a direct
// deploy, and deploying the stage half pushes by itself. A change left open
// asks for its description.
func hqPushNextActions(ref *changeRef, hostname string) string {
	if ref == nil {
		return "Nothing differs from main in HQ, so no change is open. Nothing builds from HQ: deploy the pair directly to run the code — deploying its stage half delivers and asks for the change again."
	}
	return fmt.Sprintf("Pushed to %s in HQ; change #%d (%s) carries it to %q, and the person merges it. %s %s Nothing builds from the branch: deploy the pair directly to run the code — deploying its stage half pushes and updates the change while it is open.",
		ref.Branch, ref.Number, ref.URL, ref.Base, describeLine(ref, hostname), hqMergeAnyMoment(ref.Base))
}

// hqMergeAnyMoment prepares the agent for a person merging the change from
// the conversation before the agent has finished working.
func hqMergeAnyMoment(base string) string {
	return fmt.Sprintf("The person may merge it at any moment, before you finish — that is the hand-over, not something to check or undo: the next stage deploy folds the merge in and opens a new change only for what %q still lacks.", base)
}

// hqHandoffNote is what the person needs from the Mate's closing message in a
// Mate that delivers through HQ: the change to review, and what they do next.
const hqHandoffNote = "In your closing message tell the person: the change to review, by its link, and that once it is merged they add a stage and a production from the projects page."
