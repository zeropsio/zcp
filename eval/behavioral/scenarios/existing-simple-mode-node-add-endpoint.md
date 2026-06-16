---
id: existing-simple-mode-node-add-endpoint
description: |
  Existing simple-mode Node service deployed externally via the prod
  setup of nodejs-hello-world-app. The agent must adopt the runtime,
  recognise simple mode (single container, no stage), and add a new
  endpoint then re-deploy as a self-deploy (DM-2). This is the
  simple-mode adopt coverage scenario.
seed: deployed
fixture: fixtures/nodejs-simple-deployed.yaml
tags: [adopt, simple-mode, self-deploy, develop, node, no-stage]
area: adopt-and-develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Your single Node service `api` is running on Zerops in simple
  mode (one container, no staging). You want to add `GET /version`
  returning a JSON object with the current build SHA, and have it
  deployed and verified. You don't want a staging slot — keep it as
  one immutable runtime. Push back if the agent proposes promoting
  to a dev/stage pair or treats this as a fresh bootstrap.
notableFriction:
  - id: adopt-simple-no-stage
    description: |
      Adopting a simple-mode service should not surface a
      stage-promote question. Surfaces whether the adopt atom
      branches on mode rather than always asking the standard-pair
      stage question.
  - id: simple-mode-self-deploy
    description: |
      Re-deploy of a simple-mode runtime is self-deploy (DM-2). Agent
      must NOT narrow deployFiles below `[.]` for a self-deploy.
      Surfaces whether the deploy atom flags self-deploy on a single
      runtime.
---

The `api` Node service on Zerops is up and running. Add a `GET /version` endpoint that returns the current build SHA as JSON, then deploy and verify it. Keep it as one container — no staging slot.
