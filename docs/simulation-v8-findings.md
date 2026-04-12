# Simulation Audit — v8 Guide Across All 4 Shapes

Generated: 2026-04-12

## Method

Four parallel agents each wrote a temporary Go test calling `resolveRecipeGuidance(step, tier, plan)` for every step with their shape's fixture plan, read the EXACT assembled guide output, then audited every step as if they were the recipe-creating agent.

No MCP session was used — this is pure static analysis of the guide composition pipeline.

## Per-Shape Size Summary

| Shape | research | provision | generate | deploy | finalize | close | TOTAL |
|-------|----------|-----------|----------|--------|----------|-------|-------|
| hello-world | 2.9 KB | 9.5 KB | 18.6 KB | 21.6 KB | 15.5 KB | 13.7 KB | **81.8 KB** |
| backend-minimal | 2.9 KB | 9.5 KB | 22.6 KB | 21.6 KB | 15.5 KB | 13.7 KB | **85.8 KB** |
| fullstack-showcase | 8.5 KB | 9.5 KB | 28.0 KB | 38.7 KB | 15.5 KB | 13.7 KB | **113.9 KB** |
| dual-runtime-showcase | 8.5 KB | 12.7 KB | 38.4 KB | 38.7 KB | 15.5 KB | 13.7 KB | **127.5 KB** |

### Bloat gradient check

The widest shape (dual-runtime-showcase) is 1.56x the narrowest (hello-world). The generate step shows the strongest differentiation (18.6 KB → 38.4 KB = 2.06x), confirming predicate gating is working at generate. Deploy shows weaker differentiation (21.6 KB → 38.7 KB = 1.79x) because `dev-deploy-flow-core` is a monolith. Finalize and close show NO differentiation (15.5 KB and 13.7 KB across all 4 shapes) because their block catalogs are empty — no predicate filtering is possible.

---

## Bugs Found

### BUG 1: Smoke test step 3 missing implicit-webserver exception

**Affects**: backend-minimal (php-nginx), fullstack-showcase (php-nginx)

**Location**: `internal/content/workflows/recipe.md`, generate section, `on-container-smoke-test` block

**Problem**: The on-container smoke test step 3 says "Start the dev server" and verify port binding. But php-nginx is an implicit webserver — it auto-starts PHP-FPM on port 80 when the container runs. There is no user-facing dev server command to execute. The deploy step 2a correctly handles this ("Implicit-webserver runtimes (php-nginx, php-apache, nginx): Skip — auto-starts") but the generate-step smoke test has no equivalent skip instruction.

**Impact**: An agent following the smoke test literally would try to start php-fpm manually, causing a port conflict or redundant process. At minimum, the agent wastes an iteration figuring out there's nothing to start.

**Fix**: Add an implicit-webserver exception to smoke test step 3, matching the language in deploy step 2a: "Implicit-webserver runtimes (php-nginx, php-apache, nginx): Skip — the webserver auto-starts when the container is in RUNNING state. Verify by curling localhost:80 directly."

### BUG 2: Deploy execution-order table references non-existent steps for non-showcase

**Affects**: hello-world, backend-minimal

**Location**: `internal/content/workflows/recipe.md`, deploy section, `dev-deploy-flow-core` block, execution-order table

**Problem**: The single-runtime execution-order table references "Step 4b → Step 4c" but these steps are defined inside `dev-deploy-subagent-brief` and `dev-deploy-browser-walk` blocks which are gated on `isShowcase` and therefore do not emit for hello-world or backend-minimal. The agent is instructed to follow steps that have no corresponding content.

**Impact**: Confusion. The agent sees "do Step 4b and 4c" but has no instructions for them.

**Fix**: Either (a) remove 4b/4c from the non-showcase row in the execution-order table, or (b) conditionally render the row based on tier, or (c) annotate "(showcase only)" inline so the agent knows to skip.

### BUG 3: `hasBundlerDevServer` misses multi-base plans (Laravel+Vite)

**Affects**: fullstack-showcase (laravel-showcase with Vite asset pipeline)

**Location**: `internal/workflow/recipe_plan_predicates.go:163` (`hasBundlerDevServer` function)

