package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validateDeployStrategyParam gates the strategy parameter to the values
// zerops_deploy actually dispatches on. "manual" is a ServiceMeta
// declaration — it tells ZCP "I handle deploys myself, don't automate" —
// and is meaningless as a tool input. Accepting it would silently do
// something (the default dispatch path), which contradicts the user's
// declared intent. Unknown values are rejected with a concrete list.
//
// "zcli" is the internal Strategy LABEL written into DeployAttempt records
// (deploy_ssh.go records `Strategy: "zcli"` for the default zcli-push
// branch). Atom prose and audit-trail readers see this label and may try
// it as a tool argument. Reject explicitly with the redirect to omit, so
// the agent doesn't have to learn from a generic "Invalid strategy" — they
// get the actual mapping.
//
// Shared between RegisterDeploySSH and RegisterDeployLocal so the error
// is identical in both envs.
func validateDeployStrategyParam(strategy string) error {
	switch strategy {
	case "", deployStrategyGitPush:
		return nil
	case deployStrategyManualLabel:
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("strategy %q is not a zerops_deploy option — it's a ServiceMeta declaration meaning 'ZCP stays out of the deploy loop'", deployStrategyManualLabel),
			fmt.Sprintf("Use zerops_workflow action=\"close-mode\" closeMode={\"<service>\":%q} to mark a service as %s; don't call zerops_deploy on it. Valid deploy strategies: omit (default push) or 'git-push'.", deployStrategyManualLabel, deployStrategyManualLabel),
		)
	case deployStrategyZCLILabel:
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("strategy %q is the internal label recorded into DeployAttempt — not a tool argument", deployStrategyZCLILabel),
			fmt.Sprintf("Omit the strategy parameter to invoke the default zcli push (the path that records Strategy: %q in attempt history). Valid deploy strategies: omit (default push) or 'git-push'.", deployStrategyZCLILabel),
		)
	default:
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid strategy %q", strategy),
			fmt.Sprintf("Valid values: omit (default push) or 'git-push'. Note: %q is the internal label for the default path — to invoke it, omit the strategy parameter.", deployStrategyZCLILabel),
		)
	}
}

// checkLocalOnlyGate rejects the default zcli push against a local-only
// meta (no Zerops runtime linked — there is no deploy target). Returns nil
// in every other situation, including container env (meta exists but has
// a container-native mode) and all git-push calls (git-push doesn't need
// a stage).
//
// The gate reads meta by targetService; missing meta is not this gate's
// concern (requireAdoption already caught that earlier in the handler).
func checkLocalOnlyGate(stateDir, targetService, strategy string) error {
	if strategy == deployStrategyGitPush {
		return nil
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, targetService)
	if meta == nil || meta.Mode != topology.PlanModeLocalOnly {
		return nil
	}
	return platform.NewPlatformError(
		platform.ErrPrerequisiteMissing,
		fmt.Sprintf("project %q is local-only — no Zerops stage linked, nothing to push-deploy to", meta.Hostname),
		fmt.Sprintf(
			"Either link a Zerops runtime as stage:\n"+
				"  zerops_workflow action=\"adopt-local\" targetService=\"<runtime-hostname>\"\n"+
				"or push to an external git remote instead:\n"+
				"  zerops_deploy targetService=%q strategy=\"git-push\"",
			targetService,
		),
	)
}
