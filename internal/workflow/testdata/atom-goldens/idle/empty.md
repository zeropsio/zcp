---
id: idle/empty
atomIds: [bootstrap-route-options, idle-bootstrap-entry]
description: "Fresh project, no services bootstrapped or adopted yet."
---
### Bootstrap is two-phase

First call returns `kind: "route-menu"` listing `routeOptions[]` — no
session is open yet. Second call opens the session and returns
`kind: "session-active"`. Read `kind` on every response.

```
zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
   → kind="route-menu", routeOptions=[...]
zerops_workflow action="start" workflow="bootstrap" route="<picked>" \
   recipeSlug="<slug>"   # if route="recipe"
   sessionId="<id>"      # if route="resume"
   → kind="session-active"
```

### Coexistence with existing services

Existing services in the project (bootstrapped or not) are **independent**.
Bootstrap creates **new** services alongside them — it does not modify,
re-import, or replace existing ones. To add a service to a project that
already has some, pick a non-colliding hostname and bootstrap normally.
`zerops_import override=true` is destructive and reserved for explicit
user requests (config change on a known service), never the default
path when bootstrap surfaces a hostname collision.

### Ranked options

| Route | Present when | Carries | Dispatch / rule |
|---|---|---|---|
| `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
| `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Attach ZCP tracking to running services — no infra change. Use when the user's intent matches the listed `adoptServices[]`. To add NEW services alongside (instead of adopting these), use `classic`. |
| `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<value from routeOptions[].recipeSlug>"`. Copy the slug verbatim from the discover response — corpus slugs don't carry a `zerops-` prefix even when users name a recipe by its branded form (`"zerops-laravel-minimal"`). Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
| `classic` | Always available | none | `route="classic"` for manual planning. Default path for creating new services in any project state — fresh project or alongside existing ones. |

### Explicit overrides

Explicit `route` on the first call bypasses discovery. Use only after
prior discovery or direct user route choice. Valid values:
`adopt`, `recipe`, `classic`, `resume`. Empty route re-enters discovery.

### Collision semantics

`collisions[]` annotates recipe options; enforcement happens at plan
submission. Pre-plan hostnames: rename runtimes or set managed deps to
`EXISTS` before submitting.

---

This is an empty project. Bootstrap provisions the initial infrastructure. After the first bootstrap call returns the ranked routes, pick one and call `start` again with `route=...` to commit the session; a service plan is then proposed for you to approve before any services are created.

Bootstrap is the canonical entry point even when the user wants to USE an existing Zerops recipe (`nodejs-hello-world`, `laravel-minimal`, `nextjs-ssr-hello-world`, etc.) — describe their stack in `intent` and the recipe match surfaces as one of the ranked route options.

Keep the `intent` to one sentence — it scopes route ranking but doesn't constrain the plan.
