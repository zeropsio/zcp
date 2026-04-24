---
id: greenfield-nodejs-todo
description: Node.js REST API + Postgres — greenfield, two-step bootstrap discovery → recipe/classic commit, develop scaffolds + first-deploys
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 9
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    # JSON keys serialize alphabetically — "action":"start" is NOT adjacent
    # to "workflow":"bootstrap". Check each fragment independently.
    - '"workflow":"bootstrap"'
    - '"route":"'
  forbiddenPatterns:
    - "app-<projectId>"
  requireAssessment: true
  finalUrlStatus: 200
  # Hostname intentionally unset. "Sloučená prod+dev setup" leaves the LLM
  # free to pick simple (`app`) or dev-only (`appdev`) or standard
  # (`appdev`+`appstage`). The probe resolver picks whichever service has
  # subdomain enabled + exposes ports — works across all three modes as
  # long as the agent enables the subdomain on one of them.
followUp:
  - "Jak vypadal tvůj první `zerops_workflow action=\"start\" workflow=\"bootstrap\"` call — s jakým `route` parametrem? A co ti to vrátilo?"
  - "Zvolil jsi route=recipe nebo route=classic? Pokud recipe, který slug a proč ten a ne jiný kandidát?"
  - "Kdy jsi poprvé zavolal zerops_deploy — uvnitř bootstrap session, nebo až v develop? Proč to tak musí být?"
  - "Jak jsi spustil server po deployi (nebo v dev mode)?"
  - "Proč dynamic runtime (nodejs) vyžaduje manual start, zatímco static runtime (php-nginx) ne?"
---

# Úkol

Vytvoř REST API pro TODO items v Node.js s PostgreSQL databází. Framework je
na tobě (Express / Fastify / Hono).

Endpointy:

- `GET /todos` — vrátí seznam všech todo položek
- `POST /todos` — vytvoří novou položku (body: `{ title: string }`)
- `DELETE /todos/:id` — smaže položku

Požadavky:

- Node.js 22+ runtime.
- PostgreSQL 18 (schéma `todos (id, title, created_at)` se vytvoří při startu).
- Sloučená prod + dev setup.

Verify: `GET /todos` vrátí 200 s validním JSON arrayem (i prázdným).

Bootstrap flow (důležité):

- **První `start` call bez `route` parametru je discovery** — vrátí
  `routeOptions[]` (ranked list: recipe kandidáti / classic / adopt /
  resume). Žádná session se nevytváří.
- **Druhý call musí mít `route="<zvolený>"` (recipe vyžaduje `recipeSlug`)**
  — teprve tohle commitne session.
- Bootstrap **nikdy nedeployí**. Provisionuje services, mount, env var
  discovery — a končí.
- **Develop** scaffolduje `zerops.yaml`, napíše aplikaci a spustí první
  deploy. Passing verify překlopí `deployed=true` v envelope (Services
  block).
