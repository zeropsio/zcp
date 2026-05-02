---
id: bootstrap-recipe-match
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [discover]
title: "Recipe matched — plan from the import YAML"
coverageExempt: "recipe+discover step — 30 canonical scenarios cover recipe at provision + close; discover step is a one-shot transition before route is committed (<1% session frequency where this atom is the actionable signal)"
---

### Field mutability (change an immutable → `route="classic"`)

| Mutable | Immutable |
|---|---|
| Runtime `hostname` via `devHostname`/`stageHostname` | `type`, `zeropsSetup`, `buildFromGit`, `priority`, `mode`, autoscaling, env vars |
| Managed `resolution` (CREATE ↔ EXISTS) | Managed `hostname` — repo's `${hostname_*}` refs break on rename |

### Plan shape (no collisions)

Per runtime pair: `devHostname`/`stageHostname` from recipe's `zeropsSetup: dev`/`prod` services; `type` + `bootstrapMode` verbatim (mode from banner); `dependencies[]` hostname+type verbatim with `resolution: "CREATE"`; `isExisting: false`.

### Collision recovery (route option has `collisions: [...]`)

- **Runtime** → non-colliding `devHostname`/`stageHostname`; ZCP rewrites YAML at provision.
- **Managed, same type** → `resolution: "EXISTS"`, keep recipe's hostname. Entry drops from YAML; existing service reused via `${hostname_*}`.
- **Managed, different type** → `route="classic"`.

Unrecovered collision → plan rejected.

Do not write code — `buildFromGit` pulls the app repo at import.
