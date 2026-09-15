---
id: develop-first-deploy-promote-stage
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
modes: [standard]
environments: [container]
multiService: aggregate
title: "First-deploy — promote dev to stage"
references-fields: [workflow.ServiceSnapshot.StageHostname, ops.DeployResult.SHA, ops.DeployResult.Dirty, ops.DeployResult.AppVersionID, workflow.ServiceSnapshot.Rollback]
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

Commit the dev half over SSH before this cross-deploy — not the mount —
so the stage receives a commit: the response then carries `sha` with
`dirty: false` and an `appVersionId`. Cross-deploying uncommitted work
still succeeds, but the response marks `dirty: true` and that stage
build isn't reproducible from git alone. To ship an exact or older
commit without touching the dev working tree, add `sha="<commit>"` to
the cross-deploy — never on a self-deploy. To roll the stage back
without a rebuild, re-activate a recorded build instead: `zerops_deploy
targetService="{stage-hostname}" appVersion="<id>"`, with candidate ids
from the status envelope's `rollback` block or `zerops_events
serviceHostname="{stage-hostname}"`.
