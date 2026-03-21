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
1. Agent deploys `appdev` → `UpdateTarget("appdev", "deployed", "built ok")`
2. Agent verifies → `UpdateTarget("appdev", "verified", "health check passed")`
3. Agent checks `DevFailed()` → if false, proceed to stage
4. Agent deploys `appstage` → `UpdateTarget("appstage", "deployed", "...")`

---

## Current State

### Zero Production Callers

- `UpdateTarget()` — **0 prod callers** (only deploy_test.go:104-115)
- `DevFailed()` — **0 prod callers** (only deploy_test.go:235-250)
- `Error` field — only written by dead `UpdateTarget()`
- `LastAttestation` — only written by dead `UpdateTarget()`

### What IS Used

- `DeployTarget.Status` — initialized in `BuildDeployTargets()`, reset in `ResetForIteration()`, serialized in `BuildResponse()` → `DeployTargetOut`
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
Error and LastAttestation are **not** in the response — agents never see them.

### Persisted But Unused

Error and LastAttestation are serialized to session JSON (`.zcp/state/sessions/{id}.json`) with `omitempty` tags. They're deserialized on load but never read.

---

## Why It's Unused

The deploy workflow is **step-based**, not **target-based**:

```
Step-based (current):  prepare → deploy → verify (all targets together)
Target-based (unused): for each target: deploy → verify → next target
```

The iteration model via `ResetForIteration()` superseded per-target updates. When deploy fails, the agent calls `action="iterate"` which resets ALL targets to pending — no per-target granularity.

Tool handlers (`tools/workflow_deploy.go`) only do step-level completion:
- `handleDeployComplete()` calls `engine.DeployComplete(step, attestation)` — step-level, not target-level
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

**Current multi-service pattern**: Parent agent spawns Service Bootstrap Agents per runtime pair. Each subagent runs the full deploy independently. The parent's deploy step completes when all subagents finish. Per-target tracking at the engine level adds nothing — subagents track their own progress.
