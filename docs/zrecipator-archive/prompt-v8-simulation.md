# Simulation Prompt — Recipe Guide Walkthrough Audit

Paste this into a fresh Claude Code session at `/Users/fxck/www/zcp`.

---

## Context

Read these files first:
- `CLAUDE.md` — project conventions
- `docs/improvement-guide-v8-findings.md` — the findings this audit validates
- `internal/workflow/recipe_guidance.go` — how the guide is assembled per step
- `internal/workflow/recipe_section_catalog.go` — which blocks emit per predicate
- `internal/workflow/recipe_plan_predicates.go` — the predicate functions
- `internal/workflow/recipe_guidance_test.go` lines 30–110 — the 4 shape fixtures
- `internal/workflow/recipe_guidance_test.go` lines 220–260 — the existing cap sweep test pattern

The recipe workflow guide is a structured document (`internal/content/workflows/recipe.md`) with `<section>` and `<block>` tags. Blocks have predicates — a block only emits when its predicate returns true for the plan. Different recipe shapes produce different guides.

**The goal**: Verify the guide is correct, complete, and smooth for EVERY primary shape — not just the one shape (dual-runtime-showcase / nestjs) that was tested in v8.

## Session isolation — CRITICAL

**Do NOT call any `mcp__zerops__*` tools or `zerops_workflow`.** The ZCP server has a single session. Multiple agents calling it would corrupt shared state. This audit is a STATIC ANALYSIS of the guide content.

**How to get the assembled guide for each shape**: Write a temporary Go test file (e.g. `internal/workflow/recipe_simulation_test.go`) that calls the existing `resolveRecipeGuidance(step, tier, plan)` function for each step with the shape's fixture plan, and prints the output. The function is pure — it reads `recipe.md`, applies predicates, and returns the composed string. No MCP session, no side effects.

Pattern (adapt from the existing cap sweep test at `recipe_guidance_test.go:220`):

```go
func TestSimulation_DumpGuide(t *testing.T) {
    plan := fixtureForShape(ShapeHelloWorld) // or whichever shape
    steps := []string{
        RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
        RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
    }
    for _, step := range steps {
        guide := resolveRecipeGuidance(step, plan.Tier, plan)
        t.Logf("\n===== %s (%d bytes, %.1f KB) =====\n%s",
            step, len(guide), float64(len(guide))/1024, guide)
    }
}
```

Run with `go test ./internal/workflow/ -run TestSimulation_DumpGuide -v -count=1` and read the output. This gives you the EXACT guide the recipe-creating agent would see — no guessing about which predicates fire.

**Delete the test file when done** — it's temporary scaffolding, not a permanent test.

## What to do

Dispatch **4 parallel agents**, one per shape. Each agent:

1. Writes a temporary test that dumps the assembled guide for their shape (all 6 steps)
2. Reads the ACTUAL output — the exact text the recipe-creating agent would see
3. Walks through each step as if they were the agent creating that recipe, flagging every issue
4. Produces a structured report

### Agent 1: Hello-World shape

```
Shape: ShapeHelloWorld (nodejs-hello-world)
Plan: tier=hello-world, 2 targets (app: nodejs@22, db: postgresql@17)
Predicates that fire: NONE of the shape-specific ones
Key characteristic: SIMPLEST recipe. 1 runtime + 1 database. The guide must be lean.
```

Dump the guide using `fixtureForShape(ShapeHelloWorld)`, then audit each step:

1. **Context completeness**: Does the guide give everything needed to act? Is required info stuck in a block that didn't emit?
2. **Bloat check**: Anything in emitted blocks irrelevant to this shape? (worker instructions, dual-runtime patterns, showcase rules, etc.)
3. **Actionability**: Can you follow without guessing? Ambiguous "if applicable" where "skip" is clearer?
4. **First principles**: Are instructions structural (derived from plan data) or hardcoded to specific frameworks/runtimes/ports?
5. **Flow**: Each step leads naturally to the next? Context arrives when needed?
6. **Smart defaults**: Simplest recipe = minimal config. Any over-engineering?

Shape-specific checks:
- The on-container smoke test: what does "compile check" mean for a hello-world with just `node server.js`? Is the guidance clear that this step is optional when there's no compilation step?
- No managed services beyond db — does the env-var-discovery block correctly not emit? But the db IS a managed service... check `hasManagedServiceCatalog` against `postgresql`.
- Research section: hello-world gets only `research-minimal`, not `research-showcase`. Is that sufficient?
- Deploy: no showcase blocks (subagent brief, browser walk). Is the deploy flow clear for a 2-service recipe?

Report format: one section per step, issues tagged as BLOCKER / BUG / BLOAT / STYLE / OK.

### Agent 2: Backend-Minimal shape (implicit webserver)

```
Shape: ShapeBackendMinimal (laravel-minimal)
Plan: tier=minimal, 2 targets (app: php-nginx@8.3, db: postgresql@17)
Predicates that fire: NONE of the shape-specific ones (same as hello-world)
Key characteristic: Implicit webserver (php-nginx auto-starts). No explicit start command. Package manager is composer, not npm.
```

