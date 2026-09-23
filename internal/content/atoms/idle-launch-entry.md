---
id: idle-launch-entry
priority: 2
phases: [idle]
idleScenarios: [bootstrapped]
envelopeDeployStates: [deployed]
title: "Launch production entry"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), what applies depends on whether this Mate is wired to the account's own Gitea (a repository the broker gave its pairs).

Not wired to the account's own Gitea — use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

```
zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
```

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), a single integration token with project-creation permission staged as a service secret for the launch window and never persisted — either minted by ZCP itself from a one-time platform delegation on the user's confirmation, or supplied once by the user as a fallback — and a post-launch checklist (first release, window close via confirm-production, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (a stage-half hostname is accepted too — the handler normalizes it to the dev half). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.

Wired to the account's own Gitea — do not start launch-production: it refuses outright there, since a group's production is not this workflow's to create. It belongs to the group, added by the person from Mate's projects page and fed by release tags through the broker — never a project this workflow creates or a token it stages. Say what is true of the pair's own recorded pull request instead of starting the workflow, and give each state its own words — "nothing open" does NOT mean "merged": an open request ("pull request #N is open on `<repo>` — merge it first") means the code has not reached the group yet; a request the fresh read finds merged is as far as the Mate's part goes — do not claim what `main` now runs; production is added and released from the project on Mate's projects page, whose release offer shows what it would put live; no request at all, or one closed without merging, means nothing of that work has landed either — the pair delivers through its stage half first, same as the open case, before production is added from the projects page. Continue developing through the develop entry for anything short of that.
