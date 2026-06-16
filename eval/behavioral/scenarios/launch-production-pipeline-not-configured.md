---
id: launch-production-pipeline-not-configured
description: |
  Continuation of a new-project launch from a working standard pair: the
  launch succeeded (the launchKey advanced launching → configuring-pipeline
  → launched), the read-only pipeline check returned no live integration for
  the prod runtime, and the launched response carried a
  pipeline-not-configured-<hostname> blocker at Severity=warn (P-LP-8 — a
  pipeline-config issue NEVER blocks the `launched` status) with a Zerops
  dashboard deep-link and recommendation payload.

  Tests whether the agent surfaces the warn blocker clearly (without
  pretending it failed the launch) and walks the user through the dashboard
  configuration with the right values: eventType=TAG,
  tagRegex=^v\d+\.\d+\.\d+$, zeropsYamlSetup=prod. Then verifies the agent
  RESUMES the launch (re-call launch-production) to refresh the pipeline
  state — the launch-window token resolves STAGE-FIRST from the staged
  `ZCP_LAUNCH_TOKEN` secret (P-LP-14, single-token lifecycle), so resume
  needs NO re-supplied key. ZCP re-runs the read-only pipeline check and
  clears the blocker once the integration is live.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, pipeline-extension, pipeline-configure-dashboard, warn-not-block, tag-trigger, single-token, P-LP-8, P-LP-14]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You launched production successfully. ZCP's launched response said the
  prod runtime has no CD pipeline configured (a warn-severity blocker, not a
  failure) and gave you a Zerops dashboard deep-link plus recommended values
  (repo, tag-trigger regex, setup block name). You're willing to configure
  via dashboard but expect ZCP to walk you through the specific values to
  use, not just "go set it up".

  After you tell ZCP you've wired the dashboard integration, ZCP should
  RESUME its own launch workflow to refresh the state and confirm. You handed
  ZCP the launch token ONCE on the launch call; it is staged as the push
  service's `ZCP_LAUNCH_TOKEN` secret for the rest of the window, so resume
  reads it server-side. Push back if ZCP asks you to re-paste the launch key
  to resume — the single-token model means the staged secret is the source of
  truth, not a re-pasted value.
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
  - id: resume-reads-staged-token
    description: |
      Resuming launch-production after dashboard config reads the launch
      token STAGE-FIRST from the staged `ZCP_LAUNCH_TOKEN` secret (P-LP-14)
      — the agent does NOT re-supply the launchKey. Surfaces whether the
      agent understands the staged-token resume lifecycle or wrongly asks
      the user to re-paste the one-shot key mid-window.
---

Production launched and ZCP says the runtime "has no CD pipeline integration" — there's a deep-link in the response. Walk me through configuring it in the Zerops dashboard with the recommended values, then re-check that it's wired correctly. I already handed you the launch token on the launch call — don't make me paste it again to re-check.
