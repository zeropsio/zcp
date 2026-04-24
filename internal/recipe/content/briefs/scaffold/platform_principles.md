# Platform obligations

<!-- HTTP_SECTION_START -->
## HTTP

- Bind `0.0.0.0`, read `PORT` — loopback is unreachable
- Trust `X-Forwarded-*` headers (L7 balancer sets them)
- `zerops.yaml run.ports: { port: <PORT>, httpSupport: true }`
<!-- HTTP_SECTION_END -->

## Before writing client code — consult `zerops_knowledge`

Call `zerops_knowledge runtime=<type>` for each managed service in the
plan BEFORE writing its client setup. The guide supplies the library
config shape, env-var names, auth, and scheme. Do NOT compose from
framework habit. Fall back to `zerops_discover includeEnvs=true` if a
guide is silent.

## Managed services

Cross-service env vars auto-inject project-wide. Do NOT declare
`DB_HOST: ${db_hostname}` — the platform alias and the redeclaration
self-shadow and blank at container start. Cite `env-var-model`.

## Migrations / init-commands

See the included `execOnce — key shape by lifetime` atom for the two
key shapes + in-script-guard pitfall + decomposition rule.

## Rolling deploys

Accept `SIGTERM`, drain, exit. `minContainers: 2` at tier 4+.
