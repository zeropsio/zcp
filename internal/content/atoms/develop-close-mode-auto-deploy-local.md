---
id: develop-close-mode-auto-deploy-local
priority: 2
phases: [develop-active]
deployStates: [deployed]
closeDeployModes: [auto]
environments: [local]
multiService: aggregate
title: "close-mode=auto — deploy via zerops_deploy"
---

### close-mode=auto Deploy

`zerops_deploy` deploys from your working directory into the linked Zerops stage. `zerops.yaml` placement is covered by `develop-platform-rules-common`. No sourceService: local env deploys whatever is in CWD (or the path passed as `workingDir`) — there's no dev container to cross-deploy from.

```
{services-list:zerops_deploy targetService="{stage-hostname}"}
```

The deploy blocks on the Zerops build; expect 60–120 s for dynamic runtimes.
