---
id: develop-first-deploy-promote-stage
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
modes: [standard]
environments: [container]
multiService: aggregate
title: "First-deploy — promote dev to stage"
references-fields: [workflow.ServiceSnapshot.StageHostname]
references-atoms: [develop-auto-close-semantics]
---

### Promote the first deploy to stage

Standard mode pairs dev + stage. After each dev runtime verifies,
cross-deploy it to its paired stage:

```
{services-list:zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_verify serviceHostname="{stage-hostname}"}
```

Cross-deploy builds the dev source on stage; dev side unchanged.
Auto-close fires once both halves carry a successful deploy +
passing verify.
