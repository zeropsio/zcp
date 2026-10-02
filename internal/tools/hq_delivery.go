package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
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
//   - a merge recorded, or just learned by the fresh read → the code is on
//     main; point to the projects page;
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
			if m.HQ.Change != 0 {
				if state, err := self(); err == nil {
					hqLearnLanding(stateDir, m, state)
				}
			}
			switch {
			case m.HQ.Change != 0:
				open = append(open, fmt.Sprintf("%s's change #%d on %q", m.Hostname, m.HQ.Change, m.HQ.Repo))
			case m.HQ.Landed != nil:
				merged = true
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
// — the deploy itself succeeded either way.
func deliverHQPair(
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
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if !hqPairWired(meta) || meta.HQ.Branch == "" || !hqPairPushes(meta.GitPushState) ||
		meta.StageHostname == "" || target != meta.StageHostname {
		return nil
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return nil
	}

	// A stage deploy is a delivery whether or not a pass has run since the
	// last merge — the passes are backoff-gated — so the Mate's own state is
	// read fresh here. What it says about the OLD change is folded onto
	// whatever this delivery itself reports, success or failure: nothing else
	// would ever say it once the number is off the pair.
	var news []string
	defer func() {
		if delivery != nil && len(news) > 0 {
			delivery.Line = strings.TrimSpace(delivery.Line + " " + sentenceOf(strings.Join(news, "; ")))
		}
	}()
	if state, err := hqc.Self(ctx); err == nil {
		switch {
		case state.AppID != nil && *state.AppID != meta.HQ.AppID:
			attempt := rewireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, meta, meta.HQ.Repo)
			if !attempt.wired {
				return &hqDelivery{Line: fmt.Sprintf(
					"%s runs, but its code has not reached HQ: HQ holds this Mate in another application now, and wiring %s's repository there did not complete — %s",
					target, meta.Hostname, attempt.line)}
			}
			news = append(news, attempt.line)
			if meta, _ = workflow.FindServiceMeta(stateDir, target); !hqPairWired(meta) {
				return nil
			}
		default:
			if note := hqLearnLanding(stateDir, meta, state); note != "" {
				news = append(news, note)
			}
		}
	}

	repo := meta.HQ.Repo
	landedCommit, landedHead := landingOf(meta)
	if err := hqEnsurePushCredential(ctx, client, sshDeployer, rt.ProjectID, stateDir, hqc, meta); err != nil {
		return &hqDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: %v. %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue.",
			target, repo, err, meta.Hostname)}
	}

	refreshGiteaWorkflow(ctx, sshDeployer, meta.Hostname)

	title := changeTitle(stateDir, meta)
	if pending := meta.HQ.Pending; pending != nil && workSessionIntent(stateDir) == "" {
		title = pending.Title
	}
	output, err := sshDeployer.ExecSSH(ctx, meta.Hostname,
		ops.BuildDeliveryCommand(hqPairWorkingDir, title, landedCommit, landedHead))
	if line := deliveryRefusalLine(string(output), target, repo, meta.Hostname, landedCommit); line != "" {
		return &hqDelivery{Line: line}
	}
	ahead, found := ops.DeliveryAhead(string(output))
	if err != nil || !found {
		if ops.GitRemoteUnavailable(string(output)) {
			recordPendingDelivery(stateDir, meta, title)
			return &hqDelivery{Line: fmt.Sprintf(
				"%s runs; its code is committed in %s's checkout, but HQ could not be reached (%s). The delivery is kept and finishes by itself once HQ answers — on the next stage deploy, push or pass; nothing for the person to do.",
				target, meta.Hostname, gitPushErrorDetail(err, output))}
		}
		if cls := classifyTransportError(err, deployStrategyGitPush); cls != nil && cls.Category == topology.FailureClassCredential {
			hqMarkPushRefused(stateDir, meta)
			return &hqDelivery{Line: fmt.Sprintf(
				"%s runs, but its code has not reached its repository %q in HQ: HQ refused %s's credential (%s). %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue.",
				target, repo, meta.Hostname, gitPushErrorDetail(err, output), meta.Hostname)}
		}
		return &hqDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached its repository %q in HQ: committing it and taking %q in failed (%s). Fix the cause, then deploy %s again — the change follows that deploy.",
			target, repo, hqBase, gitPushErrorDetail(err, output), target)}
	}

	shipped := shipChange(ctx, sshDeployer, stateDir, hqc, meta, title, ahead)
	result := &hqDelivery{Change: shipped.ref}
	switch {
	case shipped.ref != nil:
		result.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of its repository %q in HQ, and change #%d (%s) carries it to %q. %s Tell the person that link — the code lands on %q when they merge it.",
			meta.Hostname, shipped.ref.Branch, repo, shipped.ref.Number, shipped.ref.URL, hqBase, describeLine(shipped.ref, meta.Hostname), hqBase)
	case shipped.upToDate:
		result.Line = fmt.Sprintf(
			"Delivered: %s's code is in its repository %q in HQ as it is on %q — nothing differs from it, so no change is open; the next stage deploy asks again.",
			meta.Hostname, repo, hqBase)
	case shipped.pending:
		result.Line = fmt.Sprintf(
			"%s runs; its code is committed in %s's checkout, but %s. The delivery is kept and finishes by itself once HQ answers — on the next stage deploy, push or pass; nothing for the person to do.",
			target, meta.Hostname, shipped.line)
	default:
		result.Line = fmt.Sprintf("%s runs, but its code has not reached its repository %q in HQ: %s. Fix the cause, then deploy %s again — the change follows that deploy.",
			target, repo, shipped.line, target)
	}
	if line := reconcileGiteaGroupRecipe(ctx, client, httpClient, rt, stateDir, mate.LiveEnvStorePath); line != "" {
		result.Line += " The group's recipe: " + line
	}
	return result
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

