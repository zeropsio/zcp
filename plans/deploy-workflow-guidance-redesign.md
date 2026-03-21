# Deploy Workflow — Guidance Redesign & Production Readiness

**Date**: 2026-03-21
**Status**: Ready for implementation
**Scope**: Deploy workflow guidance model, platform validation, init instructions, transitions
**Philosophy spec**: `docs/spec-guidance-philosophy.md`
**Workflow spec**: `docs/spec-bootstrap-deploy.md`
**Prior versions**: `deploy-workflow-production-readiness.md` (v1) → `v2.md` → `v3.md` — consolidated here
**Deep reviews**: `review-1.md` (R1: 15/15 claims verified), `review-2.md` (R2: 15/17 confirmed, 0 refuted, all concerns resolved)

---

## 1. Context & Evolution

### 1.1 How We Got Here

**v1** identified 6 gaps: zero checkers, dead code, no iteration escalation, GuidanceParams underutilized, DeployTarget.Status always pending, no dev→stage gate. Proposed 3 checkers (prepare, execute, verify) and 4 phases.

**R1 deep review** verified all 15 claims correct. Added 3 critical findings: (1) StepChecker type is BootstrapState-specific — deploy needs its own, (2) DeployComplete lacks context.Context, (3) buildGuide bypasses assembleGuidance entirely. Led to design decisions: separate DeployStepChecker type, 2 checkers not 3 (verify is informational).

