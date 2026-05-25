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

Cross-service env vars are reachable as interpolation tokens in the
recipe yamls (`${db_hostname}`, `${cache_port}`, `${broker_user}`,
`${storage_apiUrl}`, `${<host>_zeropsSubdomain}`, etc.). The app
process sees a cross-service value as an OS env var ONLY when you
declare an alias in `zerops.yaml run.envVariables`. Without the
declaration the value is not in the process env at all.

**Canonical pattern** — own-key aliases:

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
const host = process.env.DB_HOST;
const port = process.env.DB_PORT;
```

Left-hand keys are what code reads; right-hand tokens resolve at
container start. Swapping a managed service later is a yaml-only
edit — code keeps reading `DB_HOST`.

**Project-level vars are different** — vars set at project scope
(`APP_SECRET`, `JWT_SECRET`, `API_URL` set at project level) auto-
inherit into every container, runtime and build. No `run.envVariables`
declaration needed — `process.env.APP_SECRET` works directly.

**Self-shadow trap on project vars only.** Re-declaring a
project-level var under the same name in `run.envVariables`
(`API_URL: ${API_URL}`) overrides the auto-inherited value with the
unresolved literal `${API_URL}` — the interpolator doesn't recurse
back to project scope on the right-hand side. Symptom: `process.env
.API_URL` is the literal string. Fix: delete the redundant line.

**Cross-service vars do not have this trap** — without a declaration
in `run.envVariables` the value isn't in the process env, so there's
nothing to shadow. Each cross-service alias is a NEW entry under
your own key.

Reference: `zerops_knowledge query=env-var-model`. Atoms:
`develop-env-var-model`, `develop-env-var-shell-usage`,
`develop-reserved-env-names`.

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