// refreshGiteaWorkflow brings a wired pair's workflow to the one this zcp
// deploys through: wiring writes the file once and never runs again for a
// wired pair, so a repository wired by an earlier zcp kept that zcp's
// workflow for good. Every delivery passes through here, and its commit
// carries the file.
//
// zcp owns the triggers and the deploy step; the project owns its Test step.
// So a file that already names this zcp's deploy action is left exactly as it
// is, and one that does not is written again with its own Test step kept.
// Best-effort: a delivery without it still lands the code, and the next one
// tries again.
func refreshGiteaWorkflow(ctx context.Context, sshDeployer ops.SSHDeployer, hostname string) {
	existing, err := sshDeployer.ExecSSH(ctx, hostname,
		ops.BuildReadRepoFileCommand(hqPairWorkingDir, giteaWorkflowFilePath))
	if err != nil || giteaWorkflowCurrent(string(existing)) {
		return
	}
	_, _ = sshDeployer.ExecSSH(ctx, hostname, ops.BuildWriteRepoFileCommand(
		hqPairWorkingDir, giteaWorkflowFilePath, giteaWorkflowKeepingTests(string(existing)),
	))
}

// giteaWorkflowCurrent reports whether a workflow deploys through the action
// this zcp writes.
func giteaWorkflowCurrent(workflow string) bool {
	return strings.Contains(workflow, "uses: "+giteaBrokerDeployAction)
}

// giteaWorkflowKeepingTests is this zcp's workflow with the Test step of the
// one it replaces — the one part of the file that is the project's own. A
// file with no such step, or one laid out differently, gets the template's.
func giteaWorkflowKeepingTests(existing string) string {
	fresh := giteaWorkflowYAML()
	kept, template := giteaWorkflowStep(existing, giteaWorkflowTestStep), giteaWorkflowStep(fresh, giteaWorkflowTestStep)
	if kept == "" || template == "" {
		return fresh
	}
	return strings.Replace(fresh, template, kept, 1)
}

// giteaWorkflowTestStep is the name of the step a project fills in.
const giteaWorkflowTestStep = "Test"

// giteaWorkflowStep is one step of a workflow zcp wrote, from its `- name:`
// line up to the next step, newline included; "" when the file has none at
// the indentation zcp writes.
func giteaWorkflowStep(workflow, name string) string {
	const stepPrefix = "      - "
	lines := strings.SplitAfter(workflow, "\n")
	start := -1
	for i, line := range lines {
		if start < 0 {
			if strings.TrimRight(line, "\r\n") == stepPrefix+"name: "+name {
				start = i
			}
			continue
		}
		if strings.HasPrefix(line, stepPrefix) || (strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "       ")) {
			return strings.Join(lines[start:i], "")
		}
	}
	if start < 0 {
		return ""
	}
	return strings.Join(lines[start:], "")
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
	return fmt.Sprintf("Pushed to %s in HQ; change #%d (%s) carries it to %q, and the person merges it. %s Nothing builds from the branch: deploy the pair directly to run the code — deploying its stage half pushes and updates the change by itself.",
		ref.Branch, ref.Number, ref.URL, ref.Base, describeLine(ref, hostname))
}

// hqHandoffNote is what the person needs from the Mate's closing message in a
// Mate that delivers through HQ: the change to review, and what they do next.
const hqHandoffNote = "In your closing message tell the person: the change to review, by its link, and that once it is merged they add a stage and a production from the projects page."
