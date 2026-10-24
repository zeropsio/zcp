package tools

import (
	"cmp"
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// repoDeliveryRedirect enforces the L1 terminal-act rule
// (spec-git-delivery-target §2, Karel-confirmed 2026-06-10): once a pair's
// GitPushState is configured AND the first deploy has landed, the repo is
// the source of truth and development DELIVERS VIA PUSH — a direct ZCP
// deploy would mint container state the repo never sees (the never-pushed
// `deploy` commit class that made head-not-pushed the expected launch
// state). The dev self-redeploy existed for PERSISTENCE; once code
// persists in git, the baking is unnecessary (Karel's rationale, spec
// §1.1).
//
// Returns nil (proceed) when: the call already delivers via push, the
// caller break-glassed, the target is pre-ladder (L0 / first deploy),
// no meta exists (recipe sessions, unadopted targets — other gates own
// those), or NO ZCP-managed integration consumes the pushes.
//
// That last rung is what makes the rule terminate. A push only reaches a
// service where an integration rebuilds it, so on `BuildIntegration=none`
// (or the bootstrap's empty default) redirecting to push sends the agent
// somewhere the code never arrives: the push handler answers "nothing
// rebuilds <target> — re-run the deploy with breakGlass=true", which is
// the call this gate just refused. Measured on a live Mate promoting
// appdev→appstage: two round trips, ending at an outage escape used for
// a routine promotion. Where nothing consumes the push, the direct
// deploy IS the delivery — it proceeds, and the caller still attaches
// repoDeliveryDivergenceWarning so the reconcile push is never forgotten.
// A user's own CI outside ZCP's knowledge also reads as `none`; the
// warning is what covers that case, since ZCP cannot prove the push
// delivered.
//
// NOT a hard gate: the structured response carries the recommended push
// call AND the break-glass re-call — an unmet requirement with a fix,
// per the goal-contracts delivery rules. Break-glass stays legitimate for
// fundamental reasons (integration outage, recovery); the self-deploy
// path preserves everything by construction (deployFiles [.] + -g ships
// the full tree incl. .git), and the proceed-response flags the
// container-ahead-of-repo divergence so the reconcile push is never
// forgotten.
func repoDeliveryRedirect(stateDir, targetService, strategy string, breakGlass bool) *mcp.CallToolResult {
	if strategy == deployStrategyGitPush || breakGlass {
		return nil
	}
	meta, err := workflow.FindServiceMeta(stateDir, targetService)
	if err != nil || meta == nil {
		return nil
	}
	if hqPairWired(meta) {
		return nil // nothing builds a Mate's services from its change
	}
	if meta.DevIsPool() {
		// A pair keyed by its dev pool: the stage deploys from a lease of the
		// pool, a cross-deploy whatever the git-push state — a git-push from a
		// lease is refused (DP-6) — and a deploy to the pool is its image
		// (DP-7). The direct deploy IS the delivery; a redirect would name the
		// pool as the push source (docs/spec-workflows.md §13 PM-10).
		return nil
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.FirstDeployedAt == "" {
		return nil // L0 artifact flow or first-deploy bypass (D2a)
	}
	if meta.BuildIntegration == "" || meta.BuildIntegration == topology.BuildIntegrationNone {
		return nil // nothing consumes the push — the direct deploy is the delivery
	}

	builds := anticipatedBuilds(meta)
	buildHost, buildSetup := firstBuild(builds)
	pushArgs := map[string]string{
		"targetService": meta.Hostname,
		"strategy":      "git-push",
	}
	body := map[string]any{
		"status":        "push-delivery-required",
		"deliveryState": string(topology.DeriveDeliveryState(meta.GitPushState, len(meta.ProdLaunches) > 0)),
		"service":       targetService,
		"pushSource":    meta.Hostname,
		"buildTarget":   buildHost,
		"buildSetup":    buildSetup,
		"message": "This pair delivers via git push (git-push is configured — the repo is the source of truth and push is the terminal act of development). A direct deploy would put code on the container that the repo never sees. Commit your changes, then push: the integration rebuilds " +
			hostList(builds) + " from the repo and ZCP watches the build to completion.",
		"recommended": map[string]any{
			"tool": "zerops_deploy",
			"args": pushArgs,
		},
		"breakGlass": map[string]any{
			"how":  "Re-call this exact deploy with breakGlass=true — reserved for a FUNDAMENTAL reason (git host outage, recovery) per the delivery spec.",
			"note": "Break-glass self-deploy preserves everything (the artifact ships the full tree incl. .git) and the response will flag that the container is now ahead of the repo — push to reconcile as soon as the reason passes.",
		},
	}
	addBuildTargets(body, builds, false)
	return jsonResult(body)
}

// batchRepoDeliveryRedirect applies the same terminal-act rule to
// zerops_deploy_batch, which bypassed the gate entirely — a batch was a
// silent route around push delivery for any L1 entry it carried. The
// first refusing entry aborts the whole batch, mirroring how pre-flight
// failures abort it before any build burns time.
//
// Batch targets carry no breakGlass field, so the refusal routes the
// escape through the single deploy rather than dead-ending the agent:
// gating recovery is how a delivery rule turns into a deadlock.
func batchRepoDeliveryRedirect(stateDir string, targets []ops.DeployBatchTarget) *mcp.CallToolResult {
	for _, t := range targets {
		redirect := repoDeliveryRedirect(stateDir, t.TargetService, "", false)
		if redirect == nil {
			continue
		}
		payload := map[string]any{
			"status":  "push-delivery-required",
			"service": t.TargetService,
			"message": "Batch aborted: " + t.TargetService + " belongs to a pair that delivers via git push (a ZCP-managed integration rebuilds it from the repo). No entry was deployed.",
			"recommended": map[string]any{
				"tool": "zerops_deploy",
				"args": map[string]string{"targetService": t.TargetService, "strategy": "git-push"},
			},
			"breakGlass": map[string]any{
				"how": "Batch entries carry no breakGlass flag — deploy this target on its own with zerops_deploy targetService=\"" + t.TargetService + "\" breakGlass=true, reserved for a FUNDAMENTAL reason (git host outage, recovery) per the delivery spec.",
			},
		}
		return jsonResult(payload)
	}
	return nil
}

// configuredRemote is the remote git-push-setup points at the pair's
// RemoteURL in the dev's checkout.
const configuredRemote = "origin"

// deployShipped is what a direct deploy shipped and where its source's
// checkout is read: the commit, whether uncommitted changes rode on top of
// it, and a runner reaching the checkout at dir — nil when zcp cannot read
// it.
type deployShipped struct {
	sha   string
	dirty bool
	repo  git.Runner
	dir   string
}

// onTrackedBranch reports a clean deploy of a commit the source's checkout
// already knows on the pair's tracked branch (GF-7's trackedRef) of the
// configured remote: the repository holds what the container now runs, so
// nothing is ahead to reconcile. It reads the checkout's own remote-tracking
// ref, never the remote itself; whatever it cannot show keeps the warning.
func (s deployShipped) onTrackedBranch(ctx context.Context, meta *workflow.ServiceMeta) bool {
	if s.dirty || s.sha == "" || s.repo == nil {
		return false
	}
	return git.CommitOnRemoteBranch(ctx, s.repo, s.dir, configuredRemote, trackedRefOrDefault(meta), s.sha)
}

// sshShipped is what a container deploy shipped, its source's checkout read
// over SSH at the working directory the deploy pushed from (the source
// defaults to the target for a self-deploy).
func sshShipped(ssh ops.SSHDeployer, input DeploySSHInput, result *ops.DeployResult) deployShipped {
	shipped := deployShipped{sha: result.SHA, dirty: result.Dirty, dir: cmp.Or(input.WorkingDir, topology.CodeMountRoot)}
	if ssh != nil {
		shipped.repo = git.SSHRunner{Executor: ssh, Hostname: cmp.Or(input.SourceService, input.TargetService)}
	}
	return shipped
}

// localShipped is what a local deploy shipped, its checkout read in
// workingDir on this machine (the deploy's own default when empty).
func localShipped(workingDir string, result *ops.DeployResult) deployShipped {
	return deployShipped{sha: result.SHA, dirty: result.Dirty, repo: git.LocalRunner{}, dir: cmp.Or(workingDir, ".")}
}

// repoDeliveryDivergenceWarning is attached to a SUCCESSFUL break-glass
// (or otherwise direct) deploy of an L1 target whose container now carries
// state the repo does not — the reconcile push is the standing next step.
// A clean deploy of a commit already on the tracked branch of the configured
// remote, as the source's checkout last saw it, carries none: the container
// is not ahead (docs/spec-workflows.md §8 S5).
func repoDeliveryDivergenceWarning(ctx context.Context, stateDir, targetService string, shipped deployShipped) string {
	meta, err := workflow.FindServiceMeta(stateDir, targetService)
	if err != nil || meta == nil || hqPairWired(meta) {
		return "" // a wired pair's stage deploy pushes by itself
	}
	if meta.DevIsPool() {
		return "" // the stage's deploy from a lease is its delivery; no push from the pool reconciles it
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.FirstDeployedAt == "" {
		return ""
	}
	if shipped.onTrackedBranch(ctx, meta) {
		return ""
	}
	return "container is now AHEAD of the configured repo (direct deploy on a push-delivering pair) — reconcile with: zerops_deploy targetService=\"" + meta.Hostname + "\" strategy=\"git-push\""
}
