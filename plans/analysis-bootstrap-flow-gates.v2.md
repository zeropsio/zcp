# Bootstrap Flow Gates & Termination — Design v2

**Date**: 2026-03-21
**Based on**: Review 1 findings from team review
**Status**: Proposed — pending implementation

---

## 1. Overview

Three changes to the bootstrap workflow:

1. **Mode gate**: Explicit plan-exists check before non-discover steps
2. **5-step bootstrap**: Remove strategy from bootstrap, close after verify
3. **Strategy as post-bootstrap gate**: Block deploy/cicd until strategy is set

---

## 2. Bootstrap Flow (5 Steps)

### Before (6 steps)
```
discover → provision → generate → deploy → verify → strategy
```

### After (5 steps)
```
discover → provision → generate → deploy → verify → [COMPLETE]
                                                        ↓
                                              transition message
                                              presents strategy choice
                                                        ↓
                                              action="strategy" (independent)
                                                        ↓
                                              deploy / cicd workflow
```

### Step Changes

| Step | Index | Category | Skippable | Change |
|------|-------|----------|-----------|--------|
| discover | 0 | fixed | false | No change |
| provision | 1 | fixed | false | No change |
| generate | 2 | creative | true | No change |
| deploy | 3 | branching | true | No change |
| verify | 4 | fixed | false | Now LAST step. Triggers bootstrap completion. |
| ~~strategy~~ | ~~5~~ | ~~fixed~~ | ~~true~~ | **REMOVED from bootstrap steps** |

### What Happens to Strategy

Strategy becomes a standalone post-bootstrap action. No session required.

**Already exists**: `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}` — this handler (`handleStrategy()` in `workflow_strategy.go`) already works independently of any session. It reads/writes ServiceMeta directly.

**Auto-assignment preserved**: At `writeBootstrapOutputs()`, dev/simple modes still get `push-dev` auto-assigned if no explicit strategy was set. This means:
- **Dev mode**: Bootstrap completes → meta has `deployStrategy: "push-dev"` → deploy workflow available immediately
- **Simple mode**: Same as dev
- **Standard mode**: Bootstrap completes → meta has `deployStrategy: ""` → deploy workflow BLOCKED until strategy is set

---

## 3. Mode Gate (Defense-in-Depth)

### Current State
Mode is determined at discover step (plan submission). Step ordering + non-skippable discover structurally guarantees plan exists before provision. No explicit check.

### Proposed Change
Add explicit check in `BootstrapComplete()`:

```go
// In engine.BootstrapComplete, before processing non-discover steps:
if stepName != StepDiscover && state.Bootstrap.Plan == nil {
    return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
}
```

**Files affected**: `internal/workflow/engine.go` (1 line)

**Rationale**: Defense-in-depth. The step ordering already prevents this, but an explicit check:
- Makes the invariant visible in code
- Produces a clear error message if invariant is violated
- Costs nothing (single nil check)

---

## 4. Bootstrap Termination Redesign

### Current Termination (after strategy step)
```go
// bootstrap_guide_assembly.go:58-88
func BuildTransitionMessage(state *WorkflowState) string {
    // Lists services
    // "What's Next?" with:
    //   A) Continue deploying → deploy workflow
    //   B) Set up CI/CD → cicd workflow
    //   Other: scale, debug, configure
}
```

### New Termination (after verify step)

The transition message must be strategy-aware:

```
func BuildTransitionMessage(state *WorkflowState) string {
    // Lists services with modes

    // Check if all runtime services have strategies set
    // (dev/simple auto-assigned at writeBootstrapOutputs, standard may be empty)

    needsStrategy := false
    for _, target := range plan.Targets {
        mode := target.Runtime.EffectiveMode()
        if mode == PlanModeStandard {
            // Standard mode requires explicit strategy choice
            needsStrategy = true
        }
    }

    if needsStrategy {
        // Present strategy choice INLINE in transition message:
        // "Bootstrap complete. Choose deployment strategy for standard-mode services:"
        // "  - push-dev: SSH push to dev container (prototyping)"
        // "  - ci-cd: Git pipeline trigger (teams, production)"
        // "  - manual: No automation (existing pipelines)"
        // "→ zerops_workflow action=strategy strategies={...}"
        // "After choosing, start deploy or cicd workflow."
    } else {
        // All services have strategies (auto-assigned for dev/simple)
        // Present deploy/cicd options directly
        // "Bootstrap complete. All services ready."
        // Based on dominant strategy:
        //   push-dev → "→ zerops_workflow action=start workflow=deploy"
        //   ci-cd → "→ zerops_workflow action=start workflow=cicd"
    }
}
```

