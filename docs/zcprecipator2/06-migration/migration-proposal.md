# migration-proposal.md — parallel-run vs cleanroom, resolved

**Purpose**: resolve open decision #4 (deferred from [RESUME.md](../RESUME.md) + [README.md §8](../README.md) at research kickoff) against concrete step-3 evidence. Compare the two candidate shapes — **parallel-run on v35** vs **cleanroom** — on four axes: (1) Go-code surface replaced vs added-alongside, (2) runtime coexistence feasibility of old and new code paths, (3) shadow-diff cost per v35 candidate run, (4) rollback cost per candidate. Output: an explicit recommendation with justification.

Conventions:
- "Old path" = current `internal/content/workflows/recipe.md` (3,438 lines, 60+ `<block>` regions) + `recipe_topic_registry.go` + `recipe_guidance.go buildSubStepGuide` + current 12 `workflow_checks_*.go` files + current `StepCheck` failure payload (with `ReadSurface/HowToFix/CoupledWith/PerturbsChecks` v8.96 + v8.104 extensions).
- "New path" = [`atomic-layout.md`](../03-architecture/atomic-layout.md)'s 86-atom tree + new `atom_manifest.go` + rewritten `recipe_guidance.go` + [`check-rewrite.md`](../03-architecture/check-rewrite.md) 56 keep / 16 rewrite / 1 delete / 5 new = 78 final check surface + 16 `zcp check <name>` CLI shim + shrunken `{name, detail, preAttestCmd, expectedExit}` failure payload + P3 `SymbolContract` plan field + P5 `FactRecord.RouteTo` + P4 empty `NextSteps[]` at close.

Load-bearing inputs:
- [`principles.md §10`](../03-architecture/principles.md) Principle-to-enforcement-layer map (which layers change)
- [`atomic-layout.md §3`](../03-architecture/atomic-layout.md) block → atom mapping + [`§6`](../03-architecture/atomic-layout.md) Stitching conventions (guidance-side delta)
- [`check-rewrite.md §3`](../03-architecture/check-rewrite.md) disposition counts + [`§17`](../03-architecture/check-rewrite.md) summary matrix + [`§18`](../03-architecture/check-rewrite.md) execution-plane implications (check-side delta)
- [`calibration-bars-v35.md`](../05-regression/calibration-bars-v35.md) 97 bars (go/no-go surface)
- [`defect-class-registry.md`](../05-regression/defect-class-registry.md) 68 closed classes (coverage baseline)

---

## 1. Surface-delta quantification — how much Go code changes

### 1.1. Operational substrate (DOES NOT CHANGE under either candidate)

Per [README.md §1](../README.md) — validated pristine by v34:

