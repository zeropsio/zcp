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

**Same-key shadow trap** — declaring any per-service env under the
SAME key as something already auto-injected self-shadows. The
per-service `envVariables` write runs after the auto-inject; the
literal `${KEY}` token wins; the OS env var becomes the literal
string. Symptom: NATS `Invalid URL`, Postgres
`getaddrinfo ENOTFOUND ${db_hostname}`, JWT signing produces literal
"${APP_SECRET}" hashes.

The rule applies identically to:
- **Cross-service auto-injects** — declaring
  `db_hostname: ${db_hostname}` (or any
  `<peer-host>_<key>: ${<peer-host>_<key>}`) self-shadows.
- **Project-level envs** — declaring `APP_SECRET: ${APP_SECRET}` (or
  `STAGE_API_URL: ${STAGE_API_URL}`, or any project-level secret /
  URL constant under its own name) self-shadows the same way.

Project-level envs and cross-service envs both auto-propagate to
every container. Re-declaring them under the same name is never
necessary.

**Right pattern**: rename to a different own-key
(`API_SIGNING_KEY: ${APP_SECRET}` or `DB_HOST: ${db_hostname}`), OR
omit the per-service declaration entirely (the auto-inject is already
in the container's env).

Reference: `internal/knowledge/guides/environment-variables.md` —
fetch via `zerops_knowledge query=env-var-model` for the full
treatment of project vs cross-service vars, build-time vs runtime
scopes, and isolation modes.

## Alias-type contracts

The platform injects cross-service references under predictable
shapes. Use them as-is; do not compose, prefix, or transform.

| Alias pattern                  | Resolves to                              | Use as                                  |
|--------------------------------|------------------------------------------|-----------------------------------------|
| `${<host>_hostname}`           | bare hostname (`db`)                     | host in `host:port` URLs                |
| `${<host>_port}`               | port number                              | port                                    |
| `${<host>_user}`               | username                                 | auth user                               |
| `${<host>_password}`           | password                                 | auth pass                               |
| `${<host>_<keyname>}`          | the value as-is                          | direct value                            |
| `${<host>_connectionString}`   | full DSN                                 | pass to client constructor              |
| `${<host>_zeropsSubdomain}`    | **full HTTPS URL** (e.g. `https://apistage-2204-3000.prg1.zerops.app`) | Origin / Host / fetch URL — do NOT prepend `https://` |
| `${zeropsSubdomain}`           | **this service's own full HTTPS URL**    | APP_URL, callback URL, redirect target  |

**Resolution timing.** `${<host>_zeropsSubdomain}` is a literal token
(`${...}` verbatim) until the target service's first deploy mints the
URL. For runtime references (`process.env.APISTAGE_URL` read at
request time), the alias resolves on container start — no ordering concern.

**Build-time-baked references** (Vite `define`, Webpack
`DefinePlugin`, Astro/Next/SvelteKit static-site builds) use the
`${zeropsSubdomainHost}` workspace pattern instead — see the
included `cross-service-urls.md` principle. `${zeropsSubdomainHost}`
is a project-scope env var that resolves at provision time, before
any peer service deploys, so build-time bake works without an
ordering dance.

The deploy-peer-first fallback is a last resort, not the canonical
fix. Reach for the project-envs pattern first.

When the ORIGIN must be derived (CORS allow-list, Referer check):

```js
const origins = [
  process.env.APISTAGE_URL,  // own-key alias of ${apistage_zeropsSubdomain}
  process.env.APIDEV_URL,    // own-key alias of ${apidev_zeropsSubdomain}
].filter(Boolean);
// The values are already full https:// URLs — do NOT prepend.
```

## Migrations / init-commands

See the included `execOnce — key shape by lifetime` atom for the two
key shapes + in-script-guard pitfall + decomposition rule.

## Rolling deploys

Accept `SIGTERM`, drain, exit. `minContainers: 2` at tier 4+.
