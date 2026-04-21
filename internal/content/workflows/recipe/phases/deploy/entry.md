# Deploy — phase entry

This phase completes when every substep's predicate holds on the mount. `zerops_workflow action=status` shows the authoritative substep list; read it first, then work each substep in the order the server returns.

## What deploy does

`zerops_deploy` processes the zerops.yaml through the platform. This is the step where `run.envVariables` become OS env vars and cross-service references (`${hostname_varname}`) resolve to real values. Before this step, the dev container has no service connectivity. After this step, the app is fully configured.

## Execution order by recipe shape

The substep numbers are reference labels, not a linear script. For dual-runtime (API-first) recipes the substeps interleave because the frontend depends on the API being verified first.

| Recipe shape | Order |
|---|---|
| Single-runtime, minimal | deploy-dev → start-processes → verify-dev → init-commands → cross-deploy-stage → verify-stage → feature-sweep-stage → readmes → completion |
| Single-runtime, showcase | deploy-dev → start-processes → verify-dev → init-commands → subagent → snapshot-dev → feature-sweep-dev → browser-walk-dev → cross-deploy-stage → verify-stage → feature-sweep-stage → readmes → completion |
| Dual-runtime (API-first), minimal | API-side deploy-dev + start + verify first, then appdev deploy-dev + start + verify, then init-commands on both, then cross-deploy-stage → verify-stage → feature-sweep-stage → readmes → completion |
| Dual-runtime (API-first), showcase | API-first order above, extended with subagent → snapshot-dev → feature-sweep-dev → browser-walk-dev between verify-dev and cross-deploy-stage |

The deploy parameter is `targetService` (not `serviceHostname`). `serviceHostname` is the parameter name used by `zerops_mount`, `zerops_subdomain`, `zerops_verify`, `zerops_logs`, `zerops_env`. Deploy is the exception; if you pass `serviceHostname` to deploy you get `unexpected additional properties ["serviceHostname"]`.

## Failure-class decoder

`zerops_deploy` returns a `status` field. Each value names which lifecycle phase failed, where the stderr lives, and where the fix goes.

| status | Phase | Where stderr lives | Fix location |
|---|---|---|---|
| `ACTIVE` | — | — | Success. |
| `BUILD_FAILED` | Build container (`/build/source/`) | `buildLogs` field in deploy response | `zerops.yaml` `build.buildCommands` |
| `PREPARING_RUNTIME_FAILED` | Runtime prepare (before deploy files arrive) | `buildLogs` field (historical naming) | `zerops.yaml` `run.prepareCommands`. Common cause: referencing `/var/www/` paths that do not exist yet — use `addToRunPrepare` with `/home/zerops/` targets instead. |
| `DEPLOY_FAILED` | Runtime init (container started, deploy files at `/var/www/`) | Runtime logs — `zerops_logs serviceHostname={service} severity=ERROR since=5m`, NOT buildLogs | `zerops.yaml` `run.initCommands`. The deploy response's `error.meta[].metadata.command` field names the specific initCommand that exited non-zero. Common cause: a buildCommand baked `/build/source/...` paths into a compiled cache that does not resolve at runtime — move `config:cache` / `asset:precompile`-style commands from `buildCommands` into `run.initCommands`. |
| `CANCELED` | — | — | User or system canceled; redeploy. |

On `DEPLOY_FAILED` the response includes a structured `error` field that names the failing command, e.g. `{"error":{"code":"commandExec","meta":[{"metadata":{"command":["php artisan migrate --force"],"containerId":["..."]}}]}}`. This tells you *which* initCommand failed. For *why* it failed, fetch runtime logs on the target service — the stderr is there, not in buildLogs.

## Fact recording during deploy

Every non-trivial fix, verified platform behavior, cross-codebase contract, and known-trap observation during deploy is logged via `zerops_record_fact` at the moment of freshest knowledge. Facts with `scope: "content"` route to the readmes substep's writer dispatch. Facts with `scope: "downstream"` route to the next sub-agent's dispatch brief. This phase records facts; it does not narrate them — README authorship lives at the readmes substep.
