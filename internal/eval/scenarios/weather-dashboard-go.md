---
id: weather-dashboard-go
description: Cross-runtime weather dashboard audit — Go (dynamic runtime, compiled, minimal framework). Used by multi-runtime-weather-audit suite.
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 6
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"'
  forbiddenPatterns:
    - "app-<projectId>"
    - "${projectId}.prg1.zerops.app"
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appstage
followUp:
  - "Kolik řádků guidance jsi dostal z prvního `zerops_workflow action=\"start\" workflow=\"develop\"` (přibližně)? Kolik z nich bylo pro tvůj konkrétní stav irelevantních?"
  - "Jak jsi řešil compile step v deploy pipeline — buildCommands v zerops.yaml, nebo jiný pattern? Byla k tomu v atomech jasná guidance?"
---

# Úkol

Vytvoř jednoduchý weather dashboard v Go nasazený na Zerops.

Aplikace:
- Seznam oblíbených měst (Praha, Brno, Ostrava) v PostgreSQL databázi.
- Zobrazení aktuální teploty z Open-Meteo API (bez API klíče).
- Std-lib (`net/http` + `database/sql` + `lib/pq`) nebo minimal framework dle preference.
- Veřejná subdomain URL.

Deploy na Zerops v standard mode (dev + stage pair), ověř že `appstage` vrací HTTP 200 s renderovanou stránkou.

---

## ATOM BUCKET CLASSIFICATION (povinná sekce reportu)

Poté co dokončíš task, v sekci `## EVAL REPORT` přidej blok:

```
### Atom bucket classification

Pro každý develop-phase atom, který jsi viděl v dispatch briefu (v responsech `zerops_workflow action="start" workflow="develop"` nebo `action="status"`), klasifikuj jeho hodnotu pro tvůj konkrétní envelope (Go/golang@1.23 + standard mode + bootstrapped + never-deployed + container env):

- **A** — LOAD-BEARING: bez něj bys selhal / klopýtnul
- **B** — USEFUL AWARENESS: nepotřeboval, ale dobré mít
- **C** — PURE NOISE: čistý ballast pro tento stav

Formát (YAML blok):

atoms:
  - id: develop-first-deploy-intro
    bucket: A
    note: "entry to branch"
  - id: develop-dev-server-triage
    bucket: A|B|C
    note: "důvod volby"
  # ... jeden řádek per atom ID, který jsi viděl

friction:
  - area: "stručný popis"
    cost_minutes: 2
    suggestion: "konkrétní návrh"

timing_minutes:
  bootstrap: 3
  coding: 6
  deploy_plus_verify: 2
  total: 11
```

Nezapomeň: tohle je audit, ne soutěž — upřímně označ **C** i guidance, která je obecně užitečná ale pro tvůj konkrétní stav irelevantní.
