# Phase 5 tracker — Schema validation pass

Started: 2026-04-29 (immediately after Phase 4 EXIT `45586751`)
Closed: TBD (pending Codex POST-WORK round + user go for Phase 6)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 5.
> EXIT: validators compile + tests pass, wired into BuildBundle, embedded schemas refreshed,
> Codex POST-WORK APPROVE on validation correctness.
> Risk classification: MEDIUM (Phase 0 amendment §14 raised from LOW-MEDIUM due to no JSON Schema lib vendored).

## Plan reference

- Plan SHA at session start: `45586751`
- Phase 0 amendments folded: §13 (specifically amendment 10 — vendor github.com/santhosh-tekuri/jsonschema/v5).
- Phase 4 atoms in place — `export-validate.md` references-fields includes `ops.ExportBundle.Warnings`; needs to also include `ops.ExportBundle.Errors` (added in Phase 5).

## Library choice

`github.com/santhosh-tekuri/jsonschema/v5 v5.3.1` — mature, draft-2020-12 compliant, no cgo, BSD-2.

## Coding deltas (Phase 5 EXIT)

- `internal/schema/validate_jsonschema.go` (~200 LOC, NEW): `ValidationError` type, `ValidateImportYAML(content)`, `ValidateZeropsYAML(content, requiredSetup)`, embedded schemas via `embed.FS`, leaf-flattening of jsonschema.ValidationError trees.
- `internal/schema/validate_jsonschema_test.go` (~150 LOC, NEW): 9 test functions covering happy path, empty content, malformed YAML, missing required field, missing setup, preprocessor header tolerance, deterministic output, embedded schema present.
- `internal/schema/testdata/import_yml_schema.json` REFRESHED from live (was 202B behind).
- `internal/ops/export_bundle.go`:
  - `ExportBundle.Errors []schema.ValidationError` field added.
  - `BuildBundle` calls validators after composition; populates `Errors`.
  - `mapImportMode` removed; `runtimeImportMode` returns "NON_HA" (platform scaling enum). Plan §3.3 (β) topology mapping was wrong against real schema.
  - `importModeNonHA` constant replaces `importModeSimple`.
- `internal/ops/export_bundle_test.go`: `TestRuntimeImportMode` replaces `TestMapImportMode`; mode assertions in fixtures updated to "NON_HA".
- `internal/tools/workflow_export.go`:
  - `validationFailedResponse` returns `status="validation-failed"` body when bundle.Errors is non-empty.
  - Handler branches: `if len(bundle.Errors) > 0 → validation-failed; else → publish-ready`.
  - `bundlePreview` exposes `errors` array to agent.
  - `formatBundleErrors` renders `[]schema.ValidationError` to JSON shape.

## Atom prose updates

Three atoms touched to reflect Phase 5 reality:

- `export-classify-envs.md` — replaced "Forward-compat note: schema validation lands in Phase 5" with current "Schema validation" section that describes `bundle.errors` flow.
- `export-intro.md` — replaced `mode: dev`/`mode: simple` table with HA/NON_HA reality + topology-determined-by-bootstrap explanation.
- `export-validate.md` — replaced "not yet client-side" prose with `bundle.errors` flow + spot-check list aligned to NON_HA + dropped now-unneeded axis-k-keep marker.

Atom corpus total raw bytes: 28,719B (over 28,672B raw cap by 47B). `TestCorpusCoverage_OutputUnderMCPCap` PASSES — synthesized output strips frontmatter so the rendered total fits under the soft cap.

## Plan §3.3 reality-check

Plan §3.3 (β) declared `mode: dev` / `mode: simple` / `mode: local-only` post-import mappings. The published platform schema enforces `mode: HA` / `NON_HA` only — the plan author conflated topology.Mode (ZCP-internal: dev/standard/stage/simple/local-stage/local-only) with the platform's scaling-mode enum.

