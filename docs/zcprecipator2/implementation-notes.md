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
