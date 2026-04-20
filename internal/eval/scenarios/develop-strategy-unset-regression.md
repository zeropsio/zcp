---
id: develop-strategy-unset-regression
description: Regression guard for develop-strategy-unset atom firing in idle phase
seed: deployed
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
  workflowCallsMin: 5
  mustEnterWorkflow:
    - develop
followUp:
  - "Viděl jsi v odpovědi zerops_workflow hlášku o 'Strategy selection required'? V jakém kroku?"
  - "Co by se stalo, kdybys editoval soubor před zvolením strategy?"
---

# Úkol

V projektu je nasazená Laravel aplikace `appdev` + `db`. **Projektový stav
má prázdnou deploy strategy** (nebyla zvolena při předchozí develop session).

Změň titulek na home page (soubor `resources/views/welcome.blade.php`)
z výchozí "Laravel" na "Weather Dashboard".

Pravidla:

- Jdi přes develop workflow (ne bootstrap).
- Pokud ti `zerops_workflow` vrátí guidance na výběr strategy, přečti ji
  a **vyber strategy dřív, než provedeš Edit**.
- Po změně ověř, že home page vrací text "Weather Dashboard".

Verify: `GET https://<appdev-subdomain>/` obsahuje `Weather Dashboard`.
