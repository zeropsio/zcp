---
id: develop-dev-server-container
description: Container env + close-mode=auto + dev mode + dynamic runtime (Node.js). Agent must use zerops_dev_server for dev-server lifecycle — not raw ssh backgrounding.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_dev_server
    - zerops_verify
  workflowCallsMin: 4
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
    # Canonical dev-server lifecycle primitive in container env
    - 'zerops_dev_server'
  forbiddenPatterns:
    # Raw-SSH dev-server start is the anti-pattern zerops_dev_server replaces
    - 'cd /var/www && npm run'
    - 'cd /var/www && bun run'
    - 'nohup '
    - '& disown'
  requireAssessment: true
  finalUrlHostname: app
followUp:
  - "Proč jsi nepustil dev server přes raw `ssh {host} \"cmd &\"`? Jaký problém by to způsobilo?"
  - "Kdy jsi volal `zerops_dev_server action=status` vs `action=start`? V jakém pořadí a proč?"
  - "Co je podstata `reason` pole v odpovědi z `zerops_dev_server`? Jaké reason kódy jsi viděl?"
---

# Úkol

V projektu běží Laravel aplikace (`app` + `db`). Služby byly vytvořené mimo
ZCP — adoptuj je přes bootstrap discovery (route=adopt), pak v develop
módu iteruj na kódu:

1. **Adoptuj** existující služby do ZCP přes `zerops_workflow action="start" workflow="bootstrap"` (bez route → discovery, pak s `route="adopt"`).

2. V develop flow **iteruj na kódu**:
   - Edituj soubory přímo na SSHFS mount (`/var/www/app/`).
   - Po každé významné změně **zajisti, že dev server běží** — použij
     `zerops_dev_server` tool s akcemi `status`, `start`, `restart` podle
     situace. Tool má strukturovaný výsledek (reason kódy, healthStatus);
     spoléhej na něj pro diagnostiku.
   - **NESMÍŠ** použít raw pattern `ssh {host} "cmd &"` nebo `nohup ... &`
     — tohle drží SSH channel do 120s timeoutu (broken anti-pattern).

3. Na konci `zerops_verify` pro ověření.

Verify: `zerops_verify serviceHostname="app"` projde.
