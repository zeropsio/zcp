# Deep Review Report: deploy-workflow-production-readiness — Review 1
**Date**: 2026-03-21
**Reviewed version**: plans/deploy-workflow-production-readiness.md
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Review whether plan is ready for implementation and makes sense as structured
**Resolution method**: Evidence-based (no voting)

---

## Input Document

[See plans/deploy-workflow-production-readiness.md — complete copy omitted for brevity, reviewed in full]

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Documentation Facts:**
- zerops.yml `healthCheck` is optional — supports `httpGet` and `exec` methods [DOCUMENTED]
- `deployFiles` defines what survives from build to runtime container [DOCUMENTED]
- `${hostname_varName}` env var refs resolved at container level — invalid refs silently become literal strings [MEMORY: verified 2026-03-04]
- `zcli push` triggers build pipeline (build → deploy) [DOCUMENTED]
- Deploy always creates new container — local files lost [MEMORY: verified 2026-03-04]

**Codebase Facts — ALL plan claims verified correct:**
- Strategy flow through data model: DeployTarget.Strategy (deploy.go:61), DeployState.Strategy (deploy.go:43), BuildDeployTargets returns 3 values (deploy.go:130) [VERIFIED]
- Strategy flow through guidance: resolveDeployStepGuidance (deploy_guidance.go:46), mode+strategy layering (deploy_guidance.go:56-73) [VERIFIED]
- Strategy sections in deploy.md: deploy-push-dev, deploy-ci-cd, deploy-manual [VERIFIED]
- Knowledge injection: assembleKnowledge receives Strategy (guidance.go:72) [VERIFIED]
- Zero checkers in deploy: handleDeployComplete calls engine.DeployComplete which only does CompleteStep [VERIFIED]
- All dead code confirmed: UpdateTarget (0 callers), DevFailed (0 callers), ResolveDeployGuidance (0 callers), 4 status constants dead [VERIFIED]

**Key findings NOT in plan:**
1. StepChecker type at bootstrap_checks.go:24 is `func(ctx, *ServicePlan, *BootstrapState)` — BootstrapState-specific, NOT generic [VERIFIED]
2. Deploy's buildGuide() bypasses unified assembleGuidance() — calls resolveDeployStepGuidance() + assembleKnowledge() separately [VERIFIED]
3. DeployComplete() at engine.go:358 has NO context.Context parameter [VERIFIED]
4. buildGuide() has TWO ignored params: `_ int` AND `_ Environment` (plan only mentions iteration) [VERIFIED]
5. Bootstrap checkers live in `internal/tools/` (workflow_checks.go, workflow_checks_generate.go), NOT in `internal/workflow/` [VERIFIED]
6. deployTargetPending is the ONLY live status constant — must be preserved [VERIFIED]

### Platform Verification Results (kb-verifier)

All 15 testable claims verified:
- 13 CONFIRMED (exact line matches, grep results, dead code confirmed)
- 1 PARTIAL (status constants: `pending` must be kept, plan's delete list doesn't explicitly preserve it)
- 1 CONFIRMED (platform: project "zcp20" ACTIVE, 1 service "zcpx")
- All tests pass: `go test ./internal/workflow/... -count=1 -short` → OK

---

## Stage 2: Analysis Reports

### Security & Architecture Analysis (combined)
**Assessment**: SOUND
**Evidence basis**: 18/18 VERIFIED

**Findings:**
- [F1] Attestation-only advancement — CRITICAL — Phase 2 correctly addresses this [VERIFIED]
- [F2] Invalid env var refs pass silently — MAJOR — Phase 2 checkDeployPrepare must validate [VERIFIED]
- [F3] No dev→stage failure routing — MAJOR — Phase 2 re-implements correctly [VERIFIED]
- [F4] Shell/YAML injection vectors: SAFE — existing code uses safe patterns [VERIFIED]
- [F5] Env var credential exposure: known gap, not worsened by plan [VERIFIED]
- [F6] StepChecker asymmetry correctly identified [VERIFIED]
- [F7] Phase sequencing sound (1→2→3→4 dependencies correct) [VERIFIED]
- [F8] Integration points clear (context.Context threading needed) [VERIFIED]

**3 clarifications needed before Phase 2:**
1. Env var discovery approach (early discover vs schema-only validation)
2. Dev→stage health gate specifics (when/how enforcement happens)
3. Checker result population (does it update DeployTarget.Status?)

### Architecture & Correctness Analysis (combined)
**Assessment**: CONCERNS (minor)
**Evidence basis**: 18/18 VERIFIED

