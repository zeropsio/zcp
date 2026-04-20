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