**Files affected**: `internal/workflow/bootstrap_guide_assembly.go`

---

## 5. Deploy/CI-CD Strategy Gate (NEW FEATURE)

**Important**: Deploy/CI-CD workflows currently have NO strategy gate. `handleDeployStart()` checks metas exist and are complete (`BootstrappedAt` set) but never reads `DeployStrategy`. Adding strategy gates is a NEW feature that enforces strategy-first-then-deploy ordering.

### Deploy Workflow Gate

In `handleDeployStart()`, after reading ServiceMeta files, check that every runtime meta has a non-empty `DeployStrategy`:

```
// Before building deploy targets:
for _, m := range runtimeMetas {
    if m.DeployStrategy == "" {
        return error: "Strategy not set for {hostname}.
          Set strategy first: zerops_workflow action=strategy strategies={\"hostname\":\"push-dev|ci-cd|manual\"}"
    }
}
```

**Files affected**: `internal/tools/workflow.go` (handleDeployStart)

### CI/CD Workflow Gate

In `handleCICDStart()`, similar check — ensure at least one service has `ci-cd` strategy:

```
// Check at least one service has ci-cd strategy
hasCICD := false
for _, m := range metas {
    if m.DeployStrategy == StrategyCICD {
        hasCICD = true
    }
}
if !hasCICD {
    return error: "No services configured for CI/CD strategy.
      Set strategy first: zerops_workflow action=strategy strategies={\"hostname\":\"ci-cd\"}"
}
```

**Files affected**: `internal/tools/workflow.go` (handleCICDStart)

### Router Enhancement

Add strategy-awareness to router offerings:

```
// In Route(), after filtering metas:
// Check if any runtime meta has empty DeployStrategy
needsStrategy := false
for _, m := range metas {
    if (m.Mode != "" || m.StageHostname != "") && m.DeployStrategy == "" {
        needsStrategy = true
    }
}

if needsStrategy {
    // Inject p0 offering: "Set deployment strategy"
    offerings = append(offerings, FlowOffering{
        Workflow: "strategy",
        Priority: 0,
        Reason:   "Deployment strategy not set — required before deploy or CI/CD",
        Hint:     `zerops_workflow action="strategy" strategies={"hostname":"push-dev|ci-cd|manual"}`,
    })
}
```

**Files affected**: `internal/workflow/router.go` (Route function)

---

## 6. Managed-Only Bootstrap Fix

### Problem
`ValidateBootstrapTargets` requires `len(targets) > 0`, blocking managed-only projects.

### Solution
Allow empty targets when dependencies exist. The validation function needs a way to receive dependencies without targets.

**Option A** (minimal): Change validation to allow `len(targets) == 0` — skip target validation, return success.

**Option B** (better): Accept top-level dependencies in the plan alongside targets. This requires extending `BootstrapCompletePlan()` to accept dependencies without a runtime target.

**Recommendation**: Option A for now. Managed-only projects submit an empty targets array. The managed-only fast path (skip generate/deploy/strategy) already works correctly with `validateConditionalSkip()`.

**Files affected**: `internal/workflow/validate.go` (remove len check)

---

## 7. Implementation Order

1. **[Pre-work] Fix managed-only validation** — `validate.go` (unblocks a real use case)
2. **Remove strategy step** — `bootstrap_steps.go` (drop from stepDetails), `bootstrap.go` (update step constants)
3. **Update writeBootstrapOutputs** — auto-assign still works (just happens after verify now)
4. **Redesign transition message** — `bootstrap_guide_assembly.go` (strategy-aware)
5. **Add deploy/cicd strategy gates** — `workflow.go` (handleDeployStart, handleCICDStart)
6. **Add router strategy offering** — `router.go` (p0 "set strategy" when empty)
7. **Add mode gate** — `engine.go` (Plan!=nil check)
8. **Remove strategy checker** — `workflow_checks_strategy.go`, `workflow_checks.go` (no longer needed in bootstrap)
9. **Update tests** — bootstrap_test.go (5 steps), router_test.go (strategy offering), new strategy-gate tests
10. **Update spec** — `docs/spec-bootstrap-deploy.md` (5-step bootstrap)