| Substrate surface | File(s) | Disposition |
|---|---|---|
| MCP tool implementations (deploy / dev_server / browser / logs / verify / subdomain / mount / import / env / discover / knowledge / record_fact) | `internal/tools/*.go` except `workflow_checks_*.go` | Untouched |
| Workflow state machine (substep attestation, SUBAGENT_MISUSE, ordering gates) | `internal/tools/workflow.go`, `internal/workflow/session.go`, `work_session.go` | Untouched |
| SSH boundary (v17.1 / v8.90) | `internal/platform/ssh/*`, bash_guard middleware | Untouched |
| git-config-mount pre-scaffold init (v8.93.1) | `internal/workflow/recipe_substeps.go` provision hooks | Untouched |
| Post-scaffold `.git/` cleanup rule (v8.96 Fix #4) | provision hooks | Untouched |
| Export-on-request (v8.103) | `internal/sync/push_recipes.go`, `ExportRecipe` gate | Untouched |
| Read-before-Edit sentinel (v8.97 Fix 3) | MCP tool Edit impl | Untouched |
| Facts log core | `internal/ops/facts_log.go` (`FactRecord.Scope` kept) | **Extended** (adds optional `RouteTo` field) — backward compatible |
| Env-README Go-source templates (v8.95 Fix B) | `internal/workflow/recipe_templates.go` | Untouched |
| `zerops_workspace_manifest` | substrate tool | Untouched |
| Dev-server spawn shape (v17.1) / pkill classifier (v8.80) / port-stop polling (v8.104 Quality Fix #2) | substrate | Untouched |

Both candidates leave the substrate untouched. Rollback scope is bounded to the new-path commit range in either case.

### 1.2. Guidance-layer delta (atomic-layout.md §3 + §6)

| File | Old path | New path | Change class |
|---|---|---|---|
| `internal/content/workflows/recipe.md` | 3,438 lines, 60+ `<block>` regions | deleted | **Replace** (not extend) |
| `internal/content/workflows/recipe/` (directory) | — | 86 atom files, ≤300 lines each, ~6,500 lines total after dedup | **Add new tree** |
| `internal/workflow/recipe_topic_registry.go` | topic-to-block mapping (eager-scope + substep-scope declarations) | deleted | **Replace** |
| `internal/workflow/atom_manifest.go` | — | path-manifest for the 86 atoms + audience metadata + tier-conditional flags | **New file** |
| `internal/workflow/recipe_guidance.go buildSubStepGuide` + eager composition | block-lookup from recipe.md via topic registry | atom-lookup via atom_manifest + stitcher helpers (`buildStepEntry`, `buildSubStepCompletion`, `buildScaffoldDispatchBrief`, `buildFeatureDispatchBrief`, `buildWriterDispatchBrief`, `buildCodeReviewDispatchBrief`) | **Replace** (same function signatures, different implementation) |
| `internal/workflow/recipe_brief_facts.go BuildPriorDiscoveriesBlock` | pulls from facts log filtered by Scope | same filter, minor addition: consume `RouteTo` for writer/code-review lanes | **Extend** |
| `internal/workflow/recipe_plan.go Research` | current fields | adds `SymbolContract` sub-struct | **Extend** (additive) |
| `internal/workflow/symbol_contract.go` | — | derivation helper `BuildSymbolContract(plan.Research) SymbolContract` with 12 seeded FixRecurrenceRules | **New file** (~200 lines per [`atomic-layout.md §4`](../03-architecture/atomic-layout.md)) |
| `docs/zcprecipator2/DISPATCH.md` | — | human-facing dispatcher composition guide | **New file** (non-code) |

Net Go-source delta on guidance layer:
- **Deleted**: `recipe_topic_registry.go` (~300 lines) + block-lookup logic in `recipe_guidance.go` (~400 lines of current stitching)
- **Rewritten**: `recipe_guidance.go` stitcher body (~500 lines after rewrite, down from ~800 lines of block-composition logic)
- **Added**: `atom_manifest.go` (~200 lines) + `symbol_contract.go` (~200 lines)
- **Content tree**: `recipe.md` (3,438 lines) deleted → `recipe/` (86 files × ~75 avg lines = ~6,500 lines) added

Guidance-layer Go surface: **~-400 LoC Go + ~+3,000 LoC markdown (net) after dedup**. The Go surface is roughly net-neutral; the content tree grows because the monolith had aggressive cross-audience deduplication that atomization reverses (principle P6 accepts this — per-atom testability + per-atom versioning + bounded review).

### 1.3. Check-layer delta (check-rewrite.md §3 + §17 + §18)

Per [`check-rewrite.md §17 summary matrix`](../03-architecture/check-rewrite.md):

| Change class | Count |
|---:|---|
| Kept (predicate unchanged; runnable pre-attest added to atom) | 56 |
| Rewrite-to-runnable (shim CLI introduced; predicate refactored into reusable `ops.Check<Name>(...)` Go function) | 16 |
| Delete (upstream-handled) | 1 (`knowledge_base_exceeds_predecessor`) |
| New (architecture-level, per P3 / P5 / P6 / P8) | 5 |
| **Total after rewrite** | **78** |

Per [`check-rewrite.md §18 execution-plane implications`](../03-architecture/check-rewrite.md): rewrite-to-runnable checks share the **same predicate Go function** between server-side gate invocation and author-side `zcp check <name>` CLI shim. Design invariant: impossible to diverge. This means the check-layer Go code is not "two implementations" — it's one implementation with two call sites (gate + shim). The 16 rewritten checks are refactors, not reimplementations.

Check-layer Go surface:
- **Rewritten**: 16 check predicates extracted into `ops/checks/*.go` with CLI adapter in `cmd/zcp/check/*.go` (~400–600 LoC added for CLI surface; check body logic moves, doesn't grow)
- **Added**: 5 new checks (~50–100 LoC each = ~400 LoC) with paired `zcp check` CLI subcommands
- **Deleted**: 1 check (~30 LoC) + its test
- **Failure payload shape**: `StepCheck` drops `ReadSurface/Required/Actual/CoupledWith/HowToFix/PerturbsChecks`; adds `PreAttestCmd` + `ExpectedExit`. Per [`data-flow-showcase.md §9`](../03-architecture/data-flow-showcase.md): ~3× payload size reduction but the value is convergence, not bytes. **This is a struct-shape change — breaking to the failure-consumer contract.**

### 1.4. Surface-delta summary

| Layer | Old-replaced | New-added | Net change |
|---|---|---|---|
| Operational substrate | 0 | 0 | unchanged |
| Guidance Go | ~-400 LoC | ~+400 LoC | neutral, functions replace in-place |
| Guidance content | recipe.md (3,438 lines) | recipe/ (86 files, ~6,500 lines) | monolith → tree |
| Check Go | 1 deletion + 16 refactors | 5 new + CLI shim surface | net +~400 LoC |
| Plan schema | — | `SymbolContract` field | additive |
| FactRecord schema | — | `RouteTo` field | additive |
| StepCheck payload | 6 verbose fields | `PreAttestCmd` + `ExpectedExit` | breaking shape change |
| Server NextSteps @ close | 1-entry autonomous | empty | server-side config change |

---

## 2. Runtime coexistence — can old and new paths coexist?

### 2.1. Coexistence class-by-class

| Axis | Coexistence feasibility | Mechanism |
|---|---|---|
| Content tree | ✅ trivially — different paths (`recipe.md` vs `recipe/`) | No feature flag needed; disk-level separation |
| `SymbolContract` plan field | ✅ additive — old path ignores the field; new path populates and reads it | Unused-field tolerance in Go struct deserialization |
| `FactRecord.RouteTo` field | ✅ additive — old writer manifest schema accepts but doesn't require; old check ignores | Optional field in JSON |
| Atom manifest vs topic registry | ⚠️ requires feature flag — both exist as Go symbols, but `buildSubStepGuide` calls exactly one | Env var `ZCP_RECIPE_V2=true` dispatches to new stitcher |
| Guidance stitcher body | ⚠️ same — one function, two bodies selected by flag | see above |
| Check suite | ⚠️ `StepCheck` struct shape differs; dual-shape requires either (a) union struct with optional fields, or (b) shape-per-path dispatch | Either dual-struct or flag-dispatched gate |
| Failure payload | ⚠️ shape change — agent consumes either verbose v8.96/v8.104 shape or shrunken `{name,detail,preAttestCmd,expectedExit}` | Flag-dispatched emission |
| NextSteps @ close | ✅ config-level — default-empty is compatible with export-on-request substrate | No flag needed |
| TodoWrite discipline (P4) | ✅ agent-side behavior; no server enforcement needed | Purely in content |
| Build-time lints (P2 / P6 / P8 grep guards) | ✅ applied only to the new content tree | Scope-limited |

### 2.2. Coexistence verdict

**Coexistence is feasible with a feature flag, but costly.** Three classes require either a runtime dispatch (flag) or dual-struct support: stitcher body, check payload shape, failure payload. The rest is additive.

The feature-flag cost:
- **Double content tree maintenance** during transition: any substrate fix or recipe-behavior change landed during the parallel window must update BOTH `recipe.md` AND the atom tree. Content drift between the two is a silent regression class identical to v22/v23 gotchas-as-incident-log (old-path fix doesn't reach new-path atoms).
- **Dual-shape `StepCheck`** increases struct surface in every callsite: server gate, CLI shim, failure payload consumer in agent. A single shape means one data contract; two shapes means every consumer branches.
- **Test surface doubles**: every path-dependent test needs old-path + new-path variants.
- **Test isolation risk**: CLAUDE.md warns against global mutable state; a feature flag is effectively global state that test parallelization must respect. Either every test that touches emission runs non-parallel, or the flag becomes per-context (painful plumbing).
- **Substrate invariant risk**: v8.90 `SUBAGENT_MISUSE` enforcement, v8.97 Fix 3 Read-before-Edit sentinel, etc. interact with agent-facing content; dual paths double the invariant-verification surface.

### 2.3. The structural case for NOT coexisting

Three architectural points push against parallel:

1. **P1's whole value is agent behavior change.** Author-runnable pre-attest means the agent runs shims locally and the gate confirms. The old path's rich-metadata-on-failure vs the new path's compact-preAttest-payload produces different agent loops. You cannot shadow-diff this: the agent can only follow one path in a given run. You can compare composed briefs byte-by-byte (cheap, not v35's question), but you cannot compare convergence behavior without an agent actually running on each path (expensive, and v35 is exactly one run).

2. **P2's leaf-artifact separation is a content-tree invariant.** The new tree's `briefs/` directory carries build-time grep guards forbidding dispatcher vocabulary, version anchors, internal check names, Go-source paths ([`principles.md P2 §Enforcement`](../03-architecture/principles.md)). The old `recipe.md` violates all four guards (~80 version-anchor matches alone). Cohabiting trees means the grep guards apply to one and not the other, and any cross-pollination (copy-paste between trees) silently leaks forbidden tokens.

3. **P4's server-state-is-plan principle is a user-facing framing change.** Step-entry atoms forbid "your tasks for this phase are …" phrasing (build-time lint H-4 in calibration-bars). Old path's blocks read as fresh planning context; new path's atoms read as "substep X completes when predicate P holds." A flag-toggle between phrasings during a run would be incoherent; across runs, it halves the data usable for v35 convergence measurement.

These aren't individually dealbreakers, but collectively they say: the two paths represent fundamentally different agent-interaction contracts, not incremental delta. Parallelizing them duplicates the maintenance burden while providing no usable signal that a cleanroom wouldn't provide through its own rollback criteria.

---

## 3. Shadow-diff cost — what does "run both and compare" actually buy

### 3.1. What shadow-diff can measure

- **Composed brief byte-diff** — produce the scaffold / feature / writer / code-review dispatches under both paths from the same `plan.Research`, diff bytes. Cost: low (compute-only; no agent). Value: confirms atomic stitching reproduces v34's transmitted content structure **minus the things we want removed** (dispatcher text, version anchors, internal vocab). Step 4 already did this exercise against captured v34 dispatches — see [`../04-verification/brief-*-diff.md`](../04-verification/). The shadow-diff at v35 would be redundant.
- **Step-entry guide byte-diff** — same as above at step-entry boundaries. Cost: low. Value: confirms no guidance lost in atomic decomposition. Step 4 coverage does this per role × tier.
- **Check-list byte-diff** — enumerate StepCheck rows server emits per phase-complete under both paths. Cost: low. Value: confirms no gate silently removed. `check-rewrite.md §17` summary matrix already reconciles this (56 + 16 + 1 + 5 = 78 vs current ~73).

### 3.2. What shadow-diff CANNOT measure

- **Agent convergence rounds** (C-1 / C-2 in `calibration-bars-v35.md` — bar #0 for the whole rewrite). Convergence emerges from the agent interacting with the payload shape under one architecture. Running the agent under the old path AND the new path in the same session is impossible (one plan, one attestation sequence). Running sequentially is two runs, not one run with shadow.
- **Cross-scaffold contract adherence** (CS-1 / CS-2 — v34 DB_PASS recurrence). SymbolContract's byte-identical interpolation requires the scaffold dispatch to happen under the new stitching path. Old path can't produce the contract fragment; there's no byte-comparable counterfactual.
- **Manifest honesty across all routing dimensions** (M-3a..f — P5 v34 direct fix). The expanded honesty check's failure surface needs `RouteTo` populated by a new-path writer. Old path writer doesn't emit `RouteTo`, so the check has no input.
- **TodoWrite check-off-only pattern** (C-11 — P4). This is agent discipline under new-path framing. Can't be shadowed.

### 3.3. Shadow-diff verdict

**Shadow-diff buys almost nothing v35 doesn't already get from the step-4 coverage artifacts + post-v35 calibration-bar measurement.** The byte-diff work is already done against captured v34 dispatches (see [`../04-verification/brief-*-diff.md`](../04-verification/) — 9 role×tier pairs × 4 artifacts = 36 files). What shadow cannot provide — convergence rounds, SymbolContract adherence, manifest honesty — is exactly what v35 is supposed to measure, and it requires the agent to actually run on the new path.

Cost-benefit:
- Shadow cost: feature flag plumbing across guidance + checks + payload + NextSteps (~500 LoC of dual-dispatch), test-surface doubling, content-drift risk during transition.
- Shadow benefit: byte-diff that step 4 already produced against v34.
- **Net: negative.**

---

## 4. Rollback cost — what happens if v35 regresses

### 4.1. Parallel-run rollback

Under parallel-run, v35's `ZCP_RECIPE_V2=true` triggers new path; rollback = flip the flag to `false` (or remove the env-var setter from the run environment). Rollback is ~seconds. Recovery keeps the old tree intact and falls back to the v34 behavior.

**But**: the flag-based rollback is only "cheap" if the dual-path code survives rollback-unmodified. Any post-v35 hotfix lands on one path or the other — if on the new path, old path doesn't benefit; if on both paths, content drift continues. The longer parallel lives, the harder cleaning up becomes. And "cheap rollback" has a hidden tax: the new path's code stays shipped, polluting the repo with experimental code some subset of devs think is the future and some subset think is abandoned.

### 4.2. Cleanroom rollback

Under cleanroom, v35 runs on new-only; rollback = `git revert <commit-range>` against the rollout-sequence commits (see [`rollout-sequence.md`](rollout-sequence.md)). Rollback is ~minutes (one revert commit per rollout commit, plus CI/tests). Recovery restores `recipe.md` + `recipe_topic_registry.go` + current check suite + v8.96/v8.104 failure payload fields.

**Rollback scope**: ≤15 commits per [`rollout-sequence.md`](rollout-sequence.md); each individually revertable. The operational substrate is untouched (§1.1), so rollback doesn't threaten any v8.90 / v8.93.1 / v8.95 / v8.96 / v8.97 / v8.103 / v8.104 invariants.

**Rollback risk**: if during the rollout-sequence any commit broke a substrate invariant (regression class not caught by step-1 analysis), rollback may partially fail. Mitigation: rollout-sequence's C-0 commit adds substrate-regression tests before any new-path commits ship, and every subsequent commit runs the CI harness covering those tests.

### 4.3. Rollback-cost summary

| Dimension | Parallel-run | Cleanroom |
|---|---|---|
| Rollback latency | seconds (flag flip) | minutes (revert commit range) |
| Rollback cleanliness | fragile (dual-code ossification; drift accumulates) | clean (reverts restore old state exactly) |
| Substrate risk | low (same for both) | low (same for both) |
| Post-rollback forward path | ambiguous — which path evolves? | clear — fresh attempt on old substrate, new-path commits revertable individually for re-attempt |
| Total code-base complexity post-rollback | **higher** (dual paths permanent) | **lower** (one path, clean history) |

Cleanroom's rollback is slower per event but cheaper in aggregate across the decision horizon. Parallel-run's "fast rollback" optimizes for the wrong metric: latency-of-revert-decision when the real cost is *post-rollback maintenance*.

---

## 5. Decision criteria check

Per [README.md §3 step 6](../README.md) decision criteria:

### 5.1. How big is the delta?

Answer from §1: guidance-layer net-neutral Go (~400 LoC replaced by ~400 LoC), atomic content tree grows ~2× from monolith (dedup undone by design per P6), check layer grows ~+400 LoC CLI surface + 5 new checks, plan schema grows 1 field, FactRecord schema grows 1 field, **StepCheck payload shape changes (breaking)**.

The delta is *large in content-tree surface* but *bounded in Go surface*. Parallel preservation is plausible *for the additive fields* but requires feature-flag dispatch for stitching + check-suite — which is the expensive part.

### 5.2. Can old and new coexist at runtime?

Answer from §2: Yes, with feature flag. Cost is double content maintenance + dual-struct check surface + test-surface doubling + global-state-via-env-var tension with test parallelism. Maintaining parallel paths during transition invites exactly the defect class P6 atomization is closing (monolithic content with mixed cohabiting concerns).

### 5.3. What's the shadow-diff cost?

Answer from §3: compute-cost is low; **value is negligible above what step 4 already produced against captured v34 dispatches**. The interesting signal v35 produces — convergence rounds, SymbolContract adherence, manifest honesty — requires an agent to actually run on one path, which is binary not shadowable.

### 5.4. What's the rollback cost?

Answer from §4: cleanroom = minutes latency, clean history; parallel = seconds latency, ossified dual-path code. Cleanroom's rollback is more expensive per event but aggregates lower over the decision horizon.

---

## 6. Recommendation — CLEANROOM

### 6.1. Verdict

**Adopt candidate B (cleanroom).** v35 runs on the new path only. Rollback = revert the rollout-sequence commit range if calibration-bar thresholds (see [`rollback-criteria.md`](rollback-criteria.md)) regress.

### 6.2. Justification

1. **The new architecture's value is structural, not incremental.** P1 (author-runnable pre-attest), P2 (leaf briefs), P3 (SymbolContract byte-identical interpolation), P4 (server state IS the plan), P5 (two-way manifest graph) are not individually shadowable: each changes the **shape** of agent–server interaction, not a quantity along an existing axis. Shadow-diff measures bytes; the research question measures behavior.

2. **Operational substrate is pristine and untouched** (§1.1). Rollback cannot threaten v8.90/v8.93.1/v8.95/v8.96/v8.97/v8.103/v8.104 invariants because they live in code paths the rewrite does not modify. This bounds blast radius.

3. **Parallel-run's dual-path maintenance is precisely the class of architectural problem the rewrite closes** (monolithic content with cross-audience mixing, multiple parallel workflow models — cf. P4 "server state IS the plan" vs v34 TodoWrite parallel plan). Institutionalizing a dual guidance path inside the codebase reintroduces the class.

4. **Step-4 coverage + step-5 calibration bars + step-6 rollback-criteria form a complete gate regimen** that substitutes for shadow-diff. Pre-v35 gate: every composed brief passed cold-read (step 4); every defect class v20–v34 has coverage (step 4 §3 audit + step 5 §3 principle-to-row map); every rewrite-to-runnable check has a paired shim (check-rewrite §17 + §18). Post-v35 gate: 97 numeric bars (calibration-bars-v35.md) measured automatically; 5 headline bars (C-1, C-2, C-9, M-3b, CS-1) drive rollback per [`rollback-criteria.md`](rollback-criteria.md).

5. **Cleanroom enables the v35 dry-run infrastructure to serve both roles** (pre-ship validation + post-ship measurement) against one path. Parallel-run would require dry-run instrumentation on two paths, doubling the test surface.

6. **Every `(carry)` row in the defect-class registry** (v22 coherence, v25 app-static, v27 shallow fix, v30 .DS_Store, **v34 manifest / cross-scaffold / convergence / self-referential**) depends on mechanisms that only exist in the new path. Parallel-run leaves these `(carry)` rows uncovered when the flag is off — which means every v35 candidate run under the old path regresses against step 5's calibration bars simply by virtue of being old-path. There is no "v35 under parallel-run with old-path fallback" that also closes v34's surfaced classes.

### 6.3. What cleanroom is not

- **Not "reckless big-bang."** Cleanroom under zcprecipator2 is a **sequence of 15 small commits** (see [`rollout-sequence.md`](rollout-sequence.md)), each individually revertable, each with explicit test coverage at unit / tool / integration / e2e layers per [CLAUDE.md](../../../CLAUDE.md). The commits are ordered so each "breaks alone" consequence is explicit and bounded.
- **Not "skip a proof."** Cleanroom is gated by (a) step 4's pre-merge cold-read per (role × tier), (b) build-time grep/lint guards on every atom per P2 + P6 + P8, (c) a `zcp dry-run recipe` harness added in commit C-14 of the rollout sequence that exercises the full atomic-composition path without a live Zerops call, (d) the calibration-bar measurement scripts in post-v35 audit.
- **Not "v35 is the only test."** Every rewrite-to-runnable check ships with unit + tool tests; every new check ships with unit + tool + integration tests. The atom tree ships with build-time lints. v35 is the **end-to-end convergence measurement** — the one thing tests cannot reproduce.

### 6.4. What's deferred

- **A v35.5 minimal-tier confirmation run** — [`data-flow-minimal.md §11`](../03-architecture/data-flow-minimal.md) identifies 4 escalation triggers that may require a commissioned minimal run. Under cleanroom, this trivially runs on the new path; under parallel-run, it would require deciding path per tier. Cleanroom simplifies.
- **Deletion of the 6 deferred-deletion check candidates** from [`check-rewrite.md §15`](../03-architecture/check-rewrite.md) — per RESUME decision #5 conservative threshold, these remain as `keep`/`rewrite-to-runnable` through v35. Post-v35 measurement may upgrade them to `delete` if they provably didn't fire. Cleanroom allows this in a subsequent patch without coexistence complexity.
- **Retention of v8.96 Theme A / v8.104 Fix E verbose failure-payload fields for human-readable debugging** — under cleanroom the `StepCheck` shape change drops these fields. An alternative "compact payload on wire, rich payload in debug log" refinement is deferred to post-v35; if retention is decided to be valuable, it lands as a field-restoration patch, not a rollback.

---

## 7. What remains on-decision

The migration candidate is decided. The commit-level execution lives in [`rollout-sequence.md`](rollout-sequence.md). The v35 go/no-go thresholds and rollback procedure live in [`rollback-criteria.md`](rollback-criteria.md).

Open post-step-6 items (handed to the implementation phase, not this research):

1. **Config shape for SymbolContract** — plan-field versus separate computed artifact. [`principles.md §12.1`](../03-architecture/principles.md) leaves this open; [`atomic-layout.md §4`](../03-architecture/atomic-layout.md) places it inside `plan.Research`. Implementation-phase decision.
2. **Expanded `writer_manifest_honesty` trigger — deploy-readmes complete vs close-code-review complete.** [`principles.md §12.2`](../03-architecture/principles.md) defers. Either trigger is cleanroom-compatible.
3. **TodoWrite structural refusal vs content-only discipline** — [`principles.md §12.3`](../03-architecture/principles.md) defers whether the Go layer actively rejects TodoWrite full-rewrite payloads at step-entry boundaries. Cleanroom lands with content-only discipline (principle atom); structural refusal (if desired) is a subsequent patch.
4. **Minimal-tier writer dispatch Path A (main-inline default) vs Path B (dispatched, per `data-flow-minimal.md §5a`)** — Path A is the current minimal default; Path B is `briefs/writer/*`-capable under the new architecture. Decision: ship Path A as default + `zerops_dev_server`/harness Path B as a configurable opt-in, revisit after v35.5 confirmation.

None of the four block the migration decision.
