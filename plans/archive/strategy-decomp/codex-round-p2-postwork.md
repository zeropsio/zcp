# Codex POST-WORK round — Phase 2 ServiceMeta migration

Date: 2026-04-28
Phase 2 commit reviewed: `2a1a890b` (`meta(P2): add CloseDeployMode/GitPushState/BuildIntegration + migrateOldMeta`)
Round duration: ~4m 5s (245,015 ms)
Round status: APPROVE — no amendments required.

## Round prompt summary

Codex was handed the Phase 2 migration changes and asked to validate against the plan §3.4 + §6 + §10 contracts. Six questions:

- **Q1**: parseMeta hook covers ALL read paths (no inline `json.Unmarshal` bypass).
- **Q2**: migrate truth table completeness (12 combinations + extras).
- **Q3**: migrate idempotency + non-overwrite contract enforced by every branch.
- **Q4**: GitPushState heuristic edge cases.
- **Q5**: Bootstrap defaults + mergeExistingMeta carry-through.
- **Q6**: Cross-cutting concerns (other ServiceMeta deserialization paths, JSON ordering, nil-safe reachability).

## Verdict

**APPROVE.** Every question PASS. No recommended amendments.

## Q1 — parseMeta hook coverage

| call site | location | result |
|---|---|---|
| `parseMeta` calls `migrateOldMeta` then returns | service_meta.go:248-254 | PASS |
| `ReadServiceMeta` → `parseMeta(data)` | service_meta.go:331, 339 | PASS |
| `ListServiceMetas` → `parseMeta(data)` | service_meta.go:363, 367 | PASS |
| `FindServiceMeta` → `ReadServiceMeta` then `ListServiceMetas` | service_meta.go:458, 461 | PASS (inherits coverage) |
| No other `internal/workflow/` path deserializes ServiceMeta JSON | grep — only one `json.Unmarshal(data, &meta)` exists | PASS |

`recipe_meta.go` has separate `meta` unmarshals — those target `RecipeMeta`, not `ServiceMeta`. Codex confirmed the type distinction.

## Q2 — Migrate truth table completeness

All 12 (DeployStrategy × PushGitTrigger) combinations covered in `TestMigrateOldMeta`:

| row name | service_meta_test.go line |
|---|---|
| empty_x_empty | 1038 |
| empty_x_webhook | 1039 |
| empty_x_actions | 1040 |
| pushdev_x_empty | 1041 |
| pushdev_x_webhook | 1042 |
| pushdev_x_actions | 1043 |
| pushgit_x_empty | 1044 |
| pushgit_x_webhook | 1045 |
| pushgit_x_actions | 1046 |
| manual_x_empty | 1047 |
| manual_x_webhook | 1048 |
| manual_x_actions | 1049 |

Extra dimensions also covered:
- GitPushState heuristic (push-git + FirstDeployedAt set) at L1052-1053
- StrategyConfirmed propagation at L1056-1057
- Typed `StrategyUnset` / `TriggerUnset` round-trip at L1060-1061

## Q3 — Idempotency + non-overwrite

Per-branch guard analysis:

| guard | location | result |
|---|---|---|
| nil pointer early-return | service_meta.go:284-286 | PASS |
| `CloseDeployMode == ""` | service_meta.go:288 | PASS |
| `!CloseDeployModeConfirmed && StrategyConfirmed` (false→true only) | service_meta.go:301-302 | PASS |
| `BuildIntegration == ""` | service_meta.go:304 | PASS |
| `GitPushState == ""` | service_meta.go:315 | PASS |
| `TestMigrateOldMeta_Idempotent` exists | service_meta_test.go:1112-1123 | PASS |
| `TestMigrateOldMeta_DoesNotOverwriteExplicitNewFields` exists | service_meta_test.go:1135-1156 | PASS |

## Q4 — GitPushState heuristic

| case | location | result |
|---|---|---|
| push-git + FirstDeployedAt set → GitPushConfigured | service_meta.go:317-318 | PASS |
| push-git + no FirstDeployedAt → GitPushUnknown | service_meta.go:319-320 | PASS |
| else → GitPushUnconfigured | service_meta.go:321-322 | PASS |

Edge cases inspected:
- `topology.StrategyUnset` ("unset" string, typed) falls through to default branches → `CloseModeUnset` + `GitPushUnconfigured`. Covered by `strategyUnset_typed` row.
- `DeployStrategy == ""` with `FirstDeployedAt` set correctly yields `GitPushUnconfigured` (the FirstDeployedAt-sensitive branch requires `DeployStrategy == StrategyPushGit`).
- `push-git` + trigger unset + no FirstDeployedAt → `CloseModeGitPush` + `BuildIntegrationNone` + `GitPushUnknown`.

No false positives.

## Q5 — Bootstrap defaults

| code path | location | result |
|---|---|---|
| `writeBootstrapOutputs` writes new defaults | bootstrap_outputs.go:46, 50-52 | PASS |
| `writeProvisionMetas` writes new defaults | bootstrap_outputs.go:106, 110-112 | PASS |
| `mergeExistingMeta` preserves 5 new fields | bootstrap_outputs.go:144-148 | PASS |
| `mergeExistingMeta` preserves 3 legacy fields | bootstrap_outputs.go:150-152 | PASS |

Trace result for legacy on-disk meta (pre-Phase 2 disk shape):
1. `ReadServiceMeta` → `parseMeta` → `migrateOldMeta` derives new fields from legacy
2. `mergeExistingMeta` copies migrated values into upgraded meta
3. `WriteServiceMeta` writes the merged shape with both new + legacy fields populated

The `TestWriteBootstrapOutputs_ExpansionPreservesExistingFields` extension (bootstrap_outputs_test.go:1177-1191) asserts this trace carries the migrated values.

## Q6 — Cross-cutting concerns

| concern | result |
|---|---|
| No `json.Unmarshal.*ServiceMeta` bypass | PASS — only call site is service_meta.go:249-250 |
| No direct `os.WriteFile` on marshaled ServiceMeta | PASS — other os.WriteFile calls target recipe staged/generated files |
| Existing legacy-field tests still compile and assert correctly | PASS — migration preserves all 3 legacy fields; existing assertions intact |
| `migrateOldMeta(nil)` reachable from production? | NO — purely defensive; parseMeta always passes `&meta`. Pinned by `TestMigrateOldMeta_NilSafe` |
| JSON serialization order concern? | PASS — no consumer depends on field ordering; tests unmarshal into structs and assert named fields |

## Notes from Codex

The validation environment couldn't execute `go test` (sandbox restriction on go-build temp dir). The verdict is based on static code + test inspection only. Codex recommended running `go test ./internal/workflow/... -run TestMigrateOldMeta` locally before marking Phase 2 closed.

**Local run completed before Phase 2 EXIT close**: `go test ./internal/workflow -run "TestMigrateOldMeta|TestParseMeta_AppliesMigration|TestMergeExistingMeta|TestWriteBootstrapOutputs_WritesDeployDecompDefaults|TestWriteBootstrapOutputs_ExpansionPreservesExistingFields" -count=1 -race` → PASS in 1.518s. Race detector clean. Final gate green.

## Convergence

APPROVE — Phase 2 EXITs cleanly per §5 contract. No amendments. Phase 3 (envelope + router) proceeds on user go.
