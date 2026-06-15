# Cross-service URLs — workspace + deliverable (summary)

Run-30 Fix #1 PARTIAL — codebase-content variant. Drops the verbose
worked-example pair (113 lines) carried by `cross-service-urls.md` to
keep the composed codebase-content brief under the Read-tool 25K-token
ceiling. Env-content still loads the full atom; the full version
carries `## The pair is BIDIRECTIONAL` worked examples + the env-0/1
vs env-2-5 four-vs-two project-env shape.

Two completely different scopes for cross-service URL composition.
Reaching for the wrong one is the failure mode that bit run-19's SPA:
the build-time-bake trap is real, the recommended fix is **not**
"deploy api first" — that's the fallback, not the canonical solution.

## The two scopes

| Scope | Purpose | Variable | Resolution timing |
|---|---|---|---|
| **Workspace** (the dev/stage project you're authoring inside) | Cross-service URL composition for build-time bake + runtime CORS allow-list | `${zeropsSubdomainHost}` (project-scope, present at project creation) | **Resolved at provision time** — already a real value when scaffold runs |
| **Deliverable** (the published `import.yaml` for click-deploy) | Same purpose, end-user's project | `${zeropsSubdomainHost}` (literal in published yaml) | Resolved at end-user's click-deploy import |

**Same variable. Two contexts.** In workspace yaml + project envs the
variable is real; the platform substitutes it at provision time. In
the deliverable tier yaml the engine emits at finalize, the literal
token stays unresolved so each end-user's click-deploy mints fresh
values.

The **workspace** scope is what scaffold sub-agents author. The
**deliverable** scope is what the engine emits at finalize.

## The build-time bake trap

Vite / Webpack `DefinePlugin` / Astro / Next / SvelteKit static builds
inline `import.meta.env.VITE_*` (or equivalent) constants at **build
time**. The build container reads the env, substitutes literally into
the bundle, ships compiled JS. If the env value is a literal
`${apistage_zeropsSubdomain}` token (target service hasn't deployed
yet, alias hasn't minted), the bundle ships with the literal token
string instead of a URL. The browser then fetches
`${apistage_zeropsSubdomain}/api/items` and gets DNS failure.

The trap fires whenever a build-time consumer references a peer
service's `zeropsSubdomain` alias before that peer's first deploy.
Parallel scaffold dispatch makes the race common.

## The canonical fix — workspace project envs

Set project-scope env constants derived from `${zeropsSubdomainHost}`
+ the known peer hostname + the peer's port. These resolve at
provision time, before any scaffold sub-agent runs.

**Port-suffix rule:** when the runtime exposes httpSupport on a
non-default port (Vite dev = 5173, NestJS = 3000, anything dynamic),
the platform-issued subdomain carries a `-PORT` segment. When the
runtime is `base: static` (Nginx serving built assets on default
80/443), the subdomain has no port segment.

```
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app   # dynamic runtime
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app          # base: static (no port)
```

In the canonical SPA + API pair: `app{stage}` ships compiled assets on
`base: static` (no port); `appdev` runs Vite's dev server on 5173
(port-suffixed); `api{stage}` runs on its own port (3000 NestJS / 8000
Django / etc) — port-suffixed.

## Naming convention for the project-env constants

- `{ROLE}_URL` — present in **every env** (0-5). At dev-pair envs
  (0-1) the value carries `{role}stage` (the production-setup side);
  at single-slot envs (2-5) the value is the bare `{role}`.
- `DEV_{ROLE}_URL` — only in env 0-1 (dev-pair envs). Resolves to
  `{role}dev`.
- Roles: `API`, `FRONTEND`. Add `WORKER` only if the worker has a
  public surface (rare).

The SPA reads `${API_URL}` in `build.envVariables`; the api reads
`${FRONTEND_URL}` (and `${DEV_FRONTEND_URL}`) in `run.envVariables`
for CORS. Both halves consume the same project envs — set one
without the other and the build-time-bake trap reappears on the
runtime side.

## The setup-name rule

The `setup:` name MUST be the generic role-contract value (`dev` /
`prod` / `worker`) — never the slot hostname (`appdev` / `appstage` /
`apistage`). Tier import.yamls reference `zeropsSetup: prod` (or
`zeropsSetup: dev`) and the slot mapping happens at the import-yaml
layer. A slot-named setup leaves every tier yaml's reference
orphaned.

## Deliverable tier yaml — the literal-stays-literal rule

For the engine-emitted deliverable yamls (`<env>/import.yaml` per
tier), `${zeropsSubdomainHost}` and the `{ROLE}_URL` constants stay
LITERAL in the published file. The end-user's click-deploy mints
fresh values. The engine handles this at finalize; finalize-phase
authoring rules forbid resolving these variables to literal URLs.
That rule is for the deliverable surface, NOT for the workspace
yaml.