**Findings:**
- [F9] StepChecker typed to *BootstrapState — deploy needs new type or generalization — MAJOR [VERIFIED: bootstrap_checks.go:24]
- [F10] DeployComplete needs context.Context added for checker API calls — MAJOR [VERIFIED: engine.go:358]
- [F11] buildGuide() bypasses assembleGuidance() — structural gap not addressed — MAJOR [VERIFIED: deploy.go:341 vs guidance.go:27]
- [F12] TWO ignored params in buildGuide, plan only mentions one — MINOR [VERIFIED: deploy.go:341]
- [F13] Checker files belong in internal/tools/, not internal/workflow/ — MINOR [VERIFIED: existing pattern]
- [F14] deployTargetPending must be preserved — plan's delete list ambiguous — MINOR [VERIFIED]
- [F15] Phase 2 line estimates reasonable but context.Context threading adds ~10 lines not counted — MINOR [LOGICAL]

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)
| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| 1 | StepChecker is BootstrapState-specific — deploy needs DeployStepChecker type or refactor | MAJOR | bootstrap_checks.go:24 shows `func(ctx, *ServicePlan, *BootstrapState)` | kb-research, architecture |
| 2 | DeployComplete() needs context.Context parameter added | MAJOR | engine.go:358 has no ctx; BootstrapComplete at engine.go:137 does | kb-verifier, architecture |
| 3 | buildGuide() bypasses unified assembleGuidance() — structural gap | MAJOR | deploy.go:341 calls resolveDeployStepGuidance+assembleKnowledge; bootstrap uses assembleGuidance (guidance.go:27) | kb-research, architecture |
| 4 | Phase 2 checker files should go in internal/tools/ (existing pattern) | MINOR | Existing checkers: tools/workflow_checks.go, tools/workflow_checks_generate.go | self-verified |
| 5 | buildGuide() ignores BOTH iteration AND Environment (plan mentions only iteration) | MINOR | deploy.go:341 `_ int, _ Environment` | kb-verifier |
| 6 | deployTargetPending must be preserved when deleting other constants | MINOR | Used at deploy.go:152,154,161,164,273 | kb-verifier |
| 7 | ResetForIteration Error clear line is effectively dead (Error only set by dead UpdateTarget) | MINOR | deploy.go:274 clears Error; Error only written by dead UpdateTarget at deploy.go:253 | kb-research |

#### LOGICAL (follows from verified facts)
| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| 8 | Phase 3 should consider unifying buildGuide with assembleGuidance rather than creating parallel iteration delta | MAJOR | If deploy used assembleGuidance() like bootstrap, iteration escalation would work automatically via BuildIterationDelta. Creating a separate mechanism violates "extend existing" principle. | architecture |
| 9 | Phase 2 estimates undercount by ~20 lines (context threading + new checker type) | MINOR | DeployComplete needs ctx param (+5), new DeployStepChecker type or refactor (+10), handleDeployComplete needs checker wiring (+5) | architecture |

#### UNVERIFIED (flagged but not confirmed)
| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| 10 | checkGenerate path logic may be tightly coupled to bootstrap state | LOW | Plan says "reuse checkGenerate's path logic" but coupling not verified | kb-research |

### Disputed Findings
None — all analysts agreed on findings.

### Key Insights from Knowledge Base

1. **The biggest architectural gap the plan doesn't address**: Deploy's `buildGuide()` uses a completely separate code path from bootstrap's guidance assembly. Bootstrap uses the unified `assembleGuidance()` which handles iteration deltas, static guidance, AND knowledge injection in one call. Deploy manually calls two separate functions. Phase 3 (iteration escalation) should fix this by switching deploy to use `assembleGuidance()` rather than creating a parallel mechanism.

2. **StepChecker type constraint is a real blocker for Phase 2**: The plan assumes checkers can be wired in but doesn't mention that the `StepChecker` type signature includes `*BootstrapState`. Deploy workflow needs either a new `DeployStepChecker` type or a generalization of the existing type. This is a design decision that should be made explicit before implementation.

3. **Checker file location**: The plan mentions `workflow_checks.go` or `workflow_checks_deploy.go` as if they're in `internal/workflow/`. The existing pattern puts checkers in `internal/tools/` (where they have access to platform.Client, ops, etc.). New deploy checkers should follow this pattern.

---

## Action Items

### Must Address (VERIFIED Critical + Major)
1. **Add StepChecker type decision to Phase 2** — either create `DeployStepChecker func(ctx, *DeployState) (*StepCheckResult, error)` or generalize existing StepChecker. Design decision needed before coding.
2. **Add context.Context to DeployComplete()** — Phase 2 prerequisite, not mentioned in plan.
3. **Consider unifying buildGuide with assembleGuidance** — Phase 3 would be simpler if deploy used the same guidance assembly as bootstrap. Decide: unify vs parallel mechanism.

