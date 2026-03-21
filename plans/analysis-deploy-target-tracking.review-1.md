# Review Report: analysis-deploy-target-tracking.md — Review 1
**Date**: 2026-03-21
**Reviewed version**: plans/analysis-deploy-target-tracking.md
**Team**: kb-scout, architect, security, qa-lead, dx-product, zerops-expert, evidence-challenger
**Focus**: Keep per-target tracking and implement it — how to approach
**Resolution method**: Evidence-based (no voting)
**Note**: Evidence challenger was unresponsive; orchestrator performed evidence evaluation directly.

---

## Evidence Summary

| Agent | Findings | Verified | Unverified | Post-Challenge Downgrades |
|-------|----------|----------|------------|--------------------------|
| Architect | 2 critical + 3 design questions | 9/9 verified | 0 | N/A (no challenge round) |
| Security | 4 (1 critical, 2 major, 1 minor) | 4/4 verified | 0 | N/A |
| QA Lead | 6 (3 critical, 2 major, 1 pass) | 6/8 verified | 0 | N/A |
| DX/Product | 6 (1 critical, 3 major, 2 minor) | 6/6 verified | 0 | N/A |
| Zerops Expert | 5 (2 major clarity, 3 informational) | 5/5 verified | 0 | N/A |

**Overall**: CONCERNS — infrastructure is sound, but wiring requires hardening + design clarification on scope.

---

## Input Document

# Analysis: Deploy Per-Target Tracking System (A4-A6)

**Date**: 2026-03-21
**Status**: Pending decision
**Scope**: `UpdateTarget()`, `DevFailed()`, `DeployTarget.Error`, `DeployTarget.LastAttestation`

---

## What It Is

A per-target deploy progress tracking system in `internal/workflow/deploy.go`. Designed to let agents report deployment status for individual hostnames during a multi-service deploy.

### Components

| Component | Location | Purpose |
|-----------|----------|---------|
| `UpdateTarget(hostname, status, attestation)` | deploy.go:239-251 | Set per-hostname status + attestation |
| `DevFailed()` | deploy.go:275-282 | Check if any dev target failed (gate for stage deploy) |
| `DeployTarget.Error` | deploy.go:58 | Error message for failed target |
| `DeployTarget.LastAttestation` | deploy.go:59 | Last attestation for target |

### Original Design Intent

Per-target tracking flow (never implemented):
1. Agent deploys `appdev` -> `UpdateTarget("appdev", "deployed", "built ok")`
2. Agent verifies -> `UpdateTarget("appdev", "verified", "health check passed")`
3. Agent checks `DevFailed()` -> if false, proceed to stage
4. Agent deploys `appstage` -> `UpdateTarget("appstage", "deployed", "...")`

---

## Current State

### Zero Production Callers

- `UpdateTarget()` -- **0 prod callers** (only deploy_test.go:104-115)
- `DevFailed()` -- **0 prod callers** (only deploy_test.go:235-250)
- `Error` field -- only written by dead `UpdateTarget()`
- `LastAttestation` -- only written by dead `UpdateTarget()`

### What IS Used

- `DeployTarget.Status` -- initialized in `BuildDeployTargets()`, reset in `ResetForIteration()`, serialized in `BuildResponse()` -> `DeployTargetOut`
- BUT Status is never **modified** between init (`pending`) and reset (back to `pending`) in production

### Not Exposed to Agents

`DeployTargetOut` (the response type) has only 3 fields:
```go
type DeployTargetOut struct {
    Hostname string `json:"hostname"`
    Role     string `json:"role"`
    Status   string `json:"status"`
}
```
Error and LastAttestation are **not** in the response -- agents never see them.

### Persisted But Unused

Error and LastAttestation are serialized to session JSON (`.zcp/state/sessions/{id}.json`) with `omitempty` tags. They're deserialized on load but never read.

---

## Why It's Unused

The deploy workflow is **step-based**, not **target-based**:

```
Step-based (current):  prepare -> deploy -> verify (all targets together)
Target-based (unused): for each target: deploy -> verify -> next target
```

The iteration model via `ResetForIteration()` superseded per-target updates. When deploy fails, the agent calls `action="iterate"` which resets ALL targets to pending -- no per-target granularity.

Tool handlers (`tools/workflow_deploy.go`) only do step-level completion:
- `handleDeployComplete()` calls `engine.DeployComplete(step, attestation)` -- step-level, not target-level
- No code reads individual target status, error, or attestation

---

## Decision: Delete or Keep?

### If Delete (recommended if per-target tracking is abandoned)

