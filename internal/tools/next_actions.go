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
			"CRITICAL: Deploy created a new container — ALL previous SSH sessions to %s are dead (exit 255). "+
				"Dev server is NOT running (idle start: zsc noop). "+
				"Open a NEW SSH connection and start the server (Bash run_in_background=true): "+
				"ssh %s \"cd /var/www && {start_command}\". "+
				"Check TaskOutput after 3-5s. Then: zerops_verify.",
			result.TargetService, result.TargetService,
		)
	}
	return nextActionDeploySuccess
}

// deploySuggestionForStatus returns a phase-aware suggestion for a non-ACTIVE
// deploy status. The agent needs to know WHICH phase failed AND where to find
// the actual stderr:
//   - BUILD_FAILED: buildCommands in build container → logs in deploy response buildLogs
//   - DEPLOY_FAILED: run.initCommands in runtime container → stderr in runtime logs, NOT buildLogs
//   - PREPARING_RUNTIME_FAILED: run.prepareCommands in runtime prep phase → check both
func deploySuggestionForStatus(status string, hasLogs bool) string {
	switch status {
	case statusBuildFailed:
		if hasLogs {
			return "BUILD phase failed — buildCommands exited non-zero. See buildLogs for build container output. Fix buildCommands in zerops.yaml and redeploy."
		}
		return "BUILD phase failed — build logs unavailable. Check zerops.yaml buildCommands syntax, package manifests, and dependencies."
	case statusDeployFailed:
		return "DEPLOY phase failed — build succeeded, but a run.initCommand crashed the new container on startup. The deploy response's 'error' field identifies the exact failing command. For the actual stderr output, fetch runtime logs: zerops_logs serviceHostname={service} severity=ERROR since=5m. The buildLogs field does NOT contain this error (it's build container output). Common causes: initCommand references /build/source paths baked into build-time caches (move cache commands like artisan config:cache from buildCommands to run.initCommands), DB connection issues during migration, missing env vars at container start."
	case statusPreparingRuntimeFailed:
		if hasLogs {
			return "RUNTIME PREPARE failed — run.prepareCommands exited non-zero before deploy files arrived. See buildLogs for stderr. Common cause: prepareCommands referencing /var/www/ paths (empty during prepare — use addToRunPrepare + /home/zerops/ instead)."
		}
		return "RUNTIME PREPARE failed — run.prepareCommands exited non-zero. Logs unavailable; fetch via zerops_logs serviceHostname={service} severity=ERROR since=5m."
	case statusCanceled:
		return "Deploy was canceled. Re-run zerops_deploy."
	}
	return fmt.Sprintf("Deploy ended with status %s — see buildLogs for output.", status)
}

// deployNextActionForStatus returns the next-action for a non-ACTIVE status.
func deployNextActionForStatus(status string) string {
	switch status {
	case statusDeployFailed:
		return "DEPLOY failed — fix run.initCommands in zerops.yaml (NOT buildCommands). Fetch runtime stderr: zerops_logs serviceHostname={target} severity=ERROR since=5m. If the error mentions /build/source paths, a build-time cache (e.g. config:cache) baked build-container paths into the runtime — move that cache command to run.initCommands."
	case statusPreparingRuntimeFailed:
		return "RUNTIME PREPARE failed — fix run.prepareCommands in zerops.yaml (NOT buildCommands, NOT initCommands). prepareCommands run BEFORE deploy files arrive at /var/www — use addToRunPrepare to ship needed files to /home/zerops/."
	case statusBuildFailed:
		return nextActionDeployBuildFail
	}
	return nextActionDeployBuildFail
}