Same audit structure. Shape-specific checks:
- Smoke test: there's no "start the dev server" for implicit webservers — php-nginx auto-serves. Does the guidance handle this? Is step 3 of the smoke test (start dev server) clearly optional for implicit-webserver runtimes?
- Does the guide handle `composer install` without assuming npm?
- Does serve-only dev override correctly NOT emit? (php-nginx is NOT serve-only)
- Does `hasManagedServiceCatalog` fire for postgresql? (check the predicate's `managedServiceBases` map)
- Deploy step 2a: "Start via SSH" — but php-nginx is an implicit webserver. Does the guide say "skip"?
- This shape has the SAME predicates as hello-world but a DIFFERENT tier (minimal vs hello-world). Does the guide differentiate correctly? (tier=minimal gets `generate-fragments` deep-dive, hello-world doesn't)

### Agent 3: Full-Stack Showcase (shared worker)

```
Shape: ShapeFullStackShowcase (laravel-showcase)
Plan: tier=showcase, 7 targets (app: php-nginx@8.3, worker: php@8.3 sharesCodebaseWith="app", db, cache, queue, storage, search)
Predicates that fire: isShowcase, hasWorker, hasSharedCodebaseWorker, hasManagedServiceCatalog, needsMultiBaseGuidance, hasBundlerDevServer (check this one carefully — does it fire for php-nginx?)
Key characteristic: 1 repo, 3 setups (dev, prod, worker). Multi-base (PHP + Node for assets). Implicit webserver + asset dev server.
```

Same audit structure. Shape-specific checks:
- Worker-setup-block: shared-codebase pattern = 3 setups in 1 zerops.yaml. Is this clear?
- Multi-base (dev-dep-preinstall): does this make sense for PHP+Node?
- Dashboard-skeleton: enough detail without hardcoding framework patterns?
- Smoke test with implicit webserver PLUS secondary asset dev server — what's the sequence?
- Feature subagent brief: makes sense for full-stack (not API-first)?
- git-init-per-codebase: must NOT emit (shared codebase = 1 mount, `hasMultipleCodebases` should return false)
- where-to-write-files-single should emit (not multi), since shared worker = 1 mount
- Deploy ordering: NO API-first interleaving (straight Step 1 → 2 → 3)
- `hasBundlerDevServer` — trace the predicate. Does it fire for this plan? The plan has `isDualRuntime: false`, but the fallback clause checks `isDualRuntime(p) && hasServeOnlyProd(p)`. For this plan, is there a bundler dev server? (Laravel uses Vite for assets — check if the `bundlerFrameworks` list catches this.)

### Agent 4: Dual-Runtime Showcase (separate worker)

```
Shape: ShapeDualRuntimeShowcase (nestjs-showcase)
Plan: tier=showcase, targets include app:static, api:nodejs@24 role=api, worker:nodejs@24 isWorker sharesCodebaseWith="", plus managed services
Predicates that fire: isShowcase, isDualRuntime, hasWorker, hasSeparateCodebaseWorker, hasServeOnlyProd, hasManagedServiceCatalog, hasBundlerDevServer, hasMultipleCodebases
Key characteristic: WIDEST shape. 3 repos. Serve-only prod with dev override. API-first deploy. This is the v8 shape.
```

Same audit structure. Shape-specific checks:
- Serve-only dev override: teaches dev type override WITHOUT `run.os`? (v8 Finding 2)
- Dual-runtime URL pattern: arrives at provision, not generate? Timing correct?
- Deploy ordering: matches API-first interleaving table?
- where-to-write-files-multi: describes 3-mount layout?
- Finalize: handles `appdev: type: static` → dev type override for env0/env1? (v8 Finding 5)
- v8 findings addressed: smoke test, no os:ubuntu, dev ports, verify all targets, subagent port hygiene?
- git-init-per-codebase: emits and covers all 3 mounts?
- Stage cross-deploy: parallel for all 3 codebases?
- Worker deploy: separate codebase means separate `zerops_deploy` — is this clear?

## Cross-agent synthesis

After all 4 agents complete, synthesize their reports. Look for:

1. **Cross-shape contradictions**: A rule that works for one shape but breaks another
2. **Missing predicates**: A block that emits where it shouldn't, or doesn't emit where it should
3. **Framework leakage**: Any instruction assuming a specific runtime, package manager, or port
4. **Context timing**: Info arrives too late for one shape but right for another (block position or predicate issue)
5. **Bloat gradient**: Hello-world guide should be MUCH smaller than dual-runtime-showcase. If similar, too many always-on blocks.
6. **Smoke test universality**: Does the on-container smoke test work for all 4 shapes, including implicit webservers, hello-worlds with no compile step, and multi-base builds?

Write the synthesis to `docs/simulation-v8-findings.md`.
