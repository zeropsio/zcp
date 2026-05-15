---
id: launch-production-dev-only
description: |
  Single dev-mode runtime (no stage half) with one managed dep
  (postgres). Tests the minimal source-state shape for launch-production:
  sourceContext should auto-suggest the only runtime via
  `suggestedRuntime` and ship the dev-half hostname into `targetService`
  without asking. Single managed dep means keepNonHA decision is trivial
  (default: db goes HA in prod).

  Surfaces whether the launch workflow handles the simplest valid source
  shape — one runtime, one dep, no pair-collapse logic — without
  routing the agent through unnecessary prompts.
seed: deployed
fixture: fixtures/dev-only-node-postgres-deployed.yaml
tags: [launch-production, prod-transition, one-shot-key, separate-project, trust-boundary, single-runtime]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're shipping a simple Node API to production for the first time.
  The dev project has one runtime (`api`) and one postgres database
  (`db`) — no stage half, no worker, no object storage. Dev works;
  you want a separate Zerops production project mirroring this shape.

  You will NOT hand ZCP a standing production-scoped API key. When ZCP
  asks, you generate a temporary account-wide Zerops API token, use it
  for the launch window only, and DELETE it as soon as ZCP reports
  launched. Push back if ZCP tries to persist the key anywhere or asks
  for ongoing prod access.
notableFriction:
  - id: trust-boundary-clarity
    description: |
      ZCP must explicitly tell the user a one-shot key is needed and
      explicitly tell them to delete it after launch. Surfaces whether
      the launched response embeds the delete-key step OR buries it
      in checklist content the agent doesn't surface.
  - id: single-runtime-auto-suggestion
    description: |
      sourceContext.suggestedRuntime is populated when source has
      exactly one runtime — the agent should default `targetService`
      to this value without asking. Surfaces whether the agent
      applies the suggestion or unnecessarily prompts the user to
      pick a runtime that has no alternative.
  - id: source-control-mutation-phase
    description: |
      Before the import API call, the launch needs `setup: prod` in
      the source repo's zerops.yaml. Surfaces whether the agent
      drives the write+commit+push step (launch-write-prod-setup
      atom) instead of trying to publish without it.
  - id: scope-classify-narrowing
    description: |
      Launch is stateless multi-call narrowing — scope first, then
      env classification (likely few envs in this minimal shape),
      then ready-to-launch. Surfaces whether the agent walks all
      three calls or short-circuits.
  - id: keepNonHA-defaults
    description: |
      Single managed dep (db). User did not opt out of HA promotion;
      the keepNonHA prompt either skips (only one dep, HA-eligible)
      or surfaces a one-item choice. Surfaces how the UX handles
      the trivial-case keepNonHA question — should be either silent
      default or single-item confirmation, not a four-item menu.
---

I want to launch this to production now. The dev project has just one Node runtime and one postgres database — no stage half, no worker. Create a separate Zerops project called `api-prod` in the eu-central region. I'll generate a one-shot Zerops API key when you ask, and I'll delete it right after. Don't try to keep any standing access to the prod project.
