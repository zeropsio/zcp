---
id: develop-manual-deploy
priority: 2
phases: [develop-active]
deployStates: [deployed]
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

**Dev services (`zsc noop`):** Server does not auto-start after deploy.
Start via `zerops_dev_server` in container env, or via your harness
background task primitive (e.g. `Bash run_in_background=true`) in
local env:

```
# container env
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"

# local env — runs on your machine
Bash run_in_background=true command="{start-command}"
```

**Stage / simple services:** Server auto-starts with `healthCheck`. No
`zerops_dev_server` call needed.

**Code-only changes (no zerops.yaml change):** Edit on mount, then
`zerops_dev_server action=restart` — no redeploy needed.

**Switch to guided deploys:** `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
