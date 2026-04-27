---
id: verify-rendered-text
description: zerops_verify on a deployed Laravel app must surface the rendered page text via http_root.bodyText (agent-browser walk) — proves the new field reaches the MCP transcript end-to-end.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_verify
  workflowCallsMin: 0
  requiredPatterns:
    # The verify response (JSON) goes into the agent's transcript as a
    # tool result. Proving "bodyText" / "httpStatus" appears means the
    # new CheckResult fields reached the MCP wire payload — the change
    # is live in the deployed binary.
    - '"bodyText"'
    - '"httpStatus":200'
  finalUrlStatus: 200
  finalUrlHostname: app
  requireAssessment: true
followUp:
  - "Co `zerops_verify` vrátil pro `app` — jaký HTTP status a co bylo v `bodyText`?"
  - "Jaký rozdíl je mezi `detail` (krátký text) a `bodyText` (rendered DOM) ve verify response?"
---

# Úkol

Projekt obsahuje běžící Laravel aplikaci na hostname `app` (php-nginx + postgres,
subdomain enabled). Tvůj úkol:

1. Spusť `zerops_workflow action="status"` ať vidíš stav projektu.
2. Adoptuj `app` přes bootstrap (route=adopt — služba existuje bez ZCP meta).
3. Po dokončení bootstrapu zavolej `zerops_verify serviceHostname="app"`.
4. Reportuj, co `zerops_verify` vrátil — především:
   - aggregate status (healthy/degraded/unhealthy)
   - http_root.httpStatus
   - http_root.bodyText (rendered text z prohlížeče — co reálně user vidí na URL)

Cíl ověření: že `zerops_verify` u web-facing služby vrací rendered page text,
ne jen "HTTP 200" bez kontextu.
