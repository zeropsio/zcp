# Phase 3 tracker — Tool handler (`internal/tools/workflow_export.go`)

Started: 2026-04-28 (immediately after Phase 2 EXIT `1df479f6`)
Closed: TBD (pending Codex POST-WORK round + user go for Phase 4)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 3.
> EXIT: handler compiles + tests pass, all three phases pin tested,
> chain-to-prereq pin tested, Codex POST-WORK APPROVE.
> Risk classification: MEDIUM.

## Plan reference

- Plan SHA at session start: `abf0743f`
- Plan file: `plans/export-buildfromgit-2026-04-28.md`
- Phase 0 amendments folded: §13 (13 amendments)
- Phase 2 amendments folded: §14 (Phase 2 retrospective + Phase 3 clarifications)

## Pre-Phase-3 reality check (Claude-side)

| claim | location | result |
|---|---|---|
| `synthesizeImmediateGuidance` routes export to atom guidance | `internal/tools/workflow_immediate.go:35` | PASS — to be replaced for the no-action path |
| Existing handler pattern (`handleCloseMode`, `handleGitPushSetup`) shows inline `nextSteps` chain construction | `workflow_close_mode.go:120-136` + `workflow_git_push_setup.go:117` | PASS — pattern matches plan §13 amendment 3 |
| `ops.SSHDeployer` interface available + threaded through `RegisterWorkflow` | `internal/ops/deploy_common.go:111` + `workflow.go:141` (sshDeployer parameter) | PASS |
| `fetchZeropsYamlOverSSH` already exists in deploy path | `internal/tools/deploy_git_push.go:22-38` | PASS — informs my `readZeropsYAMLBody` design (separate impl with `||` fallback to `.yml`) |
| `topology.Mode` source = `ServiceMeta.Mode` from stateDir, NOT `svc.Mode` from Discover | `internal/workflow/service_meta.go:35` | PASS — `svc.Mode` from platform is HA/NON_HA, different concept |
| Phase 2 `BundleInputs` shape supports the handler's BuildBundle call site | `internal/ops/export_bundle.go:59` | PASS |

## Plan deviations (deliberate, called out for Codex POST-WORK)

1. **Two export entry points coexist for Phase 3**:
   - `workflow="export"` (no action) → `handleExport` (the new canonical path).
   - `action="start" workflow="export"` → still routes to `synthesizeImmediateGuidance` (returns the legacy atom).
   Phase 4 will delete the legacy atom; both paths should converge then. Surfaced to Codex POST-WORK to confirm or amend.

2. **Setup name resolution heuristic** (`pickSetupName` + `setupCandidatesFor`): generalized the legacy atom's mapping (`*dev → dev`, `*stage|*prod → prod`, `*worker → worker`) into a candidate-list approach (exact hostname → prefix-strip + each suffix → suffix-only → first if single). Adds variance without enumerating tables.

3. **Probe / generate split**: the SSH read + Discover happens in the handler (Phase A), `BuildBundle` is pure composition (Phase B). Matches Phase 2 amendment §14.2 #1.

4. **Stateless multi-call narrowing**: handler reads `WorkflowInput.Variant` + `WorkflowInput.EnvClassifications` per call; no server-side state. `TestNoCrossCallHandlerState` enforces.

## TDD discipline

Phase 3 was test-driven with several iterations:

