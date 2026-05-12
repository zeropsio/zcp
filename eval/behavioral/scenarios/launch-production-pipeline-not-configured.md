---
id: launch-production-pipeline-not-configured
description: |
  Continuation of launch-production-from-standard-pair: the launch
  succeeded, ZCP entered configuring-pipeline status, GetStatus returned
  IntegrationNotConfigured for the prod runtime, and the launched
  response carried a pipeline-not-configured-<hostname> blocker with a
  Zerops dashboard deep-link and recommendation payload.

  Tests whether the agent surfaces the blocker clearly (without
  pretending it failed the launch) and walks the user through the
  dashboard configuration with the right values: eventType=TAG,
  tagRegex=^v\d+\.\d+\.\d+$, zeropsYamlSetup=prod. Then verifies the
  agent re-calls launch-production with the same launchKey to refresh
  the pipeline state.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, pipeline-extension, path-b, dashboard-driven, tag-trigger, trust-boundary]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You launched production successfully. ZCP's launched response said the
  runtime has no CD pipeline configured and gave you a Zerops dashboard
  deep-link plus recommended values (repo, tag-trigger regex, setup
  block name). You're willing to configure via dashboard but expect ZCP
  to walk you through the specific values to use, not just "go set it
  up". After you tell ZCP you've configured it, ZCP should re-call its
  own workflow to refresh the state and confirm.
notableFriction:
  - id: blocker-readability
    description: |
      The pipeline blocker carries deepLink + recommendation in JSON.
      Surfaces whether the agent extracts the recommendation values
      and presents them as a clear copy-into-dashboard list instead
      of dumping the raw blocker payload.
  - id: launched-not-failed
    description: |
      Pipeline-not-configured is a warn-severity blocker on launched
      status, not a failure (P-LP-8). Surfaces whether the agent
      treats the launch as succeeded and the pipeline as a follow-up,
      or panics and tries to retry the whole launch.
  - id: tag-trigger-default
    description: |
      Default tag-regex is ^v\d+\.\d+\.\d+$ per Zerops production
      checklist. Surfaces whether the agent surfaces this default
      verbatim or invents its own.
  - id: same-launchkey-refresh
    description: |
      Re-running launch-production after dashboard config uses the
      SAME launchKey. Surfaces whether the agent reuses the key or
      asks for a new one (the latter breaks the one-shot model
      mid-window).
---

Production launched and ZCP says the runtime "has no CD pipeline integration" — there's a deep-link in the response. Walk me through configuring it in the Zerops dashboard with the recommended values, then re-check that it's wired correctly. Same `launchKey` as the launch call.
