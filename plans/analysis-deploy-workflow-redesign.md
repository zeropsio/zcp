# Analysis: Deploy Workflow Redesign — From Skeleton to Production

**Date**: 2026-03-21
**Status**: Analysis complete, ready for implementation planning
**Scope**: Standalone deploy workflow (`action="start" workflow="deploy"`) — the post-bootstrap lifecycle workflow for autonomous LLM deployment
**Team**: deploy-mapper, bootstrap-comparator, usecase-analyst
**Prior work**: analysis-deploy-target-tracking (dead code), analysis-workflow-flow-completeness (all-workflow review)

---

## Problem Statement

The standalone deploy workflow is a **thin state machine with zero validation**. Bootstrap was refined to production quality (5 steps, 3 real checkers, per-target feedback, escalating iteration). Deploy workflow was left as a skeleton: 3 steps, 0 checkers, agent self-attests everything. An LLM agent using deploy is flying blind.

This matters because deploy is the **ongoing lifecycle workflow** — users trigger it repeatedly after bootstrap. It's where autonomous LLM operation provides the most value: "deploy my app", "fix the broken deploy", "push the update to stage".

---

## Current State — What Exists

### Flow

```
User: "deploy my updated code"
  → Router suggests deploy (intent pattern match)
  → Agent: zerops_workflow action="start" workflow="deploy"

handleDeployStart():                              # workflow.go:187-247
  1. Read ServiceMetas from stateDir               # service_meta.go
  2. Reject if incomplete or missing
  3. Filter to runtime services only
  4. Strategy gate: if any service has no strategy → return selection guidance
  5. BuildDeployTargets() → ordered targets (dev before stage)
  6. engine.DeployStart() → create session, return step 1 guidance

Agent walks through 3 steps:
  prepare  → agent self-attests (NO checker)
  deploy   → agent self-attests (NO checker)
  verify   → agent self-attests (NO checker) → session deleted
```

### What Agent Gets at Each Step

| Step | Guidance | Tools | Checker | Per-target feedback |
|------|----------|-------|---------|-------------------|
| prepare | deploy-prepare section: "run discover, check zerops.yml" | zerops_discover, zerops_knowledge | NONE | NONE |
| deploy | Mode-specific sections + iteration guidance | zerops_deploy, zerops_subdomain, zerops_logs, zerops_verify, zerops_manage | NONE | NONE (targets always "pending") |
| verify | deploy-verify section: "read verify results, map failures" | zerops_verify, zerops_discover | NONE | NONE |

### What Bootstrap Has (for comparison)

| Step | Checker | What it validates |
|------|---------|------------------|
| provision | checkProvision() | Service exists, RUNNING, type matches plan, env vars available |
| generate | checkGenerate() | zerops.yml exists, YAML valid, setup entries match, env var refs valid |
| deploy | checkDeploy() | VerifyAll (HTTP, logs, startup) + subdomain access per target |

### Dead Code in deploy.go

| Code | Lines | Purpose | Callers |
|------|-------|---------|---------|
| `UpdateTarget()` | 238-251 | Set per-target status | 0 prod (test only) |
| `DevFailed()` | 274-282 | Gate stage on dev failure | 0 prod (test only) |
| `DeployTarget.Error` | 58 | Error message | Written by dead UpdateTarget |
| `DeployTarget.LastAttestation` | 59 | Last attestation | Written by dead UpdateTarget |
| Status constants (deployed/verified/failed/skipped) | 30-33 | Target states | Used by dead methods |

---

## When Deploy Is Used (Use Cases)

1. **Post-bootstrap redeployment** — Code changed, push updates. Most common.
2. **Config change** — zerops.yml modified (envVars, ports, buildCommands). Requires full redeploy.
3. **Failed deploy retry** — Previous deploy failed. Agent diagnoses → fixes → redeploys.
4. **Dev→stage promotion** — Dev verified, deploy to stage.
5. **Router-suggested** — Project CONFORMANT + push-dev strategy → router offers deploy as P1.

### Per-Strategy Agent Behavior

| Strategy | Agent does | Validation needs |
|----------|-----------|-----------------|
| push-dev | SSH self-deploy via zerops_deploy, manual server start, test endpoints | Full: health check, startup, HTTP, subdomain |
| ci-cd | Guide user to connect repo, monitor build via zerops_process | Lighter: verify build triggered, health after deploy |
| manual | Direct zerops_deploy, minimal guidance | Basic: health check only |

