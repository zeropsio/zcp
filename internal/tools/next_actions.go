package tools

// NextActions constants provide actionable follow-up instructions for LLMs.
const (
	nextActionDeploySuccess    = "Enable subdomain: zerops_subdomain action=enable. Check logs: zerops_logs severity=ERROR since=5m."
	nextActionDeployBuildFail  = "Check build logs: zerops_logs severity=ERROR. Fix and redeploy."
	nextActionImportSuccess    = "Verify services: zerops_discover. Deploy code, then enable subdomains: zerops_subdomain action=enable."
	nextActionImportPartial    = "Check failed processes: zerops_events. Fix and re-import."
	nextActionEnvSetSuccess    = "Reload service: zerops_manage action=reload (~4s, faster than restart)."
	nextActionEnvDeleteSuccess = "Reload service: zerops_manage action=reload (~4s, faster than restart)."
	nextActionManageReload     = "Verify health: zerops_logs severity=ERROR since=1m."
	nextActionScaleSuccess     = "Verify scaling: zerops_discover."
	nextActionSubdomainEnable  = "Test subdomain URL. If 502: zerops_logs severity=ERROR."
)
