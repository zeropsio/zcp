# Recipe plan validator's 2-role world blind spot

**Surfaced**: 2026-05-19 + flow-eval run on `recipe-laravel-showcase-fullstack`
(suite 20260519-132715). Agent submitted natural 2-target plan
(app dev/stage pair + worker), validator rejected with opaque
`"no recipe service matches plan target type \"php-nginx@8.4\" (role=dev)"`.
Agent reformulated to single-target (worker dropped from plan, recipe
import provisioned it). Retrospective explicitly named the friction:

> "A future agent hitting a recipe with a worker will make the same
> mistake I did. The briefing should say explicitly: 'Only create plan
> targets for dev/prod pairs. Workers and other non-paired runtime
> services in the recipe YAML are handled by the import step — do not
> represent them in the plan.'"

**Why deferred**: Friction is recipe-specific (laravel-showcase is the
ONLY recipe in the corpus with `zeropsSetup: worker`), one-rejection
recovery cost, not a hard block. Fix needs design decision among 3
candidates (briefing-only vs validator tolerance vs first-class
worker role) — not a 5-line patch.

**Trigger to promote**: Second recipe in the corpus introducing
`zeropsSetup: worker` / `queue` / any non-{dev,prod,staging} runtime
service, OR repeated agent friction here (flow-eval retrospective
catches it again).

## Root cause

`internal/workflow/recipe_override.go::recipeRuntimeRole` (lines 180-188)
collapses all non-"dev" `zeropsSetup` values to `recipeRoleStage`:

```go
func recipeRuntimeRole(zeropsSetup string) string {
    if zeropsSetup == recipeRoleDev {
        return recipeRoleDev
    }
    return recipeRoleStage  // worker, queue, scheduler, anything ≠ "dev"
}
```

Validator's world is 2-role (dev + stage). Worker is mis-classified as
stage half. Plan validator's strict "every plan slot must claim a
recipe service" check (lines 109-113) then surfaces as the opaque error
when agent has a separate worker plan target whose dev slot can't be
matched.

The plan-submission path (`route=discover` → submit plan →
`RewriteRecipeImportYAML` → import) hits this. The direct-recipe path
(`route=recipe recipeSlug=X`) bypasses rewrite/validation entirely;
recipe imports verbatim. This split explains why the friction never
surfaced before — historically laravel-showcase was tested via direct
recipeSlug path.

## Sketch — three approaches, increasing scope

### A. Diagnostic + briefing (lightest)

1. Improve validator error message — name the recipe runtime services,
   classify which one couldn't claim a slot, explain "worker /
   non-paired recipe runtimes are managed by recipe import — drop them
   from the plan".
2. Discover-step atom briefing explicitly teaches the convention.

Pros: minimal code, preserves strict invariant ("every plan slot must
claim a recipe service" still catches typo'd hostnames).
Cons: relies on agent reading briefing; if the prompt context obscures
it, agent makes the same mistake.

### B. Validator tolerance (medium)

When an unused plan slot's hostname matches a recipe service hostname
verbatim, validator skips the "unused slot" error and warns instead.
Worker plan target effectively becomes a no-op pass-through.

Pros: agent's natural multi-target plan just works.
Cons: weakens the typo-catching invariant (`appdev` typo'd as
`appdev2` would pass if recipe happens to have `appdev2`).

### C. First-class worker role (heavy)

Extend `recipeRuntimeRole` to pass through literal `zeropsSetup`. Plan
target schema gains worker awareness (new `WorkerHostname` field, or
`bootstrapMode=worker`). Validator matches by literal role.

Pros: architecturally symmetric — all runtime variants first-class.
Cons: big surface — ServicePlan schema + atom corpus + briefing +
validator + tests + spec docs. Only worth it if multiple
`zeropsSetup` variants beyond dev/prod proliferate.

## Recommendation

**A + B together.** Briefing teaches the convention (A) so well-
informed agents avoid the friction; validator tolerance (B) recovers
gracefully when agents naturally model "plan target per recipe
runtime". Defer C until 2+ recipes carry `zeropsSetup: worker`-style
services.

## Refs

- `internal/workflow/recipe_override.go:180-188` — role mapping
- `internal/workflow/recipe_override.go:109-113` — strict slot check
- `internal/knowledge/recipes/laravel-showcase.import.yml` — only recipe
  with `zeropsSetup: worker` today
- `eval/behavioral/runs/20260519-132715/recipe-laravel-showcase-fullstack/self-review.md` —
  reproducer retrospective
