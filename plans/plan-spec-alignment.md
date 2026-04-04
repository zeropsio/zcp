# Plan: Align Implementation with spec-workflows.md

> **Status**: Phase 1, 2, 4a, 5c DONE. Phase 3 (adoption), 4b-d, 5a-b remaining.
> **Created**: 2026-04-04
> **Spec reference**: `docs/spec-workflows.md`, `docs/spec-knowledge-distribution.md`

---

## Summary

The spec defines how bootstrap, adoption, strategy, and deploy flows should work. The current implementation has significant divergences. This plan describes every change needed to align implementation with spec.

---

## Phase 1: Bootstrap — Remove Strategy (PRIORITY: P1)

Bootstrap should ONLY create infrastructure and write evidence. No strategy selection, no strategy storage.

### 1a. Remove `Strategies` field from BootstrapState

**File**: `internal/workflow/bootstrap.go:40`
**Change**: Delete `Strategies map[string]string` field from struct.
**Impact**: All references to `b.Strategies` or `state.Bootstrap.Strategies` will break — find and remove all.
**Spec ref**: Invariants B7, E2.

### 1b. Remove strategy section from BuildTransitionMessage()

**File**: `internal/workflow/bootstrap_guide_assembly.go:91-115`
**Change**: Delete the entire "## Deploy Strategy" block. Keep: service list with modes, router offerings, utilities.
**Update comment** at line 62: remove "deploy strategy selection" from function description.
**Spec ref**: §2.7 "NO strategy prompt."

### 1c. Always write empty DeployStrategy in writeBootstrapOutputs()

**File**: `internal/workflow/bootstrap_outputs.go:27,41`
**Change**:
- Remove: `strategy := state.Bootstrap.Strategies[devHostname]`
- Change: `DeployStrategy: ""` (hardcoded empty string)
**Spec ref**: §2.7, E2.

### 1d. Delete BootstrapStoreStrategies() function

**File**: `internal/workflow/engine.go:283-298`
**Change**: Delete entire function. Find and remove all callers.
**Spec ref**: B7.

### 1e. Update tests

**File**: `internal/workflow/bootstrap_outputs_test.go`
**Changes**:
- Remove tests that set strategies during bootstrap
- Add test: "bootstrap always writes empty DeployStrategy"
- Remove tests for `BuildTransitionMessage` strategy section
- Update `TestBuildTransitionMessage` to verify NO strategy content
- Fix "ci-cd" references in comments → "push-git"

### 1f. Bootstrap generate — enforce minimal scaffolding (guidance change)

**File**: `internal/content/workflows/bootstrap.md` — generate sections
**Change**: Add clear directive in generate guidance:
> "Write MINIMAL scaffolding — a hello-world server with required endpoints (/, /health, /status). Do NOT implement the user's application logic. That happens in the deploy flow after bootstrap completes."
**Spec ref**: §2.5, B9.

### 1g. Bootstrap close — add natural transition hint

**File**: `internal/workflow/bootstrap_guide_assembly.go` — BuildTransitionMessage()
**Change**: After removing strategy section, add transition guidance:
> "Infrastructure is ready. To implement your application, start the deploy flow: `zerops_workflow action=\"start\" workflow=\"deploy\"`"
**Spec ref**: §2.7, B10.

---

## Phase 2: Deploy Flow — Refactor to Lifecycle Model (PRIORITY: P2)

Deploy flow changes from gate-based 3-step model to inform-based lifecycle.

### 2a. Remove cached Strategy from DeployState

**File**: `internal/workflow/deploy.go:35`
**Change**: Remove `Strategy string` field from DeployState struct.
**Impact**: All references to `state.Strategy`, `d.Strategy`, `ds.Strategy` will break.
**Fix**: Replace with fresh ServiceMeta reads where strategy is needed.
**Spec ref**: D3, S4.

### 2b. Remove strategy gate from handleDeployStart()

**File**: `internal/tools/workflow_deploy.go:53-71`
**Change**:
- Remove lines 53-62: strategy check that blocks session creation
- Remove lines 64-71: manual strategy early exit (no session)
- Instead: ALWAYS create session. Read strategy from meta. Include strategy status in START phase response:
  - If empty: "No strategy set. Work on code, discuss strategy with user before deploying."
  - If set: "Strategy: {value}. Will deploy accordingly. Change anytime."
  - If manual: session still created, guidance says "manual — tell user what to do"
