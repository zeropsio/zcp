# R3-P4 — Recipe-route bootstrap: single-owner derive (authoritative impl spec)

Merges the whole-system workflow design (wf_097b0e06-d54) + Codex P4 review + 3 adversarial
verdicts. Parent: `plans/r3-recipe-shape-impl-2026-06-04.md`, `plans/zcp-consolidation-2026-06-04.md` (R3).

## The root (one violation, six symptoms)
The recipe import YAML OWNS the service shape. The live path derives the **TELL** from it
(`InferRecipeShape` → mode banner + verbatim YAML at discover) but derives the **CHECK/DO** from a
re-authored intermediate — the agent's hand-written plan, slot-matched back onto the YAML
(`engine.go:581` pre-flight + `bootstrap_guide_assembly.go:78` provision rewrite). Two derivations
of one shape from two sources → drift → ships green. The slot-matcher (`buildRuntimeSlots` +
`findRuntimeSlot` + `recipeRuntimeRole`) is a LOSSIER model than the recipe: one dev/stage pair of
one type. Every symptom is that model being narrower than the recipe it validates:
simple-rejected, cross-type-rejected, worker-untracked, multi-repo-dropped, empty-plan→zero-metas,
local-dev-drop-incoherence. The shipped R3 code (`DeriveRecipePlan`, `RewriteRecipeImportYAMLFromShape`)
is the correct single owner but is **dead in production** — nothing calls it but the test. P4 wires
it in as the ONE source of tell + do, and deletes the slot model.

## The contract (what recipe-route is FOR)
C1 provision exactly the recipe's shape (every runtime: app dev/stage, workers, secondary-repo pairs + managed deps).
C2 register a ServiceMeta for EVERY provisioned runtime (untracked = invisible to all downstream flows).
C3 let the user adjust only what the YAML can't hold — hostname renames + managed EXISTS flips. Nothing else.
C4 tell the truth about the deploy model (buildFromGit auto-deploys at import) at the step the agent acts on it.
Job = **derive-and-confirm**, not author-and-validate.

## How it connects to all flows (the meta is the contract surface)
Recipe-bootstrap's only durable output is the ServiceMeta set; downstream reads metas, never re-reads the recipe.
Three meta fields are knowable from the shape at PARSE time: **Mode**, **setup names**, **ServesHTTP**.
| field | from shape | consumed by | breaks if wrong |
|---|---|---|---|
| Mode | standard/simple/dev/local-stage | develop ModeFor, deploy next-action, subdomain probe, launch git-push | stage read as dev → 502 probe misfires |
| StageHostname | stage half of standard pair | pair-keyed resolution, SetupNameFor | pair invariant (E8) |
| PrimarySetupName/StageSetupName | **literal zeropsSetup string** (dev/prod/worker) | deploy_preflight SetupNameFor | wrong setup block bound at deploy |
| ServesHTTP *bool | false for workers, true for HTTP | verify classifyRuntime | nil on worker → http_root → "enable subdomain on a worker" → platform rejects |
Export reads live API (unaffected). Launch gates on IsComplete + GitPushState (worker-no-push → R-5 backlog).