### Should Address (LOGICAL + VERIFIED Minor)
4. **Fix checker file location** — plan says `workflow_checks.go`, should be `internal/tools/workflow_checks_deploy.go` (matching existing `workflow_checks.go` and `workflow_checks_generate.go` in tools/).
5. **Note both ignored params** — buildGuide ignores `_ int` AND `_ Environment`. Phase 1 enrichment should address both.
6. **Explicitly preserve deployTargetPending** — plan's Phase 1 delete list should explicitly note this constant is kept.
7. **Phase 2 estimates**: add ~20 lines for context threading + checker type definition.

### Investigate (UNVERIFIED but plausible)
8. **checkGenerate path coupling** — verify whether bootstrap's zerops.yml path resolution can be extracted for deploy reuse, or if it needs reimplementation.

---

## Revised Version

# Deploy Workflow — Production Readiness Plan

**Date**: 2026-03-21 (verified against actual code state, deep-reviewed)
**Status**: Ready for implementation with clarifications below
**Scope**: Standalone deploy workflow (`action="start" workflow="deploy"`)

---

## 1. Motivation

The standalone deploy workflow is the **post-bootstrap lifecycle workflow** — the primary way an LLM agent deploys and redeploys services. Bootstrap was refined to production quality (5 steps, 3 checkers, per-target feedback, escalating iteration). Deploy workflow needs to reach the same quality bar.

---

## 2. What's Already Done (verified — compiles, tests pass)

Strategy flow is **partially implemented**. Contrary to initial analysis, significant work already exists:

### Strategy flows through data model
- `DeployTarget.Strategy` field exists (deploy.go:61) — populated from ServiceMeta
- `DeployState.Strategy` field exists (deploy.go:43) — set from first meta
- `BuildDeployTargets()` returns 3 values: targets, mode, strategy (deploy.go:130)
- `engine.DeployStart()` accepts strategy (engine.go:341)
- `handleDeployStart()` unpacks all 3 values (workflow.go:238)

### Strategy flows through guidance
- `resolveDeployStepGuidance(step, mode, strategy)` accepts strategy (deploy_guidance.go:46)
- At deploy step: mode-specific section + strategy-specific section layered (deploy_guidance.go:69-73)
- `buildGuide()` passes `d.Strategy` to guidance (deploy.go:342)
- `GuidanceParams.Strategy` field exists (guidance.go:13)
- `buildGuide()` passes Strategy in GuidanceParams (deploy.go:347)

### Strategy sections in deploy.md exist
- `deploy-push-dev` — SSH self-deploy guidance
- `deploy-ci-cd` — git webhook guidance
- `deploy-manual` — manual deploy guidance
- These ARE delivered during deploy workflow via resolveDeployStepGuidance lines 69-73

### Knowledge injection works
- `assembleKnowledge()` receives Strategy in GuidanceParams (guidance.go:72)
- Runtime knowledge at deploy-prepare step (guidance.go:86-98)
- zerops.yml schema + rules at deploy-prepare (guidance.go:106-112)
- Env vars at deploy step (guidance.go:120-125)

---

## 3. What's Still Missing

### 3.1 Zero Checkers (CRITICAL)

Deploy workflow has **no validation gates**. Every step advances on attestation alone.

| Step | Bootstrap equivalent | Deploy equivalent |
|------|---------------------|-------------------|
| prepare | checkGenerate: zerops.yml valid, env var refs valid | NONE — agent self-attests |
| deploy | checkDeploy: VerifyAll + subdomain + health | NONE — agent self-attests |
| verify | merged into deploy checker | NONE — agent self-attests |

`handleDeployComplete()` (workflow_deploy.go:12-34) calls `engine.DeployComplete()` which just does `CompleteStep()` — no checker at all.

**Prerequisite gaps** (from deep review):
- `engine.DeployComplete()` lacks `context.Context` parameter — checkers need context for API calls
- `StepChecker` type is `func(ctx, *ServicePlan, *BootstrapState)` — typed to BootstrapState, not usable for deploy. Need either `DeployStepChecker` type or generalize existing type.
- Existing bootstrap checkers live in `internal/tools/` (workflow_checks.go, workflow_checks_generate.go) — deploy checkers should follow same pattern.

### 3.2 Dead Per-Target Code

Still present with 0 production callers:

