# R3 recipe-route shape contract — implementation design (durable; survives compaction)

**Parent plan:** `plans/zcp-consolidation-2026-06-04.md` (R3 root). This doc captures the concrete decomposition (from a Codex design pass + an Explore blast-radius map, both ephemeral) so P3/P4 can be implemented without them.

**Root cause:** on `route=recipe` the recipe import YAML is the authoritative owner of the service shape, but ZCP makes the agent re-author it as a plan and validates the re-authoring against the owner (two copies → drift; a worker has no plan slot → provisioned-but-untracked; dev-only narrowing rejected → provisions a paid stage against explicit user instruction). Fix: parse the YAML once into `RecipeImportShape`; DERIVE the plan from it (agent authors nothing); rewrite the YAML from the same shape; delete the 2-role guards.

## Status
- **P1 DONE** (commit 21c3e3fa): `RecipeImportShape{Runtimes[]RecipeRuntimeShape, ManagedDeps[]RecipeManagedDepShape}` + `ParseRecipeImportShape` + `Mode()`/`RuntimeCount()` accessors in `internal/workflow/recipe_shape.go`. `InferRecipeShape` is a BC shim. Worker is a first-class `RecipeRuntimeRoleWorker` (ServesHTTP=false). `Mode()` ignores worker extras (showcase appdev+appstage+workerstage → "standard", not old lossy ("",3)). NOTE: type named `RecipeImportShape` not `RecipeShape` — `RecipeShape int` is a pre-existing test-only fixture enum (recipe_guidance_test.go etc.). Pinned: `TestParseRecipeImportShape`.
- **P2 DONE** (commit 6843e342): `DeriveRecipePlan(shape, RecipeShapeOverrides) []BootstrapTarget`. Pinned: `TestDeriveRecipePlan`.
- **P2b DONE** (commit bd333176): DeriveRecipePlan groups runtimes **by `buildFromGit` repo** — one repo = one app codebase, dev+stage→standard, lone→dev/simple, each worker→standalone. Fixes the multi-repo gap: zerops-showcase (bun app repo + python worker repo = TWO pairs) was dropping the 2nd pair (R3-P2 kept only first dev + first stage). Managed deps stay CREATE on the PRIMARY (first repo) app target; other runtimes reach them via ${host_*}. Every real recipe runtime carries buildFromGit (verified; mailpit is the only no-runtime/managed-only recipe); hostname-fallback for empty buildFromGit is a safe degradation never hit in practice. BC: 35 single-pair recipes + laravel-showcase identical. Pinned: `TestDeriveRecipePlan/multi_repo_two_pairs_both_tracked`.
- **P3 DONE** (commit 23a6a82b): `RewriteRecipeImportYAMLFromShape(recipe, overrides)` — shape-driven rewrite keyed by ORIGINAL hostname (runtime rename + managed EXISTS-drop; everything else verbatim; empty overrides = faithful re-marshal keeping EVERY runtime). Legacy `RewriteRecipeImportYAML` + slot-matcher helpers kept ONLY as the parity baseline (deleted in P4). Pinned: `TestRecipeShapeRoundTrip_DerivePlanRewriteParity` (37-recipe identity + legacy byte-parity where the slot-matcher can place the derived plan).
- **P4 / R4 / dev-narrow: TODO** (below). Codex design review in flight (brief `/tmp/codex-brief-r3p4.md`).

