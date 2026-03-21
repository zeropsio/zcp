# Deep Review Report: deploy-workflow-production-readiness.v2.md — Review 2
**Date**: 2026-03-21
**Reviewed version**: plans/deploy-workflow-production-readiness.v2.md
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Is the plan ready for implementation? Does it make sense as-is?
**Resolution method**: Evidence-based (no voting)

---

## Input Document

```markdown
# Deploy Workflow — Production Readiness Plan

**Date**: 2026-03-21 (verified against actual code state, deep-reviewed)
**Status**: Ready for implementation
**Scope**: Standalone deploy workflow (`action="start" workflow="deploy"`)

---

## 1. Motivation & Philosophy

The standalone deploy workflow is the **post-bootstrap lifecycle workflow** — the primary way an LLM agent deploys and redeploys services.

### Core principle: Help, don't gatekeep

**We don't know what the user wants from their application.** We don't know how it should work, what state the code is in, whether they want health checks, or what "healthy" means for their app. We DO know:

- **Mode** of services (standard/dev/simple) — and they may want to change it
- **Strategy** (push-dev/ci-cd/manual) — and they may want to switch
- **Zerops platform mechanics** — how the deploy pipeline works, what survives deploy, env var behavior, zerops.yml schema

The deploy workflow should **maximize knowledge delivery and contextual diagnostics** — not impose assumptions about application correctness. Its value is in:
1. Validating **platform integration** (zerops.yml syntax, hostname matching) — things that are always wrong if wrong
2. Providing **contextual diagnostic guidance** when things break — pointing to the right place based on WHERE in the pipeline the failure occurred
3. Delivering **Zerops-specific knowledge** relevant to the current mode, strategy, and runtime
4. Supporting **mode/strategy transitions** (push-dev → ci-cd, dev → standard)

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

### 3.1 No Platform-Level Validation (prepare step)

Deploy workflow has no validation at the prepare step. Agent self-attests zerops.yml is correct. Platform-level checks (syntax, hostname match) are objectively correct/incorrect and should be validated:

- zerops.yml exists and parses
- hostname entries match deploy targets
- env var reference syntax valid (`${hostname_varName}` format)

These are **always wrong if wrong** — not application-dependent.

### 3.2 No Contextual Diagnostic Feedback (deploy/verify steps)

When deploy fails, the agent gets zero platform-aware diagnostic guidance. The Zerops deploy pipeline has clear failure points, each with different diagnostic paths:

| Where it broke | What to check | Zerops-specific context |
|---|---|---|
| **Build failed** | build logs (`zerops_logs`), zerops.yml build section, dependencies, runtime version | Build container ≠ run container; different env |
| **Deploy failed** (container didn't start) | start command, ports, env vars, run section | New container = all local files lost, only `deployFiles` content persists |
| **Runtime crash** (started then died) | runtime logs, env var references | `${hostname_varName}` typo = silent literal string, no error |
| **Runs but unreachable** | subdomain, routing, ports in zerops.yml vs app | Zerops routing, subdomain assignment |

This diagnostic feedback should be the **primary value** of the deploy step — not blocking, but guiding.

### 3.3 Dead Per-Target Code

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

### 3.4 No Iteration Escalation

`buildGuide()` passes `_ int` for iteration AND `_ Environment` (deploy.go:341) — both parameters are ignored. Agent gets identical guidance on every retry.

**Structural gap**: Deploy's `buildGuide()` calls `resolveDeployStepGuidance()` + `assembleKnowledge()` separately, bypassing the unified `assembleGuidance()` function (guidance.go:27) that bootstrap uses.

### 3.5 GuidanceParams Underutilized

`buildGuide()` passes only Step, Mode, Strategy, KP (deploy.go:344-348). Missing: RuntimeType, DependencyTypes, DiscoveredEnvVars, Iteration, FailureCount.

### 3.6 DeployTarget.Status Always "pending"

Targets in response always show `status: "pending"`. Never changes in production.

---

## 4. Implementation Plan

### Phase 1: Dead code cleanup + GuidanceParams enrichment

**Delete dead code:**
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation`, `ResolveDeployGuidance()`
- 4 dead status constants: `deployTargetDeployed`, `deployTargetVerified`, `deployTargetFailed`, `deployTargetSkipped`
- **Keep** `deployTargetPending` (live — used in BuildDeployTargets and ResetForIteration)
- Related tests: `TestDeployState_UpdateTarget`, `TestDeployState_DevFailed`
- `ResetForIteration()` Error clear line (deploy.go:274) — effectively dead
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

