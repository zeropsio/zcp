# DeployIntent resolver — workflow-side classifier for deploy plan generation

**Surfaced**: 2026-04-30 — `plans/pre-internal-testing-fixes-2026-04-30.md`
Phase 3 (H1 fix). The H1 closure landed a SYMPTOM-FIX inline at
`internal/workflow/build_plan.go::deployActionFor`: it consults
`env.Services` to find the dev half of a stage pair and emit cross-deploy
args. The TODO comment on `deployActionFor` names DeployIntent as the
structural target.

**Why deferred**: H1's symptom fix is small, contained, and covered by 3
unit tests. The DeployIntent refactor would unify (mode, deploy-class,
source, target, setup) in one place feeding both build_plan and
ops.ClassifyDeploy — that's a 4-5 file structural change that touches
the deploy classification path, the typed Plan generation, and any
future deploy-related handler. Out of scope for the pre-internal-testing
fix bundle.

**Trigger to promote**: a third deploy-class-related symptom-fix is
needed (after H1's stage-half cross-deploy and any future "first deploy
ignores close-mode in Plan" type work). Two parallel symptom-fixes can
live as comments; three signals it's time for the structural fix.

## Sketch

New package or sub-namespace `internal/workflow/deployintent`:

1. **Type**: `DeployIntent { Mode topology.Mode; Class DeployClass;
   Source, Target string; Setup string; Reason string }` — a
   deterministic projection of (ServiceSnapshot, target hostname) onto
   what `zerops_deploy` would dispatch on.

2. **Resolver**: `Resolve(env StateEnvelope, target string) DeployIntent`
   pure function:
   - Lookup target snapshot in `env.Services`.
   - If `target.Mode == ModeStage` → find dev half via `StageHostname`
     index → emit cross-deploy with setup="prod".
   - Else → emit self-deploy with target's setup name (or default).
   - Build-time first-deploy skip rules (D2a — first deploy ignores
     close-mode) folded in here.

3. **Consumers**:
   - `build_plan.go::deployActionFor` calls `DeployIntent.Resolve`,
     converts to NextAction shape.
   - `ops/deploy_classify.go::ClassifyDeploy` (currently called at
     deploy time) consults the same Intent — no double-source-of-truth.
   - Future H1-class symptoms (e.g. local-stage with cross-deploy
     semantics) get their dispatch from the resolver, no new ad-hoc
     branch in build_plan.

4. **Pin tests**: golden-table that for every (snapshot shape, target
   hostname) fixture, the resolver returns the expected DeployIntent.
   Pinned by both build_plan_test.go (Plan emission) and
   deploy_classify_test.go (ClassifyDeploy consumption).

## Risks

- Touches the deploy classification path — must not regress DM-2 / DM-3
  / DM-4 invariants pinned at `TestValidateZeropsYml_DM2_*` / `DM3_*`.
- Refactor cost: ~4-5 files, ~200 LOC, ~10 new tests. Need a clean cut
  date so the change doesn't drag.
- DeployIntent and `ClassifyDeploy` both currently produce sub-truths
  about a deploy. Unifying needs care to preserve backward compat for
  any test/handler that depends on the current `ClassifyDeploy`
  signature. Likely solvable with `ClassifyDeploy` becoming a thin
  adapter that calls Resolve internally.

## Refs

- H1 symptom-fix landing in commit `cfbd0793`
  (Phase 3 of `plans/pre-internal-testing-fixes-2026-04-30.md`).
- TODO comment on `deployActionFor` at `internal/workflow/build_plan.go:251`
  (per Codex POST-WORK file:line citation).
- `internal/ops/deploy_classify.go::ClassifyDeploy` — current
  classifier producing the parallel sub-truth.
- Audit H1 verified at HEAD `9669ebb5`:
  `internal/workflow/build_plan.go:242-249`.
