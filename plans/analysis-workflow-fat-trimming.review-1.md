# Deep Review Report: Workflow Fat Trimming — Review 1

**Date**: 2026-03-21
**Reviewed version**: `plans/analysis-workflow-fat-trimming.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Identify unnecessary accumulated complexity in the workflow system that can be trimmed
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

10 potential fat items identified via grep/read analysis:

1. `InitSession` (session.go:35) — superseded by `InitSessionAtomic`, zero prod callers
2. `StepCategory` type (bootstrap.go:17-24) — never branched on, only serialized
3. `DeployTarget.Error` (deploy.go:58) — written, never read
4. `DeployTarget.LastAttestation` (deploy.go:59) — written, never read
5. `DeployState.UpdateTarget()` (deploy.go:239) — zero prod callers
6. `DeployState.DevFailed()` (deploy.go:275) — zero prod callers
7. `extractDeploySection()` (workflow_strategy.go:105) — duplicate of `ExtractSection`
8. `isManagedNonStorage` prefix list (workflow_checks.go:327) — copy-paste of `managedServicePrefixes`
9. `hasDevStagePattern` suffixes slice (managed_types.go:66) — single entry loop
10. `SaveSessionState` exported (session.go:122) — test-only export

Cross-cutting: Deploy workflow per-target tracking infrastructure never wired into engine.

### Platform Verification Results (kb-verifier)

- Project state NON_CONFORMANT — correct (zcpx has no dev/stage pairs)
- Zero ServiceMeta files on disk (no bootstrap completed)
- Orphaned session file `.zcp/state/sessions/88144f0953e4facf.json` (Mar 7, deploy, PID 95988)
- Orphaned evidence directory `.zcp/state/evidence/f9bba766d534d52e/`
- Router output matches code path expectations
- Deploy strategies are real routing logic (NOT fat — just dormant)

---

## Stage 2: Analysis Reports

### Correctness Analysis
**Assessment**: CONCERNS (11 fat items, zero correctness bugs)
- Confirmed all 10 KB items; added `StepDetail.Verification` as NOT fat (actively emitted to LLM)
- Key insight: Deploy target tracking (Error, LastAttestation, UpdateTarget, DevFailed) is a coherent cluster of dead code — designed for per-target orchestration never wired into the step-based engine

### Architecture Analysis
**Assessment**: SOUND with minor consolidation opportunities
- 5 findings, all MINOR. System is well-layered.
- Parallel response types (Bootstrap/Deploy/CICD) have intentional duplication (workflow-specific fields)
- Guidance layer is correctly separated by workflow
- Recommended deleting dead code, not restructuring architecture

### Security Analysis
**Assessment**: SOUND with one concern
- `DeployTarget.Error` serialized to disk JSON but never read — potential for sensitive data persistence
- `InitSession` race condition already fixed by `InitSessionAtomic` but function remains as foot-gun
- Orphaned session/evidence files are information hygiene issue, not security vulnerability

### Adversarial Analysis
**Assessment**: Confirmed 7/10 KB items, disputed 3
- **Disputed `LastAttestation`**: Claimed it's used in `GuidanceParams`. RESOLVED by orchestrator: `GuidanceParams.LastAttestation` reads from `BootstrapState.lastAttestation()`, NOT `DeployTarget.LastAttestation`. Different structs, different data paths. DeployTarget field IS dead.
- **Disputed `StepCategory`**: Argued it should be kept. RESOLVED: Type adds zero logic; string literals are sufficient since value is only serialized.
- **Disputed `hasDevStagePattern`**: Argued it's real business logic. ACCEPTED: The function is active, the single-entry loop is trivial syntactic cruft only.
- **New finding**: Orphaned session has `"phase": "INIT"` and `"history": []` fields not in current `WorkflowState` struct. CONFIRMED: These are remnants of an old schema. Not in current code. Stale files only.

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by code inspection + grep)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | `InitSession()` dead function | MAJOR | session.go:35-72, zero prod callers, superseded by `InitSessionAtomic` | KB + all analysts |
| V2 | `DeployTarget.Error` write-only field | MAJOR | deploy.go:58, written at :245, never read, serialized to disk JSON | KB + correctness + security |
| V3 | `DeployTarget.LastAttestation` write-only field | MAJOR | deploy.go:59, written at :243, never read (NOT the same as `GuidanceParams.LastAttestation`) | KB + correctness (adversarial disputed, orchestrator resolved) |
| V4 | `DeployState.UpdateTarget()` dead method | MAJOR | deploy.go:239-251, zero prod callers, only test callers | KB + all analysts |
| V5 | `DeployState.DevFailed()` dead method | MAJOR | deploy.go:275-282, zero prod callers, only test callers | KB + all analysts |
| V6 | `extractDeploySection()` duplicate | MINOR | workflow_strategy.go:105-118, identical to `workflow.ExtractSection()` | KB + correctness + adversarial |
| V7 | `isManagedNonStorage` prefix list copy-paste | MINOR | workflow_checks.go:327-332, duplicates `managedServicePrefixes` from managed_types.go | KB + correctness + adversarial |
| V8 | `StepCategory` type overhead | MINOR | bootstrap.go:17-24, 3 constants never branched on, only serialized as string | KB + correctness + architecture |
| V9 | `SaveSessionState` test-only export | MINOR | session.go:122-124, 1 test caller, comment says "test access" | KB + architecture |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | `hasDevStagePattern` suffixes slice | TRIVIAL | Single entry `{"dev","stage"}`, loop iterates once — syntactic cruft | KB (adversarial says keep — function is real, loop is the cruft) |
| L2 | Orphaned session/evidence files on disk | MINOR | Registry clean but files remain — `ResetSessionByID` doesn't clean evidence dir | kb-verifier + security |

#### UNVERIFIED

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | Old session files have `phase`/`history` fields not in current struct | INFO | Confirmed absent from code, but only one stale file inspected | adversarial |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | `DeployTarget.LastAttestation` | KB+correctness: dead field | deploy.go:59 written by `UpdateTarget()` which has 0 prod callers | adversarial: used in guidance | `guidance.go:18` has `LastAttestation` field | **KB wins**: `GuidanceParams.LastAttestation` is populated from `BootstrapState.lastAttestation()` (bootstrap_guide_assembly.go:34), NOT from `DeployTarget`. Different structs. |
| D2 | `StepCategory` type | KB+correctness: fat | Never branched on, only serialized | adversarial: keep it | Serialized in API response | **KB wins**: Type adds zero enforcement. String literals in `stepDetails` are sufficient. Category string still appears in JSON. |
| D3 | `hasDevStagePattern` suffixes | KB: trivial fat | Single-entry loop | adversarial: real business logic | Called from `DetectProjectState` | **Both right**: Function is real; the single-entry slice wrapper is trivial cruft. Not worth a separate change. |

### Key Insights from Knowledge Base

1. **Deploy per-target tracking is a coherent dead cluster** (V2-V5): The engine operates on steps, not targets. `UpdateTarget()`, `DevFailed()`, `Error`, and `LastAttestation` were designed for per-target state management that was never wired in. Removing them as a group clarifies that deploy is step-based.

2. **Orphaned files accumulate** (L2): Session reset removes the session file and registry entry but not evidence directories. This is an incomplete cleanup pattern that should be addressed.

3. **Two section extractors exist** (V6): `extractDeploySection` in `tools/` is an exact copy of `workflow.ExtractSection`. Since `tools/` already imports `workflow/`, this is pure oversight.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

| # | Item | File | Lines to Remove | Effort |
|---|------|------|----------------|--------|
| 1 | Delete `InitSession()` function | session.go | :35-72 (38L) | 5min — migrate 8 test callers to `InitSessionAtomic` |
| 2 | Delete `DeployState.UpdateTarget()` method | deploy.go | :239-251 (13L) | 5min — delete test callers too |
| 3 | Delete `DeployState.DevFailed()` method | deploy.go | :275-282 (8L) | 2min — delete test callers too |
| 4 | Delete `DeployTarget.Error` field | deploy.go | :58 (1L) + write at :245 + reset at :265 | 2min |
| 5 | Delete `DeployTarget.LastAttestation` field | deploy.go | :59 (1L) + write at :243 | 2min |

### Should Address (VERIFIED Minor)

| # | Item | File | Action | Effort |
|---|------|------|--------|--------|
| 6 | Replace `extractDeploySection()` with `workflow.ExtractSection()` | workflow_strategy.go | Delete :105-118, update caller at :96 | 2min |
| 7 | Derive `isManagedNonStorage` from `isManagedService` | workflow_checks.go | Call `isManagedService()` + exclude storage | 10min |
| 8 | Inline `StepCategory` constants as strings | bootstrap.go, bootstrap_steps.go | Delete type+constants :17-24, use `"fixed"/"creative"/"branching"` | 5min |
| 9 | Delete `SaveSessionState` export | session.go | Delete :122-124, restructure 1 test caller | 5min |

### Investigate (worth checking but not confirmed)

| # | Item | Notes |
|---|------|-------|
| 10 | Evidence directory cleanup on session reset | `ResetSessionByID` doesn't remove `.zcp/state/evidence/{id}/` — consider adding |
| 11 | Stale session files with old schema fields | Clean up `.zcp/state/sessions/88144f0953e4facf.json` and `.zcp/state/evidence/f9bba766d534d52e/` |

**Total lines removed**: ~85 lines of dead code + ~14 lines of duplicated logic
**Total effort**: ~40 minutes

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | deploy.go | Remove `UpdateTarget`, `DevFailed`, `Error`, `LastAttestation` | Zero prod callers (grep verified) | V2-V5, correctness+KB |
| 2 | session.go | Remove `InitSession`, `SaveSessionState` export | Superseded by atomic version; test-only export | V1, V9, all analysts |
| 3 | workflow_strategy.go | Remove `extractDeploySection` duplicate | Identical to `workflow.ExtractSection` | V6, correctness+adversarial |
| 4 | workflow_checks.go | Derive `isManagedNonStorage` from shared prefix list | Copy-pasted prefix list divergence risk | V7, correctness+adversarial |
| 5 | bootstrap.go | Inline `StepCategory` as string | Type never used for control flow | V8, correctness+architecture |
