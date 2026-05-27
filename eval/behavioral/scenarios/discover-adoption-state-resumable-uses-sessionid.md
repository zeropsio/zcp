---
id: discover-adoption-state-resumable-uses-sessionid
description: |
  "Něco se mi tu zaseklo, podívej se a vyřeš to" — natural-Czech
  mid-bootstrap recovery scenario testing v9.101.x resumable
  classification: discover surfaces `adoptionState:"resumable"` plus
  a directive warning naming the sessionId, and the agent MUST use
  that sessionId in `route="resume"` (not ask the user, not pick
  route="adopt" which would reject as ZCP-owned).

  Pre-state: fixture deploys appdev/appstage/db; preseed plants an
  INCOMPLETE pair-keyed ServiceMeta for appdev (no BootstrappedAt
  + non-empty BootstrapSession) + matching session-registry entry
  for the resume target. Mirrors workflow.resumeOption semantics
  (route.go:271-282).

  Surfaces the agent must navigate:

   1. adoptionState=resumable, NOT adoptable — incomplete meta with
      BootstrapSession is owned by a prior session. Discover marks
      it resumable; the right recovery is route=resume sessionId=X,
      not route=adopt (which rejects ZCP-owned slots).
   2. SessionId from warning, not user — discover's resume warning
      prose carries sessionId="sess-stale-mid-bootstrap-2026-05-27"
      verbatim. Agent should copy-paste it into route=resume call,
      not ask user "what was your session ID?".
   3. INVALID_PARAMETER avoidance — route="resume" without sessionId
      rejects with INVALID_PARAMETER (handler at
      tools/workflow.go:961-967). Warning explicitly mentions this
      — agent reading the warning sees the failure mode upfront.
   4. No classic-bootstrap over resumable services — classic
      bootstrap ignores the BootstrapSession lock and would collide
      with the orphan records at provision. Atom bootstrap-resume.md
      explicitly forbids this; agent should not propose it.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: preseed/discover-resumable-orphan-meta.sh
tags: [discover, adoption-state, resumable, resume-route, sessionid-passthrough, czech-prompt, real-life]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
verification:
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
  noFailedProcesses: true
  retrospectiveMustNotMention:
    - "asked user for session"
    - "tried classic"
    - "tried adopt"
    - "had to delete state"
userPersona: |
  Jsi vývojář, vrátil ses k Zerops projektu po chvíli a něco se
  tady zaseklo — nedávno jsi začal bootstrap appdev/appstage pair,
  ale session se mezitím nějak ztratila (process kill, restart),
  takže ZCP teď má napůl rozjeté metas. Služby na Zerops platformě
  ale běží.

  Tvoje preference:
   - Akceptuj agent's discover-based diagnostiku.
   - Pokud agent správně přečte adoptionState=resumable + warning
     s sessionId → očekáváš následující call: `zerops_workflow
     action="start" workflow="bootstrap" route="resume"
     sessionId="<the-id-from-warning>"`. Akceptuj.
   - Pokud agent navrhne route="adopt" → odmítni: "ne, vidíš
     v adoptionState že je to resumable, ne adoptable — resume
     route s tím sessionId co je ve warningu."
   - Pokud se agent zeptá "jakou session ID jsi měl?" → odmítni:
     "to máš ve warningu z discoveru, prostě to přečti."
   - Pokud agent navrhne classic-bootstrap nebo smazat state → 
     odmítni: "ne, recovery, ne fresh start."

  Co odmítneš:
   - Agent zkusí `route="adopt"` → "resumable, ne adoptable."
   - Agent se ptá na sessionId → "máš ho ve warningu."
   - Agent navrhne smazat .zcp/state/ → "to je nuclear option,
     zkus nejdřív resume."
   - Agent zkusí `route="resume"` bez sessionId → "to fail s
     INVALID_PARAMETER, dej tam ten sessionId z warningu."

  Co očekáváš na konci:
   - Agent přečetl warning z discoveru
   - Vyparsoval sessionId="sess-stale-mid-bootstrap-2026-05-27"
   - Zavolal route="resume" s tím sessionId
   - Pokud resume sám fail (incomplete fixture session), agent to
     vyhodnotí + navrhne next step (např. status check) — ale
     žádný adopt / classic fallback

notableFriction:
  - id: resumable-vs-adoptable-classification
    description: |
      Agent must distinguish adoptionState=resumable from
      adoptionState=adoptable. Pre-fix the discover surface
      conflated them via the generic "not tracked by ZCP" reading.
      Post-fix the two warnings are separate (route=adopt vs
      route=resume). Surfaces whether the two-warning shape is
      clear enough.
  - id: sessionid-from-warning-not-user
    description: |
      Discover's resume warning includes the literal sessionId.
      Agent should copy-paste, not ask the user. Surfaces whether
      warning prose telegraphs the actionable values OR whether
      agents default to "ask user for missing arg" reflex.
  - id: route-resume-handler-coupling
    description: |
      route=resume without sessionId returns INVALID_PARAMETER.
      Warning explicitly mentions this failure mode. Surfaces
      whether the failure-mode-in-prose pattern reduces wasted
      round-trips vs agent trying route=resume bare first.
  - id: no-state-deletion-as-shortcut
    description: |
      Agent might be tempted to `rm -rf .zcp/state` to clear
      "stuck" state. Atom bootstrap-resume.md says "abandon only
      when stale". Surfaces whether atom guidance holds when
      recovery looks complicated.
---

Něco se mi tady v Zerops projektu zaseklo — vidím že tam mám appdev/appstage pair na platformě, ale stav v ZCP vypadá nějak napůl. Podívej se na to a vyřeš to, ať to dotáhnu do hotového stavu.
