---
id: develop-close-push-dev-local
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev, stage]
closeDeployModes: [auto]
environments: [local]
title: "Close task — push-dev"
---

### Closing the task

Local mode builds from your committed tree — no SSHFS, no dev container.
Close through the deploy cadence in `develop-change-drives-deploy`:

```
zerops_deploy targetService="{hostname}"
zerops_verify serviceHostname="{hostname}"
```

For local+standard, `{hostname}` is the stage service — there is no separate
dev container to cross-deploy from, so a single deploy+verify covers the close.
