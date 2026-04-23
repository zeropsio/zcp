# Platform obligations

## HTTP surface (ServesHTTP=true)

- Bind `0.0.0.0`, read `PORT` env var — loopback is unreachable.
- Trust `X-Forwarded-*` (the L7 balancer sets them).
- `zerops.yaml run.ports`: `{ port: <PORT>, httpSupport: true }`.

## Managed services

- Cross-service env vars auto-inject project-wide. Do NOT declare
  `DB_HOST: ${db_hostname}` — the platform copy and the alias collide
  and blank at container start (self-shadow). Cite `env-var-model`.
- Valkey: auth-free. Postgres: `{hostname}_{user,password,hostname,port}`.

## Migrations / one-time setup

- `initCommands`: `zsc execOnce <static-key> -- <cmd>`. Pair with
  `--retryUntilSuccessful`. Cite `execOnce`.
- execOnce marks the key successful on exit 0 — silent no-ops burn it.

## Rolling deploys

- Accept `SIGTERM`, drain, exit. Zerops sends it before removing the
  container from the balancer.
- `minContainers: 2` at tier 4+ — two replicas run simultaneously.
