# Cross-service URLs — workspace + deliverable

Two completely different scopes for cross-service URL composition.
Reaching for the wrong one is the failure mode that bit run-19's
SPA: the build-time-bake trap is real, the recommended fix is
**not** "deploy api first" — that's the fallback, not the canonical
solution.

## The two scopes

| Scope | Purpose | Variable | Resolution timing |
|---|---|---|---|
| **Workspace** (the dev/stage project you're authoring inside) | Cross-service URL composition for build-time bake + runtime CORS allow-list | `${zeropsSubdomainHost}` (project-scope, present at project creation) | **Resolved at provision time** — already a real value when scaffold runs |
| **Deliverable** (the published `import.yaml` for click-deploy) | Same purpose, end-user's project | `${zeropsSubdomainHost}` (literal in published yaml) | Resolved at end-user's click-deploy import |

**Same variable. Two contexts.** In the workspace yaml + project envs
you set with `zerops_env project=true action=set`, the variable is
real; the platform substitutes it at provision time. In the
deliverable tier yaml the engine emits at finalize, the literal token
stays unresolved so each end-user's click-deploy mints fresh values.

The **workspace** scope is what scaffold sub-agents author. The
**deliverable** scope is what the engine emits at finalize. Don't
confuse the two.

## The build-time bake trap

Vite / Webpack `DefinePlugin` / Astro / Next / SvelteKit static
builds inline `import.meta.env.VITE_*` (or equivalent) constants at
**build time**. The build container reads the env, substitutes
literally into the bundle, ships compiled JS. If the env value is a
literal `${apistage_zeropsSubdomain}` token (target service hasn't
deployed yet, alias hasn't minted), the bundle ships with the
literal token string instead of a URL. The browser then fetches
`${apistage_zeropsSubdomain}/api/items` and gets DNS failure.

The trap fires whenever a build-time consumer references a peer
service's `zeropsSubdomain` alias before that peer has had its first
deploy. Parallel scaffold dispatch makes the race common.

## The canonical fix — workspace project envs

Set project-scope env constants derived from `${zeropsSubdomainHost}`
+ the known peer hostname + the peer's port. These resolve at
provision time, before any scaffold sub-agent runs.

**Port-suffix rule (read this BEFORE composing URLs):** when the
runtime exposes httpSupport on a non-default port (Vite dev = 5173,
NestJS = 3000, anything dynamic), the platform-issued subdomain
carries a `-PORT` segment. When the runtime is `base: static`
(Nginx serving built assets on default 80/443), the subdomain has
no port segment. The project-env URL must match what the platform
actually issues:

```
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app   # dynamic runtime, non-default port
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app          # base: static (Nginx default 80/443)
```

In the canonical SPA + API pair, `app{stage}` ships compiled assets
on `base: static` (no port) but `appdev` runs the framework's dev
server on Vite's 5173 (port-suffixed). `api{stage}` runs the
framework's start command on its own port (3000 NestJS / 8000 Django
/ etc) — port-suffixed. So:

```bash
zerops_env project=true action=set \
  STAGE_API_URL="https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app" \
  STAGE_FRONTEND_URL="https://appstage-${zeropsSubdomainHost}.prg1.zerops.app" \
  DEV_API_URL="https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app" \
  DEV_FRONTEND_URL="https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app"
```

`DEV_FRONTEND_URL` carries `-5173` because appdev runs Vite on 5173;
`STAGE_FRONTEND_URL` does not because appstage serves a built bundle
on `base: static`. Match this to whatever ports / runtimes the
recipe's dev/stage pair actually uses (port-suffix follows the
runtime, not the env tier).

The frontend SPA reads `${STAGE_API_URL}` in `build.envVariables` —
build-time bake works because the constant resolved at provision
time, not at peer-service first-deploy time. The api reads
`${STAGE_FRONTEND_URL}` in `run.envVariables` for CORS allow-list at
runtime.

```yaml
# appdev/zerops.yaml — frontend SPA (the `prod` setup block)
zerops:
  - setup: prod
    build:
      envVariables:
        VITE_API_URL: ${STAGE_API_URL}     # bakes a real URL into the bundle
    run:
      base: static                          # see SPA static runtime atom
```

```yaml
# apidev/zerops.yaml — backend API (the `prod` setup block)
zerops:
  - setup: prod
    run:
      envVariables:
        CORS_ORIGINS: ${DEV_FRONTEND_URL},${STAGE_FRONTEND_URL}
```

