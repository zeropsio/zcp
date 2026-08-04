---
id: onboard-guided-on
description: |
  Exact onboarding trigger with guided mode enabled locally after the harness's
  plain init. The preseed script runs `zcp init --guided`; guided is deliberately
  not a scenario field, and the tag below is descriptive only. Pins the
  onboarding-first handoff and the recipe branch's consent rule from
  docs/spec-onboarding.md §§1–3 and §6: choosing **Try a ready-made recipe**
  is a provisioning act that runs the same way whether guided is on or off.

  Surfaces the agent must navigate:

   1. Onboarding stays first — fetch
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once, then
      greet and present the fork immediately. The opening performs no state read
      before greeting.
   2. FRESH fork stays intact — present **Build something**,
      **Try a ready-made recipe**, and **What are Zerops & ZCP?**, plus the
      escape line "Or just tell me what you want". Guided must not infer a
      product intent from the bare phrase or skip ahead.
   3. Recipe branch defers to the standard flow — the persona chooses
      **Try a ready-made recipe**. The agent resolves the slug from the
      playbook mapping, runs the standard bootstrap recipe route
      (`route="recipe"`), and follows the workflow guidance each step
      returns. The one onboarding rule on top: before `zerops_import`, tell
      the person what the returned recipe plan will create and get one plain
      yes. Guided being on does not skip or reorder any of it.
   4. Consent boundary — before the person's yes, the agent MUST NOT call
      `zerops_import` or `zerops_deploy`; the only pre-consent bootstrap call
      is the read-only route menu / route commit that returns the plan.
seed: empty
preseedScript: preseed/onboard-guided-on.sh
tags: [onboarding, trigger-positive, fresh, guided-on, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are new to Zerops and want the onboarding conversation before doing any
  work; guided mode is already on locally, though you don't know that. When
  the agent first presents the onboarding fork, choose exactly:
  "Try a ready-made recipe." If asked which language, say "Node.js, no
  strong preference."

  When the agent shows what the recipe plan will create and asks for a yes,
  say yes. If the agent asks build-idea questions or tries to skip past the
  onboarding fork before it's shown, push back: "Show me the onboarding
  choices first." Never choose **Build something** or
  **What are Zerops & ZCP?** in this scenario.
notableFriction:
  - id: onboarding-precedes-guided
    description: |
      Guided is already enabled, but the bare phrase carries no product intent.
      The exact three-option onboarding fork must appear before guided asks
      build questions or owns the request.
  - id: recipe-branch-defers-to-standard-flow
    description: |
      Choosing **Try a ready-made recipe** is a provisioning act, not a build
      request — the agent runs the standard recipe route and follows the
      returned workflow guidance rather than inventing its own choreography;
      the only onboarding addition is one plain yes before the import. It runs
      the same with guided on or off.
  - id: guided-opening-keeps-consent-boundary
    description: |
      Before the person's yes there is no import and no deploy; the opening
      performs no state read before greeting.
---

onboard me to Zerops
