---
id: onboard-trigger-negative
description: |
  Specific-technology negative trigger from docs/spec-onboarding.md §1. The
  request says "get started", but its object is PostgreSQL rather than Zerops
  itself, so normal routing owns it.

  Surfaces the agent must navigate:

   1. NORMAL routing — "help me get started with PostgreSQL" is a concrete
      technology request, not meta-onboarding.
   2. No onboarding playbook — the agent MUST NOT fetch
      `zerops_knowledge uri="zerops://playbooks/onboarding"`.
   3. No onboarding fork — the agent MUST NOT present the onboarding trio
      **Build something**, **Try a ready-made recipe**, and
      **What are Zerops & ZCP?**. It should handle PostgreSQL through the
      normal routing contract.
seed: empty
tags: [onboarding, trigger-negative, normal-routing, postgresql]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: specific-technology-is-not-meta-onboarding
    description: |
      The phrase "get started" alone is insufficient. The concrete PostgreSQL
      object keeps this on normal routing and must not be intercepted by the
      onboarding conversation router.
  - id: negative-excludes-onboarding-surfaces
    description: |
      Neither the onboarding playbook fetch nor its exact three-option fork
      should appear. Other read-only discovery or knowledge calls required by
      normal PostgreSQL routing remain valid.
---

help me get started with PostgreSQL
