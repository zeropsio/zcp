---
id: onboard-guided-on
description: |
  Exact onboarding trigger with guided mode enabled locally after the harness's
  plain init. The preseed script runs `zcp init --guided`; guided is deliberately
  not a scenario field, and the tag below is descriptive only. Pins the
  onboarding-first handoff from docs/spec-onboarding.md §§1–3 and §6.

  Surfaces the agent must navigate:

   1. Onboarding stays first — fetch
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once, then
      greet and present the fork immediately. The opening performs no state read
      before greeting.
   2. FRESH fork stays intact — present **Bring an app**, **Start something
      new**, and **Take a quick tour**, plus "Or tell me the outcome you want."
      Guided must not infer a product intent from the bare phrase or skip ahead.
   3. Deterministic handoff — the simulated user chooses **Start something
      new** and says they want to "build an idea". Only then does the guided
      contract own the build request, exactly once.
   4. Consent before provisioning — before that choice, the agent MUST NOT call
      `zerops_workflow action="start"`, `zerops_import`, or `zerops_deploy`.
seed: empty
preseedScript: preseed/onboard-guided-on.sh
tags: [onboarding, trigger-positive, fresh, guided-on, handoff, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are new to Zerops and want the onboarding conversation before doing any
  work. When the agent first presents the onboarding fork, choose exactly:
  "Start something new — I want to build an idea."

  If the agent asks whether you want a ready-made demo or an idea, answer:
  "An idea. Help me build a tiny personal reading-list app."

  Accept the guided flow once it begins and answer its questions consistently
  with that small reading-list idea. If guided starts before the onboarding
  fork, push back: "Show me the onboarding choices first." Never choose Bring
  an app or Take a quick tour in this scenario.
notableFriction:
  - id: onboarding-precedes-guided
    description: |
      Guided is already enabled, but the bare phrase carries no product intent.
      The exact three-option onboarding fork must appear before guided asks
      build questions or owns the request.
  - id: build-an-idea-hands-off-once
    description: |
      The persona deterministically selects **Start something new** and "build
      an idea". That explicit choice hands work to guided exactly once; the
      onboarding playbook must not compete with or restyle guided afterward.
  - id: guided-opening-keeps-consent-boundary
    description: |
      Before the choice, the opening performs no state read and there is no
      bootstrap commit, import, or deploy call.
---

onboard me to Zerops
