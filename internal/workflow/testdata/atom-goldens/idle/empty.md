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

### Ranked options

| Route | Present when | Carries | Dispatch / rule |
|---|---|---|---|
| `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
| `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Use when services match intent; otherwise use classic for non-colliding names. |
| `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<value from routeOptions[].recipeSlug>"`. Copy the slug verbatim from the discover response — corpus slugs don't carry a `zerops-` prefix even when users name a recipe by its branded form (`"zerops-laravel-minimal"`). Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
| `classic` | Always, last | none | `route="classic"` for manual planning. |

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

Bootstrap is the canonical entry point even when the user wants to USE an existing Zerops recipe (`nodejs-hello-world`, `laravel-minimal`, `nextjs-ssr-hello-world`, etc.) — describe their stack in `intent` and the recipe match surfaces as one of the ranked route options. The separate `zerops_recipe` tool is for AUTHORING new recipes (recipe-corpus maintainer tooling), not for end-users.

Keep the `intent` to one sentence — it scopes route ranking but doesn't constrain the plan.
