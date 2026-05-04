---
id: existing-standard-appdev-only-reminders
description: |
  Existing Node.js appdev/appstage pair with Postgres, created outside the
  current ZCP session. The agent must adopt existing runtimes, scope develop
  to appdev only, treat db as a dependency, and avoid stage promotion.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, develop, standard-pair, scoped-dev-only, node, postgres, no-stage-promotion]
area: adopt-and-develop
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: existing-project-adopt
    description: |
      Existing appdev/appstage/db services should lead to adoption before
      code work, not a fresh bootstrap plan that recreates services.
  - id: explicit-appdev-scope
    description: |
      The user names appdev and explicitly excludes stage. Develop scope
      should include appdev only; db is a dependency, not a runtime target.
  - id: no-stage-promotion
    description: |
      Standard mode exposes appstage, but stage deploy/verify is not part
      of completion unless the user asks for promotion.
---

Use `appdev` in the existing Node project and add `GET /api/reminders` returning an empty JSON array for now. Do not change or deploy `appstage`; I only want the dev service verified.
