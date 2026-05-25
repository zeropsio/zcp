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

## Env-roll timing — wait for new container before SSH-probing

`run.envVariables` set in zerops.yaml only appear in the container's
environment AFTER the container restarts with the new zerops.yaml.
Sequence:

1. `zerops_deploy` ships the zerops.yaml.
2. Platform stops the old container, starts a new one with the new
   `run.envVariables`.
3. SSH into the new container — env vars now present.

If you SSH in BEFORE step 2 completes, env vars from your new
zerops.yaml will not yet be set. The fix is to wait, NOT to write a
wrapper script that re-exports them. Wrapper scripts that duplicate
`run.envVariables` mapping become dead code as soon as the next
deploy lands.

For dev-pair carve-out (omitted `run.start` + manual SSH workflow),
the env vars ARE set on the idle container — porter SSHing in after
container roll inherits them. No wrapper needed.

## Env-wrapper-script ban

Do NOT author a script (`start-dev.sh`, `run.sh`, `entrypoint.sh`,
`bootstrap.sh`, etc.) whose only job is to re-export env vars and
chain into the framework's start command:

```sh
# BAD — dead code as soon as zerops.yaml run.envVariables maps the same keys.
#!/bin/sh
export NATS_HOST="$broker_hostname"
export NATS_PORT="$broker_port"
export NATS_USER="$broker_user"
export NATS_PASS="$broker_password"
exec npm run start:dev
```

When zerops.yaml `run.envVariables` already maps the same keys
(`NATS_HOST: ${broker_hostname}`), the platform sets them on the
container at boot. Use the `start:` field directly:

```yaml
# GOOD — start: invokes the framework command; envVariables already wired.
run:
  envVariables:
    NATS_HOST: ${broker_hostname}
    NATS_PORT: ${broker_port}
    NATS_USER: ${broker_user}
    NATS_PASS: ${broker_password}
  start: npm run start:dev
```

If env vars genuinely fail to materialize, that's the env-roll
timing trap above — wait for the new container, do NOT paper over
with a wrapper.
