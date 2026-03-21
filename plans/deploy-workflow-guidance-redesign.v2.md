# Deploy Workflow Guidance Redesign — Complete Plan (v2)

**Date**: 2026-03-21
**Status**: Ready for implementation (Phase 1 unblocked, Phases 2+ after resolutions)
**Scope**: Deploy workflow guidance model + init instructions + transitions
**Philosophy**: `docs/spec-guidance-philosophy.md` (authoritative)
**Workflow spec**: `docs/spec-bootstrap-deploy.md` (authoritative for step mechanics)
**Review**: `plans/deploy-workflow-guidance-redesign.review-1.md` (evidence-based deep review)

---

## 1. Problem Statement

### 1.1 Current State

The deploy workflow pushes ~200+ lines of knowledge per step: runtime briefings, zerops.yml schema, env var lists, mode-specific sections, strategy sections. This is a **push model** — ZCP decides what the agent needs and injects it.

### 1.2 Why This Is Wrong

Four scenarios expose the problem:

**Scenario A** ("create an image upload app"): Agent writes code → starts deploy. Doesn't need a Node.js briefing — just needs to know the deploy steps and platform rules.

**Scenario B** ("create Laravel + DB + MQ + storage"): Agent bootstraps → sets strategy → done. When user returns later, agent just needs to deploy, not re-learn the entire zerops.yml schema.

**Scenario C** (continuing work on existing project): Agent may just edit code, or may need to debug, or add a service, or scale. We don't know — and 200 lines of runtime briefing wastes tokens if the agent is just changing CSS.

**Scenario D** (unknown): Agent does something ZCP didn't anticipate. The push model breaks because we pushed wrong information.

### 1.3 What Should Happen

**Inject MUST-KNOW** (compact, personalized, always relevant):
- Platform mechanics (container lifecycle, env var behavior)
- Mode/strategy workflow steps (specific to THEIR setup)
- Brief strategy alternatives note

**Point to MIGHT-NEED** (on-demand, agent pulls when needed):
- Runtime knowledge → `zerops_knowledge query="nodejs"`
- Recipe patterns → `zerops_knowledge recipe="nextjs"`
- Schema details → `zerops_knowledge query="zerops.yml schema"`
- Env var discovery → `zerops_discover includeEnvs=true`

Total guidance per step: **15-55 lines** instead of 200+.

---

## 2. What's Already Done (verified, compiles, tests pass)

### 2.1 Strategy Flow (complete, no changes needed)

- `DeployTarget.Strategy` populated from ServiceMeta (workflow/deploy.go:61)
- `DeployState.Strategy` set from first meta (workflow/deploy.go:43)
- `BuildDeployTargets()` returns targets, mode, strategy (workflow/deploy.go:130)
- Strategy gate in `handleDeployStart()`: rejects empty strategy (tools/workflow.go:228-236)
- Mixed strategy gate: rejects different strategies (tools/workflow.go:241-248)
- Strategy-specific guidance sections in deploy.md exist and are delivered

### 2.2 Preflight Gates (complete, no changes needed)

`handleDeployStart()` (tools/workflow.go:187-258) has 5 sequential gates:

1. **Metas exist?** (line 188-194) — "Run bootstrap first"
2. **Metas complete?** (line 204-211) — `IsComplete()` checks `BootstrappedAt != ""`
3. **Runtime services?** (line 213-225) — filters `Mode != "" || StageHostname != ""`
4. **Strategy set?** (line 228-236) — soft gate: returns strategy selection guidance
5. **Strategy consistent?** (line 241-248) — rejects mixed strategies

### 2.3 Session Lifecycle (complete, no changes needed)

- Session creation, step progression, iteration, resume all work
- `ResetForIteration()` resets deploy+verify, preserves prepare
- Auto-cleanup on completion (session file deleted)
- `IterateSession()` increments counter, calls `DeployState.ResetForIteration()`

### 2.4 Knowledge Injection (exists but needs redesign)

- `buildGuide()` (workflow/deploy.go:341-354) assembles guidance
- Calls `resolveDeployStepGuidance()` for static content from deploy.md
- Calls `assembleKnowledge()` for runtime briefings + schema + env vars
- **Problem**: injects full runtime briefing + schema = too much. Parameters ignored (`_ int, _ Environment`)
- **Problem**: 4 of 11 GuidanceParams fields used (Step, Mode, Strategy, KP). 7 fields unused by deploy.
- **Problem**: `assembleKnowledge()` currently injects runtime briefings at DeployStepPrepare (guidance.go:86-98) — this VIOLATES the "no knowledge injection in deploy" principle and must be actively removed in Phase 2.

### 2.5 Dead Code (verified, 0 production callers)

