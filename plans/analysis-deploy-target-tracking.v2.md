# Implementation Plan: Deploy Per-Target Tracking — Cleanup + DevFailed Gate

**Date**: 2026-03-21 (updated after refactor)
**Status**: Ready for implementation
**Scope**: Delete dead per-target persistence code; add dev->stage gating as pure API check in checkDeploy()
**Review**: analysis-deploy-target-tracking.review-1.md

---

## Design Decision

**Delete per-target persistence. API is source of truth. Add dev->stage gating as inline checker logic.**

Rationale (updated after user feedback + refactor review):
- `checkDeploy()` now calls `VerifyAll()` — already gives per-target health status from live API
- Agent already sees per-target results in `checkResult.Checks[]` (e.g. `appdev_health=pass`)
- Persisting target status to session JSON is pointless — next check queries API again anyway
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation` have 0 production callers
- Dev->stage gating belongs as inline checker logic, not as stored state

---

## What Changed in the Refactor

| Before | After | Impact |
|--------|-------|--------|
| 6 bootstrap steps (discover→...→verify→strategy) | 5 steps (discover→...→deploy→close) | Verify merged into deploy checker; strategy is post-bootstrap |
| `checkDeploy()` checked RUNNING status only | `checkDeploy()` calls `VerifyAll()` (HTTP, logs, startup) + subdomain checks | Much richer per-target feedback already flows through StepCheckResult |
| `checkVerify()` was separate checker | Removed — merged into `checkDeploy()` | One checker does everything |
| Strategy was bootstrap step 6 | Post-bootstrap via `action="strategy"`, pre-gate in `handleDeployStart()` | Strategy checker deleted |
| `deploy.go` per-target code unchanged | Still unchanged — all dead code still there | Cleanup still needed |

**Key observation**: `checkDeploy()` at workflow_checks.go:144-225 now does:
1. `VerifyAll()` → per-target health checks (lines 167-185)
2. Subdomain access per-target (lines 196-213)
3. Returns per-target `StepCheck` results that agent sees in `checkResult`

This is exactly the per-target feedback we wanted — it already exists, just not persisted. No persistence needed.

---

## Architecture (after change)

```
Agent calls action="complete" step="deploy"
  -> checkDeploy() runs:
    1. VerifyAll() → per-hostname health: appdev_health=pass/fail
    2. Subdomain checks → per-hostname: appdev_subdomain=pass/fail
    3. NEW: Dev->stage gating for standard mode
       if any dev hostname is unhealthy → add failing check
       "dev service {hostname} is unhealthy — fix before stage deployment"
    4. Returns StepCheckResult with all per-target checks
  -> Agent sees checkResult.Checks[] with per-target status
  -> If check fails → agent gets detailed per-target errors, iterates
  -> If check passes → step advances
```

No UpdateTarget(). No persisted target status. API queried fresh each time.

---

## Implementation Steps

### Step 1: Delete dead per-target persistence code

**Delete from deploy.go:**
- `UpdateTarget()` method (lines 238-251) — 0 prod callers
- `DevFailed()` method (lines 274-282) — 0 prod callers
- `DeployTarget.Error` field (line 58) — only written by dead UpdateTarget
- `DeployTarget.LastAttestation` field (line 59) — only written by dead UpdateTarget
- Status constants `deployTargetDeployed`, `deployTargetVerified`, `deployTargetFailed`, `deployTargetSkipped` (lines 30-33) — only used by dead methods
- `ResetForIteration()` line 265: `d.Targets[i].Error = ""` — clears dead field

**Delete from deploy_test.go:**
- `TestDeployState_UpdateTarget` (lines 97-115) — tests dead method
- `TestDeployState_DevFailed` (lines 235-250) — tests dead method

**Result**: `DeployTarget` becomes a simple transport struct:
```go
type DeployTarget struct {
    Hostname string `json:"hostname"`
    Role     string `json:"role"`
    Status   string `json:"status"`
}
```

Status stays initialized to `"pending"` (via `BuildDeployTargets`) and reset to `"pending"` (via `ResetForIteration`). It's display-only — agent sees it in `DeployTargetOut`.

### Step 2: Add dev->stage gating to checkDeploy() (RED -> GREEN)

**2a. Write failing test (RED)**
```
Location: internal/tools/workflow_checks_deploy_test.go
Test: TestCheckDeploy_DevUnhealthyBlocksStage
Scenario:
  - Plan has standard-mode target: appdev (dev) + appstage (stage)
  - VerifyAll returns appdev=unhealthy, appstage=healthy
  - Assert: StepCheckResult.Passed == false
  - Assert: check includes message about dev being unhealthy
```

**2b. Implement (GREEN)**
```
Location: internal/tools/workflow_checks.go, checkDeploy()
Change: After VerifyAll health loop (line ~185), check for standard mode dev targets:
  - Identify dev hostnames from plan (EffectiveMode == "standard")
  - If any dev hostname has a failing health check → add explicit gating check:
    "{hostname}_dev_gate" status=fail detail="dev service unhealthy — fix before stage deploy"
  - This is pure logic on the existing check results, no new API calls
```

### Step 3: Verify and clean up (REFACTOR)

```bash
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
```

---

## What Changes

| File | Change | Lines |
|------|--------|-------|
| `internal/workflow/deploy.go` | Delete UpdateTarget, DevFailed, Error, LastAttestation, 4 status constants | -35 |
| `internal/workflow/deploy_test.go` | Delete TestDeployState_UpdateTarget, TestDeployState_DevFailed | -25 |
| `internal/tools/workflow_checks.go` | Add dev->stage gating logic in checkDeploy() | +15 |
| `internal/tools/workflow_checks_deploy_test.go` | Add TestCheckDeploy_DevUnhealthyBlocksStage | +25 |

**Net**: ~20 lines fewer code. Zero new files. Zero new MCP actions.

---

## What Does NOT Change

- `DeployTargetOut` stays at 3 fields (Hostname, Role, Status)
- `DeployTarget.Status` stays (initialized to "pending", display-only)
- `WorkflowInput` stays the same
- `StepChecker` signature stays the same
- `BuildDeployTargets()` stays the same
- `ResetForIteration()` still resets targets to pending (minus dead Error clear)
- Bootstrap workflow unaffected
- `BuildResponse()` still converts DeployTarget -> DeployTargetOut

---

## Test Plan (TDD)

### RED phase

| Test | File | Scenario |
|------|------|----------|
| `TestCheckDeploy_DevUnhealthyBlocksStage` | workflow_checks_deploy_test.go | Standard mode, dev unhealthy → check fails with gate message |
| `TestCheckDeploy_DevHealthyAllowsStage` | workflow_checks_deploy_test.go | Standard mode, dev healthy → check passes (no gate) |
| `TestCheckDeploy_SimpleModeNoGate` | workflow_checks_deploy_test.go | Simple mode → no dev->stage gating logic applied |

### GREEN phase

1. Delete dead code (Step 1) — existing tests pass minus removed ones
2. Implement dev->stage gating (Step 2) — new tests pass

### Verify phase

```bash
go test ./... -count=1 -short
make lint-fast
```

---

## Risks

| Risk | Mitigation |
|------|-----------|
| DeployTarget.Status always "pending" is confusing | It's display-only context (hostname+role mapping). Real status comes from checkResult.Checks[]. Could remove Status field entirely in a follow-up if needed. |
| Dev->stage gating logic duplicates VerifyAll results | Logic reads existing check results, doesn't re-query API. No duplication. |
| Session JSON still has DeployTarget.Status="pending" | Harmless — it's the init value and correctly reflects "not yet checked by API". |
