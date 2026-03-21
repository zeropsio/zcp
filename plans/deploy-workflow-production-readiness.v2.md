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
