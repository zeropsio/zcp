---
id: develop-strategy-unset-regression
description: Regression guard — after the first deploy lands, the agent picks a deploy strategy via the develop-strategy-review atom before continuing
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 6
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Kdy konkrétně ti `zerops_workflow` vrátilo guidance 'Pick an ongoing deploy strategy' a jak jsi strategy vybral?"
  - "Šel jsi bootstrap discovery první (bez route) a adopt až druhým callem? Proč to tak musí být?"
  - "Proč strategy-review atom nefiruje před prvním deployem?"
---

# Úkol

V projektu jsou Laravel služby (`app` + `db`), byly naseedované mimo ZCP
(bez ZCP metadat, bez zvolené deploy strategy). Tvým úkolem je:

1. **Adoptovat** existující služby přes bootstrap discovery:
   - První `zerops_workflow action="start" workflow="bootstrap"` bez route
     = discovery response, druhý call s `route="adopt"` = commit.
2. Po adopci bootstrap-verify krok stampne `FirstDeployedAt` (služba je
   running). Develop briefing pak vyrenderuje `develop-strategy-review`
   atom — zvol deploy strategy dřív, než provedeš další kroky.
3. Změnit titulek na home page (soubor `resources/views/welcome.blade.php`)
   z výchozí "Laravel" na "Weather Dashboard".

Pravidla:

- Začni `zerops_workflow action="status"` a řiď se vráceným next-action.
- Bootstrap discovery je **dvoukrokový**: první call bez route = discovery
  response, druhý call s route=adopt = commit.
- Po adopci se ti v develop briefingu ukáže "Pick an ongoing deploy
  strategy" — zvol jednu přes `action="strategy"` dřív, než editovat.
- Po změně ověř, že home page vrací text "Weather Dashboard".

Verify: `GET https://<app-subdomain>/` obsahuje `Weather Dashboard`.
