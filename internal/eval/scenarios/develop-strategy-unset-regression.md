---
id: develop-strategy-unset-regression
description: Regression guard — LLM must go through bootstrap discovery → adopt, then pick a deploy strategy before any Edit
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
    - '"action":"start","workflow":"bootstrap"'
    - '"route":"adopt"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Kdy přesně ti `zerops_workflow` vrátilo guidance 'Strategy selection required' a jak jsi strategy vybral?"
  - "Šel jsi bootstrap discovery první (bez route) a adopt až druhým callem? Proč to tak musí být?"
  - "Co by se stalo, kdybys editoval soubor před zvolením strategy?"
---

# Úkol

V projektu jsou Laravel služby (`app` + `db`), byly naseedované mimo ZCP
(bez ZCP metadat, bez zvolené deploy strategy). Tvým úkolem je:

1. **Adoptovat** existující služby přes bootstrap discovery:
   - První `zerops_workflow action="start" workflow="bootstrap"` bez route
     = discovery response, druhý call s `route="adopt"` = commit.
2. V develop flow vybrat deploy strategy — než cokoli editovat.
3. Změnit titulek na home page (soubor `resources/views/welcome.blade.php`)
   z výchozí "Laravel" na "Weather Dashboard".

Pravidla:

- Začni `zerops_workflow action="status"` a řiď se vráceným next-action.
- Bootstrap discovery je **dvoukrokový**: první call bez route = discovery
  response, druhý call s route=adopt = commit.
- Pokud ti `zerops_workflow` vrátí guidance na výběr strategy, přečti ji
  a **vyber strategy dřív, než provedeš Edit**.
- Po změně ověř, že home page vrací text "Weather Dashboard".

Verify: `GET https://<app-subdomain>/` obsahuje `Weather Dashboard`.
