---
id: bootstrap-simple-node-api
description: Greenfield simple-mode Node.js API — validates guide promise that simple mode stays a single runtime without dev/stage pairing.
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 6
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"bootstrapMode":"simple"'
    - '"workflow":"develop"'
    - '"scope":['
    - '"app"'
  forbiddenPatterns:
    - '"targetService":"appstage"'
    - 'github_pat_'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Proč byl simple mode vhodnější než standard mode?"
  - "Kolik runtime services vzniklo a jaký hostname jsi použil v develop scope?"
  - "Kdy proběhla první deploy/verify smyčka — v bootstrapu, nebo až v develop workflow?"
---

# Úkol

V prázdném ZCP projektu vytvoř jednoduché Node.js 22 API jako jednu službu.

Požadavky:

- Jeden runtime s hostname `app`.
- Žádný dev/stage pár.
- Žádná databáze ani jiná managed služba.
- Endpoint `GET /` vrací HTML s textem `Simple ZCP API`.
- Endpoint `GET /health` vrací JSON `{ "ok": true }`.
- Veřejná subdomain URL musí vrátit HTTP 200.

Chci jednoduchou single-service aplikaci, ne staging workflow.
