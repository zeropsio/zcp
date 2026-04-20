---
id: develop-strategy-unset-regression
description: Regression guard — LLM must pick a deploy strategy (after adopting seeded services) before any Edit
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
    - develop
followUp:
  - "Kdy přesně ti `zerops_workflow` vrátilo guidance 'Strategy selection required' a jak jsi strategy vybral?"
  - "Co by se stalo, kdybys editoval soubor před zvolením strategy?"
---

# Úkol

V projektu jsou Laravel služby (`app` + `db`), byly naseedované mimo ZCP
(bez ZCP metadat, bez zvolené deploy strategy). Tvým úkolem je:

1. **Adoptovat** existující služby (bootstrap/adopt route).
2. V develop flow vybrat deploy strategy — než cokoli editovat.
3. Změnit titulek na home page (soubor `resources/views/welcome.blade.php`)
   z výchozí "Laravel" na "Weather Dashboard".

Pravidla:

- Začni `zerops_workflow action="status"` a řiď se vráceným next-action.
- Pokud ti `zerops_workflow` vrátí guidance na výběr strategy, přečti ji
  a **vyber strategy dřív, než provedeš Edit**.
- Po změně ověř, že home page vrací text "Weather Dashboard".

Verify: `GET https://<app-subdomain>/` obsahuje `Weather Dashboard`.
