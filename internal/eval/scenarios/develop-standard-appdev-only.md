---
id: develop-standard-appdev-only
description: Existing Node.js appdev+appstage project — user scopes work to appdev only; agent must adopt and develop appdev without treating db as a target.
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
  forbiddenPatterns:
    - '"scope":["db"]'
    - 'github_pat_'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appdev
followUp:
  - "Jak jsi poznal, že existující appdev/appstage projekt se má adoptovat a ne bootstrapovat znovu?"
  - "Proč byl develop scope jen `appdev`? Jakou roli hrály `appstage` a `db`?"
  - "Promotoval jsi změnu do appstage? Pokud ne, proč ne?"
---

# Úkol

V projektu už existuje Node.js dev+stage pár `appdev` + `appstage` a managed
PostgreSQL `db`. Ten projekt byl vytvořený mimo aktuální ZCP session.

Uprav jen `appdev`:

- Přidej endpoint `GET /api/reminders`, který vrací JSON pole.
- Pro začátek stačí prázdné pole `[]`.
- Neměň ani nenasazuj `appstage`; stage promotion teď nechci.
- `db` je dependency, ne target pro kódové změny.

Začni recovery/status nebo discovery krokem, adoptuj existující services, potom
v develop workflow pracuj se scope `appdev`. Na konci ověř public URL pro
`appdev`.
