# setup: worker — rules

The worker setup runs background job processors — no HTTP surface, queue-consumer start command, and a lifecycle driven by broker messages. Showcase recipes only. Whether the worker shares a codebase with its host target is set at research time via `sharesCodebaseWith`.

## Two shapes — shared codebase and separate codebase

- **Shared codebase** (`sharesCodebaseWith` is set to the host target's hostname): write a `setup: worker` block in the **same** zerops.yaml as the host target. No `workerdev` service exists; the agent starts both the web server and the queue consumer as SSH processes from the host target's dev container. The shared worker inherits the host target's `build.base` and `cache` — only `start` differs from the host's setups.
- **Separate codebase** (`sharesCodebaseWith` is empty — the default): the worker has its own repo with its own zerops.yaml containing `setup: dev` and `setup: prod`. Mount path is `/var/www/workerdev/`. This covers the three-repo case.

## Worker setup fields

- `start` — mandatory. The command that launches the broker consumer (`node dist/worker.js`, `php artisan queue:work`, `python worker.py`, or the framework's equivalent).
- `build` and `envVariables` — match prod. The worker is a second runtime role on top of the same application code, so the build output and the managed-service references align with the host or dedicated-worker prod setup.
- `ports` — not declared. Workers do not serve HTTP.
- `healthCheck` and `readinessCheck` — not declared. Platform health probes assume an HTTP surface; workers have none.

## Queue-consumer conventions

NATS subscribers declare `queue: '<plan.SymbolContract.NATSQueues[role]>'` so multiple worker replicas form one competing-consumer group — each message is processed by exactly one replica, not broadcast across all of them. Subject names and queue names both come from the frozen `SymbolContract` interpolated into the scaffold brief; the worker's code and the zerops.yaml setup reference the same values.

## Lifecycle signals

Workers register a SIGTERM handler that drains in-flight work before exiting. The `graceful-shutdown` fix-recurrence rule in `plan.SymbolContract.FixRecurrenceRules` covers the positive form for each runtime family — the scaffold sub-agent is responsible for the implementation; this atom names what the zerops.yaml side does (nothing extra beyond the start command, since SIGTERM delivery and restart policy are platform defaults).

## What does not belong in the worker setup

`envVariables` carries cross-service references and mode flags only, per `env-var-model.md`. No HTTP-specific configuration (ports, health checks, readiness checks). No UI, no static file serving. Build output and cross-service references stay in sync with the prod setup of the host target (shared codebase) or with the worker's own prod setup (separate codebase).
