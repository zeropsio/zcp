# Phase 2 tracker — ServiceMeta migration

Started: 2026-04-28
Closed: 2026-04-28 (Phase 2 EXIT commit `2a1a890b` + tracker EXIT, this commit); Phase 3 pending user go

> Phase contract per `plans/deploy-strategy-decomposition-2026-04-28.md`
> §6 Phase 2. Risk classification: MEDIUM (load-bearing migration). Codex
> POST-WORK round MANDATORY per plan §7.

## Plan reference

- Plan SHA at Phase 2 entry: `df32b811` (post Phase 1 close)
- Phase 2 EXIT commit: `2a1a890b` (`meta(P2): add CloseDeployMode/GitPushState/BuildIntegration + migrateOldMeta`)
- Phase 1 EXIT inheritance: `b4d2929a` + `c300d509` + `df32b811`

## Implementation summary

### ServiceMeta struct extension (service_meta.go)

5 new fields in a labelled block, sandwiched between identity (Hostname / Mode / StageHostname) and the now-deprecated legacy strategy fields:

| field | type | purpose |
|---|---|---|
| `CloseDeployMode` | `topology.CloseDeployMode` | what develop workflow auto-does at close |
| `CloseDeployModeConfirmed` | `bool` | twin of legacy `StrategyConfirmed` |
| `GitPushState` | `topology.GitPushState` | per-pair record of git-push capability |
| `RemoteURL` | `string` | cache for remote URL; runtime source = git origin |
| `BuildIntegration` | `topology.BuildIntegration` | which ZCP-managed CI integration responds on remote push |

Legacy `DeployStrategy` / `PushGitTrigger` / `StrategyConfirmed` are PRESERVED on the struct through Phase 9; Phase 10 deletes them post-migrate-cycle.

### migrateOldMeta + parseMeta hook (service_meta.go)

`migrateOldMeta(meta *ServiceMeta)` runs from `parseMeta` on every read. Mapping per plan §3.4 Scenario F:

| legacy | new | guard |
|---|---|---|
| DeployStrategy ∈ {"", "unset"} | CloseDeployMode = unset | only when CloseDeployMode is empty |
| DeployStrategy = "push-dev" | CloseDeployMode = auto | only when CloseDeployMode is empty |
| DeployStrategy = "push-git" | CloseDeployMode = git-push | only when CloseDeployMode is empty |
| DeployStrategy = "manual" | CloseDeployMode = manual | only when CloseDeployMode is empty |
| PushGitTrigger ∈ {"", "unset"} | BuildIntegration = none | only when BuildIntegration is empty |
| PushGitTrigger = "webhook" | BuildIntegration = webhook | only when BuildIntegration is empty |
| PushGitTrigger = "actions" | BuildIntegration = actions | only when BuildIntegration is empty |
| StrategyConfirmed = true | CloseDeployModeConfirmed = true | only when not already true |
| push-git + FirstDeployedAt set | GitPushState = configured | only when GitPushState is empty |
| push-git + no FirstDeployedAt | GitPushState = unknown | only when GitPushState is empty |
| else | GitPushState = unconfigured | only when GitPushState is empty |

`RemoteURL` stays empty on migration (data lost; fills on next push or probe).

`migrateOldMeta` is **idempotent** (every branch guards on "new field is empty") and **nil-safe** (returns early on nil pointer). The single integration point is `parseMeta` — used by `ReadServiceMeta`, `ListServiceMetas`, and `FindServiceMeta`. Per Codex PRE-WORK Bonus surfacing, hooking only `ReadServiceMeta` would leave router/envelope (which uses `ManagedRuntimeIndex` over `ListServiceMetas`) seeing un-migrated metas.

### Bootstrap defaults (bootstrap_outputs.go)

- `writeBootstrapOutputs` writes `CloseDeployMode: CloseModeUnset, GitPushState: GitPushUnconfigured, BuildIntegration: BuildIntegrationNone` explicitly.
- `writeProvisionMetas` writes the same defaults on the partial-meta path.
- `mergeExistingMeta` preserves all 5 new fields PLUS the 3 legacy fields during expansion-merge.

### Tests pinned

