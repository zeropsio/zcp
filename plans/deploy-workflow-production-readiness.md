# Deploy Workflow — Production Readiness Plan

**Date**: 2026-03-21
**Status**: Analysis complete, ready for implementation
**Scope**: Standalone deploy workflow (`action="start" workflow="deploy"`)
**Subsumes**: analysis-deploy-target-tracking (dead code), analysis-deploy-workflow-redesign (checkers)

---

## 1. Motivation

The standalone deploy workflow is the **post-bootstrap lifecycle workflow** — the primary way an LLM agent deploys and redeploys services for users. It triggers when users say "deploy my app", "push update to stage", "fix the broken deploy".

Bootstrap was refined to production quality: 5 steps, 3 real checkers with per-target feedback, escalating iteration guidance, mode-specific knowledge injection. The deploy workflow was left as a skeleton: 3 steps, 0 checkers, agent self-attests everything with no validation.

**The core problem**: ServiceMeta stores two axes of information from bootstrap — **mode** (standard/dev/simple = topology) and **strategy** (push-dev/ci-cd/manual = how to deploy). The deploy workflow reads both at start, uses mode for guidance section selection, but **loses strategy entirely**. Strategy-specific guidance content exists in deploy.md but is never delivered to the agent during the deploy workflow.

---

## 2. Current Architecture

### ServiceMeta (the evidence from bootstrap)

```
stateDir/services/{hostname}.json:
{
  "hostname": "appdev",
  "mode": "standard",           // topology: standard (dev+stage) | dev (dev-only) | simple
  "stageHostname": "appstage",  // paired stage service (standard mode only)
  "deployStrategy": "push-dev", // how to deploy: push-dev | ci-cd | manual
  "bootstrapSession": "...",
  "bootstrappedAt": "..."
}
```

Two axes:
- **Mode** determines service topology and deploy ordering (dev before stage, dev-only, or single service)
- **Strategy** determines the deployment mechanism (SSH self-deploy, git webhook, or user-managed)

### Deploy Workflow Flow (current)

```
handleDeployStart()                           # workflow.go:187-247
  1. Read ServiceMetas                         # ✓ mode + strategy available
  2. Strategy gate: reject if any missing      # ✓ strategy READ here
  3. BuildDeployTargets(metas)                 # ✓ mode → targets with roles
     Returns: targets, mode                    # ✗ strategy NOT returned
  4. engine.DeployStart(targets, mode)         # ✗ strategy NOT passed
     Creates DeployState{Mode, Targets}        # ✗ strategy NOT stored
  5. BuildResponse → buildGuide                # ✗ strategy NOT in guidance
     resolveDeployStepGuidance(step, mode)     # mode-only section selection
```

**Strategy is checked at step 2, then discarded.** From step 3 onward, only mode flows through.

### What Agent Gets (deploy step)

```
Mode-specific sections assembled:
  deploy-execute-overview      (always)
  deploy-execute-standard      (if mode=standard)
  deploy-execute-dev           (if mode=dev)
  deploy-execute-simple        (if mode=simple)
  deploy-iteration             (if mode!=simple)

Strategy-specific sections NEVER delivered:
  deploy-push-dev              ← exists in deploy.md, unreachable
  deploy-ci-cd                 ← exists in deploy.md, unreachable
  deploy-manual                ← exists in deploy.md, unreachable
```

### What Agent Does NOT Get

1. **No strategy-specific guidance** — push-dev agent doesn't know to use SSH, ci-cd agent doesn't know about webhooks
2. **No validation** — every step advances on attestation alone (min 15 chars)
3. **No per-target feedback** — targets always show `status: "pending"`
4. **No iteration escalation** — guidance identical on retry 1 vs retry 5
5. **No runtime/dependency knowledge** — GuidanceParams passes only Mode+Step+KP, missing RuntimeType, DependencyTypes, Plan, Iteration, FailureCount

---

## 3. Findings

### 3.1 Strategy Flow Is Broken

**Where strategy is written:**
- `workflow_strategy.go:55` — `handleStrategy()` sets `ServiceMeta.DeployStrategy`
- `bootstrap_outputs.go:27` — bootstrap close copies `Strategies[hostname]` → meta

**Where strategy is read:**
- `workflow.go:230` — `handleDeployStart()` checks `DeployStrategy != ""` (gate)
- `workflow.go:261` — CI/CD start checks `DeployStrategy == "ci-cd"` (filter)
- `workflow_strategy.go:87` — `buildStrategyGuidance()` maps strategy → deploy.md section

