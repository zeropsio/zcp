---
id: bootstrap-recipe-collision
description: Project already has a managed `db` (postgres) — any recipe whose import YAML tries to create its own `db` collides. Agent must read the collisions annotation and adopt the existing managed service via Dependency.resolution=EXISTS (F6 rename/adopt contract).
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
    # to see. This pattern proves the LLM actually received it.
    - '"collisions":["db"]'
  # F6 contract (2026-04-24): collision-before-import is a HARD gate.
  # bootstrap-recipe-match atom teaches the agent to set
  # `resolution: "EXISTS"` on the matching managed dependency when the
  # colliding service has the same runtime type. Platform's
  # `serviceStackNameUnavailable` error only fires when the agent
  # ignored the guidance and submitted the YAML as-is — that's now a
  # test failure (no more soft-goal recovery lottery).
  forbiddenPatterns:
    - 'serviceStackNameUnavailable'
    - 'hostname already in use'
  requireAssessment: true
  finalUrlStatus: 200
followUp:
  - "Objevila se v odpovědi discovery `collisions:` anotace? Na jakém hostname a u jakého recipe option?"
  - "Jak jsi tu kolizi vyřešil? Reuse existující `db` přes EXISTS resolution, rename runtime hostnames v planu, nebo něco jiného? Proč tahle cesta?"
  - "Co konkrétně dělá `resolution: \"EXISTS\"` na dependency v bootstrap planu — kdy se služba vytváří a kdy ne?"
---

# Úkol

Potřebuju Node.js REST API pro úkoly s PostgreSQL — tři endpointy:
`GET /todos` (list), `POST /todos` (vytvoří novou, body `{title}`),
`DELETE /todos/:id` (smaže). Schéma `todos (id, title, created_at)`.

Výsledek na veřejné subdomeně, HTTP 200 na `GET /todos` (i prázdné pole
je OK).
