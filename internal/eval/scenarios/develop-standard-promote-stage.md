---
id: develop-standard-promote-stage
description: Existing Node.js standard pair — user explicitly asks for stage promotion; agent must deploy dev first, cross-deploy appdev→appstage, then verify stage.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 6
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
    - '"workflow":"develop"'
    - '"scope":['
    - '"appdev"'
    - '"sourceService":"appdev"'
    - '"targetService":"appstage"'
    - '"setup":"prod"'
  forbiddenPatterns:
    - '"scope":["db"]'
    - 'github_pat_'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appstage
followUp:
  - "Jaký byl rozdíl mezi deployem na appdev a promocí na appstage?"
  - "Jaké argumenty měl cross-deploy do stage (`sourceService`, `targetService`, `setup`)?"
  - "Co jsi ověřil na appdev před stage promocí a co na appstage po ní?"
---

# Úkol

V projektu už existuje Node.js standard mode pár `appdev` + `appstage` a
managed PostgreSQL `db`. Projekt byl vytvořený mimo aktuální ZCP session.

Přidej jednoduchou verzi endpointu `GET /api/version`, který vrací JSON:

```json
{ "version": "2026.05", "runtime": "nodejs" }
```

Požadavky:

- Nejdřív adoptuj existující služby.
- Kódovou změnu udělej a ověř na `appdev`.
- Pak ji explicitně promotuj do `appstage`.
- Stage promotion má být cross-deploy z `appdev` do `appstage` s produkčním
  setupem.
- Na konci ověř veřejnou URL `appstage`.
