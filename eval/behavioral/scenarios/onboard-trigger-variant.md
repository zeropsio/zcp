---
id: onboard-trigger-variant
description: |
  Clear meta-onboarding variant in an empty project. Pins the trigger semantics
  from docs/spec-onboarding.md §§1–3: the user need not repeat the exact kickoff
  phrase when the request is plainly about learning Zerops itself.

  Surfaces the agent must navigate:

   1. Variant trigger — "get me started with Zerops, I'm new here" fetches
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once and
      follows it.
   2. Status-then-discover state read — the agent calls
      `zerops_workflow action="status"` before `zerops_discover`, and uses
      discover to classify this empty project as FRESH.
   3. Exact FRESH fork — the opening includes **Bring an app**, **Start
      something new**, and **Take a quick tour**, plus "Or tell me the outcome
      you want."
   4. Consent before provisioning — before the user chooses a direction, the
      agent MUST NOT call `zerops_workflow action="start"`, `zerops_import`, or
      `zerops_deploy`. The variant request is no more permission to mutate than
      the exact phrase.
seed: empty
tags: [onboarding, trigger-variant, fresh, conversation-router, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: semantic-trigger-not-exact-string-only
    description: |
      A clear request to get started with Zerops is the positive trigger even
      without the exact phrase. Missing it exposes an overly literal router.
  - id: variant-keeps-state-aware-opening
    description: |
      Semantic trigger matching must still fetch the playbook and preserve its
      `zerops_workflow action="status"` then `zerops_discover` state sequence.
  - id: variant-keeps-consent-boundary
    description: |
      The agent presents all three exact option labels and waits. No bootstrap
      commit, import, or deploy call occurs before the user chooses.
---

get me started with Zerops, I'm new here
