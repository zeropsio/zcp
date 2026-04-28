# Phase 1 tracker — Topology types + atom parser axes

Started: 2026-04-28
Closed: 2026-04-28 (Phase 1 EXIT commits `b4d2929a` + `c300d509`); Phase 2 pending user go

> Phase contract per `plans/deploy-strategy-decomposition-2026-04-28.md`
> §6 Phase 1. Two-commit phase split for clarity per plan §6 step 6:
> topology vocabulary first, atom parser axes second. Risk classification:
> LOW (pure addition; old types coexist for migration).

## Plan reference

- Plan SHA at Phase 1 entry: `7de87c4d` (post Phase 0 backfill)
- Phase 1.A (topology) commit: `b4d2929a`
- Phase 1.B (parser axes) commit: `c300d509`
- Phase 0 EXIT inheritance: `9f2b2203` (calibration + PRE-WORK Codex round)

## Phase 1.A — topology types + IsPushSource predicate

Pure addition to `internal/topology/`. Three enums + one predicate + truth-table tests.

### Type additions (types.go)

| type | values | doc anchor |
|---|---|---|
| `CloseDeployMode` | `unset`, `auto`, `git-push`, `manual` | per-pair developer choice for what develop workflow auto-does at close |
| `GitPushState` | `unconfigured`, `configured`, `broken`, `unknown` | per-pair record of whether git-push capability is set up |
| `BuildIntegration` | `none`, `webhook`, `actions` | per-pair record of which ZCP-managed CI integration responds to git pushes |

