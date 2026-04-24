---
id: bootstrap-route-options
priority: 1
phases: [idle]
title: "Bootstrap route selection — discovery response"
---

### Bootstrap route discovery

Bootstrap starts with a discovery pass: call
`zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"`
**without** a `route` parameter. The response is a
`BootstrapDiscoveryResponse` with `routeOptions[]` ordered by priority.
No session is committed yet.

Pick one option and call `start` again with the chosen route (and
`recipeSlug` / `sessionId` as relevant) to commit the session.

### What the ranked list means

Options always arrive in this priority order:

1. **resume** — present when at least one service snapshot has
   `resumable: true`; carries `resumeSession` + `resumeServices`;
   dispatch via `route="resume" sessionId="<resumeSession>"`. When
   this option is present, **pick it first** unless you have a
   specific reason to override.
2. **adopt** — present when runtime services exist without a
   matching bootstrap record (the envelope's Services block renders
   them as `not bootstrapped`). Carries `adoptServices[]` with the
   hostnames to be adopted. Prefer adopt over recipe when existing
   services match the user's intent; otherwise use `route="classic"`
   to plan from scratch without colliding.
3. **recipe** — up to three ranked recipe matches. Each carries
   `recipeSlug`, `confidence`, and `collisions[]`. Collisions are
   recoverable inside the recipe route (runtime rename in plan, or
   same-type managed adopt via `resolution: EXISTS`); the
   `bootstrap-recipe-match` atom details both. Switch routes only
   when the collision is on a managed service whose type differs
   from the recipe's, or when the user wants independent infra.
   Dispatch via `route="recipe" recipeSlug="<slug>"`.
4. **classic** — always present, always last. Manual plan path.
   Dispatch via `route="classic"`.

### Explicit overrides

An explicit `route` on the first call bypasses discovery entirely.
Pass it when the LLM has already chosen (from a prior discovery), or
when the user directly specified a route. Valid values: `adopt`,
`recipe`, `classic`, `resume`. Empty route always re-enters discovery.

### Collision semantics

`collisions[]` annotates recipe options; enforcement runs at plan
submission, not at discovery. Pre-plan the hostnames: rename runtime
targets or set managed deps to `EXISTS` before submitting.
