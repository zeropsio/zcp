# Dual-runtime — URL shapes and source-code consumption

Dual-runtime recipes require two halves to stay correct together: the zerops.yaml half (env vars into the bundle) and the source-code half (the bundle actually reads the baked value). Each half is necessary; both must be right or the stage frontend silently breaks.

## URL pattern — the platform constant

Every service has a deterministic public URL derived from its `${hostname}`, the project-scope `${zeropsSubdomainHost}` env var, and its HTTP port. Static services omit the port segment. The format is a platform constant:

```
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app   # dynamic runtime on port N
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app          # static (Nginx, no port segment)
```

Dual-runtime recipes use two env-var name families:

- `STAGE_{ROLE}_URL` is present in every env (0 through 5) — resolves to `{role}stage` in envs 0-1 and the bare `{role}` in envs 2-5.
- `DEV_{ROLE}_URL` exists only in envs 0-1 (dev-pair envs) — resolves to `{role}dev`.

Typical roles are `API` and `FRONTEND`. Add `WORKER` only when the worker has a public surface (usually it does not).

### Envs 0-1 (dev-pair — `STAGE_*` + `DEV_*`)

```yaml
# import.yaml for env 0 and env 1
project:
  envVariables:
    DEV_API_URL: https://apidev-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app
    STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app
```

### Envs 2-5 (single-slot — `STAGE_*` only)

```yaml
# import.yaml for envs 2, 3, 4, 5
project:
  envVariables:
    STAGE_API_URL: https://api-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app
```

Substitute `{apiPort}` with the API's actual HTTP port (`run.ports[0].port` in the API's zerops.yaml). Static frontends have no port segment.

## Half 1 — zerops.yaml half (env vars into the bundle)

Framework-bundled dev servers (Vite, webpack dev server, Next dev, Nuxt dev) read `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` / equivalent at dev server startup, not at build time. For `setup: dev`, the client-side vars live in `run.envVariables`. For `setup: prod`, they live in `build.envVariables` because prod builds bake the values into the bundle.

```yaml
zerops:
  - setup: dev
    run:
      base: nodejs@22
      envVariables:
        # Client-side vars live in run.envVariables so the Vite/webpack/Next
        # dev server picks them up at startup. build.envVariables is
        # build-time only and dev servers do not run a build step.
        VITE_API_URL: ${DEV_API_URL}
        NODE_ENV: development

  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        # Client-side vars in build.envVariables get baked into the bundle.
        # This is the prod pattern: `npm run build` substitutes at build time.
        VITE_API_URL: ${STAGE_API_URL}
    run:
      base: static
```

`setup: dev` reads `DEV_*`; `setup: prod` reads `STAGE_*`. The same zerops.yaml works in every env because envs 2-5 never invoke `setup: dev` (there is no `appdev` there); the `DEV_*` reference is dormant and safe.

## Half 2 — source-code half (a single API helper reads the baked value)

Baking an env var into the bundle is useful only if code reads it. Every dual-runtime scaffold includes one API helper module that reads the baked env var and prefixes every API call; components call the helper rather than `fetch()` directly.

```ts
// src/lib/api.ts (or the equivalent for your framework)
// One helper. Reads the baked env var, defaults to empty string so that
// dev's bundler proxy handles relative paths unchanged.
const BASE = (import.meta.env.VITE_API_URL ?? "").replace(/\/$/, "");

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const url = `${BASE}${path}`;
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`API ${res.status} ${res.statusText} ${path}: ${body.slice(0, 200)}`);
  }
  const ct = res.headers.get("content-type") ?? "";
  if (!ct.toLowerCase().includes("application/json")) {
    throw new Error(`API ${path} returned non-JSON content-type ${ct} — likely SPA fallback, check VITE_API_URL baking`);
  }
  return res;
}

// Components consume the helper:
// const res = await api("/api/items");
// const items = await res.json();
```

## Why the content-type check is mandatory

Nginx's `try_files ... /index.html` SPA fallback returns HTTP 200 with `text/html` for any unknown path. A bare `fetch('/api/items').then(r => r.json())` in an `appstage` container throws a silent `SyntaxError: Unexpected token '<'` that most frameworks catch into an empty-state render. The user sees a dashboard with zero items. The helper above surfaces the condition visibly — the caller sees a thrown error whose message names the likely cause.

## Consumption model

Project-level env vars auto-inject into both runtime and build containers. Reference them directly by name in zerops.yaml — `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bakes the stage URL into the cross-deployed bundle; `run.envVariables: FRONTEND_URL: ${STAGE_FRONTEND_URL}` forwards the value under a framework-conventional name (useful for CORS).

Destination and source names differ whenever the forward-under-different-name pattern applies — `FRONTEND_URL: ${STAGE_FRONTEND_URL}` is a forward; `FRONTEND_URL: ${FRONTEND_URL}` is a self-shadow and belongs to the env-var-model atom's grep check.

## Setup names — only `dev` and `prod`

Stage deploys use `setup: prod`. There is no `setup: stage`. For URL building, reference the platform's project-scope `${zeropsSubdomainHost}` variable plus the constant URL format above — not another service's `${hostname}_zeropsSubdomain`.

## Project-level workspace setup

The workspace-level `DEV_*` and `STAGE_*` variables land during the provision step via `zerops_env project=true action=set`. By the time this substep runs, they resolve cleanly. Single-runtime recipes skip this entirely — they do not cross services for URL baking.

## What the deploy step enforces

The `feature-sweep-dev` and `feature-sweep-stage` sub-steps at deploy run `curl` against every API-surface feature's health check. Any response with `text/html` content-type is rejected — the exact symptom of a missing source-code half. A zerops.yaml-perfect recipe with the source-code half wrong fails the sweep before it reaches the browser walk.
