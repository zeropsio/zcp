---
id: idle/incomplete-resume
atomIds: [bootstrap-resume, bootstrap-route-options]
description: "Idle project with one resumable runtime — bootstrap session interrupted before completion."
---
<!-- UNREVIEWED -->

### Interrupted bootstrap detected

Envelope has `idleScenario: incomplete`: at least one runtime snapshot
has `resumable: true`, meaning a prior bootstrap wrote partial state
and died before close. **Do not classic-bootstrap over these services**
— a new session collides with the partial records.

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
