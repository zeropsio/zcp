**Verdict** — NEEDS-REVISION.

Blocker: the Phase 3 clarification says incomplete `EnvClassifications` requires redacting unclassified env values in the agent-facing preview; `BuildBundle` emits unclassified envs verbatim (`internal/ops/export_bundle.go:304-307`), and the handler returns `bundlePreview(bundle)` plus raw row `value` in the classify prompt (`internal/tools/workflow_export.go:300-316`). That violates the folded Phase 3 requirement (`plans/export-buildfromgit-2026-04-28.md:926-937`). Secondary issue: `action="start" workflow="export"` still routes to static immediate guidance (`internal/tools/workflow.go:328-337`) while no-action `workflow="export"` routes to `handleExport` (`internal/tools/workflow.go:172-179`).

---

## 1. Verdict

**NEEDS-REVISION**

Two blockers:

1. **Unclassified env value leakage** — classify prompt exposes raw secret values through `bundlePreview(bundle)` and the row-level `value` field before the agent has accepted them (`internal/tools/workflow_export.go:300-316`, `internal/ops/export_bundle.go:304-307`). Plan §Phase-C explicitly requires redacted previews when classifications are incomplete (`plans/export-buildfromgit-2026-04-28.md:926-937`).

2. **Split-brain routing** — `action="start" workflow="export"` still forks to legacy `synthesizeImmediateGuidance` (`internal/tools/workflow.go:328-337`) while `action="" workflow="export"` correctly reaches `handleExport` (`internal/tools/workflow.go:172-179`). Since Phase 4 deletes the static atom, leaving this split creates a user-visible inconsistency that must be repaired before Phase 4.

---

## 2. Multi-call narrowing audit per branch

**(a) Empty TargetService → scope-prompt**
Guard fires at `workflow_export.go:65-67`; `scopePromptResponse()` returns `workflow_export.go:214-231`. No ambiguity.

**(b) ModeStandard + Variant unset → variant-prompt**
Branch: `workflow_export.go:180-191`. ModeStandard with empty Variant triggers `variantPromptResponse()` at `workflow_export.go:238-249`. ✓

**(c) ModeSimple / Dev / LocalOnly → variant forced unset, proceed**
`workflow_export.go:176-179` forces variant unset. Default unknown modes also fall through (`workflow_export.go:205-208`). ✓

**(d) Empty zerops.yaml → scaffold-required**
`workflow_export.go:111-117` tests empty content after SSH read; `workflow_export.go:255-264` returns scaffold response. ✓

**(e) Empty git remote → git-push-setup-required (no preview)**
`workflow_export.go:119-125` short-circuits before bundle build; `workflow_export.go:273-288` returns setup-required with nil bundle, so no preview attached. ✓

**(f) Project envs > 0 + EnvClassifications empty → classify-prompt with preview**
`workflow_export.go:158-160` calls `needsClassifyPrompt`; `workflow_export.go:371-373` returns true when `len(EnvClassifications)==0` and `len(projectEnvs)>0`; preview is built from `bundlePreview(bundle)` at `workflow_export.go:300-316`. Blocker: preview includes raw values.

**(g) Classifications populated + GitPushState != configured → git-push-setup-required (with preview)**
`workflow_export.go:162-164` and `workflow_export.go:285-287`. ✓

**(h) All set → publish-ready**
`workflow_export.go:166` and `workflow_export.go:326-351`. ✓

**Ambiguity check:** no double-fire is possible because branches are sequential guards. The only edge case is the interaction between (e) and (g): an empty remote fires (e) before env/git-push checks are reached, so it can mask an unconfigured GitPushState — by design, but worth a comment.

---

## 3. Statelessness compliance

Phase 3 is read-only. Reads: `workflow.FindServiceMeta` (`workflow_export.go:87-97`), `ops.Discover` (`workflow_export.go:69-79`, `workflow_export.go:214-216`), SSH reads via `ExecSSH` (`workflow_export_probe.go:25-57`), `client.GetProjectEnv` (`workflow_export_probe.go:201-214`). No meta writes; Phase 6 owns RemoteURL persistence (`plans/export-buildfromgit-2026-04-28.md:623-648`).

No package-level mutable vars introduced. `workflow_export_probe.go:15-19` declares only constants. `workflow_export.go` declares functions with local vars. `TestNoCrossCallHandlerState` will pass.

---

## 4. Chain composition faithfulness

**(i) No premature gitPushStates axis** — no atom files modified in Phase 3. The plan defers axis additions to Phase 4 (`plans/export-buildfromgit-2026-04-28.md:511-517`, `plans/export-buildfromgit-2026-04-28.md:532-538`). ✓

**(ii) Chain pointers are actionable command strings** — scaffold nextStep: `workflow_export.go:262` names the atom + re-call command; git-push setup nextStep: `workflow_export.go:281` gives the tool-call string. ✓

**(iii) Agent can act without status/list round-trip** — both pointers encode the target tool + params inline. Agent does not need to discover current state again to execute them. ✓

---

## 5. Test coverage gaps

12 functions: `TestHandleExport_ScopePrompt`, `TestHandleExport_VariantPrompt`, `TestHandleExport_ScaffoldRequired`, `TestHandleExport_GitPushSetupRequired_NoRemote`, `TestHandleExport_ClassifyPrompt`, `TestHandleExport_GitPushSetupRequired_WithPreview`, `TestHandleExport_PublishReady`, `TestHandleExport_ModeSimple`, `TestHandleExport_ModeDev`, `TestHandleExport_ModeLocalOnly`, `TestHandleExport_ModeStandard_VariantSet`, and one more — plus `TestPickSetupName`.