| Code | Location | Purpose |
|------|----------|---------|
| `UpdateTarget()` | deploy.go:248-260 | Set per-target status |
| `DevFailed()` | deploy.go:284-291 | Gate stage on dev failure |
| `DeployTarget.Error` | deploy.go:59 | Error field |
| `DeployTarget.LastAttestation` | deploy.go:60 | Attestation field |
| Status constants (deployed/verified/failed/skipped) | deploy.go:30-33 | Used by dead methods |
| `ResolveDeployGuidance()` | deploy_guidance.go:20-42 | Per-hostname strategy lookup — 0 callers |

**Note**: `deployTargetPending` (deploy.go:29) IS live — used in BuildDeployTargets and ResetForIteration. Must be preserved.

### 3.3 No Iteration Escalation

`buildGuide()` passes `_ int` for iteration AND `_ Environment` (deploy.go:341) — both parameters are ignored. No `BuildIterationDelta()` equivalent for deploy. Agent gets identical guidance on every retry.

**Structural gap**: Deploy's `buildGuide()` calls `resolveDeployStepGuidance()` + `assembleKnowledge()` separately, bypassing the unified `assembleGuidance()` function (guidance.go:27) that bootstrap uses. The unified function already handles iteration deltas. Switching deploy to use `assembleGuidance()` would provide iteration escalation without creating a parallel mechanism.

### 3.4 GuidanceParams Underutilized

`buildGuide()` passes only Step, Mode, Strategy, KP (deploy.go:344-348). Missing:
- RuntimeType (runtime briefings unavailable)
- DependencyTypes (dependency knowledge unavailable)
- DiscoveredEnvVars (not passed — though injected at deploy step via separate path)
- Iteration / FailureCount (no escalation)
- Plan / LastAttestation (no iteration context)

### 3.5 DeployTarget.Status Always "pending"

Targets in response always show `status: "pending"`. Never changes in production. Confusing for agents — suggests nothing happened.

### 3.6 No Dev->Stage Gate

Standard mode: dev should be healthy before stage deploys. `DevFailed()` exists but is dead code. No enforcement anywhere.

---

## 4. Implementation Plan

### Phase 1: Dead code cleanup + GuidanceParams enrichment

