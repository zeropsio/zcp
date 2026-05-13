---
id: idle-launch-entry
priority: 2
phases: [idle]
idleScenarios: [bootstrapped]
envelopeDeployStates: [deployed]
title: "Launch production entry"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

```
zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
```

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), one-shot account-wide launch-window token (validated, never persisted), and a post-launch checklist (delete the key, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (stage-half input fires a corrective scope-prompt blocker). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.
