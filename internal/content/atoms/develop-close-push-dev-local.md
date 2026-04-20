---
id: develop-close-push-dev-local
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev, stage]
strategies: [push-dev]
environments: [local]
title: "Close task — push-dev local"
---

### Closing the task (local)

Local mode builds from your committed tree — no SSHFS, no dev container.
Deploy and verify from your checkout:

```
zerops_deploy targetService="{hostname}"
zerops_verify serviceHostname="{hostname}"
```

For local+standard, `{hostname}` is the stage service — there is no separate
dev container to cross-deploy from, so a single deploy+verify covers the close.