**Delete dead code:**
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation`, `ResolveDeployGuidance()`
- 4 dead status constants: `deployTargetDeployed`, `deployTargetVerified`, `deployTargetFailed`, `deployTargetSkipped`
- **Keep** `deployTargetPending` (live — used in BuildDeployTargets and ResetForIteration)
- Related tests: `TestDeployState_UpdateTarget`, `TestDeployState_DevFailed`
- `ResetForIteration()` Error clear line (deploy.go:274) — effectively dead since Error only set by dead UpdateTarget
- Tests for `ResolveDeployGuidance` in deploy_guidance_test.go (4 test functions)

**Enrich buildGuide():**
- Pass iteration counter (currently ignored `_ int`)
- Pass Environment (currently ignored `_ Environment`)
- Pass RuntimeType from first target (readable from ServiceMeta)
- Pass Iteration + FailureCount for future escalation

| File | Change | Est. |
|------|--------|------|
| deploy.go | Delete dead code, keep deployTargetPending | -40 |
| deploy_test.go | Delete 2 dead tests | -25 |
| deploy_guidance.go | Delete dead ResolveDeployGuidance | -23 |
| deploy_guidance_test.go | Delete 4 dead tests for ResolveDeployGuidance | -40 |
| deploy.go buildGuide | Pass iteration, env, runtimeType to GuidanceParams | +10 |

### Phase 2: Checkers

**Design decisions (resolve before coding):**
1. **Checker type**: Create `DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)` — separate from bootstrap's StepChecker since deploy has no ServicePlan.
2. **Context threading**: Add `context.Context` to `engine.DeployComplete()` signature.
3. **File location**: New checkers go in `internal/tools/workflow_checks_deploy.go` (matching existing pattern).

Wire checkers into deploy workflow. `handleDeployComplete()` needs to accept checker dependencies and run them before advancing steps.

**checkDeployPrepare(stateDir):**
- zerops.yml exists at projectRoot/ or projectRoot/{hostname}/
- YAML parses correctly
- setup entries match target hostnames
- Env var references resolvable

**checkDeployExecute(client, fetcher, projectID, httpClient):**
- `ops.VerifyAll()` per target — health (HTTP, logs, startup)
- Subdomain access for services with ports
- Standard mode: dev healthy before stage (inline DevFailed logic, API-only)

**checkDeployVerify(client, fetcher, projectID, httpClient):**
- Lighter health confirmation
- Iteration count warning

| File | Change | Est. |
|------|--------|------|
| engine.go DeployComplete | Add context.Context + DeployStepChecker params | +15 |
| tools/workflow_deploy.go | Wire checker deps, build checker, pass to engine | +40 |
| tools/workflow_checks_deploy.go | 3 new deploy checkers + DeployStepChecker type | +130 |
| tools/workflow_checks_deploy_test.go | Tests for 3 checkers | +100 |

### Phase 3: Iteration escalation

**Recommended approach**: Unify deploy's `buildGuide()` with the existing `assembleGuidance()` function (guidance.go:27) rather than creating a parallel iteration delta mechanism. This:
- Reuses `BuildIterationDelta()` which already handles escalating tiers
- Follows CLAUDE.md "extend existing mechanisms" principle
- Requires deploy-specific static guidance resolver (replacing resolveDeployStepGuidance call)

Deploy iteration escalation tiers:
- Tier 1: "Check zerops_logs severity=error; fix and redeploy"
- Tier 2: "Systematic: env var refs, service type, ports, start command"
- Tier 3: "Stop — present diagnostic summary to user"

| File | Change | Est. |
|------|--------|------|
| deploy.go buildGuide | Switch to assembleGuidance() or add deploy-specific delta | +15 |
| guidance.go | Extend BuildIterationDelta for deploy steps if needed | +20 |

### Phase 4: Polish

- Document deploy iteration in spec
- Remove orphaned bootstrap.md verify section (from prior review)
- Populate DeployTarget.Status from checker results (replaces always-"pending")

---

## 5. What Does NOT Need Changing

These are **already working correctly**:

- Strategy flow: ServiceMeta → DeployTarget → DeployState → guidance assembly
- Mode flow: ServiceMeta → BuildDeployTargets → roles → guidance sections
- Strategy gate: handleDeployStart rejects if strategy missing
- Strategy-specific guidance: deploy-push-dev/ci-cd/manual sections delivered at deploy step
- Mode-specific guidance: deploy-execute-standard/dev/simple sections delivered at deploy step
- Knowledge injection: runtime briefings, zerops.yml schema, deploy rules
- Env var injection at deploy step
- Iteration reset: ResetForIteration resets deploy+verify, preserves prepare
- Session lifecycle: auto-cleanup on completion

---

## 6. Risks

| Risk | Severity | Mitigation |
|------|----------|-----------|
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | Verify checkGenerate reusability; may need reimplementation if tightly coupled to bootstrap state |
| Shared checker logic with bootstrap checkDeploy | LOW | Extract helpers or duplicate (small) |
| handleDeployComplete gets more complex | LOW | Mirror bootstrap's handleBootstrapComplete pattern |
| Mixed strategies per deploy session | LOW | Gate in handleDeployStart (first meta's strategy used, validated consistent) |
| StepChecker type divergence (bootstrap vs deploy) | LOW | DeployStepChecker is simpler (no ServicePlan); keep separate unless 3+ checker types emerge |
| DeployComplete context threading | LOW | Straightforward — all callers (tools layer) already have context |

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | 3.1 | Added prerequisite gaps: context.Context, StepChecker type, file location | engine.go:358 has no ctx; bootstrap_checks.go:24 typed to BootstrapState; checkers in tools/ | kb-verifier, self-verified |
| 2 | 3.2 | Explicitly note deployTargetPending must be kept | deploy.go:152,154,161,164,273 uses it | kb-verifier |
| 3 | 3.3 | Note BOTH ignored params and structural gap (bypasses assembleGuidance) | deploy.go:341 has `_ int, _ Environment`; guidance.go:27 is unified path | kb-research |
| 4 | 4 Phase 1 | Separate kept vs deleted constants; add deploy_guidance_test.go cleanup | deploy.go:29 deployTargetPending is live | kb-verifier |
| 5 | 4 Phase 2 | Added design decisions section: checker type, context, file location | bootstrap_checks.go:24, engine.go:358, tools/workflow_checks.go | all sources |
| 6 | 4 Phase 2 | Corrected file locations to internal/tools/ | Existing pattern: tools/workflow_checks.go | self-verified |
| 7 | 4 Phase 2 | Updated estimates (+15 for engine, +130 for checkers including type def) | Context threading + new type adds ~25 lines | architecture |
| 8 | 4 Phase 3 | Recommend unifying with assembleGuidance instead of parallel mechanism | guidance.go:27 already handles iteration deltas | kb-research |
| 9 | 6 Risks | Added StepChecker type divergence and context threading risks | New findings from deep review | architecture, security |
