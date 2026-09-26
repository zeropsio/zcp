package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate wired to its group's Gitea delivers through pull requests, and only a
// dev/stage pair can (launch_source_control_gate.go): the dev half is the
// checkout that pushes, the stage half the verified basis a production is
// promoted from. The rules below were each measured missing on 2026-09-17, when
// "create a todo app" on a fresh Mate got bootstrapMode simple — one service,
// nothing pushed, no pull request, and the person expanding it by hand:
//
//   - the classic route plans every runtime of a wired Mate as a standard pair;
//   - a deploy onto a wired pair's stage half delivers it: commit, push, pull
//     request, with nothing asked of the agent or the person;
//   - a push to the group's Gitea is watched for no build and offers no
//     integration, and a wired pair's direct deploys are never redirected.

// giteaWired reports whether this container carries the group's Gitea wiring.
func giteaWired() bool {
	return ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath)).Ready()
}

// giteaPairPlanError refuses a classic-route plan that gives a wired Mate a
// runtime with no stage half. Nil when the Mate is not wired, or every
// runtime is a standard pair.
func giteaPairPlanError(plan []workflow.BootstrapTarget, wired bool) *platform.PlatformError {
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
			fmt.Sprintf("Plan validation failed: target %q: this Mate is wired to its group's Gitea, where code lands through pull requests, so every runtime is a dev/stage pair — bootstrapMode %q with no stage half cannot be pushed to the group's repository or promoted to stage and production", rt.DevHostname, rt.EffectiveMode()),
			fmt.Sprintf("Re-submit the plan with runtime.bootstrapMode=\"standard\" and an explicit stageHostname for %q (for example devHostname %q, stageHostname %q).", rt.DevHostname, rt.DevHostname, giteaStageNameFor(rt.DevHostname)),
		)
	}
	return nil
}

// giteaLaunchProductionRefusal refuses the whole launch-production workflow
// in a wired Mate (plans/backlog/mate-wired-production-intent-misroutes-to-launch.md):
// launch-production creates and imports its OWN production project on a
// user-owned remote (workflow_launch_production.go) — it has no case for a
// group's production, which the person adds from the projects page and
// which the broker feeds from a release tag on the group repo (spec-mate.md
// §10, D16, D27, D28). Running it anyway would try to build a second,
// unwired production the group's broker never touches, on a plan gate
// (giteaPairPlanError) that already refuses it a stage half to build from.
//
// wired is the caller's own giteaWired() read — no second detector. nil
// when the Mate is not wired, so the classic route is untouched.
func giteaLaunchProductionRefusal(ctx context.Context, httpClient ops.HTTPDoer, stateDir string, wired bool) *mcp.CallToolResult {
	if !wired {
		return nil
	}
	return launchFailedResponse(nil, topology.BlockerCategoryOther, "wired_mate_production_is_the_groups",
		"This Mate is wired to its group's Gitea, where a group's production is a project the person adds "+
			"from the projects page and runs only what a release tag lists — launch-production creates its "+
			"own, ungrouped production project and has no case for that. "+giteaLaunchProductionNextStep(ctx, httpClient, stateDir))
}

