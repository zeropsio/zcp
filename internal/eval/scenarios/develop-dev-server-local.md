---
id: develop-dev-server-local
description: Local env + dev mode + dynamic runtime. Agent uses harness background task primitive (Bash run_in_background=true in Claude Code) on the user's machine — not SSH to remote, not zerops_dev_server (container-only).
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_verify
  workflowCallsMin: 3
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
  forbiddenPatterns:
    # zerops_dev_server is container-only — in local env the dev server
    # runs on the user's machine via the harness's background task primitive.
    - 'zerops_dev_server action=start'
    - 'zerops_dev_server action=restart'
    # Raw ssh to remote for starting a dev server is the legacy myth
    # — local env does not SSH into remote containers to start processes.
    - 'ssh -o StrictHostKeyChecking=no'
    - 'cd /var/www && npm run'
  requireAssessment: true
  finalUrlHostname: app
followUp:
  - "Jak jsi pustil dev server v lokálu? Proč nebylo vhodné použít `zerops_dev_server`?"
  - "Co se děje když pustíš proces přes běžný `Bash` bez `run_in_background=true`?"
  - "Jak se dev server dostane k managed-service env vars (DB_HOST, DATABASE_URL) v lokálu?"
---

# Úkol

Pracuješ lokálně (ne v Zerops containeru). Služby `app` + `db` jsou
v Zerops projectu; adoptuj je, pak rozběhni dev server **na svém stroji**:

1. **Adoptuj** přes bootstrap discovery (jako v scenario
   `develop-add-endpoint.md`).

2. V develop módu v lokálu:
   - Dev server běží **na tvém stroji**, ne na Zerops containeru.
   - Použij harness background task primitive (v Claude Code:
     `Bash run_in_background=true`), ne `zerops_dev_server` (ten je
     container-only).
   - Pro env vars z managed services (DB, cache) vygeneruj `.env`:
     `zerops_env action=generate-dotenv serviceHostname="app"`.

3. `zerops_verify serviceHostname="app"` ověří subdomain + deploy.

Verify: dev server odpovídá na `http://localhost:{port}/` a deployed
service je healthy.