**Spec ref**: D2 "Strategy is informational at start, not a gate."

### 2c. Add fresh strategy read to guidance assembly

**File**: `internal/workflow/deploy_guidance.go`
**Change**: `buildPrepareGuide()` and `buildDeployGuide()` must read ServiceMeta from disk instead of using `state.Strategy`.
**New dependency**: Guidance functions need `stateDir` parameter to read metas.
**Implementation**:
```go
func (d *DeployState) buildGuide(step string, iteration int, env Environment, stateDir string) string {
    // Read current strategy from meta for each target
    for _, t := range d.Targets {
        meta, _ := ReadServiceMeta(stateDir, t.Hostname)
        currentStrategy := ""
        if meta != nil {
            currentStrategy = meta.DeployStrategy
        }
        // Use currentStrategy in guidance
    }
}
```
**Spec ref**: §4.4 "Strategy is always read from meta, never cached."

### 2d. Add pre-deploy strategy resolution

**File**: `internal/tools/workflow_deploy.go` — in handleDeployComplete() for execute step
**Change**: Before executing deploy, read strategy from meta:
- If empty → return response asking agent to discuss with user and set via action="strategy". Step does NOT advance.
- If set → proceed with deploy using that strategy.
**This replaces the current gate at session start with a gate at deploy time.**
**Spec ref**: §4.5 "If empty → agent must discuss with user."

### 2e. Manual strategy — session still runs

**File**: `internal/tools/workflow_deploy.go`
**Change**: Remove `allManualStrategy()` early exit. Manual strategy creates a normal session. Guidance at execute step says: "Strategy is manual. Tell the user what needs to be deployed and how. User will execute the deployment themselves."
**Spec ref**: D7 "manual strategy: agent informs, user executes."

### 2f. Update DeployState.BuildResponse()

**File**: `internal/workflow/deploy.go:264-307`
**Change**: Remove Strategy from response struct (or populate from fresh meta read).
**Spec ref**: D3.

### 2g. Update deploy tests

**Files**: `internal/workflow/deploy_test.go`, `internal/tools/workflow_deploy_test.go`
**Changes**:
- Remove tests for strategy gate blocking
- Add tests: session created regardless of strategy status
- Add tests: strategy read from meta at guidance time
- Add tests: manual strategy creates session with appropriate guidance
- Add tests: empty strategy at execute time returns resolution prompt

---

## Phase 3: Adoption — Simplify (PRIORITY: P3)

### 3a. Create standalone adoption handler

**File**: New `internal/tools/workflow_adopt.go` or extend `workflow_bootstrap.go`
**Change**: Implement simplified adoption:
1. Agent calls `zerops_workflow action="start" workflow="adopt"` (or action="adopt")
2. System verifies service exists and is running
3. System writes ServiceMeta directly (no 5-step session)
4. Returns evidence confirmation

**Alternative**: Keep bootstrap with isExisting but make it truly skip import/generate/deploy:
- Provision: if ALL targets isExisting → skip import, just mount + discover
- Generate: already skips for isExisting
- Deploy: skip entirely for isExisting (just verify)
- This reduces to: discover → (minimal provision) → close

### 3b. Allow empty BootstrapSession for adoption

**File**: `internal/workflow/service_meta.go`
**Change**: For adopted services, BootstrapSession = "" (empty string = null equivalent).
**File**: `internal/workflow/bootstrap_outputs.go` (if adoption goes through bootstrap)
**Change**: If isExisting target, write BootstrapSession as "".
**Spec ref**: E3.

### 3c. Add adoption tests

**Files**: New test files
**Changes**:
- Test: adoption writes ServiceMeta with empty BootstrapSession
- Test: adopted service passes IsComplete()
- Test: adopted service enters deploy flow correctly

---

## Phase 4: Spec Consistency (PRIORITY: P2)

### 4a. Fix spec-knowledge-distribution.md — "ci-cd" → "push-git"

**File**: `docs/spec-knowledge-distribution.md`
**Change**: Find and replace remaining "ci-cd" references with "push-git" (~4 occurrences in §4.6, §8.3).

