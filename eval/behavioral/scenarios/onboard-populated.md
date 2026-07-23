---
id: onboard-populated
description: |
  Exact onboarding trigger against an existing deployed Node.js standard pair
  and PostgreSQL dependency. Reuses the neighboring
  `fixtures/nodejs-standard-deployed.yaml` fixture so discover has real
  non-system rows and the opening is observably POPULATED. Oracle:
  docs/spec-onboarding.md §§1–3.

  Surfaces the agent must navigate:

   1. Exact trigger — fetch
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once.
   2. Status-then-discover state read — call
      `zerops_workflow action="status"` first, then `zerops_discover`; classify
      POPULATED from discover, never from status's Services line.
   3. POPULATED opening — lead with one compact found-services summary, then
      prepend **Continue this project** to **Bring an app**, **Start something
      new**, and **Take a quick tour**. Keep "Or tell me the outcome you want."
   4. Consent before provisioning — before the user chooses a direction, the
      agent MUST NOT call `zerops_workflow action="start"`, `zerops_import`, or
      `zerops_deploy`. Existing services do not weaken the consent boundary.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [onboarding, trigger-positive, populated, deployed-fixture, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: populated-classification-comes-from-discover
    description: |
      The deployed fixture yields non-system service rows. The agent must read
      status first but classify POPULATED only from the subsequent direct
      `zerops_discover` result.
  - id: populated-opening-adds-continue
    description: |
      A compact service summary and **Continue this project** must precede the
      same three exact FRESH labels; dropping or renaming a fork literal loses
      the shipped conversation contract.
  - id: populated-opening-still-read-only
    description: |
      The existing deployment is context, not permission. No bootstrap commit,
      import, or deploy call occurs before the user chooses.
---

onboard me to Zerops
