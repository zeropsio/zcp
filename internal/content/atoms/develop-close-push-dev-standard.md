---
id: develop-close-push-dev-standard
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [standard]
strategies: [push-dev]
environments: [container]
title: "Close task — push-dev standard mode"
references-atoms: [develop-first-deploy-promote-stage, develop-auto-close-semantics, develop-dynamic-runtime-start-container]
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

Cross-deploy details (no second build, stage auto-starts via its
`healthCheck`): see `develop-first-deploy-promote-stage` and
`develop-auto-close-semantics`. If the dev server is already
running after a code-only change, run `action=status` first; if
`running: true`, skip `action=start` and go straight to
`zerops_verify`.
