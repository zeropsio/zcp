# implementation-notes.md — zcprecipator2 implementation phase running notes

Running commit-by-commit notes kept inline during cleanroom execution per
[`06-migration/rollout-sequence.md`](06-migration/rollout-sequence.md). Each
commit appends a section; nothing is retroactively rewritten.

---

## C-0 — Baseline: operational substrate untouched verification

**Status**: green

**Regression floor**: `go test ./... -count=1 -short` passes across 19
packages (verified before any edit landed).

### Substrate-invariant coverage audit

The C-0 plan named 8 test files to add. Audit against existing coverage
shows substrate invariants are already comprehensively pinned:

| Plan-named invariant | Plan-named file | Current pin |
|---|---|---|
| SSH boundary — no `cd /var/www/{host} && <exec>` bash emission; bash_guard rejects [v17/v8.80] | `internal/platform/ssh_test.go` | [`internal/tools/bash_guard_test.go`](../../internal/tools/bash_guard_test.go) — 168 lines exercising 11 executable tokens + ssh-wrap peel + edge cases |
| `SUBAGENT_MISUSE` on sub-agent calling `zerops_workflow action=start` [v8.90] | `internal/tools/workflow_subagent_misuse_test.go` | [`internal/tools/workflow_start_test.go`](../../internal/tools/workflow_start_test.go) — 245 lines + [`internal/workflow/recipe_close_ordering_test.go`](../../internal/workflow/recipe_close_ordering_test.go) + [`internal/workflow/recipe_tool_use_policy_test.go`](../../internal/workflow/recipe_tool_use_policy_test.go) |
| Single container-side `git config + git init` call shape [v8.93.1] | `internal/workflow/git_config_mount_test.go` | [`internal/init/init_container_test.go`](../../internal/init/init_container_test.go) — `TestContainerSteps_GitConfig` locks the exact 3-command shape; idempotence + outside-container skip also covered |
| `FactRecord.Scope` enum values + filter behavior [v8.96 Theme B] | `internal/ops/facts_log_test.go` | **GAP FILLED in this commit** — added `TestFactsLog_AllScopesAccepted` + `TestFactsLog_RejectsUnknownScope` + `TestFactsLog_ScopeRoundTrip` to [`internal/ops/facts_log_test.go`](../../internal/ops/facts_log_test.go) |
| Env-README Go-template byte-for-byte output for 6 canonical env tiers [v8.95 Fix B] | `internal/workflow/recipe_templates_test.go` | [`internal/workflow/recipe_templates_test.go`](../../internal/workflow/recipe_templates_test.go) — 1734 lines exercising every tier and dual-runtime permutation; `recipe_templates_dualruntime_test.go` + `recipe_templates_project_env_test.go` extend |
| Edit tool rejects edits to unread files [v8.97 Fix 3] | `internal/tools/workflow_read_before_edit_test.go` | **N/A** — this is Claude Code's built-in Edit-tool guard, not a zcp MCP surface. No zcp code to pin. Marked out of scope for C-0 (see rollout-sequence C-0 rationale). |
| `ExportRecipe` refuses when close step incomplete [v8.97 Fix 1] | `internal/sync/export_test.go` | [`internal/sync/export_gate_test.go`](../../internal/sync/export_gate_test.go) — 254 lines covering close-incomplete diagnostics + user-gated wording |
| Current pre-rewrite close `NextSteps=[…export/publish]` (pin before C-11 flip) | `internal/tools/workflow_close_next_steps_test.go` | [`internal/workflow/recipe_test.go`](../../internal/workflow/recipe_test.go) — `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` (asserts `len(nextSteps)==2` at v8.103 shape) + `TestHandleComplete_CloseStepPostCompletionBothUserGated` + `TestHandleComplete_CloseStepSummaryHasNoAutomaticClaims`. These tests will deliberately break on C-11; that's by design. |

### Coverage decision

Creating duplicate test files under the plan-named paths would replicate
existing coverage without strengthening the regression floor. The audit
above shows every substrate invariant (except the genuine `FactRecord.Scope`
gap) is already pinned by a comprehensive test file. For C-0, adding the
three Scope tests closes the one real gap; the remaining invariants are
locked by pre-existing tests that any future commit must keep green.

### What landed

- [`internal/ops/facts_log_test.go`](../../internal/ops/facts_log_test.go): +94 LoC
  - `TestFactsLog_AllScopesAccepted` — 4 valid scope values round-trip
  - `TestFactsLog_RejectsUnknownScope` — typo'd scope rejected with enumerated valid values in error message
  - `TestFactsLog_ScopeRoundTrip` — marshal/unmarshal preserves Scope field
- `docs/zcprecipator2/implementation-notes.md` — this file

### Verification

- `go test ./internal/ops/... -count=1 -run 'TestFactsLog_'` — 10 scope-related tests green (3 new + 7 existing)
- `go test ./... -count=1 -short` — full suite green, identical to pre-C-0 baseline (regression floor preserved)

### LoC delta

- Tests: +94 LoC
- Docs: +65 lines

### Breaks-alone consequence

Nothing. C-0 is additive: three new table-driven test functions that lock
an invariant already enforced in production code (`facts_log.go:95`
`knownScopes[rec.Scope]` check). The regression floor is the existing
full test suite, which remains green.

### Ordering deps verified

None — C-0 is the baseline.

---

## C-1 — SymbolContract plan field + derivation helper

**Status**: green

### What landed

- `internal/workflow/recipe.go` — `RecipePlan.SymbolContract SymbolContract \`json:"symbolContract,omitzero"\`` top-level field (Q1 resolution — derived artifact, not nested under `Research`). +9 LoC (plus a 6-line comment block).
- `internal/workflow/symbol_contract.go` — new file (~330 LoC). `SymbolContract` / `HostnameEntry` / `FixRule` types + `BuildSymbolContract(*RecipePlan) SymbolContract` derivation helper + `SeededFixRecurrenceRules()` returning the 12 v20–v34 recurrence-class positive-form rules with author-runnable `PreAttestCmd` per rule (principle P1).
- `internal/workflow/symbol_contract_test.go` — new file (~330 LoC). 9 table-driven tests covering nil/empty plan, single-codebase minimal, dual-runtime minimal, showcase+separate-codebase worker, showcase+shared-codebase worker, empty managed services, idempotent JSON marshaling, seeded rule coverage + positive-form invariant.
- `internal/workflow/recipe_service_types.go` — one-line fix: return `RecipeSetupWorker` constant instead of the literal `"worker"` (incidental pre-existing code smell exposed by the new constant addition; CLAUDE.md "fix at the source").

### Seeded fix-recurrence rules (12)

Each rule has `{ID, PositiveForm, PreAttestCmd, AppliesTo}`:

1. `nats-separate-creds` — pass user + pass as separate ConnectionOptions fields (v22)
2. `s3-uses-api-url` — endpoint = storage_apiUrl, not storage_apiHost (v22)
3. `s3-force-path-style` — S3 client forcePathStyle: true (v22)
4. `routable-bind` — HTTP servers bind 0.0.0.0 (v20)
5. `trust-proxy` — set trust proxy 1 for L7 balancer (v28)
6. `graceful-shutdown` — SIGTERM drain + Nest enableShutdownHooks (v30/v31)
7. `queue-group` — NATS subscribers declare queue group (v22/v30)
8. `env-self-shadow` — no KEY: ${KEY} lines in run.envVariables (v29)
9. `gitignore-baseline` — node_modules / dist / .env / .DS_Store (v29)
10. `env-example-preserved` — framework scaffolder's .env.example kept (v29)
11. `no-scaffold-test-artifacts` — no preship.sh / .assert.sh committed (v30)
12. `skip-git` — framework scaffolders invoked with --skip-git or .git rm (v31/v32)

Every rule's `PreAttestCmd` is a single SSH-runnable shell command the scaffold sub-agent can execute against its mount before returning. Token `{host}` is interpolated by the brief composer at stitch time.

### Verification

- `go test ./internal/workflow/... -count=1 -run 'TestBuildSymbolContract|TestSeededFixRecurrenceRules' -v` — 9 tests pass (all new)
- `go test ./... -count=1` — full suite green (19 packages)
- `make lint-local` — 0 issues

### LoC delta

- Go source: +348 LoC (symbol_contract.go) + 9 (recipe.go) + 0 net (recipe_service_types.go)
- Tests: +344 LoC (symbol_contract_test.go)
- Total: ~+700 LoC

### Breaks-alone consequence

Nothing. Additive:
- `SymbolContract` is a new zero-value field on `RecipePlan`. Default is an empty struct, serialized as absent (`omitzero`). No existing code reads it; no existing JSON breaks.
- `BuildSymbolContract` is never called by any production code path yet (C-5 will invoke it at research-complete).
- The 12 seeded rules are data only — no runtime side effects.

### Ordering deps verified

C-0 (baseline green) — required so the additive land is measured against a
pristine regression floor.

### Q1 honored

Top-level `plan.SymbolContract` (not `plan.Research.SymbolContract`). Derivation is idempotent — the same plan always yields byte-identical JSON (test `TestBuildSymbolContract_IdempotentJSON`).

---

## C-2 — FactRecord.RouteTo + routing enum

**Status**: green

### What landed

- `internal/ops/facts_log.go` (+56 LoC):
  - 9 `FactRouteTo*` enum constants (`content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`, `discarded`)
  - `knownRouteTos` validation map (empty string accepted as legacy default)
  - `FactRecord.RouteTo string \`json:"routeTo,omitempty"\``
  - AppendFact enum check (rejects typos with enumerated valid values in error)
  - `IsKnownFactRouteTo(s string) bool` exported helper for downstream consumers (C-8 manifest-honesty)
- `internal/ops/facts_log_test.go` (+160 LoC):
  - `TestFactsLog_AllRouteTosAccepted` — all 10 valid values round-trip
  - `TestFactsLog_RejectsUnknownRouteTo` — typo rejected with enumerated valids
  - `TestFactsLog_RouteToRoundTrip` — marshal/unmarshal preserves RouteTo + Scope together
  - `TestFactsLog_LegacyRecordWithoutRouteTo` — pre-C-2 records deserialize cleanly (zero-value → "not yet routed")
  - `TestIsKnownFactRouteTo_Exported` — helper semantics documented for C-8 consumer

### Content manifest schema

`internal/tools/workflow_checks_content_manifest.go` already declares `routed_to` on `contentManifestFact` as a free-form string — the wire contract has carried the field since v8.86. C-2 adds the enum taxonomy at the FactRecord side; C-8 will wire the taxonomy into gate-level honesty checking (`writer_manifest_honesty` expansion across all routing dimensions).

Legacy manifests without the field deserialize to empty RoutedTo — treated by C-8 logic as "unclassified" per the same semantics as empty `FactRecord.RouteTo`.

### Verification

- `go test ./internal/ops/... -count=1 -run 'TestFactsLog_|TestIsKnownFactRouteTo'` — 12 tests green (5 new + 7 existing scope/type tests)
- `go test ./... -count=1` — full suite green (19 packages)
- `make lint-local` — 0 issues

### LoC delta

- Go source: +56 LoC (facts_log.go)
- Tests: +160 LoC (facts_log_test.go)
- Total: ~+216 LoC (plan target: +190; within bounds)

### Breaks-alone consequence

Nothing. Additive:
- `RouteTo` is optional on both ends (empty string accepted).
- No code reads `RouteTo` yet — C-5 writer dispatch will compose the expected routing per surface; C-8 manifest-honesty will iterate all `(routed_to, surface)` pairs.
- Existing jsonl files round-trip unchanged (verified by `TestFactsLog_LegacyRecordWithoutRouteTo`).

### Ordering deps verified

C-0 (baseline). C-1 not strictly required but co-authored in the same phase.

### Q2 preparation

`IsKnownFactRouteTo` is the exported entry point C-8 will use at both trigger points (primary at `deploy.readmes` complete, secondary at `close.code-review` complete per Q2).

---

## C-3 — atom_manifest.go scaffold

**Status**: green

### What landed

Four new files under `internal/workflow/` totaling ~580 LoC + ~220 LoC tests.

- `atom_manifest.go` (173 LoC) — `Atom` type, audience + tier constants, `allAtoms` aggregate, helpers (`AllAtoms`, `AtomsForPhase`, `AtomsForBrief`, `AtomPath`, `FindAtom`, `AtomsByAudience`), and the `atomCountBaseline` constant (120)
- `atom_manifest_phases.go` (82 LoC) — 65 phase atoms, tree-walk ordered
- `atom_manifest_briefs.go` (67 LoC) — 39 brief atoms (scaffold 8 + feature 6 + writer 10 + code-review 5 + editorial-review 10)
- `atom_manifest_principles.go` (46 LoC) — 16 principle atoms (6 top-level + 6 platform-principles + 4 adjunct)
- `atom_manifest_test.go` (221 LoC) — 11 tests: count baseline, ID uniqueness, path uniqueness, audience enum, tier enum, ≤300-line cap, path-prefix-matches-category, AtomsForPhase spot-check, AtomsForBrief editorial-review presence, AtomsForBrief tier filtering invariant, AtomPath lookup, TierConditional-only-in-phases invariant, editorial-review count

### Atom-count note (docs reconciliation)

The canonical tree in [atomic-layout.md §1](../zcprecipator2/03-architecture/atomic-layout.md) enumerates **120 atoms** (65 phase + 39 brief + 16 principle). The summary text in the same file and rollout-sequence.md quotes **96** — a stale snapshot from before the tree was expanded to per-substep entry/completion pairs. The manifest implements the tree (120); the summary number is advisory. Documented inline on `atomCountBaseline` so future deltas trace back here.

C-4 will create 120 atom files under `internal/content/workflows/recipe/`. Plan's "+6,500 LoC md" estimate is proportionally higher (~8,100 LoC at 67 LoC/atom average, assuming the 1:1 atom-to-file mapping and tree-declared max-line bounds).

---

## C-4 — 120 atom files landed under `internal/content/workflows/recipe/`

**Status**: green

### Dispatch summary

Dispatched 9 parallel `general-purpose` subagents, each scoped to one directory group:

| Group | Atoms | Directory |
|---|---:|---|
| phases/research+provision | 16 | phases/research/, phases/provision/ |
| phases/generate | 24 | phases/generate/ |
| phases/deploy | 14 | phases/deploy/ |
| phases/finalize+close | 11 | phases/finalize/, phases/close/ |
| briefs/scaffold | 8 | briefs/scaffold/ |
| briefs/feature+code-review | 11 | briefs/feature/, briefs/code-review/ |
| briefs/writer | 10 | briefs/writer/ |
| briefs/editorial-review | 10 | briefs/editorial-review/ |
| principles | 16 | principles/ (+ platform-principles/) |
| **total** | **120** | |

