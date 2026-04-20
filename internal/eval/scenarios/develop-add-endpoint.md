---
id: develop-add-endpoint
description: Adopt seeded Laravel services (no ZCP meta) and add endpoint via develop flow
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_verify
  workflowCallsMin: 8
  mustEnterWorkflow:
    - bootstrap
    - develop
followUp:
  - "Jak jsi poznal, že služby `app` + `db` existují, ale nemají ZCP metadata? Co jsi udělal?"
  - "V jakém pořadí jsi šel workflow-y (bootstrap/adopt → develop)? Proč přesně v tomhle pořadí?"
  - "Zvolil jsi push-dev nebo push-git strategy? Proč právě tuhle?"
---

# Úkol

V projektu běží Laravel aplikace (`app` + `db`) — services byly vytvořené
mimo ZCP (žádné metadata o bootstrap modu ani strategy). Tvým úkolem je:

1. **Adoptovat** existující služby do ZCP (bootstrap/adopt route) — jinak
   develop flow nemá kontext a `zerops_deploy` nemá kam cílit.
2. Po adoptování přidat endpoint `GET /api/status`, který vrací JSON:

   ```json
   { "status": "ok", "timestamp": "<iso-8601 now>", "db": "<connected|disconnected>" }
   ```

   Endpoint musí zkusit DB ping; pokud spojení selže, `db` je
   `"disconnected"`, ale status zůstane `"ok"` (endpoint sám nesmí failnout).

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav a teprve poté se
  rozhodni pro další kroky.
- Po adopci přejdi do develop workflow — tam probíhá kódová iterace +
  deploy + verify.
- Po deployi ověř endpoint.

Verify: `curl https://<app-subdomain>/api/status` vrací 200 s validním JSON.
