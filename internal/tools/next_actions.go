package tools

// NextActions constants provide actionable follow-up instructions for LLMs.
const (
	nextActionDeploySuccess    = "Enable subdomain: zerops_subdomain action=enable. Check logs: zerops_logs severity=ERROR since=5m."
	nextActionDeployBuildFail  = "Check build logs: zerops_logs severity=ERROR. Fix and redeploy."
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
	nextActionSubdomainEnable  = "Test subdomain URL. If 502: zerops_logs severity=ERROR."
)
