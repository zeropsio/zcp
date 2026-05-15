---
id: launch-production-existing-with-webhook
description: |
  Variation of `launch-production-existing-project-token`: user picks
  the EXISTING-project path AND prefers Zerops native webhook (OAuth-
  based pull-mode) over GitHub Actions push-mode for production CI/CD.

  After launch succeeds:
   1. Terminal phase emits cicd method prompt (Actions / Webhook / None).
   2. User picks Webhook.
   3. ZCP emits `launch-pipeline-configure-dashboard.md` atom with a
      deep-link to the target (existing-prod) project's service-stack
      source-code page.
   4. User clicks through Zerops dashboard: Connect to GitHub →
      authorize org-level Zerops OAuth (one-time) → pick Event type:
      Tag + tag regex `^v\\d+\\.\\d+\\.\\d+$` + Zerops yaml setup `stage`.
   5. User re-calls launch with same launchKey (within window); ZCP
      refreshes `pipelineConfigurations` via Path B verify
      (read-only `GetServiceStackIntegrationStatus` — P-LP-7 stays).
   6. Path B success → terminal response shows configured state.

  Validates Path B (webhook) stays alive as a first-class alternative
  alongside Actions push-mode (§3.6, §4.6). Atom corpus keeps
  `launch-pipeline-configure-dashboard.md` etc.; ZCP only reads, never
  PUTs to integration endpoint.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, project-mode-existing, cicd-webhook, path-b, P-LP-7, P-LP-8, P-LP-9]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're an SRE seeding code into an existing production project. You
  prefer Zerops's native webhook (Connect to GitHub OAuth) over
  managing a GitHub Actions workflow file — fewer moving parts, no
  extra Token secret to rotate, dashboard shows wiring at a glance.

  Provide the existing prod project's UUID + a properly scoped token
  (same gate as the Actions-path scenario). After launch, pick Webhook
  on the method prompt. Walk through the dashboard OAuth flow, then
  re-call launch with the launchKey to refresh state.

  Push back if ZCP tries to railroad you to Actions just because the
  repo is on GitHub — Webhook is a first-class alternative, not a
  fallback.
notableFriction:
  - id: cicd-method-prompt-webhook
    description: |
      Method prompt presents both Actions and Webhook as first-class
      options. Surfaces whether the agent reads "Webhook" as a real
      choice or treats it as "for GitLab only" (it works for GitHub too).
  - id: dashboard-deep-link
    description: |
      The webhook atom must include the EXACT URL for the target
      project's service-stack source-code config page (deep-link). Not
      a generic dashboard URL. Surfaces whether the agent surfaces the
      URL to the user verbatim.
  - id: path-b-verify-read-only
    description: |
      ZCP refreshes pipeline state via GetServiceStackIntegrationStatus
      (READ). Never PUTs to the integration endpoint (P-LP-7). User
      clicks dashboard themselves — ZCP doesn't try to skip the manual
      OAuth step.
  - id: launched-resume-with-key
    description: |
      After dashboard wiring, user re-calls launch with the SAME
      launchKey (within 5-15 min window). ZCP runs Path B verify and
      updates `pipelineConfigurations`. Surfaces whether the agent
      understands resume semantics or treats it as a fresh launch.
  - id: pipeline-failure-is-warn-not-block
    description: |
      If Path B verify returns IntegrationNotConfigured even after
      dashboard wiring (user got the event-type wrong, say), the
      response carries WARN severity blockers, never blocks `launched`
      (P-LP-8). Surfaces whether the agent treats WARN as actionable
      or pretends it's a hard fail.
---

I have an existing production project (UUID provided). Seed the code in. After launch, I want to use Zerops native webhook for CI/CD, NOT GitHub Actions. Walk me through the dashboard OAuth flow — give me the exact source-code config URL. After I wire it up, I'll call launch again with the same launchKey so you can verify the pipeline state. Push to main triggers production builds via Zerops pull-mode.
