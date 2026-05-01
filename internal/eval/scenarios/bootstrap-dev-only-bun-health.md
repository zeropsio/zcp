---
id: bootstrap-dev-only-bun-health
description: Greenfield dev-only Bun API — validates guide promise that dev mode creates one mutable runtime and does not invent a stage service.
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
    - '"bootstrapMode":"dev"'
    - '"workflow":"develop"'
    - '"scope":['
    - '"appdev"'
  forbiddenPatterns:
    - '"targetService":"appstage"'
    - 'github_pat_'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appdev
followUp:
  - "Z čeho bylo vidět, že jde opravdu o dev-only mode a nevznikl stage target?"
  - "Jaký runtime scope jsi předal do develop workflow a proč v něm nebyla žádná managed služba ani stage?"
  - "Jak jsi ověřil veřejnou URL pro appdev?"
---

# Úkol

V prázdném ZCP projektu vytvoř malou Bun HTTP API službu jen pro vývoj.

Požadavky:

- Bun runtime.
- Jeden mutable dev runtime s hostname `appdev`.
- Žádná databáze.
- Žádný stage target teď nechci.
- Endpoint `GET /` vrací HTTP 200 s krátkým textem `Bun dev API`.
- Endpoint `GET /health` vrací JSON `{ "ok": true, "mode": "dev" }`.
- Veřejná subdomain URL musí vrátit HTTP 200.

Postup nech na ZCP workflow. Nejde o ruční import YAML cvičení; důležité je,
aby zvolený mode odpovídal zadání a aby první deploy + verify proběhly proti
`appdev`.
