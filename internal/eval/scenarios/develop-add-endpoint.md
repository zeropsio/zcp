---
id: develop-add-endpoint
description: Add endpoint to deployed Laravel app via develop workflow
seed: deployed
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_verify
  workflowCallsMin: 8
  mustEnterWorkflow:
    - develop
  forbiddenPatterns:
    - '"workflow":"bootstrap"'
followUp:
  - "Zvolil jsi push-dev nebo push-git strategy? Proč právě tuhle?"
  - "Kdy přesně se develop session uzavřel (auto-close nebo manual)?"
---

# Úkol

V projektu už běží Laravel aplikace (`appdev` + `db`). Přidej endpoint
`GET /api/status`, který vrací JSON:

```json
{ "status": "ok", "timestamp": "<iso-8601 now>", "db": "<connected|disconnected>" }
```

Endpoint musí zkusit DB ping; pokud spojení selže, `db` je `"disconnected"`,
ale status zůstane `"ok"` (endpoint sám nesmí failnout).

Požadavky:

- **Neprovádí nový bootstrap** — služby existují a jsou deployed.
- Jdi přes develop workflow — nevolej bootstrap.
- Po deployi / reloadu ověř endpoint.

Verify: `curl https://<appdev-subdomain>/api/status` vrací 200 s validním JSON.
