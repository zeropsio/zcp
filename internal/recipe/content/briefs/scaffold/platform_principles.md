# Platform obligations

## HTTP surface (ServesHTTP=true)

- Bind `0.0.0.0`, read `PORT` env var — loopback is unreachable.
- Trust `X-Forwarded-*` (the L7 balancer sets them).
- `zerops.yaml run.ports`: `{ port: <PORT>, httpSupport: true }`.

## Before writing client code — consult `zerops_knowledge`

For each managed service in the plan, call
`zerops_knowledge runtime=<type>` or
`zerops_knowledge query="<service> connection"` BEFORE writing client
setup. The guide supplies the library config shape, exact env-var
names, auth expectations, and scheme. Do NOT compose from framework
habit — `nats://user:pass@host` URLs when the library takes separate
`{servers, user, pass}` fields, or `http://` on object-storage (it's
`https://`), become self-inflicted bugs that classify as
`framework-quirk` and get discarded at editorial-review.

Fall back to `zerops_discover includeEnvs=true` output when the guide
is silent — that catalog is authoritative for which keys to read.

## Managed services — platform rules

- Cross-service env vars auto-inject project-wide. Do NOT declare
  `DB_HOST: ${db_hostname}` — the platform copy and the alias collide
  and blank at container start (self-shadow). Cite `env-var-model`.

## Migrations / one-time setup

- `initCommands`: `zsc execOnce <static-key> -- <cmd>`. Pair with
  `--retryUntilSuccessful`. Cite `execOnce`.
- execOnce marks the key successful on exit 0 — silent no-ops burn it.

## Rolling deploys

- Accept `SIGTERM`, drain, exit. Zerops sends it before removing the
  container from the balancer.
- `minContainers: 2` at tier 4+ — two replicas run simultaneously.