Remove:
- `UpdateTarget()` method (13 lines)
- `DevFailed()` method (8 lines)
- `DeployTarget.Error` field
- `DeployTarget.LastAttestation` field
- `ResetForIteration()` line 265: `d.Targets[i].Error = ""` (clear of dead field)
- `deploy_test.go`: `TestDeployState_UpdateTarget` and `TestDeployState_DevFailed`

Impact: ~50 lines deleted, ~30 test lines deleted. Zero behavior change.

### If Keep (if per-target tracking is a planned feature)

Wire into production:
1. Add `action="target-update"` to `zerops_workflow` tool
2. Call `UpdateTarget()` from tool handler when agent reports per-service status
3. Add `DevFailed()` check in deploy step checker (block stage if dev failed)
4. Expose `Error` in `DeployTargetOut` so agents can see per-target errors
5. Add target-level iteration (reset single target, not all)

This would require significant new code and guidance changes.

### Key Question

Is per-target deploy tracking a planned feature for multi-service bootstraps (where each service deploys independently)? Or was it exploratory scaffolding that the step-based model replaced?

**Current multi-service pattern**: Parent agent spawns Service Bootstrap Agents per runtime pair. Each subagent runs the full deploy independently. The parent's deploy step completes when all subagents finish. Per-target tracking at the engine level adds nothing -- subagents track their own progress.

---

## Knowledge Brief

### Bootstrap multi-service deploy
- Single service pair: inline. 2+ pairs: parent spawns Service Bootstrap Agent subagents per pair
- Step-level tracking only -- NO per-target tracking in bootstrap
- BootstrapState has NO Targets field (bootstrap.go:35-42)
- Deploy checker validates RUNNING status via API, not per-target state

### Deploy workflow (standalone)
- DeployState has Targets []DeployTarget with all fields
- DeployTargetOut deliberately omits Error/LastAttestation
- UpdateTarget() and DevFailed() exist with tests but 0 prod callers
- 3 steps: prepare -> deploy -> verify (step-level, not per-target)

### Standard mode
- Dev gates stage (spec says so), but NO code enforcement
- DevFailed() would be the natural enforcement point
- Dev->stage ordering is a ZCP policy, NOT a Zerops platform requirement (Zerops deploys services independently)

### Iteration model
- ResetForIteration() already resets targets properly
- Infrastructure ready for per-target use

### Agent view
- Deploy workflow shows targets with status (always "pending" in production)
- Bootstrap shows NO targets in response

---

## Agent Reports

### Architect
**Assessment**: SOUND with 2 architectural concerns

**[C1] CRITICAL**: Bootstrap has NO per-target tracking; Deploy alone gets it -- asymmetry. BootstrapState has no Targets field (bootstrap.go:35-42). But KB Scout refined this: bootstrap doesn't NEED it (subagents self-manage), deploy DOES need it (single agent deploys multiple targets sequentially).

**[C2] MAJOR**: DeployTargetOut intentionally omits Error/LastAttestation (deploy.go:93-98, 295-302). Proposal breaks this design. Recommendation: use guidance/message for errors, not response fields.

**Key insight**: action="target-update" would be the FIRST per-entity action. All current actions are workflow-level (step, iteration, session). This is a granularity shift.

**Alternative**: Deploy step checker (`checkDeploy()`) already queries API per target. It could call UpdateTarget() internally, avoiding new MCP action entirely.

### Security
**Assessment**: CONCERNS (4 findings)

**[C1] CRITICAL**: UpdateTarget() accepts ANY string as status -- no enum validation (deploy.go:239-251). Constants defined at lines 29-33 but not enforced. MUST fix before production.

**[C2] MAJOR**: Error/LastAttestation could leak sensitive info (SSH errors, connection strings) if exposed. Current BuildResponse() filters correctly via DeployTargetOut, but this is a fragile runtime contract.

**[C3] MAJOR**: No hostname validation in UpdateTarget() against current session's target list. Tool handler MUST validate hostname is in state.Deploy.Targets.

**[C4] MINOR**: No upper bound on attestation string length. Risk: session JSON bloat / DoS.

### QA Lead
**Assessment**: CONCERNS (3 critical gaps)

**[C1] CRITICAL**: UpdateTarget() zero production callers. Test exists (deploy_test.go:97-115) but covers only the method, not the integration.

**[C2] CRITICAL**: DevFailed() zero production callers. Same pattern -- tested but orphaned.

**[C3] MAJOR**: Edge cases partially covered. Untested: invalid status constants, empty hostname, attestation->error field mapping when status=failed, DevFailed() with mixed role targets.

**[C4] CRITICAL**: No tool handler for target-update action exists.

**[C5] MAJOR**: No integration test for dev->stage flow with DevFailed() gate.

