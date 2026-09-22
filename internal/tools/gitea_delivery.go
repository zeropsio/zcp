package tools

import (
	"context"
	"fmt"
	"strings"

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
// pull request, and proposes the group's recipe again — the deploys have just
// recorded the setups it names. The dev half's deploys are the loop and
// deliver nothing.
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
) *giteaDelivery {
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
	giteaLearnLanding(ctx, httpClient, stateDir, wiring, meta)

	repo, branch := meta.Gitea.FullName, meta.Gitea.Branch
	landedCommit, landedHead := "", ""
	if landed := meta.Gitea.Landed; landed != nil {
		landedCommit, landedHead = landed.Commit, landed.Head
	}

	refreshGiteaWorkflow(ctx, sshDeployer, meta.Hostname)

	output, err := sshDeployer.ExecSSH(ctx, meta.Hostname,
		ops.BuildGiteaDeliveryCommand(giteaPairWorkingDir, branch, giteaBaseOf(meta), giteaCommitMessage(stateDir, meta),
			landedCommit, landedHead))
	if conflicts := ops.GiteaDeliveryConflict(string(output)); conflicts != "" {
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
	// content), this delivery is done with it.
	if meta.Gitea.Landed != nil {
		clearGiteaLanding(stateDir, meta)
	}

	delivery := &giteaDelivery{PullRequest: openGiteaPairPullRequest(ctx, httpClient, wiring, stateDir, meta)}
	if pr := delivery.PullRequest; pr != nil {
		delivery.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of %s, and pull request #%d (%s) carries it to %q. Tell the person that link — the code reaches the group's stage when they merge it.",
			meta.Hostname, branch, repo, pr.Number, pr.URL, pr.Base)
	} else {
		delivery.Line = fmt.Sprintf(
			"Delivered: %s's code is on %s of %s. No pull request is open onto %q yet — Gitea opens one only for a branch that differs from it; the next stage deploy asks again.",
			meta.Hostname, branch, repo, giteaBaseOf(meta))
	}
	if line := reconcileGiteaGroupRecipe(ctx, client, httpClient, rt, stateDir, mate.LiveEnvStorePath); line != "" {
		delivery.Line += " The group's recipe: " + line
	}
	return delivery
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
func giteaLearnLanding(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir string,
	wiring ops.GiteaWiring,
	m *workflow.ServiceMeta,
) {
	if m == nil || m.Gitea == nil || m.Gitea.PullRequest == 0 {
		return
	}
	_ = readGiteaPairPullRequestOutcome(ctx, httpClient, nil, stateDir, wiring, m)
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
// conflict) clears the landing and returns "". A REAL conflict — the same
// shape a delivery aborts on — returns the conflicting files and leaves the
// checkout exactly as it was; the caller must not push on top of that. Any
// other failure (a transport hiccup reaching the container) is not a real
// conflict and is not this function's to classify — it returns "" on the
// wager that a genuinely broken container fails the push that follows right
// after through its own, better-classified path, rather than duplicating
// that classification here.
func giteaAbsorbBeforePush(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	stateDir, hostname, workingDir string,
	meta *workflow.ServiceMeta,
) string {
	if meta == nil || meta.Gitea == nil {
		return ""
	}
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	if !wiring.Ready() {
		return ""
	}
	giteaLearnLanding(ctx, httpClient, stateDir, wiring, meta)
	landed := meta.Gitea.Landed
	if landed == nil {
		return ""
	}
	output, err := sshDeployer.ExecSSH(ctx, hostname,
		ops.BuildGiteaAbsorbAndSyncCommand(workingDir, giteaBaseOf(meta), landed.Commit, landed.Head))
	if conflictFiles := ops.GiteaDeliveryConflict(string(output)); conflictFiles != "" {
		return conflictFiles
	}
	if err != nil {
		return ""
	}
	clearGiteaLanding(stateDir, meta)
	return ""
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
