---
id: bootstrap-route-options
priority: 1
phases: [idle]
title: "Bootstrap route selection — discovery response"
---

### Bootstrap route discovery

Bootstrap starts with a discovery pass: call
`zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"`
**without** a `route` parameter first. The engine inspects the project
state, the intent, and the recipe corpus and returns a ranked list of
options in `routeOptions[]`. No session is committed yet.

Pick one option and call `start` again with the chosen route to commit
the session.

### What the ranked list means

Options always arrive in this priority order:

1. **resume** — a previous bootstrap session was interrupted.
   `BootstrapSession` tags mark those services as reserved; the only
   clean recovery is resuming that session. When this
   option is present, **pick it first** unless you have a specific
   reason to override. Carries `resumeSession` + `resumeServices`;
   dispatch via `route="resume" sessionId="<resumeSession>"`.
2. **adopt** — the project has runtime services (non-managed,
   non-system) without complete `ServiceMeta`. Picking adopt attaches
   ZCP metadata to the existing services rather than bootstrapping
   new ones. Carries `adoptServices` with the hostnames that will
   be adopted. Prefer adopt over recipe when services match what
   the user wants; if they don't, use `route="classic"` instead so
   you can plan from scratch without colliding with the existing
   stack.
3. **recipe** — up to three ranked recipe matches. Each carries
   `recipeSlug`, `confidence`, `collisions[]`. Collisions are
   recoverable inside the recipe route (runtime rename in plan, or
   same-type managed adopt via `resolution: "EXISTS"`); the
   `bootstrap-recipe-match` atom details both. Switch routes only
   when the collision is on a managed service whose type differs
   from the recipe's, or when the user wants independent infra.
   Dispatch via `route="recipe" recipeSlug="<slug>"`.
4. **classic** — always present, always last. Manual plan path. Pick
   this to bypass every auto-detection and describe the infrastructure
   yourself. Dispatch via `route="classic"`.

### Explicit overrides

The `route` parameter on the commit call bypasses discovery entirely.
Pass it when the LLM has already chosen (from a prior discovery), or
when the user directly specified a route. Valid values: `adopt`,
`recipe`, `classic`, `resume`. Empty route always re-enters discovery.

### Collision semantics

`collisions[]` annotates recipe options; enforcement runs at plan submission
(`runtimeCollisionError` + recipe-override pre-flight). Pre-plan the
hostnames: rename runtime targets or set managed deps to `EXISTS` before
submitting.
