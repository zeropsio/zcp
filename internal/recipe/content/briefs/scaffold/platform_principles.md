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

Cross-service env vars auto-inject project-wide under platform-specific
keys (`${db_hostname}`, `${cache_port}`, `${broker_user}`,
`${storage_apiUrl}`, `${<host>_zeropsSubdomain}`, etc.). Do NOT read
those names directly in code — the code becomes platform-coupled.

**Recommended pattern** — declare own-key aliases in
`zerops.yaml run.envVariables`; code reads the own-key names:

```yaml
run:
  envVariables:
    DB_HOST: ${db_hostname}
    DB_PORT: ${db_port}
    DB_PASSWORD: ${db_password}
    APP_URL: ${zeropsSubdomain}
    API_URL: ${apistage_zeropsSubdomain}
    NODE_ENV: production
```

```js
// application code
const host = process.env.DB_HOST;
const port = process.env.DB_PORT;
```

The keys on the left differ from the platform-side keys on the right.
Different-key aliasing does NOT self-shadow — the platform injects
`db_hostname=...` and your envVariables writes `DB_HOST=...`; two
distinct env entries.

**Same-key shadow trap** — declaring `db_hostname: ${db_hostname}`
(SAME key as the platform's auto-inject) self-shadows. The
per-service envVariables write runs after the project-wide
auto-inject; the literal `${db_hostname}` token wins; the OS env var
becomes the literal string. Symptom: NATS `Invalid URL`, Postgres
`getaddrinfo ENOTFOUND ${db_hostname}`. Never use the same key as
the source.

Reference: `internal/knowledge/guides/environment-variables.md` —
fetch via `zerops_knowledge query=env-var-model` for the full
treatment of project vs cross-service vars, build-time vs runtime
scopes, and isolation modes.

## Migrations / init-commands

See the included `execOnce — key shape by lifetime` atom for the two
key shapes + in-script-guard pitfall + decomposition rule.

## Rolling deploys

Accept `SIGTERM`, drain, exit. `minContainers: 2` at tier 4+.
