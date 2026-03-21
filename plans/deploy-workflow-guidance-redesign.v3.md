# Deploy Workflow Guidance Redesign — v3 (Final)

**Date**: 2026-03-21
**Status**: Ready for implementation — all phases unblocked
**Base**: `deploy-workflow-guidance-redesign.v2.md` (all content carries forward)
**Reviews**: `review-1.md` (R1: 15/15 verified), `review-2.md` (R2: 4-agent analysis, 15/17 verified, 0 refuted)
**Philosophy spec**: `docs/spec-guidance-philosophy.md`
**Workflow spec**: `docs/spec-bootstrap-deploy.md`

---

## Changes from v2 (review-2 amendments)

### Amendment 1: Phase 1 — DeployTarget.Status field KEPT (sequencing fix)

**Problem identified by R2 adversarial**: Removing `DeployTarget.Status` in Phase 1 breaks `BuildResponse` at deploy.go:309 which reads `t.Status`. Between Phase 1 and Phase 3, no source for `DeployTargetOut.Status`.

**Resolution**: v2 Phase 1 already correctly omits Status field removal from the delete list (only removes Error, LastAttestation, dead methods, dead constants). Status field remains until Phase 3 where checker results populate `DeployTargetOut.Status` via a new mapping. **No change needed — v2 is correct.**

Explicitly: Phase 1 removes:
- `UpdateTarget()` method
- `DevFailed()` method
- `DeployTarget.Error` field
- `DeployTarget.LastAttestation` field
- 4 dead status constants (deployed, verified, failed, skipped)
- `ResetForIteration()` Error clear line
- `ResolveDeployGuidance()` exported function
- Dead tests (6 total)

Phase 1 KEEPS:
- `DeployTarget.Status` field (used by BuildResponse:309)
- `deployTargetPending` constant (used by BuildDeployTargets + ResetForIteration)

### Amendment 2: Phase 3 — Existing test file naming clarification

**Finding from R2 kb-verifier**: `tools/workflow_checks_deploy_test.go` (90 lines) already exists but tests **bootstrap's** `checkDeploy` function (from workflow_checks.go:145), NOT deploy workflow checkers.

**Resolution**: Phase 3 deploy workflow checker tests should go in a NEW file: `tools/workflow_checks_deploy_workflow_test.go`. The existing file tests bootstrap infrastructure and should remain unchanged (or be renamed to `workflow_checks_deploy_bootstrap_test.go` for clarity).

**Updated Phase 3 file table**:

| File | Change | Lines |
|------|--------|-------|
| workflow/engine.go | Add ctx to DeployComplete + DeployStart | +20 |
| workflow/deploy.go or deploy_checks.go | DeployStepChecker type | +5 |
| tools/workflow_deploy.go | Wire ctx + checker (follow `buildStepChecker` pattern from `workflow_checks.go`) | +35 |
| tools/workflow_checks_deploy.go | `buildDeployStepChecker` + `checkDeployPrepare` + `checkDeployResult` | +120 |
| **tools/workflow_checks_deploy_workflow_test.go** | Deploy workflow checker tests (6 scenarios) | +80 |

### Amendment 3: Phase 3 — Checker wiring pattern reference

**Finding from R2 adversarial**: Plan underspecifies how `buildDeployStepChecker` gets its dependencies (client, projectID, stateDir).

**Resolution**: Follow the existing pattern from `tools/workflow_checks.go:buildStepChecker`:

```go
// Pattern from bootstrap (tools/workflow_checks.go):
func buildStepChecker(step string, client platform.Client, ..., projectID string, ..., stateDir string) workflow.StepChecker {
    switch step {
    case workflow.StepProvision:
        return checkProvision(client, ...)
    // ...
    }
}

// Deploy equivalent (tools/workflow_checks_deploy.go):
func buildDeployStepChecker(step string, client platform.Client, projectID string, stateDir string) workflow.DeployStepChecker {
    switch step {
    case workflow.DeployStepPrepare:
        return checkDeployPrepare(client, projectID, stateDir)
    case workflow.DeployStepDeploy:
        return checkDeployResult(client, projectID)
    default:
        return nil // verify step: no checker (informational)
    }
}
```

The handler (`handleDeployComplete`) already has access to `client`, `projectID`, and engine's `stateDir` from the tool handler scope — same as `handleBootstrapComplete`.

### Amendment 4: Verification checklist — Phase 3 updated

Replace Phase 3 checklist item:
```
- [ ] Tests in NEW file: workflow_checks_deploy_workflow_test.go (not in existing bootstrap test file)
```

### Amendment 5: R2 Disputed Findings — All Resolved

| Dispute | Resolution | Evidence |
|---------|-----------|----------|
| ctx missing = "BLOCKING"? | NOT BLOCKING — plan explicitly schedules addition in Phase 3 | Plan v2 §5 Phase 3 line 385: "+20 lines for ctx" |
| BuildIterationDelta nil plan safety | SAFE — `_ *ServicePlan` parameter ignored. Tests at bootstrap_guidance_test.go:467 call with nil. | bootstrap_guidance.go:94: underscore param |
| StepDeploy vs DeployStepDeploy fragile? | INTENTIONAL — same string value "deploy" by design. Plan v1 §2.4 documents this. | Add code comment during implementation |
| stateDir same for deploy and bootstrap? | YES — Engine.stateDir set once at construction (engine.go:27), used by all workflows | engine.go:27: `stateDir: baseDir` |

---

## Implementation Readiness Summary

| Phase | Status | Blockers | Confidence |
|-------|--------|----------|------------|
| 1: Dead Code + Bugs | READY | None | HIGH (all dead code verified by 6+ agents) |
| 2: Guidance Redesign | READY | None (RuntimeType: Decision #18 resolved) | HIGH |
| 3: Checkers | READY | None (ctx addition is planned work, not gap) | HIGH |
| 4: Init Instructions | READY | Integration point needs code read (LOW risk) | MEDIUM |
| 5: Transitions | READY | None | HIGH |
| 6: Route | DECISION NEEDED | Refactor vs document exception (Open Q#7) | MEDIUM |

**Phases 1-5**: GO. All details resolved, all claims verified, architecture sound.

**Phase 6**: Decide router philosophy before starting. Two options documented in v2 §5 Phase 6.

---

## All other content from v2 carries forward unchanged:
- §1: Problem Statement
- §2: What's Already Done
- §3: Design Decisions (20 decisions + 8 rejected alternatives)
- §4: Guidance Assembly templates (prepare, deploy, verify)
- §5: Implementation Plan (Phases 1-6 with file tables)
- §6: File Impact Summary
- §7: Risks & Mitigations
- §8: Open Questions
- §9: Verification Checklist
- §10: Decision Record