### Phase 2: Platform validation + contextual diagnostics

**Design decisions:**
1. **Checker type**: `DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)` — separate from bootstrap's StepChecker (no ServicePlan).
2. **Context threading**: Add `context.Context` to `engine.DeployComplete()`.
3. **File location**: `internal/tools/workflow_checks_deploy.go` (matching existing pattern).

**Principle**: Validate only what is **objectively correct/incorrect** (platform integration). Everything application-specific is informational, not blocking.

**checkDeployPrepare(stateDir) — platform integration validation:**
- zerops.yml exists and parses correctly
- setup entries match target hostnames
- env var reference syntax valid (`${hostname_varName}` format check)
- **NOT**: "is the app ready" — we don't know what the app does

**checkDeployResult(client, projectID) — pipeline status + diagnostic feedback:**
- Query API: did build succeed? Are containers RUNNING?
- If build failed → diagnostic: "check build logs, common issues: dependencies, runtime version mismatch"
- If container didn't start → diagnostic: "check start command, ports, env vars in zerops.yml run section. Note: deploy creates new container, local files lost"
- If container running → informational: "service running, access via subdomain X, check logs with zerops_logs"
- **NOT**: health check validation, **NOT**: "is the app working", **NOT**: hard dev→stage gate
- Standard mode: if dev shows errors in logs, **inform** ("dev service shows errors — review before stage deploy") — agent decides, not ZCP

| File | Change | Est. |
|------|--------|------|
| engine.go DeployComplete | Add context.Context + DeployStepChecker params | +15 |
| tools/workflow_deploy.go | Wire checker deps, build checker, pass to engine | +35 |
| tools/workflow_checks_deploy.go | 2 checkers (prepare + result) + DeployStepChecker type + diagnostic builder | +100 |
| tools/workflow_checks_deploy_test.go | Tests for 2 checkers | +80 |

### Phase 3: Contextual iteration escalation

**Merge with diagnostics from Phase 2.** Iteration escalation = progressively more specific diagnostic guidance based on WHERE things keep failing.

**Recommended approach**: Unify deploy's `buildGuide()` with `assembleGuidance()` (guidance.go:27) to reuse `BuildIterationDelta`. Deploy-specific iteration tiers:

- **Iteration 1** (first failure): "Check zerops_logs for the error. Build failed? → build log. Container crash? → runtime log, start command, env vars."
- **Iteration 2**: "Systematic check: zerops.yml config (ports, start command, deployFiles), env var references (typos become literal strings!), runtime version compatibility."
- **Iteration 3**: "Present diagnostic summary to user with: exact error from logs, current config state, env var values. User decides next step."

Key: escalation is about **better diagnostics**, not harder gates. Each iteration the agent gets more specific guidance about WHERE to look, with Zerops-specific knowledge about what commonly goes wrong.

| File | Change | Est. |
|------|--------|------|
| deploy.go buildGuide | Switch to assembleGuidance() | +15 |
| guidance.go or deploy_guidance.go | Deploy-specific iteration delta tiers | +30 |

### Phase 4: Polish

