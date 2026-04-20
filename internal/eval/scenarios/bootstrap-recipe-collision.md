---
id: bootstrap-recipe-collision
description: Project already has a managed `db` (postgres) — any recipe whose import YAML tries to create its own `db` collides. Agent must read the collisions annotation and choose a non-breaking path.
seed: deployed
fixture: fixtures/managed-only-postgres.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # Discovery must have surfaced the collision annotation for the LLM
    # to see. This pattern proves the LLM actually received it — the
    # scenario's whole point is gating on visibility of this signal.
    - '"collisions":["db"]'
  # A blind classic or blind recipe route would attempt to create a second
  # `db` service and fail with serviceStackNameUnavailable. If either of
  # these surfaces in the log, the agent didn't handle the collision.
  forbiddenPatterns:
    - "serviceStackNameUnavailable"
    - "hostname already in use"
  requireAssessment: true
followUp:
  - "Objevila se v odpovědi discovery `collisions:` anotace? Na jakém hostname a u jakého recipe option?"
  - "Jak jsi tu kolizi vyřešil? Reuse existující `db` přes EXISTS resolution, rename v importu, nebo něco jiného? Proč tahle cesta?"
  - "Co by se stalo, kdybys přesto spustil recipe route bez ohledu na kolizi?"
---

# Úkol

Potřebuju Node.js REST API pro úkoly s PostgreSQL — tři endpointy:
`GET /todos` (list), `POST /todos` (vytvoří novou, body `{title}`),
`DELETE /todos/:id` (smaže). Schéma `todos (id, title, created_at)`.

Výsledek na veřejné subdomeně, HTTP 200 na `GET /todos` (i prázdné pole
je OK).
