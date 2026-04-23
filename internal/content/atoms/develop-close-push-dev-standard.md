---
id: develop-close-push-dev-standard
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [standard]
strategies: [push-dev]
environments: [container]
title: "Close task — push-dev standard mode"
---

### Closing the task

Deploy dev first, start the dev server, verify, then promote to stage:

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"

zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_verify serviceHostname="{stage-hostname}"
```

The cross-deploy packages the dev tree into stage — no second build. Stage
has a real `run.start` + `healthCheck`, so the platform auto-starts it;
no `zerops_dev_server` call on the stage side.

If the dev server is already running after a code-only change, skip
`action=start` and go straight to `zerops_verify`. Confirm first via
`zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"`.
