---
id: greenfield-node-postgres-dev-stage
description: |
  Greenfield Node + Postgres dashboard via bootstrap recipe-route, with
  develop first-deploy on a dev/stage pair. Reproduces eval-session 1.
seed: empty
tags: [bootstrap, recipe-route, develop, dev-stage, fullstack, node, postgres, first-deploy]
area: bootstrap-and-develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
  noFailedProcesses: true
  liveness: {service: appstage, marker: "team-notes"}
  nodePostgresRecord: {stage: appstage, database: db}
  never: ["zerops_import{override=true}"]
notableFriction:
  # Informational only — does NOT gate anything. Helps the assistant in
  # the local Claude Code session know what to look for in the retrospective.
  - id: trap-1
    description: |
      bootstrap recipe-route discover plan retries — agent stringifies the
      array OR submits flat shape without `runtime: { ... }` wrapper.
    suspectedCauses:
      - internal/tools/workflow.go:37 (Plan jsonschema, no string warning)
      - internal/content/atoms/bootstrap-recipe-match.md (no JSON example)
  - id: trap-2
    description: |
      dev-mode dynamic-runtime first-deploy verify returns HTTP 502 because
      the dev process never started — agent must call zerops_dev_server first.
    suspectedCauses:
      - internal/content/atoms/develop-first-deploy-verify.md (no precondition)
      - internal/content/atoms/develop-dev-server-triage.md (timing-locked gate)
---

Build me a small team-notes dashboard with a Node backend and Postgres.
I want both a dev and a stage service.

Implement `POST /records` (JSON body `{"nonce": "...", "value": "..."}` → `201` with `{id, nonce, value, environment: "stage"}`, persisted to the Postgres `db` service) and `GET /records/:id` returning the same shape on `appstage`, and make sure the dashboard page served at `/` mentions "team-notes" in its body.
