# Behavioral gate

Scaffold verifies the runnable surface only:

1. Deploy succeeds (build passes, runtime starts).
2. `/health` returns 200 (or equivalent for non-HTTP services).
3. ONE happy-path read endpoint returns a valid shape.

Cross-service fan-out, behavior matrices, end-to-end smoke tests,
multi-step user flows — these belong in **feature phase**. Do NOT
exercise every managed service from scaffold; trust scaffold to
produce a deployable shell, feature to verify behavior.

In scope at scaffold (runtime-required):
- `initCommands` (migrations, seed bootstrap) must succeed — without
  them the runtime won't boot.
- Trust-proxy / SIGTERM-drain / stderr-clean checks if the runtime
  framework needs them to start cleanly.

Out of scope at scaffold (move to feature):
- POST/PUT roundtrips against managed services (db CRUD, cache hits,
  broker publish/consume).
- Cross-service URL fetches between dev runtimes.
- Behavior matrices (auth flows, panel-by-panel browser exercises).

Record a fact for any deviation from the runnable-surface contract.
