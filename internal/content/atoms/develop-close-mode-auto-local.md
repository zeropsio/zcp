---
id: develop-close-mode-auto-local
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev, stage, local-stage]
closeDeployModes: [auto]
environments: [local]
multiService: aggregate
title: "Close task — close-mode=auto"
coverageExempt: "local-mode close-mode-auto — 30 canonical scenarios are container-focused; covered by Phase 5 quarterly live-eval"
---

### Closing the task

Local mode builds from your committed tree — no SSHFS, no dev container. Every code change → `zerops_deploy`:

```
{services-list:zerops_deploy targetService="{hostname}"
zerops_verify serviceHostname="{hostname}"}
```

For local+standard the targeted hostname is the stage service — there is no separate dev container to cross-deploy from, so a single deploy+verify per service covers the close.
