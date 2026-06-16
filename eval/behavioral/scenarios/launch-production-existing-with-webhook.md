---
id: launch-production-existing-with-webhook
description: |
  Variation of `launch-production-existing-project-token`: the user seeds code
  into an EXISTING production project (entered by supplying `ExistingProjectID`
  + `ExistingProdToken` — input presence, not a project-mode prompt), and the
  source pair declares `BuildIntegration=webhook`, so the DERIVED production
  delivery family is `webhook` (Zerops native dashboard TAG integration) rather
  than the GitHub Actions push track. The delivery family is DERIVED, not picked
  from a method menu.

  Webhook-family launched response:
   1. `firstRelease.deliveryFamily` is `webhook`; the next-steps point at the
      dashboard TAG integration.
   2. Each promoted runtime that has no live integration surfaces a
      `pipeline-not-configured-<hostname>` blocker at `Severity=warn` (P-LP-8 —
      a pipeline-config issue NEVER blocks the `launched` status). The blocker
      carries a dashboard deep-link plus a recommendation payload
      (`repositoryFullName`, `eventType=TAG`, `tagRegex` default
      `^v\d+\.\d+\.\d+$`, `zeropsYamlSetup=prod`).
   3. The `launch-pipeline-configure-dashboard` atom walks the user through:
      open the deep-link → Connect to GitHub (one-time OAuth) → Event type Tag +
      the recommended tag regex + the recommended Zerops YAML setup → Save.
   4. ZCP only READS integration status (`GetServiceStackIntegrationStatus`) —
      it NEVER PUTs the integration config (P-LP-7). The user wires it in the
      dashboard themselves.
   5. After wiring, the user re-calls `launch-production` (resume): the launch-
      window token resolves from the staged `ZCP_LAUNCH_TOKEN` secret (P-LP-14,
      no key re-send), ZCP re-runs the read-only pipeline check and refreshes
      `PipelineConfigurations`; once configured the blocker clears.

  The mandatory `launch-delete-key` atom ("Close the launch window
  (confirm-production)") still appears in the launched response (P-LP-4).
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, existing-project, cicd-webhook, pipeline-configure-dashboard, warn-not-block, single-token, P-LP-7, P-LP-8, P-LP-14]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're an SRE seeding code into an existing production project. Your source
  pair is wired with Zerops's native webhook integration (Connect to GitHub
  OAuth) rather than a GitHub Actions workflow file — fewer moving parts, no
  extra repo secret to rotate, the dashboard shows the wiring at a glance.

  You provide the existing prod project's UUID + a properly project-scoped token
  (same scope gate as the Actions-path existing-project scenario). Because your
  source declares webhook integration, you expect the launched response to
  DERIVE `deliveryFamily=webhook` and walk you through the dashboard TAG
  integration — you are NOT picking a delivery method from a menu. After you
  wire the dashboard integration, you re-call launch to let ZCP refresh the
  pipeline state.

  Expect ZCP to READ the integration status, never to PUT it — the OAuth wiring
  is yours to do in the dashboard. Push back if ZCP tries to railroad you onto
  GitHub Actions just because the repo is on GitHub: your source declares
  webhook, so that is the derived family.
notableFriction:
  - id: derived-webhook-delivery-family
    description: |
      The source declares `BuildIntegration=webhook`, so the launched response
      DERIVES `deliveryFamily=webhook` and emits the dashboard-integration story
      — no delivery-method prompt. Surfaces whether the agent reads the derived
      family and the configure-dashboard atom, or stalls waiting for a method
      choice that does not exist.
  - id: pipeline-not-configured-is-warn
    description: |
      An unconfigured webhook runtime surfaces a `pipeline-not-configured-
      <hostname>` blocker at `Severity=warn` — it NEVER blocks the `launched`
      status (P-LP-8). Surfaces whether the agent treats the warn blocker as
      actionable guidance or mistakes it for a hard failure.
  - id: dashboard-deep-link
    description: |
      The configure-dashboard atom + the warn blocker carry the EXACT deep-link
      to the target runtime's source-code config page (`.../service-stack/
      <svcID>/deploy`), not a generic dashboard URL. Surfaces whether the agent
      relays the deep-link verbatim to the user.
  - id: path-b-read-only
    description: |
      ZCP refreshes pipeline state via `GetServiceStackIntegrationStatus` (READ
      only); it NEVER PUTs the integration config (P-LP-7). Surfaces whether the
      agent lets the user do the dashboard OAuth step rather than trying to wire
      it programmatically.
  - id: resume-reads-staged-token
    description: |
      After dashboard wiring, the user re-calls launch (resume). The launch-
      window token resolves from the staged `ZCP_LAUNCH_TOKEN` secret server-
      side (P-LP-14) — no key re-send. ZCP re-runs the read-only pipeline check
      and clears the blocker once configured. Surfaces whether the agent
      understands resume semantics + the staged-token lifecycle or treats it as
      a fresh launch needing a re-pasted token.
---

I have an existing production Zerops project — UUID I'll provide, plus a project-scoped token for just that project. Seed this dev/stage code into it. My source pair is wired with Zerops's native webhook integration (Connect to GitHub), so production should be delivered that way too, NOT via a GitHub Actions workflow file. Give me the exact dashboard deep-link for the source-code config so I can wire the TAG integration. After I wire it, I'll call launch again so you can verify the pipeline state. Push to a tag triggers production builds via Zerops pull-mode.
