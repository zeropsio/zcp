---
id: greenfield-fullstack-multi-runtime
description: |
  Greenfield fullstack architecture: Next.js SSR frontend + Node API
  + Postgres. Two runtime services, one managed dep. Tests
  multi-runtime plan submission, cross-service env wiring (frontend
  reads `${api_*}`, api reads `${db_*}`), per-runtime mode selection
  (both standard or both dev or asymmetric), and ordered first-deploy
  on multiple runtimes. Surfaces atoms covering multi-runtime
  bootstrapping which single-runtime scenarios cannot exercise.
seed: empty
tags: [bootstrap, multi-runtime, fullstack, nextjs, node, postgres, env-wiring, first-deploy-ordering]
area: bootstrap-and-develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4
  expectedServices:
    - hostname: api
      status: [ACTIVE]
      type: nodejs@*
    - hostname: web
      status: [ACTIVE]
      type: nginx@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
    - hostname: cache
      status: [ACTIVE]
      type: valkey@*
  noFailedProcesses: true
  liveness: {service: web, marker: "multiruntime-dashboard"}
  containerCheck:
    - {service: api, cmd: "printenv db_hostname", match: ".+"}
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  You are building a small fullstack app: a Next.js frontend that
  talks to a Node API which reads from Postgres. You want both
  runtimes deployable. Dev + stage pair on each is fine. You expect
  the agent to wire env vars between services so the frontend knows
  the API URL and the API knows the database connection. Managed
  catalog substitutions are fine. Push back if the agent collapses
  this into a single runtime.
notableFriction:
  - id: multi-runtime-plan-shape
    description: |
      Plan submission must include both runtimes plus the managed dep
      in one envelope. Surfaces whether the plan-shape atom telegraphs
      multi-runtime layout (array of runtime entries, not a single
      object).
  - id: cross-service-env-wiring
    description: |
      Frontend needs the API service URL; API needs db connection
      vars. Surfaces whether provision atoms describe the
      `${service_*}` interpolation contract for cross-service env
      vars without hand-edit.
  - id: first-deploy-ordering
    description: |
      Two runtimes, both never deployed. Surfaces whether the develop
      flow handles parallel first-deploys or whether ordering matters
      (db wait → api → frontend).
---

Build me a small fullstack app on Zerops: a Next.js frontend (statically exported, served as `web`) that talks to a Node API (`api`), with Postgres (`db`) for storage and a small Redis-compatible cache (`cache`) for session data. I want dev environments on both `web` and `api`, plus a staging slot for each. Make sure the dashboard page served at `/` on `web` contains the text "multiruntime-dashboard" somewhere in its body.