Missing:

**(i) ModeStage hostname** — `TestPickSetupName` covers stage naming (`workflow_export_test.go:593-598`) but no handler test exercises a `ModeStage` service as TargetService through the full narrowing path.

**(ii) ModeLocalStage exercise** — no handler test for `ModeLocalStage`; the branch at `workflow_export.go:193-203` is untested end-to-end.

**(iii) Extra envClassifications buckets** — populated maps in tests match project env keys exactly (`workflow_export_test.go:402-407`, `workflow_export_test.go:450-459`). A map with extra keys not in project envs is unexercised.

**(iv) Partial classifications** — `needsClassifyPrompt` returns false for any non-empty map (`workflow_export.go:371-373`). A map where some envs are classified and others are not will skip the classify prompt and proceed — which may produce an incomplete bundle. No test covers this path.

**(v) SSH error propagation** — `routedSSH.errs` is declared (`workflow_export_test.go:22-39`) but never populated in any test; `ExecSSH` error paths in probe (`workflow_export_probe.go:30-37`, `workflow_export_probe.go:48-55`) are untested.

---

## 6. Phase 4 atom corpus forward-compat

Design is coherent. Phase 4 adds six atoms (`plans/export-buildfromgit-2026-04-28.md:476-525`). Phase 3 handler returns JSON state structs through `jsonResult` (`workflow_export.go:226-231`, `workflow_export.go:309-321`, `workflow_export.go:330-351`); it does not inline atom prose. State, preview, and command pointers are payload fields; guidance is deferred to atoms. The split matches the plan's design at `plans/export-buildfromgit-2026-04-28.md:422-447`. ✓

---

## 7. workflow.go routing decision

Current state:
- `action="" workflow="export"` → `handleExport` (`workflow.go:157-179`) ✓
- `action="start" workflow="export"` → `handleStart` → `synthesizeImmediateGuidance` (`workflow.go:151-155`, `workflow.go:217-220`, `workflow.go:328-337`) ✗

**Adjudication: route action=start+workflow=export → handleExport in Phase 3.**

Rationale: Phase 4 deletes the static atom. Leaving the split in place means an agent that invokes `action=start workflow=export` sees completely different output than `action="" workflow=export`. Since `handleExport` already handles the "start" case (it returns the scope prompt or variant prompt which are the correct first-interaction responses), routing start here is safe and eliminates the split-brain. The fix is three lines in `handleStart` or in the action dispatch — trivially verifiable.

---

## 8. Test-layer recommendations

The MCP round-trip layer (`mcp.Server` + `RegisterWorkflow` + mock client + `routedSSH`) is correct for branch orchestration, routing, and JSON payload verification. It costs little and exercises the full decode/encode path.

Add lightweight unit tests for pure helpers:

- `resolveExportVariant` (`workflow_export.go:173-209`) — pure function, 6 mode combinations, no fixture needed.
- `needsClassifyPrompt` (`workflow_export.go:371-373`) — pure function, currently underspecified (partial-map case).
- `setupCandidatesFor` (`workflow_export_probe.go:147-183`) — `TestPickSetupName` is indirect; a direct table test would add `ModeLocalStage`, `ModeStage`, and suffix ordering without MCP fixture overhead.

---

## 9. Recommended amendments

**Amendment 1 (blocker) — Redact unclassified env values in classify prompt**
`internal/tools/workflow_export.go:300-316` and `internal/ops/export_bundle.go:304-307`. When `needsClassifyPrompt` is true, do not pass `bundlePreview(bundle)` (which contains raw values) to the classify response. Pass only the list of unclassified env keys and their source-service labels. Add a test asserting that no raw value appears in the classify-prompt JSON when envs are unclassified.

**Amendment 2 (blocker) — Route action=start+workflow=export to handleExport**
`internal/tools/workflow.go:217-220` (inside `handleStart`) or before the action switch. Add a guard that redirects `workflow="export"` to `handleExport` before the `synthesizeImmediateGuidance` call at `workflow.go:328-337`. Update `TestWorkflowTool_Immediate_Export` at `workflow_test.go:40-61` to assert the same nil-client error that the no-action path returns.

**Amendment 3 — Fix needsClassifyPrompt for partial classifications**
`internal/tools/workflow_export.go:371-373`. Change condition to `len(EnvClassifications) < len(projectEnvs)` so partial maps still prompt. Add test: supply 2 project envs, classify 1, assert classify-prompt fires.

**Amendment 4 — Add ModeStage and ModeLocalStage handler tests**
New test functions in `internal/tools/workflow_export_test.go` covering: (a) `ModeStage` TargetService through variant-prompt path; (b) `ModeLocalStage` through the no-variant-needed proceed path. Each should be a standard MCP round-trip table case.

**Amendment 5 — Add SSH error propagation test**
Populate `routedSSH.errs` in one test case (yaml-read path), assert the handler returns an error response rather than scaffold-required or empty-yaml. Exercises `workflow_export_probe.go:30-37`.

**Amendment 6 — Add extra envClassifications key test**
Supply classifications map with a key not present in project envs, assert the handler either ignores the extra key or returns an error — whichever the spec requires — pinning current behavior.
