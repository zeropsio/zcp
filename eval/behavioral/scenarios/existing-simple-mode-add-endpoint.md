---
id: existing-simple-mode-add-endpoint
description: |
  Existing simple-mode Python service deployed externally. The agent
  must adopt the runtime, recognise simple mode (single container, no
  stage), and add a new endpoint then re-deploy as a self-deploy
  (DM-2). Counterpart to existing-standard-appdev-only-reminders
  (which tests adopt-into-standard-pair scoped to dev). Tests
  adopt-into-simple, simple-mode self-deploy, and the absence of a
  stage-promote question.
seed: deployed
fixture: fixtures/python-simple-deployed.yaml
tags: [adopt, simple-mode, self-deploy, develop, python, no-stage]
area: adopt-and-develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Your single Python service `api` is running on Zerops in simple
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

The `api` Python service on Zerops is up and running. Add a `GET /version` endpoint that returns the current build SHA as JSON, then deploy and verify it. Keep it as one container — no staging slot.