**v2** integrated R1 findings. Added "help, don't gatekeep" philosophy from user feedback: checkers validate PLATFORM integration (always objectively correct/incorrect), never APPLICATION correctness (we don't know what user wants).

**R2 deep review** (4-agent team) found: (1) step naming mismatch — resolveStaticGuidance handles bootstrap steps only, (2) DiscoveredEnvVars not available in deploy, (3) ErrBootstrapNotActive used for deploy errors, (4) strategy gate exists and works (adversarial claim refuted). Led to: keep resolveDeployStepGuidance for static content, re-discover env vars via API.

**Philosophy discussion** fundamentally changed the guidance model. The core insight: deploy workflow PUSHES ~200+ lines of knowledge (runtime briefings, schema, env vars) that the agent may not need. Four scenarios expose the problem:

- **A** ("create an image upload app"): Agent writes code → deploys. Doesn't need Node.js briefing — just deploy steps and platform rules.
- **B** ("create Laravel + DB"): Agent bootstraps → sets strategy. Returns later to deploy — doesn't need to re-learn zerops.yml schema.
- **C** (existing project, new session): Agent may just edit code, debug, scale — 200 lines of runtime briefing wastes tokens.
- **D** (unknown): Agent does something unexpected. Push model breaks because we pushed wrong information.

**Solution**: Switch from push to pull. Inject only what's ALWAYS relevant (platform mechanics, mode workflow steps — personalized to their setup). Point to everything else (runtime knowledge, recipes, schema — agent pulls when needed). Total guidance: 15-55 lines per step instead of 200+.

### 1.2 Key Philosophical Shifts

| Before | After | Why |
|--------|-------|-----|
| Generic guidance from deploy.md sections | Personalized guidance from DeployState + ServiceMeta | Agent sees THEIR hostnames, modes, exact steps |
| Runtime briefing injected at prepare step | Knowledge pointer: "zerops_knowledge query='nodejs'" | Agent may not need it. Available on demand. |
| zerops.yml schema injected | Knowledge pointer: "zerops_knowledge query='zerops.yml'" | Verbose reference. Agent pulls when modifying config. |
| Env var list injected at deploy step | "zerops_discover includeEnvs=true" pointer | Dynamic data. Agent checks when needed. |
| Route recommends workflows | Route returns facts only | "Dumb data, smart agent." LLM decides. |
| No environment concept in init instructions | Container vs local explained upfront | Agent needs to know WHERE code is, HOW mounts work. |
| Strategy not mentioned after setting | Brief 2-line mention of alternatives | Agent knows it can change without being pushed. |

---

## 2. Current Code State (verified, compiles, all tests pass)

### 2.1 Deploy Workflow Data Model

```go
// workflow/deploy.go

type DeployState struct {
    Active      bool           `json:"active"`
    CurrentStep int            `json:"currentStep"`
    Steps       []DeployStep   `json:"steps"`      // 3 steps: prepare, deploy, verify
    Targets     []DeployTarget `json:"targets"`
    Mode        string         `json:"mode"`        // standard, dev, simple
    Strategy    string         `json:"strategy"`    // push-dev, ci-cd, manual
}

type DeployTarget struct {
    Hostname        string `json:"hostname"`
    Role            string `json:"role"`           // dev, stage, simple
    Status          string `json:"status"`          // ALWAYS "pending" — never changes (bug)
    Error           string `json:"error,omitempty"` // DEAD — only set by dead UpdateTarget
    LastAttestation string `json:"lastAttestation"` // DEAD — only set by dead UpdateTarget
    Strategy        string `json:"strategy"`
}

// 3 deploy steps
var deployStepDetails = []struct{ Name string; Tools []string }{
    {"prepare", {"zerops_discover", "zerops_knowledge"}},
    {"deploy",  {"zerops_deploy", "zerops_subdomain", "zerops_logs", "zerops_verify", "zerops_manage"}},
    {"verify",  {"zerops_verify", "zerops_discover"}},
}

// Status constants — 4 DEAD, 1 LIVE
const (
    deployTargetPending  = "pending"   // LIVE — used in BuildDeployTargets:154,164, ResetForIteration:273
    deployTargetDeployed = "deployed"  // DEAD — only in dead UpdateTarget
    deployTargetVerified = "verified"  // DEAD — 0 callers anywhere
    deployTargetFailed   = "failed"    // DEAD — only in dead UpdateTarget + DevFailed
    deployTargetSkipped  = "skipped"   // DEAD — 0 callers anywhere
)
```

### 2.2 Guidance Assembly (the system being redesigned)

**Bootstrap** uses unified path:
```go
// bootstrap_guide_assembly.go → calls assembleGuidance(GuidanceParams{all 10 fields})
// guidance.go:27 assembleGuidance() → resolveStaticGuidance() + assembleKnowledge()
// guidance.go:29 if iteration > 0 → BuildIterationDelta() replaces normal guidance
```

**Deploy** uses separate path:
```go
// deploy.go:341 buildGuide(step string, _ int, _ Environment, kp knowledge.Provider)
//   → resolveDeployStepGuidance(step, mode, strategy)  // static from deploy.md
//   → assembleKnowledge(GuidanceParams{Step, Mode, Strategy, KP})  // 4 of 10 fields
//   Ignores: iteration (always 0 behavior), Environment, RuntimeType, DependencyTypes,
//            DiscoveredEnvVars, Plan, LastAttestation, FailureCount
```

**GuidanceParams** (guidance.go:10-22) — all 10 fields:

| Field | Bootstrap | Deploy (current) | Deploy (planned) |
|-------|-----------|-------------------|-------------------|
| Step | ✅ | ✅ | ✅ (in personalized builder) |
| Mode | ✅ | ✅ | ✅ |
| Strategy | ✅ | ✅ | ✅ |
| RuntimeType | ✅ | ❌ (empty) | ✅ (for knowledge pointers) |
| DependencyTypes | ✅ | ❌ (nil) | ❌ (not needed — pointers instead) |
| DiscoveredEnvVars | ✅ | ❌ (nil) | ❌ (re-discover via API in checker) |
| Iteration | ✅ | ❌ (ignored `_`) | ✅ (for escalation) |
| Plan | ✅ | ❌ (nil) | ❌ (deploy has no plan) |
| LastAttestation | ✅ | ❌ | ❌ |
| FailureCount | ✅ | ❌ | ❌ |
| KP | ✅ | ✅ | ✅ (for knowledge pointers) |

**Step name compatibility** (critical for Phase 2):
- `resolveStaticGuidance()` (guidance.go:56) handles: `StepGenerate="generate"`, `StepDeploy="deploy"`, `StepClose="close"` — ALL bootstrap steps
- `needsRuntimeKnowledge()` (guidance.go:67) handles: `StepGenerate || DeployStepPrepare` — already deploy-aware
- `StepDeploy == DeployStepDeploy == "deploy"` — shared constant works for iteration escalation
- `DeployStepPrepare="prepare"`, `DeployStepVerify="verify"` — NOT handled by resolveStaticGuidance → must keep resolveDeployStepGuidance for deploy's static content

### 2.3 Preflight Gates (handleDeployStart)

`handleDeployStart()` at tools/workflow.go:186-258 — 5 sequential gates, all working correctly:

| # | Gate | Line | Behavior |
|---|------|------|----------|
| 1 | Metas exist? | 188-194 | Error: "Run bootstrap first" |
| 2 | Metas complete? | 204-211 | `IsComplete()` checks `BootstrappedAt != ""`. Error: "bootstrap didn't complete" |
| 3 | Runtime services? | 213-225 | Filters `Mode != "" \|\| StageHostname != ""`. Error: "nothing to deploy" |
| 4 | Strategy set? | 227-236 | **Soft gate**: returns conversational strategy selection guidance (not error) |
| 5 | Strategy consistent? | 241-248 | Error: "mixed strategies not supported" |

### 2.4 Iteration Mechanism

```go
// session.go:19-26
func maxIterations() int {
    if v := os.Getenv("ZCP_MAX_ITERATIONS"); v != "" { ... }
    return defaultMaxIterations  // 10
}

// session.go:99-118 — IterateSession
// 1. state.Iteration++ (0→1→2→...)
// 2. state.Deploy.ResetForIteration() — resets deploy+verify to pending, preserves prepare
// 3. Saves state

// engine.go:85 — checks max before iterating
if state.Iteration >= maxIterations() { return error "max iterations reached" }

// deploy.go:262-281 — ResetForIteration
// Resets steps 1-2 (deploy, verify) to pending. Step 0 (prepare) preserved.
// Resets all target statuses to deployTargetPending.
// Sets CurrentStep=1 (deploy), marks in_progress.
```

### 2.5 deploy.md Section Structure

10 sections extracted via `<section name="...">` tags:

| Section | Lines | Content | Used by |
|---------|-------|---------|---------|
| `deploy-prepare` | 14-51 | Discover targets, check zerops.yml, prerequisites | resolveDeployStepGuidance |
| `deploy-execute-overview` | 53-65 | zerops_deploy blocks, git handled, path distinction | resolveDeployStepGuidance |
| `deploy-execute-standard` | 67-89 | Standard mode 10-step flow (dev→stage) | resolveDeployStepGuidance |
| `deploy-execute-dev` | 91-101 | Dev-only 5-step flow | resolveDeployStepGuidance |
| `deploy-execute-simple` | 103-112 | Simple mode 4-step flow | resolveDeployStepGuidance |
| `deploy-iteration` | 114-134 | Dev iteration cycle (edit→restart→test) | resolveDeployStepGuidance |
| `deploy-verify` | 136-172 | Diagnosis table, fix patterns, common symptoms | resolveDeployStepGuidance |
| `deploy-push-dev` | 174-181 | Push-dev strategy (3 lines) | resolveDeployStepGuidance |
| `deploy-ci-cd` | 183-192 | CI/CD strategy (4 lines) | resolveDeployStepGuidance |
| `deploy-manual` | 194-200 | Manual strategy (3 lines) | resolveDeployStepGuidance |

**After redesign**: deploy.md sections remain for bootstrap's deploy step and zerops_knowledge queries. Deploy WORKFLOW generates personalized guidance programmatically instead of extracting these sections.

### 2.6 Checker Type Comparison (bootstrap vs deploy)

```go
// Bootstrap (bootstrap_checks.go:24):
type StepChecker func(ctx context.Context, plan *ServicePlan, state *BootstrapState) (*StepCheckResult, error)
// Requires: ServicePlan (from discover), BootstrapState (session data)
// Deploy has NEITHER — needs its own type.

// Proposed DeployStepChecker:
type DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)
// Takes: DeployState only. Reuses StepCheckResult (generic, no bootstrap fields).

// StepCheckResult is generic (bootstrap_checks.go:6-17):
type StepCheckResult struct {
    Passed  bool        `json:"passed"`
    Checks  []StepCheck `json:"checks"`
    Summary string      `json:"summary"`
}
type StepCheck struct {
    Name   string `json:"name"`
    Status string `json:"status"` // pass, fail, skip
    Detail string `json:"detail,omitempty"`
}
```

**Bootstrap checker comparison** (what exists vs what deploy needs):

| Step | Bootstrap checker | Deploy equivalent (planned) |
|------|------------------|----------------------------|
| Prepare/Generate | `checkGenerate`: zerops.yml parse, hostname match, env var refs, ports | `checkDeployPrepare`: same checks, but env vars re-discovered via API (no BootstrapState) |
| Deploy | `checkDeploy`: VerifyAll + subdomain + health | `checkDeployResult`: API status + diagnostics (informational, not blocking) |
| Close/Verify | nil (administrative) | nil (informational step) |

### 2.7 Dead Code (verified, 0 production callers)

| Code | Location | Evidence |
|------|----------|----------|
| `UpdateTarget()` | workflow/deploy.go:248-260 | grep: 0 callers outside dead tests |
| `DevFailed()` | workflow/deploy.go:284-291 | grep: 0 callers outside dead tests |
| `DeployTarget.Error` | workflow/deploy.go:59 | Only written by dead UpdateTarget |
| `DeployTarget.LastAttestation` | workflow/deploy.go:60 | Only written by dead UpdateTarget |
| 4 status constants | workflow/deploy.go:30-33 | Used only by dead methods |
| `ResolveDeployGuidance()` | workflow/deploy_guidance.go:20-42 | grep: 0 production callers |
| `ResetForIteration() Error clear` | workflow/deploy.go:274 | Error never set in production |
| Dead tests | deploy_test.go, deploy_guidance_test.go | TestDeployState_UpdateTarget, TestDeployState_DevFailed, 4 ResolveDeployGuidance tests |

**LIVE — must preserve**: `deployTargetPending` (workflow/deploy.go:29) — used at lines 154, 164, 273.

### 2.8 Pre-existing Bugs

**ErrBootstrapNotActive in deploy handlers**: `handleDeployComplete` (tools/workflow_deploy.go:29), `handleDeploySkip` (:51), `handleDeployStatus` (:62) all return `platform.ErrBootstrapNotActive` for deploy-specific errors. Semantically wrong.

**DeployTarget.Status always "pending"**: BuildDeployTargets sets all targets to `deployTargetPending`. No code path ever updates it. Agent always sees `status: "pending"` in response, suggesting nothing happened.

---

## 3. Design Decisions

### 3.1 All Decisions

| # | Decision | Rationale | Source |
|---|----------|-----------|--------|
| 1 | Separate `DeployStepChecker` type | Deploy has no ServicePlan/BootstrapState. Only 2 checker types — no premature abstraction. | R1 finding: StepChecker typed to BootstrapState |
| 2 | `context.Context` on both `DeployComplete()` AND `DeployStart()` | Checkers need ctx for API calls. engine.go:358 has no ctx; BootstrapComplete at :137 does. | R1 finding + R2 architecture |
| 3 | Checkers in `internal/tools/workflow_checks_deploy.go` | Follows existing `workflow_checks.go`, `workflow_checks_generate.go` pattern in tools/. | R1 self-verified |
| 4 | Checkers validate PLATFORM, not APPLICATION | "Help, don't gatekeep." We don't know what user wants from their app. | User philosophy feedback |
| 5 | 2 checkers: `checkDeployPrepare` + `checkDeployResult` | v1 had 3 (prepare, execute, verify). Reduced to 2: verify is informational, not blocking. Philosophy: don't gate on app health. | User: "we don't know if user wants health checks" |
| 6 | Dev→stage informational, not hard gate | User may intentionally deploy broken code to dev for debugging. | User: "don't assume app correctness" |
| 7 | Re-discover env vars via API at prepare step | DiscoveredEnvVars only on BootstrapState. Deploy is standalone. `client.GetServiceEnv()` is cheap, handles post-bootstrap changes. | R2: all 4 agents confirmed gap |
| 8 | Keep `resolveDeployStepGuidance()` for static content | `resolveStaticGuidance()` (guidance.go:56) handles only bootstrap step names. Deploy uses different names (prepare, verify). | R2 adversarial C1 |
| 9 | Deploy guidance personalized from state, not generic | Assembled from DeployState + ServiceMeta. Agent sees THEIR hostnames, modes, exact steps. Not template extraction. | Philosophy discussion |
| 10 | Runtime knowledge NEVER injected in deploy | Bootstrap = creative workflow → inject OK. Deploy = operational → point only. Agent pulls on demand via zerops_knowledge. | Philosophy: "inject rules, point to knowledge" |
| 11 | Total guidance ≤ 55 lines per step | Compact. 15-55 lines vs current 200+. Agent reads what matters, pulls what it needs. | Philosophy discussion |
| 12 | Route returns facts, never recommendations | "Dumb data, smart agent." LLM decides what to do. | User: "zcp should be dumb" |
| 13 | Init instructions explain environment (container vs local) | Agent needs to know WHERE code is, HOW mounts work, WHEN to deploy vs restart. | User: "vysvětlit ten koncept" |
| 14 | Strategy alternatives mentioned in 2 lines | Agent knows options exist without being pushed toward any. | User: "drobná zmínka jaké jsou varianty" |
| 15 | zerops_discover = state refresh mechanism | Agent calls whenever it needs current state. Init instructions say this. | User: "to je pokryté v discovery" |
| 16 | Fix ErrBootstrapNotActive → ErrDeployNotActive | Semantic correctness. 3 occurrences. | R2 adversarial M2 |
| 17 | Populate DeployTarget.Status from API/checker results | Replaces always-"pending" with actual state. Agent sees what happened. | v1 §3.5, all versions identified |

### 3.2 Rejected Alternatives

| # | Alternative | Why Rejected | Source |
|---|------------|--------------|--------|
| 1 | Generalize StepChecker for both workflows | Only 2 types. Premature abstraction. | R1 |
| 2 | Health check validation in deploy checker | Application-dependent. We don't know if user wants health checks. | User philosophy |
| 3 | Hard dev→stage gate | User may intentionally deploy broken code. Gatekeeping. | User philosophy |
| 4 | 3 checkers (prepare/execute/verify) | Verify is informational. Execute diagnostics don't block. Simpler with 2. | v1→v2 philosophy shift |
| 5 | Full assembleGuidance() unification | Step name incompatibility (prepare/verify not in resolveStaticGuidance). | R2 adversarial C1 |
| 6 | Skip env var validation in deploy | Env vars can change after bootstrap. Deploy should be standalone. | R2 security F1 |
| 7 | Inject runtime briefings in deploy | Push model. Agent may not need it. | Philosophy discussion |
| 8 | Route recommends workflows | "Dumb data, smart agent." | User |
| 9 | Subdomain verification as blocker | Service may not have HTTP endpoint. App-dependent. | User philosophy |

---

## 4. Guidance Assembly — Detailed Design

### 4.1 Data Sources

```go
// Available for personalization:
DeployState.Targets    → hostnames, roles, strategies
DeployState.Mode       → standard, dev, simple
DeployState.Strategy   → push-dev, ci-cd, manual
state.Iteration        → 0, 1, 2, ... (from WorkflowState)
Engine.Environment()   → container, local
ServiceMeta (via hostname) → RuntimeType (nodejs@22, go@1, ...)

// Available at checker time (API):
client.ListServices()  → status: ACTIVE, RUNNING, STOPPED, READY_TO_DEPLOY
client.GetServiceEnv() → env var names for re-discovery
zerops_verify          → healthy, degraded, unhealthy + checks array
zerops_events          → hint field (human-readable status)
zerops_deploy response → buildLogs array (on BUILD_FAILED)
```

### 4.2 Prepare Step Guidance

```markdown
## Deploy Preparation

### Your services
{hostname} ({runtimeType}, {role}) [→ {stageHostname} (stage)]
Mode: {mode} | Strategy: {strategy}

### Checklist
1. zerops.yml must exist with `setup:` entries for: {target hostnames}
2. Env var references (`${hostname_varName}`) must match real variables
3. {if standard}: Dev entry: `start: zsc noop --silent`, NO healthCheck
4. {if simple}: Entry must HAVE `start:` (real command) and `healthCheck`

### Platform rules
- Deploy = new container — local files lost, only `deployFiles` content survives
- `${hostname_varName}` typo = silent literal string, no error from platform
- Build container ≠ run container (different environment, packages)
- {if container}: Code on SSHFS mount is already on the container — deploy rebuilds, not transfers

### Strategy
Currently: {strategy} ({one-line description})
Other options: {alternatives with one-line descriptions}
Change: zerops_workflow action="strategy" strategies={example}

### Knowledge on demand
- {hostname} ({runtimeType}): zerops_knowledge query="{base runtime}"
{if recipe hints}: - Recipe: zerops_knowledge recipe="{name}"
- zerops.yml schema: zerops_knowledge query="zerops.yml schema"
- Env var discovery: zerops_discover includeEnvs=true

{IF service status READY_TO_DEPLOY}:
### First deploy
First deploy for {hostnames}. zerops.yml needs creation.
Load runtime knowledge first: zerops_knowledge query="{runtime}"
```

### 4.3 Deploy Step Guidance

```markdown
## Deploy — {mode} mode, {strategy}

### Workflow
{STANDARD}:
1. Deploy to dev: zerops_deploy targetService="{devHostname}"
2. Start server on dev manually (dev uses zsc noop)
   {if php-nginx/php-apache/nginx/static}: Skip — auto-starts
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify dev: zerops_verify serviceHostname="{devHostname}"
5. Deploy to stage: zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"
   Stage auto-starts (real start command + healthCheck)
6. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{stageHostname}"
7. Verify stage: zerops_verify serviceHostname="{stageHostname}"

{DEV}:
1. Deploy: zerops_deploy targetService="{devHostname}"
2. Start server via SSH (zsc noop) {or skip if implicit-webserver}
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify: zerops_verify serviceHostname="{devHostname}"

{SIMPLE}:
1. Deploy: zerops_deploy targetService="{hostname}" — auto-starts
2. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{hostname}"
3. Verify: zerops_verify serviceHostname="{hostname}"

### Key facts
- zerops_deploy blocks until complete — returns DEPLOYED or BUILD_FAILED with buildLogs
- After deploy: only deployFiles content exists. All other local files lost.
- {if dev}: Start server manually after deploy. Env vars are OS env vars.
- {if stage}: Auto-starts with healthCheck. Zerops monitors and restarts.
- Subdomain must be enabled after every deploy (idempotent)

{IF iteration > 0}:
### Iteration {n} — Diagnostic escalation
{1}: Check zerops_logs severity="error". Build failed? → buildLogs in deploy response.
     Container crash? → check start command, ports, env vars.
{2}: Systematic: zerops.yml (ports, start, deployFiles), env var refs (typos = literal!),
     runtime version compatibility.
{3+}: Present diagnostic summary to user: exact error, current config, env var values.
      User decides next step. Max {maxIterations} iterations.

### If something breaks
- Build failed → zerops_logs, buildCommands, dependencies, runtime version
- Container didn't start → start command, ports, env vars. Deploy = new container.
- Running but unreachable → zerops_subdomain, ports in zerops.yml vs app listen port
- zerops_verify unhealthy → check `detail` field for specific failed check
```

### 4.4 Verify Step Guidance

Reuse existing `deploy-verify` section from deploy.md (lines 136-172). Already well-structured: diagnosis table from checks array, fix patterns table, redeploy commands, common symptom→fix mapping. Only change: don't inject runtime knowledge alongside it.

---

## 5. Implementation Plan

### Phase 1: Dead Code Cleanup + Bug Fixes

**Scope**: Remove dead code, fix error codes. No behavioral changes.

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go | Delete: UpdateTarget, DevFailed, Error field, LastAttestation field, 4 dead status constants, ResetForIteration Error clear | -60 |
| workflow/deploy_test.go | Delete: TestDeployState_UpdateTarget, TestDeployState_DevFailed | -25 |
| workflow/deploy_guidance.go | Delete: ResolveDeployGuidance (lines 20-42) | -23 |
| workflow/deploy_guidance_test.go | Delete: 4 dead tests for ResolveDeployGuidance | -40 |
| tools/workflow_deploy.go | Replace ErrBootstrapNotActive → ErrDeployNotActive (lines 29, 51, 62) | ~0 |
| platform/errors.go | Add `ErrDeployNotActive` constant | +1 |

**DeployTarget.Status**: KEEP in Phase 1. `BuildResponse` (deploy.go:309) reads `t.Status` — removing the field breaks compilation. Phase 3 will populate `DeployTargetOut.Status` from checker results; the dead `Status` field on `DeployTarget` can be removed then.

**TDD**: Pure refactor — skip RED, verify GREEN. `go test ./... -count=1 -short` must pass with 6 fewer tests.

### Phase 2: Guidance Model Redesign

**Scope**: Replace deploy.md section extraction with personalized guidance generation.

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go `buildGuide()` | Rewrite: named params (`iteration int, env Environment`), call new personalized builders | ~50 (rewrite) |
| workflow/deploy_guidance.go | Rewrite: `buildPrepareGuide()`, `buildDeployGuide()`, `buildVerifyGuide()` — generate from state. Keep `StrategyToSection` for reference. | ~130 (rewrite) |
| workflow/deploy_guidance_test.go | Rewrite: table-driven tests for mode×strategy combinations | ~100 (rewrite) |
| workflow/guidance.go | Add: `buildKnowledgeMap()` — pointers instead of injection | +30 |

**buildGuide() new design**:
```go
func (d *DeployState) buildGuide(step string, iteration int, env Environment, kp knowledge.Provider) string {
    // Iteration escalation replaces normal guidance (reuse existing mechanism)
    if iteration > 0 && step == DeployStepDeploy {
        if delta := BuildIterationDelta(step, iteration, nil, ""); delta != "" {
            return delta
        }
    }

    switch step {
    case DeployStepPrepare:
        return buildPrepareGuide(d, env, kp)
    case DeployStepDeploy:
        return buildDeployGuide(d, iteration, env)
    case DeployStepVerify:
        return buildVerifyGuide()
    }
    return ""
}
```

**Key**: Guidance is GENERATED from state, not EXTRACTED from deploy.md. deploy.md sections remain for bootstrap's deploy step and zerops_knowledge queries.

**CRITICAL**: Phase 2 must REMOVE the `assembleKnowledge()` call from `buildGuide()` (deploy.go:344-350). Current code injects runtime briefings at DeployStepPrepare via `needsRuntimeKnowledge()` (guidance.go:67). This violates Decision #10 — deploy must use knowledge pointers only, never inject.

**RuntimeType access**: buildGuide needs RuntimeType for knowledge pointers. ServiceMeta does NOT have a RuntimeType field (verified). Fetch from API via `client.ListServices()` at deploy start time — `handleDeployStart` already has access to the API client. Store in DeployTarget or pass as param to buildGuide.

**TDD**:
- RED: Tests for personalized output (standard+push-dev, dev+ci-cd, simple+manual minimum)
- GREEN: Implement builders
- Verify: each output ≤ 55 lines, contains actual hostnames, platform facts, knowledge pointers

### Phase 3: Platform Validation Checkers

**Scope**: Add checkers for platform integration validation.

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go or workflow/bootstrap_checks.go | Add `DeployStepChecker` type definition | +3 |
| workflow/engine.go `DeployComplete` | Add `ctx context.Context` + `checker DeployStepChecker` params | +20 |
| workflow/engine.go `DeployStart` | Add `ctx context.Context` param | +5 |
| tools/workflow_deploy.go | Wire ctx, build checker via `buildDeployStepChecker()`, pass to engine. Follow wiring pattern from `tools/workflow_checks.go:buildStepChecker` — receives client, projectID, stateDir from handler closure. | +40 |
| tools/workflow_checks_deploy.go | `buildDeployStepChecker` + `checkDeployPrepare` + `checkDeployResult` + env var re-discovery | +130 |
| tools/workflow_checks_deploy_workflow_test.go | Tests for deploy workflow checkers (NOTE: existing `workflow_checks_deploy_test.go` tests bootstrap's `checkDeploy` — do NOT mix) | +100 |

**DeployStepChecker type**:
```go
type DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)
```

**checkDeployPrepare(client, projectID, stateDir)**:
1. Find zerops.yml via `filepath.Dir(filepath.Dir(stateDir))` (same pattern as checkGenerate:29)
2. Parse YAML — syntax valid?
3. `setup:` entries match deploy target hostnames?
4. Env var refs (`${hostname_varName}`) — validate against env vars re-discovered via `client.GetServiceEnv()` per dependency service
5. Return `StepCheckResult{Passed, Checks, Summary}`

**checkDeployResult(client, projectID)**:
1. Query API: `client.ListServices()` → service status per target
2. Diagnostic logic:

| Status | Diagnostic |
|--------|-----------|
| `BUILD_FAILED` | "Check buildLogs from deploy response. Common: wrong buildCommands, missing deps, runtime version mismatch." |
| `READY_TO_DEPLOY` (still) | "Container didn't start. Check: start command, ports, env vars in zerops.yml run section. Deploy = new container, local files lost." |
| `ACTIVE/RUNNING` + zerops_verify unhealthy | "Service running but issues detected. Check zerops_logs severity=error." |
| `ACTIVE/RUNNING` + healthy | "Deployed successfully." (informational) |

3. Check `SubdomainAccess` for services with ports
4. Use Events API `hint` field for LLM-friendly status if available
5. Return `StepCheckResult` — diagnostic, not hard-blocking (except objective failures like missing service)

**DeployTarget.Status update**: After checker runs, populate `DeployTargetOut.Status` in response from checker results (pass="deployed", fail="failed", etc.) — replaces always-"pending".

**Engine pattern** (mirrors BootstrapComplete at engine.go:137-191):
```go
func (e *Engine) DeployComplete(ctx context.Context, step, attestation string, checker DeployStepChecker) (*DeployResponse, error) {
    // ... load state ...
    if checker != nil {
        result, err := checker(ctx, state.Deploy)
        if !result.Passed {
            resp := state.Deploy.BuildResponse(...)
            resp.CheckResult = result
            resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", step, result.Summary)
            return resp, nil  // step NOT advanced
        }
    }
    // ... complete step, save state ...
}
```

**TDD**:
- RED: Tests per failure scenario (build failed, crash, unhealthy, success, env var typo, nil state)
- GREEN: Implement
- Verify: `StepCheckResult` reused (not duplicated), ≤350 lines per file

### Phase 4: Init Instructions

**Scope**: Add environment concept, code access, state refresh to init instructions.

| File | Change | Lines |
|------|--------|-------|
| Init content (zcp init output) | Container mode: environment section | +25 |
| Init content (zcp init output) | Local mode: environment section | +10 |

**Container mode**:
```
## Your Environment

You're on the zcpx container inside a Zerops project.

### Code Access
Runtime services are SSHFS-mounted:
  /var/www/{hostname}/ — edit code here, changes appear instantly on the service container
Mount is read/write. No file transfer needed.

### Deploy = Rebuild
Editing files on mount does NOT trigger deploy. Deploy runs the full build pipeline
(buildCommands → deployFiles → start). Deploy when: zerops.yml changes, need clean rebuild,
or promote dev → stage. Code-only changes on dev: just restart the server via SSH.

### Staying Current
zerops_discover always returns the CURRENT state of all services.
Call it whenever you need to refresh your understanding of what exists and its status.
```

**Local mode**:
```
## Your Environment

You're running locally. Code is in the working directory.
Deploy pushes code to Zerops via zcli push. zerops.yml at repository root.
Each deploy = full rebuild + new container.
```

**Integration point**: Needs verification — read `internal/init/init.go` to find where CLAUDE.md template lives.

### Phase 5: Workflow Transitions

**Scope**: Smooth transitions between bootstrap → strategy → deploy.

| File | Change | Lines |
|------|--------|-------|
| workflow/bootstrap_guide_assembly.go | Verify `BuildTransitionMessage()` (line 58) includes strategy prompt + deploy command | ~5 |
| tools/workflow_strategy.go | Add "ready to deploy" to response | +5 |

**Bootstrap complete** → must output:
```
Services ready. Choose deploy strategy for each service:
→ zerops_workflow action="strategy" strategies={"appdev":"push-dev"}
```

**Strategy set** → must output:
```
Strategies configured. When code is ready to deploy:
→ zerops_workflow action="start" workflow="deploy"
```

### Phase 6: Route Refactor — Facts Only

**Scope**: Remove recommendation logic from Route. Return structured facts, let agent decide.

**Current state** (router.go): Route is a recommendation engine — `boostByIntent()` (lines 91-104) promotes workflows based on intent keyword matching, `intentPatterns` map (lines 81-88) hardcodes editorial decisions, `FlowOffering.Reason` contains editorial text like "Fresh project — no runtime services". This violates "dumb data, smart agent."

| File | Change | Lines |
|------|--------|-------|
| workflow/router.go | Delete: `boostByIntent()`, `intentPatterns` map. Remove `Reason` from `FlowOffering`. Route returns factual workflow list with availability conditions, not ranked recommendations. | -80, +40 |
| workflow/router_test.go | Rewrite tests for factual output format | ~rewrite |
| workflow/bootstrap_guide_assembly.go | Update `BuildTransitionMessage` — remove `routeFromBootstrapState()` editorial Reasons from transition output | ~10 |

**Target route output format** (facts only):
```json
{
  "project": {"state": "ACTIVE"},
  "services": [
    {"hostname": "appdev", "type": "nodejs@22", "status": "RUNNING",
     "mode": "standard", "strategy": "push-dev", "stageHostname": "appstage"}
  ],
  "activeSessions": [],
  "environment": "container",
  "availableWorkflows": ["bootstrap", "deploy", "cicd", "debug", "scale", "configure"]
}
```

No `suggestedAction`, no `recommendation`, no `Reason`, no intent-matching. Agent decides based on facts + user request.

---

## 6. End-to-End Flow (after implementation)

```
zerops_workflow action="start" workflow="deploy"
│
├─ Gate 1: Metas exist?           → NO: "Run bootstrap first"
├─ Gate 2: Metas complete?        → NO: "Bootstrap didn't complete"
├─ Gate 3: Runtime services?      → NO: "Nothing to deploy"
├─ Gate 4: Strategy set?          → NO: Strategy selection guidance (soft)
│                                      → action="strategy" → retry
├─ Gate 5: Strategy consistent?   → NO: "Mixed strategies not supported"
│
└─ OK → Create session (prepare → deploy → verify)
         │
         ├─ PREPARE (in_progress)
         │   Guidance: personalized setup summary + platform rules + knowledge pointers
         │   Agent: discover, check/create zerops.yml
         │   action="complete" step="prepare"
         │   → checkDeployPrepare: yml parse, hostname match, env var refs (re-discovered)
         │     FAIL → feedback with details, agent fixes, retries
         │     PASS → advance to deploy
         │
         ├─ DEPLOY (in_progress)
         │   Guidance: personalized mode workflow + strategy commands + platform facts
         │   {iteration > 0}: escalating diagnostics replace normal guidance
         │   Agent: zerops_deploy, zerops_subdomain, zerops_verify, SSH start
         │   action="complete" step="deploy"
         │   → checkDeployResult: API status + diagnostics
         │     BUILD_FAILED → diagnostic: check buildLogs
         │     CRASH → diagnostic: check start, ports, env vars
         │     RUNNING+unhealthy → diagnostic: check zerops_logs
         │     RUNNING+healthy → pass, advance
         │     FAIL → agent may action="iterate":
         │            ResetForIteration → deploy+verify reset, prepare preserved
         │            Iteration++ → escalating guidance (tier 1→2→3)
         │            Max iterations: 10 (ZCP_MAX_ITERATIONS env override)
         │
         ├─ VERIFY (in_progress)
         │   Guidance: diagnostic patterns from deploy-verify section
         │   No checker (informational step)
         │   action="complete" step="verify"
         │   → advance, Active=false
         │
         └─ DONE → session file deleted, engine cleared
```

---

## 7. Platform Reference (verified on live Zerops)

Facts needed for checker implementation, verified by KB-verifier (2026-03-21):

| Fact | Value | Source |
|------|-------|--------|
| Service status values | `ACTIVE`, `RUNNING`, `STOPPED`, `READY_TO_DEPLOY` | Live API + code (verify_checks.go:52-61) |
| Build failure detection | `BUILD_FAILED` status + `buildLogs` array in deploy response | E2E test build_logs_test.go |
| First deploy fail state | Service stays `READY_TO_DEPLOY` | Docs: zerops-complete-knowledge.md:252 |
| Re-deploy fail state | Previous version stays `ACTIVE` (zero-downtime model) | Docs: line 491 |
| Env var typo behavior | Silent literal string, no error | Verified live 2026-03-04 |
| Subdomain queryable | `SubdomainAccess` bool field on ServiceStack | Live API + types.go:31 |
| Subdomain env var | `zeropsSubdomain` env var with actual URL (platform-injected) | Live zerops_discover |
| Events hint field | Human-readable status in `hint` field (e.g., "DEPLOYED: App version is deployed") | Live zerops_events |
| Log availability | Build logs in deploy response. Runtime logs via zerops_logs (depends on backend). | Live API |
| Non-HTTP error code | `serviceStackIsNotHttp` when enabling subdomain on non-HTTP service | Live zerops_subdomain |
| Verify output | `healthy`/`degraded`/`unhealthy` with individual check results | Live zerops_verify |

---

## 8. File Impact Summary

| File | Phase | Change | Est. Lines |
|------|-------|--------|-----------|
| workflow/deploy.go | 1,2 | Delete dead code (keep Status field) + rewrite buildGuide + remove assembleKnowledge call | -60, +50 |
| workflow/deploy_test.go | 1,2 | Delete dead tests + personalization tests | -25, +50 |
| workflow/deploy_guidance.go | 1,2 | Delete ResolveDeployGuidance + personalized builders | -23, +130 |
| workflow/deploy_guidance_test.go | 1,2 | Delete dead tests + builder tests | -40, +100 |
| workflow/guidance.go | 2 | Knowledge map builder (pointers) | +30 |
| workflow/bootstrap_checks.go (or new file) | 3 | DeployStepChecker type | +3 |
| workflow/engine.go | 3 | ctx on DeployComplete + DeployStart | +25 |
| tools/workflow_deploy.go | 1,3 | Fix error codes + wire checkers | +40 |
| tools/workflow_checks_deploy.go | 3 | buildDeployStepChecker + 2 checkers + env re-discovery | +130 |
| tools/workflow_checks_deploy_workflow_test.go | 3 | Deploy workflow checker tests (separate from existing bootstrap tests) | +100 |
| platform/errors.go | 1 | ErrDeployNotActive | +1 |
| init content | 4 | Environment concept + state refresh | +35 |
| tools/workflow_strategy.go | 5 | Transition guidance | +5 |
| workflow/bootstrap_guide_assembly.go | 5 | Verify transition message (line 58) | ~5 |
| workflow/router.go | 6 | Remove boostByIntent, intentPatterns, Reason — facts only | -80, +40 |
| workflow/router_test.go | 6 | Rewrite for factual output | ~rewrite |

---

## 9. Risks & Open Questions

### Risks

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Personalized builders = more code than template extraction | MEDIUM | Simple string assembly from struct fields. Table-driven tests cover all mode×strategy combos. |
| deploy.md sections orphaned from deploy workflow | LOW | Remain for: bootstrap deploy step, zerops_knowledge queries. Mark sections with consumers. |
| checkDeployPrepare zerops.yml path resolution | MEDIUM | checkGenerate's `filepath.Dir(filepath.Dir(stateDir))` is generic, reusable (verified). |
| Env var re-discovery adds API calls | LOW | One `GetServiceEnv` per dependency. Lightweight. |
| Guidance output differs significantly from current | MEDIUM | Table-driven tests. Gradual rollout possible (feature flag). |
| Init instructions integration point unclear | LOW | Phase 4 defines content; read `internal/init/init.go` during implementation. |
| Events API hint field availability | LOW | Diagnostic builder degrades gracefully if unavailable. |
| deploy.go file size after changes | LOW | Phase 1 deletes ~60 lines, Phase 2 rewrites buildGuide. Net should stay ≤350. |

### Open Questions

| # | Question | Phase | Resolution |
|---|----------|-------|------------|
| 1 | Where does `zcp init` put environment concept? | 4 | Read `internal/init/init.go` and current template |
| 2 | RuntimeType storage: DeployTarget field or ServiceMeta read? | 2 | RESOLVED: Fetch from API via `client.ListServices()` at deploy start. ServiceMeta has no RuntimeType field. |
| 3 | Events API hint field Go type? | 3 | RESOLVED: `TimelineEvent.Hint` at ops/events.go:40. ZCP-generated via processHintMap + appVersionHintMap. |
| 4 | Deploy.md section simplification after guidance redesign? | Future | Keep for bootstrap + knowledge. Mark consumers. |

---

## 10. Verification Checklist

### Phase 1 (Dead Code + Bugs)
- [ ] `UpdateTarget()`, `DevFailed()` deleted
- [ ] `Error`, `LastAttestation` fields deleted from DeployTarget
- [ ] 4 dead status constants deleted; `deployTargetPending` PRESERVED
- [ ] `ResolveDeployGuidance()` deleted
- [ ] Dead tests deleted (2 in deploy_test, 4 in deploy_guidance_test)
- [ ] `ErrDeployNotActive` added, 3 occurrences in workflow_deploy.go updated
- [ ] `DeployTarget.Status` field KEPT (BuildResponse:309 reads it — remove in Phase 3)
- [ ] `go test ./... -count=1 -short` passes, `make lint-fast` passes

### Phase 2 (Guidance Redesign)
- [ ] `buildGuide()` uses named params, not `_ int, _ Environment`
- [ ] `assembleKnowledge()` call REMOVED from deploy buildGuide (deploy.go:344-350)
- [ ] RuntimeType fetched from API via `client.ListServices()`, NOT from ServiceMeta
- [ ] Personalized guidance generated (actual hostnames, mode steps, strategy commands)
- [ ] No runtime briefing injected — knowledge pointers only
- [ ] Platform facts always included
- [ ] Strategy alternatives in 2 lines
- [ ] First deploy detection (READY_TO_DEPLOY) as contextual note
- [ ] Iteration escalation (1/2/3+) as contextual guidance
- [ ] Each step guidance ≤ 55 lines
- [ ] Tests: standard×push-dev, dev×ci-cd, simple×manual minimum
- [ ] All files ≤ 350 lines

### Phase 3 (Checkers)
- [ ] `DeployStepChecker` type defined (separate from StepChecker)
- [ ] `context.Context` on DeployComplete AND DeployStart
- [ ] `handleDeployComplete` passes ctx + checker to engine
- [ ] `checkDeployPrepare`: yml parse + hostname match + env var ref validation via API
- [ ] `checkDeployResult`: API status + diagnostic feedback
- [ ] `DeployTargetOut.Status` populated from checker results
- [ ] `DeployTarget.Status` field removed (was kept in Phase 1)
- [ ] `DeployTargetOut.Status` populated from checker results
- [ ] Tests in NEW file `workflow_checks_deploy_workflow_test.go` (existing file tests bootstrap)
- [ ] Tests: nil state, build failed, crash, unhealthy, success, env var typo
- [ ] `StepCheckResult` reused (not duplicated)

### Phase 4 (Init Instructions)
- [ ] Container vs local environment explained
- [ ] Code access (mounts vs local), deploy = rebuild
- [ ] State refresh via zerops_discover
- [ ] No workflow recommendations

### Phase 5 (Transitions)
- [ ] Bootstrap complete → strategy selection prompt + command
- [ ] Strategy set → "ready to deploy" + command

### Phase 6 (Route Refactor)
- [ ] `boostByIntent()` deleted from router.go
- [ ] `intentPatterns` map deleted from router.go
- [ ] `Reason` field removed from `FlowOffering`
- [ ] Route returns structured facts: services, sessions, environment, available workflows
- [ ] `BuildTransitionMessage` updated — no editorial Reasons in transition output
- [ ] Router tests rewritten for factual output format

---

## 11. Decision Record — Evidence Traceability

| Decision | Evidence | Source |
|----------|----------|--------|
| Dead code list accurate | grep: 0 production callers for all items | R1 + R2 (6 agents total) |
| deployTargetPending is LIVE | deploy.go:154, 164, 273 | R1 + R2 |
| Strategy gate exists and works | tools/workflow.go:227-248 verified | R2 adversarial refuted |
| StepCheckResult is generic, reusable | bootstrap_checks.go:6-17, no bootstrap fields | R1 KB-research |
| checkGenerate path resolution reusable | workflow_checks_generate.go:29, filepath.Dir generic | R1 KB-research |
| DiscoveredEnvVars not on DeployState | Only on BootstrapState, grep confirmed | R2 all 4 agents |
| Build failure returns buildLogs | Live platform, E2E build_logs_test.go | R2 KB-verifier |
| Service statuses: ACTIVE/RUNNING/STOPPED/READY_TO_DEPLOY | Live API + code | R2 KB-verifier |
| Env var typos = silent literal strings | Verified live Zerops 2026-03-04 | Memory |
| Events API has hint field | Live API inspection | R2 KB-verifier |
| ErrBootstrapNotActive in deploy handlers | tools/workflow_deploy.go:29,51,62 | R2 adversarial |
| resolveStaticGuidance bootstrap-only | guidance.go:56 | R2 adversarial C1 |
| needsRuntimeKnowledge handles DeployStepPrepare | guidance.go:67 | R1 KB-research |
| v1→v2: 3 checkers → 2 | Verify step informational, philosophy shift | User feedback |
| Push model wastes tokens | Scenarios A-D analysis | Philosophy discussion |
| Route = facts only | "ZCP should be dumb" | User directive |
| Init instructions need environment concept | "Vysvětlit koncept jak to funguje" | User directive |
| Guidance personalized to setup | "Mělo by být na míru dle toho co víme za setup" | User directive |
| zerops_discover = state refresh | "To je pokryté v discovery" | User confirmation |
