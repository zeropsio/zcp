package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate wired to its group's Gitea delivers through pull requests, and only a
// dev/stage pair can (launch_source_control_gate.go): the dev half is the
// checkout that pushes, the stage half the verified basis a production is
// promoted from. Two rules follow, both measured missing on 2026-09-17, when
// "create a todo app" on a fresh Mate got bootstrapMode simple — one service,
// nothing pushed, no pull request, and the person expanding it by hand:
//
//   - the classic route plans every runtime of a wired Mate as a standard pair;
//   - a direct deploy of a wired pair names the push that lands its code.

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

// giteaDeliveryNextAction names the push that lands a wired pair's code after
// a direct deploy: the container runs it, the group's repository has not seen
// it, and the pull request that lands it exists only once a git-push deploy
// makes it (giteaPullRequestAfterPush). Empty for a pair that is not wired to
// Gitea or not git-push-configured.
func giteaDeliveryNextAction(stateDir, target string, wired bool) string {
	if !wired {
		return ""
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if meta == nil || meta.Gitea == nil || meta.Gitea.FullName == "" || meta.GitPushState != topology.GitPushConfigured {
		return ""
	}
	return fmt.Sprintf("Then deliver: commit, and run zerops_deploy targetService=%q strategy=\"git-push\" — it pushes this Mate's branch to %s and opens the pull request the code lands through. Nothing reaches the group's repository until it runs; do it before you finish.", meta.Hostname, meta.Gitea.FullName)
}

// giteaHandoffNote is what the person needs from the Mate's closing message
// in a wired group: the request to review, and what they do next.
const giteaHandoffNote = "In your closing message tell the person: the pull request to review, by its link, and that once it is merged they add a stage and a production from the projects page — the group's recipe is on its repository's main."