The `setup:` name MUST be the generic role-contract value (`dev` /
`prod` / `worker`) — never the slot hostname (`appdev` / `appstage` /
`apistage`). Tier import.yamls reference `zeropsSetup: prod` (or
`zeropsSetup: dev`) and the slot mapping happens at the import-yaml
layer (`zeropsSetup` per slot block + `--setup` on workspace deploy).
A slot-named setup in the codebase yaml leaves every tier yaml's
`zeropsSetup: prod` reference orphaned. See `themes/core.md` —
"ALWAYS use generic `setup:` names".

**Naming convention** for the project-env constants:

- `STAGE_{ROLE}_URL` — present in **every env** (0-5). Resolves to
  `{role}stage` in env 0-1 (dev-pair envs) and the bare `{role}` in
  envs 2-5.
- `DEV_{ROLE}_URL` — only in env 0-1 (dev-pair envs). Resolves to
  `{role}dev`.
- Roles: `API`, `FRONTEND`. Add `WORKER` only if the worker has a
  public surface (rare).

## The pair is BIDIRECTIONAL

Cross-service URL pairs come in two halves:
- The SPA reads `${STAGE_API_URL}` at **build time** to bake the API
  origin into the bundle.
- The api reads `${STAGE_FRONTEND_URL}` at **runtime** for the CORS
  allow-list (`enableCors({ origin: [...] })`).

Both halves consume the SAME project envs. Setting one without the
other reintroduces the chicken-and-egg from the other direction —
api rejects the SPA's request because CORS_ORIGINS still points at
the post-active alias that hasn't resolved.

### GOOD — both yamls reference the project envs

```yaml
# appstage/zerops.yaml — frontend SPA (build-time bake)
build:
  envVariables:
    VITE_API_URL: ${STAGE_API_URL}      # ← project env, resolved at provision time

# apistage/zerops.yaml — backend API (runtime CORS)
run:
  envVariables:
    CORS_ORIGINS: ${DEV_FRONTEND_URL},${STAGE_FRONTEND_URL}   # ← project envs
```

### BAD — one side fixed, the other still using post-active aliases

```yaml
# appstage/zerops.yaml — uses project envs ✓
build:
  envVariables:
    VITE_API_URL: ${STAGE_API_URL}

# apistage/zerops.yaml — STILL uses post-active alias ✗
run:
  envVariables:
    CORS_ORIGINS: ${appdev_zeropsSubdomain},${appstage_zeropsSubdomain}
```

This is a half-fix. The SPA bakes correctly, but api's CORS
allow-list has a literal `${appdev_zeropsSubdomain}` token until
appdev's first deploy mints the URL — same chicken-and-egg the
build-time fix was meant to solve, just on the api's runtime side.

### When you set up project envs, set up BOTH halves

If your recipe has a frontend + api pair, the provision phase sets
**four** project envs (envs 0-1) or **two** (envs 2-5):

```bash
# Envs 0-1 (dev-pair): both DEV_* and STAGE_*
# DEV_FRONTEND_URL carries -5173 because appdev runs Vite on 5173 (dynamic).
# STAGE_FRONTEND_URL has no port suffix because appstage runs base: static.
zerops_env project=true action=set \
  STAGE_API_URL="https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app" \
  STAGE_FRONTEND_URL="https://appstage-${zeropsSubdomainHost}.prg1.zerops.app" \
  DEV_API_URL="https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app" \
  DEV_FRONTEND_URL="https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app"

# Envs 2-5 (single-slot): only STAGE_* (single-slot hostnames are
# `api` / `app`, not `apistage` / `appstage`). `app` is base: static
# in stage/prod tiers — no port suffix.
zerops_env project=true action=set \
  STAGE_API_URL="https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app" \
  STAGE_FRONTEND_URL="https://app-${zeropsSubdomainHost}.prg1.zerops.app"
```

## When the fallback applies

The "deploy target service first, then build the consumer" fallback
applies only when the URL pattern is genuinely unknown at scaffold
time — e.g. a service whose hostname is computed dynamically. For
the standard frontend + api pair (which dominates dual-runtime
recipes), the workspace project-envs path is the canonical
solution; the fallback is a last resort.

## Deliverable tier yaml — the literal-stays-literal rule

For the engine-emitted deliverable yamls (`<env>/import.yaml` per
tier), `${zeropsSubdomainHost}` and the `STAGE_*_URL` constants stay
LITERAL in the published file. The end-user's click-deploy mints
fresh values. The engine handles this at finalize — finalize-phase
authoring rules forbid resolving these variables to literal URLs.
That rule is for the deliverable surface, NOT for the workspace
yaml.
