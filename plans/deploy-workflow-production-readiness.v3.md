# Deploy Workflow — Production Readiness Plan

**Date**: 2026-03-21 (review-2 revision)
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
- `DeployTarget.Strategy` field exists (workflow/deploy.go:61) — populated from ServiceMeta
- `DeployState.Strategy` field exists (workflow/deploy.go:43) — set from first meta
- `BuildDeployTargets()` returns 3 values: targets, mode, strategy (workflow/deploy.go:130)
- `engine.DeployStart()` accepts strategy (workflow/engine.go:341)
- `handleDeployStart()` unpacks all 3 values (tools/workflow.go:238)

### Strategy flows through guidance
- `resolveDeployStepGuidance(step, mode, strategy)` accepts strategy (workflow/deploy_guidance.go:46)
- At deploy step: mode-specific section + strategy-specific section layered (workflow/deploy_guidance.go:69-73)
- `buildGuide()` passes `d.Strategy` to guidance (workflow/deploy.go:342)
- `GuidanceParams.Strategy` field exists (workflow/guidance.go:13)
- `buildGuide()` passes Strategy in GuidanceParams (workflow/deploy.go:347)

### Strategy sections in deploy.md exist
- `deploy-push-dev` — SSH self-deploy guidance
- `deploy-ci-cd` — git webhook guidance
- `deploy-manual` — manual deploy guidance
- These ARE delivered during deploy workflow via resolveDeployStepGuidance lines 69-73

### Knowledge injection works
- `assembleKnowledge()` receives Strategy in GuidanceParams (workflow/guidance.go:72)
- Runtime knowledge at deploy-prepare step (workflow/guidance.go:86-98)
- zerops.yml schema + rules at deploy-prepare (workflow/guidance.go:106-112)
- Env vars at deploy step (workflow/guidance.go:120-125)

### Strategy gate works
- `handleDeployStart` rejects if any runtime meta has empty DeployStrategy (tools/workflow.go:228-236)
- Mixed strategy gate rejects if targets have different strategies (tools/workflow.go:241-248)

---

## 3. What's Still Missing

### 3.1 No Platform-Level Validation (prepare step)

Deploy workflow has no validation at the prepare step. Agent self-attests zerops.yml is correct. Platform-level checks (syntax, hostname match) are objectively correct/incorrect and should be validated:

- zerops.yml exists and parses
- hostname entries match deploy targets
- env var reference syntax valid (`${hostname_varName}` format)

These are **always wrong if wrong** — not application-dependent.

**Env var validation data source**: Deploy workflow has no `DiscoveredEnvVars` (stored on `BootstrapState` only). Solution: re-discover via `client.GetServiceEnv()` at deploy-prepare step. This is cheap (one API call per dependency), handles env vars added after bootstrap, and keeps deploy fully standalone.

### 3.2 No Contextual Diagnostic Feedback (deploy/verify steps)

When deploy fails, the agent gets zero platform-aware diagnostic guidance. The Zerops deploy pipeline has clear failure points, each with different diagnostic paths:

