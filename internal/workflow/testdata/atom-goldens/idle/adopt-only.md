---
id: idle/adopt-only
atomIds: [bootstrap-route-options, idle-adopt-entry, discover-activity-inflight]
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

Per-service `adoptionState` in `zerops_discover` output classifies each
service into one of five states: `adopted` (ZCP-tracked, ready for
develop/deploy), `adoptable` (live runtime without ServiceMeta — call
bootstrap route=adopt), `resumable` (mid-bootstrap, owned by prior
session — call bootstrap route=resume with the session ID surfaced in
the warning), `managed-dep` (db/cache/storage, no adoption concept),
`zcp-self` (control-plane container, never adopted). The discover
response also surfaces directive warnings naming exact recovery calls
per state — read them before deriving anything from per-service flags.

**FIRST CALL when discover surfaces adoptable services:** open the
bootstrap-adopt session immediately, with the SAME intent string you'd
pass to develop/deploy later. Do NOT probe with `workflow="develop"`
expecting an ADOPT_REQUIRED redirect — the redirect works but costs a
wasted round-trip + clutters the session log with a rejected start.

Concrete shape (commits the route on the first call — skip the menu
when adopt intent is already clear from discover output):

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
```

Replace `<one-line user task summary>` with the actual task intent
(e.g. `intent="redesign appdev homepage as tech blog"`) — NOT a
placeholder, NOT a generic "adopt existing". The intent threads
through to the develop session that follows, so phrasing it as the
real task scope avoids a re-typed intent on the next call.

Service-scoped tools (`workflow="develop"`, `zerops_deploy`,
`zerops_verify`) reject with `ADOPT_REQUIRED` until adoption completes.
That gate is structural backstop, not the primary path — read the
warning, fire adopt directly.

Services in project, `not bootstrapped`. Two primary paths, both
legitimate; existing services stay independent either way:

1. **Adopt the listed services** — attach ZCP tracking to running
   services without changing their code, config, or scale.
2. **Create new services alongside** — pick non-colliding hostnames
   and bootstrap normally; existing services keep running untouched.

Re-importing or rewriting the existing services is **not** one of
these paths — `zerops_import override=true` is destructive and only
runs on explicit user request for a known service.

**Branch on intent.** Clear adopt intent ("adopt", "převzít", named
existing hostnames) → run adopt, no menu. Clear add-new intent
("add another", "new service") → use `classic`, no menu. Unclear →
offer routes, not workflows.

- ✅ "Adopt existing appdev/appstage, or create new alongside?"
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

After adopt completes the runtime becomes a valid `launch-production`
source — the adopted ServiceMeta carries the pair identity + setup
cascade state, so `zerops_workflow workflow="launch-production"
targetService=<adopted-hostname>` lands on the canonical dev-half
without an extra normalization round-trip.

---

A service can be mid-build or mid-deploy while its status still reads
`READY_TO_DEPLOY`. `zerops_discover` carries a per-service `activity` object
whenever a build or deploy is live on it: a runtime doing its first deploy reads
`status:"READY_TO_DEPLOY"` with `activity:{action:"build", status:"BUILDING"}`
(or `action:"deploy", status:"DEPLOYING"`), passes through `CREATING`, and flips
to `ACTIVE` only once the deploy activates.

A service carrying `activity` is NOT idle — its first deploy is still running, so
adopting or deploying onto it now is premature. Wait until the field clears
(re-run `zerops_discover`, or watch `zerops_events serviceHostname=<svc>` until
the build reaches RUNNING/ACTIVE), then proceed. The `activity` object always
carries the live `processId`; a genuinely stuck process can be canceled with
`zerops_process processId=<id> action="cancel"`.
