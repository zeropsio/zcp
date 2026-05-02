---
id: develop-standard-unset-promote-stage
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [standard]
closeDeployModes: [unset]
environments: [container]
multiService: aggregate
title: "Standard pair — promote dev to stage"
references-fields: [workflow.ServiceSnapshot.StageHostname]
references-atoms: [develop-strategy-review, develop-auto-close-semantics]
---

### Promote dev to stage

After each successful `zerops_deploy` + `zerops_verify` on the dev half, cross-deploy the dev tree into the paired stage so the stage's public artifact reflects current code:

```
{services-list:zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_verify serviceHostname="{stage-hostname}"}
```

Cross-deploy packages the dev tree without a second build; stage runs its own `run.start`. Independent of close-mode — close-mode picks the per-mode iteration cadence on the dev side, not whether the stage half stays current. Standard-pair auto-close requires both halves to carry a successful deploy + passing verify and `closeDeployMode ∈ {auto, git-push}`; while `unset`, the session stays open until you commit a delivery pattern.
