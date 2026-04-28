# Phase 1 tracker — Types + tool input/output shape

Started: 2026-04-28 (immediately after Phase 0 EXIT `eed181ba`)
Closed: 2026-04-28 (commits `aee5e5d5` topology + `fa23a376` tool); Phase 2 pending user go

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 1.
> EXIT: types compile + lint clean, tests pin enum values, tracker committed.
> Risk classification: LOW (pure additive vocabulary).

## Plan reference

- Plan SHA at session start: `eed181ba` (Phase 0 EXIT) → `180c13c7` (tracker backfill)
- Plan file: `plans/export-buildfromgit-2026-04-28.md`
- Sister plan EXIT: `plans/archive/strategy-decomp/phase-1-tracker.md` (precursor topology types)

## Pre-Phase-1 reality check (Claude-side)

Plan §6 Phase 1 step 2 lists three `WorkflowInput` fields to add: `TargetService`, `Variant`, `EnvClassifications`. Verified against current `internal/tools/workflow.go:86-90`:

- **`TargetService`** — ALREADY EXISTS (used by `action="adopt-local"` and `action="record-deploy"`). Phase 1 work is description extension, not addition. Plan-cited spec drift; folded into commit message.
- **`Variant`** — does NOT exist. New field.
- **`EnvClassifications`** — does NOT exist. New field.

No parser-extension Phase 1.0 needed (deferred from sister plan). All three new front-matter axes (`closeDeployModes`, `gitPushStates`, `buildIntegrations`) are already wired in `internal/workflow/atom.go:131-133`, per Phase 0 sanity check.

## TDD discipline

RED → GREEN sequence on the topology enum tests:

| step | command | result |
|---|---|---|
| RED | Add `TestExportVariantValues` + `TestSecretClassificationValues` to `internal/topology/types_test.go` referencing symbols that don't exist; run `go test` | FAIL — undefined symbols (build failed). RED confirmed. |
| GREEN | Add `ExportVariant` + `SecretClassification` types + constants to `internal/topology/types.go` with full doc comments; rerun tests | `ok internal/topology 0.216s` |
| REFACTOR | (none — pure additive) | n/a |

WorkflowInput field additions don't have a separate test layer in `internal/tools/` for input shape — fields are exercised through handler tests in Phase 3. Build + full short suite + race confirm no breakage.

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues; recipe_atom_lint 0 violations; atom-template-vars 0 unbound |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test ./internal/{topology,tools,workflow,content} -short -race -count=1` | all PASS |

## Sub-pass work units

| # | sub-pass | initial state | final state | commit |
|---|---|---|---|---|
| 1 | reality check WorkflowInput field landscape | unverified | DONE — `TargetService` exists; `Variant` + `EnvClassifications` are new | n/a |
| 2 | RED test for new enum types | absent | DONE — undefined-symbol failure confirmed | (squashed into commit 4) |
| 3 | GREEN — add `ExportVariant` + `SecretClassification` to `internal/topology/types.go` with full doc comments | absent | DONE — types compile + tests pass | `aee5e5d5` |
| 4 | tests pin closed-set + empty-string sentinel invariants | absent | DONE — `TestExportVariantValues`, `TestSecretClassificationValues` | `aee5e5d5` |
| 5 | extend `WorkflowInput.TargetService` jsonschema description (export consumer added) | adopt-local + record-deploy only | DONE — export consumer mentioned | `fa23a376` |
| 6 | add `WorkflowInput.Variant` field | absent | DONE — typed string with full jsonschema | `fa23a376` |
| 7 | add `WorkflowInput.EnvClassifications` field | absent | DONE — `map[string]string` with bucket-value enumeration in jsonschema | `fa23a376` |
| 8 | run verify gate | unverified | DONE — lint-local + race PASS | n/a |
| 9 | split commits by logical concern | (drafted) | DONE — topology vocabulary separate from tool entry-point shape | `aee5e5d5` + `fa23a376` |

## Phase 1 EXIT (§6)

- [x] Types compile + lint clean (`make lint-local` 0 issues).
- [x] Tests pin enum values (closed-set + sentinel invariants).
- [x] `phase-1-tracker.md` committed (this commit).
- [x] Verify gate green at commit time.
- [ ] User explicit go to enter Phase 2 (per session pause instruction extending into Phase 1).

## §15.2 EXIT enforcement

- [x] Every sub-pass row above has non-empty final state.
- [x] Action-taking rows cite a commit hash.
- [x] No Codex round was mandatory for Phase 1 (LOW risk, pure additive — per plan §7 Codex protocol table).
- [x] `Closed:` 2026-04-28.

## Notes for Phase 2 entry

1. Phase 2 is MEDIUM risk: new file `internal/ops/export_bundle.go`, ~600-900 LOC of YAML composition + classification application + zerops.yaml verification.
2. Phase 2 GATE: `ops.ExportBundle` struct must exist before Phase 4 atoms can declare `references-fields: [ops.ExportBundle.*]`. `TestAtomReferenceFieldIntegrity` at `internal/workflow/atom_reference_field_integrity_test.go:17-57` enforces; Phase 0 amendment 8 pinned this.
3. Mandatory Codex POST-WORK round per plan §7 Codex protocol table (generator behavior, YAML composition edge cases on Laravel / Node / static / PHP shapes).
4. The `composeImportYAML` function consumes `topology.ExportVariant` (mapping dev → mode=dev, stage → mode=simple per §3.3 decision Q7-β). The `composeServiceEnvVariables` function consumes `map[string]topology.SecretClassification` and emits per-bucket directives.
5. Session pause point: Phase 2 begins ONLY after explicit user go.