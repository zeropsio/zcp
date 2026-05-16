---
id: resume-after-compaction
description: |
  "Pokračuj v práci na mojí Zerops appce — nevím, kde jsem skončil"
  — natural-Czech compaction-recovery scenario. Pre-state: deployed
  pair (services exist, ServiceMeta written by prior bootstrap), but
  agent's session memory is "fresh" (simulating post-compact recovery).
  Agent must call `zerops_workflow action="status"` first to recover
  envelope, plan, atoms, then act on the recovered state instead of
  re-bootstrapping or asking the user to re-explain.

  Surfaces the agent must navigate:

   1. action="status" as recovery primitive — atom routing-table says
      "Wake up / not sure where you are → status". Agent must follow
      this BEFORE attempting workflow action="start" (which would
      open a fresh session and ignore existing meta).
   2. Envelope-as-truth — status response carries the StateEnvelope
      with PhaseDevelopActive, deployStates, runtimes, etc. Agent
      reads this verbatim, doesn't second-guess via additional
      discover/list calls.
   3. Plan + atoms drive next step — status response carries
      synthesized atoms scoped to the recovered phase. Agent follows
      atom guidance for the next action (verify, deploy, develop
      iteration) without inventing one.
   4. No re-prompting the user for state — agent doesn't ask "what
      services do you have?" or "what were you doing?" — the
      envelope answers both.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [resume, compaction-recovery, develop, node, czech-prompt, real-life]
area: develop
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
    - had to re-bootstrap
    - "started over"
    - re-importing
userPersona: |
  Jsi vývojář co pracuje na Zerops appce (Node.js + Postgres). Před
  chvílí jsi měl session co se nějak ztratila (compaction, restart,
  jiný window). Služby ale stále běží — `appdev` + `appstage` + `db`
  na Zerops, kód funguje, jenom jsi ztratil kontext téhle session.

  Cíl: agent musí pochopit pre-state přes `zerops_workflow
  action="status"` a navrhnout next step (např. develop iterace
  nebo verify). NEMUSÍ znovu bootstrap, NEMUSÍ se ptát "co jsi
  dělal" — env je platná, ZCP umí číst stav.

  Tvoje preference:
   - Akceptuj agent's status-based read.
   - Pokud agent navrhne fresh bootstrap nebo importu z nuly,
     odmítni: "služby UŽ EXISTUJÍ, jen recovery session."
   - Pokud agent navrhne smysluplnou další akci (verify, dev
     iteration, deploy ack), akceptuj.
   - Pokud agent nechce dělat nic specifického ("co bys chtěl?"),
     řekni: "nech mě vidět že to běží — verify nebo health check."

  Co odmítneš:
   - Agent zkusí `action="start"` nový bootstrap → "ne, recovery,
     ne fresh start."
   - Agent požaduje od tebe enumerace služeb → "status to vyřeší."

  Co očekáváš na konci:
   - Agent pochopil pre-state přes status response
   - Akceptoval develop-active jako phase
   - Navrhl + provedl smysluplný next step (verify / health check)
   - Žádný re-bootstrap, žádný nový import

notableFriction:
  - id: status-as-recovery-primitive
    description: |
      `zerops_workflow action="status"` musí být první call, ne
      `action="start"`. Surfaces whether claude_shared.md routing
      table's "Wake up / not sure where you are" hint dispatches
      to status reflexively.
  - id: envelope-driven-next-step
    description: |
      Status response's envelope + atoms drive next action. Agent
      nesmí trigger další discover/list pro state co envelope
      already shows. Surfaces whether atom guidance is followed
      vs. agent doing parallel re-discovery.
  - id: no-user-state-reprompting
    description: |
      Agent must NOT ask "what services do you have / what were
      you doing" — envelope answers both. Surfaces whether atoms
      telegraph this trust-the-envelope pattern.
---

Pokračuj prosím v práci na mojí Zerops appce — nevím přesně kde jsem skončil, něco se mi tady v session vyresetovalo. Služby běží (mám tam pair Node.js + Postgres v eval-zcp projektu), všechno je funkční na platformě, jenom kontext session už nemám. Zkus z toho vyčíst kam jsem se dostal a navrhni co dál.
