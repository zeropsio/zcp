# Phase 7 tracker — Tests (scenarios + corpus_coverage + e2e mock)

Started: 2026-04-29 (immediately after Phase 6 EXIT `2474c652`)
Closed: 2026-04-29 (Phase 7 EXIT commits below)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 7.
> EXIT: all test layers green, scenarios + corpus_coverage updated.
> Risk classification: LOW.

## Plan reference

- Plan SHA at session start: `2474c652`

## Status of plan §6 Phase 7 work scope

| step | scope | status |
|---|---|---|
| 1. `scenarios_test.go::TestScenario_S12_ExportActiveEmptyPlan` split into S12a/b/c/d | the test now exact-matches the six new atoms (intro/classify/validate/publish/publish-needs-setup/scaffold). The split into a/b/c/d sub-cases would be redundant — the test already pins the full atom set in one assertion since `SynthesizeImmediatePhase` renders ALL atoms with `phases: [export-active]` regardless of upstream state (no service context to discriminate per call). The handler's per-call narrowing is asserted by `internal/tools/workflow_export_test.go` instead. | DONE in Phase 4 |
| 2. `corpus_coverage_test.go` export envelope coverage for variant=dev/stage/unset | The current `export_active` fixture exercises a single envelope (ModeStandard, deployed). Phase 6's variant logic happens at the handler level, not the rendering pipeline (atoms don't filter on variant — variant is a runtime decision). Per-variant corpus_coverage entries would be no-ops since the synth pipeline doesn't see variant. | DONE — current single-envelope fixture is sufficient |
| 3. `bootstrap_outputs_test.go` confirm export doesn't perturb meta state | Phase 6 added `meta.RemoteURL` writes via `refreshRemoteURLCache`. Other meta fields (Mode, BootstrappedAt, FirstDeployedAt, CloseDeployMode/Confirmed, GitPushState, BuildIntegration) are NOT touched by the export handler. The aligned-cache test (`TestHandleExport_RemoteURLAligned_NoWarning`) already implicitly pins this — when cache equals live, no meta write occurs. No bootstrap_outputs change needed. | DONE — implicitly covered |
| 4. `internal/tools/workflow_export_test.go` integration paths for all three handler call shapes | scope-prompt + variant-prompt + classify-prompt + publish-ready + validation-failed + scaffold-required + git-push-setup-required + ModeStage / ModeLocalStage / ModeSimple branches + RemoteURL drift / aligned / seed + helper unit tests for `pickSetupName` / `setupCandidatesFor` / `needsClassifyPrompt` / `resolveExportVariant` / `refreshRemoteURLCache`. ~30 test functions across the suite. | DONE in Phases 3-6 |
| 5. Mock e2e `integration/export_test.go` | `TestExportFlow_MultiCallThroughServer` exercises the four-step flow (scope → variant → classify → publish-ready) through the full MCP transport with mock platform.Client + routedSSH stub. Pins the JSON wire shape + tool dispatch end-to-end. Verifies cache refresh writes the live URL after publish-ready. | DONE — added in this Phase 7 commit |

## Coding deltas (Phase 7 EXIT)

- `integration/export_test.go` (NEW, 245 LOC): `TestExportFlow_MultiCallThroughServer` exercises the full multi-call narrowing through `mcp.NewClient` + in-memory transports + mock `platform.Client` + routed SSH stub. Pins:
  - Call 1 (`workflow="export"` no targetService) → `status="scope-prompt"` with project runtimes listed.
  - Call 2 (targetService set, ModeStandard) → `status="variant-prompt"`.
  - Call 3 (variant=dev, no envClassifications) → `status="classify-prompt"` with rows containing `key` only (no leaked values per Phase 3 redaction).
  - Call 4 (envClassifications populated, GitPushConfigured, schema-valid yamls) → `status="publish-ready"` with bundle.importYaml containing `buildFromGit:` + the live remote URL + nextSteps mentioning `zerops_deploy strategy=git-push`.
  - Cache refresh: meta.RemoteURL ends up at the live URL after the flow.

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test ./... -short -race -count=1` | all packages PASS |

## Codex rounds

NONE. Plan §7 Codex protocol does NOT mandate POST-WORK for Phase 7 (LOW risk, test consolidation only).

## Phase 7 EXIT

- [x] All test layers green.
- [x] Mock e2e integration test landed.
- [x] Scenarios + corpus_coverage + bootstrap_outputs updates accounted for (most done in earlier phases; gaps documented above as no-op-required cases).
- [x] Verify gate green.
- [x] `phase-7-tracker.md` finalized.
- [ ] Phase 7 EXIT commit (single — see hash once made).
- [ ] User explicit go to enter Phase 8 (E2E LIVE on eval-zcp — requires GitHub PAT + provisioned services).

## Notes for Phase 8 entry

1. Phase 8 is the LIVE verification phase: provision a small Laravel-style test scenario on eval-zcp; run the export workflow end-to-end against real Zerops; verify the exported repo + re-import on a fresh project.
2. **Phase 8 requires resources Claude does not control**:
   - GitHub PAT with `Contents: Read and write` scope (for buildFromGit re-import).
   - eval-zcp provisioning permissions (already configured per CLAUDE.local.md).
3. Phase 8 is the natural pause point: Claude's local-test work (Phases 0-7) is complete; live verification requires user-provided tokens + a willingness to spin up real Zerops services that survive the test run.
4. Codex POST-WORK for Phase 8 fans out per plan §7: dev variant log + stage variant log + re-import behavior.