---
id: develop-close-mode-unset-regression
description: Regression guard — after the first deploy lands, the agent picks a close-mode via the develop-strategy-review atom before continuing
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
    # scope is REQUIRED for develop start (cb63bf3). `app` is the runtime
    # hostname; `db` is managed and rejected by validateDevelopScope.
    - '"scope":['
    - '"app"'
    # action="close-mode" is the canonical CloseDeployMode setter
    # post-deploy-strategy-decomposition. The develop-strategy-review atom
    # tells the agent to call it post-first-deploy — this pin verifies the
    # agent actually executed the call, not just acknowledged the atom.
    - '"action":"close-mode"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Kdy konkrétně ti `zerops_workflow` vrátilo guidance 'Pick an ongoing close-mode' a jak jsi close-mode vybral?"
  - "Šel jsi bootstrap discovery první (bez route) a adopt až druhým callem? Proč to tak musí být?"
  - "Proč strategy-review atom nefiruje před prvním deployem?"
---

# Úkol

V projektu jsou Laravel služby (`app` + `db`), byly naseedované mimo ZCP
(bez ZCP metadat, bez zvolené close-mode). Tvým úkolem je:

1. **Adoptovat** existující služby přes bootstrap discovery:
   - První `zerops_workflow action="start" workflow="bootstrap"` bez route
     = discovery response, druhý call s `route="adopt"` = commit.
2. Po adopci envelope ukáže `deployed=true` pro běžící službu. Develop
   briefing pak vyrenderuje `develop-strategy-review` atom — zvol
   close-mode dřív, než provedeš další kroky.
3. Změnit titulek na home page (soubor `resources/views/welcome.blade.php`)
   z výchozí "Laravel" na "Weather Dashboard".

Pravidla:

- Začni `zerops_workflow action="status"` a řiď se vráceným next-action.
- Bootstrap discovery je **dvoukrokový**: první call bez route = discovery
  response, druhý call s route=adopt = commit.
- Po adopci se ti v develop briefingu ukáže "Pick an ongoing close-mode" —
  zvol jednu přes `action="close-mode"` dřív, než editovat.
- Po změně ověř, že home page vrací text "Weather Dashboard".

Verify: `GET https://<app-subdomain>/` obsahuje `Weather Dashboard`.