| Code | Location | Callers |
|------|----------|---------|
| `UpdateTarget()` | workflow/deploy.go:248-260 | 0 production |
| `DevFailed()` | workflow/deploy.go:284-291 | 0 production |
| `DeployTarget.Error` | workflow/deploy.go:59 | Written by dead UpdateTarget only |
| `DeployTarget.LastAttestation` | workflow/deploy.go:60 | Written by dead UpdateTarget only |
| `deployTargetDeployed/Verified/Failed/Skipped` | workflow/deploy.go:30-33 | Used by dead methods only |
| `ResolveDeployGuidance()` | workflow/deploy_guidance.go:20-42 | 0 production |
| `ResetForIteration() Error clear` | workflow/deploy.go:274 | Effectively dead (Error never set in prod) |

**Live**: `deployTargetPending` (workflow/deploy.go:29) — used in BuildDeployTargets and ResetForIteration. MUST be preserved.

### 2.6 Error Code Bug (pre-existing)

`handleDeployComplete`, `handleDeploySkip`, `handleDeployStatus` use `platform.ErrBootstrapNotActive` for deploy errors (tools/workflow_deploy.go:29,51,62). Semantically wrong — should use deploy-specific code.

### 2.7 Engine Methods Missing ctx and Checker (NEW WORK required)

**Current state** (verified engine.go:341,358):
- `DeployStart(projectID, intent string, targets []DeployTarget, mode, strategy string)` — NO `context.Context`
- `DeployComplete(step, attestation string)` — NO `context.Context`, NO checker param

**Contrast**: `BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker)` HAS both ctx and checker.

**Required**: Phase 3 must add `context.Context` to both methods AND add checker parameter to `DeployComplete`. This is NEW implementation work, not a pre-existing decision.

---

## 3. Design Decisions

