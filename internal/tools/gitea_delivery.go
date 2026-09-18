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
	repo, branch := meta.Gitea.FullName, meta.Gitea.Branch

	refreshGiteaWorkflow(ctx, sshDeployer, meta.Hostname)

	output, err := sshDeployer.ExecSSH(ctx, meta.Hostname,
		ops.BuildGiteaDeliveryCommand(giteaPairWorkingDir, branch, giteaCommitMessage(stateDir, meta)))
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