// giteaLaunchProductionNextStep says what is true of THIS Mate's own pairs
// rather than a lecture (§10.10 "What became of the request"). A pair's
// recorded PullRequest number is only cleared when a reconcile pass reads
// Gitea, and that pass is backoff-gated — so a request the person already
// merged or closed on Gitea can still be sitting here as "open" the moment
// this runs. Every non-zero PullRequest is freshened with giteaLearnLanding
// (the same fresh-read helper deliverGiteaPair and giteaAbsorbBeforePush
// use) before it is ever named, so "merge #N first" is never said about work
// that already landed.
//
// Three states, never collapsed into "nothing open means merged"
// (measured live: `Mate: weatherdev (#4)` merged on Gitea while this still
// said to merge it):
//   - a request still open after the fresh read → name it, tell the person
//     to merge it;
//   - a merge recorded in Landed, or just learned by the fresh read → the
//     code is on the group's main; point to the projects page;
//   - no request at all (never pushed, no repository yet) or one closed
//     without merging → nothing of this pair's work has reached main; tell
//     the person to deliver through the stage half first.
func giteaLaunchProductionNextStep(ctx context.Context, httpClient ops.HTTPDoer, stateDir string) string {
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	var open []string
	merged := false
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		for _, m := range metas {
			if m == nil || m.Gitea == nil {
				continue
			}
			if m.Gitea.PullRequest != 0 {
				giteaLearnLanding(ctx, httpClient, stateDir, wiring, m)
			}
			switch {
			case m.Gitea.PullRequest != 0:
				open = append(open, fmt.Sprintf("%s's pull request #%d on %s", m.Hostname, m.Gitea.PullRequest, m.Gitea.FullName))
			case m.Gitea.Landed != nil:
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
	return "Tell the person: none of this Mate's work has reached the group's repository yet — deliver it by deploying the pair's stage half first, then production is added and released from the project on Mate's projects page."
}

// giteaStageNameFor suggests the stage half's hostname: appdev → appstage,
// todoapp → todoappstage.
func giteaStageNameFor(devHostname string) string {
	if strings.HasSuffix(devHostname, "dev") && len(devHostname) > len("dev") {
		return strings.TrimSuffix(devHostname, "dev") + "stage"
	}
	return devHostname + "stage"
}

// giteaDelivery is what a wired pair's delivery did, in the words the agent
// passes on.
type giteaDelivery struct {
	PullRequest *giteaPullRequestRef
	Line        string
}

// deliverGiteaPair is how a wired pair's work reaches its group without anyone
// saying how (the owner, 2026-09-17, of a prompt that had to name a git-push
// deploy: "no person is ever going to say this"). A deploy onto the pair's
// stage half is the moment the work is shippable, so zcp commits the dev
// half's tree as it was deployed, pushes the Mate's branch, opens or finds the
// pull request, and proposes whatever tiers of the group's recipe the group
// repo's main still lacks — the deploys have just recorded the setups they
// name. The dev half's deploys are the loop and deliver nothing.
//
// Nil when there is nothing to deliver: no Gitea on this Mate, no wired pair
// behind the target, not its stage half, not in a container. Otherwise a line
// for the deploy's next actions, a failed delivery included — the deploy
// itself succeeded either way.
func deliverGiteaPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir, target string,
) (delivery *giteaDelivery) {
	if sshDeployer == nil || !rt.InContainer {
		return nil
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if meta == nil || meta.Gitea == nil || meta.Gitea.FullName == "" || meta.Gitea.Branch == "" ||
		meta.GitPushState != topology.GitPushConfigured || meta.StageHostname == "" || target != meta.StageHostname {
		return nil
	}
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	if !wiring.Ready() {
		return nil
	}

	// A stage deploy is a delivery whether or not the reconcile pass has run
	// since the last merge — the passes are backoff-gated (giteaAttemptDue)
	// and a delivery must not wait on one to learn a fresh merge.
	//
	// The learned line ("pull request #N is merged…") is folded onto
	// whatever this delivery itself reports, success or failure — it is
	// news about the OLD request, independent of this attempt's own
	// outcome, and PullRequest is now 0 (or Landed, not PullRequest) so no
	// later pass would ever say it otherwise.
	if note := giteaLearnLanding(ctx, httpClient, stateDir, wiring, meta); note != "" {
		defer func() {
			if delivery != nil {
				delivery.Line = strings.TrimSpace(delivery.Line + " " + note)
			}
		}()
	}

	repo, branch := meta.Gitea.FullName, meta.Gitea.Branch
	landedCommit, landedHead := "", ""
	if landed := meta.Gitea.Landed; landed != nil {
		landedCommit, landedHead = landed.Commit, landed.Head
	}

	refreshGiteaWorkflow(ctx, sshDeployer, meta.Hostname)

	output, err := sshDeployer.ExecSSH(ctx, meta.Hostname,
		ops.BuildGiteaDeliveryCommand(giteaPairWorkingDir, branch, giteaBaseOf(meta), giteaCommitMessage(stateDir, meta),
			landedCommit, landedHead))
	if ops.GiteaAbsorbDirty(string(output)) {
		return &giteaDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached %s: uncommitted changes in %s's checkout block taking in this Mate's own landed pull request. Commit them, then deploy %s again — the push and the pull request follow that deploy.",
			target, repo, meta.Hostname, target)}
	}
	if conflicts := ops.GiteaAbsorbConflict(string(output)); conflicts != "" {
		return &giteaDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached %s: a real conflict inside absorbing this Mate's own earlier pull request — %s changes the same lines (%s). %s, then deploy %s again — the push and the pull request follow that deploy.",
			target, repo, giteaBaseOf(meta), conflicts, giteaManualAbsorbSequence(meta.Hostname, landedCommit, giteaBaseOf(meta), true), target)}
	}
	if conflicts := ops.GiteaDeliveryConflict(string(output)); conflicts != "" {
		if ops.GiteaAbsorbUnprovable(string(output)) && landedCommit != "" {
			return &giteaDelivery{Line: fmt.Sprintf(
				"%s runs, but its code has not reached %s: %s has moved on and %s changes the same lines (%s) — this may be this Mate's own squashed pull request, which this container's git could not prove safe to fold in automatically. %s, then deploy %s again — the push and the pull request follow that deploy.",
				target, repo, giteaBaseOf(meta), branch, conflicts, giteaManualAbsorbSequence(meta.Hostname, landedCommit, giteaBaseOf(meta), false), target)}
		}
		return &giteaDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached %s: %s has moved on and %s changes the same lines (%s). In %s's checkout run `git fetch origin && git merge origin/%s`, resolve it, then deploy %s again — the push and the pull request follow that deploy.",
			target, repo, giteaBaseOf(meta), branch, conflicts, meta.Hostname, giteaBaseOf(meta), target)}
	}
	if unignored := ops.GiteaDeliveryUnignored(string(output)); unignored != "" {
		return &giteaDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached %s: %s in %s is not ignored and would be committed. Add a .gitignore that ignores it, then deploy %s again — the push and the pull request follow that deploy.",
			target, repo, unignored, meta.Hostname, target)}
	}
	if err != nil {
		return &giteaDelivery{Line: fmt.Sprintf(
			"%s runs, but its code has not reached %s: pushing %s failed (%s). Fix the cause, then deploy %s again — the push and the pull request follow that deploy.",
			target, repo, branch, gitPushErrorDetail(err, output), target)}
	}

	// The push landed — whatever the landing needed (an absorb, or nothing:
	// S was already an ancestor, or the ordinary merge already carried its
	// content), this delivery is done with it. The absorb's own merges, if
	// it ran, are committed and stay — this only forgets the bookkeeping.
	if meta.Gitea.Landed != nil {
		clearGiteaLanding(stateDir, meta)
	}

	result := &giteaDelivery{PullRequest: openGiteaPairPullRequest(ctx, httpClient, wiring, stateDir, meta)}
	if pr := result.PullRequest; pr != nil {
		result.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of %s, and pull request #%d (%s) carries it to %q. Tell the person that link — the code reaches the group's stage when they merge it.",
			meta.Hostname, branch, repo, pr.Number, pr.URL, pr.Base)
	} else {
		result.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of %s. No pull request is open onto %q yet — Gitea opens one only for a branch that differs from it; the next stage deploy asks again.",
			meta.Hostname, branch, repo, giteaBaseOf(meta))
	}
	if line := reconcileGiteaGroupRecipe(ctx, client, httpClient, rt, stateDir, mate.LiveEnvStorePath); line != "" {
		result.Line += " The group's recipe: " + line
	}
	return result
}