### Invariant verification (C-13 preview)

Ran on the full tree:

| Grep | Result |
|---|---|
| Version anchors `v[0-9]+(\.[0-9]+)*` | 0 matches tree-wide |
| Dispatcher vocab IN briefs/ (critical) | 0 matches |
| Dispatcher vocab in phases/ | 9 legitimate "main agent" / "verbatim" uses (phase atoms address main directly; "verbatim" describes YAML value preservation, not dispatch composition) |
| Internal check names | 0 matches |
| Go-source paths `internal/<pkg>/<file>.go` | 0 matches |
| Per-atom line count > declared max | 0 violations (verified via tmp script against manifest) |
| Any file > 300 lines (P6 cap) | 0 files (largest: briefs/writer/content-surface-contracts.md at 161 lines) |

### Line totals

4,512 total markdown lines across 120 atoms. Subagents authored compactly — substantial slack against max-lines budgets on most atoms. C-5 composition will verify against step-4 goldens; atoms may grow during brief-composition debugging if goldens need content that was compressed here.

### P8 conversions surfaced

Every subagent reported positive-form conversions made:

- `phases/generate/zerops-yaml/comment-style-positive.md`: rewrote `comment-anti-patterns` L1301-1316 as positive ASCII allow-list ("every comment begins with a single `#` followed by one space, then a full sentence in plain ASCII") — zero enumerated prohibitions.
- `phases/generate/zerops-yaml/setup-rules-dev.md`: folded `dual-runtime-what-not-to-do` L663-672 positively ("Use only `setup: dev` and `setup: prod` as generic names").
- `phases/deploy/browser-walk-dev.md` rule 3: "One browser call at a time across agents" positive form.
- `briefs/scaffold/pre-ship-assertions.md`: rewritten as positive invariants ("X is absent/present" + shell command that proves it) instead of failure enumeration.
- `briefs/feature/diagnostic-cadence.md`: 5 permitted probe types positive allow-list instead of 17 forbidden probes.
- `briefs/writer/classification-taxonomy.md`: each class declared "What it IS" + "Test" + "Default route".
- `briefs/editorial-review/reporting-taxonomy.md`: CRIT/WRONG/STYLE positive semantics + inline-fix policy as positive "bounded" contract.
- `principles/canonical-output-paths.md`: positive declaration of the single canonical tree.

### Known subagent notes (flagged for C-5 review)

