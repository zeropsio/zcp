---
id: idle/dev-pool-only
atomIds: [bootstrap-route-options, idle-dev-pool-entry]
description: "Idle project whose only runtimes are two parts' dev pools beside a managed database — nothing to bootstrap or adopt; each part is worked on in a leased container."
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
| `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Attach ZCP tracking to existing services — no infra change, and no deploy: one with `deployed=false` stays empty until develop's first deploy. Use when the user's intent matches the listed `adoptServices[]`. To add NEW services alongside (instead of adopting these), use `classic`. |
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

=== idle-dev-pool-entry ===
### A Swarm pool in the project

A Swarm pool (`swarm@1`) is no runtime to bootstrap, adopt or deploy. It is
one of two things — tell which before you touch it:

- **A part's dev pool.** Its zerops.yaml setup starts `zcp-devsup run`, or
  the repository's `.zcp/dev-servers.json` names it as a server's `service`.
  Each branch you work on leases one of its containers, and you work in that
  leased container:
  - `zerops_lease action=status` lists the leases you hold — alias, branch,
    state — and each pool's `warm` container.
  - `zerops_lease action=acquire pool="<pool>" repo="<https clone url>" branch="<branch>"`
    leases a container on the branch, checks the branch out in it and starts
    its dev servers; `pools=["<pool>","<pool>"]` leases one per part for a
    branch that spans several parts. `repo` is needed on a pool's first
    acquire only.
  - `zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["<pool>"]`
    answers with the whole loop — work, commit and push, sync, release — and
    opens no develop session: nothing deploys or verifies a pool itself.
- **Infrastructure an app uses.** The app calls its orchestrator with
  `${<pool>_API_TOKEN}`. Leave it to the app: a lease takes one of its
  containers away from it.
