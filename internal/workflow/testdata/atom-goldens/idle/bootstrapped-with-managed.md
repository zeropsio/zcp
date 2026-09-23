---
id: idle/bootstrapped-with-managed
atomIds: [bootstrap-route-options, idle-develop-entry, idle-launch-entry]
description: "Idle project with one runtime + one managed dep, both bootstrapped and deployed."
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

---

=== idle-develop-entry ===
The project has at least one bootstrapped service ready to receive
code. Start a develop session:

```
zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["appdev",…]
```

The envelope will flip to `phase: develop-active`; subsequent status
calls show `workSession.deploys[]` and `workSession.verifies[]` as
you iterate. Once the develop session is active, auto-close semantics
land in the develop response.

**To add a NEW service to this project** — run bootstrap workflow
again with a non-colliding hostname (`route="classic"` or
`route="recipe"`). The new service exists alongside the bootstrapped
ones; bootstrap never modifies or replaces existing services.

---

=== idle-launch-entry ===
The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), what applies depends on whether this Mate is wired to the account's own Gitea (a repository the broker gave its pairs).

Not wired to the account's own Gitea — use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

```
zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
```

The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), a single integration token with project-creation permission staged as a service secret for the launch window and never persisted — either minted by ZCP itself from a one-time platform delegation on the user's confirmation, or supplied once by the user as a fallback — and a post-launch checklist (first release, window close via confirm-production, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (a stage-half hostname is accepted too — the handler normalizes it to the dev half). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.

Wired to the account's own Gitea — do not start launch-production: it refuses outright there, since a group's production is not this workflow's to create. It belongs to the group, added by the person from Mate's projects page and fed by release tags through the broker — never a project this workflow creates or a token it stages. Say what is true of the pair's own recorded pull request instead of starting the workflow, and give each state its own words — "nothing open" does NOT mean "merged": an open request ("pull request #N is open on `<repo>` — merge it first") means the code has not reached the group yet; a request the fresh read finds merged is as far as the Mate's part goes — do not claim what `main` now runs; production is added and released from the project on Mate's projects page, whose release offer shows what it would put live; no request at all, or one closed without merging, means nothing of that work has landed either — the pair delivers through its stage half first, same as the open case, before production is added from the projects page. Continue developing through the develop entry for anything short of that.