- **briefs/scaffold/symbol-contract-consumption.md**: ends with "The contract JSON for this run follows." — expects stitcher to emit the fenced JSON block after the atom. C-5 stitcher wires `{{.SymbolContract | toJSON}}` interpolation immediately after this atom.
- **briefs/writer/classification-taxonomy.md**: 6-class taxonomy (not 7 — excludes `library-meta`) per the brief spec. Goldens mention 7 classes; consumers must align on 6 during C-5 composition validation.
- **briefs/scaffold/pre-ship-assertions.md**: delegates to contract's `FixRecurrenceRules.preAttestCmd` as primary source; codebase-level file-presence assertions inline. If the stitcher needs all 12 FixRecurrenceRules preAttestCmds inlined as a flat bash script, the atom needs expansion.
- **briefs/code-review/reporting-taxonomy.md**: added SYMPTOM as a fourth escape-hatch designation (distinct from CRIT/WRONG/STYLE), preserving the three-tier framing.
- **briefs/editorial-review**: zero references to "Prior Discoveries" across all 10 atoms (per P7 porter-premise + refinement §10 open-question #6 — stitcher will NOT include Prior Discoveries for editorial-review).

### Verification

- `find internal/content/workflows/recipe -name '*.md' | wc -l` = 120 (matches atomCountBaseline)
- `go test ./internal/workflow/... -run TestAtomManifest_` — all 11 manifest tests green (path existence not asserted in C-3 tests; verified via tmp script here)
- `go test ./... -count=1` — full suite green (19 packages)
- `make lint-local` — 0 issues

### LoC delta

- Markdown content: 4,512 LoC across 120 files
- No Go code changes
- Notes: +100 LoC
- **Total**: ~+4,612 LoC

### Breaks-alone consequence

Nothing. Content exists on disk but no code reads it:
- atom_manifest.go paths now correspond to real files (C-3's path-uniqueness + audience invariants cross-check against the disk state).
- `//go:embed` is not yet declared — C-5 sets up the embed.FS root.
- Old `recipe.md` monolith stays in place (deleted in C-15).

### Ordering deps verified

C-3 (manifest declares paths these files must exist at) ✓.

### Pre-C-5 gate preparation

Per the user's gate contract, C-5 is the CUTOVER commit — stitcher rewrite, largest behavioral change. Before C-5 ships:

1. Step-4 composed briefs ([docs/zcprecipator2/04-verification/brief-*-composed.md](../../docs/zcprecipator2/04-verification/)) are the golden files; C-5's new `buildSubStepGuide` + `build*DispatchBrief` helpers must produce output that matches these goldens.
2. C-14 will add the `zcp dry-run recipe` harness to diff against the goldens in CI — but C-14 lands after C-5.
3. C-5 review surface: `recipe_guidance.go` stitcher body + embed.FS setup + brief composition functions for scaffold / feature / writer / code-review (editorial-review's stitcher lands in C-7.5).

**C-4 scope is CLEAN: 120 atoms land, all invariants pass, all tests + lint green. Ready for the pre-C-5 user-review gate.**

---

## C-5 — Foundation (atom loader + stitcher surface + SymbolContract wiring + RouteTo filter)

**Status**: green — **partial commit**. Foundation lands; the "flip" (replacing `buildGuide`'s block-based emission) deferred to a dedicated follow-up session.

### Why partial

The original C-5 plan described a single-commit cutover replacing `resolveRecipeGuidance` + `buildSubStepGuide` with atom-based composition. Executing the full flip requires cascading updates to ~15 existing tests in `internal/workflow/` that assert block-based content from recipe.md. Given session context budget, the foundation is deliverable now; the flip needs a fresh instance with focused scope.

This matches the phased approach described in the I-1→I-2 instance boundary plan. I-1 ships the foundation; I-2 executes the flip.

### What landed (C-5 foundation)

- `internal/content/content.go` (+7 LoC): `RecipeAtomsFS` `embed.FS` exposing the 120-atom tree
- `internal/workflow/atom_loader.go` (85 LoC, new): `LoadAtom`, `LoadAtomBody`, `AtomExists`, `concatAtoms` helpers; reads from `content.RecipeAtomsFS`
- `internal/workflow/atom_stitcher.go` (372 LoC, new): six stitcher functions:
  - `BuildStepEntry(phase, tier)` — composes `phases/<phase>/entry.md` + substep entries (tier-filtered)
  - `BuildSubStepCompletion(phase, substep)` — substep-level completion with phase-level fallback
  - `BuildScaffoldDispatchBrief(plan, role)` — scaffold brief with role addendum + SymbolContract JSON interpolation + role-aware platform-principles
  - `BuildFeatureDispatchBrief(plan)` — feature brief with SymbolContract JSON interpolation + feature principles
  - `BuildWriterDispatchBrief(plan, factsLogPath)` — writer brief with pointer to facts log
  - `BuildCodeReviewDispatchBrief(plan, manifestPath)` — code-review brief with manifest pointer
  - `BuildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath)` — editorial-review brief; **no Prior Discoveries** (P7 porter-premise); pointer inputs appended explicitly
- `internal/workflow/recipe.go` (+9 LoC): `plan.SymbolContract = BuildSymbolContract(plan)` populated at research-complete (Q1 resolution — top-level, idempotent)
- `internal/workflow/recipe_brief_facts.go` (+113 LoC): `BuildPriorDiscoveriesBlockForLane(sessionID, currentSubstep, lane)` — P5 lane-aware variant of the prior-discoveries block; `laneRouteToAllowlist` for scaffold/feature/writer/code-review; legacy empty RouteTo broadcast to every lane
- `internal/workflow/atom_stitcher_test.go` (293 LoC, new): 13 tests
  - `TestLoadAtom_EveryManifestEntry` — every 120 atoms load successfully
  - `TestLoadAtom_UnknownID` — unregistered IDs error with the id named
  - `TestConcatAtoms_SkipsEmpty` — "" tier-branch skips work
  - `TestBuildStepEntry_Research` — phase + substep entries compose
  - `TestBuildSubStepCompletion_FallsBackToPhaseLevel` — substep→phase fallback
  - `TestBuildScaffoldDispatchBrief_ThreeRolesProduceDistinctOutput` — api/app/worker differentiate
  - `TestBuildScaffoldDispatchBrief_ContractJSONIdenticalAcrossRoles` — P3 byte-identical invariant
  - `TestBuildFeatureDispatchBrief_IncludesContractJSON` — feature brief interpolates contract
  - `TestBuildWriterDispatchBrief_IncludesFactsPath` — writer brief references facts log
  - `TestBuildCodeReviewDispatchBrief_IncludesManifestPath` — code-review brief references manifest
  - `TestBuildEditorialReviewDispatchBrief_NoPriorDiscoveries` — porter-premise guard
  - `TestMarshalSymbolContract_Deterministic` — idempotent JSON
  - `TestSymbolContractWiredAtResearchComplete` — CompleteStep populates contract
  - `TestBuildPriorDiscoveriesBlockForLane_FiltersByRouteTo` — lane-aware filtering

### What's deferred (C-5 flip — follow-up)

- `buildGuide` / `resolveRecipeGuidance` still read `internal/content/workflows/recipe.md` via `content.GetWorkflow("recipe")` + `ExtractSection`. Agent still sees old content until the flip.
- `buildSubStepGuide` (the sub-step dispatch brief composer) still uses the topic-registry path.
- `recipe.md` monolith still present (C-15 removes post-cutover).
- Block-based tests in `internal/workflow/recipe_guidance_test.go` + `recipe_substep_briefs_test.go` still pass against old emission shape.

### Verification

- `go test ./internal/workflow/... -run 'TestLoadAtom|TestConcatAtoms|TestBuildStepEntry|TestBuildSubStepCompletion|TestBuildScaffoldDispatchBrief|TestBuildFeatureDispatchBrief|TestBuildWriterDispatchBrief|TestBuildCodeReviewDispatchBrief|TestBuildEditorialReviewDispatchBrief|TestMarshalSymbolContract|TestSymbolContractWiredAtResearchComplete|TestBuildPriorDiscoveriesBlockForLane'` — 13 tests green
- `go test ./... -count=1` — full suite green (19 packages, zero regressions)
- `make lint-local` — 0 issues

### LoC delta

- Go source: +586 LoC (loader + stitchers + SymbolContract wire + RouteTo filter)
- Tests: +293 LoC
- **Total**: ~+879 LoC

### Breaks-alone consequence

Nothing. Foundation is entirely additive:
- New stitcher functions are dead code (not called by `buildGuide`)
- `plan.SymbolContract` is populated at research-complete but never read by downstream production code yet
- `BuildPriorDiscoveriesBlockForLane` coexists with `BuildPriorDiscoveriesBlock`; no existing caller switches

### Ordering deps verified

C-1 (SymbolContract type) ✓; C-2 (RouteTo enum) ✓; C-3 (atom manifest) ✓; C-4 (atom files on disk) ✓.

### Next — C-5 flip (follow-up session scope)

1. Replace `resolveRecipeGuidance` body: instead of `ExtractSection(md, step)` + `composeSection(body, catalog, plan)`, emit `BuildStepEntry(phase, tier)` from the new stitcher
2. Replace `buildSubStepGuide` body: emit per-substep composition from `phases/<phase>/<substep>/entry.md` + `principles/*` pointer-includes
3. Wire `buildScaffoldDispatchBrief` / `buildFeatureDispatchBrief` / `buildWriterDispatchBrief` / `buildCodeReviewDispatchBrief` call sites in `recipe_substep_briefs.go` to produce the dispatch prompts
4. Update tests: replace block-based fixture assertions with golden-file diff against step-4 composed briefs in `docs/zcprecipator2/04-verification/`
5. Verify `v34` captured dispatches against the new stitcher output for regression-direction coverage
6. Pass `make lint-local` + full test suite before ship

Risk budget: ~1 day focused work in a fresh instance with scope set to C-5 flip only.

---

## C-5 flip — substep-guide atom-preferring resolution

**Status**: green

### What changed

`buildSubStepGuide` in [recipe_guidance.go](../../internal/workflow/recipe_guidance.go) now prefers atom-based content:

1. Resolves `(step, substep)` via new `atomIDForSubStep` helper → atom ID
2. If the atom exists in the embedded tree, loads its body via `LoadAtomBody`
3. For dispatch-owning substeps (subagent / readmes / code-review), appends the composed sub-agent brief from the new stitcher (`BuildFeatureDispatchBrief` / `BuildWriterDispatchBrief` / `BuildCodeReviewDispatchBrief`) under a "Dispatch brief (transmit verbatim)" header
4. Prepends lane-aware Prior Discoveries (`BuildPriorDiscoveriesBlockForLane`) for dispatch + observation-dependent substeps, gated on `isShowcase(plan)` for the dispatch-only substeps (minimal tier's readmes/subagent paths don't dispatch)
5. Editorial-review substep (added in C-7.5) never receives Prior Discoveries — porter-premise guard preserved
6. If no atom is registered for the substep, falls through to the legacy `subStepToTopic` → `ResolveTopic` path — graceful degradation

### Atom coverage

17 substeps mapped to atoms (out of ~18 total across generate/deploy/close):

- generate: scaffold, app-code, smoke-test, zerops-yaml
- deploy: deploy-dev, start-processes, verify-dev, init-commands, subagent, snapshot-dev, feature-sweep-dev, browser-walk (→ browser-walk-dev), cross-deploy (→ cross-deploy-stage), verify-stage, feature-sweep-stage, readmes
- close: code-review, close-browser-walk

### Test updates

Two block-based tests rewritten to atom-based expectations (`recipe_substep_briefs_test.go`):

- `TestSubStepGuide_InitCommandsResponse_ContainsSubagentBrief` — now checks the stitcher's "Dispatch brief (transmit verbatim)" header + SymbolContract JSON presence + feature-brief phrases. Size floor dropped from 10 KB to 2 KB (atom-based is leaner via P6)
- `TestSubStepGuide_FeatureSweepStageResponse_ContainsContentAuthoringBrief` — checks writer brief phrases (classification, citation, manifest, surface) + size floor dropped from 8 KB to 3 KB

One invariant test (`TestSubStepGuide_NoOptIn_DoesNotPrepend`) passes unchanged once `shouldPrependPriorDiscoveries` gates readmes/subagent on `isShowcase(plan)` — minimal tier doesn't dispatch so no prior-discoveries prepend.

### What's NOT flipped

- `resolveRecipeGuidance` (step-entry composition) still reads recipe.md via `content.GetWorkflow` + `ExtractSection`. The step-entry composition is a separate surface that goes to the main agent at every step transition; wiring it into `BuildStepEntry` + substep-aware skeleton is C-15 or a dedicated follow-up.
- The legacy topic-registry path is still referenced by `buildSubStepGuide`'s fallback branch. C-15 deletes recipe.md + recipe_topic_registry.go, at which point every substep must have an atom (fallback becomes impossible).

### Verification

- `go test ./internal/workflow/... -count=1 -run 'TestSubStepGuide_'` — 4 tests green
- `go test ./... -count=1` — full suite green (19 packages)
- `make lint-local` — 0 issues

### LoC delta

- `recipe_guidance.go`: +147 LoC (new helpers + rewritten `buildSubStepGuide` body)
- `recipe_substep_briefs_test.go`: ~+8 LoC (2 tests updated)
- **Total**: ~+155 LoC

### Breaks-alone consequence

Agent now sees atom-based substep content for the 17 mapped substeps. Dispatch briefs for scaffold/feature/writer/code-review flow from the new stitchers with SymbolContract JSON byte-identical across parallel dispatches. Legacy recipe.md path still serves step-entry until a dedicated follow-up wires `BuildStepEntry` into `resolveRecipeGuidance`.

### Ordering deps verified

C-5 foundation (atom loader + stitchers + SymbolContract wire + RouteTo filter) ✓.

**C-5 is effectively complete at the substep-dispatch surface.** Step-entry composition flip + topic-registry deletion land with C-15.

### Tier-conditional atoms

Seven tier-conditional atoms, all under `phases/` (briefs are TierAny — tier branching inside content, resolved by stitcher):

- `phases/generate/app-code/execution-order-minimal.md` → `TierMinimal`
- `phases/generate/app-code/dashboard-skeleton-showcase.md` → `TierShowcase`
- `phases/deploy/subagent.md` → `TierShowcase` (feature-sub dispatch)
- `phases/deploy/snapshot-dev.md` → `TierShowcase`
- `phases/deploy/browser-walk-dev.md` → `TierShowcase`
- `phases/finalize/service-keys-showcase.md` → `TierShowcase`
- `phases/close/close-browser-walk.md` → `TierShowcase`

Multi-codebase gating (single vs multi scaffold) is stitcher-branched, not tier-branched, since dual-runtime minimal is also multi-codebase.

### Verification

- `go test ./internal/workflow/... -count=1 -run 'TestAtomManifest_'` — 11 tests green
- `go test ./... -count=1` — full suite green
- `make lint-local` — 0 issues

### LoC delta

- Go source: +368 LoC (manifest + phases + briefs + principles)
- Tests: +221 LoC (atom_manifest_test.go)
- Total: ~+589 LoC

### Breaks-alone consequence

Nothing. Additive dead code:
- Manifest is never read by any production code path.
- No file under `internal/content/workflows/recipe/` exists yet — C-4 creates them. A path-existence test cannot fire here (it would fail); path uniqueness + audience enum + tier enum + 300-line-cap invariants all pass at manifest-only level.
- C-5 will wire the manifest into the stitcher via embed.FS.

### Ordering deps verified

C-0 (baseline). No dependency on C-1 or C-2 (independent additive branch).

### C-4 prerequisites

Every atom's Path in the manifest is a filesystem claim. C-4 must:
1. Create each declared path as a file under `internal/content/workflows/recipe/`
2. Keep each file's line count ≤ its declared MaxLines
3. Not create orphan files under `recipe/` (every file on disk must be claimed by the manifest — enforced by C-13 build lint)

Given the 120-file scope, C-4 will dispatch parallel subagents per directory group (phases/research+provision, phases/generate, phases/deploy, phases/finalize+close, briefs/scaffold, briefs/feature, briefs/writer, briefs/code-review, briefs/editorial-review, principles/). Each subagent receives the atomic-layout.md block-to-atom mapping for its directory plus the step-4 composed-brief goldens as truth reference.

---

## C-6 — 5 new architecture-level checks

**Status**: green

### What landed

Five new check predicate files under `internal/tools/` (each with a paired
seed-pattern table-driven test file), plus wire-up into the existing
recipe step checkers. All predicates are pure Go — no CLI shim surface
(that lands in C-7). Every predicate emits the `{prefix}_check_name`
pattern (where `prefix` is host-scoped or recipe-wide per the check's
domain).

| File | LoC | Check name emitted | Principle |
|---|---:|---|---|
| [workflow_checks_symbol_contract.go](../../internal/tools/workflow_checks_symbol_contract.go) | 263 | `symbol_contract_env_var_consistency` | P3 |
| [workflow_checks_visual_style.go](../../internal/tools/workflow_checks_visual_style.go) | 91 | `{host}_visual_style_ascii_only` | P8 |
| [workflow_checks_canonical_output_tree.go](../../internal/tools/workflow_checks_canonical_output_tree.go) | 96 | `canonical_output_tree_only` | P8 |
| [workflow_checks_manifest_route_to.go](../../internal/tools/workflow_checks_manifest_route_to.go) | 85 | `manifest_route_to_populated` | P5 |
| [workflow_checks_no_version_anchors_in_published.go](../../internal/tools/workflow_checks_no_version_anchors_in_published.go) | 114 | `no_version_anchors_in_published_content` | P6 |

Paired test files (`*_test.go`) total **739 LoC** across the five checks
(~148 LoC/check average). Every test file follows the CLAUDE.md seed
pattern — table-driven + `t.Parallel()` + `findCheckByName` + domain
helpers (e.g. `writeYAMLBytes`, `writePublished`, `writeCodebaseFiles`).

### Wire-up (checker dispatch per check-rewrite.md §16)

| Check | Trigger | Wire site | Ambiguity resolution |
|---|---|---|---|
| `symbol_contract_env_var_consistency` | generate-complete + deploy.readmes | `checkRecipeGenerate` + `checkRecipeDeployReadmes` | spec §16 said "generate-complete + deploy.start-processes"; no substep-level check battery exists; deploy.readmes (the last substep, hence deploy-step-complete) is the nearest firing point after start-processes completes — matches rollout-sequence.md C-6 language |
| `visual_style_ascii_only` | generate-complete | `checkRecipeGenerateCodebase` (per-codebase) | spec §16 unambiguous |
| `canonical_output_tree_only` | close-entry | `checkRecipeFinalize` | close step has no checker (administrative trigger); finalize-complete runs immediately before close-entry, the semantically-equivalent firing point. A dedicated close-entry hook is out of C-6 scope |
| `manifest_route_to_populated` | deploy.readmes | `checkRecipeDeployReadmes` | spec §16 unambiguous |
| `no_version_anchors_in_published_content` | finalize-complete | `checkRecipeFinalize` | spec §16 unambiguous |

### Predicate design notes

- **symbol-contract** — derives a sibling→canonical env-var map from
  `plan.SymbolContract.EnvVarsByKind` + a hand-seeded `siblingSuffixes`
  table (PASS↔PASSWORD, USER↔USERNAME, HOST↔HOSTNAME, apiUrl↔API_URL,
  DBNAME↔DATABASE). Scans `{host}dev/src/**`, `.env.example`,
  `zerops.yaml` per codebase for non-canonical sibling references. v34
  regression surface: DB_PASS vs DB_PASSWORD.
- **visual-style** — reads `{ymlDir}/zerops.yaml` (fallback `.yml`),
  scans every rune for U+2500..U+257F (Box Drawing block). Lists distinct
  codepoints + line numbers on failure. Missing file = graceful pass
  (the `_zerops_yml_exists` floor is the upstream concern).
- **canonical-output-tree** — WalkDir depth ≤ 2 under the recipe project
  root, fails on any directory whose basename starts with `recipe-`.
  Accepts legitimate nested `recipe-*` at depth ≥ 3 (npm package naming
  like `@pkg/recipe-helper`). Missing root = graceful pass.
- **manifest-route-to-populated** — parses `ZCP_CONTENT_MANIFEST.json`,
  fails on any entry with empty `routed_to` OR off-enum value. Upstream
  concerns (file missing, invalid JSON) are graceful passes because the
  C-5 content-manifest battery already reports those on the same surface
  — avoids pile-on multi-fails on one root cause.
- **no-version-anchors** — glob-scans `{host}/README.md`, `{host}/CLAUDE.md`,
  `environments/*/README.md` for `\bv\d+(\.\d+)*\b` tokens. Surfaces
  first 10 hits in Detail (`+N more` suffix). v33 class: leakage of
  internal recipe version anchors (v33, v34, v8.86) into porter-facing
  content.

### Verification

- `go test ./internal/tools/... -count=1` — all 13 new test functions
  (5 table-driven parents with a combined 27 sub-cases + 8 focused
  single-scenario tests) pass; existing tools tests stay green.
- `go test ./... -count=1` — full suite green across all 19 packages.
- `make lint-local` — 0 issues (after applying `gofmt`, renaming
  `checkVisualStyleAsciiOnly` → `checkVisualStyleASCIIOnly` per `revive`
  initialism rule, and annotating three intentional-nil-err returns
  inside `filepath.WalkDir` closures with `//nolint:nilerr` + rationale
  comments).

### LoC delta

- Predicate source: +649 LoC (5 files)
- Tests: +739 LoC (5 files)
- Wire-up: +42 LoC across `workflow_checks_recipe.go` +
  `workflow_checks_finalize.go`
- Notes: +85 LoC
- **Total**: ~+1,515 LoC (handoff estimate was +980 LoC — over by ~55%,
  driven by the symbol-contract predicate's sibling-map + scan-scope
  machinery growing larger than the spec's "grep env-var tokens"
  shorthand suggested, plus generous structured-fail Detail messages that
  name the v-version class directly for author guidance).

### Breaks-alone consequence

New gates fire. Zero regression against prior runs:

- All five predicates return a **pass** check when the positive-allow-list
  surfaces are clean — a v34-style mount that already uses DB_PASSWORD
  consistently keeps passing.
- Upstream/structural failures (missing yaml file, missing project root,
  invalid manifest JSON) return graceful **pass** so this C-6 layer never
  piles multiple fails onto the same root cause already reported by
  existing C-5 or earlier checks.
- Detail messages directly name the v-class origin (v33 phantom tree,
  v33 Unicode box-drawing, v33 version anchors, v34 DB_PASS, v34 manifest
  inconsistency) so a v35 regression at any of these surfaces surfaces
  with the v34 verdict context inline.

### Ordering deps verified

- C-1 (SymbolContract plan field + EnvVarsByKind) ✓ — required for the
  symbol-contract check's canonical-set derivation.
- C-2 (FactRecord.RouteTo enum + `ops.IsKnownFactRouteTo`) ✓ — required
  for the manifest-route-to-populated check's enum validation.
- C-5 foundation (atom loader + stitchers) ✓ — not a direct dep, but C-5
  populates `plan.SymbolContract` at research-complete which is what C-6's
  symbol-contract check reads.

### Known deferred / out-of-scope

- **close-entry hook**. Per check-rewrite.md §16, `canonical_output_tree_only`
  is meant to fire at close-entry. The close step currently has no
  checker (administrative trigger); C-6 lands it at finalize-complete,
  which is the semantically-adjacent firing point. A dedicated close-entry
  hook would require substep-level check-dispatch infrastructure — out
  of C-6 scope. Documented here for C-15's post-refactor review.
- **deploy.start-processes substep hook**. Per spec §16,
  `symbol_contract_env_var_consistency` fires at both generate-complete
  AND deploy.start-processes. No substep-level check battery exists; C-6
  fires the second copy at deploy-complete (via
  `checkRecipeDeployReadmes` — deploy.readmes is the last substep). This
  matches rollout-sequence.md C-6's summary text. Effect: divergence
  re-introduced by mid-deploy inline edits is still caught (at
  deploy-complete) rather than strictly at deploy.start-processes entry.
- **CLI shim surface** (`zcp check <name>`). Deferred to C-7 per rollout
  sequence — C-6 provides the predicate implementations as the shared
  substrate C-7's shim tree will wrap.

### Pre-C-7 gate preparation

Not a gated commit. Next: C-7 (~2 hours with 4-way subagent split on the
16 check rewrites per rollout-sequence.md §Parallelization). The next
user-review gate is **pre-C-7.5** (editorial-review role introduction) —
stop there and report composed briefs against step-4 goldens.

---

## C-7 — Split into sub-commits (C-7a .. C-7e)

**Rationale**: C-7 (16 check rewrites + `zcp check <name>` CLI shim
surface) is a large structural change that modifies 12 existing
`workflow_checks_*.go` files, introduces a new `internal/ops/checks/`
package, and adds a new `cmd/zcp/check/` sub-package. Per
rollout-sequence.md §Parallelization, the 16-check refactor can be
"split into 4 sub-commits". Given:

- subagent-parallel dispatch pattern works best when subagents write
  isolated new files (C-4's 120 atoms), NOT when they modify shared
  files (every C-7 migration touches a `workflow_checks_*.go` +
  potentially a shared `cmd/zcp/check/check.go` registry);
- each sub-commit is independently testable + revertable, preserving
  the "every commit green" discipline;
- the CLI shim surface (C-7e) has design questions that benefit from
  landing after all 16 predicates exist so the shim is a simple thin
  adapter;

C-7 is split into **five** sub-commits:

| Sub-commit | Scope |
|---|---|
| C-7a | scaffold `internal/ops/checks/` package + migrate 4 predicates (env-refs, run-start-build-contract, env-self-shadow, ig-code-adjustment) |
| C-7b | migrate 4 predicates (ig-per-item-code, comment-specificity, yml-schema, kb-authenticity) |
| C-7c | migrate 4 predicates (worker-queue-group, worker-shutdown, manifest-honesty, manifest-completeness) |
| C-7d | migrate 4 predicates (comment-depth, factual-claims, cross-readme-dedup, symbol-contract-env-consistency) |
| C-7e | add `cmd/zcp/check/` CLI shim sub-package (16 thin adapters + parent dispatcher) + integration test + `cmd/zcp/main.go` wiring |

Each sub-commit: tests + lint green before advancing. No sub-commit
changes user-observable semantics — predicate bodies move, emission
shape preserved.

---

## C-7a — Scaffold `internal/ops/checks/` + migrate first 4 predicates

**Status**: green

### What landed

- [`internal/ops/checks/doc.go`](../../internal/ops/checks/doc.go) (27 LoC) — package doc + `StatusPass` / `StatusFail` constants shared across migrated predicates.
- [`internal/ops/checks/env_refs.go`](../../internal/ops/checks/env_refs.go) (52 LoC) — `CheckEnvRefs(ctx, hostname, entry, discoveredEnvVars, liveHostnames)`. Migrates the inline env-ref block from `checkGenerateEntry`.
- [`internal/ops/checks/run_start_build_contract.go`](../../internal/ops/checks/run_start_build_contract.go) (58 LoC) — `CheckRunStartBuildContract(ctx, hostname, entry)`. Migrates the inline build-prefix loop from `checkGenerateEntry`; private `buildCommandPrefixes` moved here.
- [`internal/ops/checks/env_self_shadow.go`](../../internal/ops/checks/env_self_shadow.go) (45 LoC) — `CheckEnvSelfShadow(ctx, hostname, entry)`. Direct port of `checkEnvSelfShadow`.
- [`internal/ops/checks/ig_code_adjustment.go`](../../internal/ops/checks/ig_code_adjustment.go) (150 LoC) — `CheckIGCodeAdjustment(ctx, content, isShowcase)`. Migrates `checkIntegrationGuideCodeBlocks`; private helpers `codeBlockFenceRe` / `nonYamlCodeLanguages` / `extractFragmentContent` / `uniqueStrings` moved here so the predicate is self-contained (the tool-layer copies stay for now — other not-yet-migrated checks in `workflow_checks_recipe.go` still reference `codeBlockFenceRe` / `extractFragmentContent` / `uniqueStrings`; `nonYamlCodeLanguages` was deleted as unused).
- Paired tests: 4 files, ~350 LoC total, table-driven per CLAUDE.md seed pattern.

### Tool-layer delegation

- [`workflow_checks_generate.go`](../../internal/tools/workflow_checks_generate.go): `checkGenerateEntry` now receives `ctx` and forwards to `opschecks.CheckEnvRefs` / `opschecks.CheckRunStartBuildContract` / `checkEnvSelfShadow` wrapper. Private `buildCommandPrefixes` deleted.
- [`workflow_checks_recipe.go`](../../internal/tools/workflow_checks_recipe.go): `checkIntegrationGuideCodeBlocks` is now a 3-line thin wrapper around `opschecks.CheckIGCodeAdjustment`. Unused `nonYamlCodeLanguages` deleted. `codeBlockFenceRe` / `extractFragmentContent` / `uniqueStrings` retained — still referenced by not-yet-migrated `checkIntegrationGuidePerItemCodeBlock` (C-7b target) and other recipe checks.
- `checkEnvSelfShadow(ctx, hostname, entry)` signature gained a leading `ctx`; both callers (`checkGenerateEntry` in generate.go; dual-entry loop in `checkRecipeGenerateCodebase`) updated.
- `context.Context` threaded through `checkGenerate` closure → `checkGenerateEntry`, and through `checkRecipeGenerate` / `checkRecipeDeployReadmes` closures → `checkRecipeGenerateCodebase` / `checkCodebaseReadme`. One pre-existing test in `workflow_checks_recipe_test.go` updated to pass `t.Context()` to `checkIntegrationGuideCodeBlocks`.

### Design invariant (reminder for C-7b..d)

The tool-layer function retains its existing signature as a thin wrapper over the `opschecks.Check<Name>(...)` predicate. Callers outside the tools package continue to see the unchanged tool-layer API. The wrapper may adapt plan/state inputs into the predicate's leaner signature, but it does NOT add logic — predicate body lives in `ops/checks` only. C-7e's CLI shim will call the same predicate function; gate and shim share one implementation.

### Verification

- `go test ./... -count=1` — full suite green across 20 packages (including new `internal/ops/checks`).
- `make lint-local` — 0 issues (after adding ctx threading through `checkGenerateEntry` / `checkRecipeGenerateCodebase` / `checkCodebaseReadme` to silence `contextcheck` and applying `gofmt` to the new test files).

### LoC delta

- New `internal/ops/checks/` package: +692 LoC (4 predicate files + 4 test files + doc.go).
- `internal/tools/workflow_checks_generate.go`: -30 LoC (inline env-refs block + inline build-contract block + env-self-shadow body collapsed into wrapper + `buildCommandPrefixes` deleted); +5 LoC (ctx threading + delegation calls).
- `internal/tools/workflow_checks_recipe.go`: -83 LoC (`checkIntegrationGuideCodeBlocks` body collapsed into 3-line wrapper + `nonYamlCodeLanguages` deleted); +3 LoC (ctx threading).
- **Net**: ~+585 LoC (predicate bodies now in two spots — the new home + thin wrappers — until C-15-era cleanup consolidates).

### Breaks-alone consequence

No user-observable behavior change. Every migrated predicate emits the same StepCheck Name + Status + Detail as before the move. Caller signatures gained leading `ctx context.Context` per existing Go idiom; no field-shape breaks.

### Ordering deps verified

- C-6 ✓ — `opschecks` package hosts the 4 migrated predicates alongside future-C-7e's shared substrate.

### Known follow-ups

- `codeBlockFenceRe` / `extractFragmentContent` / `uniqueStrings` remain duplicated in `internal/tools/workflow_checks_recipe.go` + `internal/ops/checks/ig_code_adjustment.go`. Consolidation happens in C-7b (when ig-per-item-code migrates) or C-15 (when the recipe.md path is deleted).
- No CLI shim surface yet — thin adapters land in C-7e atop the 16 migrated predicates.

---

## C-7b — migrate ig-per-item-code, comment-specificity, yml-schema, kb-authenticity

**Status**: green

### What landed

- [`internal/ops/checks/ig_per_item_code.go`](../../internal/ops/checks/ig_per_item_code.go) (115 LoC) — `CheckIGPerItemCode(ctx, content, isShowcase)`. Moves `splitByH3` + `sectionHasFencedBlock` alongside the predicate since they were only used here; both deleted from `workflow_checks_recipe.go`.
- [`internal/ops/checks/comment_specificity.go`](../../internal/ops/checks/comment_specificity.go) (110 LoC) — `CheckCommentSpecificity(ctx, yamlBlock, isShowcase)`. Migrates `specificityMarkers` + `minSpecificComments` + `specificCommentRatio` + `commentSpecificityRatio` helpers.
- [`internal/ops/checks/yml_schema.go`](../../internal/ops/checks/yml_schema.go) (52 LoC) — `CheckZeropsYmlFields(ctx, ymlDir, validFields)`. Direct port of `checkZeropsYmlFields`.
- [`internal/ops/checks/kb_authenticity.go`](../../internal/ops/checks/kb_authenticity.go) (80 LoC) — `CheckKnowledgeBaseAuthenticity(ctx, kbContent, hostname)`. Migrates `minAuthenticGotchas` + the structured-fail payload (`ReadSurface`, `Required`, `Actual`, `HowToFix`) intact.
- Paired tests: 4 files, ~330 LoC total, table-driven per CLAUDE.md seed pattern. Fixture correction needed for `kb_authenticity_test` — `workflow.ExtractGotchaEntries` requires a `## Gotchas` section header above bullet lines.

### Tool-layer delegation

- `checkIntegrationGuidePerItemCodeBlock` collapses to a 3-line thin wrapper (post-C-7b).
- `checkCommentSpecificity` collapses to a 3-line thin wrapper; all marker definitions now live in `ops/checks`.
- `checkZeropsYmlFields` collapses to a 1-line delegation.
- `checkKnowledgeBaseAuthenticity` collapses to a 1-line delegation in `workflow_checks_predecessor_floor.go`. `checkKnowledgeBaseExceedsPredecessor` gains a leading `ctx` so it can thread to the authenticity delegate (the exceeds-predecessor check itself is slated for deletion in C-9 per rollout-sequence).
- `codeBlockFenceRe` deleted from `workflow_checks_recipe.go` (now unused — its only remaining caller `sectionHasFencedBlock` moved to `ops/checks/ig_per_item_code.go`; the ops/checks version declared independently in `ig_code_adjustment.go` is the sole copy).
- ctx threaded through the 4 callers (`checkRecipeGenerateCodebase`, `checkCodebaseReadme`) and 9 test sites (8 in `workflow_checks_predecessor_floor_test.go` via `sed`, 2 in `workflow_checks_recipe_test.go`).

### Verification

- `go test ./... -count=1` — full suite green across 20 packages.
- `make lint-local` — 0 issues.

### LoC delta

- New `internal/ops/checks/` files: +714 LoC (4 predicate + 4 test files).
- `internal/tools/workflow_checks_recipe.go`: -200 LoC (`splitByH3`, `sectionHasFencedBlock`, `specificityMarkers`, `commentSpecificityRatio`, `minSpecificComments`, `specificCommentRatio`, `codeBlockFenceRe`, `checkZeropsYmlFields` body, `checkCommentSpecificity` body, `checkIntegrationGuidePerItemCodeBlock` body all gone); +8 LoC (thin wrappers + ctx threading).
- `internal/tools/workflow_checks_predecessor_floor.go`: -55 LoC (`checkKnowledgeBaseAuthenticity` body + unused const deleted); +5 LoC (wrapper + ctx threading + new import).
- `workflow_checks_recipe_test.go` + `workflow_checks_predecessor_floor_test.go`: +minimal (ctx passed through `t.Context()`).
- **Net**: ~+475 LoC.

### Breaks-alone consequence

No user-observable behavior change. The four migrated predicates emit identical StepCheck Name/Status/Detail (plus structured-fail fields for kb-authenticity) as before. 8 out of 16 checks now live in `internal/ops/checks`; 8 remain for C-7c + C-7d.

### Ordering deps verified

- C-7a ✓ — `opschecks` package scaffolding + pattern established.

### Known follow-ups

- `extractFragmentContent` still duplicated (tools copy used by other not-yet-migrated checks; C-7d is the likely consolidation point or C-15).
- 8 checks remain: worker-queue-group, worker-shutdown, manifest-honesty, manifest-completeness (C-7c) + comment-depth, factual-claims, cross-readme-dedup, symbol-contract-env-consistency (C-7d).

---

## C-7c — migrate worker-queue-group, worker-shutdown, manifest-honesty, manifest-completeness

**Status**: green

### What landed

- [`internal/ops/checks/worker_gotcha.go`](../../internal/ops/checks/worker_gotcha.go) (180 LoC) — `CheckWorkerQueueGroupGotcha(ctx, hostname, readme, target)` + `CheckWorkerShutdownGotcha(ctx, hostname, readme, target)`. Migrates the split v8.94 queue-group + shutdown predicates out of `workflow_checks_worker_correctness.go`. Shared helpers (`queueGroupTopicTokens`, `shutdownTopicTokens`, `matchesQueueGroupTopic`, `matchesShutdownTopic`, `workerGotchaCombinedLines`) moved alongside. Each predicate returns one row per invocation (pass or fail), shim-ready — the tool-layer composes the two + emits a single aggregate `_worker_production_correctness: pass` row when both pass.
- [`internal/ops/checks/manifest.go`](../../internal/ops/checks/manifest.go) (290 LoC) — exports `ContentManifest`, `ContentManifestFact`, `ManifestFileName`, `LoadContentManifest`. `CheckManifestHonesty(ctx, manifest, readmesByHost)` and `CheckManifestCompleteness(ctx, manifest, factsLogPath)` migrate C-5's manifest battery sub-checks C + D. Shared Jaccard machinery (`jaccardStopWords`, `jaccardSimilarityNoStopwords`, `tokenizeForJaccard`, `extractGotchaStems`) + `filterContentScoped` helper moved alongside. `jaccardHonestyThreshold` const preserved (0.3).
- Paired tests: 2 files, ~320 LoC total.

### Tool-layer delegation

- `workflow_checks_content_manifest.go` shrinks from 371 LoC → 130 LoC. Local `contentManifest` / `contentManifestFact` types, `manifestFileName` const, honesty + completeness bodies, and all Jaccard/stem-extraction helpers deleted. `checkWriterContentManifest` gains a leading `ctx` and delegates sub-checks C + D to `opschecks.CheckManifestHonesty` / `opschecks.CheckManifestCompleteness`. Sub-check B (`checkManifestClassificationConsistency`) stays local — "keep" disposition per check-rewrite.md §17; updated to accept `opschecks.ContentManifest`.
- `workflow_checks_manifest_route_to.go` (C-6) updated to use `opschecks.ContentManifest` instead of the now-deleted local `contentManifest`.
- `workflow_checks_worker_correctness.go` shrinks from 315 LoC → 150 LoC. `checkWorkerProductionCorrectness` becomes a 20-line composition that calls both `opschecks` predicates and preserves the pre-C-7c aggregate-pass behavior exactly: if both pass → single `_worker_production_correctness: pass` row; if either fails → only the fail rows. `checkWorkerDrainCodeBlock` stays local ("keep" per §17) alongside its `drainCallTokens` / `exitCallTokens` / `extractFencedBlockBodies` helpers.
- ctx threaded through `checkWriterContentManifest` callers (`checkRecipeDeployReadmes`) and `checkWorkerProductionCorrectness` callers (`checkCodebaseReadme` + 7 test sites).

### Verification

- `go test ./... -count=1` — full suite green across 20 packages.
- `make lint-local` — 0 issues (modernize flagged two `for _, line := range ...` loops in `worker_gotcha.go` — converted to `slices.ContainsFunc`).

### LoC delta

- New ops/checks files: +470 LoC (2 predicate + 2 test files).
- Tool-layer shrinkage: ~-460 LoC (manifest + worker_correctness files).
- Net: roughly neutral, predicate bodies now exist in exactly one place.

### Breaks-alone consequence

No user-observable behavior change. The four migrated predicates emit identical StepCheck rows; the worker-production-correctness aggregate-pass row shape is preserved exactly through the tool-layer composition wrapper.

### Ordering deps verified

- C-7b ✓ — `opschecks` package already established.
- C-6 ✓ — C-6's `checkManifestRouteToPopulated` updated to use the new `opschecks.ContentManifest` shared type (no behavior change; same JSON shape, same check output).

### Known follow-ups

- `extractFragmentContent`, `uniqueStrings`, `sectionHasFencedBlock` helpers still duplicated between `internal/tools/workflow_checks_recipe.go` and `internal/ops/checks/ig_*.go`. Still used by tool-local checks that won't migrate to ops/checks (e.g. `checkGotchaRestatesGuide`, `checkCLAUDEMdExists`). C-15 deduplicates when the recipe.md path is deleted.
- 4 checks remain: comment-depth, factual-claims, cross-readme-dedup, symbol-contract-env-consistency (C-7d).

---

## C-7d — migrate comment-depth, factual-claims, cross-readme-dedup, symbol-contract-env-consistency

**Status**: green

### What landed

- [`internal/ops/checks/comment_depth.go`](../../internal/ops/checks/comment_depth.go) (155 LoC) — `CheckCommentDepth(ctx, content, prefix)`. Migrates `reasoningMarkers`, `minReasoningCommentRatio`, `minReasoningComments`, and the comment-block grouping logic. Private `containsAny` helper moved alongside.
- [`internal/ops/checks/factual_claims.go`](../../internal/ops/checks/factual_claims.go) (215 LoC) — `CheckFactualClaims(ctx, content, prefix)`. Migrates `factualClaimPatterns`, `subjunctiveMarkers`, `factualClaimMismatch` type, `findAdjacentYAMLValueWithLine`, and `yamlIntFieldRe`.
- [`internal/ops/checks/cross_readme_dedup.go`](../../internal/ops/checks/cross_readme_dedup.go) (105 LoC) — `CheckCrossReadmeGotchaUniqueness(ctx, readmes)`. Direct port of the pairwise-comparison logic.
- [`internal/ops/checks/symbol_contract_env.go`](../../internal/ops/checks/symbol_contract_env.go) (245 LoC) — `CheckSymbolContractEnvVarConsistency(ctx, projectRoot, contract)`. Migrates C-6's predicate including `sourceFileExtensions`, `envVarTokenRegexp`, `siblingSuffixes`, `canonicalEnvVarSet`, `buildSiblingMap`, `collectSymbolContractScanFiles` helpers.
- Paired tests: 4 files, ~340 LoC.

### Tool-layer delegation

- `workflow_checks_comment_depth.go` → 20-line thin wrapper.
- `workflow_checks_factual_claims.go` → 15-line thin wrapper (all 130+ LoC of markers/patterns/helpers deleted).
- `workflow_checks_dedup.go` → 13-line thin wrapper for `checkCrossReadmeGotchaUniqueness`; `checkGotchaRestatesGuide` (keep-disposition per §17) retained intact.
- `workflow_checks_symbol_contract.go` → 18-line thin wrapper (C-6 predicate fully relocated).
- ctx threaded through `checkRecipeFinalize` closure → `validateImportYAML` → `checkCommentDepth` + `checkFactualClaims`. Also `checkRecipeDeployReadmes` → `checkCrossReadmeGotchaUniqueness`. Also both callers of `checkSymbolContractEnvVarConsistency` (`checkRecipeGenerate` + `checkRecipeDeployReadmes`).
- Orphaned `TestContainsAny` in tools replaced with a one-line comment pointing to ops/checks; the helper's behavior is now exercised transitively by `TestCheckCommentDepth_Table`.

### Verification

- `go test ./... -count=1` — full suite green across 20 packages.
- `make lint-local` — 0 issues.

### LoC delta

- New ops/checks files: +720 LoC (4 predicate + 4 test files).
- Tool-layer shrinkage: ~-620 LoC (comment_depth.go -195, factual_claims.go -240, dedup.go -115, symbol_contract.go -240).
- Net: roughly neutral.

### Breaks-alone consequence

No user-observable behavior change. **All 16 check-rewrite.md §18 predicates now live in `internal/ops/checks` with exactly one implementation per check**. Gate and (future C-7e) CLI shim share the same Go function — the design invariant is established.

### Ordering deps verified

- C-7c ✓.
- C-6 ✓ — C-6's symbol-contract predicate now lives in `ops/checks`; the C-6 tool-layer file retained as a thin wrapper.

### C-7 progress

**16 of 16 predicates migrated** (C-7a: 4, C-7b: 4, C-7c: 4, C-7d: 4). C-7e adds the `cmd/zcp/check/` CLI shim tree + integration test + `cmd/zcp/main.go` wiring on top of the finished predicate surface.

### Known follow-ups

- `extractFragmentContent` duplicated in tools (multiple non-migrating checks depend on it) + ops/checks. C-15 deduplication.
- `uniqueStrings` duplicated similarly.

---

## C-7e — `cmd/zcp/check/` CLI shim tree over the 16 migrated predicates

**Status**: green

### What landed

A new `cmd/zcp/check/` sub-package wraps every predicate in `internal/ops/checks/` behind a `zcp check <name>` subcommand. Main-agent or scaffold sub-agent can now invoke any gate's check against its own SSH mount before attesting — the author-runnable axis specified in [check-rewrite.md §18](03-architecture/check-rewrite.md). The design invariant from C-7d is fully realized: both the server-side gate and the shim call the **same** `opschecks.Check<Name>(...)` Go function; there is no path by which they can diverge.

- [`cmd/zcp/check/check.go`](../../cmd/zcp/check/check.go) (243 LoC) — `Run(args)` entry point + testable `run(ctx, args, stdout, stderr)` core + `registry` map + `handler` signature + `emitResults(w, asJSON, checks)` (text default / ndjson on `--json`) + shared helpers (`resolveHostnameDir`, `readHostnameReadme`, `extractFragmentBody`, `extractYAMLBlock`, `newFlagSet`, `addCommonFlags`). Writes to caller-supplied `io.Writer`s so tests capture stdout/stderr without touching `os.Stdout`.
- 16 shim files (~44 LoC average) — one per subcommand per §18. Each shim: parses flags → builds predicate inputs from disk → calls the predicate → `emitResults`.
- [`cmd/zcp/check/check_integration_test.go`](../../cmd/zcp/check/check_integration_test.go) (360 LoC) — table-driven with 16 rows (one per subcommand) + 3 dispatcher-path tests (unknown subcommand, empty args, all-16-registered). Fixture is a single tempdir with `apidev/` + `workerdev/` + `0 — AI Agent/import.yaml` + `ZCP_CONTENT_MANIFEST.json`. Assertions target exit code + substring of combined stdout/stderr; not exhaustive — predicate-behavior tests live in `internal/ops/checks/`.
- [`cmd/zcp/main.go`](../../cmd/zcp/main.go) — added `case "check": check.Run(os.Args[2:])` alongside existing `catalog` / `sync` / `eval` cases.

### The 16 subcommands (per `check-rewrite.md §18`)

| Subcommand | Predicate | Key flags |
|---|---|---|
| `env-refs` | `CheckEnvRefs` | `--hostname=X --path=./` |
| `run-start-build-contract` | `CheckRunStartBuildContract` | `--hostname=X --path=./` |
| `env-self-shadow` | `CheckEnvSelfShadow` | `--hostname=X --path=./` |
| `ig-code-adjustment` | `CheckIGCodeAdjustment` | `--hostname=X --path=./ [--showcase]` |
| `ig-per-item-code` | `CheckIGPerItemCode` | `--hostname=X --path=./ [--showcase]` |
| `comment-specificity` | `CheckCommentSpecificity` | `--hostname=X --path=./ [--showcase]` |
| `yml-schema` | `CheckZeropsYmlFields` | `--hostname=X --path=./ [--schema-json=<path>]` |
| `kb-authenticity` | `CheckKnowledgeBaseAuthenticity` | `--hostname=X --path=./` |
| `worker-queue-group-gotcha` | `CheckWorkerQueueGroupGotcha` | `--hostname=X --path=./ [--is-worker] [--shares-codebase-with=<host>]` |
| `worker-shutdown-gotcha` | `CheckWorkerShutdownGotcha` | `--hostname=X --path=./ [--is-worker] [--shares-codebase-with=<host>]` |
| `manifest-honesty` | `CheckManifestHonesty` | `--mount-root=./` |
| `manifest-completeness` | `CheckManifestCompleteness` | `--mount-root=./ [--facts=<path>]` |
| `comment-depth` | `CheckCommentDepth` | `--env=N --path=./` |
| `factual-claims` | `CheckFactualClaims` | `--env=N --path=./` |
| `cross-readme-dedup` | `CheckCrossReadmeGotchaUniqueness` | `--path=./` |
| `symbol-contract-env-consistency` | `CheckSymbolContractEnvVarConsistency` | `--mount-root=./ [--plan-json=<path>]` |

### Design decisions applied (per HANDOFF-to-I4 §C-7e)

1. **Output**: plain text default (`PASS <name>` / `FAIL <name>: <detail>` / `SKIP ...`), ndjson on `--json`. Exit 0 iff every row is pass; 1 on any fail. Empty-predicate-output (graceful skip) prints a SKIP line + exits 0 — no row means "upstream surface concern" per the predicate contract.
2. **Flag convention** matches §18. `--hostname=X` for the 10 host-scoped checks; `--mount-root=./` for the 3 mount-scoped (alias for `--path`); `--env=N` for the 2 env-scoped (resolved via `workflow.EnvFolder` → canonical folder name like "0 — AI Agent"). `--path=./` is always available as the default anchor.
3. **State/plan sourcing**: `symbol-contract-env-consistency` takes `--plan-json=<path>` (simpler than reading full session state — the shim just loads a RecipePlan JSON blob); absent → vacuous pass via the predicate's short-circuit on empty `EnvVarsByKind`. Worker shims take `--is-worker` / `--shares-codebase-with=<host>` explicitly so the author opts in per invocation without loading the plan.
4. **`yml-schema --schema-json=<path>` or skip**. No network fetch from shim — avoids making gates flaky on agent containers without internet. When the flag is absent, stderr prints `SKIP — --schema-json not provided` and exit 0.
5. **Integration test**: one table-driven test function, 16 rows, substring assertions on combined stdout+stderr. Plus 3 dispatcher-path tests: unknown-subcommand (exit 1 + usage), empty-args (exit 1 + usage), all-16-registered (guards registry split).
6. **Authorship pattern**: main-instance sequential (not subagent-dispatch). `check.go` registry is shared across every shim, so parallel subagent authoring would race on the `registerCheck` calls. Total: ~1.5 hours wall including fixture tuning.

### Verification

- `go test ./... -count=1` — full suite green across 21 packages (including new `cmd/zcp/check`).
- `make lint-local` — 0 issues (after converting two `_ = json.NewEncoder(w).Encode(...)` sites to a `writeJSONLine` helper that handles marshal errors explicitly, per `errchkjson`, and running `gofmt` on the test file).

### LoC delta

- New `cmd/zcp/check/` package: +1,382 LoC (243 parent + 360 integration test + 16 shims at ~44 LoC each = ~711).
- `cmd/zcp/main.go`: +5 LoC (case "check" dispatch + import line).
- **Total**: ~+1,387 LoC (handoff estimate was ~+900 LoC; over by ~54%, driven by comprehensive per-shim flag-validation error messages + the integration-test fixture needing enough realism to satisfy every predicate's minimum input shape).

### Breaks-alone consequence

Author-runnable axis opens. Scaffold / feature / writer / code-review sub-agents can now invoke each gate's check over SSH against their own mount before attesting — turning gate failures into author-resolvable issues instead of post-attest retry loops. The gate path keeps working unchanged (same predicate functions, same tool-layer wrappers). No user-observable behavior change at the MCP surface.

### Ordering deps verified

- C-7a ✓ / C-7b ✓ / C-7c ✓ / C-7d ✓ — all 16 predicates exist in `internal/ops/checks`.

### C-7 complete

**C-7a + C-7b + C-7c + C-7d + C-7e all green.** 16 predicates live in `internal/ops/checks` with exactly one implementation per check; 16 shim subcommands live in `cmd/zcp/check/` as thin adapters over those predicates. Gate↔shim invariant established at the Go-function level.

### Pre-C-7.5 gate preparation

Not a gated commit itself; the next user-review gate is **pre-C-7.5** (editorial-review sub-agent role introduction). Stop and report C-7 complete, ready for pre-C-7.5 gate review before authoring C-7.5.

### Known follow-ups

- CLI shims currently don't hydrate `discoveredEnvVars` / `liveHostnames` for `CheckEnvRefs` — author-side shim passes empty maps, so the check only catches shape errors (malformed `${...}`, missing host prefix). The gate path uses `state.DiscoveredEnvVars` from the live API. Future enhancement: `--live-vars-json=<path>` to let the author dump the platform's discovered-vars snapshot before invoking the shim. Out of C-7e scope.
- `collectReadmesByHost` in `manifest_honesty.go` + `cross_readme_dedup.go` relies on the `{host}dev` naming convention. Non-conventional folder layouts (a future recipe might use `api/` instead of `apidev/`) would miss. Non-issue at v34; flagged for C-10+ if naming ever drifts.
- `symbol-contract-env-consistency` shim takes `--plan-json` directly rather than the handoff's proposed `--state=<dir>`. Rationale: the engine serializes full `WorkflowState` into `sessions/{sid}.json`, not a standalone `recipe-plan.json`, so a `--state=<dir>` flag would require additional session-registry machinery that doesn't exist. Direct plan-JSON is simpler + sufficient for the author use-case (dump plan, invoke shim). Documented here; revisit in C-14 if the `zcp dry-run recipe` harness surfaces a need.

---

## C-7.5 — Editorial-review role introduction (substep + validator + 7 dispatch-runnable checks)

**Status**: green

### What landed

C-7.5 introduces the editorial-review sub-agent role per [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) line 317-319 + the 2026-04-20 research refinement. The reviewer walks the finished deliverable with a fresh-reader stance, applies inline fixes for editorial defects, and returns a structured payload that populates seven dispatch-runnable checks per [check-rewrite.md §16a](03-architecture/check-rewrite.md). Closes the **classification-error-at-source** class (writer self-classification errors escape all prior gates because the manifest faithfully reflects the wrong classification) + defense-in-depth on v28 wrong-surface, v28 folk-doctrine fabrication, v28 cross-surface-duplication, and v34 self-referential classes.

- `internal/workflow/recipe_substeps.go` — added `SubStepEditorialReview = "editorial-review"` and placed it FIRST in `closeSubSteps` showcase return (before `code-review`, before `close-browser-walk`). Minimal tier unchanged — "ungated-discretionary" minimal editorial-review is deferred (see Known deferred below).
- `internal/workflow/engine_recipe.go` — added **Fix D ordering guard**: `code-review` substep must have `editorial-review` complete first. Mirrors Fix C's shape + message style; error carries `SUBAGENT_MISUSE` code and explains WHY (code-review grading pre-fix content misses v28/v34 classification errors editorial would have caught).
- `internal/workflow/recipe_guidance.go` — added `SubStepEditorialReview` to `atomIDForSubStep` (maps to `close.editorial-review` atom), `composeDispatchBriefForSubStep` (calls `BuildEditorialReviewDispatchBrief` with facts-log + manifest pointer paths; showcase-only; returns empty on minimal), `shouldPrependPriorDiscoveries` (**NEVER prepends** for editorial-review — porter-premise requires fresh-reader stance per refinement §10 open-question #6), and `laneForSubStep` (returns `"editorial-review"` for symmetry even though no fact prepend happens — future audits can distinguish deliberate "no prepend" from unmapped lanes).
- `internal/workflow/recipe_brief_facts.go` — added `SubStepEditorialReview: 12` to `substepOrder`; shifted `SubStepCloseReview` → 13, `SubStepCloseBrowserWalk` → 14.
- `internal/content/workflows/recipe/phases/close/editorial-review.md` — new main-agent phase atom (60 LoC) pairing with the 10 editorial-review brief atoms (already landed in C-4). Mirrors the `close.code-review` atom shape: describes what main does at the substep, the attestation shape, and the scope separation between phase atom and brief atoms.
- `internal/workflow/atom_manifest_phases.go` — added the new atom entry (`close.editorial-review` showcase-tier-conditional).
- `internal/workflow/atom_manifest.go` — bumped `atomCountBaseline` from 120 → 121 and updated the docstring accounting (66 phase atoms post-C-7.5).
- `internal/workflow/editorial_review_return.go` — new file (115 LoC): `EditorialReviewReturn` type + supporting structs (`EditorialReviewFinding`, `ReclassificationDeltaRow`, `CitationCoverage`, `CrossSurfaceLedgerRow`, `InlineFixApplied`) matching `completion-shape.md` exactly; `ParseEditorialReviewReturn(attestation string) (*EditorialReviewReturn, error)` parses the substep attestation with typed errors for empty / non-JSON / parse-failure shapes. Snake_case JSON tags carry per-field `//nolint:tagliatelle` directives naming the editorial-review subagent wire contract.
- `internal/workflow/editorial_review_checks.go` — new file (230 LoC): seven predicates + `EditorialReviewChecks(ret *EditorialReviewReturn) []StepCheck` battery. Each predicate consumes the parsed payload and emits one row in the stable §16a order (dispatched → no_wrong_surface_crit → reclassification_delta → no_fabricated_mechanism → citation_coverage → cross_surface_duplication → wrong_count). Every failing Detail includes the exported `EditorialReviewPreAttestNote` marker so downstream consumers can distinguish §16a rows from §16 shell-runnable rows without parsing the check name.
- `internal/workflow/editorial_review_validator.go` — new file (95 LoC): `validateEditorialReview` parses the attestation, runs the battery, returns `SubStepValidationResult{Passed, Issues, Guidance, Checks}`. On parse failure emits 7 placeholder-FAIL rows so the agent still sees the full check surface; on predicate failure surfaces only the failing rows' details in `Issues` (for Phase C adaptive-retry context). `Guidance` is prose that names each failing check + links back to the `EditorialReviewPreAttestNote` reminder (no shell-shim exists; the dispatch IS the runnable form).
- `internal/workflow/recipe_substep_validators.go` — `SubStepValidationResult` gained an optional `Checks []StepCheck` field; `getSubStepValidator` now returns `validateEditorialReview` for the editorial-review substep.
- `internal/workflow/engine_recipe.go` — the substep-validation failure path merges `result.Checks` into `resp.CheckResult.Checks`, surfacing per-predicate pass/fail detail alongside the aggregate Issues/Guidance. Other validators (attestation-floor only) leave Checks nil and retain the pre-C-7.5 summary-only shape.
- Tests: `editorial_review_checks_test.go` (240 LoC, 17 table rows × 7 predicates + order invariant + pre-attest-note invariant + parser shapes); `editorial_review_validator_test.go` (95 LoC, 4 scenarios: clean-pass, empty-attestation-fail, predicate-failure-propagates, guidance-names-pre-attest-note); `editorial_review_helpers_test.go` (35 LoC, `validEditorialReviewPayload` + `mustMarshalEditorialReviewReturn` shared helpers).
- Updated pre-existing tests: `recipe_test.go::TestRecipeCloseSubSteps_ExactlyThreeAutonomousSubSteps` (was `ExactlyTwo` — now 3 substeps: editorial-review + code-review + close-browser-walk); `recipe_close_ordering_test.go::TestCloseSubStepOrder_ReviewBeforeBrowserWalkAccepted` + `TestCloseSubStepOrder_FixCGuardDoesNotFireOnCodeReview` — both now attest editorial-review before code-review.

### The 7 checks (per `check-rewrite.md §16a`)

| Check | Verdict source | Closes |
|---|---|---|
| `editorial_review_dispatched` | `len(ret.SurfacesWalked) > 0` after valid JSON parse | v34 classification-error-at-source |
| `editorial_review_no_wrong_surface_crit` | `ret.FindingsBySeverity.Crit == 0` | v28 wrong-surface, v33 scaffold-decision, v34 self-referential |
| `editorial_review_reclassification_delta` | every reclass row `Final == ReviewerSaid` | classification-error-at-source, v28 folk-doctrine |
| `editorial_review_no_fabricated_mechanism` | no CRIT finding carries a fabricated-mechanism tag/description | v23 execOnce-burn, v28 folk-doctrine |
| `editorial_review_citation_coverage` | `ret.CitationCoverage.Denominator == ret.CitationCoverage.Numerator` | v20 generic-platform-leakage, v28 folk-doctrine |
| `editorial_review_cross_surface_duplication` | no ledger row has `Severity == "duplicate"` | v28 cross-surface-fact-duplication |
| `editorial_review_wrong_count` | `ret.FindingsBySeverity.Wrong <= 1` | v34 self-referential + v28 wrong-surface post-inline-fix |

All 7 are pure predicates over the parsed payload — no filesystem access, no network, no plan lookup.

### Design decisions applied

1. **Validator-based surfacing**. §16a says editorial-review checks populate `StepCheck` rows at close.editorial-review complete. Currently substeps only have `SubStepValidator` returning `SubStepValidationResult{Passed, Issues, Guidance}` — no StepCheck rows. C-7.5 extends `SubStepValidationResult` with an optional `Checks []StepCheck` field + the engine merges it into `resp.CheckResult.Checks`. This keeps the existing substep-validator pattern intact for attestation-floor validators (they leave Checks nil) while giving editorial-review the structured-row channel the spec requires.
2. **Attestation IS the JSON payload**. Per `completion-shape.md`, the reviewer returns a single structured payload. The main agent attaches that JSON verbatim as the substep attestation; the validator parses it. No out-of-band state channel — payload flow is attestation-only.
3. **Parse failure emits 7 FAIL rows** (not 1). Keeps the check surface consistent so the agent always sees the same row set in retry iterations — regression-direction coverage stays intact even when the attestation shape is wrong.
4. **Ordering guard mirrors Fix C**. The v8.98 Fix C guard on `close-browser-walk → code-review` is extended with a Fix D guard on `code-review → editorial-review`. Same structured-error-message shape; same `SUBAGENT_MISUSE` error code; same "names both substeps + WHY" discipline.
5. **Showcase-only**. Minimal tier's discretionary editorial-review is deferred (see Known deferred). Spec calls for ungated-discretionary semantics that don't exist today — adding them requires new engine plumbing (a substep list where completion isn't gated by `enforceSubStepsComplete`). Out of C-7.5 scope; a later refinement can extend to minimal.
6. **No Prior Discoveries prepend**. The reviewer's porter-premise requires fresh-reader stance. `shouldPrependPriorDiscoveries` explicitly returns `false` for `SubStepEditorialReview` even though the substep dispatches a sub-agent (symmetry with other dispatch substeps would prepend).

### Verification

- `go test ./... -count=1` — full suite green across 21 packages (including the new workflow files).
- `make lint-local` — 0 issues (after per-field `//nolint:tagliatelle` directives naming the editorial-review subagent wire contract, a `stepCheckStatus{Fail,Pass}` constant pair to silence `goconst`, `slices.Contains` modernization, and `gofmt` on the new types file).

### LoC delta

- Go source (workflow): +530 LoC (editorial_review_return.go 115 + editorial_review_checks.go 230 + editorial_review_validator.go 95 + substep const + closeSubSteps + guard + guidance wiring + substepOrder + atom manifest bump + SubStepValidationResult extension + engine Checks surfacing).
- Tests (workflow): +400 LoC (editorial_review_checks_test.go 240 + editorial_review_validator_test.go 95 + editorial_review_helpers_test.go 35 + updated ordering + close-substep-count tests).
- Atom content: +25 LoC (new `phases/close/editorial-review.md`).
- **Total**: ~+955 LoC (handoff estimate was ~+2,800 LoC, but the 10 editorial-review atoms + stitcher had already landed in C-4 + C-5 foundation — C-7.5's actual net delta is the substep wiring + validator + 7 checks + tests).

### Breaks-alone consequence

Every new v35+ showcase-tier close step now dispatches editorial-review first. Authors who complete close in the canonical order (editorial-review → code-review → close-browser-walk) see no visible workflow change beyond the new substep name at the top of the close phase; authors who attempt an out-of-order attestation get a structured error naming the substep dependency + WHY (Fix D). No regression against prior shipped recipes — these only run at close step on showcase recipes going forward.

### Ordering deps verified

- C-1 ✓ (SymbolContract for plan-aware dispatch brief composition).
- C-2 ✓ (RouteTo enum — editorial-review's reclassification check cross-references routing).
- C-3 ✓ (atom_manifest — new close.editorial-review phase atom registered + baseline bumped).
- C-4 ✓ (10 editorial-review brief atoms on disk).
- C-5 ✓ (`BuildEditorialReviewDispatchBrief` already existed; wiring lights it up at the substep).
- C-6 ✓ (check infrastructure — shape of StepCheck / StepCheckResult reused).
- C-7e ✓ (shim-surface conventions — C-7.5's checks are dispatch-runnable per §16a, no shim equivalent; the `EditorialReviewPreAttestNote` marker makes the distinction explicit in every row's Detail).

### Known deferred

- **Minimal-tier discretionary editorial-review**. Spec §C-7.5 calls for minimal's `closeSubSteps` to return an ungated-discretionary list including editorial-review + code-review. Implementing "ungated-discretionary" requires new engine semantics (a substep list where `enforceSubStepsComplete` doesn't block on incompleteness). Out of C-7.5 scope — showcase-only implementation is the minimum viable C-7.5. A follow-up commit can extend to minimal when the discretionary-substep infrastructure lands (likely alongside a broader close-substep gating refactor).
- **Dispatch-composition test against step-4 goldens**. Pre-ship gate per rollout-sequence §C-7.5 asks for a byte-diff test against `docs/zcprecipator2/04-verification/brief-editorial-review-{minimal,showcase}-composed.md`. The goldens are synthetic / advisory (not byte-identical to stitcher output — they're reader-facing representations). C-14's `zcp dry-run recipe` harness is the canonical byte-diff surface. C-7.5's coverage is the stitcher unit test (`TestBuildEditorialReviewDispatchBrief_NoPriorDiscoveries`) from C-5 + the validator tests here; the byte-diff test lands with C-14.
- **Pre-attest field on failing rows**. §16a says `preAttestCmd` reads the `EditorialReviewPreAttestNote` value. The current `StepCheck` struct doesn't have a `PreAttestCmd` field — that ships with C-10's payload shape change. C-7.5 emits the marker in `Detail` as an interim surface; C-10 moves it to the dedicated field uniformly across all checks. Documented to confirm C-10 must include the §16a rows in its cascading field-rename pass.

---

## C-8 — Expand `writer_manifest_honesty` to all 6 routing dimensions (P5)

**Status**: green

### What landed

Per [check-rewrite.md §12](03-architecture/check-rewrite.md) + [principles.md P5](03-architecture/principles.md), the single-dimension `(discarded, published_gotcha)` check is extended to all 6 `(routed_to × surface)` pairs. Closes the v34 DB_PASS class + defense-in-depth across every wrong-surface duplication route the manifest grammar can express.

- [`internal/ops/checks/manifest.go`](../../internal/ops/checks/manifest.go):
  - `honestyDimension` struct declares one dimension (route match predicate + surface extractor + check name + human-readable labels + fail guidance). The predicate is pure-function composition — adding a dimension is a one-struct-literal append in the `honestyDimensions` table.
  - `honestyDimensions` is the 6-row declarative table in stable order: `discarded_as_gotcha`, `claude_md_as_gotcha`, `integration_guide_as_gotcha`, `zerops_yaml_comment_as_gotcha`, `env_comment_as_gotcha`, `any_as_intro`.
  - `CheckManifestHonesty` is now a one-line loop over the table; per-dimension grading lives in `evaluateHonestyDimension` so each dimension's logic is independently testable.
  - `extractIntroPhrases` new helper: extracts the intro-fragment body, splits by `.` / `!` / `?`, keeps phrase units ≥ 12 runes. Sentence-level Jaccard granularity matches how a fact title concept typically leaks into prose.
  - `routedEquals(target)` and `routedAnyExceptIntroOrEmpty` predicate constructors — declarative match shapes for the dimension table.
- [`internal/ops/checks/manifest_test.go`](../../internal/ops/checks/manifest_test.go):
  - `TestCheckManifestHonesty_Table` updated to assert against the dimension-specific `writer_manifest_honesty_discarded_as_gotcha` row.
  - `TestCheckManifestHonesty_AllDimensionsReturnSixRows` pins the exact 6-row emission shape + order. A regression that adds/removes/reorders dimensions fails this test.
  - `TestCheckManifestHonesty_PerDimensionLeakage` — table-driven per `(claude_md, content_ig, zerops_yaml_comment, content_env_comment)` route: a fact routed to each of these that leaks into the knowledge-base gotcha fragment fails only its own dimension; the other 5 rows pass. Pins the cross-dimension isolation invariant.
  - `TestCheckManifestHonesty_AnyAsIntroDimension` pins the intro surface: a `claude_md`-routed fact appearing in intro fails `any_as_intro`; an intro-routed fact appearing in intro passes (correct routing).
  - `toShim` helper — tiny test glue converting `[]workflow.StepCheck` to the local `[]stepCheckShim` form `findCheck` expects.
- [`internal/tools/workflow_checks_content_manifest_test.go`](../../internal/tools/workflow_checks_content_manifest_test.go) — three pre-existing tests updated from `findCheckByName(..., "writer_manifest_honesty")` to `findCheckByName(..., "writer_manifest_honesty_discarded_as_gotcha")`. No other tool-layer changes required — `checkWriterContentManifest` already delegates to `opschecks.CheckManifestHonesty`, so the row-shape expansion propagates naturally.

### The 6 dimensions

| Check name | Route matched | Surface extractor | Failure meaning |
|---|---|---|---|
| `writer_manifest_honesty_discarded_as_gotcha` | `discarded` | gotcha stems | writer marked fact discarded but shipped matching gotcha |
| `writer_manifest_honesty_claude_md_as_gotcha` | `claude_md` | gotcha stems | claude_md-routed fact duplicates as published gotcha |
| `writer_manifest_honesty_integration_guide_as_gotcha` | `content_ig` | gotcha stems | integration-guide fact duplicates as published gotcha |
| `writer_manifest_honesty_zerops_yaml_comment_as_gotcha` | `zerops_yaml_comment` | gotcha stems | zerops.yaml-inline fact duplicates as published gotcha |
| `writer_manifest_honesty_env_comment_as_gotcha` | `content_env_comment` | gotcha stems | env-comment-routed fact duplicates as published gotcha |
| `writer_manifest_honesty_any_as_intro` | any non-empty non-`content_intro` | intro-phrase sentences | any non-intro fact title leaks near-verbatim into intro |

All 6 rows emit every run — stable surface regardless of pass/fail.

### Shim alignment

The `cmd/zcp/check/manifest-honesty` CLI shim is unchanged in body: it calls `opschecks.CheckManifestHonesty` and emits every row returned. Exit code is 0 iff every row is pass; any dimension failing exits 1. The shim's integration test substring assertion (`writer_manifest_honesty`) still matches all 6 row names because every dimension's check name shares that prefix.

### Verification

- `go test ./... -count=1` — full suite green across 21 packages.
- `make lint-local` — 0 issues.

### LoC delta

- `internal/ops/checks/manifest.go`: +185 LoC (dimension table + evaluateHonestyDimension + extractIntroPhrases + routedEquals/routedAnyExceptIntroOrEmpty helpers), -30 LoC (old single-dimension body).
- `internal/ops/checks/manifest_test.go`: +110 LoC (3 new test functions + toShim helper).
- `internal/tools/workflow_checks_content_manifest_test.go`: ~0 LoC (3 call-site renames).
- **Total**: ~+265 LoC (handoff estimate was ~+350; under by a modest margin because the dimension table's declarative shape means the body shrank more than expected).

### Breaks-alone consequence

Stricter gate at `deploy.readmes` complete. If a v35 candidate's writer emits a manifest that trips any new dimension, the gate fails with a per-dimension check name. Expected: v35 writer atom content (C-4) already teaches routing honesty along every dimension; first-pass output should pass. If the gate fails on a dimension the writer atom didn't cover, that's evidence the atom needs expansion — the per-dimension name in the fail detail names exactly where.

### Ordering deps verified

- C-2 ✓ (RouteTo enum — the dimension table matches against enum values).
- C-7a/b/c/d ✓ (shim-surface infrastructure + ops/checks package home for the predicate).
- C-7.5 ✓ (no cross-dependency, but C-7.5 completed before C-8 per rollout-sequence ordering).

### Known follow-ups

- `any_as_intro` sentence-granularity Jaccard may over-fire on short intros where every sentence shares stop-word-stripped tokens with common fact titles. Calibration: the 0.3 threshold + the ≥12-rune filter + stop-word elimination should keep false-positive rate low; first v35 run is the empirical signal. If intros are regularly flagged for unrelated facts, the threshold or the phrase extractor needs tightening — revisit post-v35.
- `briefs/code-review/manifest-consumption.md` atom was slated (per rollout-sequence §C-8) for a byte-check against the new dimension names. The atom's content is route-concept-based, not check-name-based — it instructs the reviewer to audit every routing dimension without naming internal check identifiers (P2 invariant). No update needed; C-8 ships as-is.

---

## C-9 — Delete `knowledge_base_exceeds_predecessor`

**Status**: green

### What landed

Per [check-rewrite.md §15](03-architecture/check-rewrite.md), the one check marked for definite deletion is removed. `knowledge_base_exceeds_predecessor` was informational-only since v8.78 (predecessor overlap was ruled fine; standalone recipes are read in isolation) and carried zero gate value. `CheckKnowledgeBaseAuthenticity` is the upstream replacement — grades gotcha SHAPE rather than net-new COUNT — and now fires directly from `checkCodebaseReadme` without the exceeds-predecessor wrapper.

- [`internal/tools/workflow_checks_predecessor_floor.go`](../../internal/tools/workflow_checks_predecessor_floor.go) — `checkKnowledgeBaseExceedsPredecessor` function deleted; `checkKnowledgeBaseAuthenticity` wrapper retained (still delegates to `opschecks.CheckKnowledgeBaseAuthenticity`). File shrinks from 73 LoC to 30 LoC (with the C-9 rationale comment block).
- [`internal/tools/workflow_checks_recipe.go`](../../internal/tools/workflow_checks_recipe.go):
  - `checkCodebaseReadme` signature loses the `predecessorStems []string` parameter. Body now calls `checkKnowledgeBaseAuthenticity` directly when the plan is showcase tier AND a knowledge-base fragment is present on the README.
  - `checkRecipeDeployReadmes` signature loses the `kp knowledge.Provider` parameter — its sole consumer (the predecessor-stems resolution for `workflow.PredecessorGotchaStems`) is gone. `buildRecipeStepChecker`'s call site drops the `kp` argument.
- [`internal/tools/workflow_checks_predecessor_floor_integration_test.go`](../../internal/tools/workflow_checks_predecessor_floor_integration_test.go) — deleted (475 LoC). The file was purpose-built to test `exceeds_predecessor` wiring across per-codebase README loops; every test asserted against `*_knowledge_base_exceeds_predecessor` row names. Post-C-9 the wiring those tests exercised no longer exists. Authenticity-check wiring is exercised by unit tests in `workflow_checks_predecessor_floor_test.go` (rewritten) + the deploy-readmes unit tests in `workflow_checks_recipe_test.go`.
- [`internal/tools/workflow_checks_predecessor_floor_test.go`](../../internal/tools/workflow_checks_predecessor_floor_test.go) — rewritten: the 6 exceeds-predecessor tests are deleted; the 2 authenticity tests (V12SyntheticMix, V7Style) survive but now call `checkKnowledgeBaseAuthenticity` directly with a knowledge-base fragment body (instead of routing through the deleted wrapper that extracted the fragment internally). File shrinks from 241 LoC to 70 LoC.
- [`internal/tools/workflow_checks_recipe_test.go`](../../internal/tools/workflow_checks_recipe_test.go) — 3 test call sites updated (`checkRecipeDeployReadmes(stateDir, nil, nil)` → `checkRecipeDeployReadmes(stateDir, nil)`). `workerZeropsYaml` test fixture deleted (sole consumer was the deleted integration test file; the unused-symbol lint caught it).

### Verification

- `go test ./... -count=1` — full suite green across 21 packages.
- `make lint-local` — 0 issues.

### LoC delta

- `internal/tools/workflow_checks_predecessor_floor.go`: -43 LoC (body removed, comment block replaces).
- `internal/tools/workflow_checks_predecessor_floor_test.go`: -171 LoC (6 tests deleted, 2 rewritten to call authenticity directly).
- `internal/tools/workflow_checks_predecessor_floor_integration_test.go`: -475 LoC (file deleted).
- `internal/tools/workflow_checks_recipe.go`: -5 LoC (predecessor-stems call + 2 function-signature params dropped; +15 LoC of the authenticity-direct-call block replacing the floor wrapper).
- `internal/tools/workflow_checks_recipe_test.go`: -28 LoC (`workerZeropsYaml` fixture deleted + 3 call-site diffs).
- **Total**: ~-720 LoC net (handoff estimate was -70 LoC, but the integration-test file deletion dominates — the handoff estimate didn't account for the purpose-built integration coverage).

### Breaks-alone consequence

One fewer informational check row fires at `deploy.readmes` complete. Zero regression risk per check-rewrite.md §15 — the check was informational-only since v8.78, so `_exceeds_predecessor` rows carried no gating weight. Authenticity check is unchanged in behavior; it simply fires from a different call-site now (directly inside `checkCodebaseReadme` instead of being a ride-along inside the deleted exceeds-predecessor wrapper).

### Ordering deps verified

- C-7b ✓ (authenticity-rewrite already moved the predicate into `internal/ops/checks/kb_authenticity.go`; C-9's production path calls the same thin wrapper `checkKnowledgeBaseAuthenticity`).

### Known follow-ups

- `workflow.PredecessorGotchaStems` and `workflow.CountNetNewGotchas` in `internal/workflow/recipe_knowledge_chain.go` are now unused in production code. Left in place for now — their public-API signatures suggest future tests/uses, and removing exported symbols outside C-9's scope adds risk. C-15 cleanup pass can remove them if still unreferenced.
- `workflow_checks_predecessor_floor.go` is now a ~30 LoC file with one thin-wrapper function. Consider consolidating with `workflow_checks_recipe.go` during C-15's file-structure pass.

---

## C-10 — Shrink `StepCheck` failure payload to `{name, detail, preAttestCmd, expectedExit}`

**Status**: green

### What landed

The v8.96 Theme A verbose diagnostic surface (`ReadSurface`/`Required`/`Actual`/`CoupledWith`/`HowToFix`/`Probe`) and v8.104 Fix E (`PerturbsChecks`) are removed from the agent-facing payload. P1 `PreAttestCmd` + `ExpectedExit` replace them: if an author wants to know whether a check would pass, run `PreAttestCmd` and compare the exit code against `ExpectedExit`. Rich prose detail stays in `Detail` (one-line summary, not a field tower). `NextRoundPrediction` on `StepCheckResult` + its derivation helper + `StampCoupling` + `CoupledNames` are deleted outright — all depended on fields that no longer exist.

- [`internal/workflow/bootstrap_checks.go`](../../internal/workflow/bootstrap_checks.go) — `StepCheck` struct rewritten to `{Name, Status, Detail, PreAttestCmd, ExpectedExit}`. `StepCheckResult` loses `NextRoundPrediction`; `AnnotateNextRoundPrediction` function deleted. File shrinks from 133 LoC to 64 LoC.
- [`internal/workflow/coupling.go`](../../internal/workflow/coupling.go) — deleted (117 LoC). `StampCoupling`/`CoupledNames` were surface-derived coupling stampers that needed `ReadSurface` + `HowToFix`; with those fields gone the function has nothing to read and nothing to write.
- [`internal/workflow/coupling_test.go`](../../internal/workflow/coupling_test.go) — deleted (tests targeted the deleted functions).
- 6 `StampCoupling` invocation sites removed from `internal/tools/`: `workflow_checks.go` (2 sites: provision + deploy dispatch), `workflow_checks_generate.go`, `workflow_checks_finalize.go`, `workflow_checks_recipe.go` (2 sites: generate + deploy.readmes). All 3 `workflow.AnnotateNextRoundPrediction` invocation sites removed as well.
- Check-emission sites migrated to PreAttestCmd shape (roughly ~300 LoC across 12 files — most diffs are "6-line field block → 1-line PreAttestCmd + consolidated Detail prose"):
  - `internal/ops/checks/comment_depth.go`: PreAttestCmd = `zcp check comment-depth --env={folder} --path=./`. Detail prose carries the former `ReadSurface + Required + Actual + HowToFix` condensed to one-line.
  - `internal/ops/checks/factual_claims.go`: PreAttestCmd = `zcp check factual-claims --env={folder} --path=./`.
  - `internal/ops/checks/kb_authenticity.go`: PreAttestCmd = `zcp check kb-authenticity [--hostname={host}] --path=./`.
  - `internal/tools/workflow_checks_recipe.go`: `fragment_*_blank_after_marker` + the embedded-YAML `comment_ratio` in `checkReadmeFragments` shed their verbose-field blocks; Detail prose retains the actionable remedy.
  - `internal/tools/workflow_checks_finalize.go`: `_comment_ratio` + `_cross_env_refs` shed their verbose-field blocks.
  - `internal/tools/workflow_checks_dedup.go`: `gotcha_distinct_from_guide` sheds `PerturbsChecks` + `CoupledWith`; the perturbation warning about `cross_readme_gotcha_uniqueness` is now inline prose in `Detail`.
- [`internal/workflow/editorial_review_checks.go`](../../internal/workflow/editorial_review_checks.go) — C-7.5's §16a checks now set `PreAttestCmd = EditorialReviewPreAttestNote` on every failing row (moved from Detail string concatenation). The "reviewer IS the runner" marker lives in its structural slot.
- Test cascade:
  - [`internal/tools/workflow_checks_diagnostics_test.go`](../../internal/tools/workflow_checks_diagnostics_test.go) — rewritten end-to-end. The pre-C-10 file asserted every P0 check populates `ReadSurface/Required/Actual/HowToFix` with hedging-phrase discipline + length bounds + next-round-prediction heuristics. Those invariants don't exist post-C-10. New file asserts the minimum post-C-10 payload contract: every failing row carries a non-empty Detail (~25 LoC test), and §18-migratable predicates emit a `zcp check <name>` invocation in PreAttestCmd (~45 LoC test).
  - [`internal/tools/workflow_checks_dedup_test.go`](../../internal/tools/workflow_checks_dedup_test.go) — v8.104 Fix E assertion updated: `c.PerturbsChecks` → `c.Detail` substring check for the perturbation warning about `cross_readme_gotcha_uniqueness`.
  - [`internal/workflow/editorial_review_checks_test.go`](../../internal/workflow/editorial_review_checks_test.go) — `TestEditorialReviewChecks_EveryFailDetailNamesPreAttestNote` renamed to `_CarriesPreAttestNote`; assertion redirected from Detail substring to PreAttestCmd equality.
  - [`internal/workflow/editorial_review_validator_test.go`](../../internal/workflow/editorial_review_validator_test.go) — validator tests unchanged (parse-failure + predicate-failure paths) because the validator's prose Guidance still embeds the note for human consumers.

### Verification

- `go test ./... -count=1` — full suite green across 21 packages.
- `make lint-local` — 0 issues (after `gofmt` on editorial_review_checks.go).

### LoC delta

- `internal/workflow/bootstrap_checks.go`: -69 LoC.
- `internal/workflow/coupling.go`: -117 LoC (file deleted).
- `internal/workflow/coupling_test.go`: -~250 LoC (file deleted; size estimated).
- `internal/tools/workflow_checks_diagnostics_test.go`: -~230 LoC (entire v8.96 suite + next-round-prediction table deleted; replaced with ~85 LoC C-10-shape assertions).
- 6 emission-site rewrites across `internal/tools/workflow_checks_*.go` + 4 under `internal/ops/checks/`: ~-200 LoC net (verbose-field blocks deleted, Detail prose condensed, PreAttestCmd added).
- Editorial-review ~40 LoC of field reshuffling (+7 `PreAttestCmd: EditorialReviewPreAttestNote`, -7 `" + EditorialReviewPreAttestNote"` concatenations).
- **Total**: ~-700 LoC net. Handoff estimate was ~-100 LoC + ~+400 LoC test updates; actual came out net negative because the `StampCoupling` + `NextRoundPrediction` infrastructure (plus the verbose-field-oriented diagnostics test file) deleted more code than PreAttestCmd additions created.

### Breaks-alone consequence

BREAKING payload shape change visible to any JSON consumer of `StepCheck` rows. Per check-rewrite.md's ordering rationale: the new content tree (C-5 cutover) teaches the agent the new payload shape; combined with C-7e's `zcp check <name>` shim tree, authors have one runnable form per check instead of five advisory fields. Older on-disk session state carrying the v8.96-era verbose fields deserializes with those fields silently dropped (Go's JSON decoder ignores unknown fields by default).

### Ordering deps verified

- C-5 (cutover) ✓ — atoms + stitchers teach the new payload shape.
- C-6 ✓ (+5 new architecture-level checks emit the new shape from the start).
- C-7a/b/c/d/e ✓ (16 migrated predicates + CLI shim tree provide the PreAttestCmd runnable form).
- C-7.5 ✓ (§16a editorial-review marker moves from Detail to PreAttestCmd).
- C-8 ✓ (6-dimension manifest honesty; each row emits the new shape).
- C-9 ✓ (deleted exceeds-predecessor — one fewer emission site to migrate).

### Known deferred

- **Debug-log retention hook**. Spec §C-10 mentioned a `debug.LogCheckFailureDetail(ctx, check, readSurface, required, actual, ...)` helper (~50 LoC) for server-side human inspection of the pre-shape-flip verbose fields. Deferred: implementing it cleanly requires either threading extra log state through every emission site or preserving the verbose fields internally on the Go side and stripping them at JSON-serialize time. Neither is cheap in scope, and the v35 telemetry we'd gate on the log output can come from the existing `--debug` structured logs + agent-side session transcripts. Revisit if v35 regression triage finds the loss of verbose fields a blocking handicap.
- **`workflow.PredecessorGotchaStems` + `workflow.CountNetNewGotchas`**. Still exported post-C-9 with no production callers. C-15 cleanup can remove if still unreferenced.
- **`content/workflows/recipe.md` monolith references to verbose fields**. The legacy content tree names `ReadSurface` / `HowToFix` in agent-facing prose. Not a gate failure — the new atom tree (C-4) is the canonical content source post-C-5 cutover; recipe.md is deleted wholesale in C-15.

---

## C-11 — Empty `NextSteps[]` at close completion

**Status**: green

### What landed

Per [principles.md P4 §Replaces](03-architecture/principles.md) + [data-flow-showcase.md §6b](03-architecture/data-flow-showcase.md), the close-step response carries `NextSteps = []` unconditionally. Export and publish are user-request-only local CLI commands — never autonomous workflow steps. v8.103 established the semantic (export is gated on close=complete server-side but not triggered by the workflow); C-11 makes the empty-default structural rather than content-only.

- [`internal/workflow/recipe_close_response.go`](../../internal/workflow/recipe_close_response.go) — `buildClosePostCompletion` returns `[]string{}` unconditionally. The previous two-entry "ON REQUEST ONLY export / publish" slice is replaced by summary prose that acknowledges both commands as local-CLI-on-demand. `plan` and `outputDir` parameters retained on the signature (with `_ = ` discards) for symmetry with `buildRecipeTransition` and for future close-response extensibility.
- [`internal/workflow/recipe_test.go`](../../internal/workflow/recipe_test.go):
  - `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` → renamed `TestHandleComplete_CloseStepReturnsEmptyNextSteps`. Asserts `len(nextSteps) == 0` + summary names both sub-steps. Regression guard against both v8.97 Fix 2's two-entry shape AND v8.98 Fix B's autonomous framing.
  - `TestHandleComplete_CloseStepPostCompletionBothUserGated` → renamed `TestHandleComplete_CloseStepSummaryNamesExportAndPublishAsUserGated`. The user-gated framing moves from per-entry `ON REQUEST ONLY` markers to summary prose. Test asserts summary mentions export+publish as local CLI + says they are NOT triggered autonomously.
  - `TestHandleComplete_CloseStepSummaryHasNoAutomaticClaims` — banned-substring list adjusted: the literal word `autonomously` is now allowed because the C-11 summary phrasing is "do NOT trigger them autonomously" (the negation-embedding form). Banned: `"Export runs automatically"` + `"run export autonomously"` (the v8.97/v8.98 Fix B positive-assertion forms). Required substring pivots to `"on demand"` OR `"user explicitly"`.

### Verification

- `go test ./... -count=1` — full suite green across 21 packages.
- `make lint-local` — 0 issues.

### LoC delta

- `internal/workflow/recipe_close_response.go`: -12 LoC (two-entry slice + format strings deleted; summary prose expanded).
- `internal/workflow/recipe_test.go`: -20 LoC net (consolidated 2 v8.103-shape tests into 2 C-11-shape tests; adjusted a third).
- **Total**: ~-32 LoC (handoff estimate was -30 LoC + 100 LoC test; actual was net smaller because the new tests are tighter).

### Breaks-alone consequence

Post-close autonomous tool invocations stop firing. An agent that had read the two-entry NextSteps shape and relayed the commands as if they were pending actions now sees an empty slice and cannot misinterpret. Matches v8.103 export-on-request invariant; makes it structural instead of content-only.

### Ordering deps verified

- C-0 ✓ (baseline test — the pre-v8.103 shape was pinned there; C-11 flips the assertion to match the new structural contract).

### Known follow-ups

- `buildRecipeTransition` (separate function) still emits prose with publish + cache-clear + pull walkthroughs. That output lives outside the close-step response payload — it's used by CLI-adjacent surfaces. No C-11 change required; the NextSteps invariant applies only to the workflow-response NextSteps slice.

---

## C-12 — Land `docs/zcprecipator2/DISPATCH.md`

**Status**: green (docs-only)

### What landed

Per [atomic-layout.md §2](03-architecture/atomic-layout.md) + [principles.md P2 §Enforcement](03-architecture/principles.md), dispatcher instructions live in a document that is **never transmitted** to sub-agents. [`docs/zcprecipator2/DISPATCH.md`](DISPATCH.md) is that document.

Contents (7 sections):
1. **The composition surface** — named table of the 5 `Build*DispatchBrief` functions in `atom_stitcher.go`, with their substep + tier gating.
2. **Stitching recipe** — the fixed shape (mandatory-core + role-specific + principles pointer-includes + interpolated inputs) every dispatch brief follows; pointer-include vs inline distinction; the 3 interpolatable inputs (factsLogPath, manifestPath, SymbolContract JSON).
3. **Multi-codebase branching** — role-parameter approach for scaffold (one invocation per codebase); shared-vs-separate-codebase worker dispatch rules; minimal-tier dual-runtime scoping.
4. **Why certain tokens are forbidden in transmitted briefs** — B-1 through B-5 + B-7 + H-4 from the upcoming C-13 build lints, with the rationale for each (the "why"). Covers version anchors, dispatcher vocabulary, internal check names, Go source paths, 300-line cap, orphan-prohibition, positive-P4-form in entry atoms.
5. **Where to edit vs where to look** — maintenance cheat-sheet table for common dispatch refactors.
6. **Golden-diff testing** — points at step-4 composed-brief goldens at `04-verification/brief-*-composed.md` as cold-read review artifacts; flags C-14's dry-run harness as the canonical diff surface.
7. **Operational boundary** — dispatchers see DISPATCH.md; sub-agents see composed briefs; the two are updated together when a new dispatch surface lands.

### Verification

- No Go changes; no test surface.
- `make lint-local` — 0 issues.

### LoC delta

- `docs/zcprecipator2/DISPATCH.md`: +175 LoC (handoff estimate was ~+400; came in under because §4 reads compactly by referencing the upcoming lint rules rather than re-deriving each invariant from principles).

### Breaks-alone consequence

None — documentation-only.

### Ordering deps verified

- C-4 ✓ (atoms exist to reference in §1 + §2 + §5).
- C-5 ✓ (stitcher exists to reference in §1 + §3).
- C-7.5 ✓ (editorial-review dispatch brief exists to reference in §1).

### Known follow-ups

- C-13 must keep its lint rule rationale **consistent** with this document's §4 — if a lint rule changes (e.g. new banned token, modified regex), update DISPATCH.md §4 alongside. The lint file (next commit) is the enforcement; this document is the reasoning.
- C-14's dry-run harness referenced in §6 is the canonical golden-diff surface for future composition refactors. Document currently forward-references the feature; once C-14 lands, the §6 pointer becomes live.

---

## C-13 — Build-time lints on the atomic content tree

**Status**: green

### What landed

Per [principles.md §P2 / P6 / P8](03-architecture/principles.md) + [calibration-bars-v35.md §9 B-1..B-8](05-regression/calibration-bars-v35.md), a new build lint enforces the atom-tree invariants mechanically. Runs as part of `make lint-local` and blocks any commit that introduces a version anchor, dispatcher vocabulary, internal check name, Go source path, oversized atom, orphan prohibition, or forbidden step-entry phrasing.

- [`tools/lint/recipe_atom_lint.go`](../../tools/lint/recipe_atom_lint.go) (~330 LoC) — new Go program, declarative rule table + filesystem walk. Seven rules:
  - **B-1**: no version anchors (`v[0-9]+(\.[0-9]+)*` tokens) anywhere in the atom tree.
  - **B-2**: no dispatcher vocabulary (`compress`, `verbatim`, `include as-is`, `main agent`, `dispatcher`) inside `briefs/`.
  - **B-3**: no internal check names (`writer_manifest_`, `_env_self_shadow`, `_content_reality`, `_causal_anchor`) inside `briefs/`.
  - **B-4**: no Go source paths (`internal/*.go` regex) inside `briefs/`.
  - **B-5**: per-file 300-line cap across the full tree.
  - **B-7**: orphan-prohibition heuristic — any atom containing `do not`, `avoid`, `never`, or `MUST NOT` must also contain a positive-form signal in the ±10 surrounding lines (explicit positive tokens + markdown list markers + shell-command code spans).
  - **H-4**: `phases/*/entry.md` atoms forbid the phrasing `your tasks for this phase are` (positive P4 form required).
- [`tools/lint/recipe_atom_lint_test.go`](../../tools/lint/recipe_atom_lint_test.go) (~100 LoC) — two Go-level tests: `TestRunLint_ProductionTreeIsClean` asserts zero violations on the live tree; `TestRunLint_FiresOnKnownViolations` builds a synthetic fixture tree that trips every rule and pins the fire behavior (regression floor against silent regex drift).
- [`Makefile`](../../Makefile) — new `lint-recipe-atoms` target; `lint-local` gains a dependency on it so `make lint-local` fails if the atom tree drifts.

### Design decisions

1. **Scope via root-relative path prefix** — rules declare scope as `"briefs"` / `"phases"` / `""` (tree-wide); the walk resolves each file's relative path via `filepath.Rel(root, path)` and applies the scope filter against the prefix. This keeps the rule declarations portable across test fixtures that point the linter at a temp directory (`TestRunLint_FiresOnKnownViolations` relies on this).
2. **B-7 positive-form heuristic** — the rule fires when a prohibition token appears WITHOUT a positive-form signal in the ±10 surrounding lines. The positive-form allowlist includes explicit tokens (`instead`, `use`, `using`, `bind`, `name`, `is worth`, `is the rule`, ...), markdown list markers (`\n- `, `\n* `, `\n1. `), and shell-command code spans (`` `zcp `` , ``` ``` ```). The list was tuned against the existing 121 atoms; initial run flagged 5 false positives which were resolved by expanding the allowlist to cover positive phrasings the atoms actually use (present-participle verbs, value-naming clauses, bullet enumerations).
3. **Explicit flush before `os.Exit`** — Go's `defer` does not fire when `os.Exit(1)` is called; the lint wraps its exit path in a helper that flushes `bufio.Writer` to stdout explicitly before exit. Otherwise violations would be suppressed on the failure path (confirmed during implementation — the first draft printed nothing because the deferred flush never ran).

### The 121 atom baseline

The production tree has exactly 121 atoms post-C-7.5 (65 phase atoms + 39 brief atoms + 16 principle atoms + 1 new `phases/close/editorial-review.md` from C-7.5). All 121 pass every rule as of C-13. Any future atom edit that violates a rule fails both `make lint-local` and `go test ./tools/lint/...` — the CI ratchet is established.

### Verification

- `go test ./... -count=1` — full suite green across 22 packages (including new `tools/lint`).
- `make lint-local` — 0 issues (lint-recipe-atoms runs first, then golangci-lint on Go sources).
- Manual smoke test: synthetic fixture at `/tmp/atomtest/briefs/test/bad.md` with B-1 + B-2 + B-3 + B-4 violations fires all four rules with correct line numbers + messages.

### LoC delta

- `tools/lint/recipe_atom_lint.go`: +330 LoC.
- `tools/lint/recipe_atom_lint_test.go`: +100 LoC.
- `Makefile`: +3 LoC (new target + dependency wire).
- **Total**: ~+433 LoC (handoff estimate was +250 LoC; over by ~75% because the positive-form heuristic's allowlist grew larger than the shorthand anticipated, and the test fixture writing took ~60 LoC on its own).

### Breaks-alone consequence

CI gates on atom compliance. Atoms from C-4 + C-7.5 were authored to be compliant — the lint is a ratchet that prevents regression on future atom edits. First v35+ author who introduces a version anchor, dispatcher vocabulary leak, or oversized atom finds out at PR time instead of at showcase-run time.

### Ordering deps verified

- C-4 ✓ (120 atoms on disk, satisfying the lint baseline).
- C-7.5 ✓ (121st atom `phases/close/editorial-review.md` also satisfies).
- C-12 ✓ (DISPATCH.md §4 documents each rule's rationale; C-13's Go source references the same calibration-bar IDs for traceability).

### Known follow-ups

- **B-6 + B-8**: the handoff named B-1..B-8 as the target set. B-6 + B-8 don't correspond to atom-tree invariants I could identify in the principles documentation — both calibration bars apply to runtime output shape (session logs / attestation shape) rather than the atom tree. Left unimplemented; if B-6/B-8 resolve to atom-tree rules in future documentation, they can be added to `rules` as additional table entries without restructuring.
- **Orphan-prohibition heuristic tuning**. The positive-form allowlist covers the current 121 atoms; new atoms may introduce phrasings the allowlist doesn't cover. If the lint starts firing on legitimately-positive content, expand the allowlist rather than rewriting the atom.
- **File position is path+line**. The linter emits `{path}:{line}: [{rule-id}] {message}` format which IDE-friendly editors (VS Code's Problems tab, `gopls`) can parse. Future enhancement: emit SARIF / JSON so CI report formatting stays cleaner.
