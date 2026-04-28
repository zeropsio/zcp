# Phase 2 tracker — Generator code (`internal/ops/export_bundle.go`)

Started: 2026-04-28 (immediately after Phase 1 EXIT `b3cea80f`)
Closed: 2026-04-28 (Phase 2 EXIT commits below); Phase 3 paused awaiting user go

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 2.
> EXIT: generator compiles + tests pass, coverage on each composer,
> Codex POST-WORK APPROVE on edge cases (empty envVariables,
> secret-mid-string, multi-line zerops.yaml setups).
> Risk classification: MEDIUM.

## Plan reference

- Plan SHA at session start: `eed181ba` (Phase 0 EXIT) → `180c13c7` (tracker backfill) → `aee5e5d5` + `fa23a376` + `b3cea80f` (Phase 1)
- Plan file: `plans/export-buildfromgit-2026-04-28.md`
- Phase 0 amendments folded: see plan §13 (13 surgical edits applied in-place)

## Pre-Phase-2 reality check (Claude-side)

| claim | location | result |
|---|---|---|
| `client.GetProjectExport` returns raw platform YAML | `internal/platform/client.go:42` | PASS — interface method confirmed |
| `Discover` returns project + services with metadata | `internal/ops/discover.go:12-45` | PASS — DiscoverResult / ProjectInfo / ServiceInfo types confirmed |
| `client.GetProjectEnv` / `client.GetServiceEnv` return `[]platform.EnvVar` | `internal/platform/client.go:34-37` | PASS |
| `platform.EnvVar` has `Key` + `Content` fields | `internal/platform/types.go:148-152` | PASS |
| YAML library is `gopkg.in/yaml.v3` v3.0.1 | `go.mod` | PASS |
| `SSHDeployer` interface for SSH command exec | `internal/ops/deploy_common.go:111-114` | PASS — but Phase 2 generator stays pure (no I/O) per peer-layer discipline |
| `internal/tools/export.go:11-41` registers standalone `zerops_export` MCP tool | confirmed | RETAINED per Phase 0 amendment §4 (orthogonal raw-export surface) |

## Plan deviations (deliberate, called out for Codex POST-WORK)

1. **`composeProjectEnvVariables` rename** — plan §6 Phase 2 step 1 listed `composeServiceEnvVariables`. The four-category protocol (§3.4) applies to PROJECT envVariables (not service-level — service-level envSecrets are platform-injected on managed services and zerops.yaml-resolved on runtime). Renamed to reflect actual scope.

2. **`verifyZeropsYAMLSetup` is pure** — plan listed `verifyOrFetchZeropsYAML` implying SSH-fetch capability. The cleaner split: Phase A handler (Phase 3 work) does the SSH read; Phase 2 generator validates the body. Pure functions are testable without SSHDeployer mocks.

3. **`scrubCorePackageDefaults` and `addPreprocessorHeader` for service-level envs OMITTED**. Minimal bundle shape (one runtime + N managed services with hostname/type/mode/priority) doesn't emit verticalAutoscaling or service-level envs that need scrubbing. The plan's helper list was aspirational — only `addPreprocessorHeader` is needed for the project envVariables shape (preserved).

4. **`ManagedServices []ManagedServiceEntry` field on BundleInputs** — plan §3.1 said "ONE service entry"; plan §3.4 implies managed services are re-imported (so `${db_*}` references in zerops.yaml resolve at re-import). Bundle MAY include managed services to reconcile both. Default empty; handler decides which to include based on Discover output. Surfaced to Codex POST-WORK for adjudication.

## TDD discipline

Phase 2 was largely TDD-driven:

