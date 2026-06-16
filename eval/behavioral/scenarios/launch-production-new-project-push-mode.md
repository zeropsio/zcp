---
id: launch-production-new-project-push-mode
description: |
  User is launching their first production project on the NEW-project path
  (a separate Zerops project, not seeding into an existing one). The
  new-project path is entered by INPUT PRESENCE — the launchKey-bearing
  mutation call — not by a project-mode prompt: the state machine starts at
  `scope-prompt`, narrows through `source-control-required` / `classify-prompt`
  to `ready-to-launch`, then the launchKey advances `launching →
  configuring-pipeline → launched`.

  This scenario's source pair declares `BuildIntegration=actions`, so the
  production DELIVERY FAMILY is DERIVED as `actions` — it is NOT chosen via a
  prompt. The `launched` response carries that derivation in the `firstRelease`
  block plus the `prodCd` actions track:
   - production import composes pipeline-first (P-LP-10): each promoted runtime
     entry has `startWithoutCode: true` and carries NO `buildFromGit`. The
     launched runtimes are ACTIVE-empty; the FIRST production build arrives as
     the first release tag through the production pipeline.
   - `prodCd.workflowFile` writes `.github/workflows/zerops-prod.yml`, tag-
     triggered on `v*.*.*`, running raw `zcli push --service-id ... --setup ...`
     (NOT zeropsio/actions@v1).
   - the GitHub repo secret is `ZEROPS_TOKEN_PROD` (P-LP-14): DISTINCT from the
     staged `ZCP_LAUNCH_TOKEN` service secret (the platform forbids a custom
     `ZEROPS_`-prefixed service env). `prodCd.secret.command` conveys the staged
     token secret-to-secret over ssh — no value re-enters the conversation.
   - the mandatory `launch-delete-key` atom ("Close the launch window
     (confirm-production)") always appears in the launched response (P-LP-4).

  Validates that the agent never proposes `buildFromGit` for the prod import,
  reads the derived `actions` delivery family from `firstRelease`/`prodCd`
  rather than expecting a delivery-method prompt, and keeps the staged token
  vs `ZEROPS_TOKEN_PROD` repo secret distinct.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, prod-transition, separate-project, pipeline-first, cicd-actions, push-mode, single-token, P-LP-10, P-LP-14]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're launching your first production environment for a Node service. You
  want a SEPARATE new Zerops project (per Zerops production policy — no zcp@1
  in prod, separate trust boundary). You mint ONE Zerops integration token with
  project-creation permission; you hand it to ZCP exactly once on the launch
  (mutation) call, and ZCP stages it as the production push service's
  `ZCP_LAUNCH_TOKEN` secret for the rest of the launch window. You'll close the
  window (which deletes the staged secret) only after production is verified
  working, via `action="confirm-production" confirmFunctional=true`.

  Your source pair already runs GitHub Actions for the stage pipeline, so you
  expect production to mirror that pattern: a tag-triggered workflow that runs
  `zcli push` on `git tag v0.1.0 && git push --tags`. You do NOT want to paste
  the prod token a second time — ZCP should convey the staged token into the
  `ZEROPS_TOKEN_PROD` GitHub repo secret itself (secret-to-secret over ssh).
  Push back if ZCP asks you to pick a delivery method from a menu — your source
  declares Actions integration, so production delivery should derive from that.
notableFriction:
  - id: input-presence-selects-new-path
    description: |
      The new-project path is entered by the launchKey-bearing mutation call,
      not a project-mode prompt. The machine starts at `scope-prompt` and
      narrows read-side (no key) through `source-control-required` /
      `classify-prompt` to `ready-to-launch`. Surfaces whether the agent walks
      the narrowing calls or expects an up-front "New vs Existing" elicitation
      that does not exist.
  - id: pipeline-first-shape
    description: |
      The composed prod import's runtime entries must have `startWithoutCode:
      true` and NO `buildFromGit` (P-LP-10). Surfaces whether the agent
      accepts the empty-runtime pipeline-first shape or wrongly expects the
      dev/stage `buildFromGit` shape and treats the empty containers as a bug.
  - id: derived-actions-delivery-family
    description: |
      The source declares `BuildIntegration=actions`, so the launched response
      DERIVES `deliveryFamily=actions` in `firstRelease` and emits the `prodCd`
      actions track — no delivery-method prompt is presented. Surfaces whether
      the agent reads the derived family + `prodCd` steps or stalls waiting for
      a choice menu.
  - id: distinct-secret-names
    description: |
      The staged service secret is `ZCP_LAUNCH_TOKEN`; the GitHub repo secret
      the prod CI reads is `ZEROPS_TOKEN_PROD` — distinct by design (the
      platform rejects a `ZEROPS_`-prefixed custom service env). Surfaces
      whether the agent conflates the two or runs `prodCd.secret.command` to
      convey one into the other without re-pasting a value.
  - id: single-token-no-repaste
    description: |
      The launch token crosses the conversation ONCE (the launchKey on the
      mutation call). Every later window op (prod-ops, pipeline re-check) reads
      the staged `ZCP_LAUNCH_TOKEN`; the `prodCd.secret.command` reads it over
      ssh too. Surfaces whether the agent re-asks the user to paste the token
      for the repo secret or understands secret-to-secret conveyance.
  - id: window-close-atom-present
    description: |
      The `launched` response ALWAYS carries the `launch-delete-key` atom
      ("Close the launch window (confirm-production)" — P-LP-4). The window
      closes at `action="confirm-production" confirmFunctional=true`, which
      deletes the staged secret. Surfaces whether the agent surfaces the
      close-window step or buries it.
---

Dev and stage are working — I want to launch production now, as a SEPARATE new Zerops project called `myapp-prod` in eu-central. I'll mint one Zerops integration token with project-creation permission and hand it to you once for the launch. My source repo already uses GitHub Actions for the stage pipeline; I want production delivered the same way — a tag-triggered workflow so `git tag v0.1.0 && git push --tags` builds production. Don't make me paste the prod token a second time for the repo secret — wire it for me. Tell me how to close the launch window once production is verified.
