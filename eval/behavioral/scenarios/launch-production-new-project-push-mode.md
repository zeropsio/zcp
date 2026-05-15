---
id: launch-production-new-project-push-mode
description: |
  User is launching their first production project (NEW Zerops project,
  not adding to existing). The redesigned launch flow surfaces a Q1
  project-mode prompt at the very start (`awaiting-project-mode-choice`)
  before scope-prompt — agent must relay the choice to the user and
  re-call with `LaunchProjectMode=New`.

  After launch succeeds, the terminal phase emits a cicd method prompt
  (Actions / Webhook / None). Agent picks Actions per user preference.
  Composer shape (production push-mode, P-LP-11):
   - runtime entry has `startWithoutCode: true` and NO `buildFromGit`
   - workflow YAML at `.github/workflows/zerops-prod.yml` with `zcli push`
     on tag push `v*.*.*`, raw `zcli` (NOT zeropsio/actions@v1.0.2)
   - secret name `ZEROPS_TOKEN_PROD` (distinct from `ZEROPS_TOKEN_STAGE`
     for the source stage cicd)
   - generate-prod-token + actions-handoff atoms appear BEFORE delete-key
     atom in the terminal response (P-LP-10).

  Validates that the agent never silently assumes mode=New (the Q1
  prompt is real elicitation, not a stub), never proposes
  `buildFromGit` for prod, and respects the distinct secret-name
  convention.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, prod-transition, project-mode-new, cicd-actions, push-mode, distinct-secrets, P-LP-10, P-LP-11]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're launching your first production environment for a Node service.
  You want a SEPARATE new Zerops project (per Zerops production policy —
  no zcp@1 in prod, separate trust boundary). You'll generate a one-shot
  account-wide Zerops launch key, use it for the launch window only, and
  delete it after.

  After launch, you want auto-deploy on tag push (`git tag v0.1.0 && git
  push --tags` should trigger production build). You prefer GitHub
  Actions push-mode over the dashboard webhook flow — your team already
  runs Actions for the stage pipeline, prod should mirror that pattern.
  You'll generate a fresh project-scoped Zerops token from the new prod
  project's dashboard for the `ZEROPS_TOKEN_PROD` secret.
notableFriction:
  - id: project-mode-q1-prompt
    description: |
      First MCP call returns status `awaiting-project-mode-choice` with
      structured options [New, Existing]. Surfaces whether the agent
      relays the prompt to the user verbatim or silently defaults to
      New. Plan §6.2 — "ZCP emits and waits, never silently assumes."
  - id: cicd-method-prompt-terminal
    description: |
      Terminal phase emits a method prompt (Actions / Webhook / None).
      Surfaces whether the agent reads the structured options array,
      relays choice to user, and re-calls with CICDMethod=Actions.
      No silent fallback to Actions just because GitHub repo detected.
  - id: push-mode-shape
    description: |
      Generated import yaml's prod runtime entry must have
      `startWithoutCode:true` and NO `buildFromGit`. Surfaces whether
      ZCP composer emits prod-shape correctly or accidentally inherits
      the dev/stage shape. P-LP-11 invariant.
  - id: distinct-secret-names
    description: |
      Stage cicd uses ZEROPS_TOKEN_STAGE; prod uses ZEROPS_TOKEN_PROD.
      Both can coexist in same repo without overwriting each other.
      Surfaces whether terminal-phase composer emits the right name.
  - id: atom-ordering-p-lp-10
    description: |
      In Actions-path terminal response, launch-generate-prod-token
      and launch-cicd-actions-handoff appear BEFORE launch-delete-key.
      Otherwise: user revokes LaunchKey before generating the prod
      token / setting secret, leaving production without working CI.
  - id: trust-boundary-clarity
    description: |
      ZCP must explicitly request a one-shot LaunchKey and explicitly
      tell the user to delete it after launch completes. The
      ZEROPS_TOKEN_PROD value is a DIFFERENT credential — a fresh
      project-scoped token generated on the prod project's dashboard.
      Surfaces whether the agent distinguishes the two credentials.
---

I'm ready to launch production for this Node service. Create a new separate Zerops project called `myapp-prod` in eu-central. I'll generate a one-shot launch key when you ask. After launch, set up CI/CD via GitHub Actions push-mode — I want auto-deploy on tag push `v*.*.*` (matching how my stage CI already works). I'll paste in a fresh prod-project token for the `ZEROPS_TOKEN_PROD` secret.
