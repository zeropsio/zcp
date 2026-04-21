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
  # `serviceStackNameUnavailable` and `hostname already in use` are NOT
  # in forbiddenPatterns. Observation (2026-04-21 run): even when the
  # collision annotation is clearly visible in the discovery response,
  # the LLM sometimes tries the import anyway, gets the platform error,
  # and recovers by switching strategy. That's not the ideal path but
  # it IS recoverable — State: SUCCESS on final assessment. Treating it
  # as FAIL would turn this scenario into a lottery on LLM
  # "proactiveness" rather than a test of "did the signal reach the
  # agent and did the task complete." The required collisions pattern
  # above covers the signal-visibility check; the assessment covers
  # completion. Collision-respect-before-import is a soft goal — probe
  # it in follow-up answers, not as a hard gate.
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
