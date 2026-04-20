---
id: greenfield-nodejs-todo
description: Node.js REST API + Postgres (dynamic runtime, SSH start path)
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_verify
  workflowCallsMin: 12
  mustEnterWorkflow:
    - bootstrap
    - develop
  forbiddenPatterns:
    - "app-<projectId>"
followUp:
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
