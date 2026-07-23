---
id: onboard-populated
description: |
  Exact onboarding trigger against an existing deployed Node.js standard pair
  and PostgreSQL dependency. Reuses the neighboring
  `fixtures/nodejs-standard-deployed.yaml` fixture so the on-demand discover
  read has real non-system rows and is observably POPULATED. Oracle:
  docs/spec-onboarding.md §§1–3.

  Surfaces the agent must navigate:

   1. Exact trigger — fetch
      `zerops_knowledge uri="zerops://playbooks/onboarding"` exactly once.
   2. Immediate opening — greet and present **Bring an app**, **Start something
      new**, and **Take a quick tour** first, with no pre-greeting status or
      discover read.
   3. On-demand state read — when the persona asks "What's already running in
      this project?", call `zerops_workflow action="status"` first and then
      `zerops_discover`; classify POPULATED from discover, give one compact
      found-services summary, and offer **Continue this project** alongside the
      other options.
   4. Consent before provisioning — before the user chooses a direction, the
      agent MUST NOT call `zerops_workflow action="start"`, `zerops_import`, or
      `zerops_deploy`. Existing services do not weaken the consent boundary.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [onboarding, trigger-positive, populated, deployed-fixture, consent]
area: onboarding
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are new to Zerops and want the onboarding choices before inspecting the
  existing project. When the agent first presents the onboarding fork, answer
  exactly: "What's already running in this project?"

  After the agent summarizes the project and offers **Continue this project**,
  say: "Thanks — stop there for now." Do not choose a direction or authorize
  any changes in this scenario.
notableFriction:
  - id: populated-classification-comes-from-discover
    description: |
      The greeting and three-option fork come first with no pre-greeting state
      read. Only after the persona asks what's running does the agent read
      status first and classify POPULATED from the subsequent direct
      `zerops_discover` result.
  - id: populated-opening-adds-continue
    description: |
      After the on-demand read, a compact service summary and **Continue this
      project** join the same three exact opening labels. They do not delay or
      alter the initial greeting.
  - id: populated-opening-still-read-only
    description: |
      The existing deployment is context, not permission. No bootstrap commit,
      import, or deploy call occurs throughout the opening or on-demand read.
---

onboard me to Zerops