| test | location | rows | what it pins |
|---|---|---|---|
| `TestMigrateOldMeta` | service_meta_test.go | 18 | 4×3 truth table + GitPushState heuristic + StrategyConfirmed propagation + typed StrategyUnset/TriggerUnset |
| `TestMigrateOldMeta_Idempotent` | service_meta_test.go | 1 | running migrate twice is no-op |
| `TestMigrateOldMeta_DoesNotOverwriteExplicitNewFields` | service_meta_test.go | 1 | explicit user writes survive migrate |
| `TestMigrateOldMeta_NilSafe` | service_meta_test.go | 1 | nil pointer doesn't panic |
| `TestParseMeta_AppliesMigration` | service_meta_test.go | 1 | parseMeta integration runs migrate |
| `TestMergeExistingMeta` (extended) | bootstrap_outputs_test.go | 1 | new fields preserved through expansion |
| `TestWriteBootstrapOutputs_WritesDeployDecompDefaults` | bootstrap_outputs_test.go | 1 | fresh bootstrap writes Phase 2 defaults |
| `TestWriteBootstrapOutputs_ExpansionPreservesExistingFields` (extended) | bootstrap_outputs_test.go | 1 | migrated values + StrategyConfirmed propagation survive expansion |

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 2 POST-WORK migration review | POST-WORK | DONE — APPROVE, zero amendments | `codex-round-p2-postwork.md` | TBD (this Phase 2 EXIT commit) |

### POST-WORK round outcome (2026-04-28)

- **Q1 (parseMeta hook coverage)**: PASS. Every read path (`ReadServiceMeta` / `ListServiceMetas` / `FindServiceMeta`) routes through `parseMeta` → `migrateOldMeta`. No bypass via inline `json.Unmarshal` for ServiceMeta. Recipe-meta unmarshals in `recipe_meta.go` target a different type.
- **Q2 (truth table completeness)**: PASS. All 12 (DeployStrategy × PushGitTrigger) combinations + 6 extra dimension rows (GitPushState heuristic, StrategyConfirmed propagation, typed StrategyUnset / TriggerUnset) covered with file:line citations in `service_meta_test.go`.
- **Q3 (idempotency + non-overwrite)**: PASS. Every branch guard verified (nil-safe, CloseDeployMode == "", false→true CloseDeployModeConfirmed propagation, BuildIntegration == "", GitPushState == ""). `TestMigrateOldMeta_Idempotent` and `TestMigrateOldMeta_DoesNotOverwriteExplicitNewFields` both anchored.
- **Q4 (GitPushState heuristic)**: PASS. Edge cases inspected — typed `StrategyUnset` falls through to default; `DeployStrategy=""` with `FirstDeployedAt` set yields `GitPushUnconfigured` (no false positive); push-git + trigger unset + no FirstDeployedAt → `GitPushUnknown` correct.
- **Q5 (bootstrap defaults + merge carry-through)**: PASS. `writeBootstrapOutputs` + `writeProvisionMetas` write new defaults; `mergeExistingMeta` preserves 5 new + 3 legacy fields. Pre-Phase-2 legacy meta trace produces correctly-migrated upgraded meta.
- **Q6 (cross-cutting)**: PASS. No `json.Unmarshal.*ServiceMeta` bypass; no direct `os.WriteFile` on marshaled meta; existing legacy-field tests still valid; `migrateOldMeta(nil)` purely defensive (no production reach); JSON field ordering not relied upon by any consumer.

### Codex's note + final local gate

Codex's validation environment couldn't execute `go test` (sandbox restriction on go-build temp dir). The verdict is based on static code + test inspection only. Codex recommended running tests locally before marking Phase 2 closed.

Final local gate executed before Phase 2 EXIT commit:
```
go test ./internal/workflow \
  -run "TestMigrateOldMeta|TestParseMeta_AppliesMigration|TestMergeExistingMeta|TestWriteBootstrapOutputs_WritesDeployDecompDefaults|TestWriteBootstrapOutputs_ExpansionPreservesExistingFields" \
  -count=1 -race
```
Result: PASS (1.518s, race detector clean).

Per §5 Phase 2 EXIT contract: APPROVE → proceed to Phase 3 on user go.

## Sub-pass work units

