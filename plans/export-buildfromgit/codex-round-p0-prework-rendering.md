# Codex PRE-WORK round — rendering + atom hygiene (agent B)

Date: 2026-04-28
Plan SHA at round time: `b743cda0`

## Verdict: CONDITIONAL-APPROVE

Main blockers to amend before Phase 1:

### 1. Priority declarations missing (BLOCKING)

The renderer composes all matching atoms and sorts by `priority` then ID. Omitted `priority:` defaults to `5` (`internal/workflow/synthesize.go:51-81`, `internal/workflow/atom.go:438-449`). The plan does not declare priorities for any of the six new atoms — all six would collide at priority `5`, leaving ordering non-deterministic. Explicit `priority:` values are required for all six before Phase 4.

### 2. Non-existent `chainSetupGitPushGuidance` function (BLOCKING)

No function with that name exists. The close-mode chaining is inline `nextSteps` construction at `internal/tools/workflow_close_mode.go:120-136`. The plan's Phase 3 reference to "reuse `chainSetupGitPushGuidance(...)`" is incorrect; the plan must either document inline reuse or introduce a shared helper in Phase 2.5.

### 3. Handler state must be agent-provided per call, not server-side (BLOCKING)

`variant` and `EnvClassifications` must be request fields supplied by the agent on every call. Package-level mutable handler state is forbidden by `TestNoCrossCallHandlerState` (`internal/topology/architecture_handler_state_test.go:117-149`, `CLAUDE.md:254-257`). The plan's §6 Phase 3 description is ambiguous — it must be clarified to make explicit that these are stateless per-request inputs.

### 4. Template var spelling: `{repoUrl}` not `{repoURL}` (BLOCKING)

`{repoUrl}` and `{targetHostname}` are whitelisted substitution tokens; `{repoURL}` (capital URL) is unknown and fails synthesis (`internal/workflow/synthesize.go:419-480`). Correct the plan's atom prose drafts.

### 5. Phase 2 must land `ops.ExportBundle` before Phase 4 (BLOCKING)

`references-fields: [ops.ExportBundle.ImportYAML, ...]` is syntactically valid, but `TestAtomReferenceFieldIntegrity` (`internal/workflow/atom_reference_field_integrity_test.go:17-57`) will fail at lint time until the struct exists. Plan phase order looks correct (Phase 2 introduces `ExportBundle`, Phase 4 adds atoms), but this dependency must be stated explicitly as a gate.

## Non-blocking flags

### 6. Axis L / N hygiene tension in `export-publish-needs-setup` and `scaffold-zerops-yaml` (NON-BLOCKING, flag)

Axis L hard-fails env-only heading tokens. Axis N fires as a CANDIDATE (not hard-fail) on prose tokens like `container env`, `local env`, `/var/www/`, `via SSH` (`internal/content/atoms_lint_axes.go:40-118`, `internal/content/atoms_lint_axes.go:256-278`). `export-publish-needs-setup` likely needs to distinguish SSH push paths — author must use `<!-- axis-n-keep: signal-#N -->` markers, not bare tokens.

### 7. S12 scenario test and corpus coverage must be updated (NON-BLOCKING before Phase 7)

`TestScenario_S12_ExportActiveEmptyPlan` (`internal/workflow/scenarios_test.go:589-618`) currently asserts exactly one atom: `export`. Corpus coverage (`internal/workflow/corpus_coverage_test.go:766-779`) pins `buildFromGit`, `zerops_export`, `import.yaml` for the export-active fixture. When `export.md` is deleted and replaced, both must be updated in Phase 7. Estimate: 3-5 test changes (S12 split into S12a-d, coverage fixture update, references-atoms integrity update).

### 8. `references-atoms` cross-refs to `setup-git-push-{container,local}` (NON-BLOCKING)

`TestAtomReferencesAtomsIntegrity` checks referenced atom IDs exist in the corpus. These two atoms already exist, so cross-refs are valid today. No action needed unless the IDs change.

## Effective verdict

CONDITIONAL-APPROVE — five blocking amendments resolvable in-place; once amended converges to APPROVE.