| Where it broke | What to check | Zerops-specific context | Data source |
|---|---|---|---|
| **Build failed** | build logs, zerops.yml build section, dependencies, runtime version | Build container ≠ run container; different env | `buildLogs` array in deploy response |
| **Deploy failed** (container didn't start) | start command, ports, env vars, run section | New container = all local files lost, only `deployFiles` content persists | Service status (READY_TO_DEPLOY if first deploy) |
| **Runtime crash** (started then died) | runtime logs, env var references | `${hostname_varName}` typo = silent literal string, no error | `zerops_verify` checks + `zerops_logs` |
| **Runs but unreachable** | subdomain, routing, ports in zerops.yml vs app | Zerops routing, subdomain assignment | `SubdomainAccess` bool + `zerops_verify` http_check |

**Note**: Build failures are directly detectable (BUILD_FAILED status + buildLogs in deploy response). Post-deploy failures require `zerops_verify` + `zerops_logs` heuristics — degrade gracefully if log backend is unavailable. Consider using Events API `hint` field for human-readable status summaries.

### 3.3 Dead Per-Target Code

Still present with 0 production callers:

| Code | Location | Purpose |
|------|----------|---------|
| `UpdateTarget()` | workflow/deploy.go:248-260 | Set per-target status |
| `DevFailed()` | workflow/deploy.go:284-291 | Gate stage on dev failure |
| `DeployTarget.Error` | workflow/deploy.go:59 | Error field |
| `DeployTarget.LastAttestation` | workflow/deploy.go:60 | Attestation field |
| Status constants (deployed/verified/failed/skipped) | workflow/deploy.go:30-33 | Used by dead methods |
| `ResolveDeployGuidance()` | workflow/deploy_guidance.go:20-42 | Per-hostname strategy lookup — 0 callers |

**Note**: `deployTargetPending` (workflow/deploy.go:29) IS live — used in BuildDeployTargets and ResetForIteration. Must be preserved.

### 3.4 No Iteration Escalation

`buildGuide()` passes `_ int` for iteration AND `_ Environment` (workflow/deploy.go:341) — both parameters are ignored. Agent gets identical guidance on every retry.

**Structural gap**: Deploy's `buildGuide()` calls `resolveDeployStepGuidance()` + `assembleKnowledge()` separately, bypassing the unified `assembleGuidance()` function (workflow/guidance.go:27) that bootstrap uses.

**Step name compatibility**: `StepDeploy == DeployStepDeploy == "deploy"` — the deploy step works with assembleGuidance(). But `DeployStepPrepare="prepare"` and `DeployStepVerify="verify"` are deploy-specific and not handled by `resolveStaticGuidance()` (which serves bootstrap steps). Solution: keep `resolveDeployStepGuidance()` for static content, use `assembleGuidance()` for knowledge injection + iteration escalation only.

### 3.5 GuidanceParams Underutilized

`buildGuide()` passes only Step, Mode, Strategy, KP (workflow/deploy.go:344-348). Missing: RuntimeType, DependencyTypes, DiscoveredEnvVars, Iteration, FailureCount.

### 3.6 DeployTarget.Status Always "pending"

Targets in response always show `status: "pending"`. Never changes in production.

### 3.7 Wrong Error Code in Deploy Handlers

`handleDeployComplete`, `handleDeploySkip`, `handleDeployStatus` all use `platform.ErrBootstrapNotActive` for deploy errors (tools/workflow_deploy.go:29,51,62). Semantically incorrect — should use a deploy-specific or generic workflow error code.

---

## 4. Implementation Plan

### Phase 1: Dead code cleanup + GuidanceParams enrichment + error code fix

**Delete dead code:**
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation`, `ResolveDeployGuidance()`
- 4 dead status constants: `deployTargetDeployed`, `deployTargetVerified`, `deployTargetFailed`, `deployTargetSkipped`
- **Keep** `deployTargetPending` (live — used in BuildDeployTargets and ResetForIteration)
- Related tests: `TestDeployState_UpdateTarget`, `TestDeployState_DevFailed`
- `ResetForIteration()` Error clear line (workflow/deploy.go:274) — effectively dead
- Tests for `ResolveDeployGuidance` in deploy_guidance_test.go (4 test functions)

**Enrich buildGuide():**
- Use iteration parameter (currently ignored `_ int` → named `iteration int`)
- Use Environment parameter (currently ignored `_ Environment` → named `env Environment`)
- Pass RuntimeType from first target (readable from ServiceMeta)
- Pass Iteration + FailureCount to GuidanceParams

**Fix error codes:**
- Replace `platform.ErrBootstrapNotActive` with `platform.ErrDeployNotActive` (or `ErrWorkflowNotActive`) in tools/workflow_deploy.go (3 occurrences: lines 29, 51, 62)

| File | Change | Est. |
|------|--------|------|
| workflow/deploy.go | Delete dead code, keep deployTargetPending | -40 |
| workflow/deploy_test.go | Delete 2 dead tests | -25 |
| workflow/deploy_guidance.go | Delete dead ResolveDeployGuidance | -23 |
| workflow/deploy_guidance_test.go | Delete 4 dead tests for ResolveDeployGuidance | -40 |
| workflow/deploy.go buildGuide | Use iteration, env, pass runtimeType to GuidanceParams | +10 |
| tools/workflow_deploy.go | Fix 3 error codes: ErrBootstrapNotActive → ErrDeployNotActive | ~0 |
| platform/errors.go | Add ErrDeployNotActive constant | +1 |

### Phase 2: Platform validation + contextual diagnostics

**Design decisions:**
1. **Checker type**: `DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)` — separate from bootstrap's StepChecker (no ServicePlan).
2. **Context threading**: Add `context.Context` to both `engine.DeployComplete()` and `engine.DeployStart()`.
3. **File location**: `internal/tools/workflow_checks_deploy.go` (matching existing pattern).
4. **Env var source**: Re-discover via `client.GetServiceEnv()` at prepare step — keeps deploy standalone, handles post-bootstrap env var changes.

**Principle**: Validate only what is **objectively correct/incorrect** (platform integration). Everything application-specific is informational, not blocking.

**checkDeployPrepare(client, projectID, stateDir) — platform integration validation:**
- zerops.yml exists and parses correctly
- setup entries match target hostnames
- env var reference syntax valid (`${hostname_varName}` format check) — validated against env vars re-discovered from API via `client.GetServiceEnv()`
- **NOT**: "is the app ready" — we don't know what the app does

**checkDeployResult(client, projectID) — pipeline status + diagnostic feedback:**
- Query API: did build succeed? Are containers RUNNING?
- If build failed → diagnostic: "check build logs (available in deploy response), common issues: dependencies, runtime version mismatch"
- If container didn't start → diagnostic: "check start command, ports, env vars in zerops.yml run section. Note: deploy creates new container, local files lost"
- If container running → informational: "service running, access via subdomain X, check logs with zerops_logs"
- Consider using Events API `hint` field for LLM-friendly status summaries
- **NOT**: health check validation, **NOT**: "is the app working", **NOT**: hard dev→stage gate
- Standard mode: if dev shows errors in logs, **inform** ("dev service shows errors — review before stage deploy") — agent decides, not ZCP

| File | Change | Est. |
|------|--------|------|
| workflow/engine.go DeployComplete + DeployStart | Add context.Context params | +15 |
| tools/workflow_deploy.go | Wire checker deps, build checker, pass ctx + checker to engine | +35 |
| tools/workflow_checks_deploy.go | 2 checkers (prepare + result) + DeployStepChecker type + diagnostic builder + env var re-discovery | +120 |
| tools/workflow_checks_deploy_test.go | Tests for 2 checkers | +80 |

### Phase 3: Contextual iteration escalation

**Merge with diagnostics from Phase 2.** Iteration escalation = progressively more specific diagnostic guidance based on WHERE things keep failing.

**Approach**: Keep `resolveDeployStepGuidance()` for deploy-specific static content (reads deploy.md sections). Use `assembleGuidance()` for knowledge injection + iteration escalation only. This works because:
- `needsRuntimeKnowledge()` (guidance.go:67) already handles `DeployStepPrepare` — runtime/dependency knowledge path is deploy-aware
- `assembleKnowledge()` handles `StepDeploy == DeployStepDeploy == "deploy"` — env var + deploy rules path works
- `BuildIterationDelta()` fires for `StepDeploy` at iteration > 0 — iteration logic reachable

Deploy-specific iteration tiers:
- **Iteration 1** (first failure): "Check zerops_logs for the error. Build failed? → build log. Container crash? → runtime log, start command, env vars."
- **Iteration 2**: "Systematic check: zerops.yml config (ports, start command, deployFiles), env var references (typos become literal strings!), runtime version compatibility."
- **Iteration 3**: "Present diagnostic summary to user with: exact error from logs, current config state, env var values. User decides next step."

Key: escalation is about **better diagnostics**, not harder gates.

| File | Change | Est. |
|------|--------|------|
| workflow/deploy.go buildGuide | Integrate assembleGuidance() for knowledge + iteration | +15 |
| workflow/guidance.go or deploy_guidance.go | Deploy-specific iteration delta tiers | +30 |

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
- Strategy gate: handleDeployStart rejects if strategy missing (tools/workflow.go:228-236)
- Mixed strategy gate: handleDeployStart rejects mixed strategies (tools/workflow.go:241-248)
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
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | checkGenerate's filepath.Dir pattern is generic and reusable (verified) |
| Diagnostic quality depends on API status detail | MEDIUM | Build failures: buildLogs in deploy response (confirmed). Post-deploy: zerops_verify + zerops_logs heuristics. Events API hint field for LLM-friendly status. Degrade gracefully if log backend unavailable |
| Env var re-discovery adds API calls at prepare step | LOW | One GetServiceEnv call per dependency service — lightweight |
| handleDeployComplete gets more complex | LOW | Mirror bootstrap's handleBootstrapComplete pattern |
| Mixed strategies per deploy session | LOW | Gate in handleDeployStart (verified at workflow.go:241-248) |
| StepChecker type divergence (bootstrap vs deploy) | LOW | Keep separate — simpler types, no premature abstraction (only 2 types) |
| DeployComplete + DeployStart context threading | LOW | Straightforward — all callers already have context |
| Step name dispatch in assembleGuidance | LOW | Keep resolveDeployStepGuidance for static content; assembleGuidance for knowledge + iteration only |
