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

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), a single integration token with project-creation permission staged as a service secret for the launch window and never persisted — either minted by ZCP itself from a one-time platform delegation on the user's confirmation, or supplied once by the user as a fallback — and a post-launch checklist (first release, window close via confirm-production, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (a stage-half hostname is accepted too — the handler normalizes it to the dev half). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.
