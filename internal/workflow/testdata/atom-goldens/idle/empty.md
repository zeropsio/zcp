---
id: idle/empty
atomIds: [bootstrap-route-options, idle-bootstrap-entry]
description: "Fresh project, no services bootstrapped or adopted yet."
---
### Bootstrap route discovery

Start with discovery:

```
zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
```

Do this **without** `route`. `BootstrapDiscoveryResponse` returns
priority-ordered `routeOptions[]`; no session is committed.

Pick one option, then call `start` again with its route plus required
`recipeSlug` / `sessionId`.

### Ranked options

| Route | Present when | Carries | Dispatch / rule |
|---|---|---|---|
| `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
| `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Use when services match intent; otherwise use classic for non-colliding names. |
| `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<slug>"`. Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
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

This is an empty project. Bootstrap provisions the initial infrastructure — `bootstrap-route-options` (rendered alongside) walks through route discovery and dispatch. After the first bootstrap call returns the ranked routes, pick one and call `start` again with `route=...` to commit the session; a service plan is then proposed for you to approve before any services are created.

Keep the `intent` to one sentence — it scopes route ranking but doesn't constrain the plan.
