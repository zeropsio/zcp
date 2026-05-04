---
id: develop-add-managed-dep-to-existing
description: |
  Existing Node dev/stage pair with Postgres, all healthy. User wants
  to add Valkey (Redis-compatible) to cache a heavy query. Tests the
  develop-add-service flow: agent adopts existing topology, then
  imports an additional managed service into the existing project,
  wires env vars on the runtime, and verifies the runtime still
  reaches the cache. Counterpart to greenfield bootstrap — exercises
  growing-the-stack atoms which initial-bootstrap scenarios cannot.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [develop, add-service, managed-dep, valkey, env-wiring, existing-stack]
area: develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Your Node app `appdev` is healthy with Postgres but a particular
  endpoint is slow because it re-runs an expensive query. You want
  to add Valkey (or Redis-compatible cache) and wire it so the
  Node app can read/write to it. You don't want to rebuild the app
  from scratch and you don't want to touch `appstage` until dev is
  validated. Trust catalog substitutions when the agent has a clear
  reason. Push back if the agent proposes recreating the project or
  jumping straight to a stage deploy.
notableFriction:
  - id: add-managed-dep-flow
    description: |
      Growing the stack on an existing project means a partial import
      (the new service only) plus env wiring on the runtimes that
      consume it. Surfaces whether atoms describe partial-import
      shape and `${cache_*}` env interpolation correctly.
  - id: scoped-to-dev-only
    description: |
      User explicitly excludes `appstage` from this iteration.
      Surfaces whether the develop scope atom honours scoped-dev-only
      when the topology is a standard pair.
---

The `appdev` Node app is working but the `/api/dashboard` endpoint runs the same heavy Postgres query on every request. Add a Redis-compatible cache to the project and wire it into appdev so we can cache the query. Don't touch `appstage` yet — I want to validate on dev first.
