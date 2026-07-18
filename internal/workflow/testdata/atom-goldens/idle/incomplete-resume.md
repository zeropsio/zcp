---
id: idle/incomplete-resume
atomIds: [bootstrap-resume, bootstrap-route-options]
description: "Idle project with one resumable runtime — bootstrap session interrupted before completion."
---
=== bootstrap-resume ===
### Interrupted bootstrap detected

Envelope has `idleScenario: incomplete`: at least one runtime snapshot
has `resumable: true`, meaning a prior bootstrap wrote partial state
and died before close. The discover output also surfaces these as
per-service `adoptionState="resumable"` plus a directive warning
naming the exact `sessionId` to pass to resume — read the warning,
copy the session ID, dispatch the resume call. **Do not
classic-bootstrap over these services** — a new session collides with
the partial records.

**Decision path:**

1. **Resume first.** Call discovery:
   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<anything>"
   ```
   Read `routeOptions[]`; the `resume` entry carries `resumeSession`
   and `resumeServices`. Dispatch:
   ```
   zerops_workflow action="start" workflow="bootstrap" route="resume" sessionId="<resumeSession>"
   ```
   Resume continues at the interrupted step.

2. **Abandon only when stale.** If the old bootstrap was deliberately
   abandoned or the services are wrong, delete orphan files under
   `.zcp/state/services/<hostname>.json`; the services become adoptable.

Either way, **never** use `route="classic"` with `resumable: true`
snapshots. Classic ignores the lock and new hostnames collide with
orphan records at provision.

---

=== bootstrap-route-options ===
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
