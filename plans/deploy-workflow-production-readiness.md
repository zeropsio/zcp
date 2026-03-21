# Deploy Workflow — Production Readiness Plan

**Date**: 2026-03-21 (verified against actual code state)
**Status**: Analysis complete, ready for implementation
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

### 3.2 Dead Per-Target Code

Still present with 0 production callers:

| Code | Location | Purpose |
|------|----------|---------|
| `UpdateTarget()` | deploy.go:238-251 | Set per-target status |
| `DevFailed()` | deploy.go:274-282 | Gate stage on dev failure |
| `DeployTarget.Error` | deploy.go:59 | Error field |
| `DeployTarget.LastAttestation` | deploy.go:60 | Attestation field |
| Status constants (deployed/verified/failed/skipped) | deploy.go:30-33 | Used by dead methods |
| `ResolveDeployGuidance()` | deploy_guidance.go:20-42 | Per-hostname strategy lookup — 0 callers |

### 3.3 No Iteration Escalation

`buildGuide()` passes `_ int` for iteration (deploy.go:341) — iteration counter is ignored. No `BuildIterationDelta()` equivalent for deploy. Agent gets identical guidance on every retry.

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
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation`, 4 status constants, `ResolveDeployGuidance()`
- Related tests: `TestDeployState_UpdateTarget`, `TestDeployState_DevFailed`
- `ResetForIteration()` Error clear line

**Enrich buildGuide():**
- Pass iteration counter (currently ignored `_ int`)
- Pass RuntimeType from first target (readable from ServiceMeta or infer from mode)
- Pass Iteration + FailureCount for future escalation

| File | Change | Est. |
|------|--------|------|
| deploy.go | Delete dead code | -40 |
| deploy_test.go | Delete 2 dead tests | -25 |
| deploy_guidance.go | Delete dead ResolveDeployGuidance | -23 |
| deploy_guidance_test.go | Update tests | -10 |
| deploy.go buildGuide | Pass iteration, runtimeType to GuidanceParams | +10 |

### Phase 2: Checkers

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
| workflow_deploy.go | Wire checker deps, call checker before advancing | +40 |
| workflow_checks.go (or new workflow_checks_deploy.go) | 3 new checkers | +120 |
| workflow_checks_deploy_test.go | Tests for 3 checkers | +100 |

### Phase 3: Iteration escalation

Add deploy iteration escalation:
- Tier 1: "Check zerops_logs severity=error; fix and redeploy"
- Tier 2: "Systematic: env var refs, service type, ports, start command"
- Tier 3: "Stop — present diagnostic summary to user"

| File | Change | Est. |
|------|--------|------|
| deploy.go or deploy_guidance.go | Build iteration delta for deploy | +25 |
| deploy.go buildGuide | Use iteration counter | +5 |

### Phase 4: Polish

- Document deploy iteration in spec
- Remove orphaned bootstrap.md verify section (from prior review)
- Consider removing DeployTarget.Status (always "pending") or populating from checker results

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
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | Reuse checkGenerate's path logic |
| Shared checker logic with bootstrap checkDeploy | LOW | Extract helpers or duplicate (small) |
| handleDeployComplete gets more complex | LOW | Mirror bootstrap's handleBootstrapComplete |
| Mixed strategies per deploy session | LOW | Gate in handleDeployStart (first meta's strategy used, validated consistent) |