---

## Gap Analysis — What's Missing

### P0: No Validation (Critical)

**The deploy workflow trusts the agent completely.** Every step advances on attestation alone. Agent can claim "deployed and verified" when services are actually broken.

Bootstrap learned this lesson: checkers are the enforcement mechanism. They run BEFORE step advances, return per-target pass/fail, and block progression until issues are fixed.

Deploy needs the same pattern:
- **Prepare checker**: zerops.yml exists + valid YAML + setup entries match targets + env var refs resolvable
- **Deploy checker**: VerifyAll (HTTP, logs, startup) + subdomain enabled + dev gates stage (standard mode)
- **Verify checker**: Redundant with deploy checker if merged (like bootstrap did) — OR: keep as lightweight final health confirmation

### P1: No Iteration Escalation

Bootstrap has `BuildIterationDelta()` with 3 tiers:
- Tier 1 (iterations 1-2): "Check logs, fix obvious issues"
- Tier 2 (iterations 3-4): "Systematic diagnosis — env vars, config, dependencies"
- Tier 3 (iterations 5+): "Stop and ask user"

Deploy has: identical guidance every iteration. No escalation. Agent loops forever or gives up randomly.

### P1: Env Vars Delayed

Bootstrap injects discovered env vars at the generate step (when agent writes zerops.yml). Deploy injects them at the deploy step (too late — agent already validated zerops.yml at prepare without knowing env var names).

### P2: Verify Step Redundancy

Bootstrap merged verify into the deploy checker. Deploy still has a separate verify step with no checker. Two options:
- Merge into deploy step (like bootstrap)
- Keep separate but add a real checker

### P2: DeployTarget.Status Always Pending

Agent sees `targets: [{hostname: "appdev", status: "pending"}]` in every response. Status never changes. Confusing display that suggests nothing happened.

---

## Design Proposal

### Principle: Checkers as Gates, API as Source of Truth

No per-target persistence. Checkers query Zerops API each time. Results flow through `StepCheckResult.Checks[]` — same mechanism bootstrap uses. Agent sees per-target pass/fail with actionable details.

### Proposed Step Structure

**Option A: Keep 3 steps, add checkers**

```
prepare  → checkDeployPrepare()  : zerops.yml valid, env var refs valid
deploy   → checkDeployExecute()  : VerifyAll + subdomain + dev→stage gate
verify   → checkDeployVerify()   : Final health confirmation (lighter check)
```

**Option B: Merge to 2 steps (like bootstrap)**

```
prepare  → checkDeployPrepare()  : zerops.yml valid, env var refs valid
deploy   → checkDeployFull()     : VerifyAll + subdomain + dev→stage gate (verify merged)
```

**Recommendation: Option A (keep 3 steps)**. Deploy's verify step serves a different purpose than bootstrap's merged verify — it's the agent's final confirmation checkpoint, and keeping it separate allows the agent to iterate on deploy without re-verifying everything. The checker can be lighter (just health check, no subdomain re-check).

### Checker Design

**checkDeployPrepare(stateDir string) StepChecker**
- Validate zerops.yml exists at projectRoot/ or projectRoot/{hostname}/
- Parse YAML, check setup entries match target hostnames
- Validate env var references against discovered vars (if available)
- Fail with specific "missing setup entry for {hostname}" or "invalid env var ref ${X}"