| step | command | result |
|---|---|---|
| Write handler + probe helpers | (Write x2) | DONE — 370 + 165 LOC, both under soft cap |
| Build + existing-suite check | `go build ./...` | PASS |
| Find broken existing test (`TestWorkflowTool_Immediate_Export` panic on nil client) | `go test ./internal/tools/` | RED — nil-client → panic in `Discover` |
| Add nil-client + nil-projectID defensive checks in handleExport | Edit | PASS — handler returns clean errors |
| Update `TestWorkflowTool_Immediate_Export` to expect new error shape | Edit | PASS |
| Write 11 new handler tests + `TestPickSetupName` | (Write) | DONE — 600 LOC of table-driven tests |
| Iterate `setupCandidatesFor` heuristic on stage→appprod test failure | Edit (add prefix×suffix combinations) | PASS |
| Run gofmt | `gofmt -w` | PASS |
| Run lint-local; fix exhaustive (missing single-half mode case), goconst (`"simple"`), unparam (`hostname`, then `mode`) | Edit + Edit + Edit | PASS — 0 issues |

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test ./... -short -race -count=1` | all packages PASS |

## Sub-pass work units

| # | sub-pass | initial state | final state | commit |
|---|---|---|---|---|
| 1 | survey existing handler patterns + SSH access + topology.Mode source | unknown | DONE | n/a |
| 2 | implement `internal/tools/workflow_export.go` (handler) | absent | DONE — 370 LOC, 7 helper functions | (commit pending) |
| 3 | implement `internal/tools/workflow_export_probe.go` (SSH + setup-name heuristic + managed-services collection) | absent | DONE — 165 LOC, 6 helper functions | (commit pending) |
| 4 | wire `workflow="export"` no-action path to `handleExport` | static atom path | DONE | (commit pending) |
| 5 | update `TestWorkflowTool_Immediate_Export` for new error shape | legacy expectation | DONE | (commit pending) |
| 6 | implement 12 new tests covering all branches + helper unit tests | absent | DONE — 600 LOC, all PASS | (commit pending) |
| 7 | iterate `setupCandidatesFor` to handle prefix×suffix combinations (e.g. appstage → appprod) | naive | DONE — combinations added | (commit pending) |
| 8 | run verify gate | unverified | DONE — lint-local + race PASS | n/a |
| 9 | Codex POST-WORK round | not run | running — agent A59 | TBD |

## Codex rounds

Single POST-WORK agent per plan §7 (Phase 3 not listed in fan-out opportunities — single agent suffices for MEDIUM-risk handler review):

| agent | scope | output target | status |
|---|---|---|---|
| A | multi-call narrowing + statelessness + chain composition + test coverage gaps + Phase 4 forward-compat + workflow.go routing + test layer | `codex-round-p3-postwork-handler.md` | running |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| A | multi-call narrowing + statelessness + chains + tests + Phase 4 forward-compat + workflow.go routing + test-layer | ~287s | NEEDS-REVISION (2 BLOCKERS + 4 amendments) |

**Convergent verdict**: NEEDS-REVISION → all 6 amendments folded in-place → effective APPROVE per §10.5 work-economics rule.

### Amendments applied (6 total)

**Blockers (must-fix-before-Phase-4)**:

1. **Unclassified env value redaction** — `classifyPromptResponse` no longer leaks the rendered `ImportYAML` (which inlines values for unclassified envs). The response now carries the live `zerops.yaml` body + warnings + table-with-keys-only. Agents fetch values via `zerops_discover includeEnvValues=true` for grep-driven classification. Pinned by `TestHandleExport_ClassifyPromptDoesNotLeakValues` (sentinel-value scan asserts no leak).

2. **Split-brain routing fixed** — `action="start" workflow="export"` now routes to `handleExport` instead of falling through to `handleStart` → `synthesizeImmediateGuidance` (legacy static atom). Both invocation shapes converge on the same multi-call flow. Affected tests updated (`TestWorkflowTool_Action_Start_Immediate`, `TestWorkflowTool_Action_Start_ImmediateNoSession`, `TestHandleStart_ImmediateWorkflow_NotRejected`).

**Non-blocking amendments**:

3. **`needsClassifyPrompt` partial-classifications fix** — walks all project envs and returns true if ANY is missing from `EnvClassifications`. Previously treated any non-empty map as "fully classified". Pinned by `TestNeedsClassifyPrompt` (5 cases) + `TestHandleExport_PartialClassifications_RePromptsClassify`.

4. **ModeStage + ModeLocalStage handler tests** — `TestHandleExport_ModeStage_VariantUnset_ReturnsVariantPrompt` + `TestHandleExport_ModeLocalStage_VariantUnset_ReturnsVariantPrompt`. Variant-prompt branch coverage extends from ModeStandard-only to all three pair modes.

5. **SSH error propagation test** — `TestHandleExport_SSHReadError_Propagates` populates `routedSSH.errs` and asserts the handler surfaces a clean platform-error response (not panic, not silent fallback to scaffold-required). Exercises the `ExecSSH` error path in `workflow_export_probe.go`.

6. **Extra classifications + pure helper unit tests** — `TestHandleExport_ExtraClassificationKeys_NoSuppress` pins the policy that classifications keys without matching project envs are informational. `TestResolveExportVariant` covers 11 mode/variant combinations directly (no MCP fixture overhead). `TestNeedsClassifyPrompt` covers the partial-classifications logic directly.

### Coding deltas (Phase 3 EXIT)

- `internal/tools/workflow_export.go` (368 LOC) — handler with redacted classify-prompt + correct partial-classifications gate.
- `internal/tools/workflow_export_probe.go` (170 LOC) — SSH read + setup-name heuristic + managed-services collection.
- `internal/tools/workflow_export_test.go` (1037 LOC) — 17 test functions covering all branches + 6 helper unit tests.
- `internal/tools/workflow.go` (+10 LOC) — wire export to handleExport for both `workflow="export"` no-action AND `action="start" workflow="export"` paths; new `workflowExport` constant.
- `internal/tools/workflow_test.go` (+10 LOC) — three test updates for new routing.
- `internal/tools/workflow_start_test.go` (+5 LOC) — `TestHandleStart_ImmediateWorkflow_NotRejected` updated to drop the IsError assertion (handlExport's nil-client error is fine; the SUBAGENT_MISUSE check is what matters).

## Phase 3 EXIT

- [x] Handler compiles + tests pass.
- [x] All three phase shapes (probe / generate / publish) pin tested.
- [x] Chain-to-prereq pin tested (scaffold + git-push-setup).
- [x] Verify gate green (lint-local 0 issues; race PASS; full short suite PASS).
- [x] Codex POST-WORK APPROVE (effective verdict after 6 in-place amendments per §10.5 work-economics).
- [x] Codex round transcript persisted (`codex-round-p3-postwork-handler.md`).
- [x] `phase-3-tracker.md` finalized.
- [x] Phase 3 EXIT commits: `d5a44cd0` (handler + amendments) + `8352dfa2` (tracker + Codex transcript).
- [ ] User explicit go to enter Phase 4.

## Notes for Phase 4 entry

1. Phase 4 is HIGH risk — atom corpus restructure (delete `export.md`, add 6 new atoms).
2. Phase 4 GATE: atom `references-fields: [ops.ExportBundle.*]` needs `ops.ExportBundle` to exist (Phase 2 landed it). PASS — gate satisfied.
3. Phase 4 PER-EDIT Codex rounds mandatory for `export-classify-envs.md` (load-bearing classification protocol) and `export-publish-needs-setup.md` (chain contract semantics).
4. After Phase 4, `action="start" workflow="export"` legacy path will fail (atom deleted). Phase 4 should also remove that path or route it to `handleExport` for consistency.
5. Session pause point: Phase 4 begins ONLY after explicit user go.