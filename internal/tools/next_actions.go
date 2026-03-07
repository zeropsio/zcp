package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
)

// NextActions constants provide actionable follow-up instructions for LLMs.
const (
	nextActionDeploySuccess    = "Enable subdomain: zerops_subdomain action=enable. Check logs: zerops_logs severity=ERROR since=5m."
	nextActionDeployBuildFail  = "Build failed — check buildLogs in response for build output. Fix and redeploy."
	nextActionImportSuccess    = "Verify services: zerops_discover. Continue workflow: mount dev, discover env vars, write code, then deploy."
	nextActionImportPartial    = "Check failed processes: zerops_events. Fix and re-import via zerops_workflow."
	nextActionEnvSetSuccess    = "Reload service: zerops_manage action=reload (~4s, faster than restart)."
	nextActionEnvDeleteSuccess = "Reload service: zerops_manage action=reload (~4s, faster than restart)."
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
				"Check TaskOutput after 3-5s. Then: zerops_subdomain action=enable, zerops_verify.",
			result.TargetService,
		)
	}
	return nextActionDeploySuccess
}
