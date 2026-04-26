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
URL. Build-time-baked references (Vite `define`, Webpack
`DefinePlugin`, Astro/Next/SvelteKit static-site builds) must order
the dependency's first deploy BEFORE consuming the alias — otherwise
the build container reads the literal token and the bundle ships with
`${apistage_zeropsSubdomain}` baked in instead of the resolved URL.

For runtime references (`process.env.APISTAGE_URL` read at request
time), the alias resolves on container start — no ordering concern.
The race only bites build-time consumers.

Recovery for build-time consumers: deploy the target service first,
verify the subdomain is minted, THEN trigger the consumer's build.
Parallel scaffold dispatch makes this race visible — an SPA build
running in parallel with the api's first deploy is the canonical
scenario.

`${zeropsSubdomainHost}` is a different beast — it's the
deliverable-template variable that stays LITERAL in published
import.yaml; the platform substitutes the end-user's host suffix at
their click-deploy. Use only inside finalize-phase tier yaml
templates.

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