// giteaManualAbsorbSequence is the recovery a REAL conflict inside
// absorbing a landed pull request needs — never the plain fetch+merge
// advice, which would recreate the very conflict it is meant to resolve:
// the base still does not contain S's content until this sequence lands it.
// hostname is the checkout to run it in; landedCommit is S.
//
// proven distinguishes the two shapes that reach this. An absorb conflict
// (IsAbsorbConflict) already PROVED tree(S) == a mechanical merge of S^1
// and H before the S^1 merge itself hit a real conflict against unrelated
// later work — any conflict there can only be between that later work and
// S^1 (S^1 and H were already proven not to collide), so resolving it by
// hand leaves S's own content intact, and `git merge -s ours S` (record S
// merged without touching the tree) is sound. An unprovable landing never
// established that: `-s ours` there would silently discard whatever S's
// real content was without checking it against anything. The advice is a
// plain `git merge S` instead — once its own S^1 merge above lands,
// merge-base(HEAD, S) is exactly S^1, so this is a REAL three-way merge (no
// longer the "unrelated histories" problem the whole mechanism exists to
// avoid), and any conflicts it raises are the agent's or person's to
// resolve on their own merits, never assumed away.
func giteaManualAbsorbSequence(hostname, landedCommit, base string, proven bool) string {
	absorbStep := fmt.Sprintf("`git merge -s ours %s`", landedCommit)
	if !proven {
		absorbStep = fmt.Sprintf("`git merge %s` — never `-s ours` here, nothing proved its content is already accounted for — and resolve any conflicts on their own merits", landedCommit)
	}
	return fmt.Sprintf(
		"In %s's checkout run `git merge %s^1`, resolve it and commit, then %s, then `git fetch origin && git merge origin/%s`",
		hostname, landedCommit, absorbStep, base)
}

