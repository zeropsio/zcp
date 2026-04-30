# Bootstrap plan schema can't represent cross-type recipe pairs

**Surfaced**: 2026-04-30 — Tier-3 eval `bootstrap-recipe-static-simple` (suite `20260430t154900-9c00ab`). Agent picked recipe route for `vue-static-hello-world`, hit `BootstrapCompletePlan` rejecting every shape it tried, fell back to classic. From the agent's EVAL REPORT (Failure chain, root cause classified PLATFORM_ISSUE):

> The bootstrap plan schema cannot represent cross-type standard pairs (e.g., nodejs@22 dev + static stage). The recipe route validator assumes both halves share the same `type`. Plan rejected for: type=nodejs@22 → "no recipe service matches plan target type 'nodejs@22' (role=stage)"; type=static → "no recipe service matches plan target type 'static' (role=dev)"; two separate entries → "standard mode requires explicit stageHostname".

**Why deferred**: real schema/handler change with non-trivial blast radius — touches `internal/workflow/bootstrap_plan.go` (target shape), `RewriteRecipeImportYAML` (RCO-2/RCO-6 invariants), the discover-step guidance, and probably one new test scenario per fix variant. Not in scope for the Phase 1 close-out commit. Static SPA recipes are the canonical motivating case (build with node, serve via nginx) — there will be more (Vue, React, Angular, Astro, Nuxt, Next.js static-export, etc.) so the workaround "use classic" scales poorly.

**Trigger to promote**: more than ONE additional eval scenario (Tier-3+ or future Phase) hits the same wall, OR a user reports recipe route doesn't work for static SPAs and asks for the fix.

## Sketch

Three options, ordered by structural goodness:

1. **Plan target schema gets a `stageType` field** (or `dev.type` + `stage.type` pair). `BootstrapTarget.Runtime` extended:
   ```go
   type RuntimeTarget struct {
       DevHostname    string
       Type           string  // dev half type (nodejs@22 in the example)
       StageType      string  // optional stage half type override (static)
       BootstrapMode  string
       StageHostname  string
       ...
   }
   ```
   `RewriteRecipeImportYAML` matches recipe services by (role + matchingType) where matchingType picks between `Type` and `StageType`. RCO-6 invariant updated to acknowledge `StageType`.

2. **Discover handler auto-detects cross-type recipe** and offers a SPLIT plan up-front: instead of standard pair, propose two `simple`-mode services with explicit hostnames + a flag that develop later understands as "they're a pair, do cross-deploy". Doesn't extend schema but introduces a new mode-like concept; complicated.

3. **Recipe corpus refactor**: strip the cross-type pair from recipes — vue-static-hello-world becomes static-only (build done outside Zerops, push pre-built `dist/`). Trade-off: loses the build-on-Zerops affordance the recipe was designed around. Probably wrong direction.

Recommend option 1 — adds one optional field, minimal blast radius, makes the schema honest about the cross-type case it already needs to support.

## Risks

- Schema change ripples through `internal/workflow/validate.go`, atom guidance for plan construction (bootstrap-classic-plan-*, bootstrap-mode-prompt), and any test fixture that assumes single-type targets.
- Backward-compat with existing `Type` field needs care — if `StageType` is empty, fall back to current behavior (both halves share `Type`).
- The `RewriteRecipeImportYAML` slot-matcher (RCO-6) needs an updated rule: stage slot matches by `StageType` if set, else by `Type`.

## Refs

- Tier-3 triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER3-TRIAGE.md` §S10
- Per-scenario assessment: `tier3/bootstrap-recipe-static-simple/result.json` (Failure chain #1)
- vue-static-hello-world recipe: `internal/knowledge/recipes/vue-static-hello-world.md` — canonical motivating case
- Validator current shape: `internal/workflow/bootstrap_plan.go` (BootstrapTarget) + RCO-6 slot-matching