| # | sub-pass | initial state | final state | commit | notes |
|---|---|---|---|---|---|
| 1 | extend ServiceMeta with 5 new fields + reorder block-style | unset | DONE | `2a1a890b` | identity / new dimensions / legacy / lifecycle blocks |
| 2 | implement migrateOldMeta (mapping + guards) | absent | DONE | `2a1a890b` | idempotent; nil-safe |
| 3 | hook migrateOldMeta into parseMeta | absent | DONE | `2a1a890b` | doc-comment cites Codex PRE-WORK surfacing |
| 4 | writeBootstrapOutputs writes Phase 2 defaults | only legacy | DONE | `2a1a890b` | CloseModeUnset / GitPushUnconfigured / BuildIntegrationNone |
| 5 | writeProvisionMetas writes Phase 2 defaults | only legacy | DONE | `2a1a890b` | symmetric with writeBootstrapOutputs |
| 6 | mergeExistingMeta preserves new fields | only legacy | DONE | `2a1a890b` | 5 new + 3 legacy preserved |
| 7 | TestMigrateOldMeta truth table (18 rows) | absent | DONE | `2a1a890b` | covers 4×3 + heuristics + propagation + typed sentinels |
| 8 | TestMigrateOldMeta_Idempotent + non-overwrite + nil-safe | absent | DONE | `2a1a890b` | three sibling guards |
| 9 | TestParseMeta_AppliesMigration | absent | DONE | `2a1a890b` | confirms parseMeta integration runs migrate |
| 10 | TestMergeExistingMeta extended | partial | DONE | `2a1a890b` | now seeds + asserts all 5 new fields |
| 11 | TestWriteBootstrapOutputs_WritesDeployDecompDefaults | absent | DONE | `2a1a890b` | fresh-bootstrap writes Phase 2 defaults |
| 12 | TestWriteBootstrapOutputs_ExpansionPreservesExistingFields extended | partial | DONE | `2a1a890b` | migrated values survive expansion-merge |
| 13 | gofmt re-align after struct insert | unformatted | DONE | `2a1a890b` | bootstrap_outputs_test.go + service_meta_test.go |
| 14 | verify gate (lint-fast + go test ./... -short -count=1) | green pre-changes | DONE — 0 lint issues, all packages PASS | `2a1a890b` | including 18 truth-table rows + 4 sibling guards |
| 15 | Phase 2 POST-WORK Codex round | not run | DONE — APPROVE (zero amendments) | TBD (this commit) | output captured in `codex-round-p2-postwork.md` |
| 16 | apply amendments (if NEEDS-REVISION) | n/a | N/A — APPROVE, no amendments | n/a | Codex returned PASS on every question |
| 17 | final local gate per Codex note (-race) | unrun | DONE — PASS in 1.518s, race-detector clean | TBD (this commit) | mitigates Codex sandbox restriction |
| 18 | author phase-2-tracker.md + commit | absent | DONE | TBD (this commit) | bookkeeping closes Phase 2 |

## Phase 2 EXIT (§6)

- [x] New fields persist + load correctly (TestParseMeta_AppliesMigration + TestWriteBootstrapOutputs_WritesDeployDecompDefaults).
- [x] migrateOldMeta truth table pinned (18-row TestMigrateOldMeta + 4 sibling guards).
- [x] All existing tests pass with auto-migration (full short test suite green; legacy field assertions intact).
- [x] Bootstrap writes new defaults (TestWriteBootstrapOutputs_WritesDeployDecompDefaults).
- [x] Codex POST-WORK APPROVE (zero amendments; final local gate PASS with -race).
- [x] `phase-2-tracker.md` committed (this commit).
- [x] Verify gate green at commit time (lint-fast + targeted go test -race PASS).
- [ ] User explicit go to enter Phase 3 (per session discipline).

## §15.2 EXIT enforcement (inherited schema)

- [x] Every sub-pass row above has non-empty final state.
- [x] Every row that took action cites a commit hash (rows 1-14 → `2a1a890b`; rows 15-18 → this commit).
- [x] Codex round outcome cited (`codex-round-p2-postwork.md` + summary in §Codex rounds).
- [x] `Closed:` 2026-04-28.

## Notes for Phase 3 entry

1. Phase 3 (envelope + router) is MEDIUM risk per plan §6. Codex POST-WORK round MANDATORY per §7. Two-snapshot fixture for `develop-active/git-push/standard/container` is the §G5 ship-gate regression guard — must pin single-render with dev hostname.
2. Phase 3 must wire `serviceSatisfiesAxes` in `internal/workflow/synthesize.go` for the new axes (`CloseDeployModes`, `GitPushStates`, `BuildIntegrations`). Phase 1 only wired `hasServiceScopedAxes`. Wiring needs ServiceSnapshot to grow `CloseDeployMode` / `GitPushState` / `BuildIntegration` / `RemoteURL` fields first.
3. `compute_envelope.go::buildOneSnapshot` reads new fields from meta and stamps the snapshot. Existing `Strategy` / `Trigger` fields stay on ServiceSnapshot through Phase 10.
4. `router.go::Route` updates dominant-strategy detection (line ~169) to use CloseDeployMode.
5. Two-snapshot fixture in `scenarios_test.go` (currently single-snapshot at `develop-active/push-git/standard/container` line 820) — extend to dev+stage pair so future double-render bugs are caught.
6. `synthesize.go::SynthesizeStrategySetup` may need axis-match update — check Phase 3 plan section.
