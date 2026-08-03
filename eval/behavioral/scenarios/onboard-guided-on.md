---
id: onboard-guided-on
description: |
  Exact onboarding trigger with guided mode enabled locally after the harness's
  plain init. The preseed script runs `zcp init --guided`; guided is deliberately
  not a scenario field, and the tag below is descriptive only. Pins the
  onboarding-first handoff and the recipe branch's staged consent from
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
   3. Recipe branch runs the staged consent regardless of guided — the persona
      chooses **Try a ready-made recipe**. The agent walks the full sequence:
      footprint consent (name dev + stage + database, get a yes) → commit
      (`route="recipe"`) → renewed consent on EXISTS when applicable → narrate
      the three concepts, THEN the blocking `zerops_import`. Guided being on
      does not skip, reorder, or shortcut any step.
   4. Consent before provisioning — before the footprint yes, the agent MUST
      NOT call `zerops_workflow action="start"`, `zerops_import`, or
      `zerops_deploy`.
seed: empty
preseedScript: preseed/onboard-guided-on.sh
tags: [onboarding, trigger-positive, fresh, guided-on, staged-consent, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are new to Zerops and want the onboarding conversation before doing any
  work; guided mode is already on locally, though you don't know that. When
  the agent first presents the onboarding fork, choose exactly:
  "Try a ready-made recipe." If asked which language, say "Node.js, no
  strong preference."

  When the agent names what it will create (a dev service, a stage service,
  and a database if the recipe has one) and asks for a yes, say yes. If the
  agent later says an existing managed dependency will receive the recipe's
  data and asks for a fresh yes before writing into it, say yes again. Accept
  the staged consent sequence as the agent walks you through it. If the agent
  asks build-idea questions or tries to skip past the onboarding fork before
  it's shown, push back: "Show me the onboarding choices first." Never choose
  **Build something** or **What are Zerops & ZCP?** in this scenario.
notableFriction:
  - id: onboarding-precedes-guided
    description: |
      Guided is already enabled, but the bare phrase carries no product intent.
      The exact three-option onboarding fork must appear before guided asks
      build questions or owns the request.
  - id: recipe-branch-runs-full-staged-consent
    description: |
      Choosing **Try a ready-made recipe** is a provisioning act, not a build
      request — guided being on must not shortcut the staged consent order
      (footprint yes, then commit, then EXISTS disclosure when applicable,
      then narrate-then-import). It runs the same with guided on or off.
  - id: guided-opening-keeps-consent-boundary
    description: |
      Before the footprint yes, the opening performs no state read and there
      is no bootstrap commit, import, or deploy call.
---

onboard me to Zerops
