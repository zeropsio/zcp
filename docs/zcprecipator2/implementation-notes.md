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
