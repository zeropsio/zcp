---
id: develop-close-push-dev-dev
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Close task — push-dev dev mode (no stage)"
---

### Closing the task

Dev mode has no stage pair. Deploy the single runtime container, start the
dev server via `zerops_dev_server`, then verify:

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"
```

The deploy replaces the container — any previously-running dev server is
gone. `zerops_dev_server action=start` spawns the new one detached and
probes the health endpoint before returning. `zerops_verify` then
confirms infrastructure + app.

If the dev server is already running (e.g. after an atomic code-only
edit): `zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"` — if it returns `running: true`, skip straight to `zerops_verify`.
