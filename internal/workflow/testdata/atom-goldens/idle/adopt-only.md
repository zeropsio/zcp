---
id: idle/adopt-only
atomIds: [bootstrap-route-options, idle-adopt-entry, idle-tool-preload]
description: "Idle project with one unmanaged runtime — eligible for adoption."
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

Services in project, `not bootstrapped`. Bootstrap must close before
develop/deploy/verify on these hostnames is reachable.

**Branch on intent.** Clear adopt intent ("adopt", "převzít", named
existing hostnames) → run adopt, no menu. Unclear → offer routes,
not workflows.

- ✅ "Adopt existing appdev/appstage, or create new in parallel?"
- ❌ "Develop on appdev, finish staging, or something else?" — those
  workflows aren't reachable yet; framing presents the unreachable as
  available.

Start (route-menu, no session yet):

```
zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
```

Commit (opens session):

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
```

Type field in the plan carries the full identifier from
`zerops_discover` verbatim — `alpine/nodejs@22`, `postgresql:single@18`.
Legacy `os:` + `mode:` sibling fields still accepted for BC but the
composite `type` is canonical; don't split. Pair-OS mismatch
(ubuntu/alpine) accepted silently — dev half's type is what the
plan carries.

Close: each adopted hostname stamps `bootstrapped: true`, mode preserved.
Close-mode + git-push stay empty (develop configures on first use).

---

### Pre-load tool schemas in one batch

`zerops_*` tools are deferred — schemas load via `ToolSearch`. Loading
them one at a time across turns burns N-1 round-trips before the first
real action. On the very first turn — BEFORE calling `zerops_workflow` —
batch-load every tool you'll need across bootstrap and develop:

```
ToolSearch query="select:mcp__zerops__zerops_workflow,mcp__zerops__zerops_discover,mcp__zerops__zerops_knowledge,mcp__zerops__zerops_import,mcp__zerops__zerops_env,mcp__zerops__zerops_mount,mcp__zerops__zerops_deploy,mcp__zerops__zerops_verify,mcp__zerops__zerops_logs,mcp__zerops__zerops_events,mcp__zerops__zerops_subdomain,mcp__zerops__zerops_manage,mcp__zerops__zerops_process"
```

`select:` accepts a comma-separated list and returns all matching schemas
in one round-trip. Phase-specific batch reminders appear later (in
bootstrap and develop) as fallbacks for late-arriving sessions
(compaction recovery, develop without prior bootstrap); when you start
fresh at idle, this single batch covers the full session.