### DX/Product
**Assessment**: UNSOUND (as currently architected)

**[C1] CRITICAL**: Dead API surface -- 0 callers across codebase.

**[C2] MAJOR**: Architectural mismatch -- deploy is step-based, proposal adds target-based operations. Mixed granularity.

**[C3] MAJOR**: Naming confusion. "target-update" is unclear. Status values are unexported constants, not documented in jsonschema.

**Recommendation**: If feature IS needed, extend `action="complete"` with optional hostname parameter rather than adding new action. Reuses existing validation patterns.

### Zerops Expert
**Assessment**: SOUND -- no platform conflicts.

**[C1] INFORMATIONAL**: Dev->stage ordering is ZCP policy, not Zerops platform requirement.

**[C2] MAJOR CLARITY**: "Dev" and "stage" are ZCP abstractions, not Zerops API concepts.

**[C4] MAJOR CLARITY**: DevFailed() is a ZCP workflow policy, not platform enforcement.

---

## Evidence-Based Resolution

### Verified Concerns (drive changes)

1. **Status enum validation missing** (Security C1) -- deploy.go:239-251, constants at 29-33. MUST fix.
2. **Zero production callers** (QA C1/C2, DX C1) -- grep confirms test-only usage.
3. **No tool handler exists** (QA C4) -- workflow.go action switch has no target-update case.
4. **Deploy is step-based, not target-based** (Architect C1, DX C2) -- current iteration resets ALL targets.
5. **DeployTargetOut deliberately omits Error** (Architect C2) -- explicit 3-field conversion at deploy.go:295-302.

### Logical Concerns (inform changes)

1. **Bootstrap doesn't need per-target tracking** (Architect C1 + KB Scout refinement) -- subagents handle per-service, parent verifies batch.
2. **Deploy workflow IS the right place** (KB Scout) -- single agent deploys multiple targets sequentially in standalone deploy.
3. **Checker-internal approach avoids API surface growth** (Architect alternative) -- checkDeploy() already queries per-target API status.
4. **Extend action="complete" rather than add action="target-update"** (DX R2) -- reuses existing parameter patterns.

### Unverified Concerns (flagged for investigation)

1. **Attestation content may contain secrets** (Security C2) -- plausible but no evidence of actual leakage path in current code.

### Top Recommendations (evidence-backed, max 7)

1. **Add status enum validation to UpdateTarget()** -- 5-line fix, CRITICAL before any production use. Evidence: Security C1, deploy.go:239-251.

2. **Wire via checker, not new MCP action** -- checkDeploy() already queries API per target (workflow_checks.go:148-211). After checking RUNNING status, call UpdateTarget() internally. No new action="target-update" needed. Avoids API surface growth (Architect) and naming confusion (DX). Evidence: Architect alternative + KB Scout "checker can call internally."

3. **Integrate DevFailed() into checkDeploy()** -- Standard mode: if dev target status is failed, fail the check with guidance to iterate. Evidence: Security/QA/Zerops-expert all confirm this is ZCP policy with zero code enforcement.

4. **Keep DeployTargetOut at 3 fields** -- Don't expose Error. Use DeployResponse.Message for error context. Evidence: Architect C2, deploy.go:295-302 deliberate filtering.

5. **Add hostname validation in tool handler** -- If any external path to UpdateTarget() is added, validate hostname against session targets. Evidence: Security C3.

6. **Add attestation length limit** -- Cap at 10KB in UpdateTarget(). Evidence: Security C4, no upper bound currently.

7. **Expand test coverage before wiring** -- TDD RED phase: invalid status, DevFailed mixed roles, checker-internal UpdateTarget calls, dev->stage integration flow. Evidence: QA C3/C5.

---

## Revised Version

See `plans/analysis-deploy-target-tracking.v2.md`

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Decision | Changed from "delete or keep?" to implementation plan | User directive | Task description |
| 2 | Implementation | Checker-internal approach instead of new MCP action | workflow_checks.go:148-211 | Architect + KB Scout |
| 3 | DevFailed | Integrate into checkDeploy() as standard-mode gate | deploy.go:275-282 | Security + Zerops Expert |
| 4 | DeployTargetOut | Keep at 3 fields, use Message for errors | deploy.go:295-302 | Architect C2 |
| 5 | Validation | Add status enum + hostname + attestation length | deploy.go:239-251 | Security C1/C3/C4 |
| 6 | Scope | Deploy only, not bootstrap | bootstrap.go:35-42 | Architect C1 + KB Scout |
| 7 | Tests | Full TDD plan added | deploy_test.go gaps | QA C3/C5 |
