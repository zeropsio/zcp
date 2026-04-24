---
id: develop-first-deploy-promote-stage
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
modes: [standard]
environments: [container]
title: "First-deploy — promote dev to stage"
references-fields: [workflow.ServiceSnapshot.StageHostname]
---

### Promote the first deploy to stage

Standard mode pairs dev + stage. After `{hostname}` verifies,
cross-deploy to `{stage-hostname}`:

```
zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"
zerops_verify serviceHostname="{stage-hostname}"
```

No second build — cross-deploy packages the dev tree straight into
stage. Auto-close requires BOTH halves verified; see
`develop-auto-close-semantics`. Skipping stage leaves the session
active and blocks auto-close.
