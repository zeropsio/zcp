---
id: launch-production-pipeline-configured
description: |
  Continuation of launch-production-from-standard-pair where the
  production runtime already has its external-repository-integration
  configured (a prior launch's user completed dashboard setup, or the
  bundle re-uses a service that was integrated separately).

  Tests whether the agent reads the launched response correctly when
  pipelineConfigurations[<hostname>].configured == true: NO pipeline
  blocker should appear, and the post-launch checklist should land
  on the "push a tag to deploy" guidance instead of the configure-
  dashboard guidance.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, pipeline-extension, path-b, happy-path, no-blockers]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You launched production and ZCP reports the runtime is fully wired
  for CD — no blockers, integration is configured for tag-trigger
  builds. You expect ZCP to tell you the deploy mechanism is
  `git tag vX.Y.Z && git push --tags`, not to invent additional setup
  steps that aren't needed.
notableFriction:
  - id: no-false-blockers
    description: |
      Surfaces whether the agent invents pipeline-config friction when
      the launched response actually carries no pipeline blockers
      (over-eager dashboard guidance vs reading state).
  - id: tag-push-instruction
    description: |
      After confirmation, the user wants the literal `git tag vX.Y.Z
      && git push --tags` command surfaced. Surfaces whether the agent
      reads the launch-pipeline-configured atom or paraphrases away
      the concrete command.
---

I just launched production and ZCP says the pipeline is fully configured. How do I deploy a new release?
