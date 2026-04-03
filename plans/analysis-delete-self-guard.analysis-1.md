# Implementation Plan: Self-Delete Guard for ZCP Service — Iteration 1
**Date**: 2026-04-03
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (Explore), adversarial (Explore)
**Complexity**: Deep (4 agents, ultrathink)
**Task**: Add guard so ZCP cannot delete the service it's running on via `zerops_delete`

## Goal

Prevent the `zerops_delete` MCP tool from deleting the service that ZCP itself is running on when deployed inside a Zerops container. Self-deletion would terminate the container mid-operation, breaking the MCP connection. The guard must be clear, testable, and consistent with existing patterns.

## Summary

The Zerops platform has **zero protection** against self-deletion — the API accepts any valid service ID with project-scoped auth. The guard must live in ZCP code. All building blocks exist: `runtime.Info` already carries `ServiceName` and `InContainer`. The change is narrow (3 files + tests), high feasibility, medium risk.

### Key architectural decision: Guard at tools layer (not ops)

The adversarial analyst challenged this, arguing ops layer is simpler (single caller, no signature change). After evidence review:

- **Tools layer wins** because:
  1. `RegisterDeploySSH` already uses `rtInfo` at tools layer for environment-aware behavior — established pattern (`internal/tools/deploy_ssh.go:29`)
  2. The guard is a **tool-level safety constraint** (preventing destructive MCP misuse), not business logic validation
  3. `ops.Delete` is a thin function (6 lines of logic) — injecting runtime dependency there couples a pure API wrapper to container detection
  4. Consistency: all `rtInfo`-dependent decisions happen at tools/server layer, not ops

- Adversarial's CH2 (mount.go doesn't guard hostnames) is valid but irrelevant — mount doesn't have a self-destruction risk. The analogy is deploy_ssh, not mount.

### Adversarial finding accepted: Pre-delete unmount timing (MF1)

The guard MUST run **before** the pre-delete unmount attempt. Current code unmounts first (line 34-40), then calls ops.Delete (line 42). The guard should be inserted at line 33, before the unmount block.

## Scope

### Files to Modify
| File | Changes | Lines |
|------|---------|-------|
| `internal/platform/errors.go` | Add `ErrSelfDeleteBlocked` constant | +1 |
| `internal/tools/delete.go` | Add `rtInfo` param + guard check before unmount | +10 |
| `internal/server/server.go` | Pass `s.rtInfo` to RegisterDelete | +1 word |
| `internal/tools/delete_test.go` | Add 4 test cases for guard | +80 |

### Files to Create
None.

## Implementation Steps

### Step 1: Add error code
**File**: `internal/platform/errors.go`
**TDD**: Compile check only (pure constant)
**Change**: Add after line 49:
```go
ErrSelfDeleteBlocked = "SELF_DELETE_BLOCKED"
```
**Dependencies**: None
**Evidence**: [VERIFIED: errors.go:10-50 — no existing self-delete code]

### Step 2: RED — Write failing tests
**Files**: `internal/tools/delete_test.go`
**TDD**: Write 4 new test functions that fail because guard doesn't exist yet
**Tests**:
1. `TestDeleteTool_SelfDelete_Blocked` — InContainer=true, hostname matches ServiceName → expect `ErrSelfDeleteBlocked`
2. `TestDeleteTool_SelfDelete_LocalDev_Allowed` — InContainer=false, same hostname → expect success
3. `TestDeleteTool_SelfDelete_CaseInsensitive` — InContainer=true, "API" vs ServiceName "api" → expect blocked
4. `TestDeleteTool_OtherService_InContainer_Allowed` — InContainer=true, different hostname → expect success
**Dependencies**: Step 1 (error code must exist for test assertions)
**Evidence**: [VERIFIED: delete_test.go — no self-delete tests exist]

Note: `RegisterDelete` signature must be updated first (Step 3) for tests to compile with `runtime.Info` parameter. Steps 2 and 3 are effectively atomic — tests written with new signature, both committed together as RED phase.

### Step 3: Update RegisterDelete signature + wire rtInfo
**File**: `internal/tools/delete.go` line 22
**Change**: Add `rtInfo runtime.Info` parameter
```go
func RegisterDelete(srv *mcp.Server, client platform.Client, projectID string, stateDir string, mounter ops.Mounter, rtInfo runtime.Info) {
```
**File**: `internal/server/server.go` line 109
**Change**: Pass `s.rtInfo`
```go
tools.RegisterDelete(s.server, s.client, projectID, stateDir, s.mounter, s.rtInfo)
```
**File**: `internal/tools/delete_test.go` — all existing `RegisterDelete` calls
**Change**: Add `runtime.Info{}` as last argument to existing tests (local dev = no guard)
**Dependencies**: None
**Evidence**: [VERIFIED: server.go:109 — currently missing rtInfo; deploy_ssh.go:29 — pattern match]

