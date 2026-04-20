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

When the code is finished, deploy dev first, verify, then promote to stage:

```
zerops_deploy targetService="{hostname}" setup="dev"
# start the server via a NEW SSH session (the old ones died with the deploy)
zerops_verify serviceHostname="{hostname}"

zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_verify serviceHostname="{stage-hostname}"
```

The cross-deploy packages the dev tree into stage — no second build.