// giteaLearnLanding reads a pair's own recorded pull request's outcome
// directly — not through the backoff-gated reconcile pass — so a merge
// landed since the last read is known before the caller acts on
// meta.Gitea.Landed. Shared by deliverGiteaPair (which embeds the landing in
// its own commit+push command, ops.BuildGiteaDeliveryCommand) and
// giteaAbsorbBeforePush (which absorbs it in a standalone step ahead of an
// unrelated push, handleGitPush): both need a landing known FRESH, not
// whatever a reconcile pass happened to have seen last.
//
// Passes a nil SSHDeployer to readGiteaPairPullRequestOutcome, so its own
// best-effort checkout catch-up (absorbLandedPullRequestOnCheckout) never
// fires here — that catch-up exists for the RECONCILE PASS, the one caller
// with no absorb step of its own right after. Both callers here run their
// own absorb unconditionally the moment this returns, so a second, silent
// attempt first would only be redundant SSH round trips.
//
// Returns readGiteaPairPullRequestOutcome's own line ("" while the request
// is still open, the ordinary state) — the caller folds it onto whatever it
// itself reports, since it is news about the OLD request, independent of
// this attempt's own outcome, and nothing else would ever say it once the
// number is gone from PullRequest.
func giteaLearnLanding(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir string,
	wiring ops.GiteaWiring,
	m *workflow.ServiceMeta,
) string {
	if m == nil || m.Gitea == nil || m.Gitea.PullRequest == 0 {
		return ""
	}
	return readGiteaPairPullRequestOutcome(ctx, httpClient, nil, stateDir, wiring, m)
}

