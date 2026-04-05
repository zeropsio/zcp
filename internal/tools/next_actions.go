package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
)

// NextActions constants provide actionable follow-up instructions for LLMs.
const (
	nextActionDeploySuccess    = "Check logs: zerops_logs severity=ERROR since=5m."
	nextActionDeployBuildFail  = "Build failed — check buildLogs in response for build output. Fix and redeploy."
	nextActionImportSuccess    = "Verify services: zerops_discover. Continue workflow: mount dev, discover env vars, write code, then deploy."
	nextActionImportPartial    = "Check failed processes: zerops_events. Fix and re-import via zerops_workflow."
	nextActionEnvSetSuccess    = "IMPORTANT: Env var changes require a service restart to take effect (reload is NOT sufficient — runtime processes cache env vars at startup). Restart affected services: zerops_manage action=\"restart\" serviceHostname=\"{service}\". For project-level vars, restart ALL running runtime services."
	nextActionEnvDeleteSuccess = "IMPORTANT: Env var removal requires a service restart to take effect. Restart affected services: zerops_manage action=\"restart\" serviceHostname=\"{service}\". For project-level vars, restart ALL running runtime services."
	nextActionManageStart      = "Verify service is running: zerops_discover."
	nextActionManageStop       = "Service stopped. Start with: zerops_manage action=start."
	nextActionManageRestart    = "Verify health: zerops_logs severity=ERROR since=1m."
	nextActionManageReload     = "Verify health: zerops_logs severity=ERROR since=1m."
	nextActionManageConnect    = "Verify storage mount: zerops_discover."
	nextActionManageDisconnect = "Storage disconnected. Verify: zerops_discover."
	nextActionScaleSuccess     = "Verify scaling: zerops_discover."
	nextActionSubdomainEnable  = "Subdomain active. Verify: zerops_verify."
)

// deploySuccessNextActions returns dev-aware next actions for successful deploys.
// Self-deploy to dynamic runtimes warns that the server is NOT running.
func deploySuccessNextActions(result *ops.DeployResult) string {
	isSelfDeploy := result.SourceService == result.TargetService
	if isSelfDeploy && ops.NeedsManualStart(result.TargetServiceType) {
		return fmt.Sprintf(
			"CRITICAL: Deploy restarted the container — dev server is NOT running. "+
				"Start it via SSH immediately (Bash run_in_background=true): "+
				"ssh %s \"cd /var/www && {start_command}\". "+
				"Check TaskOutput after 3-5s. Then: zerops_verify.",
			result.TargetService,
		)
	}
	return nextActionDeploySuccess
}

// deploySuggestionForStatus returns a phase-aware suggestion for a non-ACTIVE
// deploy status. The agent needs to know WHICH phase failed to look in the
// right place: buildCommands (build container) vs prepareCommands/initCommands
// (runtime container starting up).
func deploySuggestionForStatus(status string, hasLogs bool) string {
	switch status {
	case statusBuildFailed:
		if hasLogs {
			return "BUILD phase failed — buildCommands exited non-zero. See buildLogs for build container output. Fix buildCommands in zerops.yaml and redeploy."
		}
		return "BUILD phase failed — build logs unavailable. Check zerops.yaml buildCommands syntax, package manifests, and dependencies."
	case statusPreparingRuntimeFailed:
		if hasLogs {
			return "RUNTIME startup failed — build succeeded, but run.prepareCommands or run.initCommands failed when the new container tried to start. See buildLogs for the actual stderr (naming is historical: these are runtime logs, not build logs). Common causes: initCommand references /build/source paths baked into build-time caches (move cache commands to run.initCommands), missing env vars, DB connection issues during migration."
		}
		return "RUNTIME startup failed — container couldn't start because run.prepareCommands or run.initCommands failed. Logs unavailable; fetch via zerops_logs serviceHostname={service} severity=ERROR since=5m."
	case statusCanceled:
		return "Deploy was canceled. Re-run zerops_deploy."
	}
	return fmt.Sprintf("Deploy ended with status %s — see buildLogs for output.", status)
}

// deployNextActionForStatus returns the next-action for a non-ACTIVE status.
func deployNextActionForStatus(status string) string {
	switch status {
	case statusPreparingRuntimeFailed:
		return "RUNTIME startup failed — fix run.initCommands or run.prepareCommands in zerops.yaml (NOT buildCommands). If the error mentions /build/source paths, move cache/config commands from buildCommands to run.initCommands."
	case statusBuildFailed:
		return nextActionDeployBuildFail
	}
	return nextActionDeployBuildFail
}
