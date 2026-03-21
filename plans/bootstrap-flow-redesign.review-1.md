# Deep Review Report: bootstrap-flow-redesign — Review 1
**Date**: 2026-03-21
**Reviewed version**: `plans/bootstrap-flow-redesign.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: orchestrator-only (Stage 2 agents timed out in plan mode)
**Focus**: Analyze the plan, recommend best approach, prepare implementation plan
**Resolution method**: Evidence-based (orchestrator analysis with Stage 1 KB)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**checkDeploy** (`workflow_checks.go:148-211`): API status only (RUNNING/ACTIVE) + SubdomainAccess. No VerifyAll, no health checks, no log inspection.

**checkVerify** (`workflow_checks.go:213-265`): Calls `ops.VerifyAll()` — full health: service_running, error_logs, startup_detected, http_root, http_status. Filters to plan hostnames only.

**writeBootstrapOutputs** (`bootstrap_outputs.go:13-52`): Auto-assigns `push-dev` for dev/simple when strategy empty. Standard mode gets empty string. Best-effort — errors logged to stderr, not returned.

**handleDeployStart** (`workflow.go:187-236`): NO strategy gate. Reads metas, checks IsComplete(), filters to runtime services, calls BuildDeployTargets. Never reads DeployStrategy.

**handleCICDStart** (`workflow.go:239-269`): NO strategy gate. Collects hostnames with mode/stage, never checks strategy.

**ValidateBootstrapTargets** (`validate.go:128-129`): Hard error on `len(targets)==0`. Blocks managed-only projects.

**ResetForIteration** (`bootstrap.go:112-126`): Hardcoded `for i := 2; i <= 4`. Resets generate(2), deploy(3), verify(4). Preserves discover(0), provision(1), strategy(5).

**validateConditionalSkip** (`bootstrap.go:325-336`): Only guards `generate` and `deploy` skip when runtime services exist. No guard for verify or strategy.

**StepVerify**: `Skippable: false` (`bootstrap_steps.go:48-53`). Managed-only cannot skip it.

**BootstrapComplete** (`engine.go:133-186`): When `!state.Bootstrap.Active`: calls writeBootstrapOutputs, then ResetSessionByID (deletes session). No Plan!=nil check. No final gate.

**handleStrategy** (`workflow_strategy.go:25`): Standalone — engine param unused (underscore). Reads/writes ServiceMeta directly, no session required.

**BuildTransitionMessage** (`bootstrap_guide_assembly.go:57-88`): Generic "What's Next?" with deploy/cicd/scale/debug/configure. No strategy mention, no mode-specific variants.

**strategyOfferings** (`router.go:223-277`): Reads DeployStrategy from metas. Empty strategy → nil return → generic deploy offering.

**deploy.md**: Has strategy-specific execution sections (`deploy-push-dev`, `deploy-ci-cd`, `deploy-manual`) but NO strategy selection section. Strategy selection only lives in bootstrap.

**StrategyToSection** map: Maps strategy → deploy.md section names. Used by `buildStrategyGuidance()` in `workflow_strategy.go`.

### Platform Verification Results (kb-verifier)

| # | Claim | Result | Evidence |
|---|-------|--------|----------|
| 1 | Strategy is ZCP-only metadata | CONFIRMED | Zero refs in `internal/platform/` |
| 2 | checkStrategy exists at workflow_checks_strategy.go:11-72 | CONFIRMED | File verified |
| 3 | handleDeployStart has NO strategy gate | CONFIRMED | No DeployStrategy reference |
| 4 | handleCICDStart has NO strategy gate | CONFIRMED | No strategy reference |
| 5 | writeBootstrapOutputs auto-assigns push-dev | CONFIRMED | bootstrap_outputs.go:27-29 |
| 6 | ValidateBootstrapTargets blocks empty targets | CONFIRMED | validate.go:128-129 |
| 7 | ResetForIteration resets indices 2-4 | CONFIRMED | bootstrap.go:117 |
| 8 | checkDeploy is weak (API status only) | CONFIRMED | No VerifyAll, no health |
| 9 | checkVerify calls VerifyAll | CONFIRMED | workflow_checks.go:219 |
| 10 | StepVerify Skippable: false | CONFIRMED | bootstrap_steps.go:48 |
| 11 | Live project has only zcpx service | CONFIRMED | zerops_discover |

---

## Stage 2: Orchestrator Analysis (replacing timed-out agents)

### Correctness Analysis

**Assessment**: CONCERNS — 4 issues need resolution before implementation

#### Findings

- **[F1] ResetForIteration will reset close step** — CRITICAL — With close at index 4 and the proposed range change to "2-3" (generate+deploy), the plan is correct. BUT the plan text in Section 4 says "ResetForIteration resets steps 2-3 (generate, deploy)" while the implementation plan Phase 1 task 3 says "Update iteration reset" without specifying the new range. The plan must be explicit: `for i := 2; i <= 3`. Close (index 4) must NOT be in the iteration loop — it's administrative. [KB-FACT: ResetForIteration hardcodes 2-4, bootstrap.go:117]

- **[F2] writeBootstrapOutputs double-execution risk** — MAJOR — Currently writeBootstrapOutputs runs in `engine.go:173-174` when `!state.Bootstrap.Active` (after last step completes). If close is the last step, completing close makes Active=false, triggering writeBootstrapOutputs. BUT the plan says close step itself "writes final ServiceMeta files." This creates a double-write: close step writes metas → step completes → Active becomes false → writeBootstrapOutputs writes metas AGAIN. Resolution: writeBootstrapOutputs must be the ONLY place metas are written, triggered by close completion. Close step's "job" is to be the trigger point, not to duplicate the write. [KB-FACT: engine.go:173-174, bootstrap_outputs.go:13-52]

- **[F3] Managed-only path hits non-skippable verify (currently) — but plan removes verify** — MINOR — The plan correctly removes verify as a separate step, so this concern from the KB disappears. With the 5-step flow (close is Skippable: true), managed-only skips generate+deploy+close. Path: discover → provision → DONE. But validateConditionalSkip needs a new guard for close (currently only guards generate+deploy). [KB-FACT: validateConditionalSkip at bootstrap.go:325-336, StepVerify Skippable:false at bootstrap_steps.go:48]

- **[F4] Strategy gate blocks pre-existing bootstrapped services** — MAJOR — If the deploy strategy gate is added to handleDeployStart, services bootstrapped BEFORE this change have empty DeployStrategy (standard mode). They would be permanently blocked from deploying until someone manually calls `action=strategy`. The plan mentions this in the risk table ("Only blocks when DeployStrategy=''") but doesn't provide a migration path. [KB-FACT: handleDeployStart at workflow.go:187-236, writeBootstrapOutputs auto-assigns only for dev/simple]

### Architecture Analysis

**Assessment**: SOUND — clean separation maintained

#### Findings

- **[F5] Merging verify into deploy is architecturally clean** — The plan correctly moves VerifyAll from checkVerify (tools/) into checkDeploy (tools/). Both are in the same package, same layer. The ops.VerifyAll function stays in ops/ where it belongs. No dependency direction violation. checkDeploy already receives `client` and `projectID`; it would additionally need `fetcher` (LogFetcher) and `httpClient` (HTTPDoer) — same params checkVerify already uses. [VERIFIED: workflow_checks.go function signatures]

- **[F6] Close step fits the existing step architecture** — Steps are data-driven via `stepDetails` array. Adding close = adding one entry. The close checker can be nil (administrative step). This matches existing patterns — no architectural novelty needed. [VERIFIED: bootstrap_steps.go:18-61]

- **[F7] Strategy gate is in the correct layer** — The plan places the gate in `handleDeployStart` (tools/ package), which is where workflow-start validation already happens (IsComplete check, runtime filtering). This is correct — tools/ is the boundary layer that validates inputs before delegating to engine. [VERIFIED: workflow.go:187-236]

- **[F8] buildStepChecker needs parameter update** — The function at `workflow_checks.go:23-37` passes different params to each checker. Merging verify into deploy means checkDeploy needs `fetcher` and `httpClient` params that it currently doesn't receive. The `buildStepChecker` signature already has these (`fetcher platform.LogFetcher, httpClient ops.HTTPDoer`), so no interface change needed — just wire them to checkDeploy. [VERIFIED: workflow_checks.go:23]

### Security Analysis

**Assessment**: SOUND — no new security concerns introduced

#### Findings

- **[F9] Strategy values are properly validated** — `validStrategies` map in workflow_strategy.go:15-19 restricts to push-dev/ci-cd/manual. ServiceMeta uses JSON marshaling (no injection vector). The strategy gate would compare against empty string — safe. [VERIFIED: workflow_strategy.go:15-19, service_meta.go:22-29]

- **[F10] Best-effort meta writes are acceptable for close** — writeBootstrapOutputs already uses best-effort (stderr logging). Close step doesn't change this risk profile. Meta writes use atomic rename (tmp → final) which is safe. [VERIFIED: service_meta.go:39-58, bootstrap_outputs.go:40-41]

### Adversarial Analysis

**Assessment**: CONCERNS — 2 gaps the plan doesn't address

#### Findings

- **[F11] Plan Section 9 managed-only close is ambiguous** — The plan says "close → SKIP (or: write metas for managed services, present 'done')". The "or" creates implementation ambiguity. Resolution: close should SKIP for managed-only (no runtime services = no metas to write, no strategy to present). Managed services are API-authoritative per the codebase convention. [KB-FACT: bootstrap_outputs.go only writes metas for runtime targets]

- **[F12] Plan implementation Phase 4 task 16 mentions "add close skip logic" but Phase 1 task 2 doesn't account for it** — The step replacement (task 2) and skip guard update (task 16) are in different phases but are logically coupled. If task 2 is done without task 16, close becomes non-skippable for managed-only until Phase 4. This should be Phase 1. [VERIFIED: plan section 10]

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| F1 | ResetForIteration range must be explicitly 2-3 (not 2-4) | CRITICAL | bootstrap.go:117 hardcodes 2-4, plan says 2-3 but impl plan is vague | Correctness |
| F2 | writeBootstrapOutputs double-execution when close completes | MAJOR | engine.go:173 triggers on !Active, close completion makes Active=false | Correctness |
| F4 | Strategy gate blocks pre-existing services without migration | MAJOR | handleDeployStart has no strategy check today; adding one breaks existing | Correctness |
| F5 | Verify→deploy merge is architecturally clean | — (positive) | Same package, same layer, params available | Architecture |
| F7 | Strategy gate correctly placed in tools/ | — (positive) | Matches existing validation pattern | Architecture |
| F8 | buildStepChecker already has needed params | — (positive) | workflow_checks.go:23 signature | Architecture |
| F12 | Close skip guard should be Phase 1, not Phase 4 | MINOR | Plan phases 1 vs 4 logical coupling | Adversarial |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| F3 | Managed-only path works with 5-step flow | — (positive) | verify removed → no non-skippable blocker after provision | Correctness |
| F6 | Close step fits existing architecture | — (positive) | stepDetails is data-driven, adding entry is trivial | Architecture |
| F11 | Managed-only close should SKIP (not "or") | MINOR | No runtime targets → no metas → no strategy → skip | Adversarial |

#### UNVERIFIED

None — all findings backed by code evidence.

### Key Insights from Knowledge Base

1. **The plan is fundamentally SOUND.** All 11 factual claims confirmed. The 6→5 step reduction is well-motivated and architecturally clean.

2. **The biggest risk is F2 (double-write)** — the plan describes close step writing metas, but the existing engine.go:173 writeBootstrapOutputs trigger would ALSO fire. This must be resolved by design: either close step is purely a "trigger point" (the engine writes metas when Active→false), or the engine trigger is moved into the close checker/handler.

3. **F4 (migration) is a real gap** — any standard-mode service bootstrapped before this change has empty DeployStrategy. The strategy gate would brick them. Need either: (a) migration script, (b) soft gate (warn but allow), or (c) auto-assign `push-dev` as default for all modes.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

1. **[F1] Specify iteration reset range explicitly** — Plan must state: `for i := 2; i <= 3` (generate + deploy only). Close is excluded from iteration.

2. **[F2] Resolve double-write** — Two options:
   - **Option A (recommended)**: Close step is a pure trigger. Its checker is nil. When close completes, `!Active` triggers existing `writeBootstrapOutputs`. No code moves — just the step acts as a gate.
   - **Option B**: Move writeBootstrapOutputs logic into close step handler. Remove the `!Active` trigger in engine.go. More invasive.

3. **[F4] Add migration path for strategy gate** — Three options:
   - **Option A (recommended)**: Auto-default empty DeployStrategy to `push-dev` in handleDeployStart (not in meta — just as runtime default). This preserves existing behavior while enabling the gate.
   - **Option B**: Soft gate — warn but allow deploy without strategy.
   - **Option C**: Migration script to backfill metas.

### Should Address (LOGICAL + VERIFIED Minor)

4. **[F12] Move close skip guard to Phase 1** — validateConditionalSkip update should be in Phase 1 alongside step replacement, not Phase 4.

5. **[F11] Resolve managed-only close ambiguity** — Close should SKIP for managed-only. Remove the "or" from Section 9.

---

## Revised Version

# Bootstrap Flow Redesign — Complete Specification (v2)

**Date**: 2026-03-21
**Status**: Design reviewed, implementation-ready
**Branch**: v2
**Predecessor**: `plans/bootstrap-flow-redesign.md` (v1), review-1

---

## 1. Motivation

The current 6-step bootstrap flow has three design issues:

1. **Strategy doesn't belong in bootstrap.** Strategy (push-dev / ci-cd / manual) is a deployment maintenance decision, not an infrastructure setup concern. It's ZCP-only metadata — never sent to the Zerops API (verified: zero "strategy" references in `internal/platform/`). Dev/simple modes auto-assign `push-dev` anyway. Forcing the choice inside bootstrap delays completion without adding value.

2. **Deploy and verify have unclear boundaries.** Deploy's checker (`checkDeploy`) only validates API status (RUNNING + subdomain). Verify's checker (`checkVerify`) does full health validation (HTTP endpoints, error logs, startup detection). The deploy guidance tells the agent to verify per-service, but the deploy checker doesn't enforce this. Verify re-runs the same checks the agent already did during deploy.

3. **No post-bootstrap gate for strategy.** Deploy and CI/CD workflows currently have NO strategy check — `handleDeployStart()` never reads `DeployStrategy` from ServiceMeta. There's no enforcement that strategy is set before deploying.

---

## 2. Current State (6 Steps)

```
discover → provision → generate → deploy → verify → strategy
```

*(Unchanged from v1 — see original for full table)*

---

## 3. Proposed Design (5 Steps)

```
discover → provision → generate → deploy → close
```

### Step definitions

```go
var stepDetails = []StepDetail{
    {Name: "discover",  Skippable: false},
    {Name: "provision", Skippable: false},
    {Name: "generate",  Skippable: true},
    {Name: "deploy",    Skippable: true},
    {Name: "close",     Skippable: true},
}
```

- **close** is `Skippable: true` because managed-only projects skip it (no runtime services → nothing to close).
- **deploy** checker now runs `VerifyAll()` instead of just API status checks.
- **close** checker is nil — its job is administrative (trigger writeBootstrapOutputs), not validation.

---

## 4. Deploy Step Redesign

*(Deploy guidance phases unchanged from v1)*

### Deploy checker (strengthened)

```go
func checkDeploy(client, fetcher, projectID, httpClient) StepChecker {
    return func(ctx, plan, state) (*StepCheckResult, error) {
        // 1. Run full VerifyAll — HTTP health, error logs, startup detection
        result, err := ops.VerifyAll(ctx, client, fetcher, httpClient, projectID)
        // 2. Filter to plan targets only
        // 3. Check each plan target is healthy
        // 4. Check subdomains enabled for services with ports
        // 5. Return per-service breakdown
    }
}
```

### Iteration with merged deploy

**Current**: `ResetForIteration()` resets steps 2-4 (generate, deploy, verify).
**New**: `ResetForIteration()` resets steps 2-3 (generate, deploy) **only**.

```go
// Explicit range: generate(2) + deploy(3). Close(4) is administrative — NOT retried.
for i := 2; i <= 3 && i < len(b.Steps); i++ {
    b.Steps[i] = BootstrapStep{Name: b.Steps[i].Name, Status: stepPending}
}
```

Close is excluded from iteration because it's a completion trigger, not a retryable operation.

---

## 5. Close Step

### Purpose

Administrative closure of bootstrap. Close step is a pure trigger point — its completion makes `Active=false`, which triggers the existing `writeBootstrapOutputs` in `engine.go:173`. No duplication of meta-write logic.

Specifically, `writeBootstrapOutputs` (triggered by `!Active`):
1. Writes final ServiceMeta files (with `BootstrappedAt` timestamp)
2. Auto-assigns `push-dev` strategy for dev/simple modes
3. Appends reflog entry to CLAUDE.md

The close step's **response** (not checker) includes the strategy-aware transition message.

### Checker

Nil. Close doesn't validate infrastructure — it records outcomes via the existing `writeBootstrapOutputs` trigger.

### Transition message (strategy-aware)

*(4 mode variants unchanged from v1)*

---

## 6. Post-Bootstrap Strategy Gate

### Deploy workflow gate

In `handleDeployStart()`, after filtering runtime metas:

```go
for _, m := range runtimeMetas {
    if m.DeployStrategy == "" {
        // Auto-default to push-dev for backward compatibility
        m.DeployStrategy = workflow.StrategyPushDev
        _ = workflow.WriteServiceMeta(engine.StateDir(), m)
    }
}
```

**Design choice**: Auto-default empty strategy to `push-dev` rather than blocking. This ensures pre-existing bootstrapped services (from before the strategy gate) continue working. Standard-mode services get `push-dev` as a safe default — users can upgrade to `ci-cd` via `action=strategy` later.

### CI/CD workflow gate

In `handleCICDStart()`, check at least one service has `ci-cd` strategy:

```go
hasCICD := false
for _, m := range metas {
    if m.DeployStrategy == workflow.StrategyCICD {
        hasCICD = true
    }
}
if !hasCICD {
    return error: "No services have ci-cd strategy. Set strategy first."
}
```

### Router enhancement

*(Unchanged from v1)*

---

## 7. Mode Gate (Defense-in-Depth)

*(Unchanged from v1)*

---

## 8. Managed-Only Bootstrap Fix

*(Unchanged from v1)*

---

## 9. Complete Bootstrap Flow by Mode

### Managed-only

```
discover    → identify managed services, submit plan (empty targets)
provision   → import managed services, discover env vars
generate    → SKIP (no runtime services)
deploy      → SKIP (no runtime services)
close       → SKIP (no runtime services → no metas, no strategy)
```

Close is explicitly SKIPPED for managed-only (not "or"). Managed services are API-authoritative — no ServiceMeta files needed.

*(Other modes unchanged from v1)*

---

## 10. Implementation Plan

### Phase 1: Structural changes (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 1 | Fix managed-only validation | `validate.go` | Add managed-only test case |
| 2 | Replace strategy with close in step list | `bootstrap_steps.go` | Update step count assertions |
| 3 | Update iteration reset range to 2-3 | `bootstrap.go` (ResetForIteration) | Verify close NOT reset |
| 4 | Add close skip guard to validateConditionalSkip | `bootstrap.go` | Test managed-only skips close |
| 5 | Add mode gate (Plan!=nil) | `engine.go` | Add Plan-nil test |

### Phase 2: Deploy checker strengthening (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 6 | Merge VerifyAll into checkDeploy | `workflow_checks.go` | Update deploy checker tests |
| 7 | Remove separate checkVerify | `workflow_checks.go` | Remove verify checker tests |
| 8 | Wire fetcher+httpClient to checkDeploy in buildStepChecker | `workflow_checks.go` | Verify params passed |

### Phase 3: Transition and gates (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 9 | Redesign BuildTransitionMessage (4 mode variants) | `bootstrap_guide_assembly.go` | Test all 4 variants |
| 10 | Add deploy strategy auto-default gate | `workflow.go` (handleDeployStart) | Test deploy with empty strategy → auto push-dev |
| 11 | Add CI/CD strategy gate | `workflow.go` (handleCICDStart) | Test cicd blocked without ci-cd |
| 12 | Add router strategy offering | `router.go` | Test p0 offering when strategy empty |

### Phase 4: Cleanup

| # | Task | Files |
|---|------|-------|
| 13 | Delete strategy checker | `workflow_checks_strategy.go` (DELETE), `workflow_checks.go` (remove case) |
| 14 | Delete strategy checker tests | `workflow_checks_strategy_test.go` (DELETE) |
| 15 | Update bootstrap.md content | `internal/content/workflows/bootstrap.md` |

---

## 11. Design Decisions Log

| # | Decision | Why | Alternative | Why rejected |
|---|----------|-----|-------------|--------------|
| 1 | Remove strategy from bootstrap | ZCP-only metadata, auto-assign for dev/simple | Keep as step 6 | Delays bootstrap without value |
| 2 | Merge verify into deploy | Eliminate redundant re-check | Keep separate | Iteration works fine merged |
| 3 | Flat steps, not sub-steps | Engine stays simple | Nested sub-steps | Guidance phases achieve same clarity |
| 4 | Close as pure trigger | Avoids double-write with writeBootstrapOutputs | Close writes metas directly | Double-write risk (F2) |
| 5 | Auto-default strategy to push-dev | Backward compat for pre-existing metas | Hard gate blocking deploy | Bricks existing services (F4) |
| 6 | Close skip guard in Phase 1 | Logically coupled with step replacement | Phase 4 | Would break managed-only until Phase 4 (F12) |
| 7 | Managed-only close: SKIP | No runtime → no metas → no strategy | "Or: write managed metas" | Managed are API-authoritative |

---

## 12. Risk Assessment

*(Unchanged from v1, with addition:)*

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Pre-existing metas blocked by strategy gate | HIGH | HIGH | Auto-default to push-dev (Decision 5) |

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | §4 Iteration | Explicit range 2-3, close excluded | bootstrap.go:117 hardcodes 2-4 | [F1] |
| 2 | §5 Close | Pure trigger, no direct meta writes | engine.go:173 double-write risk | [F2] |
| 3 | §6 Deploy gate | Auto-default push-dev instead of hard block | handleDeployStart has no strategy today | [F4] |
| 4 | §9 Managed-only | Close explicitly SKIP, removed "or" | bootstrap_outputs.go writes runtime only | [F11] |
| 5 | §10 Phase 1 | Added task 4 (close skip guard) | validateConditionalSkip needs update | [F12] |
| 6 | §10 Phase 4 | Removed task 16 (moved to Phase 1) | Logical coupling | [F12] |