### 4b. Align deploy step nomenclature

**File**: `docs/spec-knowledge-distribution.md`
**Change**: §5 subsections currently say "Prepare Step", "Execute Step", "Verify Step". These are implementation step names. Either:
- Rename to match spec-workflows.md phases (Start/Work/Pre-deploy/Deploy/Verify), OR
- Add note: "Implementation uses 'prepare/execute/verify' as internal step names. These map to the conceptual phases described in spec-workflows.md §4."
**Decision needed**: Which approach? Recommend the note approach — internal names don't need to match conceptual model exactly.

### 4c. Fix test comments

**File**: `internal/workflow/bootstrap_outputs_test.go:432,548,784`
**Change**: Replace "ci-cd" with "push-git" in test names and comments.

### 4d. Add missing spec details

**File**: `docs/spec-workflows.md`
**Add**:
- Invariant: "Non-discover steps require plan from discover step" (defense-in-depth check in engine.go:143)
- Note: Checker mechanism — StepChecker function blocks step advancement when Passed=false
- Note: Mixed strategies rejected in single deploy session
- Note: completedState cache for post-completion queries

---

## Phase 5: Deploy Flow Enforcement (PRIORITY: P2)

Ensure deploy flow is mandatory for code changes.

### 5a. Init instructions — add deploy flow mandate

**File**: `internal/server/instructions.go` — container and local instruction sections
**Change**: Add to init instructions:
> "For ANY code change to a runtime service, start a deploy flow: zerops_workflow action='start' workflow='deploy'. Do not edit service code without an active deploy flow."
**Spec ref**: D0, §4.1.

### 5b. Bootstrap completion guidance — natural transition

**File**: `internal/content/workflows/bootstrap.md` — close section
**Change**: Add transition text in close step guidance:
> "Infrastructure is ready with minimal scaffolding. If your goal requires application development, start the deploy flow now."
**Spec ref**: B10.

### 5c. Workflow routing — deploy as primary offering post-bootstrap

**File**: `internal/workflow/router.go`
**Change**: After bootstrap completes (evidenced services, no active session), deploy should be the primary offering regardless of strategy status. Currently it only offers deploy when strategy is set.
**Fix**: Offer deploy even when strategy is unset — the flow handles strategy resolution internally.
**Spec ref**: D2 — strategy doesn't gate anything.

---

## Execution Order

```
Phase 1 (Bootstrap cleanup)     ← Start here. Clear scope, breaks nothing external.
  ↓
Phase 4a-4c (Spec fixes)        ← Quick text changes, do alongside Phase 1.
  ↓
Phase 2 (Deploy refactor)       ← Biggest change. Affects guidance, tools, engine.
  ↓
Phase 5 (Deploy enforcement)    ← Depends on Phase 2 being done.
  ↓
Phase 3 (Adoption)              ← Can be deferred. Current bootstrap+isExisting works.
  ↓
Phase 4d (Spec additions)       ← Final polish after all code changes.
```

---

## Risk Assessment

| Phase | Risk | Mitigation |
|-------|------|-----------|
| 1 (Bootstrap) | Tests break due to Strategies removal | Update tests first (RED), then remove code (GREEN) |
| 2 (Deploy) | Strategy cached in many places | Grep for all `Strategy` references before starting |
| 2 (Deploy) | Guidance needs stateDir for meta reads | Thread stateDir through Engine → BuildResponse → buildGuide |
| 3 (Adoption) | New handler vs simplified bootstrap path | Start with simplified bootstrap (less new code) |
| 5 (Enforcement) | Agent ignores init instructions | Enforcement is guidance-level, not code-level. Can't force. |

---

## Definition of Done

- [ ] All spec invariants (§8) pass as testable assertions
- [ ] Bootstrap writes empty DeployStrategy — always
- [ ] Deploy flow creates session regardless of strategy status
- [ ] Strategy always read from ServiceMeta at point of use
- [ ] Manual strategy runs through deploy flow (not shortcut)
- [ ] Generate step produces minimal scaffolding, not full app
- [ ] Init instructions mandate deploy flow for code changes
- [ ] All "ci-cd" references replaced with "push-git"
- [ ] `go test ./... -count=1 -short` passes
