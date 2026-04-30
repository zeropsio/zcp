---
id: develop-add-endpoint
description: Adopt seeded Laravel services (no ZCP meta) via bootstrap discovery → route=adopt, then add endpoint via develop flow
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_verify
  workflowCallsMin: 7
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
    # scope is REQUIRED for develop start (cb63bf3). `app` is the single
    # runtime hostname in this fixture — `db` is managed and not eligible.
    - '"scope":['
    - '"app"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Jak jsi poznal, že služby `app` + `db` existují, ale nemají ZCP metadata? Objevila se v discovery response adopt option?"
  - "V jakém pořadí jsi šel workflow-y (bootstrap/adopt → develop)? Proč přesně v tomhle pořadí?"
  - "Zvolil jsi closeMode=auto nebo closeMode=git-push? Proč právě tuhle?"
---

# Úkol

V projektu běží Laravel aplikace (`app` + `db`) — services byly vytvořené
mimo ZCP (žádné metadata o bootstrap modu ani strategy). Tvým úkolem je:

1. **Rozpoznat existující services z discovery** a adoptovat je do ZCP:
   - První `zerops_workflow action="start" workflow="bootstrap"` bez
     `route` parametru vrátí `routeOptions[]` s adopt option na top —
     `adoptServices: ["app"]`. Commit s `route="adopt"`.
2. Po adoptování přidat endpoint `GET /api/status`, který vrací JSON:

   ```json
   { "status": "ok", "timestamp": "<iso-8601 now>", "db": "<connected|disconnected>" }
   ```

   Endpoint musí zkusit DB ping; pokud spojení selže, `db` je
   `"disconnected"`, ale status zůstane `"ok"` (endpoint sám nesmí failnout).

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav a teprve poté se
  rozhodni pro další kroky.
- Bootstrap discovery je **dvoukrokový**: první call bez route = discovery
  response, druhý call s `route=adopt` = commit.
- Po adopci přejdi do develop workflow — tam probíhá kódová iterace +
  deploy + verify.
- Po deployi ověř endpoint.

Verify: `curl https://<app-subdomain>/api/status` vrací 200 s validním JSON.
