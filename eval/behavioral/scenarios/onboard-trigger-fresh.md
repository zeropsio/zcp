---
id: onboard-trigger-fresh
description: |
  Exact onboarding trigger in an empty project. Pins the user-only conversation
  router from docs/spec-onboarding.md §§1–3 against the shipped onboarding
  playbook rather than a paraphrased expectation.

  Surfaces the agent must navigate:

   1. Exact trigger — "onboard me to Zerops" enters onboarding and fetches
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once.
   2. Immediate opening — after the playbook fetch, the agent's FIRST
      user-visible act is the greeting and three-option fork. There is no status
      or discover call before it.
   3. Exact fork — the opening includes **Bring an app**, **Start
      something new**, and **Take a quick tour**, plus "Or tell me the outcome
      you want."
   4. Consent throughout the opening — before the user chooses a direction, the
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
      The first user-visible act after the playbook fetch must be the greeting
      and three-option fork; there is no status or discover call before it.
      State reads move to on demand after the person answers or asks what's here.
  - id: fresh-fork-before-mutation
    description: |
      The response must contain all three exact option labels before any
      bootstrap commit, import, or deploy call. Mutating calls remain forbidden
      throughout the opening.
---

onboard me to Zerops