Each type carries doc-comments referencing the plan §3.1 orthogonality matrix. `BuildIntegration` doc explicitly states UTILITY framing — `BuildIntegrationNone` does NOT mean "no build will fire", only "no ZCP-managed integration is configured" (users may still have independent CI/CD that ZCP doesn't track). This framing pre-empts atom-corpus drift in Phase 8.

### Predicate addition (predicates.go)

`IsPushSource(Mode) bool` — true for `ModeStandard`, `ModeSimple`, `ModeLocalStage`, `ModeLocalOnly`; false for `ModeStage` (build target) and `ModeDev` (legacy dev-only mode, invalid combo with push-git per the decomposition).

Switch lists every named Mode value explicitly to satisfy the `exhaustive` linter — same pattern as `modeEligibleForSubdomain` at `internal/tools/deploy_subdomain.go:113-125`. Pre-commit hook initially blocked the commit because the original switch only listed the `true` cases; explicit `case ModeDev, ModeStage: return false` resolved the lint.

### Tests pinned (types_test.go)

| test | what it pins | rows |
|---|---|---|
| `TestIsPushSource` | predicate truth table covering every Mode value with rationale per row | 6 |
| `TestCloseDeployModeValues` | closed-set pin (4 distinct values) — typo introducing new value fails build | 1 |
| `TestGitPushStateValues` | closed-set pin (4 distinct values) | 1 |
| `TestBuildIntegrationValues` | closed-set pin (3 distinct values) | 1 |

### doc.go

Updated package overview to list the new vocabulary (CloseDeployMode / GitPushState / BuildIntegration / IsPushSource) and pointer to the plan for the orthogonality matrix and migration schedule.

### Verify gate (Phase 1.A)

- `make lint-fast`: 0 issues after exhaustive-switch fix
- `go test ./... -short -count=1`: ALL packages PASS

## Phase 1.B — atom parser axes

Pure addition to `internal/workflow/atom.go`, `internal/workflow/synthesize.go`, `internal/content/atoms_lint.go`. Per Codex PRE-WORK Bonus 2 finding: `validAtomFrontmatterKeys` is closed (`atom.go:108-135`) and `validateAtomFrontmatter` rejects unknown keys (`atom.go:250-266`); without parser support, Phase 8 atom corpus restructure would fail every atom declaring the new axes.

### Parser changes (atom.go)

| change | effect |
|---|---|
| AxisVector grows 3 slice fields | `CloseDeployModes []topology.CloseDeployMode`, `GitPushStates []topology.GitPushState`, `BuildIntegrations []topology.BuildIntegration` |
| `validAtomFrontmatterKeys` adds 3 keys | `closeDeployModes`, `gitPushStates`, `buildIntegrations` |
| `listAxisKeys` adds 3 keys | enforces inline-list form for the new axes |
| `validAtomEnumValues` adds 3 enum sets | typo in axis value fails ParseAtom rather than silently degrading |
| `validateAtomFrontmatter` error message extended | unknown-key error lists the full closed set so offender sees valid keys directly |
| `ParseAtom` AxisVector init extended | wires `parseCloseDeployModes` / `parseGitPushStates` / `parseBuildIntegrations` |
| 3 new parser helpers | follow `parseModes` / `parseTriggers` shape (parseYAMLList → typed slice) |

### Synthesize wiring (synthesize.go)

`hasServiceScopedAxes` updated to count `CloseDeployModes`, `GitPushStates`, `BuildIntegrations` as service-scoped axes — atoms declaring them are correctly classified as per-service.

The matching logic in `serviceSatisfiesAxes` is **deferred to Phase 3**. ServiceSnapshot has no `CloseDeployMode` / `GitPushState` / `BuildIntegration` field at this point in the migration, and no atom in the corpus declares the axes yet, so the filter gap is inert during Phases 1–7. Phase 3 is responsible for closing the gap when ServiceSnapshot grows the fields.

### Atom-lint hook stubs (atoms_lint.go)

Three Phase-1 stub functions wired into `lintAtomCorpus`:

- `closeDeployModeViolations` — Phase 8 candidate: enforce `closeDeployModes: [manual]` atoms MUST NOT contain `zerops_deploy` invocations (spec D7)
- `gitPushStateViolations` — Phase 8 candidate
- `buildIntegrationViolations` — Phase 8 candidate: enforce UTILITY framing ("ZCP-managed integration", not "CI/CD"); warn if "no build will fire" appears alongside `buildIntegrations: [none]` (since users may have independent CI)

All three currently return `nil`. Per plan §6 Phase 1 EXIT, "rules can be empty stubs initially". Wiring is in place so Phase 8 rules go inside these functions rather than scattering across the lint scaffold.

### Tests pinned (atom_test.go)

| test | cases | what it pins |
|---|---|---|
| `TestParseAtom_DeployDecompAxes` | 11 | each axis on its own (positive parse), all three combined, full-enum round-trip per axis, invalid-value rejection per axis, bare-scalar rejection |

Plus: `slicesEqual[T ~string]` generic helper introduced so the typed-string-slice comparisons stay readable. Existing `equalPhases` / `equalModes` etc. left as-is per CLAUDE.md "don't refactor beyond what the task requires."

### Verify gate (Phase 1.B)

- `gofmt`: 2 issues post-edit on atom.go and atom_test.go (struct-field alignment after CloseDeployModes/etc. insertion); auto-fixed via `gofmt -w`
- `make lint-fast`: 0 issues
- `go test ./... -short -count=1`: ALL packages PASS, including 11/11 `TestParseAtom_DeployDecompAxes` cases

## Phase 1 EXIT (§6)

- [x] New topology types compile (`internal/topology/` builds clean).
- [x] IsPushSource truth table pinned (`TestIsPushSource`, 6 rows).
- [x] Old types still present (zero churn elsewhere; baseline-callsites.txt 281-hit set unchanged).
- [x] Atom parser accepts new frontmatter axis keys without rejecting valid atoms (`TestParseAtom_DeployDecompAxes` 11/11 PASS).
- [x] Atom-lint hooks for new axes present (3 stub functions, wired, currently return nil).
- [x] `phase-1-tracker.md` committed (this commit).
- [x] Verify gate green at commit time (lint-fast clean; full short test suite PASS).

## §15.2 EXIT enforcement (inherited schema)

- [x] Every sub-pass row above has non-empty final state.
- [x] Every row that took action cites a commit hash.
- [x] Codex round outcome: NONE required (LOW risk per plan §7 — no Codex round mandatory in Phase 1).
- [x] `Closed:` 2026-04-28.

## Sub-pass work units

| # | sub-pass | initial state | final state | commit | notes |
|---|---|---|---|---|---|
| 1 | extend `internal/topology/types.go` with CloseDeployMode, GitPushState, BuildIntegration | absent | DONE | `b4d2929a` | doc-comments cite plan §3.1 |
| 2 | add IsPushSource(Mode) to predicates.go | absent | DONE | `b4d2929a` | exhaustive switch covers all 6 Mode values |
| 3 | extend types_test.go with truth-table tests | absent | DONE | `b4d2929a` | 4 new test functions, all PASS |
| 4 | update doc.go with new vocabulary | partial | DONE | `b4d2929a` | references plan + orthogonality matrix |
| 5 | extend AxisVector struct | absent | DONE | `c300d509` | 3 new fields with Phase-3-deferral doc |
| 6 | extend validAtomFrontmatterKeys + listAxisKeys + validAtomEnumValues | absent | DONE | `c300d509` | 3 keys, 3 enum sets |
| 7 | update validateAtomFrontmatter error message | stale | DONE | `c300d509` | lists full closed set |
| 8 | wire parsers into ParseAtom AxisVector init | absent | DONE | `c300d509` | parseCloseDeployModes etc. |
| 9 | add 3 parser helpers | absent | DONE | `c300d509` | follow parseModes/parseTriggers shape |
| 10 | update hasServiceScopedAxes in synthesize.go | partial | DONE | `c300d509` | new axes counted; serviceSatisfiesAxes deferred to Phase 3 |
| 11 | add 3 lint-hook stubs in atoms_lint.go | absent | DONE | `c300d509` | wired into lintAtomCorpus, return nil per plan |
| 12 | add TestParseAtom_DeployDecompAxes | absent | DONE | `c300d509` | 11 cases including invalid-enum + bare-scalar rejection |
| 13 | gofmt re-align after struct-field insertion | unformatted | DONE | `c300d509` | atom.go + atom_test.go |
| 14 | verify gate (lint-fast + go test ./... -short) | green pre-changes | DONE — 0 lint issues, all packages PASS | `c300d509` | covers both Phase 1.A and Phase 1.B |
| 15 | author phase-1-tracker.md + commit | absent | DONE | (this commit) | bookkeeping closes Phase 1 |

## Notes for Phase 2 entry

1. Phase 2 introduces the meta migration (`migrateOldMeta`). Risk classification: MEDIUM (load-bearing). Codex POST-WORK round MANDATORY per §7.
2. Per plan §6 Phase 2 step 3 + §10 anti-pattern: `migrateOldMeta` MUST be hooked into `parseMeta` (single deserialization point per `service_meta.go:218-220` doc-comment), not just `ReadServiceMeta`. `ListServiceMetas` calls `parseMeta` directly; router/envelope use `ManagedRuntimeIndex` over the list. Hooking only `ReadServiceMeta` would silently break router/envelope state.
3. `ServiceMeta` migration truth table (4 × 3 = 12 combinations of DeployStrategy × PushGitTrigger) must be pinned in `service_meta_test.go` per Phase 2 step 5.
4. Old `DeployStrategy` / `PushGitTrigger` / `StrategyConfirmed` fields stay during the migration window (deleted in Phase 10). New fields land alongside.
5. `bootstrap_outputs.go` writes new defaults (Phase 2 step 4); existing `bootstrap_outputs_test.go:424` assertion will need updating.
6. ServiceSnapshot field additions and serviceSatisfiesAxes wiring for the new axes land in Phase 3, not Phase 2 — Phase 2 is purely meta layer.
7. `FirstDeployedAt` semantic stays unchanged ("any successful deploy attempt"); plan §6 Phase 2 step 1 explicitly retains it.
