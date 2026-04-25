---
id: weather-dashboard-nextjs-ssr
description: Cross-runtime weather dashboard audit — Next.js SSR (Node-based SSR, full asset pipeline with npm/build). Targets build-pipeline atoms.
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
  - "Kolik řádků guidance jsi dostal z prvního `zerops_workflow action=\"start\" workflow=\"develop\"` (přibližně)? Kolik z nich bylo irelevantních?"
  - "Jak jsi řešil npm/build step před deployem — buildCommands v zerops.yaml, nebo manuální ssh + npm run build? Byla guidance ohledně asset pipeline jasná?"
---

# Úkol

Vytvoř weather dashboard v Next.js (SSR) nasazený na Zerops.

Aplikace:
- Seznam oblíbených měst (Praha, Brno, Ostrava) v PostgreSQL databázi.
- Zobrazení aktuální teploty z Open-Meteo API (bez API klíče).
- Next.js 15+ s App Router a server components (SSR, ne static export).
- Veřejná subdomain URL.

Deploy na Zerops v standard mode (dev + stage pair), ověř že `appstage` vrací HTTP 200 s renderovanou SSR stránkou.

---

## ATOM BUCKET CLASSIFICATION (povinná sekce reportu)

Poté co dokončíš task, v sekci `## EVAL REPORT` přidej blok:

```
### Atom bucket classification

Pro každý develop-phase atom, který jsi viděl v dispatch briefu, klasifikuj jeho hodnotu pro tvůj konkrétní envelope (Next.js SSR/nodejs@22 + standard mode + bootstrapped + never-deployed + container env + build pipeline):

- **A** — LOAD-BEARING: bez něj bys selhal / klopýtnul
- **B** — USEFUL AWARENESS: nepotřeboval, ale dobré mít
- **C** — PURE NOISE: čistý ballast pro tento stav

Formát (YAML blok):

atoms:
  - id: develop-first-deploy-intro
    bucket: A
    note: "entry to branch"
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

Nezapomeň: označ upřímně **C** i guidance obecně užitečnou ale pro tvůj stav irelevantní.
