package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

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
// caller break-glassed, the target is pre-ladder (L0 / first deploy), or
// no meta exists (recipe sessions, unadopted targets — other gates own
// those).
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
	if meta.GitPushState != topology.GitPushConfigured || meta.FirstDeployedAt == "" {
		return nil // L0 artifact flow or first-deploy bypass (D2a)
	}

	buildHost, buildSetup := anticipatedBuildTarget(meta)
	pushArgs := map[string]string{
		"targetService": meta.Hostname,
		"strategy":      "git-push",
	}
	return jsonResult(map[string]any{
		"status":        "push-delivery-required",
		"deliveryState": string(topology.DeriveDeliveryState(meta.GitPushState, len(meta.ProdLaunches) > 0)),
		"service":       targetService,
		"pushSource":    meta.Hostname,
		"buildTarget":   buildHost,
		"buildSetup":    buildSetup,
		"message": "This pair delivers via git push (git-push is configured — the repo is the source of truth and push is the terminal act of development). A direct deploy would put code on the container that the repo never sees. Commit your changes, then push: the integration rebuilds " +
			buildHost + " from the repo and ZCP watches the build to completion.",
		"recommended": map[string]any{
			"tool": "zerops_deploy",
			"args": pushArgs,
		},
		"breakGlass": map[string]any{
			"how":  "Re-call this exact deploy with breakGlass=true — reserved for a FUNDAMENTAL reason (git host outage, recovery) per the delivery spec.",
			"note": "Break-glass self-deploy preserves everything (the artifact ships the full tree incl. .git) and the response will flag that the container is now ahead of the repo — push to reconcile as soon as the reason passes.",
		},
	})
}

// repoDeliveryDivergenceWarning is attached to a SUCCESSFUL break-glass
// (or otherwise direct) deploy of an L1 target: the container now carries
// state the repo does not — the reconcile push is the standing next step.
func repoDeliveryDivergenceWarning(stateDir, targetService string) string {
	meta, err := workflow.FindServiceMeta(stateDir, targetService)
	if err != nil || meta == nil {
		return ""
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.FirstDeployedAt == "" {
		return ""
	}
	return "container is now AHEAD of the configured repo (direct deploy on a push-delivering pair) — reconcile with: zerops_deploy targetService=\"" + meta.Hostname + "\" strategy=\"git-push\""
}
