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

**Dev services (`zsc noop`)** do not auto-start after any deploy
(manual, push-dev, push-git). Start via `zerops_dev_server` in
container env, or your harness background-task primitive in local
env:

```
# container env
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"

# local env — runs on your machine
Bash run_in_background=true command="{start-command}"
```

**Stage / simple services:** auto-start with `healthCheck`; no
`zerops_dev_server` call.

**Code-only changes:** for dev services, `zerops_dev_server
action=restart` — no redeploy needed.

**Switch to guided deploys:** `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