### Step 4: GREEN — Implement guard
**File**: `internal/tools/delete.go`
**Change**: Insert guard at line 33 (before unmount block):
```go
// Guard: prevent self-deletion when running inside a Zerops container.
if rtInfo.InContainer && strings.EqualFold(input.ServiceHostname, rtInfo.ServiceName) {
    return convertError(platform.NewPlatformError(
        platform.ErrSelfDeleteBlocked,
        fmt.Sprintf("Cannot delete %q — ZCP is running on this service", input.ServiceHostname),
        "Delete this service manually via Zerops GUI, zcli, or from a different machine.",
    )), nil, nil
}
```
**Dependencies**: Steps 1-3
**Evidence**: [VERIFIED: runtime.go — InContainer + ServiceName available; KB — EqualFold recommended]

### Step 5: REFACTOR + verify
- Run `go test ./internal/tools/... -v -count=1`
- Run `go test ./internal/ops/... -v -count=1`  
- Run `go test ./... -count=1 -short`
- Run `make lint-fast`

## Risk Assessment

| # | Risk | Likelihood | Impact | Mitigation | Evidence |
|---|------|-----------|--------|------------|---------|
| 1 | Case sensitivity mismatch | M | M | `strings.EqualFold` | [KB: hostnames lowercase but input arbitrary] |
| 2 | Guard bypassed via service ID | L | H | Input struct only accepts hostname | [VERIFIED: delete.go:16 — ServiceHostname only] |
| 3 | Legitimate delete blocked | L | M | Guard only active when InContainer=true AND hostname matches | [VERIFIED: runtime.go:18-22] |
| 4 | Unmount runs before guard | L | M | Place guard before unmount block | [VERIFIED: delete.go:34-40 — adversarial MF1] |
| 5 | Container detection unreliable | L | L | Same mechanism used by all container features | [VERIFIED: runtime.go:19] |

## Test Plan

| Layer | Tests | Package | Status |
|-------|-------|---------|--------|
| Constant | `ErrSelfDeleteBlocked` compiles | platform | New |
| Tool | `SelfDelete_Blocked` (InContainer=true, matching hostname) | tools | New |
| Tool | `SelfDelete_LocalDev_Allowed` (InContainer=false) | tools | New |
| Tool | `SelfDelete_CaseInsensitive` ("API" vs "api") | tools | New |
| Tool | `OtherService_InContainer_Allowed` (different hostname) | tools | New |
| Tool | All existing delete tests pass with `runtime.Info{}` | tools | Update signatures |
| Ops | Existing tests unmodified (no ops-layer change) | ops | No change |
| E2E | Skip — requires real container env | e2e | N/A |

## Adversarial Challenges

### Challenged Findings
| # | Challenge | Resolution | Evidence |
|---|-----------|-----------|---------|
| CH1 | Guard should be at ops layer, not tools | **Rejected** — tools layer matches deploy_ssh pattern; ops.Delete is a thin API wrapper | deploy_ssh.go:29, ops/delete.go:10-35 |
| CH2 | mount.go doesn't guard hostnames at tools layer | **Accepted but irrelevant** — mount has no self-destruction risk | mount.go:20 |

### Missed Findings (accepted)
| # | Finding | Severity | Resolution |
|---|---------|----------|-----------|
| MF1 | Pre-delete unmount runs before guard check | MAJOR | Guard placed before unmount block (Step 4) |

### Confirmed
- F2: hostname-only input — no ServiceID bypass possible
- F4: ops.Delete has single caller
- F6: `strings.EqualFold` is appropriate for hostname comparison

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| No platform self-delete protection | VERIFIED | Platform verifier: API accepts any service ID |
| runtime.Info available with ServiceName | VERIFIED | runtime.go:8-13, 18-29 |
| RegisterDelete missing rtInfo | VERIFIED | server.go:109, delete.go:22 |
| Guard at tools layer is correct | VERIFIED | deploy_ssh.go:29 pattern, architectural consistency |
| strings.EqualFold sufficient | LOGICAL | Zerops hostnames are DNS-format lowercase |
| Pre-delete unmount timing issue | VERIFIED | delete.go:34-40, adversarial MF1 |
