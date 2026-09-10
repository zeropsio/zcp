---
# promote to required after two consecutive baseline greens (plan D9)
id: resume-status-not-discover
description: |
  Third S5 signal: lifecycle recovery on an already-bootstrapped, seeded
  project is `zerops_workflow{action="status"}` (CLAUDE.md P4: the
  envelope is the recovery primitive), never a fresh `zerops_discover` +
  re-bootstrap. Same pre-state as `resume-after-compaction` (a project
  with services already adopted/bootstrapped and an open work session) —
  this scenario isolates whether the agent reaches for status vs.
  re-running the bootstrap workflow from scratch on a project ZCP already
  knows about.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [resume, compaction-recovery, status-primitive, no-rebootstrap, observe, s5-signal]
area: develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář co pracuje na Zerops appce (Node.js + Postgres). Před chvílí
  jsi měl session co se nějak ztratila (compaction, restart, jiný
  window). Služby ale stále běží — `appdev` + `appstage` + `db` na
  Zerops, kód funguje, jenom jsi ztratil kontext téhle session.

  Pokud agent navrhne fresh bootstrap nebo `zerops_workflow
  action="start" workflow="bootstrap"` na projekt co už existuje, odmítni:
  "služby UŽ EXISTUJÍ, jen recovery session, ne nový bootstrap."
verification:
  mode: observe
  spec: spec-work-session.md §6.2
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
  unchanged: [appdev, appstage]
  never: ["zerops_import{override=true}", "zerops_workflow{action=start,workflow=bootstrap}"]
notableFriction:
  - id: status-not-rebootstrap
    description: |
      CLAUDE.md P4: lifecycle recovery is `action="status"` (envelope =
      recovery primitive), never a fresh `zerops_discover` + re-bootstrap
      of an adopted project. Surfaces whether the routing-table hint
      ("Wake up / not sure where you are → status") is followed
      reflexively vs. the agent falling back to `zerops_workflow
      action="start" workflow="bootstrap"` on a project it should
      recognize as already provisioned.
    suspectedCauses:
      - internal/content/atoms claude_shared.md routing table (status vs. start ambiguity when memory is empty)
---

Pokračuj prosím v práci na mojí Zerops appce — nevím přesně kde jsem skončil, něco se mi tady v session vyresetovalo. Služby běží (mám tam pair Node.js + Postgres v projektu `{{projectId}}`), všechno je funkční na platformě, jenom kontext session už nemám. Zkus z toho vyčíst kam jsem se dostal a navrhni co dál.