**Where strategy is LOST:**
- `BuildDeployTargets()` (deploy.go:127) — reads `meta.Mode` but ignores `meta.DeployStrategy`
- `DeployState` (deploy.go:37) — has `Mode` field but no `Strategy` field
- `DeployTarget` (deploy.go:54) — has `Role` field but no `Strategy` field
- `resolveDeployStepGuidance()` (deploy_guidance.go:46) — takes `(step, mode)` but not strategy
- `GuidanceParams` (guidance.go) — has `Mode` but no `Strategy`

**Dead code:** `ResolveDeployGuidance()` (deploy_guidance.go:20-42) reads strategy from ServiceMeta and maps to deploy.md section — but this function has **0 callers** anywhere in the codebase.

### 3.2 Deploy Workflow Has Zero Checkers

Bootstrap step checkers (workflow_checks.go):
- `checkProvision()` — service exists, RUNNING, type matches, env vars available
- `checkGenerate()` — zerops.yml exists, YAML valid, setup entries match
- `checkDeploy()` — VerifyAll (HTTP, logs, startup) + subdomain access

Deploy workflow checkers: **NONE**.

`handleDeployComplete()` (workflow_deploy.go:12-34) calls `engine.DeployComplete()` which calls `state.Deploy.CompleteStep()` — validates attestation length only. No checker runs. No API query. No health check.

### 3.3 Dead Per-Target Tracking Code

Code in deploy.go with 0 production callers:

| Code | Lines | Purpose | Callers |
|------|-------|---------|---------|
| `UpdateTarget()` | 238-251 | Set per-target status + attestation | test only |
| `DevFailed()` | 274-282 | Gate stage on dev failure | test only |
| `DeployTarget.Error` | 58 | Error message field | written by dead UpdateTarget |
| `DeployTarget.LastAttestation` | 59 | Attestation field | written by dead UpdateTarget |
| Status constants (deployed/verified/failed/skipped) | 30-33 | Target states | used by dead methods |
| `ResetForIteration()` Error clear | 265 | Clears dead field | n/a |

Also dead: `ResolveDeployGuidance()` (deploy_guidance.go:20-42) — strategy guidance resolver with 0 callers.

### 3.4 GuidanceParams Underutilized

Deploy's `buildGuide()` (deploy.go:332-344) creates GuidanceParams with only:
- Step ✓
- Mode ✓
- KP ✓