- Document deploy diagnostics in deploy.md
- Remove orphaned bootstrap.md verify section
- Update DeployTarget.Status to reflect API status (running/failed/building) from checker results
- Consider strategy transition support (update ServiceMeta when user switches strategy)

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
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | Verify checkGenerate reusability; may need reimplementation if coupled to bootstrap state |
| Diagnostic quality depends on API status detail | MEDIUM | Verify what Zerops API returns for failed builds/deploys; degrade gracefully if status is opaque |
| handleDeployComplete gets more complex | LOW | Mirror bootstrap's handleBootstrapComplete pattern |
| Mixed strategies per deploy session | LOW | Gate in handleDeployStart (validated consistent) |
| StepChecker type divergence (bootstrap vs deploy) | LOW | Keep separate — simpler types, no premature abstraction |
| DeployComplete context threading | LOW | Straightforward — all callers already have context |
```

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Documentation Facts:**
- Pipeline is: build → deploy artifacts → start runtime [DOCUMENTED]
- `zerops_deploy` blocks until completion, returns DEPLOYED or BUILD_FAILED [VERIFIED: deploy.md]
- zerops.yml `setup:` key maps hostname to config, must be at repository root [DOCUMENTED]
- Invalid env var references silently kept as literal strings — no error [MEMORY: verified 2026-03-04]
- Deploy always creates new container, local files LOST [MEMORY: verified 2026-03-04]
- Subdomain must be explicitly enabled per service [VERIFIED: workflow_checks.go:199-211]
- No separate "build status" or "deploy stage" API field — zerops_deploy returns DEPLOYED/BUILD_FAILED, post-deploy needs zerops_verify + zerops_logs heuristics [UNCHECKED]

**Codebase Facts:**
- handleDeployStart: ServiceMeta → BuildDeployTargets → engine.DeployStart [VERIFIED: workflow.go:187-258]
- Strategy gate exists: rejects empty DeployStrategy [VERIFIED: workflow.go:228-236]
- Mixed strategy gate exists [VERIFIED: workflow.go:241-248]
- Bootstrap buildGuide → assembleGuidance() with 10 GuidanceParams fields [VERIFIED]
- Deploy buildGuide → resolveDeployStepGuidance() + assembleKnowledge() SEPARATELY, 4 fields only [VERIFIED]
- Deploy buildGuide ignores iteration (`_ int`) and Environment (`_ Environment`) [VERIFIED: deploy.go:341]
- BuildIterationDelta fires for StepDeploy and iteration > 0; StepDeploy == DeployStepDeploy == "deploy" [VERIFIED]
- ALL dead code items confirmed 0 production callers [VERIFIED]
- deployTargetPending confirmed LIVE (lines 154, 164, 273) [VERIFIED]
- checkGenerate coupled to ServicePlan + BootstrapState [VERIFIED: workflow_checks_generate.go:23]
- checkGenerate path resolution generic and reusable [VERIFIED: line 29]
- StepCheckResult/StepCheck generic, not bootstrap-specific [VERIFIED: bootstrap_checks.go:6-17]
- DiscoveredEnvVars NOT available in deploy workflow — only on BootstrapState [VERIFIED]
- handleDeployComplete uses ErrBootstrapNotActive for deploy errors [VERIFIED: workflow_deploy.go:29,51,62]

**Risks Plan Underestimates:**
1. DiscoveredEnvVars unavailability — plan lists as missing but doesn't address WHERE they come from for deploy
2. Phase 2 diagnostic granularity may degrade to zerops_verify + zerops_logs heuristics

### Platform Verification Results (kb-verifier)

| # | Claim | Status | Key Finding |
|---|-------|--------|-------------|
| 1 | Service status fields | CONFIRMED | ACTIVE, RUNNING, STOPPED, READY_TO_DEPLOY |
| 2 | Build/deploy failure visibility | CONFIRMED | BUILD_FAILED with buildLogs array in deploy response |
| 3 | Container status after failed deploy | PARTIAL | First fail → READY_TO_DEPLOY; re-deploy fail → previous stays ACTIVE |
| 4 | Env var reference typos | CONFIRMED | Silent literal strings, no error (verified 2026-03-04) |
| 5 | Subdomain access queryable | CONFIRMED | SubdomainAccess bool + zeropsSubdomain env var + error codes |
| 6 | Log availability after failure | PARTIAL | Build logs in deploy response (yes); runtime logs via API (depends on backend) |
| 7 | Service metadata for diagnostics | CONFIRMED | Rich: hostname, type, status, ports, events, verify checks, hint field |

---

## Stage 2: Analysis Reports

### Correctness Analysis (correctness agent)

**Assessment**: SOUND with CRITICAL GAPS
**Evidence basis**: 18/18 VERIFIED

**Findings:**
- [F1] Zero checkers in deploy workflow — CRITICAL — engine.DeployComplete has no checker parameter, unlike BootstrapComplete
- [F2] Dead code confirmed — 6 items with 0 callers — VERIFIED complete
- [F3] Asymmetry in builder pattern — MAJOR — bootstrap uses assembleGuidance (10 fields), deploy uses separate path (4 fields)
- [F4] Missing fields block runtime knowledge injection — MAJOR — RuntimeType and DependencyTypes empty in deploy GuidanceParams, so deploy-prepare gets zero runtime briefings
- [F5] Env var injection inconsistency — MINOR — different paths for bootstrap vs deploy
- [F6] DevFailed gate not wired — MINOR — logic exists but unenforced

**Verdict**: READY FOR IMPLEMENTATION. Phases correctly ordered, dead code verified, Phase 1 immediately implementable.

### Architecture Analysis (architecture agent)

**Assessment**: SOUND BUT TWO CORRECTIONS REQUIRED
**Evidence basis**: 15 VERIFIED + 2 corrections

**Findings:**
- [F1] Location ambiguity — MINOR — plan references "deploy.go" without "workflow/" prefix
- [F2] Dead code 100% accurate — VERIFIED
- [F3] Strategy flow complete — VERIFIED
- [F4] Strategy guidance complete — VERIFIED
- [F5] Context threading gap — VERIFIED — DeployComplete needs ctx for Phase 2 checkers
- [F6] GuidanceParams 4/10 fields used — VERIFIED
- [F7] Iteration escalation: assembleGuidance() feasibility question — Phase 3 needs to specify approach for step name handling
- [F10] DeployTarget.Status always pending — VERIFIED harmless
- [F12] Mixed-mode deploy: gate exists at workflow.go:241-248 — VERIFIED

**Verdict**: IMPLEMENTATION-READY with noted corrections.

### Security Analysis (security agent)

**Assessment**: SOUND
**Evidence basis**: 6 VERIFIED, 0 UNVERIFIED

**Findings:**
- [F1] Env var validation requires design choice — MAJOR — DiscoveredEnvVars not in deploy flow
- [F2] Context threading sound — MINOR — straightforward propagation
- [F3] Dev→stage gate sound — MINOR — read-only check, no injection vectors
- [F4] Dead code cleanup safe — MINOR
- [F5] Iteration escalation safe — MINOR — advisory text only
- [F6] No new credential exposure — VERIFIED

**Verdict**: IMPLEMENTATION-READY. One design decision (DiscoveredEnvVars source) required for Phase 2.

### Adversarial Analysis (adversarial agent)

**Assessment**: CONCERNS (3 "blocking" issues claimed)
**Evidence basis**: 3 CRITICAL claimed, partially valid

**Findings:**
- [C1] Step naming mismatch breaks assembleGuidance() — CRITICAL claimed
- [C2] buildGuide bypasses iteration — CRITICAL claimed (but plan already says this)
- [C3] DiscoveredEnvVars data flow broken — CRITICAL claimed
- [M1] ResolveDeployGuidance deletion creates rework — MEDIUM claimed
- [M2] handleDeployComplete wrong error code — MEDIUM (pre-existing)

**Challenges to other analysts:**
- R1 checked factual accuracy but not implementation feasibility
- Strategy gate "NOT FOUND" (adversarial claim — REFUTED by orchestrator)

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | Dead code list 100% accurate, all 0 callers confirmed | — | grep verification across full codebase | All 4 agents |
| V2 | Deploy buildGuide passes 4/10 GuidanceParams fields, ignores iteration + Environment | MAJOR | deploy.go:341-348 signature with `_ int, _ Environment` | Correctness F3, Architecture F6 |
| V3 | DiscoveredEnvVars not available in deploy workflow | MAJOR | Only stored on BootstrapState, DeployState has no equivalent | All 4 agents |
| V4 | engine.DeployComplete lacks context.Context | MAJOR | engine.go:358 vs engine.go:137 (BootstrapComplete) | Architecture F5 |
| V5 | Strategy gate exists and works | — | workflow.go:227-236 (empty strategy), 241-248 (mixed strategy) | Orchestrator verified |
| V6 | handleDeployComplete uses wrong error code (ErrBootstrapNotActive) | MINOR | workflow_deploy.go:29,51,62 | Adversarial M2 |
| V7 | StepCheckResult/StepCheck are generic, reusable for deploy | — | bootstrap_checks.go:6-17, no bootstrap-specific fields | KB-research |
| V8 | checkGenerate path resolution is generic, reusable | — | workflow_checks_generate.go:29 uses filepath.Dir | KB-research |
| V9 | Build failure returns buildLogs in deploy response | — | Live platform verification | KB-verifier claim 2 |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | Phase 3 assembleGuidance() unification needs step name dispatch | MAJOR | resolveStaticGuidance (guidance.go:56) handles StepGenerate/StepDeploy/StepClose only; DeployStepPrepare="prepare" falls through to ResolveGuidance() which reads bootstrap.md (wrong source). StepDeploy=="deploy"==DeployStepDeploy works. But prepare/verify need deploy.md dispatch. | Adversarial C1, orchestrator verification |
| L2 | Env var ref validation in checkDeployPrepare needs data source decision | MAJOR | checkGenerate uses state.DiscoveredEnvVars + plan liveHostnames (workflow_checks_generate.go:92-94). Deploy has neither. Options: (a) re-discover via API, (b) skip env var validation, (c) read from ServiceMeta | Security F1, KB-research |
| L3 | Phase 2 diagnostic granularity will rely on zerops_verify + zerops_logs heuristics, not single API status | MINOR | zerops_deploy returns only DEPLOYED/BUILD_FAILED. Post-deploy failures (crash vs unreachable) need zerops_verify checks + zerops_logs entries | KB-verifier claims 2,6 |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | Strategy transition support (Phase 4) — no mechanism to update ServiceMeta.DeployStrategy | LOW | Would need new action or ServiceMeta update path; not blocking | KB-research |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | Strategy gate exists? | Adversarial: "NOT FOUND IN DEPLOY WORKFLOW CODE" | No evidence cited | Orchestrator: exists at workflow.go:227-248 | Direct code reading | **REFUTED** — gate exists, adversarial was wrong |
| D2 | C1 is a BLOCKER? | Adversarial: "IMPOSSIBLE without renaming" | resolveStaticGuidance only handles bootstrap steps | Orchestrator: StepDeploy=="deploy" works; prepare/verify need dispatch but plan allocates +15/+30 lines for this | Code shows partial compatibility | **DOWNGRADED to MAJOR** — underspecification in plan, not a blocker. Phase 3 scope (+45 lines) accounts for this work. |
| D3 | C2 is a new finding? | Adversarial: "BLOCKER — iteration escalation doesn't exist" | deploy.go:341 `_ int` | Plan: Section 3.4 explicitly states "both parameters are ignored" and Phase 1/3 propose fixing this | Plan text | **DISMISSED** — plan already identifies and addresses this |
| D4 | M1 — deleting ResolveDeployGuidance creates rework? | Adversarial: "Phase 2 will likely re-implement" | Speculative | CLAUDE.md: "Remove, don't disable. Git preserves history" | Project convention | **DISMISSED** — dead code deletion is correct; reimplementation if needed is trivial |

### Key Insights from Knowledge Base

1. **Build logs are embedded in deploy response** (KB-verifier claim 2) — checkDeployResult can access build failure details without extra API call. This REDUCES the diagnostic granularity risk.

2. **Events API has `hint` field** (KB-verifier claim 7) — human-readable status summaries ideal for LLM consumption. Plan doesn't mention using events API for diagnostics — could improve Phase 2 quality significantly.

3. **Service status after failed deploy depends on history** (KB-verifier claim 3) — first deploy fail = READY_TO_DEPLOY, re-deploy fail = previous version stays ACTIVE. Diagnostic builder must handle both scenarios.

4. **needsRuntimeKnowledge() already handles DeployStepPrepare** (guidance.go:67) — the runtime knowledge path is already deploy-aware. Only resolveStaticGuidance() is bootstrap-only. This makes Phase 3 unification easier than adversarial claims.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

1. **[V3/L2] Resolve DiscoveredEnvVars data source for deploy** — Plan must specify: (a) re-discover via client.GetServiceEnv() at deploy-prepare, (b) skip env var ref validation in deploy (bootstrap already validated), or (c) read from ServiceMeta if stored during bootstrap. Recommendation: option (a) for correctness — re-discover is cheap and handles env vars added after bootstrap.

2. **[L1] Add step name dispatch note to Phase 3** — Phase 3 must handle `DeployStepPrepare` and `DeployStepVerify` in assembleGuidance(). Options: (a) add deploy step dispatch to resolveStaticGuidance(), (b) keep resolveDeployStepGuidance() as the deploy equivalent, calling assembleKnowledge() separately. Recommendation: option (b) — keep deploy's own static guidance resolution but switch to assembleGuidance() for knowledge + iteration only.

3. **[V2] Phase 1 must wire ignored parameters** — buildGuide must use iteration and Environment parameters, not ignore them with `_`. Plan already says this but should be explicit: change signature from `_ int, _ Environment` to named parameters.

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)

4. **[V6] Fix ErrBootstrapNotActive in deploy handlers** — workflow_deploy.go uses bootstrap error code for deploy errors. Add ErrDeployNotActive or use generic ErrWorkflowNotActive. Pre-existing bug, can be fixed in Phase 1.

5. **[V4] Thread context to both DeployComplete AND DeployStart** — Architecture agent notes DeployStart also lacks context. Phase 2 should add context to both methods when adding checkers.

### Investigate (UNVERIFIED but plausible)

6. **[U1] Strategy transition mechanism** — Phase 4 mentions "consider strategy transition support" but no concrete design. Defer to Phase 4 implementation.

7. **[L3] Events API hint field for diagnostics** — KB-verifier found `hint` field in events with human-readable status. Worth investigating for Phase 2 diagnostic builder.

---

## Revised Version

See `plans/deploy-workflow-production-readiness.v3.md` for the revised plan incorporating all Must Address and Should Address items.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | §3.5 | Added DiscoveredEnvVars source decision: re-discover via API at deploy-prepare | KB-FACT: DiscoveredEnvVars only on BootstrapState | Security F1, KB-research |
| 2 | Phase 1 | Added: fix ErrBootstrapNotActive → ErrDeployNotActive in workflow_deploy.go | VERIFIED: workflow_deploy.go:29,51,62 | Adversarial M2 |
| 3 | Phase 2 | Added: context threading for DeployStart (not just DeployComplete) | VERIFIED: engine.go:341 also lacks ctx | Architecture F5 |
| 4 | Phase 2 | Added: env var re-discovery via client.GetServiceEnv() for checkDeployPrepare | VERIFIED: checkGenerate needs DiscoveredEnvVars which deploy lacks | L2 |
| 5 | Phase 3 | Clarified: keep resolveDeployStepGuidance() for static content, use assembleGuidance() for knowledge + iteration only | VERIFIED: resolveStaticGuidance (guidance.go:56) is bootstrap-only for prepare/verify steps | Adversarial C1, orchestrator |
| 6 | §6 Risks | Added: Events API hint field as diagnostic opportunity | KB-PLATFORM: zerops_events returns hint field | KB-verifier claim 7 |
