---
id: idle-launch-entry
priority: 2
phases: [idle]
idleScenarios: [bootstrapped]
envelopeDeployStates: [deployed]
title: "Launch production entry"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), what applies depends on whether this Mate keeps its code in HQ (enrolled with it, its pairs given repositories there).

Not keeping its code in HQ — use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

```
zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
```

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), a single integration token with project-creation permission staged as a service secret for the launch window and never persisted — either minted by ZCP itself from a one-time platform delegation on the user's confirmation, or supplied once by the user as a fallback — and a post-launch checklist (first release, window close via confirm-production, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (a stage-half hostname is accepted too — the handler normalizes it to the dev half). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.

Keeping its code in HQ — do not start launch-production: it refuses outright there, since an application's production is not this workflow's to create. It belongs to the application, added by the person with Put it live in Mate — never a project this workflow creates or a token it stages. Say what is true of the pair's own recorded change instead of starting the workflow, and give each state its own words — "nothing open" does NOT mean "merged": an open change ("change #N is open on `<repo>` — merge it first") means the code has not reached `main` yet; a change the fresh read finds merged is as far as the Mate's part goes — do not claim what `main` now runs; production is added with Put it live and released with Release, both offered in the conversation and on the project's card in Mate, whose release offer shows what it would put live; no change at all, or one closed without merging, means nothing of that work has landed either — it goes to `main` first, a branch of your own pushed and merged by the person, before production is added with Put it live. Continue developing through the develop entry for anything short of that.