Missing (compared to bootstrap):
- Strategy (doesn't exist in GuidanceParams at all)
- RuntimeType (runtime briefings unavailable during deploy)
- DependencyTypes (dependency knowledge unavailable)
- Plan (target info unavailable)
- Iteration / FailureCount (no escalating guidance)
- DiscoveredEnvVars (injected only at deploy step, not prepare)
- LastAttestation (no iteration context)

### 3.5 Guidance Content Exists But Is Disconnected

deploy.md has 11 sections across two concerns:

**Mode sections** (delivered during deploy workflow):
- `deploy-prepare` — prerequisites check
- `deploy-execute-overview` — how zerops_deploy works
- `deploy-execute-standard` — dev+stage 10-step flow
- `deploy-execute-dev` — dev-only 5-step flow
- `deploy-execute-simple` — single service 4-step flow
- `deploy-iteration` — dev iteration cycle
- `deploy-verify` — health check interpretation

**Strategy sections** (NOT delivered during deploy workflow):
- `deploy-push-dev` — SSH self-deploy via zcli push
- `deploy-ci-cd` — git webhook automated deploys
- `deploy-manual` — user-managed, no ZCP automation

Strategy sections are delivered ONLY during post-bootstrap `action="strategy"` call. Once deploy workflow starts, they are unreachable.

### 3.6 No Iteration Escalation

Bootstrap has `BuildIterationDelta()` with 3 tiers:
- Tier 1 (iterations 1-2): "Check logs, fix obvious issues"
- Tier 2 (iterations 3-4): "Systematic diagnosis"
- Tier 3 (iterations 5+): "Stop and ask user"

Deploy has: identical guidance every iteration. No escalation. No escape hatch.

---

## 4. Goal State

After implementation, the deploy workflow should:

1. **Carry strategy through the entire flow** — from ServiceMeta through DeployState to guidance assembly
2. **Deliver strategy-specific guidance** — agent knows HOW to deploy (SSH vs webhook vs manual) at every step
3. **Validate every step** — checkers query Zerops API, return per-target pass/fail
4. **Gate dev→stage** — standard mode: dev must be healthy before stage deploys
5. **Escalate on failure** — iteration guidance intensifies, eventually asks user
6. **Inject full knowledge** — runtime type, dependency types, env vars at prepare step
7. **Remove dead code** — UpdateTarget, DevFailed, Error, LastAttestation, dead status constants, dead ResolveDeployGuidance

---

## 5. Design

### 5.1 Data Model Changes

**DeployTarget** — add Strategy, remove dead fields:
```go
type DeployTarget struct {
    Hostname string `json:"hostname"`
    Role     string `json:"role"`               // dev | stage | simple
    Status   string `json:"status"`             // pending (display only)
    Strategy string `json:"strategy,omitempty"` // NEW: push-dev | ci-cd | manual
}
```

**DeployState** — add Strategy:
```go
type DeployState struct {
    Active      bool           `json:"active"`
    CurrentStep int            `json:"currentStep"`
    Steps       []DeployStep   `json:"steps"`
    Targets     []DeployTarget `json:"targets"`
    Mode        string         `json:"mode"`
    Strategy    string         `json:"strategy,omitempty"` // NEW: primary strategy
}
```

**BuildDeployTargets** — return strategy:
```go
func BuildDeployTargets(metas []*ServiceMeta) ([]DeployTarget, string, string)
// Returns: targets, mode, strategy
// Strategy = from first runtime meta's DeployStrategy
// Each target also carries its own strategy
```

**GuidanceParams** — add Strategy:
```go
type GuidanceParams struct {
    // existing fields...
    Strategy string // NEW
}
```

### 5.2 Strategy Flow Fix

```
handleDeployStart():
  1. Read ServiceMetas
  2. Strategy gate (unchanged)
  3. BuildDeployTargets(metas) → targets, mode, strategy  // NEW: returns strategy
  4. engine.DeployStart(targets, mode, strategy)           // NEW: accepts strategy
     Creates DeployState{Mode, Strategy, Targets}          // NEW: stores strategy
  5. BuildResponse → buildGuide(step, mode, strategy)      // NEW: passes strategy
     resolveDeployStepGuidance(step, mode, strategy)       // NEW: uses strategy
       → mode sections + strategy section layered together
```

### 5.3 Guidance Assembly Fix

`resolveDeployStepGuidance(step, mode, strategy string)`:

For **deploy step** (DeployStepDeploy):
```
sections = [
  deploy-execute-overview,           // always
  deploy-execute-{mode},             // mode-specific topology
  deploy-{strategy},                 // NEW: strategy-specific HOW
  deploy-iteration,                  // if mode != simple
]
```

For **prepare step**: unchanged (mode-agnostic)
For **verify step**: unchanged (mode-agnostic)

Agent now gets BOTH axes: "you have dev+stage topology" (mode) AND "you deploy via SSH push" (strategy).

### 5.4 Checkers

Add 3 deploy step checkers, routed from `handleDeployComplete()`:

**checkDeployPrepare(stateDir)**:
- zerops.yml exists
- YAML parses correctly
- setup entries match target hostnames
- env var references resolvable (if discovered vars available)

**checkDeployExecute(client, fetcher, projectID, httpClient)**:
- `ops.VerifyAll()` — per-target health (HTTP, logs, startup)
- Subdomain access for services with ports
- Standard mode: dev healthy → allow stage (inline DevFailed logic, no persistence)
- Strategy-aware: push-dev checks SSH deploy, ci-cd checks build status

**checkDeployVerify(client, fetcher, projectID, httpClient)**:
- Lighter health confirmation
- Iteration count warning at threshold

Checkers return `StepCheckResult` with per-target `Checks[]` — agent sees exactly what passed and failed.

### 5.5 Iteration Escalation

Add `BuildDeployIterationDelta()` or extend existing `BuildIterationDelta()`:
- Tier 1: "Check zerops_logs severity=error; verify zerops.yml env vars"
- Tier 2: "Systematic: all env var refs, service type, ports, start command"
- Tier 3: "Stop — present diagnostic summary to user"

### 5.6 Knowledge Enrichment

Deploy's `buildGuide()` should pass full GuidanceParams:
- RuntimeType from first target's service type (readable from ServiceMeta or API)
- DependencyTypes from ServiceMetas
- DiscoveredEnvVars moved to prepare step (not just deploy)
- Iteration + FailureCount for escalation
- Strategy for strategy-aware knowledge injection

### 5.7 Dead Code Removal

Delete from deploy.go:
- `UpdateTarget()` (lines 238-251)
- `DevFailed()` (lines 274-282)
- `DeployTarget.Error` field (line 58)
- `DeployTarget.LastAttestation` field (line 59)
- Status constants: `deployTargetDeployed`, `deployTargetVerified`, `deployTargetFailed`, `deployTargetSkipped` (lines 30-33)
- `ResetForIteration()` Error clear (line 265)

Delete from deploy_test.go:
- `TestDeployState_UpdateTarget` (lines 97-115)
- `TestDeployState_DevFailed` (lines 235-250)

Delete from deploy_guidance.go:
- `ResolveDeployGuidance()` (lines 20-42) — dead function, 0 callers

DevFailed logic reimplemented as inline check in `checkDeployExecute()` — queries API, no persistence.

---

## 6. Implementation Phases

### Phase 1: Data model + strategy flow (foundation)

| Change | File | Est. |
|--------|------|------|
| Delete dead code (UpdateTarget, DevFailed, Error, LastAttestation, dead constants, dead tests, dead ResolveDeployGuidance) | deploy.go, deploy_test.go, deploy_guidance.go | -70 lines |
| Add Strategy to DeployTarget, DeployState | deploy.go | +5 |
| Update BuildDeployTargets to return strategy, carry per-target strategy | deploy.go | +15 |
| Update NewDeployState to accept strategy | deploy.go | +3 |
| Update engine.DeployStart to accept+store strategy | engine.go | +5 |
| Update handleDeployStart to extract+pass strategy | workflow.go | +10 |
| Add Strategy to GuidanceParams | guidance.go | +2 |
| Update tests for new signatures | deploy_test.go, engine_test.go | +30 |

### Phase 2: Guidance assembly (strategy-aware)

| Change | File | Est. |
|--------|------|------|
| Update resolveDeployStepGuidance to accept+use strategy | deploy_guidance.go | +15 |
| Update buildGuide to pass strategy to guidance | deploy.go | +5 |
| Update buildGuide to pass richer GuidanceParams (RuntimeType, Iteration, etc.) | deploy.go | +10 |
| Move env var injection to prepare step | guidance.go | +5 |
| Tests for strategy-specific guidance assembly | deploy_guidance_test.go | +40 |

### Phase 3: Checkers (validation gates)

| Change | File | Est. |
|--------|------|------|
| Add checkDeployPrepare (zerops.yml + env var refs) | workflow_checks.go or new file | +50 |
| Add checkDeployExecute (VerifyAll + subdomain + dev→stage gate) | workflow_checks.go or new file | +50 |
| Add checkDeployVerify (health confirmation) | workflow_checks.go or new file | +30 |
| Wire checkers into handleDeployComplete | workflow_deploy.go | +30 |
| Tests for all 3 checkers | workflow_checks_deploy_test.go | +100 |

### Phase 4: Iteration + polish

| Change | File | Est. |
|--------|------|------|
| Add deploy iteration escalation (BuildDeployIterationDelta) | deploy_guidance.go or guidance.go | +25 |
| Update deploy.md content for checker-aware guidance | deploy.md | +20 |
| Document deploy iteration in spec | spec-bootstrap-deploy.md | +15 |

**Total**: ~+350 new lines, -70 deleted = net +280 lines across ~10 files.

---

## 7. Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Strategy per-target AND per-session | Each target carries its own strategy (future: mixed). Session has primary strategy for guidance routing. |
| Block mixed strategies for now | Simpler implementation. User deploys per-strategy. Future: separate sessions per strategy group. |
| Checkers implicit (run on complete) | Consistency with bootstrap. Agent calls `action="complete"`, checker runs automatically. |
| API as source of truth, no target persistence | Checkers query Zerops API fresh each time. No UpdateTarget, no stored target status. |
| Keep 3 steps (prepare/deploy/verify) | Verify is a separate checkpoint from deploy. Bootstrap merged them because verify was redundant with deploy checker. Deploy verify serves as final confirmation. |
| Strategy sections layered with mode sections | Agent gets BOTH: "dev+stage topology" (mode) + "SSH self-deploy" (strategy). Not one or the other. |

---

## 8. Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|-----------|
| BuildDeployTargets signature change breaks callers | LOW | Only 2 callers: handleDeployStart, deploy_test.go |
| engine.DeployStart signature change | LOW | Only 2 callers: handleDeployStart, engine_test.go |
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | Reuse checkGenerate's path logic |
| Strategy sections in deploy.md may need expansion | LOW | Existing content is brief but sufficient for MVP |
| Mixed strategies deferred | LOW | Gate in handleDeployStart with clear error message |