### Files Modified

| File | Change |
|------|--------|
| `internal/workflow/bootstrap_steps.go` | Remove StepStrategy from stepDetails (5 steps) |
| `internal/workflow/bootstrap.go` | Remove strategy-related skip constants, update step count references |
| `internal/workflow/bootstrap_outputs.go` | No change (auto-assign still works) |
| `internal/workflow/bootstrap_guide_assembly.go` | Redesign BuildTransitionMessage (strategy-aware) |
| `internal/workflow/engine.go` | Add Plan!=nil check in BootstrapComplete |
| `internal/workflow/router.go` | Add p0 "set strategy" offering when DeployStrategy empty |
| `internal/workflow/validate.go` | Allow len(targets)==0 for managed-only |
| `internal/tools/workflow.go` | Add strategy gate in handleDeployStart, handleCICDStart |
| `internal/tools/workflow_checks.go` | Remove strategy case from buildStepChecker |
| `internal/tools/workflow_checks_strategy.go` | DELETE (checker no longer part of bootstrap) |
| `internal/tools/workflow_strategy.go` | No change (standalone action still works) |
| `docs/spec-bootstrap-deploy.md` | Update to 5-step bootstrap |

### What Stays the Same

- `action="strategy"` tool handler — works independently, no session needed
- `handleStrategy()` — reads/writes ServiceMeta directly
- Strategy validation (validStrategies map) — still validates push-dev/ci-cd/manual
- `StrategyToSection` map — still drives guidance extraction
- Auto-assign at writeBootstrapOutputs — still sets push-dev for dev/simple
- CI/CD workflow — still separate 3-step workflow, unchanged
- ServiceMeta structure — `DeployStrategy` field stays, just populated post-bootstrap

---

## 8. Edge Cases

### Standard mode, no strategy set after bootstrap
- Bootstrap completes (5 steps). Meta has `deployStrategy: ""`
- Transition message: "Choose strategy for standard-mode services"
- Router: p0 "set strategy" offering
- Deploy workflow: BLOCKED with clear error
- Resolution: User calls `action="strategy"`, then starts deploy

### Dev/simple mode, auto-assigned
- Bootstrap completes. `writeBootstrapOutputs` auto-assigns `push-dev`
- Transition message: "All services ready. Deploy: ..."
- Router: p1 deploy offering (push-dev strategy detected)
- Deploy workflow: proceeds normally

### Mixed modes (standard + dev)
- Bootstrap completes. Dev services get push-dev auto-assigned. Standard services have empty strategy.
- Transition message: "Choose strategy for standard-mode services. Dev services ready for deploy."
- Router: p0 "set strategy" (some services missing), p1 deploy (for services that have strategy)
- Deploy workflow: BLOCKED on services without strategy

### Managed-only project
- Bootstrap completes (discover → provision → SKIP generate → SKIP deploy → verify)
- No runtime services → no strategy needed
- Transition message: "All managed services running."
- Router: utilities only (debug, scale, configure)

---

## 9. Spec-vs-Implementation Discrepancies Found

| # | Spec Claim | Implementation | Status |
|---|-----------|----------------|--------|
| 1 | Strategy is step 6 of 6 | checkStrategy exists and enforces (architect was wrong about it missing) | MATCH (but being changed) |
| 2 | Managed-only: "code gap: currently requires len(targets)>0" | Confirmed: `validate.go:128` | DOCUMENTED GAP |
| 3 | Mode chosen in discover step | Confirmed: plan submitted via BootstrapCompletePlan | MATCH |
| 4 | Strategy auto-assigned for dev/simple | Confirmed: `bootstrap_outputs.go:27-29` | MATCH |