## EMPIRICAL FINDINGS (verified by running code, 2026-06-04) — P4 fixes a broad correctness class
The legacy slot-matcher pre-flight (engine.go:581, on the agent's plan) REJECTS / mis-handles
the plan the discover guide tells the agent to author:
- **simple** (nextjs-ssr, lone `prod`): agent `{app, simple}` → ERROR `role=dev` (prod folds to
  stage-role; simple makes a dev-role slot). Recipe can't bootstrap with the obvious plan.
- **cross-type** (vue-static: nodejs dev + static stage): `{nodejs std, static stage}` → ERROR
  `role=stage` type mismatch (slot type = nodejs for both halves).
- **worker** (laravel-showcase): `{app std}` only → legacy SUCCEEDS but worker has no target →
  no ServiceMeta → provisioned-but-untracked.
- **multi-repo** (zerops-showcase): needs 2 std targets; under-authoring drops the 2nd pair.
Today a recipe with NO submitted plan completes discover via the free-text attestation path with
Plan=nil → provision has ZERO targets → ZERO ServiceMetas. So recipe REQUIRES an explicit plan
today, and the obvious plan is rejected for simple/cross-type. P4's derive makes empty-plan the
happy path (mirroring adopt) and tracks every runtime for all modes.

## P4 DESIGN (final, pending Codex sign-off)
1. NEW `Engine.BootstrapCompleteRecipePlan(submitted []BootstrapTarget, schemas, live)` mirroring
   BootstrapCompleteAdoptPlan: load+guard(route==recipe, step==discover) → shape=ParseRecipeImportShape
   → overrides=reconcileRecipeOverrides(shape, submitted) (empty submitted → empty) →
   targets=DeriveRecipePlan(shape, overrides) → store overrides on state → completePlanWithTargets →
   enrich "Derived plan from recipe …".
2. `reconcileRecipeOverrides(shape, submitted)`: match submitted runtime targets to recipe runtimes by
   (canonical type, dev/stage role, first-unused-within-group) to extract hostname renames; EXISTS
   flips by managed hostname. Unmatched submitted → ignored (derived plan stays COMPLETE; worst case a
   rename not honored, never an untracked runtime). Workers are RoleKind=Worker (agent never authored
   one) → not rename-matched.
3. Dispatch (tools/workflow_bootstrap.go ~52): add `route==recipe` branch on step=discover →
   BootstrapCompleteRecipePlan(input.Plan, …) for BOTH empty + explicit. Recipe stops falling through
   to attestation.
4. DELETE recipe pre-flight block (engine.go:567-585) + `ValidateBootstrapRecipeMode` — derived plan is
   mode-correct by construction. (Block is recipe-gated + recipe no longer flows through
   BootstrapCompletePlan, so it's dead for other routes.)
5. Provision guide (bootstrap_guide_assembly.go:78): `RewriteRecipeImportYAMLFromShape(ImportYAML,
   state.RecipeOverrides)` + Localize. DELETE legacy RewriteRecipeImportYAML + buildRuntimeSlots +
   findRuntimeSlot + recipeRuntimeRole + collectManagedDeps + findDepByType + runtimeSlot type + role
   consts. Rewrite recipe_override_test.go (it authors slot-matcher plans).
6. NEW `BootstrapState.RecipeOverrides RecipeShapeOverrides` (JSON-serialized session state).
7. R4 rides on P4 (below): worker target → ServiceMeta ServesHTTP=false at writeProvisionMetas.
**HIGHEST RISK — local-mode dev-drop (mechanism confirmed):** `LocalizeRecipeImportYAML` drops
`zeropsSetup:dev` services from the IMPORT YAML (local dev runtime = user's CWD). The derived PLAN
drives ServiceMetas. `writeProvisionMetas` (bootstrap_outputs.go:43/:138) ALREADY has the local-mode
fallback: a target with `DevHostname==""` + stage → meta keyed on stage with `PlanModeLocalStage`. So
the agent's local plan today carries an EMPTY dev half. ⇒ P4: in local mode, `BootstrapCompleteRecipePlan`
must clear the dev half on standard targets (DevHostname="") before completePlanWithTargets, so the
derived plan matches the localized import (single owner = the shape's RoleKind=Dev decides the drop on
BOTH the plan and the import). Container mode = full plan. (dev-only-narrowing is the OPPOSITE transform
— keep dev, drop stage — and stays a separate opt-in phase.) NOTE: `LocalizeRecipeImportYAML` uses the
`recipeRoleDev` const from recipe_override.go — swap to `RecipeSetupDev` when deleting the slot-matcher.

## P3 — shape-driven RewriteRecipeImportYAML + byte-parity gate
Reimplement `RewriteRecipeImportYAML` (recipe_override.go) to be SHAPE-KEYED instead of the type+role slot matcher:
- Runtime rewrite key = ORIGINAL YAML hostname (`RecipeImportShape.Runtimes[i].Hostname`); apply the override's new hostname. (Replaces `buildRuntimeSlots`+`recipeRuntimeRole`+`findRuntimeSlot` type+role matching — which folds worker→stage and rejects an unused worker slot.)
- Managed rewrite key = original managed hostname. `Resolution=EXISTS` DROPS the YAML service entry (preserve today's behavior, recipe_override.go:102). Managed rename still errors with the current diagnostic.
- Preserve ALL non-hostname fields, especially `buildFromGit`.
- Reuse a single YAML-node parse: add `parseRecipeImportYAML(body) (*parsedRecipeImport{Doc, ServicesNode, Shape}, error)` so rewrite consumes the parsed node (no second parser).
- KEEP the legacy slot matcher available ONLY for the parity-comparison test (delete it in P4 after parity proves out).
- **Gate `TestRecipeShapeRoundTrip_DerivePlanRewriteParity`:** for each of `laravel-minimal, laravel-showcase, nextjs-ssr-hello-world, nestjs-minimal, vue-static-hello-world, zerops-showcase` — recipe YAML → ParseRecipeImportShape → DeriveRecipePlan → new shape-driven rewrite → byte-compare against the CURRENT legacy `RewriteRecipeImportYAML` output (for default hostnames the rewrite is a no-op `newHostname==svcHostname`, so parity holds). Recipe `.import.yml` files are tracked on disk under `internal/knowledge/recipes/`.

## P4 — dispatch + guard deletion + worker meta
- Dispatch: mirror the adopt seam in `internal/tools/workflow_bootstrap.go` (~:52). Add `Engine.BootstrapCompleteRecipePlan(explicit []BootstrapTarget, schemas, live) (*BootstrapResponse, error)` mirroring `BootstrapCompleteAdoptPlan` (adopt.go:107) → `completePlanWithTargets` (engine.go:561). `route=recipe` + `complete step="discover"` with empty/omitted plan → load `RecipeMatch.ImportYAML` → ParseRecipeImportShape → DeriveRecipePlan → completePlanWithTargets. An EXPLICIT plan is reconciled as OVERRIDES against the shape (hostname rename + EXISTS flip), NOT the strict slot matcher.
- DELETE (after parity gate green): `recipeRuntimeRole` fold + `buildRuntimeSlots` 2-slot limit + the "no recipe service matches" reject + `ValidateBootstrapRecipeMode` (engine.go:571 caller). `RewriteRecipeImportYAML` keeps only hostname-sub + EXISTS-drop, driven by the shape.
- Recipe discover GUIDE (bootstrap_guide_assembly.go:61-64, :78): stop telling the agent to author a plan; present the DERIVED plan + the two real choices (rename? narrow?), mirroring adopt's "Auto-derived adoption plan" (adopt.go:188).
- Worker ServiceMeta: `writeProvisionMetas`/`writeBootstrapOutputs` (bootstrap_outputs.go:23/125, recipe gate :72-83/:169-175) — the worker target gets `ServiceMeta{Hostname=workerstage, Mode=simple, PrimarySetupName=worker, ServesHTTP=false}`. Gate-R setup-name (`recipeSetupNamesForTarget`, bootstrap_setup_name.go:45; `verifySetupNameConvention` :114) must read setup names from the shape (worker→"worker"), not only from mode.
- Gates: tool-dispatch tests mirroring adopt empty-plan/empty-array; `bootstrap_setup_name_test`; `scenarios_test.go` (recipe scenarios); `engine_test.go` (5 recipe-route tests); a recipe flow-eval (`recipe-laravel-showcase-fullstack` — worker lands in ServiceMeta + verify shows no http_root fail / no subdomain recovery on the worker).

## R4 — worker ServesHTTP=false (rides on P4)
`ServesHTTP` (workflow.ServiceMeta, service_meta.go:73) is today written ONLY at deploy (deploy_setup_meta.go:12 from `setup.HasPorts()`). R4 adds a PROVISION-time stamp from the shape's `ServesHTTP` (false for workers). Then `classifyRuntime` (verify_checks.go:77 prefers `recordedServesHTTP`) returns Worker for the portless php-nginx worker → skips http_root → no degraded / no wrong "enable subdomain on a worker" recovery. (Narrow R4 — Codex: do NOT build a generic "import-YAML-declares-HTTP" owner; recipe YAML lacks run.ports/httpSupport; stamp worker⇒false from the shape, nil=unknown for adopted/external.) Pin: `internal/ops/verify_classify_bc_test.go` portless-php-nginx-worker → Worker.

## Dev-only narrowing (SEPARATE opt-in phase, AFTER P4)
Karel APPROVED opt-in, no-promote. `CanNarrowRecipeDevOnly(shape, targets, approved) error` — allow only when: approved opt-in true; `shape.Mode()==standard`; retained runtimes only RoleKindDev; all retained targets BootstrapMode=dev; no stageHostname; managed deps preserved; rewrite drops stage/worker runtime services. Use `PlanModeDev` (NOT simple) so launch/close see no-promote target. Never a silent default. This fixes the kanban "provisioned a paid appstage against explicit 'dev only'" harm.

## Blast-radius (Explore map essentials)
- `InferRecipeShape` callers: engine.go:412, recipe_corpus_store.go:42 (set RecipeMatch.Mode). `RewriteRecipeImportYAML` callers: engine.go:581, bootstrap_guide_assembly.go:78. `ValidateBootstrapRecipeMode` caller: engine.go:571.
- `RecipeSetupDev/Prod/Worker` + `SharesAppCodebase` in recipe_service_types.go. Worker decision (Codex): do NOT collapse a shared-codebase worker into the app meta — it's a real runtime, own ServiceMeta, independently scopeable/verifiable.
- local-mode `LocalizeRecipeImportYAML` (recipe_import_local.go:36) drops zeropsSetup:dev — DeriveRecipePlan stays compatible (fewer runtimes = OK).
- Recipes: laravel-showcase has the worker; 12 `*-static-hello-world` recipes are cross-type (nodejs dev → static stage).
