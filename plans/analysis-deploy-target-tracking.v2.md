# Implementation Plan: Deploy Per-Target Tracking

**Date**: 2026-03-21
**Status**: Ready for implementation
**Scope**: Wire existing `UpdateTarget()` + `DevFailed()` into production via checker-internal approach
**Review**: analysis-deploy-target-tracking.review-1.md

---

## Design Decision

**Keep per-target tracking. Wire via step checker internals, not new MCP action.**

Rationale:
- Deploy workflow IS the use case: single agent deploys multiple targets sequentially
- Bootstrap does NOT need it: subagents self-manage per service pair
- `checkDeploy()` already queries API per-target status -- natural integration point
- No new `action="target-update"` needed -- avoids API surface growth and naming confusion
- `DevFailed()` enforces dev->stage gating that currently exists only in guidance text

---

## Architecture

```
Agent calls action="complete" step="deploy"
  -> engine.BootstrapComplete() / engine.DeployComplete()
    -> checkDeploy() [step checker]
      -> queries API per target hostname
      -> calls UpdateTarget() for each target based on API status
      -> calls DevFailed() for standard mode gating
      -> returns StepCheckResult with per-target checks
Agent sees: DeployResponse.Targets[].Status updated (via BuildResponse)
Agent sees: DeployResponse.Message with error context if DevFailed
```

No new MCP action. No new WorkflowInput fields. Per-target state populated internally by checker.

---

## Implementation Steps

### Step 0: Hardening (prerequisite, no behavior change)

**0a. Add status enum validation to UpdateTarget()**
```
Location: internal/workflow/deploy.go:239
Change: Add validTargetStatuses map check before storing status
Tests: deploy_test.go — add table-driven cases for invalid status strings
```

**0b. Add attestation length limit to UpdateTarget()**
```
Location: internal/workflow/deploy.go:239
Change: Add maxAttestationLen (10240) check
Tests: deploy_test.go — add case for oversized attestation
```

**0c. Export target status constants**
```
Location: internal/workflow/deploy.go:28-34
Change: Export constants (DeployTargetPending, etc.) for use in checker
Tests: No behavior change
```

### Step 1: Wire UpdateTarget into checkDeploy (RED -> GREEN)

**1a. Write failing test (RED)**
```
Location: internal/tools/workflow_checks_deploy_test.go
Test: TestCheckDeploy_UpdatesTargetStatus
Scenario: checkDeploy runs, service is RUNNING -> target status updated to "deployed"
Assert: state.Deploy.Targets[hostname].Status == "deployed" after check
```

**1b. Implement (GREEN)**
```
Location: internal/tools/workflow_checks.go, checkDeploy()
Change: After verifying service is RUNNING, call state.Deploy.UpdateTarget(hostname, "deployed", "API status RUNNING")
Requires: Pass DeployState into checker (currently checkers only get Plan + BootstrapState)
```

**1c. Checker signature change**
```
Current: StepChecker = func(ctx, *ServicePlan, *BootstrapState) (*StepCheckResult, error)
Needed: Access to DeployState for target updates
Option A: Add *DeployState to StepChecker signature
Option B: checkDeploy captures DeployState via closure (already captures client, projectID)
Preferred: Option B (closure) — no signature change, minimal blast radius
```

### Step 2: Wire DevFailed into checkDeploy for standard mode (RED -> GREEN)

**2a. Write failing test (RED)**
```
Location: internal/tools/workflow_checks_deploy_test.go
Test: TestCheckDeploy_DevFailedBlocksStage
Scenario: Dev target exists with status="failed", stage target pending
Assert: StepCheckResult.Passed == false, check includes "dev target failed" message
```

**2b. Implement (GREEN)**
```
Location: internal/tools/workflow_checks.go, checkDeploy()
Change: After all per-target checks, if mode is standard and DevFailed() returns true,
        add failing check: "dev target failed — fix dev before deploying stage"
```

### Step 3: Expose updated target status in responses (no code change needed)

`BuildResponse()` at deploy.go:295-302 already copies Status from DeployTarget to DeployTargetOut. Once UpdateTarget() is called by the checker, agents will see updated Status values automatically.

**Do NOT expose Error or LastAttestation in DeployTargetOut.** Error context goes in DeployResponse.Message.

### Step 4: Update ResetForIteration target handling (optional)

Current: ResetForIteration() resets ALL targets to pending (deploy.go:262-266).
This is correct for now. Per-target iteration (reset only failed target) is a future enhancement, not needed for initial wiring.

---

## What Changes

| File | Change | Lines |
|------|--------|-------|
| `internal/workflow/deploy.go` | Export status constants, add validation to UpdateTarget() | ~15 |
| `internal/tools/workflow_checks.go` | checkDeploy closure captures DeployState, calls UpdateTarget + DevFailed | ~20 |
| `internal/workflow/deploy_test.go` | Edge cases: invalid status, oversized attestation, failed->error mapping | ~30 |
| `internal/tools/workflow_checks_deploy_test.go` | Integration: target status updates, DevFailed gate | ~40 |

**Total**: ~105 lines changed/added. Zero new files. Zero new MCP actions.

---

## What Does NOT Change

- `DeployTargetOut` stays at 3 fields (no Error/LastAttestation exposure)
- `WorkflowInput` stays the same (no new action or parameters)
- `StepChecker` signature stays the same (closure approach)
- Bootstrap workflow (no per-target tracking needed)
- `ResetForIteration()` behavior (resets all targets)

---

## Test Plan (TDD)

### RED phase (write first, must fail)

| Test | File | Scenario |
|------|------|----------|
| `TestUpdateTarget_InvalidStatus` | deploy_test.go | Pass status="broken" -> error |
| `TestUpdateTarget_OversizedAttestation` | deploy_test.go | 20KB attestation -> error |
| `TestUpdateTarget_FailedSetsError` | deploy_test.go | status=failed -> Error field populated |
| `TestDevFailed_MixedRoles` | deploy_test.go | dev=failed + stage=pending -> true |
| `TestDevFailed_StageFailedOnly` | deploy_test.go | dev=ok + stage=failed -> false |
| `TestCheckDeploy_UpdatesTargetStatus` | workflow_checks_deploy_test.go | RUNNING -> "deployed" |
| `TestCheckDeploy_DevFailedBlocksStage` | workflow_checks_deploy_test.go | dev failed -> check fails |

### GREEN phase (implement to pass)

Implement Step 0 (hardening) -> Step 1 (wire UpdateTarget) -> Step 2 (wire DevFailed).

### Verify phase

```bash
go test ./internal/workflow/... -run TestUpdateTarget -v
go test ./internal/workflow/... -run TestDevFailed -v
go test ./internal/tools/... -run TestCheckDeploy -v
go test ./... -count=1 -short
make lint-fast
```

---

## Risks

| Risk | Mitigation |
|------|-----------|
| Checker mutation of DeployState during check | Checker already has side effects (StoreDiscoveredEnvVars in checkProvision). Established pattern. |
| Target status out of sync with API | Checker queries live API — status reflects real state, not stale data |
| Error in DeployTarget never surfaced | By design — error context in Message field instead |
| Future refactor exposes DeployTarget directly | Code comment on DeployTargetOut: "never serialize DeployTarget directly" |