// giteaAbsorbOutcome is what giteaAbsorbBeforePush learned and did.
type giteaAbsorbOutcome struct {
	// Dirty is true when a proven-safe landing could not actually be
	// merged because the checkout was not clean — checked and reported
	// BEFORE Conflict, since it fires before any merge is attempted (no
	// unmerged file list to give; the caller must not read that as "no
	// conflict" and push anyway).
	Dirty bool
	// Conflict is the conflicting files, "" when there was none (nothing to
	// absorb, absorbed cleanly, or a non-conflict failure — see
	// giteaAbsorbBeforePush).
	Conflict string
	// IsAbsorbConflict is true when Conflict came from the absorb's own S^1
	// merge (ops.GiteaAbsorbConflict) rather than the ordinary
	// take-the-base-in step (ops.GiteaDeliveryConflict) — the two need
	// different recovery advice.
	IsAbsorbConflict bool
	// Unprovable is true when a recorded landing fell through unabsorbed
	// because it could not be proven lossless (ops.GiteaAbsorbUnprovable) —
	// meaningful only alongside an ordinary conflict: the false conflict
	// this whole mechanism exists to prevent may be exactly what Conflict
	// is reporting.
	Unprovable bool
	// LandedCommit is S — needed to build the manual absorb sequence when
	// Conflict is set. "" when nothing was recorded to absorb.
	LandedCommit string
	// LearnedNote is readGiteaPairPullRequestOutcome's own line about the
	// PREVIOUSLY recorded request ("pull request #N is merged…") — news
	// about that OLD request, independent of whether THIS absorb found
	// anything to do. "" when there was nothing to say.
	LearnedNote string
}

// giteaAbsorbBeforePush is what a wired pair's push OTHER than a delivery
// (handleGitPush's plain strategy=git-push, never deliverGiteaPair's own
// commit+push) needs first: without it, a pull request this push then opens
// or touches (giteaPullRequestAfterPush) can show a false conflict to a
// person the moment they look at it, before any delivery has run — a squash
// of THIS Mate's own earlier work shares no history with the branch it came
// from (ops.BuildAbsorbLandedPullRequestCommand), and nothing before this
// point ever took it in. Measured live: the incident's PR #2 was opened by
// exactly this path.
//
// Learns a fresh landing itself (giteaLearnLanding) — never waits on the
// backoff-gated reconcile pass — then runs ops.BuildGiteaAbsorbAndSyncCommand
// on the checkout. A clean run (nothing to absorb, or absorbed without a
// conflict) clears the landing. A REAL conflict — either inside the absorb's
// own S^1 merge, or the ordinary take-the-base-in step that follows it —
// leaves the checkout exactly as the absorb's own abort left it (the
// caller must not push on top of that) and is reported in full. Any other
// failure (a transport hiccup reaching the container) is not a real
// conflict and is not this function's to classify — it reports no conflict
// on the wager that a genuinely broken container fails the push that
// follows right after through its own, better-classified path, rather than
// duplicating that classification here.
func giteaAbsorbBeforePush(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	stateDir, hostname, workingDir string,
	meta *workflow.ServiceMeta,
) giteaAbsorbOutcome {
	if meta == nil || meta.Gitea == nil {
		return giteaAbsorbOutcome{}
	}
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	if !wiring.Ready() {
		return giteaAbsorbOutcome{}
	}
	note := giteaLearnLanding(ctx, httpClient, stateDir, wiring, meta)
	landed := meta.Gitea.Landed
	if landed == nil {
		return giteaAbsorbOutcome{LearnedNote: note}
	}
	output, err := sshDeployer.ExecSSH(ctx, hostname,
		ops.BuildGiteaAbsorbAndSyncCommand(workingDir, giteaBaseOf(meta), landed.Commit, landed.Head))
	if ops.GiteaAbsorbDirty(string(output)) {
		return giteaAbsorbOutcome{Dirty: true, LandedCommit: landed.Commit, LearnedNote: note}
	}
	if conflictFiles := ops.GiteaAbsorbConflict(string(output)); conflictFiles != "" {
		return giteaAbsorbOutcome{Conflict: conflictFiles, IsAbsorbConflict: true, LandedCommit: landed.Commit, LearnedNote: note}
	}
	if conflictFiles := ops.GiteaDeliveryConflict(string(output)); conflictFiles != "" {
		return giteaAbsorbOutcome{
			Conflict:     conflictFiles,
			Unprovable:   ops.GiteaAbsorbUnprovable(string(output)),
			LandedCommit: landed.Commit,
			LearnedNote:  note,
		}
	}
	if err != nil {
		return giteaAbsorbOutcome{LearnedNote: note}
	}
	clearGiteaLanding(stateDir, meta)
	return giteaAbsorbOutcome{LearnedNote: note}
}