### 3.1 Confirmed Decisions (from review-1, review-2, philosophy discussion)

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Separate `DeployStepChecker` type (not reuse bootstrap's `StepChecker`) | Deploy has no ServicePlan/BootstrapState. StepChecker signature is `func(ctx, *ServicePlan, *BootstrapState)` — incompatible. Only 2 types — no premature abstraction. |
| 2 | Add `context.Context` to `DeployComplete()` AND `DeployStart()` (NEW WORK) | Checkers need ctx for API calls. Matches BootstrapComplete pattern. Currently NOT implemented — requires Phase 3 signature change. |
| 3 | Deploy checkers in `internal/tools/workflow_checks_deploy.go` | Follows existing `workflow_checks*.go` pattern. |
| 4 | Checkers validate PLATFORM, not APPLICATION | "Help, don't gatekeep." We don't know what user wants from their app. |
| 5 | 2 checkers: `checkDeployPrepare` + `checkDeployResult` | Verify step is informational, not blocking. |
| 6 | Dev→stage is informational, not a hard gate | User may intentionally deploy broken code to dev. |
| 7 | Re-discover env vars via API at deploy-prepare | Deploy is standalone (no BootstrapState). API call is cheap. Handles post-bootstrap changes. |
| 8 | Keep `resolveDeployStepGuidance()` for static content | `resolveStaticGuidance()` handles bootstrap steps only. Deploy has its own step names. |
| 9 | Use `assembleGuidance()` for iteration escalation only, NOT knowledge injection | Phase 2 must REMOVE current assembleKnowledge call from deploy buildGuide. Reuse only `BuildIterationDelta()`. |
| 10 | Fix `ErrBootstrapNotActive` → `ErrDeployNotActive` | Semantic correctness. |

### 3.2 New Decisions (from philosophy discussion + deep review)

| # | Decision | Rationale |
|---|----------|-----------|
| 11 | Deploy guidance is personalized, not generic | Assembled from DeployState + API data. Agent sees THEIR hostnames, modes, steps. |
| 12 | Runtime knowledge NEVER injected in deploy — always pointed to | Agent may not need it. On-demand via zerops_knowledge. Bootstrap MAY inject (creative workflow). |
| 13 | Total guidance per step: 15-55 lines, never 200+ | Compact. Agent reads what matters, pulls what it needs. |
| 14 | Init instructions explain environment concept (container vs local) | Agent needs to know WHERE code is and HOW mounts work. Foundation for all other guidance. |
| 15 | Route returns facts, never recommendations | "Dumb data, smart agent." LLM decides what to do. **NOTE**: Current Route() violates this — has intent-matching + editorial Reason strings. Phase 6 is a SIGNIFICANT refactor. |
| 16 | Strategy alternatives mentioned in 2 lines, never forced | Agent knows options exist without being pushed toward any. |
| 17 | zerops_discover positioned as "state refresh" mechanism | Agent calls whenever it needs current state. Init instructions must say this. |
| 18 | RuntimeType fetched from API at deploy time (RESOLVED from Open Q#3) | ServiceMeta does NOT store RuntimeType. Deploy calls `client.ListServices()` to get runtime type per hostname. Matches "deploy is standalone" philosophy. |
| 19 | zerops_deploy is ASYNC — guidance must reflect polling model | zerops_deploy returns BUILD_TRIGGERED immediately. Agent must poll zerops_events for completion. Guidance templates updated to async model. |
| 20 | DeployStepChecker signature: `func(ctx, projectID, *DeployState, client) (*StepCheckResult, error)` | Different from StepChecker — no Plan/BootstrapState, adds client for API calls. |

### 3.3 Rejected Alternatives

| # | Alternative | Why Rejected |
|---|------------|--------------|
| 1 | Generalize StepChecker for both workflows | Only 2 types. Premature abstraction. StepChecker takes (*ServicePlan, *BootstrapState) — incompatible with deploy. |
| 2 | Health check validation in deploy checker | Application-dependent. We don't know if user wants health checks. |
| 3 | Hard dev→stage gate | User may intentionally deploy broken code. Gatekeeping, not helping. |
| 4 | Full assembleGuidance() unification (replace resolveDeployStepGuidance) | Step name incompatibility. resolveStaticGuidance handles bootstrap steps only. |
| 5 | Skip env var validation in deploy | Env vars can change after bootstrap. Deploy should be standalone. |
| 6 | Inject runtime briefings in deploy (current behavior) | Push model. Agent may not need it. Bootstrap is creative (inject OK), deploy is operational (point only). |
| 7 | Route recommends workflows | "Dumb data, smart agent." Route returns facts. |
| 8 | Store RuntimeType in ServiceMeta | Adds schema change to bootstrap. API re-read at deploy time is simpler and keeps deploy standalone. |

---

## 4. Guidance Assembly — Detailed Design

### 4.1 Data Sources for Personalization

Available from `DeployState` + API + `Engine`:

```go
type deployGuidanceContext struct {
    // From DeployState
    Targets    []DeployTarget  // hostname, role, status, strategy
    Mode       string          // standard, dev, simple
    Strategy   string          // push-dev, ci-cd, manual
    Iteration  int             // 0, 1, 2, ...

    // From API (client.ListServices at deploy start or buildGuide time)
    RuntimeTypes map[string]string  // hostname → "nodejs@22", "go@1", etc.

    // From Engine
    Environment  Environment  // container, local

    // From API (at checker time)
    ServiceStatuses map[string]string  // hostname → "ACTIVE", "READY_TO_DEPLOY", etc.
}
```

**RuntimeType resolution** (Decision #18): `ServiceMeta` does NOT store RuntimeType. At deploy start, `handleDeployStart` calls `client.ListServices()` (already available in scope) and maps hostname → type. This data is passed into `DeployState` or into `buildGuide` directly.

### 4.2 Prepare Step Guidance (complete template)

```markdown
## Deploy Preparation

### Your services
{hostname} ({runtimeType}, {role}) [→ {stageHostname} (stage)]  // for each target
Mode: {mode} | Strategy: {strategy}

### Checklist
1. zerops.yml must exist with `setup:` entries for: {comma-separated hostnames}
2. Env var references (`${hostname_varName}`) must match real variables
3. {if standard}: Dev entry must NOT have healthCheck (dev uses zsc noop)
4. {if simple}: Entry must HAVE healthCheck (server auto-starts)

### Platform rules
- Deploy = new container — local files lost, only deployFiles content survives
- ${hostname_varName} typo = silent literal string, no error from platform
- Build container ≠ run container — different environment
- {if container env}: Files on mount are already on the container — deploy rebuilds, doesn't transfer

### Strategy
Currently: {strategy} ({one-line description})
Other options: {other strategies with one-line descriptions}
Change: zerops_workflow action="strategy" strategies={example}

### Knowledge on demand
{for each unique runtime type}:
- {hostname} ({runtimeType}): zerops_knowledge query="{base runtime}"
{if framework known from recipe hints}:
- Recipes: zerops_knowledge recipe="{name}"
- zerops.yml help: zerops_knowledge query="zerops.yml schema"
- Env vars: zerops_discover includeEnvs=true

{IF first deploy — service status READY_TO_DEPLOY}:
### First deploy
This is the first deploy for {hostnames}. zerops.yml likely needs creation.
Load runtime knowledge before writing config.
```

### 4.3 Deploy Step Guidance (complete template)

```markdown
## Deploy — {mode} mode, {strategy}

### Workflow
{MODE-SPECIFIC STEPS — personalized with actual hostnames}

{if standard}:
1. Deploy to dev: zerops_deploy targetService="{devHostname}"
   → Returns BUILD_TRIGGERED. Poll: zerops_events serviceHostname="{devHostname}"
   → Wait for hint: "DEPLOYED: ..." or "FAILED: ..."
2. Start server on dev manually via SSH (dev uses zsc noop)
   {if implicit-webserver runtime}: Skip — {runtimeType} auto-starts
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify dev: zerops_verify serviceHostname="{devHostname}"
5. Deploy to stage: zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"
   → Poll zerops_events for completion
   Stage auto-starts (real start command + healthCheck)
6. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{stageHostname}"
7. Verify stage: zerops_verify serviceHostname="{stageHostname}"

{if dev}:
1. Deploy to dev: zerops_deploy targetService="{devHostname}"
   → Returns BUILD_TRIGGERED. Poll: zerops_events serviceHostname="{devHostname}"
2. Start server manually via SSH
   {if implicit-webserver}: Skip — auto-starts
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify: zerops_verify serviceHostname="{devHostname}"

{if simple}:
1. Deploy: zerops_deploy targetService="{hostname}" — server auto-starts
   → Returns BUILD_TRIGGERED. Poll: zerops_events serviceHostname="{hostname}"
2. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{hostname}"
3. Verify: zerops_verify serviceHostname="{hostname}"

### Key facts
- zerops_deploy is ASYNC — returns BUILD_TRIGGERED immediately
- Poll zerops_events for completion: look for hint "DEPLOYED: ..." or "FAILED: ..."
- After deploy: only deployFiles content exists. Local files lost.
- {if dev targets}: Dev server: start manually after deploy (zsc noop). Env vars are OS env vars.
- {if stage targets}: Stage: auto-starts with healthCheck monitoring.
- subdomain must be enabled after every deploy (idempotent)

{IF iteration > 0}:
### Iteration {iteration} — Diagnostic escalation
{iteration 1}: Check zerops_logs severity="error". Build failed? → check zerops_events for FAILED hint, review build logs. Container crash? → check start command, ports, env vars.
{iteration 2}: Systematic check: zerops.yml config (ports, start, deployFiles), env var references (typos = literal strings!), runtime version.
{iteration 3+}: Present diagnostic summary to user: exact error from logs, current config state, env var values. User decides next step.

### If something breaks
- Build failed → zerops_events shows FAILED hint, zerops_logs for details, check buildCommands, dependencies, runtime version
- Container didn't start → check start command, ports, env vars. Deploy = new container.
- Running but unreachable → zerops_subdomain, check ports in zerops.yml vs app
- zerops_verify shows unhealthy → check detail field for specific failed check
```

### 4.4 Verify Step Guidance

The existing `deploy-verify` section in deploy.md is already well-structured (diagnostic table, fix patterns). Reuse as-is with minor adjustment: don't inject runtime knowledge, just the diagnostic patterns.

---

## 5. Implementation Plan

### Phase 1: Dead Code Cleanup + Error Code Fix

**Scope**: Remove dead code, fix error codes. No behavioral changes. **No blockers — ready to start.**

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go | Delete: UpdateTarget, DevFailed, Error field, LastAttestation field, 4 dead status constants, ResetForIteration Error clear | -60 |
| workflow/deploy_test.go | Delete: TestDeployState_UpdateTarget, TestDeployState_DevFailed | -25 |
| workflow/deploy_guidance.go | Delete: ResolveDeployGuidance | -23 |
| workflow/deploy_guidance_test.go | Delete: 4 dead tests for ResolveDeployGuidance | -40 |
| tools/workflow_deploy.go | Replace ErrBootstrapNotActive → ErrDeployNotActive (3 occurrences) | ~0 |
| platform/errors.go | Add ErrDeployNotActive constant | +1 |

**Tests**: Run full suite after cleanup. Expect 6 fewer tests, 0 failures.

**TDD note**: This is a pure refactor (no behavior change) — skip RED phase, verify GREEN.

### Phase 2: Guidance Model Redesign

**Scope**: Replace current guidance assembly with personalized, compact model.

**Pre-requisite**: RuntimeType resolution strategy decided (Decision #18: fetch from API).

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go `buildGuide()` | Rewrite: use named params (iteration, env), build personalized guidance from DeployState. **REMOVE** current `assembleKnowledge()` call — deploy must not inject knowledge. Replace with knowledge map pointers. | ~60 (rewrite) |
| workflow/deploy_guidance.go | Rewrite: `buildPersonalizedPrepareGuide()`, `buildPersonalizedDeployGuide()`, `buildPersonalizedVerifyGuide()` — each assembles from state, not from deploy.md sections. Keep `StrategyToSection` map for strategy descriptions. Async deploy model (BUILD_TRIGGERED + events polling). | ~120 (rewrite) |
| workflow/deploy_guidance_test.go | Rewrite: test personalized output for each mode x strategy combination (minimum 7 combos) | ~100 (rewrite) |
| workflow/guidance.go | Add: deploy-specific knowledge map builder (pointers, not injection) | +30 |

**Critical Phase 2 action**: Remove `assembleKnowledge()` call from `buildGuide()` at deploy.go:344-350. Current code INJECTS runtime briefings at DeployStepPrepare via guidance.go:86-98. This violates Decision #12. Replace with knowledge map pointers only.

**RuntimeType in buildGuide**: `buildGuide` receives RuntimeTypes map (hostname → type) passed from `handleDeployStart` which already calls `ListServiceMetas`. Add `client.ListServices()` call in `handleDeployStart` to populate runtime types, pass through to `DeployState` or `buildGuide` params.

**Design detail — `buildPersonalizedDeployGuide()`**:

```go
func buildPersonalizedDeployGuide(state *DeployState, runtimeTypes map[string]string, iteration int, env Environment) string {
    var sb strings.Builder

    // Section 1: Setup summary (from state)
    sb.WriteString("## Deploy — " + state.Mode + " mode, " + state.Strategy + "\n\n")
    sb.WriteString("### Workflow\n")

    // Section 2: Mode-specific steps with actual hostnames (ASYNC deploy model)
    switch state.Mode {
    case PlanModeStandard:
        writeStandardWorkflow(&sb, state.Targets, runtimeTypes)
    case PlanModeDev:
        writeDevWorkflow(&sb, state.Targets, runtimeTypes)
    case PlanModeSimple:
        writeSimpleWorkflow(&sb, state.Targets, runtimeTypes)
    }

    // Section 3: Platform facts (always, compact) — async deploy model
    sb.WriteString("\n### Key facts\n")
    writePlatformFacts(&sb, state) // includes "zerops_deploy is ASYNC"

    // Section 4: Iteration escalation (conditional)
    if iteration > 0 {
        writeIterationEscalation(&sb, iteration)
    }

    // Section 5: Diagnostic pointers (always, compact)
    sb.WriteString("\n### If something breaks\n")
    writeDiagnosticPointers(&sb)

    return sb.String()
}
```

**deploy.md role change**: deploy.md continues to exist as reference content for `zerops_knowledge` queries and for bootstrap's deploy step. Deploy WORKFLOW guidance is now built programmatically.

**TDD**:
- RED: Write tests for personalized output (standard+push-dev, standard+ci-cd, standard+manual, dev+push-dev, dev+ci-cd, simple+push-dev, simple+manual — minimum 7 combos)
- GREEN: Implement builders
- REFACTOR: Ensure <350 lines per file

### Phase 3: Platform Validation Checkers

**Scope**: Add checkers to validate platform integration at prepare and deploy steps.

**Pre-requisite**: Phase 2 complete (buildGuide rewritten).

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/engine.go `DeployComplete` + `DeployStart` | Add context.Context params + checker param to DeployComplete | +20 |
| workflow/deploy.go or workflow/deploy_checks.go | Define DeployStepChecker type | +5 |
| tools/workflow_deploy.go | Wire ctx + checker deps, build checker, pass to engine | +35 |
| tools/workflow_checks_deploy.go | checkDeployPrepare + checkDeployResult + env var re-discovery | +120 |
| tools/workflow_checks_deploy_test.go | Tests for both checkers | +80 |

**DeployStepChecker type** (Decision #20):
```go
// DeployStepChecker validates deploy step requirements against live platform state.
// Different from bootstrap's StepChecker — no ServicePlan or BootstrapState.
type DeployStepChecker func(ctx context.Context, projectID string, state *DeployState, client platform.Client) (*StepCheckResult, error)
```

**checkDeployPrepare(client, projectID, stateDir)**:
1. Find zerops.yml (reuse `filepath.Dir(filepath.Dir(stateDir))` pattern from checkGenerate)
2. Parse YAML — syntax valid?
3. `setup:` entries match deploy target hostnames?
4. Env var reference syntax (`${hostname_varName}`) — validated against env vars re-discovered via `client.GetServiceEnv()` for each dependency service
5. Return `StepCheckResult{Passed, Checks, Summary}`

**checkDeployResult(client, projectID)** — uses Events API for async deploy:
1. Query Events API for each target: check process status (FINISHED, FAILED, RUNNING)
   - Process status FAILED → "Build failed: check zerops_events hint, zerops_logs for details"
   - Process status RUNNING → "Build still in progress"
   - Process status FINISHED → check service status
2. Check service status for each target:
   - `READY_TO_DEPLOY` (still, after process FINISHED) → "container didn't start, check start command"
   - `ACTIVE/RUNNING` + zerops_verify unhealthy → "running but issues, check zerops_logs"
   - `ACTIVE/RUNNING` + healthy → "deployed successfully"
3. Check subdomain access for services with ports
4. Use Events API `hint` field (ops/events.go) for LLM-friendly diagnostic messages
5. Return informational `StepCheckResult` (diagnostic, not blocking except for objective failures)

**TDD**:
- RED: Tests for each failure scenario (build failed, build in progress, container crash, running+unhealthy, success, env var typo)
- GREEN: Implement checkers
- REFACTOR: Ensure <350 lines per file

### Phase 4: Init Instructions Update

**Scope**: Add environment concept, code access model, state refresh instructions.

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| Content for `zcp init` output | Add container vs local environment section | +20-30 |
| Content for `zcp init` output | Add state refresh note (zerops_discover) | +5 |
| Content for `zcp init` output | Add deploy = rebuild explanation | +5 |

**Container mode additions**:
```
## Your Environment

You're on the zcpx container inside a Zerops project.

### Code Access
Runtime services are SSHFS-mounted:
  /var/www/{hostname}/ — edit code here, changes appear on the service container
Mount is read/write, changes immediate.

### Deploy = Rebuild
Editing files on mount does NOT trigger deploy. Deploy runs the full pipeline
(build → deployFiles → start). Deploy when zerops.yml changes or you need
a clean rebuild. Code-only changes on dev: just restart the server via SSH.

### Staying Current
zerops_discover always returns CURRENT state of all services.
Call it anytime to refresh your understanding.
```

**Local mode additions**:
```
## Your Environment

You're running locally. Code is in the working directory.
Deploy pushes code to Zerops via zcli push.
zerops.yml must be at repository root. Each deploy = full rebuild.
```

**Note**: Exact location of init instructions depends on how `zcp init` generates CLAUDE.md / instructions. This phase defines the CONTENT; integration point needs verification.

### Phase 5: Transition Improvements

**Scope**: Ensure smooth transitions between bootstrap → strategy → deploy.

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/bootstrap_guide_assembly.go `BuildTransitionMessage()` | Verify includes strategy selection prompt + deploy entry command | ~5 (verify/adjust) |
| tools/workflow_strategy.go `handleStrategy()` | Add "next" field with "ready to deploy" command to response | +5 |

**Bootstrap → Strategy transition**:
`BuildTransitionMessage()` at `bootstrap_guide_assembly.go:58-133` already includes strategy selection and deploy workflow offerings. Verify it's sufficient.

**Strategy → Deploy transition**:
Strategy set output should include:
```json
{
  "status": "updated",
  "services": "appdev=push-dev",
  "next": "When code is ready: zerops_workflow action=\"start\" workflow=\"deploy\""
}
```

Currently `handleStrategy` returns `{status, services, guidance}` with no "next" field. Add it.

### Phase 6: Route Simplification

**Scope**: Refactor Route to return facts only, removing recommendation logic.

**WARNING**: This is a SIGNIFICANT refactor, not "verify and adjust." Current Route() at router.go has:
- `boostByIntent()` (lines 91-104) — promotes workflows based on intent keyword matching
- `intentPatterns` map (lines 81-88) — recommendation logic
- Editorial `Reason` strings like "Fresh project — no runtime services"
- `FlowOffering.Hint` field with command suggestions

**Changes required**:

| File | Change | Lines |
|------|--------|-------|
| workflow/router.go | Remove `boostByIntent()`, `intentPatterns`. Replace `FlowOffering` with factual format. Remove editorial Reason strings. | ~-80, +40 |
| workflow/router_test.go | Update tests for new factual format | ~rewrite |

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

No "suggestedAction", no "recommendation", no intent-matching. Agent decides based on facts + user request.

**Alternative**: If Route's recommendation behavior is deemed valuable, update `docs/spec-guidance-philosophy.md` to explicitly allow route recommendations as an exception to the "facts only" principle. Document the decision.

---

## 6. File Impact Summary

| File | Phase | Change Type | Est. Lines |
|------|-------|-------------|-----------|
| workflow/deploy.go | 1,2 | Delete dead code + rewrite buildGuide + remove assembleKnowledge | -60, +60 |
| workflow/deploy_test.go | 1,2 | Delete dead tests + new personalization tests | -25, +50 |
| workflow/deploy_guidance.go | 1,2 | Delete ResolveDeployGuidance + rewrite with builders (async model) | -23, +120 |
| workflow/deploy_guidance_test.go | 1,2 | Delete dead tests + new builder tests (7 combos) | -40, +100 |
| workflow/engine.go | 3 | Add ctx + checker to DeployComplete + DeployStart | +20 |
| workflow/guidance.go | 2 | Add knowledge map builder (pointers only) | +30 |
| tools/workflow_deploy.go | 1,3 | Fix error codes + wire checkers | +35 |
| tools/workflow_checks_deploy.go | 3 | DeployStepChecker type + 2 checkers + diagnostic builder | +125 |
| tools/workflow_checks_deploy_test.go | 3 | Checker tests (6 scenarios) | +80 |
| platform/errors.go | 1 | Add ErrDeployNotActive | +1 |
| init content | 4 | Environment concept + state refresh | +30 |
| tools/workflow_strategy.go | 5 | Add "next" field to strategy response | +5 |
| workflow/bootstrap_guide_assembly.go | 5 | Verify BuildTransitionMessage | ~5 |
| workflow/router.go | 6 | Refactor to facts-only OR document exception | -80, +40 |
| workflow/router_test.go | 6 | Update tests for new format | ~rewrite |

---

## 7. Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Personalized guidance builders are more code than template extraction | MEDIUM | Builders are simple string assembly from struct fields. Table-driven tests cover all mode x strategy combos. |
| deploy.md sections become orphaned from deploy workflow | LOW | deploy.md continues to serve: bootstrap deploy step, zerops_knowledge queries, reference docs. Mark sections with their consumers. |
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | checkGenerate's filepath.Dir pattern is generic and reusable (verified). |
| Env var re-discovery adds API calls at prepare step | LOW | One GetServiceEnv call per dependency. Lightweight. |
| Phase 2 guidance output differs significantly from current | MEDIUM | Table-driven tests validate output for all mode/strategy combos. |
| Init instructions location unclear | LOW | Phase 4 defines content; integration point verified during implementation. |
| Events API hint field availability | LOW | Confirmed in ops/events.go. Diagnostic builder uses hint field for LLM-friendly messages. |
| Route refactor breaks existing behavior | MEDIUM | Phase 6 is last phase. Decide: refactor or document exception. Either way, explicit decision. |
| RuntimeType not on ServiceMeta | RESOLVED | Decision #18: Fetch from API at deploy time via client.ListServices(). |
| zerops_deploy is async, not blocking | RESOLVED | Decision #19: Guidance templates updated to async model with events polling. |

---

## 8. Open Questions (to resolve during implementation)

| # | Question | Phase | How to Resolve | Status |
|---|----------|-------|---------------|--------|
| 1 | Exact init instructions integration point — where does `zcp init` put environment concept? | 4 | Read `internal/init/init.go` and current CLAUDE.md template | OPEN |
| 2 | Should personalized guidance include service IDs or only hostnames? | 2 | Hostnames only (consistent with all existing guidance) | RESOLVED |
| 3 | How does buildGuide get RuntimeType? DeployState doesn't store it. | 2 | **RESOLVED**: Fetch from API via `client.ListServices()` at deploy start. Pass as param to buildGuide. ServiceMeta has no RuntimeType field. | RESOLVED |
| 4 | Events API hint field — what's the Go type/field name? | 3 | **RESOLVED**: `TimelineEvent.Hint` at `internal/ops/events.go:40`. ZCP-generated from `processHintMap` and `appVersionHintMap`. | RESOLVED |
| 5 | Should deploy.md sections be simplified now that deploy workflow generates guidance? | 6 | Keep for bootstrap + knowledge queries. Mark with `<!-- consumer: bootstrap, knowledge -->` | OPEN |
| 6 | Strategy transition support — how to update ServiceMeta when user switches strategy mid-lifecycle? | Future | action="strategy" already handles this (tools/workflow_strategy.go) | RESOLVED |
| 7 | Phase 6: Refactor Route to facts-only or document exception? | 6 | Decide before Phase 6 implementation. Current Route is a recommendation engine. | OPEN |
| 8 | Where to store RuntimeTypes for buildGuide access? | 2 | Add `RuntimeTypes map[string]string` to DeployState, populated in handleDeployStart. Or pass as param to buildGuide. | OPEN (impl detail) |

---

## 9. Verification Checklist (for implementation team)

Before marking each phase complete, verify:

### Phase 1 (Dead Code)
- [ ] `UpdateTarget()` deleted from workflow/deploy.go
- [ ] `DevFailed()` deleted from workflow/deploy.go
- [ ] `Error`, `LastAttestation` fields deleted from DeployTarget
- [ ] 4 dead status constants deleted (deployed, verified, failed, skipped)
- [ ] `deployTargetPending` PRESERVED (verify grep: should appear in BuildDeployTargets + ResetForIteration)
- [ ] `ResolveDeployGuidance()` deleted from workflow/deploy_guidance.go
- [ ] Dead tests deleted from deploy_test.go and deploy_guidance_test.go
- [ ] `ErrDeployNotActive` added to platform/errors.go
- [ ] 3 occurrences in tools/workflow_deploy.go updated
- [ ] `go test ./... -count=1 -short` passes
- [ ] `make lint-fast` passes

### Phase 2 (Guidance Redesign)
- [ ] `buildGuide()` uses named params (iteration, env), not `_ int, _ Environment`
- [ ] **assembleKnowledge() call REMOVED from deploy buildGuide** — knowledge not injected
- [ ] RuntimeType fetched from API and available to guidance builders
- [ ] Personalized guidance generated from DeployState (hostnames, mode, strategy)
- [ ] Platform facts include ASYNC deploy model (BUILD_TRIGGERED + events polling)
- [ ] Strategy alternatives mentioned in 2 lines
- [ ] Knowledge map includes pointers for each unique runtime type (not injection)
- [ ] First deploy detection (READY_TO_DEPLOY) included as contextual note
- [ ] Iteration escalation (iteration 1/2/3) included as contextual guidance
- [ ] Total guidance per step <= 55 lines
- [ ] Tests cover minimum 7 mode x strategy combos
- [ ] All files <= 350 lines

### Phase 3 (Checkers)
- [ ] `DeployStepChecker` type defined: `func(ctx, projectID, *DeployState, client) (*StepCheckResult, error)`
- [ ] `context.Context` added to DeployComplete AND DeployStart signatures
- [ ] Checker parameter added to DeployComplete (matches BootstrapComplete pattern)
- [ ] `handleDeployComplete` passes ctx + checker to engine
- [ ] `checkDeployPrepare`: zerops.yml parse + hostname match + env var ref validation
- [ ] `checkDeployResult`: Events API process status + service status + diagnostic feedback
- [ ] Env var re-discovery via `client.GetServiceEnv()` at prepare step
- [ ] Tests cover: nil state, build failed (process FAILED), build in progress, container crash, success, env var typo
- [ ] `StepCheckResult` reused (not duplicated)

### Phase 4 (Init Instructions)
- [ ] Container vs local environment explained
- [ ] Code access model (mounts vs local files) explained
- [ ] Deploy = rebuild concept explained
- [ ] State refresh via zerops_discover mentioned
- [ ] No workflow recommendations (just "when you need X → tool Y")

### Phase 5 (Transitions)
- [ ] `BuildTransitionMessage()` at **bootstrap_guide_assembly.go:58** includes strategy selection prompt
- [ ] Strategy set output includes "next" field with "ready to deploy" command
- [ ] No recommendations — just facts + commands

### Phase 6 (Route)
- [ ] Decision made: refactor to facts-only OR document exception
- [ ] If refactoring: boostByIntent removed, intentPatterns removed, editorial Reasons removed
- [ ] If exception: spec-guidance-philosophy.md updated with explicit Route exception
- [ ] Tests updated for chosen approach

---

## 10. Decision Record

All decisions in this plan are traceable to evidence:

| Decision | Evidence | Source |
|----------|----------|--------|
| Dead code list accurate | grep: 0 production callers for all items | Deep review R1 (4 agents verified) |
| deployTargetPending is LIVE | Used at deploy.go:154, 163, 273 | Deep review R1 |
| Strategy gate exists and works | tools/workflow.go:228-248 | Deep review R1 |
| StepCheckResult is generic, reusable | bootstrap_checks.go:6-17, no bootstrap-specific fields | KB-research |
| StepChecker IS bootstrap-specific | bootstrap_checks.go:24 takes (*ServicePlan, *BootstrapState) | KB-research + correctness |
| checkGenerate path resolution is reusable | workflow_checks_generate.go:29, filepath.Dir generic | KB-research |
| DiscoveredEnvVars not on DeployState | Only on BootstrapState | All 4 agents + KB-research |
| **zerops_deploy is ASYNC** | ops/deploy.go returns BUILD_TRIGGERED, MonitorHint says poll events | KB-verifier (live test 2026-03-21) |
| **Events API hint field at ops/events.go:40** | processHintMap + appVersionHintMap, LLM-friendly prefixes | KB-verifier (live test 2026-03-21) |
| **ServiceMeta has NO RuntimeType** | service_meta.go:22-29, 6 fields only | KB-research + all 4 analysts |
| **BuildTransitionMessage at bootstrap_guide_assembly.go:58** | grep + read confirmation | KB-research |
| **DeployStart/Complete lack ctx** | engine.go:341,358 | Correctness F4 |
| **Route is recommendation engine** | router.go:81-104 boostByIntent, intentPatterns | Adversarial F3 |
| Env var typos = silent literal strings | Verified on live Zerops 2026-03-04 | Memory (verified fact) |
| GuidanceParams has 11 fields (not 10) | guidance.go:10-22 | KB-research + correctness |
| Push model wastes tokens, agent may not need injected knowledge | Philosophy discussion with user | Session 2026-03-21 |
| Route should return facts, not recommendations | User: "zcp should be dumb" | Session 2026-03-21 |
| Init instructions must explain container vs local concept | User: "vysvetlit ten koncept toho jak to funguje" | Session 2026-03-21 |
| zerops_discover is the state refresh mechanism | User: "to si rikam ze je pokryte v discovery" | Session 2026-03-21 |
| **Deploy buildGuide currently injects knowledge (must remove)** | deploy.go:344-350 calls assembleKnowledge | Adversarial analysis |