**checkDeployExecute(client, fetcher, projectID, httpClient) StepChecker**
- Reuse `ops.VerifyAll()` for per-target health (same as bootstrap's checkDeploy)
- Check subdomain access for services with ports
- Standard mode: if dev target is unhealthy → fail with "fix dev before stage deploy"
- Return per-target checks in StepCheckResult

**checkDeployVerify(client, fetcher, projectID, httpClient) StepChecker**
- Lighter version: just VerifyAll health check
- No subdomain re-check (already done in deploy step)
- If iteration >= threshold → add warning "consider asking user for help"

### Where Checkers Hook In

Current `handleDeployComplete()` (workflow_deploy.go:12-34) has no checker. Change:

```
handleDeployComplete():
  if input.Step is deploy workflow step:
    build checker (like bootstrap does)
    load state
    run checker
    if checker fails → return response with CheckResult, don't advance step
    if checker passes → advance step
```

This requires `handleDeployComplete()` to accept client/fetcher/httpClient params (currently it doesn't). Two approaches:
- **A**: Pass dependencies through RegisterWorkflow → handleDeployComplete (mirror bootstrap)
- **B**: Build checkers in workflow.go and pass to handleDeployComplete (cleaner separation)

### Iteration Escalation

Add deploy-specific iteration delta to `BuildIterationDelta()` or create `BuildDeployIterationDelta()`:
- Tier 1: "Check zerops_logs severity=error; verify zerops.yml env vars; fix and redeploy"
- Tier 2: "Systematic: check all env var refs, verify service type, check ports/start command"
- Tier 3: "Max iterations reached — present diagnostic summary to user, ask for guidance"

### Env Var Timing

Move env var injection to `DeployStepPrepare` in `assembleKnowledge()`. Agent needs env var names when validating zerops.yml at prepare step, not at deploy step.

### Dead Code Removal

Delete as part of this work:
- `UpdateTarget()`, `DevFailed()`, `Error`, `LastAttestation`, 4 dead status constants
- Related tests (`TestDeployState_UpdateTarget`, `TestDeployState_DevFailed`)
- `ResetForIteration()` Error clear line

DevFailed logic reimplemented as inline check in `checkDeployExecute()` — pure API query, no persistence.

---

## Implementation Sequence

### Phase 1: Foundation (dead code + checker wiring)

1. Delete dead per-target code from deploy.go + deploy_test.go
2. Wire `handleDeployComplete()` to accept checker dependencies (client, fetcher, httpClient)
3. Add `buildDeployStepChecker()` function (separate from bootstrap's `buildStepChecker`)
4. Write RED tests for each checker

### Phase 2: Checkers

5. Implement `checkDeployPrepare()` — zerops.yml validation + env var refs
6. Implement `checkDeployExecute()` — VerifyAll + subdomain + dev→stage gate
7. Implement `checkDeployVerify()` — health confirmation
8. GREEN tests pass

### Phase 3: Guidance improvements

9. Move env var injection to prepare step
10. Add deploy iteration escalation (BuildDeployIterationDelta or extend existing)
11. Update deploy.md content if needed for checker-aware guidance

### Phase 4: Cleanup

12. Update spec (document deploy iteration, checker behavior)
13. Remove orphaned bootstrap.md verify section (from prior review)

---

## What Changes Per File

| File | Change | Est. lines |
|------|--------|-----------|
| `internal/workflow/deploy.go` | Delete dead code (UpdateTarget, DevFailed, Error, LastAttestation, 4 constants) | -40 |
| `internal/workflow/deploy_test.go` | Delete 2 dead tests | -25 |
| `internal/tools/workflow_deploy.go` | Wire checker dependencies, call checker before advancing | +40 |
| `internal/tools/workflow_checks.go` | Add checkDeployPrepare, checkDeployExecute, checkDeployVerify | +80 |
| `internal/tools/workflow_checks_deploy_test.go` | Tests for 3 new checkers | +120 |
| `internal/workflow/guidance.go` | Move env var injection to prepare step | +5 |
| `internal/workflow/deploy_guidance.go` | Add iteration delta support | +20 |

**Net**: ~+200 lines, -65 lines = +135 lines. Deploy goes from skeleton to production-quality.

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|-----------|
| checkDeployPrepare needs to find zerops.yml — path depends on mount mode | MEDIUM | Reuse checkGenerate's path resolution logic (already handles mount vs local) |
| checkDeployExecute shares logic with bootstrap's checkDeploy | LOW | Extract shared helpers (checkServiceHealth, checkSubdomain) — don't duplicate |
| handleDeployComplete gets more complex with checker wiring | LOW | Mirror bootstrap's handleBootstrapComplete pattern exactly |
| Deploy checkers need Plan data but deploy workflow has no Plan | MEDIUM | Checkers use DeployState.Targets (hostname+role) instead of Plan. Or: read ServiceMetas directly. |

---

## Decision Points for User

1. **3 steps vs 2 steps?** Keep verify separate (recommended) or merge into deploy?
2. **checkDeployPrepare scope?** Just zerops.yml existence, or full YAML parsing + env var ref validation?
3. **Iteration escalation?** Reuse bootstrap's BuildIterationDelta or deploy-specific tiers?
4. **Phase this or do all at once?** Foundation + checkers first, guidance later?
