---
id: weather-dashboard-rust
description: Cross-runtime weather dashboard audit — Rust (dynamic runtime, compiled via cargo, different from Go's compile flow).
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
  - "Jak jsi řešil cargo build v deploy pipeline? Byla guidance specificky pro Rust, nebo obecná dynamic-runtime?"
---

# Úkol

Vytvoř weather dashboard v Rustu nasazený na Zerops.

Aplikace:
- Seznam oblíbených měst (Praha, Brno, Ostrava) v PostgreSQL databázi.
- Zobrazení aktuální teploty z Open-Meteo API (bez API klíče).
- Framework na tobě (axum / actix-web / rocket — cokoli fits).
- Veřejná subdomain URL.

Deploy na Zerops v standard mode (dev + stage pair), ověř že `appstage` vrací HTTP 200 s renderovanou stránkou.

---

## ATOM BUCKET CLASSIFICATION (povinná sekce reportu)

Poté co dokončíš task, v sekci `## EVAL REPORT` přidej blok:

```
### Atom bucket classification

Pro každý develop-phase atom, který jsi viděl v dispatch briefu, klasifikuj jeho hodnotu pro tvůj konkrétní envelope (Rust/rust@nightly + standard mode + bootstrapped + never-deployed + container env):

- **A** — LOAD-BEARING
- **B** — USEFUL AWARENESS
- **C** — PURE NOISE

Formát (YAML blok):

atoms:
  - id: develop-first-deploy-intro
    bucket: A
    note: "entry to branch"
  # ... jeden řádek per atom ID

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

Nezapomeň: označ upřímně **C** i guidance irelevantní.
