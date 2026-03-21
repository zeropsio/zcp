# Deep Review Report: deploy-workflow-guidance-redesign — Review 1
**Date**: 2026-03-21
**Reviewed version**: `plans/deploy-workflow-guidance-redesign.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Implementation readiness — are all details resolved? Holistic view.
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Codebase Claims — All 15 Verified:**
1. `buildGuide()` at deploy.go:341 — params `_ int, _ Environment` confirmed unused
2. `resolveDeployStepGuidance()` assembles mode+strategy sections from deploy.md — VERIFIED
3. `assembleKnowledge()` at guidance.go:72 — injects runtime briefings, schema, env vars — VERIFIED
4. **GuidanceParams has 11 fields (not 10)** — Step, Mode, Strategy, RuntimeType, DependencyTypes, DiscoveredEnvVars, Iteration, Plan, LastAttestation, FailureCount, KP
5. Dead code list accurate: UpdateTarget (0 prod callers), DevFailed (0), Error field (dead writer), LastAttestation (dead writer), 4 dead status consts, ResolveDeployGuidance (0)
6. `ErrBootstrapNotActive` at workflow_deploy.go:29,51,62 — confirmed semantically wrong
7. `deployTargetPending` LIVE at deploy.go:154,163,273
8. StepCheckResult generic (no bootstrap fields); StepChecker type IS bootstrap-specific (takes *ServicePlan, *BootstrapState)
9. `filepath.Dir(filepath.Dir(stateDir))` pattern at workflow_checks_generate.go:29 — generic, reusable
10. `needsRuntimeKnowledge()` handles DeployStepPrepare at guidance.go:67
11. `resolveStaticGuidance()` handles StepGenerate, StepDeploy, StepClose — bootstrap steps only; StepDeploy == DeployStepDeploy == "deploy" (shared constant)
12. **ServiceMeta has NO RuntimeType field** — only Hostname, Mode, StageHostname, DeployStrategy, BootstrapSession, BootstrappedAt
13. ResetForIteration Error clear at deploy.go:274 — effectively dead (Error never written in prod)
14. DeployComplete at engine.go:358 and DeployStart at engine.go:341 — NO context.Context param
15. BootstrapComplete at engine.go:137 — HAS context.Context

**Critical Correction: BuildTransitionMessage Location**
- Plan Phase 5 claims: `workflow/bootstrap_outputs.go`
- Actual: `bootstrap_guide_assembly.go:58-133`
- Called from: `tools/workflow_bootstrap.go:80`

**Dead Test Count**: 6 tests (2 in deploy_test.go, 4 in deploy_guidance_test.go) — matches plan estimate.

### Platform Verification Results (kb-verifier)

| # | Claim | Result | Evidence |
|---|-------|--------|----------|
| 1 | Service statuses ACTIVE, RUNNING, STOPPED, READY_TO_DEPLOY | PARTIAL | ACTIVE, RUNNING, NEW, READY_TO_DEPLOY confirmed live. STOPPED unverified. |
| 2 | Events API `hint` field | CONFIRMED | ops/events.go generates LLM-friendly hints: DEPLOYED, IN_PROGRESS, FAILED prefixes |
| 3 | **zerops_deploy blocks until complete** | **CONFIRMED** | ops/deploy.go returns BUILD_TRIGGERED, BUT tools/deploy.go:59 wraps with pollDeployBuild() which polls SearchAppVersions until terminal state (15min timeout). Tool description says "blocks until build completes". Agent sees DEPLOYED or failure. |
| 4 | Env var discovery structure | CONFIRMED | Live response: key+value fields, isPlatformInjected boolean |
| 5 | Subdomain state | CONFIRMED | API rejects non-HTTP types (serviceStackIsNotHttp) |
| 6 | Current services | CONFIRMED | 1 service: zcpx (zcp@1, ACTIVE) |

**Platform Finding — CORRECTED**: kb-verifier examined only `ops/deploy.go` which returns BUILD_TRIGGERED immediately. However, `tools/deploy.go:59` wraps the call with `pollDeployBuild()` (ops/progress.go:49-131) which polls `SearchAppVersions` every 1-5s until terminal state (15min timeout). The tool then updates the result in-place to DEPLOYED (success) or failure status with build logs. **From the agent's perspective, zerops_deploy IS blocking.** The original plan's claim "blocks until complete — returns DEPLOYED or BUILD_FAILED" is CORRECT.

**Process vs Service Status Distinction**: Build polling uses AppVersionEvent statuses (BUILDING, DEPLOYING → in-progress; ACTIVE → success; others → failure). These are app version statuses checked via `SearchAppVersions`, not service statuses.

---

## Stage 2: Analysis Reports

### Correctness Analysis

**Assessment**: CONCERNS (4 critical, 2 major, 2 minor)

**Findings:**
- [F1] GuidanceParams has 11 fields not 10 — CRITICAL — guidance.go:9-22
- [F2] ServiceMeta has NO RuntimeType — CRITICAL — service_meta.go:22-29
- [F3] BuildTransitionMessage in wrong file — CRITICAL — bootstrap_guide_assembly.go:58, not bootstrap_outputs.go
- [F4] DeployStart/DeployComplete lack context.Context — CRITICAL — engine.go:341,358
- [F5] Deploy engine methods have NO checker parameter — MAJOR — engine.go:358,341
- [F6] ResolveDeployGuidance confirmed dead (0 prod callers) — MAJOR (confirms plan)
- [F7] ErrBootstrapNotActive semantic bug — MINOR (plan fixes)
- [F8] GuidanceParams asymmetry undocumented — MINOR

**Key insight**: Plan claims decisions #2 (add ctx) and #5 (separate DeployStepChecker) as "Confirmed Decisions" but neither is implemented. They are design decisions, not done work.

### Architecture Analysis

**Assessment**: SOUND with 3 clarifications

**Findings:**
- F1: RuntimeType NOT in ServiceMeta — CRITICAL — must resolve before Phase 2
- F2: BuildTransitionMessage location confirmed wrong
- F3: StepChecker bootstrap-specific, DeployStepChecker sound design
- F4: Strategy flow verified complete
- F5: buildGuide unused params confirmed
- F6: 350-line limit observed across all planned files
- F7-F14: Phase dependencies correct, file impacts within bounds

**Key insight**: Recommends API re-read for RuntimeType (matches "deploy is standalone" philosophy). Phase ordering is sound — Phase 1 has no blockers.

### Security Analysis

**Assessment**: SOUND (3 minor findings)

**Findings:**
- F1: ErrBootstrapNotActive semantic bug — MINOR (plan fixes)
- F2: filepath.Dir pattern safe — MINOR (stateDir engine-controlled, no user input)
- F3: Env var reference validation correct design — MINOR (re-discovery at prepare is sound)

**Key insight**: No security vulnerabilities introduced. Secret exposure prevented by architecture (names-only in guidance). Validation at boundaries is correct.

### Adversarial Analysis

**Assessment**: CONCERNS (5 findings, 3 implementation blockers)

**Findings:**
- [F1] zerops_deploy NOT blocking — CRITICAL — plan guidance templates are wrong
- [F2] RuntimeType missing from ServiceMeta — MAJOR — Phase 2 blocker
- [F3] Route is NOT facts-only — MAJOR — Route has intent-matching, priority boosting, editorial Reason strings. Phase 6 massively understates work.
- [F4] GuidanceParams 11 vs 10 — MINOR
- [F5] Env var re-discovery timing unspecified — MODERATE

**Key insight**: Current code at deploy.go:344-350 already calls assembleKnowledge() which injects runtime briefings at DeployStepPrepare. Plan says "Deploy MUST NOT inject full knowledge" but current code DOES. Phase 2 must actively remove this behavior.

**Concrete failure scenarios**:
1. First deploy: agent follows guidance saying "deploy blocks", waits forever
2. Knowledge query: no spec for what zerops_knowledge returns at deploy-time vs bootstrap-time
3. Route intent-matching too coarse: "CI/CD" intent boosts wrong workflow

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | ~~zerops_deploy is NOT blocking~~ **FALSE POSITIVE** — zerops_deploy IS blocking from agent's perspective. tools/deploy.go:59 wraps with pollDeployBuild() | RETRACTED | ops/deploy.go returns BUILD_TRIGGERED BUT tools/deploy.go polls until terminal. Original plan correct. | kb-verifier (incomplete analysis), corrected by orchestrator |
| V2 | ServiceMeta has no RuntimeType field | CRITICAL | service_meta.go:22-29 — 6 fields, none is RuntimeType | kb-research + correctness + architecture + adversarial (all 4) |
| V3 | BuildTransitionMessage at bootstrap_guide_assembly.go:58, NOT bootstrap_outputs.go | CRITICAL | grep + read confirmation | kb-research + correctness + architecture |
| V4 | DeployStart/DeployComplete lack context.Context | CRITICAL | engine.go:341,358 — no ctx param; BootstrapComplete at :137 has ctx | correctness + architecture |
| V5 | Deploy engine methods have no checker parameter | MAJOR | engine.go:358 — DeployComplete(step, attestation) only; contrast BootstrapComplete(ctx, stepName, attestation, checker) | correctness |
| V6 | Route() is a recommendation engine, not facts-only | MAJOR | router.go:91-104 boostByIntent(), :81-88 intentPatterns, Reason strings are editorial | adversarial + orchestrator |
| V7 | GuidanceParams has 11 fields, not 10 | MINOR | guidance.go:10-22 — count is 11 | kb-research + correctness + adversarial |
| V8 | Dead code list accurate (all 6 items verified) | VERIFIED | 0 production callers for all items; deployTargetPending LIVE | kb-research + correctness |
| V9 | handleStrategy has no "ready to deploy" transition | MINOR | workflow_strategy.go returns {status, services, guidance} — no "next" field | orchestrator |
| V10 | Current deploy buildGuide already injects knowledge via assembleKnowledge | MAJOR | deploy.go:344-350 calls assembleKnowledge which pulls briefings at DeployStepPrepare | adversarial |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | ~~Phase 2 guidance templates must change "blocks" to async polling~~ **RETRACTED** — zerops_deploy IS blocking. Original plan templates correct. | RETRACTED | V1 was false positive. tools/deploy.go:59 pollDeployBuild() handles polling internally. | Corrected by orchestrator |
| L2 | checkDeployResult should check service status + build logs from DeployResult | MINOR | pollDeployBuild already populates BuildStatus, BuildLogs in DeployResult. Checker can inspect these. | kb-verifier (corrected) |
| L3 | DeployStepChecker needs different signature than StepChecker | MAJOR | StepChecker takes (*ServicePlan, *BootstrapState) — deploy has neither → new type needed | correctness + architecture |
| L4 | Phase 6 is a significant refactor, not "verify and adjust" | MAJOR | V6 (Route is recommendation engine) → removing intent-matching is breaking change | adversarial |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | Service status "STOPPED" exists | LOW | Not observed in live API or E2E tests. Plausible but unconfirmed. | kb-verifier |
| U2 | Env var re-discovery API cost | LOW | GetServiceEnv call cost not benchmarked. Assumed "cheap" without evidence. | adversarial |
| U3 | Dev mode uses zsc noop --silent | LOW | Embedded in deploy.md content, not verified against live platform | kb-research |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | Is plan "ready for implementation"? | Correctness: NOT ready (4 blockers) | engine.go signatures, ServiceMeta fields | Architecture: READY for Phase 1 | Dead code cleanup has no blockers | **BOTH CORRECT**: Phase 1 is ready. Phases 2-3 have blockers that must resolve first. |
| D2 | Phase 6 scope | Plan: "verify and adjust" | Section 5, Phase 6 | Adversarial: significant refactor | router.go:81-104 intent-matching | **Adversarial wins**: Route violates philosophy spec. Phase 6 scope is underestimated. |

### Key Insights from Knowledge Base

1. **zerops_deploy async model changes everything about deploy step guidance.** The plan's core guidance templates (sections 4.2, 4.3) assume synchronous deploy. Reality: deploy triggers async build, agent must poll zerops_events, Events API hint field provides LLM-friendly status. This is not a minor correction — it changes the deploy step workflow from "call deploy → wait → check result" to "call deploy → poll events → interpret hint → proceed".

2. **Process vs Service status distinction matters for checkDeployResult.** Plan section 5 Phase 3 references BUILD_FAILED as a service status. Live platform shows PENDING/RUNNING/FINISHED/FAILED/CANCELED are process statuses. Service statuses are ACTIVE/RUNNING/NEW/READY_TO_DEPLOY. checkDeployResult should check Events API for process completion, then service status for final state.

3. **Current code already violates the "no knowledge injection in deploy" principle.** deploy.go:344-350 calls assembleKnowledge which routes to guidance.go:86-98 to inject runtime briefings at DeployStepPrepare. Phase 2 must actively remove this, not just add new behavior.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

| # | Item | Phase | Evidence |
|---|------|-------|----------|
| MA1 | Fix zerops_deploy blocking claim — change to async model with zerops_events polling in all guidance templates | 2 | V1: ops/deploy.go returns BUILD_TRIGGERED immediately |
| MA2 | Resolve RuntimeType data source — recommend: store in ServiceMeta during bootstrap OR fetch from API at deploy time | 2 (before start) | V2: ServiceMeta has no RuntimeType field |
| MA3 | Fix BuildTransitionMessage file reference — bootstrap_guide_assembly.go:58, not bootstrap_outputs.go | Plan doc | V3: grep confirmed location |
| MA4 | Plan must acknowledge ctx addition is NEW WORK, not "confirmed decision" | Plan doc | V4: engine.go:341,358 lack ctx |
| MA5 | Define DeployStepChecker signature explicitly | 3 (before start) | V5 + L3: Deploy has no ServicePlan/BootstrapState |
| MA6 | Reassess Phase 6 scope — Route is a recommendation engine, not facts-only | Plan doc | V6: router.go intent-matching, boostByIntent |
| MA7 | Phase 2 must remove current assembleKnowledge injection from deploy buildGuide | 2 | V10: deploy.go:344-350 already injects knowledge |

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)

| # | Item | Phase | Evidence |
|---|------|-------|----------|
| SA1 | Update checkDeployResult to use Events API process status, not service status for build failures | 3 | L2: BUILD_FAILED is process status |
| SA2 | Fix GuidanceParams field count (11 not 10) in plan text | Plan doc | V7 |
| SA3 | Add "ready to deploy" next-step command to handleStrategy response | 5 | V9: currently returns no transition guidance |
| SA4 | Document env var re-discovery rationale (deploy standalone, API state may change) | 2 | correctness F8 |

### Investigate (UNVERIFIED but plausible)

| # | Item | Phase | Evidence |
|---|------|-------|----------|
| I1 | Verify service status "STOPPED" exists on platform | 3 | U1: not observed |
| I2 | Benchmark GetServiceEnv API call latency | 3 | U2: assumed cheap |
| I3 | Verify dev mode zsc noop behavior on live platform | 3 | U3: not tested |

---

## Revised Version

See `plans/deploy-workflow-guidance-redesign.v2.md` for the complete revised plan incorporating all Must Address and Should Address items.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | 2.4 | Fix "4 of 10" → "4 of 11" GuidanceParams fields | guidance.go:10-22 shows 11 fields | V7 from correctness |
| 2 | 3.1 Decision 2 | Clarify ctx addition is NEW WORK to implement, not already done | engine.go:341,358 lack ctx | V4 from correctness |
| 3 | 4.1 | Add RuntimeType resolution strategy (API fetch at deploy time) | service_meta.go has no RuntimeType | V2 from all 4 analysts |
| 4 | 4.2-4.3 | Replace "blocks until complete" with async deploy + events polling model | ops/deploy.go returns BUILD_TRIGGERED | V1 from kb-verifier |
| 5 | 4.3 Deploy step | Add zerops_events polling workflow after zerops_deploy | V1 + L1 | kb-verifier + adversarial |
| 6 | 5 Phase 2 | Add: remove current assembleKnowledge injection from deploy buildGuide | deploy.go:344-350 calls assembleKnowledge | V10 from adversarial |
| 7 | 5 Phase 3 | Update checkDeployResult to use Events API process status | BUILD_FAILED is process status, not service | L2 from kb-verifier |
| 8 | 5 Phase 3 | Define DeployStepChecker signature explicitly | StepChecker takes bootstrap-specific params | V5 + L3 |
| 9 | 5 Phase 5 | Fix file reference: bootstrap_guide_assembly.go, not bootstrap_outputs.go | grep confirms location | V3 from kb-research |
| 10 | 5 Phase 5 | Add "next" field to handleStrategy response | Currently returns no transition | V9 from orchestrator |
| 11 | 5 Phase 6 | Expand scope: Route refactor is significant, not "verify and adjust" | router.go:81-104 boostByIntent | V6 + L4 from adversarial |
| 12 | 8 Q3 | Resolved: Fetch RuntimeType from API via client.ListServices() at deploy time | Matches "deploy is standalone" philosophy | V2 + architecture R1 |
