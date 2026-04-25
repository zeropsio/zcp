---
id: weather-dashboard-php-laravel
description: Cross-runtime weather dashboard audit — PHP/Laravel (implicit-webserver baseline). Used by multi-runtime-weather-audit suite to measure dispatch-brief bloat + friction across runtimes.
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
  - "Potřeboval jsi někde pre-digest bootstrap flow (kroky 1-N), nebo stačila guidance v response + atomech aby jsi věděl co dělat?"
---

# Úkol

Vytvoř jednoduchý weather dashboard v PHP / Laravel nasazený na Zerops.

Aplikace:
- Seznam oblíbených měst (Praha, Brno, Ostrava) v PostgreSQL databázi.
- Zobrazení aktuální teploty z Open-Meteo API (bez API klíče).
- Veřejná subdomain URL.

Deploy na Zerops v standard mode (dev + stage pair), ověř že `appstage` vrací HTTP 200 s renderovanou stránkou.

---

## ATOM BUCKET CLASSIFICATION (povinná sekce reportu)

Poté co dokončíš task, v sekci `## EVAL REPORT` přidej blok:

```
### Atom bucket classification

Pro každý develop-phase atom, který jsi viděl v dispatch briefu (v responsech `zerops_workflow action="start" workflow="develop"` nebo `action="status"`), klasifikuj jeho hodnotu pro tvůj konkrétní envelope (PHP/php-nginx + standard mode + bootstrapped + never-deployed + container env):

- **A** — LOAD-BEARING: bez něj bys selhal / klopýtnul
- **B** — USEFUL AWARENESS: nepotřeboval, ale dobré mít
- **C** — PURE NOISE: čistý ballast pro tento stav

Formát (YAML blok):

atoms:
  - id: develop-first-deploy-intro
    bucket: A
    note: "entry to branch"
  - id: develop-dev-server-triage
    bucket: C
    note: "implicit-webserver — triage ends at step 1"
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
