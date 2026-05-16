---
id: idle/bootstrapped-with-managed
atomIds: [bootstrap-route-options, idle-develop-entry, idle-tool-preload, idle-launch-entry]
description: "Idle project with one runtime + one managed dep, both bootstrapped and deployed."
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

The project has at least one bootstrapped service ready to receive
code. Start a develop session:

```
zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["appdev",…]
```

The envelope will flip to `phase: develop-active`; subsequent status
calls show `workSession.deploys[]` and `workSession.verifies[]` as
you iterate. Once the develop session is active, auto-close semantics
land in the develop response.

---

### Pre-load tool schemas in one batch

`zerops_*` tools are deferred — schemas load via `ToolSearch`. Loading
them one at a time across turns burns N-1 round-trips before the first
real action. On the very first turn — BEFORE calling `zerops_workflow` —
batch-load every tool you'll need across bootstrap and develop:

```
ToolSearch query="select:zerops_workflow,zerops_discover,zerops_knowledge,zerops_import,zerops_env,zerops_mount,zerops_deploy,zerops_verify,zerops_logs,zerops_events,zerops_subdomain,zerops_manage,zerops_process"
```

`select:` accepts a comma-separated list and returns all matching schemas
in one round-trip. Phase-specific batch reminders appear later (in
bootstrap and develop) as fallbacks for late-arriving sessions
(compaction recovery, develop without prior bootstrap); when you start
fresh at idle, this single batch covers the full session.

---

The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

```
zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
```

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), one-shot account-wide launch-window token (validated, never persisted), and a post-launch checklist (delete the key, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (stage-half input fires a corrective scope-prompt blocker). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.
