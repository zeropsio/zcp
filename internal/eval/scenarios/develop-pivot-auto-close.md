---
id: develop-pivot-auto-close
description: Two sequential develop tasks on the same service — tests cb63bf3 invariant "1 task = 1 session, close always wins". Agent must start two develop sessions with different intents, auto-close between them via deploy+verify, and NOT emit explicit action=close nor encounter a WORKFLOW_ACTIVE error (both retired in cb63bf3).
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/develop-pivot-auto-close.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_deploy
    - zerops_verify
  # workflowCallsMin counts only zerops_workflow calls. Realistic floor is
  # 3 (status + 2× develop start). Deploy/verify are different tool names
  # and don't count here — mustCallTools already asserts them separately.
  workflowCallsMin: 3
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    # Two distinct develop intents in the log — evidence the agent honored
    # "1 task = 1 session" rather than stuffing both changes under one
    # intent. The exact intent substrings below mirror user-phrased
    # shorthand from the prompt; both must appear across the two calls.
    - '"workflow":"develop"'
    - '"scope":['
    - '"app"'
    # Each develop start passes its own intent field — the grader scans
    # Input AND Result, so these patterns catch either serialization.
    # Loose substrings allow phrasing drift while pinning the topic.
    - 'endpoint'
    - 'title'
  forbiddenPatterns:
    # Explicit close is no longer needed — cb63bf3 removed the close guard
    # so close is always allowed, and new-intent auto-closes prior. Agent
    # calling close manually between the two tasks means it didn't trust
    # the new contract.
    - '"action":"close"'
    # Pre-cb63bf3 the second develop start returned a WORKFLOW_ACTIVE
    # error unless the agent closed first. The forbidden form is the
    # JSON-serialized error code — `"code":"WORKFLOW_ACTIVE"` as produced
    # by convertError (internal/tools/convert.go). Bare substrings match
    # the prompt text + assessment mentions which is a false positive.
    - '"code":"WORKFLOW_ACTIVE"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Kolik develop sessions jsi v průběhu úkolu start-nul? Jaký intent měl každý z nich?"
  - "Musel jsi někdy explicitně zavolat `action=\"close\"` mezi prvním a druhým úkolem? Proč ne (podle CLAUDE.md / develop-change-drives-deploy atomu)?"
  - "Co konkrétně odpálilo auto-close prvního sessionu — deploy, verify, nebo něco jiného? Odkud jsi to viděl v response?"
  - "Kdy jsi zvedl `scope=[\"app\"]` a proč to stačí pro tuhle dev-mode single-service app?"
---

# Úkol

V projektu běží Laravel služba `app` (php-nginx, dev mode) + managed `db`
(postgres). Služba je adoptovaná a běží pod push-dev strategy.

Chci, abys udělal **dvě oddělené změny, každou jako vlastní develop
session** (1 task = 1 session — nepoužívej jeden session pro obojí,
nezavolej mezitím `action="close"` manuálně).

**Task 1**: Přidej `GET /api/status` endpoint, který vrací JSON:

```json
{ "status": "ok", "time": "<iso-8601 now>" }
```

Nasaď, ověř (`curl https://<app-subdomain>/api/status` → 200 + validní JSON).

**Task 2**: Změň titulek welcome page (`resources/views/welcome.blade.php`)
z výchozího "Laravel" na "Weather Dashboard".

Nasaď, ověř (`curl https://<app-subdomain>/` obsahuje řetězec
"Weather Dashboard").

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav.
- **Každý task = jeden vlastní develop session** s explicitním
  `intent="<popis>"` a `scope=["app"]`. CLAUDE.md: "new intent on an
  already-open session auto-closes the prior one; no need to action=close
  manually between tasks."
- Po deployi a verify každého tasku auto-close zabere automaticky (všechny
  služby v scope mají passing deploy + verify).
- Druhý task musí projít stejně hladce jako první — **nesmí** ti přijít
  `WORKFLOW_ACTIVE` error, ani `ErrCloseBlocked`.

Verify (na konci): obě změny jsou dostupné na subdoméně.