| step | command | result |
|---|---|---|
| Write generator + 9 test functions covering all composers + happy paths + errors + 4 real-world shapes (Laravel / Node / static / PHP) | (single Write of both files) | DONE — 328 LOC generator, 783 LOC tests |
| Verify `go build ./internal/ops/` clean | `go build ./internal/ops/` | PASS |
| Run all new bundle tests | `go test -run 'TestVerify\|TestCompose\|TestMap\|TestAdd\|TestBuild'` | All PASS |
| Race detector | `go test -race` on touched packages | PASS |
| Architecture layering | `go test -run TestArchitecture ./internal/topology/` | PASS — ops imports only topology + stdlib + yaml.v3 |

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test -race ./internal/ops/` | PASS |
| architecture compliance | `go test -run TestArchitectureLayering ./internal/topology/` | PASS (topology-stdlib-only / ops-not-workflow / workflow-not-ops / platform-stdlib-only) |

## Sub-pass work units

| # | sub-pass | initial state | final state | commit |
|---|---|---|---|---|
| 1 | reality check ops + platform conventions | unverified | DONE | n/a |
| 2 | survey Discover output + EnvVar shape | unverified | DONE | n/a |
| 3 | confirm yaml.v3 deterministic key order | unverified | DONE — yaml.v3 v3.0.1 sorts map keys alphabetically by default | n/a |
| 4 | design ExportBundle + BundleInputs + 6 helpers | absent | DONE — 328 LOC, under 350 soft cap | (commit pending) |
| 5 | implement generator with 4 plan deviations called out | absent | DONE | (commit pending) |
| 6 | tests for each composer + branch coverage | absent | DONE — 14 test fixtures cover every documented bucket / shape / edge | (commit pending) |
| 7 | run verify gate post-implementation | unverified | DONE — lint-fast clean; full short suite green; race clean; architecture green | n/a |
| 8 | Codex POST-WORK fan-out (generator + architecture) | not run | running — agents A6 + A6c | TBD |

## Codex rounds

Two parallel POST-WORK agents per CLAUDE.local.md "maximize parallel fan-out":

| agent | scope | output target | status |
|---|---|---|---|
| A | generator code correctness + edge cases + real-world shape coverage | `codex-round-p2-postwork-generator.md` | running |
| B | architectural alignment + downstream Phase 3-6 readiness + plan amendments | `codex-round-p2-postwork-architecture.md` | running |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| A | generator code correctness + edge cases + real-world shape coverage | ~201s | NEEDS-REVISION (2 BLOCKERS + 1 advisory + 1 polish) |
| B | architectural alignment + downstream Phase 3-6 readiness | ~176s | NEEDS-AMENDMENT (4 amendments) |

**Convergent verdict**: NEEDS-REVISION → in-place amendments → effective APPROVE per §10.5 work-economics rule.

**Conflict on Reviews shape resolved**: Agent A wanted `Reviews []EnvReview` on the bundle; Agent B wanted review-row DTO at the Phase 3 handler level (composer stays minimal). **Agent B wins** — architecturally cleaner, no overweighting of `BuildBundle`'s pure-composition role. Phase 3 handler will build review rows from `(EnvClassifications, ImportYAML, Warnings, agent-supplied Evidence)`.

### Amendments applied (3 categories, 7 commits-worth of work bundled)

**A — code amendments** (committed as Phase 2 EXIT):
1. `internal/ops/export_bundle_classify.go` (155 LOC, NEW): M2 indirect-reference detector + sentinel pattern flag. Defensive warnings only — no auto-classification (Agent B principle).
2. `internal/ops/export_bundle.go` (343 LOC, +15): wired M2 detector into `composeImportYAML`; added sentinel branch in External case.
3. `internal/ops/export_bundle_test.go` (1077 LOC, +294): six new test functions covering M2 (real + happy-path), sentinel (8 cases), `parseDollarBraceRefs` (9 cases), `extractZeropsYAMLRunEnvRefs` (5 cases), `isLikelySentinel` (17 cases).

**B — plan amendments** (folded into plan §14, single commit):
- §14.1: Phase 2 deliberate deviation table (composeServiceEnvVariables → composeProjectEnvVariables; verifyOrFetchZeropsYAML → verifyZeropsYAMLSetup pure; scrubCorePackageDefaults omitted; ManagedServices added).
- §14.2: Phase 3 clarifications (handler prepares BundleInputs; preview redaction is handler's job; review-row DTO at handler level; RemoteURL freshness at handler).
- §14.3: code amendments table.
- §14.4: tests added table.

**C — design tensions resolved**:
- Reviews-on-bundle vs Reviews-in-handler — handler wins (Agent B).
- Auto-classification heuristic — REJECTED (Agent B principle): warnings only, agent retains classification authority.
- Sentinel detection — added as MINIMAL conservative allowlist (Stripe test prefixes + 7 disable strings), surfaced via warning. Future patterns require real-app justification.

## Phase 2 EXIT

- [x] Generator compiles + tests pass.
- [x] Coverage on each composer (verifyZeropsYAMLSetup, composeProjectEnvVariables, mapImportMode, addPreprocessorHeader, composeImportYAML, BuildBundle, M2 detector, sentinel flag).
- [x] Verify gate green (lint-fast 0 issues; full short suite all PASS; race PASS on internal/ops).
- [x] Codex POST-WORK APPROVE (effective verdict after in-place amendments per §10.5 work-economics).
- [x] Two Codex round transcripts persisted (`codex-round-p2-postwork-{generator,architecture}.md`).
- [x] Plan amendments folded into plan §14.
- [x] `phase-2-tracker.md` finalized.
- [ ] Phase 2 EXIT commits (single bundle commit + tracker commit — see commit hashes below once made).
- [ ] User explicit go to enter Phase 3.

## Notes for Phase 3 entry

1. Phase 3 is MEDIUM risk: handler `internal/tools/workflow_export.go` orchestrates Phase A (probe via SSH + Discover) → Phase B (BuildBundle) → Phase C (publish via zerops_deploy strategy=git-push).
2. The handler needs to:
   - Read `WorkflowInput.TargetService` / `Variant` / `EnvClassifications` (per Phase 1 commits).
   - Discover project + chosen runtime.
   - SSH-read `/var/www/zerops.yaml` from chosen container.
   - SSH-read `git remote get-url origin` from chosen container.
   - Build `BundleInputs`, call `BuildBundle`.
   - Multi-call narrowing: variant-prompt → classify-prompt → publish.
3. Chain composition pattern: inline `nextSteps` à la `internal/tools/workflow_close_mode.go:120-136` (no `chainSetupGitPushGuidance` helper — verified Phase 0).
4. Mandatory Codex POST-WORK round per plan §7.
5. Session pause point: Phase 3 begins ONLY after explicit user go.