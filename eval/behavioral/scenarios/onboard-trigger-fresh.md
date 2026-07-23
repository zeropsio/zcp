---
id: onboard-trigger-fresh
description: |
  Exact onboarding trigger in an empty project. Pins the user-only conversation
  router from docs/spec-onboarding.md §§1–3 against the shipped onboarding
  playbook rather than a paraphrased expectation.

  Surfaces the agent must navigate:

   1. Exact trigger — "onboard me to Zerops" enters onboarding and fetches
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once.
   2. Status-then-discover state read — after the playbook fetch, the agent calls
      `zerops_workflow action="status"` first, then `zerops_discover`, and
      classifies this empty project as FRESH from discover.
   3. Exact FRESH fork — the opening includes **Bring an app**, **Start
      something new**, and **Take a quick tour**, plus "Or tell me the outcome
      you want."
   4. Consent before provisioning — before the user chooses a direction, the
      agent MUST NOT call `zerops_workflow action="start"`, `zerops_import`, or
      `zerops_deploy`. The bare phrase never commits bootstrap, imports, or
      deploys anything.
seed: empty
tags: [onboarding, trigger-positive, fresh, conversation-router, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: exact-trigger-fetches-playbook
    description: |
      The exact phrase must route through
      `zerops_knowledge uri="zerops://playbooks/onboarding"` once. Reconstructing
      a generic welcome without fetching the playbook is a failure.
  - id: fresh-state-resolution-order
    description: |
      The playbook requires `zerops_workflow action="status"` before
      `zerops_discover`; only discover classifies the project as FRESH.
  - id: fresh-fork-before-mutation
    description: |
      The response must contain all three exact option labels before any
      bootstrap commit, import, or deploy call. Read-only inspection is the
      only work allowed before the person chooses.
---

onboard me to Zerops