**Problem**: Laravel uses Vite for asset compilation. The Vite dev server enforces HTTP Host-header validation (Vite 6+ `server.allowedHosts`). The `hasBundlerDevServer` predicate returns FALSE for this plan because:
1. "laravel" is not in `bundlerFrameworks` (correct — Laravel isn't a bundler framework)
2. `isDualRuntime` is false (correct — single-runtime full-stack)

But the agent WILL start Vite (`npm run dev`) via the `dev-dep-preinstall` / deploy step 2b path (multi-base: PHP primary + Node secondary). Without the `dev-server-host-check` block, the agent won't configure Vite's host allow-list, leading to broken HMR/asset loading on the `.zerops.app` subdomain.

**Impact**: HMR websocket connections and CSS/JS asset loading fail silently. The page renders (via php-nginx) but looks broken — no styles, no JS interactivity. The agent can work around it during deploy iteration but the guidance should prevent it proactively.

**Fix**: Extend `hasBundlerDevServer` to also return true when `needsMultiBaseGuidance(p)` is true. Multi-base implies a JS dev server (Vite/webpack) that needs host-check configuration. This is the cleanest structural fix — it doesn't add "laravel" to `bundlerFrameworks` (which would be semantically wrong) and it fires precisely when a secondary JS runtime with a dev server is present.

```go
// In hasBundlerDevServer, after the isDualRuntime && hasServeOnlyProd check:
return isDualRuntime(p) && hasServeOnlyProd(p) || needsMultiBaseGuidance(p)
```

### BUG 4: Deploy target enumeration says "workerdev" for shared-codebase showcase

**Affects**: fullstack-showcase (laravel-showcase with shared-codebase worker)

**Location**: `internal/content/workflows/recipe.md`, deploy section, "Verify ALL runtime targets" enumeration

**Problem**: The enumeration says "Single-runtime showcase: `appdev` (HTTP) + `workerdev` (logs only)". But a shared-codebase worker has no `workerdev` service — the worker runs as an SSH background process on the host target's dev container (`appdev`). The detailed section below it correctly explains this ("Shared-codebase worker logs live in the HOST target's log stream"), but the summary enumeration is misleading.

**Impact**: The agent may try to verify a non-existent `workerdev` service, wasting a `zerops_logs` call that returns an error.

**Fix**: Split the enumeration into shared vs separate worker shapes:
- "Single-runtime showcase (shared worker): `appdev` (HTTP + worker logs)"
- "Single-runtime showcase (separate worker): `appdev` (HTTP) + `workerdev` (logs only)"

---

## Cross-Shape Contradictions

### Contradiction 1: Smoke test vs deploy on implicit webservers

The deploy step 2a correctly teaches "Implicit-webserver runtimes: Skip — auto-starts." The generate-step smoke test does NOT have this exception. An agent creating a php-nginx recipe gets contradictory guidance: generate says "start the dev server and verify"; deploy says "skip for implicit webservers." The fix is to bring the smoke test step 3 to parity with deploy step 2a.

### Contradiction 2: Execution-order table scope vs block gating

The execution-order table in `dev-deploy-flow-core` (always-on) references steps that live in conditionally-emitted blocks. The table promises steps the agent will never receive instructions for. This is a structural contradiction between the always-on framing and the predicate-gated content.

---

## Missing Predicates

### `hasBundlerDevServer` gap for multi-base plans

The predicate fires for (a) bundler-primary frameworks and (b) dual-runtime + serve-only frontend. It misses (c) multi-base plans where the secondary runtime is a JS bundler. Laravel+Vite, Django+webpack, Rails+esbuild — all are multi-base PHP/Python/Ruby plans with a JS asset pipeline whose dev server needs host-check configuration.

**Proposed fix**: `needsMultiBaseGuidance(p)` → `hasBundlerDevServer` returns true.

---

## Framework Leakage

No framework leakage detected. All instructions are structural — derived from plan data (package manager, build commands, port, target types) rather than hardcoded to specific frameworks or runtimes. The tests in `recipe_guidance_test.go` (NoFrameworkHardcoding, NoFrameworkPortHardcoding, NoFrameworkWorkerRuleThumb) enforce this.

---

## Context Timing

### Dual-runtime URL pattern timing

Arrives at provision (`import-yaml-dual-runtime` block) with the full 6-env URL shape. Re-reinforced at generate (`dual-runtime-url-shapes` + `dual-runtime-consumption` blocks). This timing is correct — the agent needs the URL shape when writing `import.yaml` at provision, then when writing `zerops.yaml` `envVariables` at generate.

### Smoke test timing

Correctly placed at the end of generate (after `pre-deploy-checklist`, before `completion`). This is the right position — all files are written, and the smoke test is the final gate before the generate attestation.

### No timing issues detected

All information arrives when needed across all 4 shapes.

---

## Bloat Gradient Analysis

### Sections with zero differentiation (finalize + close = 29.2 KB constant)

`recipeFinalizeBlocks` and `recipeCloseBlocks` are both nil/empty — no block catalogs exist, so no predicate filtering happens. Both sections are monoliths that emit identical content for all 4 shapes. Combined, they account for 29.2 KB of the ~82 KB hello-world guide.

**Highest-impact items to gate** (by irrelevance to narrow shapes):
1. Finalize `projectEnvVariables` section (~1.8 KB) — gate on `isDualRuntime`
2. Finalize showcase service-key enumerations (~1.4 KB) — gate on `isShowcase` or `hasWorker`
3. Close browser-walk instructions after "skip for minimal" (~800 B) — gate on `isShowcase`
4. Close multi-codebase export/publish examples (~2 KB) — gate on `hasMultipleCodebases`

Total recoverable: ~6 KB from hello-world/backend-minimal guides.

### Deploy `dev-deploy-flow-core` monolith

The `dev-deploy-flow-core` block is always-on and contains content for all shapes: dual-runtime interleaving, asset dev server instructions, worker dev process, CORS, worker log verification. For hello-world, ~5 KB of the 22 KB deploy step is irrelevant.

**Highest-impact items to extract into gated blocks**:
1. Step 2b "Asset dev server" (~800 B) — gate on `hasBundlerDevServer`
2. Step 2c "Worker dev process" (~600 B) — gate on `hasWorker`
3. Step 1-API / Step 2a-API / Step 3-API interleaving (~1.5 KB) — gate on `isDualRuntime`

### Always-on generate blocks with shape-irrelevant content

1. `zerops-yaml-header` 4-row shape table (~800 B of multi-repo topology) — always-on by design, serves as the shape reference. Acceptable.
2. `asset-pipeline-consistency` (~300 B about CSS/JS build pipelines) — always-on. Could gate on `needsMultiBaseGuidance || hasBundlerDevServer`.

---

## Smoke Test Universality

The on-container smoke test has 3 steps. Universality assessment:

| Step | hello-world | backend-minimal | fullstack-showcase | dual-runtime |
|------|-------------|-----------------|-------------------|--------------|
| 1. Install deps | `npm ci` — OK | `composer install` — OK | `composer install` + `npm ci` — OK | `npm ci` per mount — OK |
| 2. Compile check | Skip (no compile) — guidance says "if the framework has a compilation step" — OK | Skip (no compile for PHP) — OK | `npm run build` (assets) — OK | `npm run build` per codebase — OK |
| 3. Start dev server | `node server.js` — OK | **BUG**: php-nginx auto-starts, no explicit command | **BUG**: php-nginx auto-starts | Multiple per mount — OK |

**Step 3 breaks for implicit webservers.** The fix (add implicit-webserver exception) restores universality.

**Multi-codebase coverage**: The smoke test says "run the smoke test on each container independently." For dual-runtime (3 mounts), this means 3 independent smoke test passes. This is correct and clear.

---

## v8 Findings Coverage

All 9 v8 findings are addressed in the guide for the dual-runtime-showcase shape (the shape they were discovered against). Cross-shape coverage:

| Finding | dual-runtime | fullstack | backend-minimal | hello-world | Universal? |
|---------|-------------|-----------|-----------------|-------------|------------|
| F1: Smoke test | YES | YES (BUG: step 3) | YES (BUG: step 3) | YES | NO — implicit webserver gap |
| F2: No `run.os` | YES | YES | YES | YES | YES |
| F3: Dev ports | YES | YES | YES | YES | YES |
| F4: @version suffix | YES | YES | YES | YES | YES |
| F5: Dev type override | YES (code) | N/A | N/A | N/A | YES (code fix) |
| F6: Verify all targets | YES | BUG: "workerdev" for shared | N/A | N/A | NO — shared worker gap |
| F7: Port hygiene | YES | YES | N/A | N/A | YES |
| F8: Managed service auth | YES | YES | N/A | N/A | YES |
| F9: execOnce burn | YES | YES | YES | YES | YES |

---

## Recommended Fixes (Priority Order)

### P0 — Bugs that cause wrong agent behavior

1. **Smoke test implicit-webserver exception** (BUG 1) — Add skip instruction for php-nginx/php-apache/nginx to the `on-container-smoke-test` block in `recipe.md`. Matches deploy step 2a language.

2. **`hasBundlerDevServer` predicate gap** (BUG 3) — Extend to return true when `needsMultiBaseGuidance(p)` is true. One-line code change in `recipe_plan_predicates.go`.

3. **Deploy execution-order dangling references** (BUG 2) — Annotate Step 4b/4c as "(showcase only)" in the execution-order table rows for single-runtime.

4. **Deploy target enumeration shared-worker** (BUG 4) — Split "Single-runtime showcase" into shared-worker and separate-worker variants in the verification enumeration.

### P1 — Bloat reduction (no behavioral change)

5. **Wire `recipeFinalizeBlocks` catalog** — Decompose finalize into blocks. Gate `projectEnvVariables` on `isDualRuntime`, service-key lists on `isShowcase`. Saves ~3.2 KB for narrow shapes.

6. **Wire `recipeCloseBlocks` catalog** — Gate browser-walk instructions on `isShowcase`, export examples on `hasMultipleCodebases`. Saves ~2.8 KB.

7. **Extract deploy sub-steps into gated blocks** — Move Step 2b (asset dev server), Step 2c (worker), and API-first interleaving out of the `dev-deploy-flow-core` monolith into separate gated blocks. Saves ~3 KB for hello-world.

### P2 — Style improvements

8. **Provision 3-mount enumeration** — For dual-runtime + separate worker, explicitly list the 3 mounts at provision.

9. **Finalize envComments example** — Add a showcase-shaped example alongside the existing minimal one.
