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

`bootstrapMode` and `stageHostname` MUST be inside `runtime` — flat placement is hard-rejected. If you flatten by reflex, the error response includes the corrected JSON literal; paste-and-resend in one turn.

```json
[
  {
    "runtime": {
      "devHostname": "appdev",
      "stageHostname": "appstage",
      "type": "nodejs@22",
      "bootstrapMode": "standard",
      "isExisting": false
    },
    "dependencies": [
      {"hostname": "db", "type": "postgresql@18", "resolution": "CREATE"}
    ]
  }
]
```

Pair fields (`devHostname`/`stageHostname`/`type`) come from the recipe's `zeropsSetup: dev`/`prod` services; `bootstrapMode` from the banner; `dependencies[]` lists managed services verbatim with `resolution: "CREATE"`.

### Collision recovery (route option has `collisions: [...]`)

- **Runtime** → non-colliding `devHostname`/`stageHostname`; ZCP rewrites YAML at provision.
- **Managed, same type** → `resolution: "EXISTS"`, keep recipe's hostname. Entry drops from YAML; existing service reused via `${hostname_*}`.
- **Managed, different type** → `route="classic"`.

Unrecovered collision → plan rejected.

Do not write code — `buildFromGit` pulls the app repo at import. (Container only; in local mode the recipe repo is cloned into CWD instead.)