## The single-owner model
recipe YAML → ParseRecipeImportShape → **RecipeImportShape (OWNER)** + RecipeShapeOverrides (agent's only input)
→ {DeriveRecipePlan (the TELL: shown at discover; the plan: stored), RewriteRecipeImportYAMLFromShape
(what import gets), setup-names + ServesHTTP (from RoleKind/zeropsSetup)}. Tell==do because all derive
from the same (shape, overrides).

## ADVERSARIAL CATCHES — must bake into the implementation (Codex + 3 verdicts converge)
1. **reconcile = IDENTITY join, NOT ordinal.** Agent can reorder targets → ordinal swaps dev↔stage.
   Match `submitted.DevHostname`/`ExplicitStage` against the shape's ORIGINAL hostnames (identity); the
   override is recorded only where the submitted name differs from a matched original. Reject on count
   mismatch / managed-rename / mode-divergence with a diagnostic that shows the derived shape. Pin the
   reorder + both-halves-renamed + multi-repo cases.
2. **local-narrow must be CONSISTENT across discover + derive + import.** If discover shows dev+stage but
   complete narrows to stage-only, a 2-target submission fails reconcile (count mismatch). FIX: narrow the
   shape (drop RoleKind=dev) at the SHAPE level, and apply it in BOTH the discover guide AND
   BootstrapCompleteRecipePlan AND the rewrite. The agent sees + submits the narrowed (stage-only) shape.
   Delete the `bootstrap_outputs.go:43/:138` local-stage fallback (no longer needed — derive yields the
   stage-keyed target directly). LocalizeRecipeImportYAML becomes redundant for recipe (only caller is
   bootstrap_guide_assembly.go:87 + its test — confirmed by grep); supersede it.
3. **worker setup-name = LITERAL zeropsSetup** ("worker"), not the mode→convention table ("prod"). Carry
   the runtime's zeropsSetup string on the target; set meta.PrimarySetupName from it. dev/stage still map
   to dev/prod because that IS their zeropsSetup. Delete `recipeSetupNamesForTarget` + `verifySetupNameConvention`
   (a re-derivation that diverges for workers; once the name IS the recipe's value it can't drift).
4. **ServesHTTP plumbing** — `RuntimeTarget` has NO ServesHTTP field yet. Add `ServesHTTP *bool` (json omitempty);
   DeriveRecipePlan sets it from `shape.Runtimes[i].ServesHTTP`; writeProvisionMetas/writeBootstrapOutputs set
   meta.ServesHTTP from it. **mergeExistingMeta MUST carry it forward** (else dev→standard expansion drops a
   known worker classification → http_root false-positive).
5. **mergeExistingMeta setup-name + ServesHTTP semantics on the convention→literal transition (subtle BC).**
   An on-disk worker meta written under old convention (`PrimarySetupName='prod'`) would WIN over the new
   'worker' via migrate-forward-empty → deploy binds wrong setup block. FIX: for PrimarySetupName/StageSetupName/
   ServesHTTP the FRESH derived value wins on re-bootstrap (these are recipe-owned, not develop-owned like
   CloseDeployMode/GitPushState). Explicitly split merge semantics: develop-owned fields migrate-forward;
   recipe-shape-owned fields take the fresh derived value.
6. **mailpit / managed-only** — DeriveRecipePlan errors on a shape with no runtimes (mailpit: buildFromGit
   service but no zeropsSetup → managed-only). Graceful path: managed-only recipe → managed-only bootstrap
   (no runtime targets, like the existing managed-only attestation/close path), NOT an error.
7. **old-plan tolerance (BC).** A mid-flight session with an old agent-authored Plan (no RecipeOverrides):
   provision shape-rewrite with nil overrides = verbatim (safe for default hostnames). If the old Plan
   renamed a host, lazily reconcile Plan→overrides at provision so the rename isn't silently lost. The
   dispatch must ACCEPT an old-style plan-with-bootstrapMode (users' on-disk CLAUDE.md/AGENTS.md still say
   "set bootstrapMode") — reconcile extracts renames/EXISTS, derive owns mode/type/pairing. Never REJECT an
   old-style plan; that's the BC seam.
8. **provision rewrite no longer soft-falls-back** — shape-rewrite can only error on malformed/empty/missing-
   services YAML; surface a clear "recipe corpus invalid" error from BootstrapCompleteRecipePlan instead.

## Unification with adopt (structural, not deriver-sharing)
adopt + recipe are now identical SHAPE: derive []BootstrapTarget from a route-specific owner → commit via
the shared `completePlanWithTargets`. Do NOT merge InferServicePairing + DeriveRecipePlan (owners + dep
semantics genuinely differ: adopt EXISTS-on-both; recipe CREATE-once-on-primary). Unify the DISPATCH shape:
adopt/recipe/classic branches each "empty→derive, non-empty→reconcile-or-validate, then commit". The
CLAUDE.md adopt invariant ("auto-derives; agent authors nothing") becomes shared adopt+recipe; classic is
the sole agent-authored route.

## Phasing (additive first, delete last, flow-eval gate before deletion)
- **P4.0** add `RuntimeTarget.ServesHTTP *bool` + setup-name carrier (literal zeropsSetup) on the target;
  DeriveRecipePlan sets both from shape. Unit: derive emits ServesHTTP + literal setup-name per role.
  Round-trip stays green. (additive, internal-only)
- **P4.1** `reconcileRecipeOverrides(shape, submitted)` (IDENTITY join) + `BootstrapState.RecipeOverrides
  *RecipeShapeOverrides` (pointer, omitempty, JSON-tagged). Unit table: reorder/both-renamed/managed-rename/
  count-mismatch/mode-divergence. Old-session fixture deserializes nil.
- **P4.2** `Engine.BootstrapCompleteRecipePlan(submitted, schemas, live)` mirroring adopt + dispatch branch in
  workflow_bootstrap.go (empty→derive, non-empty→reconcile, old-style-plan tolerated). managed-only graceful.
  Slot-matcher STILL LIVE (classic path) but unreachable for recipe. Integration: simple/cross-type/worker/
  multi-repo all complete discover (4 rejections gone); empty plan derives full shape.
- **P4.3** local-narrow at shape level, consistent across discover guide + complete + rewrite; delete
  bootstrap_outputs.go:43/:138 fallback. Unit + flow-eval-local recipe scenario.
- **P4.4** provision guide → RewriteRecipeImportYAMLFromShape(RecipeOverrides); split discover-shape-presentation
  vs provision-procedure text; surface buildFromGit + worker-no-HTTP + local-dev-drop at discover. Golden:
  bootstrap_guide_assembly_test rewritten. Atoms bootstrap-recipe-match.md / -import.md rewritten (stop telling
  the agent to author; "confirm / rename on collision"). Schema text WorkflowInput.Plan (workflow.go:51) updated.
- **P4.5** wire ServesHTTP + literal setup-name into writeProvisionMetas/writeBootstrapOutputs + mergeExistingMeta
  (fresh-wins for recipe-owned fields). Unit: worker meta ServesHTTP=false + PrimarySetupName="worker".
  Integration: verify dispatches worker class on a fresh worker (no subdomain recovery).
- **P4.6** DELETE slot-matcher (RewriteRecipeImportYAML + buildRuntimeSlots/findRuntimeSlot/recipeRuntimeRole/
  runtimeSlot/collectManagedDeps/findDepByType), ValidateBootstrapRecipeMode, recipe pre-flight engine.go:570-585,
  recipeSetupNamesForTarget + verifySetupNameConvention, LocalizeRecipeImportYAML (if grep-confirmed sole caller).
  Add a guard in BootstrapCompletePlan: route==recipe → delegate to BootstrapCompleteRecipePlan. Keep YAML-node
  helpers (documentRoot/mappingValue/mappingScalar/setMappingScalar). grep zero refs; round-trip 37/37.
- **P4.7** flow-eval gate (the proof): recipe-nextjs-ssr (simple), a cross-type, recipe-laravel-showcase-fullstack
  (worker tracked + no http_root recovery), zerops-showcase (multi-repo both pairs metas).

Tests: DELETE TestRewriteRecipeImportYAML + TestValidateBootstrapRecipeMode; REWRITE bootstrap_guide_assembly_test
recipe cases + the legacy-parity assertion in recipe_override_shape_test.go (the slot-matcher it compares against
is gone — replace with canonical-remarshal identity only); ADD reconcile table, BootstrapCompleteRecipePlan
(mirror TestHandleBootstrapComplete_Adopt*), worker ServesHTTP/setup-name pins, local-narrow, F6 recipe tests
moved off BootstrapCompletePlan (engine_test.go:1001).

## Decisions made (no blocking question)
- **reconcile, NOT a direct-overrides tool schema** — keep `plan: []BootstrapTarget` wire format (BC mandate:
  existing sessions/allowlists/on-disk guides). Backlog the direct-overrides schema as a future narrowing.
- **tolerant of old-style plans** — reconcile extracts what it can; derive owns the rest; never reject a
  plan-with-bootstrapMode.
- **worker setup-name = literal zeropsSetup** (the convention table is the bug).

## Risks → backlog
- R-5: launch gates a tracked worker on git-push (`source-control-required`). Correct behavior now surfacing on a
  now-tracked service. Backlog `plans/backlog/launch-worker-no-push.md`: launch should skip git-push enforcement
  for ServesHTTP=false runtimes promoted as part of a recipe app. Do NOT special-case silently in P4.
- R-6: multi-dev-per-repo folds 2nd dev/stage→worker target (no current recipe hits it); document in DeriveRecipePlan doc-comment.