// refreshGiteaWorkflow brings a wired pair's workflow to the one this zcp
// deploys through, and this is where it has to happen: wiring writes the file
// once and never runs again for a wired pair, so a repository wired by an
// earlier zcp kept that zcp's workflow for good (2026-09-18: two Mates still
// naming the broker's old deploy action after D27, and nothing that would ever
// change it). Every delivery passes through here, and its commit carries the
// file.
//
// zcp owns the triggers and the deploy step; the project owns its Test step. So
// a file that already names this zcp's deploy action is left exactly as it is
// — whatever a person or the agent made of it — and one that does not is
// written again with its own Test step kept. Best-effort: a delivery without it
// still lands the code, and the next one tries again.
func refreshGiteaWorkflow(ctx context.Context, sshDeployer ops.SSHDeployer, hostname string) {
	existing, err := sshDeployer.ExecSSH(ctx, hostname,
		ops.BuildReadRepoFileCommand(giteaPairWorkingDir, giteaWorkflowFilePath))
	if err != nil || giteaWorkflowCurrent(string(existing)) {
		return
	}
	_, _ = sshDeployer.ExecSSH(ctx, hostname, ops.BuildWriteRepoFileCommand(
		giteaPairWorkingDir, giteaWorkflowFilePath, giteaWorkflowKeepingTests(string(existing)),
	))
}

// giteaWorkflowCurrent reports whether a workflow deploys through the action
// this zcp writes.
func giteaWorkflowCurrent(workflow string) bool {
	return strings.Contains(workflow, "uses: "+giteaBrokerDeployAction)
}

// giteaWorkflowKeepingTests is this zcp's workflow with the Test step of the
// one it replaces — the one part of the file that is the project's own. A file
// with no such step, or one laid out differently, gets the template's.
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
// line up to the next step, newline included; "" when the file has none at the
// indentation zcp writes.
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

// giteaCommitMessage is the task in the person's words — the work session's
// intent — or, with no session open, what the commit is.
func giteaCommitMessage(stateDir string, meta *workflow.ServiceMeta) string {
	if intent := workSessionIntent(stateDir); intent != "" {
		return intent
	}
	return fmt.Sprintf("%s as deployed to %s", meta.Hostname, meta.StageHostname)
}

// giteaRemoteOfThisMate reports whether a remote is the account's own Gitea,
// where nothing builds from a Mate's branch.
func giteaRemoteOfThisMate(remoteURL string) bool {
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	return wiring.Ready() && topology.ClassifyGitHost(remoteURL, wiring.GiteaURL) == topology.GitHostGitea
}

// giteaPushNextActions answers a push to the account's Gitea. The group's
// workflow runs on main, which the person's merge moves, so there is no build
// to watch and no integration to offer: the Mate's own services change only
// through a direct deploy, and deploying the stage half pushes by itself.
func giteaPushNextActions(pr *giteaPullRequestRef) string {
	if pr == nil {
		return "Pushed to this Mate's branch on the group's Gitea; no pull request is open yet (Gitea opens one only for a branch that differs from main). Nothing builds from the branch: deploy the pair directly to run the code — deploying its stage half pushes and asks for the request again."
	}
	return fmt.Sprintf("Pushed to %s on the group's Gitea; pull request #%d (%s) carries it to %q, and the person merges it. Nothing builds from the branch: deploy the pair directly to run the code — deploying its stage half pushes and updates the request by itself.",
		pr.Branch, pr.Number, pr.URL, pr.Base)
}

// giteaHandoffNote is what the person needs from the Mate's closing message
// in a wired group: the request to review, and what they do next.
const giteaHandoffNote = "In your closing message tell the person: the pull request to review, by its link, and that once it is merged they add a stage and a production from the projects page — the group's recipe is on its repository's main."
