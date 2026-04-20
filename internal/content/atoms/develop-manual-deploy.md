---
id: develop-manual-deploy
priority: 2
phases: [develop-active]
strategies: [manual]
title: "Manual strategy — external deploys"
---

### Manual Deploy Strategy

You control when and what to deploy — the user controls deploy timing.

**Deploy directly:**
- Dev: `zerops_deploy targetService="{hostname}"`
- Stage from dev: `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"`
- Simple: `zerops_deploy targetService="{hostname}"`

**After deploy:**
- Verify health: `zerops_verify serviceHostname="..."`
- Subdomain persists across re-deploys. Check `zerops_discover` for current status and URL.

**Dev services (zsc noop):** Server does not auto-start after deploy. Start manually via SSH.
**Stage/simple services:** Server auto-starts with healthCheck.

**Code-only changes (no zerops.yaml change):** Edit on mount, restart server via SSH. No redeploy needed.

**Switch to guided deploys:** `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
