---
id: develop-push-dev-deploy-local
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [push-dev]
environments: [local]
title: "Push-dev strategy — deploy via zerops_deploy"
---

### Push-Dev Deploy Strategy

`zerops_deploy` runs `zcli push` from your working directory into the
linked Zerops stage. Requires `zerops.yaml` at the repo root and `zcli`
on PATH. No sourceService: local env deploys whatever is in CWD (or the
path passed as `workingDir`) — there's no dev container to cross-deploy
from.

```
zerops_deploy targetService="{stage-hostname}"
```

The deploy blocks on the Zerops build; expect 60–120 s for dynamic
runtimes.