Phase 5 fix: emit `mode: NON_HA` for the runtime entry (single-runtime bundles can't justify HA without explicit scaling fields). The destination project's topology Mode is established by ZCP's bootstrap on import — the bundle does NOT embed topology hints.

§3.3 amendment recorded in plan §15 (Phase 5 retrospective) below.

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test ./... -short -race -count=1` | all packages PASS |
| schema validation tests | `go test ./internal/schema/ -count=1` | PASS |
| bundle integration | `go test ./internal/ops/ -count=1` | PASS — TestBuildBundle_* exercises validators |
| handler integration | `go test ./internal/tools/ -count=1` | PASS — publish-ready / classify-prompt branches still pin |

## Codex rounds

| agent | scope | output target | status |
|---|---|---|---|
| POST-WORK | lib choice + HA/NON_HA structural fix + plan §3.3 amendment + validator correctness + testdata refresh + Errors propagation + atom alignment + test coverage gaps + compaction + legacy-validator deprecation | `codex-round-p5-postwork-schema.md` | running |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| POST-WORK | lib choice + HA/NON_HA structural fix + plan §3.3 amendment + validator correctness + testdata refresh + Errors propagation + atom alignment + test coverage gaps + branch order + legacy-validator deprecation | ~213s | APPROVE-with-amendments (5 actionable) |

**Effective verdict**: APPROVE after 6 in-place amendments folded.

### Amendments applied (6 total)

1. **Plan §3.3 + Q7 + Phase 2 step 3 + anti-pattern** rewritten to reflect HA/NON_HA reality. The `mode: dev`/`mode: simple`/`mode: local-only` table replaced with `NON_HA`-everywhere row table. Topology context preserved on `bundle.variant` + `bundle.targetHostname` + `bundle.setupName`.

2. **`variantPromptResponse` prose fixed** — workflow_export.go:249-252 stale "re-imports as mode=dev/mode=simple" replaced with "packages dev/stage hostname's working tree; emit Zerops scaling mode=NON_HA; topology established by ZCP bootstrap on import".

3. **`ops.ExportBundle.Errors` added to `references-fields`** in `export-validate.md` so `TestAtomReferenceFieldIntegrity` AST-pin protects the new field.

4. **Branch order fixed**: validation-failed now outranks git-push-setup-required. A schema-invalid bundle would fail at re-import even after setup completes — surfacing the publish prereq first would mask the real blocker. The git-push-setup chain still includes `preview.errors` via bundlePreview, so the agent doesn't lose visibility on validation issues while resolving setup.

5. **`TestHandleExport_ValidationFailed`** added — feeds an invalid zerops.yaml (missing `setup:` in one entry), asserts `status="validation-failed"` + serialized errors + preview + nextSteps.

6. **`TestHandleExport_ValidationOutranksGitPushSetup`** added — exercises the both-failures-present case and pins that validation wins over GitPushState chain.

### Codex notes (no action required)

- Raw atom corpus 47 bytes over the raw cap (28,719 vs 28,672) — synthesized output passes the executable gate. Will trim on next atom edit.
- `ValidateZeropsYmlRaw` legacy stays — distinct call site (`internal/ops/checks/yml_schema.go:21-32` + `internal/tools/workflow_checks_recipe.go:42-48` + `cmd/zcp/check/yml_schema.go:47-70`) for recipe checks; deprecation deferred until those are migrated to `ValidateZeropsYAML`.
- Testdata refresh cadence — manual signal (platform import rejects what client validator accepted). No automated drift detector yet.

## Phase 5 EXIT

- [x] JSON Schema lib vendored (`github.com/santhosh-tekuri/jsonschema/v5 v5.3.1`).
- [x] Validators compile + tests pass (9 functions covering edge cases).
- [x] Wired into `ops.BuildBundle`; populates `ExportBundle.Errors`.
- [x] Embedded testdata refreshed (matches live).
- [x] Handler branches on `bundle.Errors` to return `status="validation-failed"`.
- [x] Atom prose updated to reflect HA/NON_HA reality + `bundle.errors` flow.
- [x] Verify gate green.
- [x] Codex POST-WORK APPROVE-with-amendments (6 amendments folded → effective APPROVE).
- [x] Codex round transcript persisted (`codex-round-p5-postwork-schema.md`).
- [x] `phase-5-tracker.md` finalized.
- [x] Phase 5 EXIT commits: `c33245a6` (impl + tests + atoms + plan amendments) + `e63d5ddc` (tracker + Codex transcript).
- [ ] User explicit go to enter Phase 6.

## Notes for Phase 6 entry

1. Phase 6 is MEDIUM risk: `refreshRemoteURL` SSH-reads `git remote -v` and updates `meta.RemoteURL` cache. Mismatch warnings surface to bundle.
2. The handler currently reads live `git remote get-url origin` directly via `readGitRemoteURL` — no cache lookup. Phase 6 may add a Phase 6 helper that compares cache vs live and surfaces a Warning when they disagree.
3. After Phase 6, `export-publish-needs-setup.md`'s "stale RemoteURL" section's "manually re-run git-push-setup" advice should narrow to "intentional remote-URL changes only" — the auto-refresh handles drift.
4. Session pause point: Phase 6 begins ONLY after explicit user go.