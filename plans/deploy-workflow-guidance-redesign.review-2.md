# Deep Review Report: deploy-workflow-guidance-redesign — Review 2
**Date**: 2026-03-21
**Reviewed version**: `plans/deploy-workflow-guidance-redesign.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Implementation readiness — are all details resolved? Holistic view. Ultrathink.
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Deploy Mechanics**: zerops_deploy blocks via pollDeployBuild() (tools/deploy.go:59). Agent sees DEPLOYED or BUILD_FAILED. Deploy workflow is session-based (3 steps: prepare, deploy, verify). VERIFIED.

**Container Lifecycle**: Deploy = new container, local files lost, only deployFiles survives. Restart/reload keeps files. VERIFIED (memory 2026-03-04).

**Env Var Behavior**: `${hostname_varName}` resolved at container level. Typos = silent literal strings. ZCP validates refs at checkGenerate via ops.ValidateEnvReferences(). Env vars discovered at provision (names only), injected at generate (bootstrap) or deploy step (deploy workflow). VERIFIED.

**Service Statuses**: RUNNING, ACTIVE (both = running), NEW, READY_TO_DEPLOY, DEPLOY_FAILED confirmed in code (workflow_checks.go:67,228-229). Plain string from API, no enum type. VERIFIED.

**Guidance Assembly**: Bootstrap: assembleGuidance(GuidanceParams{all 11 fields}). Deploy: buildGuide() → resolveDeployStepGuidance() + assembleKnowledge(4 of 11 fields). needsRuntimeKnowledge() returns true for DeployStepPrepare — runtime briefings ARE currently injected in deploy. VERIFIED.

**Dead Code**: ResolveDeployGuidance (0 prod callers), UpdateTarget (0), DevFailed (0). StrategyToSection is ALIVE (used by resolveDeployStepGuidance:70). VERIFIED.

**ServiceMeta**: NO RuntimeType field. Fields: Hostname, Mode, StageHostname, DeployStrategy, BootstrapSession, BootstrappedAt. VERIFIED (service_meta.go:22-29).

**Init Template**: `internal/content/templates/claude.md` contains only "# Zerops". Green field for Phase 4. VERIFIED.

**BuildTransitionMessage**: At bootstrap_guide_assembly.go:58. Includes strategy selection + router offerings. Plan v2 correctly references this location. VERIFIED.

**ErrBootstrapNotActive**: Used incorrectly in workflow_deploy.go:29,51,62. ErrDeployNotActive does NOT exist in errors.go. VERIFIED.

**Deploy Checkers**: No checker infrastructure exists for deploy. DeployComplete() calls CompleteStep() directly (engine.go:358-380). VERIFIED.

**Router**: FlowOffering has Reason (editorial text) + boostByIntent() (recommendation logic). Route is not "facts only" currently. VERIFIED (router.go:81-105).

### Platform Verification Results (kb-verifier)

| # | Claim | Result | Evidence |
|---|-------|--------|----------|
| 1 | zerops_deploy blocks via pollDeployBuild | CONFIRMED | tools/deploy.go:59 |
| 2 | Service statuses | PARTIAL | Only ACTIVE verifiable on live (1 service) |
| 3 | Events hint field | CONFIRMED | ops/events.go:40 |
| 4 | serviceStackIsNotHttp error | PARTIAL | API-side only, not in codebase |
| 5 | Env var typo = silent literal | CONFIRMED | Memory 2026-03-04 |
| 6 | DeployComplete lacks ctx | CONFIRMED | engine.go:358 |
| 7 | DeployStart lacks ctx | CONFIRMED | engine.go:341 |
| 8 | GuidanceParams 11 fields | CONFIRMED | guidance.go:10-22 |
| 9 | assembleKnowledge injects at DeployStepPrepare | CONFIRMED | guidance.go:67,86-98 |
| 10 | ResolveDeployGuidance 0 prod callers | CONFIRMED | grep |
| 11 | UpdateTarget 0 prod callers | CONFIRMED | grep |
| 12 | DevFailed 0 prod callers | CONFIRMED | grep |
| 13 | BuildTransitionMessage at :58 | CONFIRMED | bootstrap_guide_assembly.go:58 |
| 14 | ServiceMeta no RuntimeType | CONFIRMED | service_meta.go:22-29 |
| 15 | checkGenerate filepath.Dir pattern | CONFIRMED | workflow_checks_generate.go:29 |
| 16 | ErrDeployNotActive doesn't exist | CONFIRMED | errors.go |
| 17 | workflow_checks_deploy_test tests bootstrap checkDeploy | CONFIRMED | Tests bootstrap infra, not deploy workflow |

**Summary**: 15 CONFIRMED, 2 PARTIAL, 0 REFUTED.

---

## Stage 2: Analysis Reports

### Correctness Analysis (correctness agent)
**Assessment**: SOUND with MEDIUM concerns
**Evidence basis**: 27/30 VERIFIED, 2 LOGICAL, 1 UNVERIFIED

**Key findings**:
- All dead code claims verified (6 items, exact lines match)
- ErrBootstrapNotActive misuse confirmed (3 locations)
- Engine ctx gap confirmed (DeployStart:341, DeployComplete:358 lack ctx; BootstrapComplete:137 has it)
- GuidanceParams has 11 fields (plan said 10 in one place — corrected in context doc)
- needsRuntimeKnowledge already deploy-aware (guidance.go:67)
- assembleKnowledge currently injected in deploy (deploy.go:344-350) — Phase 2 must remove
- RuntimeType storage remains Open Question #2 — both options viable
- Step constant reuse (StepDeploy == DeployStepDeploy == "deploy") confirmed intentional
- No design blockers. Recommended GO AHEAD.

### Architecture Analysis (architecture agent)
**Assessment**: SOUND — Ready for implementation
**Evidence basis**: 15/15 claims verified, 6/6 design decisions sound

**Key findings**:
- [F1] Router has editorial Reason field + boostByIntent — MEDIUM risk. Phase 6 approach must be decided.
- deploy.go file size: 354 lines → ~294 after Phase 1 → ~344 after Phase 2. Within limit.
- DeployTarget.Status removal is safe (dead field).
- Checker architecture (DeployStepChecker separate from StepChecker) is sound.
- Recommended: Proceed with Phases 1-5. STOP on Phase 6 until router philosophy decided.

### Security Analysis (security agent)
**Assessment**: SOUND
**Evidence basis**: All findings VERIFIED

**Key findings**:
- YAML parsing safe (gopkg.in/yaml.v3 v3.0.1, no billion-laughs exposure)
- Env var validation chain sound (3-tier defense: bootstrap validate, deploy re-validate, platform silences typos)
- Error message safety confirmed (step names are constants, not user input)
- Session persistence uses atomic write pattern (CreateTemp + Rename)
- Env var re-discovery adds one more exposure point but is necessary and mitigated
- No security blockers.

### Adversarial Analysis (adversarial agent)
**Assessment**: CONCERNS — 4 critical gaps, 3 logic misalignments
**Evidence basis**: Verified via code inspection

**Critical findings**:
- [C1] DeployComplete has no ctx — plan says to ADD it (Phase 3 new work)
- [C2] DeployStart has no ctx — plan says to ADD it (Phase 3 new work)
- [C3] BuildIterationDelta with nil plan — need to verify nil safety
- [C4] RuntimeType source unclear — ServiceMeta has no field, plan says populate from ServiceMeta

**Logic findings**:
- [L1] Step constant reuse (StepDeploy vs DeployStepDeploy) fragile
- [L2] Checker invocation from tools layer underspecified
- [L3] stateDir path assumed same for deploy and bootstrap
- [U5] DeployTarget.Status removal in Phase 1 breaks BuildResponse line 309

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | All dead code (6 items) confirmed safe to remove | — | grep: 0 prod callers for all; StrategyToSection ALIVE at :70 | All agents |
| V2 | ErrBootstrapNotActive misused in 3 deploy handlers | MINOR | workflow_deploy.go:29,51,62 | Correctness, Architecture |
| V3 | assembleKnowledge currently injected at DeployStepPrepare | MAJOR | guidance.go:67 returns true, deploy.go:344-350 calls it | Correctness, KB-verifier |
| V4 | GuidanceParams has 11 fields (plan §2.2 table says 10) | MINOR | guidance.go:10-22 | KB-verifier |
| V5 | Router has editorial Reason + boostByIntent | MAJOR | router.go:81-105, FlowOffering.Reason at :13 | Architecture |
| V6 | BuildTransitionMessage includes router offerings with Reasons | MINOR | bootstrap_guide_assembly.go:109 | Architecture |
| V7 | workflow_checks_deploy_test.go tests bootstrap's checkDeploy, not deploy workflow | MINOR | Test file calls checkDeploy from workflow_checks.go:145 | KB-verifier |
| V8 | Init template is empty ("# Zerops") — green field | — | content/templates/claude.md | KB-research |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | DeployTarget.Status removal in Phase 1 breaks BuildResponse:309 | MAJOR | Plan Phase 1 removes field → BuildResponse reads t.Status at :309 → compilation fails. Need sequencing fix: either keep field until Phase 3 or change BuildResponse in Phase 1 to hardcode "pending". | Adversarial U5, orchestrator-verified |
| L2 | RuntimeType data source has residual plan inconsistency | MINOR | Plan §4.2 says "populate from ServiceMeta in BuildDeployTargets" but ServiceMeta has no RuntimeType (confirmed). Context.md Decision #2 says "from API, not ServiceMeta". Plan text needs correction. | Adversarial C4, context.md |
| L3 | Phase 3 checker wiring from tools layer underspecified | MINOR | Plan says "+40 lines" in tools/workflow_deploy.go but doesn't show how buildDeployStepChecker gets client/projectID/stateDir params. Bootstrap analogue exists (tools/workflow_checks.go) — pattern is available but should be referenced. | Adversarial L2 |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | GetServiceEnv() API latency | LOW | Not benchmarked; assumed lightweight | Correctness |
| U2 | STOPPED service status existence | LOW | Not observed live or in tests | KB-verifier |
| U3 | Events API hint field availability for all process types | LOW | Verified field exists; graceful degradation untested | Correctness |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | ctx missing from DeployComplete/Start — BLOCKING? | Adversarial: "BLOCKING gap" | engine.go:358,341 lack ctx | Correctness: "plan says to ADD it" | Plan §5 Phase 3: "+20 lines" and "+5 lines" for ctx | **RESOLVED — NOT BLOCKING.** Plan explicitly schedules ctx addition in Phase 3 (§5 line 464-465). Plan §3.1 Decision #2 says "context.Context on both". This is planned new work, not a missing detail. |
| D2 | BuildIterationDelta nil plan safety | Adversarial: "not tested with nil plan + iteration > 0" | — | Orchestrator-verified | bootstrap_guidance.go:94: parameter is `_ *ServicePlan` (underscore = ignored). Tests at :467 and :477 call with nil plan. | **RESOLVED — SAFE.** Parameter is ignored. Existing tests verify nil plan works (bootstrap_guidance_test.go:467: `BuildIterationDelta("deploy", 1, nil, "some failure")`). |
| D3 | Step constant reuse fragile? | Adversarial: "works by accident" | StepDeploy vs DeployStepDeploy same string value | Correctness: "plan §2.4 acknowledges this" | Plan line 127: "StepDeploy == DeployStepDeploy == 'deploy' — shared constant works" | **RESOLVED — INTENTIONAL.** Same string value is by design, documented in plan. Style concern, not correctness risk. Add comment in code during implementation. |
| D4 | stateDir same for deploy and bootstrap? | Adversarial: "not verified" | — | Orchestrator-verified | engine.go:27: `stateDir: baseDir` set once at Engine creation. Used by all methods. checkGenerate:29 comment: `{projectRoot}/.zcp/state/` | **RESOLVED — SAME PATH.** Engine uses one stateDir for all workflows. filepath.Dir(filepath.Dir(stateDir)) pattern works identically. |

### Key Insights from Knowledge Base

1. **assembleKnowledge actively injects at DeployStepPrepare** (guidance.go:86-98 via needsRuntimeKnowledge:67). Phase 2 must REMOVE this call from deploy.buildGuide(), not just add new behavior. Plan §2.4 v2 correctly flags this ("Problem: violates 'no knowledge injection in deploy' principle").

2. **Existing workflow_checks_deploy_test.go** (90 lines) tests bootstrap's checkDeploy function, NOT deploy workflow checkers. Plan Phase 3 will need to either rename this file or add deploy-workflow-specific tests alongside. Current test file name is misleading.

3. **BuildTransitionMessage already includes router offerings with Reasons** (bootstrap_guide_assembly.go:109-125). Phase 5 says "verify BuildTransitionMessage includes strategy prompt + deploy command" — it does. But it ALSO includes editorial Reasons via routeFromBootstrapState(). Phase 6 decision on Route philosophy affects Phase 5 transition message.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

| # | Item | Evidence | Fix |
|---|------|----------|-----|
| MA1 | Phase 1: DeployTarget.Status removal must update BuildResponse:309 | deploy.go:309 reads t.Status; removing field breaks compilation | Keep Status field in Phase 1 (remove only dead constants + UpdateTarget + DevFailed + Error + LastAttestation). Remove Status field in Phase 3 when checker populates DeployTargetOut.Status. |
| MA2 | Phase 2: Must actively REMOVE assembleKnowledge call from buildGuide | deploy.go:344-350 currently calls assembleKnowledge(); needsRuntimeKnowledge returns true for DeployStepPrepare | buildGuide rewrite must NOT call assembleKnowledge(). Knowledge pointers replace injection. |
| MA3 | Plan text: RuntimeType source inconsistency | §4.2 says "populate from ServiceMeta" but ServiceMeta has no RuntimeType (confirmed). Context.md #2 says "from API" | Correct §4.2: "RuntimeType fetched from API at deploy start time (ListServices returns service type). Stored in DeployTarget for buildGuide access." |

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)

| # | Item | Evidence | Fix |
|---|------|----------|-----|
| SA1 | Phase 3: Reference bootstrap checker wiring pattern | tools/workflow_checks.go shows how buildStepChecker gets client/projectID/stateDir | Add note to Phase 3: "Follow pattern from tools/workflow_checks.go:buildStepChecker — receives client, projectID, stateDir from handler closure" |
| SA2 | Phase 6: Decide Route philosophy before implementation | Router has boostByIntent + Reason = recommendation engine. Plan says "facts only" | Either (a) remove boostByIntent+Reason in Phase 6, or (b) document as intentional exception. Decision needed, but not blocking Phases 1-5. |
| SA3 | GuidanceParams field count | Plan §2.2 table header says "10 fields" | Correct to "11 fields" (already noted in context.md Decision #8) |
| SA4 | workflow_checks_deploy_test.go naming | Existing file tests bootstrap checkDeploy | Phase 3: Add deploy workflow tests in SEPARATE file (e.g., workflow_checks_deploy_workflow_test.go) or rename existing to workflow_checks_deploy_bootstrap_test.go |

### Investigate (UNVERIFIED but plausible)

| # | Item | Priority |
|---|------|----------|
| I1 | GetServiceEnv API latency benchmark | LOW — implement first, optimize if needed |
| I2 | STOPPED service status — does it exist? | LOW — checker handles unknown statuses gracefully |
| I3 | Events hint field coverage for all process types | LOW — graceful degradation in plan |

---

## Revised Version

See `plans/deploy-workflow-guidance-redesign.v3.md` for the revised plan incorporating all Must Address and Should Address items.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | §5 Phase 1 | Keep DeployTarget.Status field; remove only dead constants, methods, and dead fields (Error, LastAttestation) | deploy.go:309 reads t.Status in BuildResponse | [L1] Adversarial U5, orchestrator |
| 2 | §4.2 Phase 2 | Correct RuntimeType source: "from API via ListServices" not "from ServiceMeta" | service_meta.go:22-29 has no RuntimeType; context.md Decision #2 | [L2] Adversarial C4, context.md |
| 3 | §5 Phase 2 | Add explicit note: "REMOVE assembleKnowledge call from buildGuide" | deploy.go:344-350 currently calls it | [V3] All agents |
| 4 | §5 Phase 3 | Add reference to bootstrap wiring pattern (tools/workflow_checks.go) | Pattern exists and is reusable | [L3] Adversarial L2 |
| 5 | §2.2 | Correct field count to 11 | guidance.go:10-22 | [V4] KB-verifier |
| 6 | §5 Phase 3 | Note: existing workflow_checks_deploy_test.go tests bootstrap, add deploy-specific tests separately | KB-verifier claim #17 | [V7] KB-verifier |
| 7 | §5 Phase 6 | Flag Route decision as prerequisite: refactor or document exception | router.go:81-105 boostByIntent | [V5] Architecture |
