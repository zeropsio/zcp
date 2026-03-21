# Deploy Workflow Guidance Redesign — Complete Plan

**Date**: 2026-03-21
**Status**: Ready for implementation
**Scope**: Deploy workflow guidance model + init instructions + transitions
**Philosophy**: `docs/spec-guidance-philosophy.md` (authoritative)
**Workflow spec**: `docs/spec-bootstrap-deploy.md` (authoritative for step mechanics)

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

`handleDeployStart()` (tools/workflow.go:186-258) has 5 sequential gates:

1. **Metas exist?** (ř. 188-194) — "Run bootstrap first"
2. **Metas complete?** (ř. 204-211) — `IsComplete()` checks `BootstrappedAt != ""`
3. **Runtime services?** (ř. 213-225) — filters `Mode != "" || StageHostname != ""`
4. **Strategy set?** (ř. 227-236) — soft gate: returns strategy selection guidance
5. **Strategy consistent?** (ř. 241-248) — rejects mixed strategies

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
- **Problem**: 4 of 10 GuidanceParams fields used (Step, Mode, Strategy, KP)

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

---

## 3. Design Decisions

### 3.1 Confirmed Decisions (from review-1, review-2, philosophy discussion)

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Separate `DeployStepChecker` type (not reuse bootstrap's `StepChecker`) | Deploy has no ServicePlan/BootstrapState. Only 2 types — no premature abstraction. |
| 2 | Add `context.Context` to `DeployComplete()` AND `DeployStart()` | Checkers need ctx for API calls. Matches BootstrapComplete pattern. |
| 3 | Deploy checkers in `internal/tools/workflow_checks_deploy.go` | Follows existing `workflow_checks*.go` pattern. |
| 4 | Checkers validate PLATFORM, not APPLICATION | "Help, don't gatekeep." We don't know what user wants from their app. |
| 5 | 2 checkers: `checkDeployPrepare` + `checkDeployResult` | Verify step is informational, not blocking. |
| 6 | Dev→stage is informational, not a hard gate | User may intentionally deploy broken code to dev. |
| 7 | Re-discover env vars via API at deploy-prepare | Deploy is standalone (no BootstrapState). API call is cheap. Handles post-bootstrap changes. |
| 8 | Keep `resolveDeployStepGuidance()` for static content | `resolveStaticGuidance()` handles bootstrap steps only. Deploy has its own step names. |
| 9 | Use `assembleGuidance()` for knowledge injection + iteration escalation only | Reuses `BuildIterationDelta()`. `StepDeploy == DeployStepDeploy == "deploy"` works. |
| 10 | Fix `ErrBootstrapNotActive` → `ErrDeployNotActive` | Semantic correctness. |

### 3.2 New Decisions (from philosophy discussion)

| # | Decision | Rationale |
|---|----------|-----------|
| 11 | Deploy guidance is personalized, not generic | Assembled from DeployState + ServiceMeta. Agent sees THEIR hostnames, modes, steps. |
| 12 | Runtime knowledge NEVER injected in deploy — always pointed to | Agent may not need it. On-demand via zerops_knowledge. Bootstrap MAY inject (creative workflow). |
| 13 | Total guidance per step: 15-55 lines, never 200+ | Compact. Agent reads what matters, pulls what it needs. |
| 14 | Init instructions explain environment concept (container vs local) | Agent needs to know WHERE code is and HOW mounts work. Foundation for all other guidance. |
| 15 | Route returns facts, never recommendations | "Dumb data, smart agent." LLM decides what to do. |
| 16 | Strategy alternatives mentioned in 2 lines, never forced | Agent knows options exist without being pushed toward any. |
| 17 | zerops_discover positioned as "state refresh" mechanism | Agent calls whenever it needs current state. Init instructions must say this. |

### 3.3 Rejected Alternatives

| # | Alternative | Why Rejected |
|---|------------|--------------|
| 1 | Generalize StepChecker for both workflows | Only 2 types. Premature abstraction. |
| 2 | Health check validation in deploy checker | Application-dependent. We don't know if user wants health checks. |
| 3 | Hard dev→stage gate | User may intentionally deploy broken code. Gatekeeping, not helping. |
| 4 | Full assembleGuidance() unification (replace resolveDeployStepGuidance) | Step name incompatibility. resolveStaticGuidance handles bootstrap steps only. |
| 5 | Skip env var validation in deploy | Env vars can change after bootstrap. Deploy should be standalone. |
| 6 | Inject runtime briefings in deploy (current behavior) | Push model. Agent may not need it. Bootstrap is creative (inject OK), deploy is operational (point only). |
| 7 | Route recommends workflows | "Dumb data, smart agent." Route returns facts. |

---

## 4. Guidance Assembly — Detailed Design

### 4.1 Data Sources for Personalization

Available from `DeployState` + `ServiceMeta` + `Engine`:

```go
type deployGuidanceContext struct {
    // From DeployState
    Targets    []DeployTarget  // hostname, role, status, strategy
    Mode       string          // standard, dev, simple
    Strategy   string          // push-dev, ci-cd, manual
    Iteration  int             // 0, 1, 2, ...

    // From ServiceMeta (readable via hostname)
    RuntimeTypes map[string]string  // hostname → "nodejs@22", "go@1", etc.

    // From Engine
    Environment  Environment  // container, local

    // From API (at checker time)
    ServiceStatuses map[string]string  // hostname → "ACTIVE", "READY_TO_DEPLOY", etc.
}
```

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
2. Start server on dev manually via SSH (dev uses zsc noop)
   {if implicit-webserver runtime}: Skip — {runtimeType} auto-starts
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify dev: zerops_verify serviceHostname="{devHostname}"
5. Deploy to stage: zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"
   Stage auto-starts (real start command + healthCheck)
6. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{stageHostname}"
7. Verify stage: zerops_verify serviceHostname="{stageHostname}"

{if dev}:
1. Deploy to dev: zerops_deploy targetService="{devHostname}"
2. Start server manually via SSH
   {if implicit-webserver}: Skip — auto-starts
3. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
4. Verify: zerops_verify serviceHostname="{devHostname}"

{if simple}:
1. Deploy: zerops_deploy targetService="{hostname}" — server auto-starts
2. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{hostname}"
3. Verify: zerops_verify serviceHostname="{hostname}"

### Key facts
- zerops_deploy blocks until complete — returns DEPLOYED or BUILD_FAILED
- After deploy: only deployFiles content exists. Local files lost.
- {if dev targets}: Dev server: start manually after deploy (zsc noop). Env vars are OS env vars.
- {if stage targets}: Stage: auto-starts with healthCheck monitoring.
- subdomain must be enabled after every deploy (idempotent)

{IF iteration > 0}:
### Iteration {iteration} — Diagnostic escalation
{iteration 1}: Check zerops_logs severity="error". Build failed? → review buildLogs from deploy response. Container crash? → check start command, ports, env vars.
{iteration 2}: Systematic check: zerops.yml config (ports, start, deployFiles), env var references (typos = literal strings!), runtime version.
{iteration 3+}: Present diagnostic summary to user: exact error from logs, current config state, env var values. User decides next step.

### If something breaks
- Build failed → zerops_logs, check buildCommands, dependencies, runtime version
- Container didn't start → check start command, ports, env vars. Deploy = new container.
- Running but unreachable → zerops_subdomain, check ports in zerops.yml vs app
- zerops_verify shows unhealthy → check detail field for specific failed check
```

### 4.4 Verify Step Guidance

The existing `deploy-verify` section in deploy.md is already well-structured (diagnostic table, fix patterns). Reuse as-is with minor adjustment: don't inject runtime knowledge, just the diagnostic patterns.

---

## 5. Implementation Plan

### Phase 1: Dead Code Cleanup + Error Code Fix

**Scope**: Remove dead code, fix error codes. No behavioral changes.

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

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/deploy.go `buildGuide()` | Rewrite: use named params (iteration, env), build personalized guidance from DeployState + ServiceMeta. Replace `resolveDeployStepGuidance + assembleKnowledge` with new assembly logic. | ~60 (rewrite) |
| workflow/deploy_guidance.go | Rewrite: `buildPersonalizedPrepareGuide()`, `buildPersonalizedDeployGuide()`, `buildPersonalizedVerifyGuide()` — each assembles from state, not from deploy.md sections. Keep `StrategyToSection` map for strategy descriptions. | ~120 (rewrite) |
| workflow/deploy_guidance_test.go | Rewrite: test personalized output for each mode × strategy combination | ~100 (rewrite) |
| workflow/guidance.go | Add: deploy-specific knowledge map builder (pointers, not injection) | +30 |

**Design detail — `buildPersonalizedDeployGuide()`**:

```go
func buildPersonalizedDeployGuide(state *DeployState, iteration int, env Environment) string {
    var sb strings.Builder

    // Section 1: Setup summary (from state)
    sb.WriteString("## Deploy — " + state.Mode + " mode, " + state.Strategy + "\n\n")
    sb.WriteString("### Workflow\n")

    // Section 2: Mode-specific steps with actual hostnames
    switch state.Mode {
    case PlanModeStandard:
        writeStandardWorkflow(&sb, state.Targets)
    case PlanModeDev:
        writeDevWorkflow(&sb, state.Targets)
    case PlanModeSimple:
        writeSimpleWorkflow(&sb, state.Targets)
    }

    // Section 3: Platform facts (always, compact)
    sb.WriteString("\n### Key facts\n")
    writePlatformFacts(&sb, state)

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

**Key change**: Guidance is now GENERATED from state, not EXTRACTED from deploy.md sections. deploy.md sections for mode-specific content (deploy-execute-standard, deploy-execute-dev, deploy-execute-simple) become REFERENCE material, not the primary source for deploy workflow guidance.

**deploy.md role change**: deploy.md continues to exist as reference content for `zerops_knowledge` queries and for bootstrap's deploy step. Deploy WORKFLOW guidance is now built programmatically.

**TDD**:
- RED: Write tests for personalized output (standard+push-dev, dev+ci-cd, simple+manual, etc.)
- GREEN: Implement builders
- REFACTOR: Ensure <350 lines per file

### Phase 3: Platform Validation Checkers

**Scope**: Add checkers to validate platform integration at prepare and deploy steps.

**Changes**:

| File | Change | Lines |
|------|--------|-------|
| workflow/engine.go `DeployComplete` + `DeployStart` | Add context.Context params | +15 |
| tools/workflow_deploy.go | Wire ctx + checker deps, build checker, pass to engine | +35 |
| tools/workflow_checks_deploy.go | DeployStepChecker type + checkDeployPrepare + checkDeployResult + env var re-discovery | +120 |
| tools/workflow_checks_deploy_test.go | Tests for both checkers | +80 |

**checkDeployPrepare(client, projectID, stateDir)**:
1. Find zerops.yml (reuse `filepath.Dir(filepath.Dir(stateDir))` pattern from checkGenerate)
2. Parse YAML — syntax valid?
3. `setup:` entries match deploy target hostnames?
4. Env var reference syntax (`${hostname_varName}`) — validated against env vars re-discovered via `client.GetServiceEnv()` for each dependency service
5. Return `StepCheckResult{Passed, Checks, Summary}`

**checkDeployResult(client, projectID)**:
1. Query API: service status for each target
2. Build diagnostic response based on status:
   - `BUILD_FAILED` → "check buildLogs, dependencies, runtime version"
   - `READY_TO_DEPLOY` (still, after deploy) → "container didn't start, check start command, ports, env vars"
   - `ACTIVE/RUNNING` + zerops_verify unhealthy → "running but issues, check zerops_logs"
   - `ACTIVE/RUNNING` + healthy → "deployed successfully"
3. Check subdomain access for services with ports
4. Consider Events API `hint` field for LLM-friendly status
5. Return informational `StepCheckResult` (diagnostic, not blocking except for objective failures)

**TDD**:
- RED: Tests for each failure scenario (build failed, container crash, running+unhealthy, success)
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
| workflow/bootstrap_outputs.go `BuildTransitionMessage()` | Verify includes strategy selection prompt + deploy entry command | ~5 (verify/adjust) |
| tools/workflow_strategy.go `handleStrategy()` | Add "ready to deploy" guidance to response | +5 |

**Bootstrap → Strategy transition**:
Bootstrap complete output should include:
```json
{
  "message": "Bootstrap complete. Services ready.",
  "transition": "Choose deploy strategy for each service",
  "command": "zerops_workflow action=\"strategy\" strategies={...}"
}
```

**Strategy → Deploy transition**:
Strategy set output should include:
```json
{
  "status": "updated",
  "services": "appdev=push-dev",
  "next": "When code is ready: zerops_workflow action=\"start\" workflow=\"deploy\""
}
```

### Phase 6: Route Simplification

**Scope**: Ensure route returns facts only, no recommendations.

**Changes**: Verify current `Route()` implementation returns data, not opinions. Adjust if needed.

**Expected route output format**:
```json
{
  "project": {"state": "ACTIVE"},
  "services": [
    {"hostname": "appdev", "type": "nodejs@22", "status": "RUNNING",
     "mode": "standard", "strategy": "push-dev", "stageHostname": "appstage"}
  ],
  "activeSessions": [],
  "environment": "container",
  "availableWorkflows": ["bootstrap", "deploy", "debug", "scale", "configure"]
}
```

No "suggestedAction", no "recommendation". Just facts.

---

## 6. File Impact Summary

| File | Phase | Change Type | Est. Lines |
|------|-------|-------------|-----------|
| workflow/deploy.go | 1,2 | Delete dead code + rewrite buildGuide | -60, +60 |
| workflow/deploy_test.go | 1,2 | Delete dead tests + new personalization tests | -25, +50 |
| workflow/deploy_guidance.go | 1,2 | Delete ResolveDeployGuidance + rewrite with builders | -23, +120 |
| workflow/deploy_guidance_test.go | 1,2 | Delete dead tests + new builder tests | -40, +100 |
| workflow/engine.go | 3 | Add ctx to DeployComplete + DeployStart | +15 |
| workflow/guidance.go | 2 | Add knowledge map builder | +30 |
| tools/workflow_deploy.go | 1,3 | Fix error codes + wire checkers | +35 |
| tools/workflow_checks_deploy.go | 3 | 2 checkers + type + diagnostic builder | +120 |
| tools/workflow_checks_deploy_test.go | 3 | Checker tests | +80 |
| platform/errors.go | 1 | Add ErrDeployNotActive | +1 |
| init content | 4 | Environment concept + state refresh | +30 |
| tools/workflow_strategy.go | 5 | Transition guidance | +5 |
| workflow/bootstrap_outputs.go | 5 | Verify transition message | ~5 |

---

## 7. Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Personalized guidance builders are more code than template extraction | MEDIUM | Builders are simple string assembly from struct fields. Table-driven tests cover all mode×strategy combos. |
| deploy.md sections become orphaned from deploy workflow | LOW | deploy.md continues to serve: bootstrap deploy step, zerops_knowledge queries, reference docs. Mark sections with their consumers. |
| checkDeployPrepare needs zerops.yml path resolution | MEDIUM | checkGenerate's filepath.Dir pattern is generic and reusable (verified). |
| Env var re-discovery adds API calls at prepare step | LOW | One GetServiceEnv call per dependency. Lightweight. |
| Phase 2 guidance output differs significantly from current | MEDIUM | Table-driven tests validate output for all mode/strategy combos. Gradual rollout possible. |
| Init instructions location unclear | LOW | Phase 4 defines content; integration point verified during implementation. |
| Events API hint field availability | LOW | Diagnostic builder degrades gracefully if hint not available. |

---

## 8. Open Questions (to resolve during implementation)

| # | Question | Phase | How to Resolve |
|---|----------|-------|---------------|
| 1 | Exact init instructions integration point — where does `zcp init` put environment concept? | 4 | Read `internal/init/init.go` and current CLAUDE.md template |
| 2 | Should personalized guidance include service IDs or only hostnames? | 2 | Hostnames only (consistent with all existing guidance) |
| 3 | How does buildGuide get RuntimeType? DeployState doesn't store it. | 2 | Read from ServiceMeta at buildGuide time, or store in DeployTarget during BuildDeployTargets |
| 4 | Events API hint field — what's the Go type/field name? | 3 | Check platform/types.go for event hint field |
| 5 | Should deploy.md sections be simplified now that deploy workflow generates guidance? | 6 | Keep for bootstrap + knowledge queries. Mark with `<!-- consumer: bootstrap, knowledge -->` |
| 6 | Strategy transition support — how to update ServiceMeta when user switches strategy mid-lifecycle? | Future | action="strategy" already handles this (tools/workflow_strategy.go) |

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
- [ ] Personalized guidance generated from DeployState (hostnames, mode, strategy)
- [ ] No runtime briefing injected (only pointers via knowledge map)
- [ ] Platform facts always included (container lifecycle, env vars, etc.)
- [ ] Strategy alternatives mentioned in 2 lines
- [ ] Knowledge map includes pointers for each unique runtime type
- [ ] First deploy detection (READY_TO_DEPLOY) included as contextual note
- [ ] Iteration escalation (iteration 1/2/3) included as contextual guidance
- [ ] Total guidance per step ≤ 55 lines
- [ ] Tests cover: standard×push-dev, dev×ci-cd, simple×manual (minimum 3 combos)
- [ ] All files ≤ 350 lines

### Phase 3 (Checkers)
- [ ] `DeployStepChecker` type defined (separate from bootstrap StepChecker)
- [ ] `context.Context` added to DeployComplete AND DeployStart
- [ ] `handleDeployComplete` passes ctx + checker to engine
- [ ] `checkDeployPrepare`: zerops.yml parse + hostname match + env var ref validation
- [ ] `checkDeployResult`: API status + diagnostic feedback
- [ ] Env var re-discovery via `client.GetServiceEnv()` at prepare step
- [ ] Tests cover: nil plan, build failed, container crash, success, env var typo
- [ ] `StepCheckResult` reused (not duplicated)

### Phase 4 (Init Instructions)
- [ ] Container vs local environment explained
- [ ] Code access model (mounts vs local files) explained
- [ ] Deploy = rebuild concept explained
- [ ] State refresh via zerops_discover mentioned
- [ ] No workflow recommendations (just "when you need X → tool Y")

### Phase 5 (Transitions)
- [ ] Bootstrap complete output includes strategy selection prompt
- [ ] Strategy set output includes "ready to deploy" command
- [ ] No recommendations — just facts + commands

### Phase 6 (Route)
- [ ] Route returns facts only (services, states, metas)
- [ ] No "suggestedAction" or "recommendation" fields
- [ ] Available workflows listed as data, not suggestions

---

## 10. Decision Record

All decisions in this plan are traceable to evidence:

| Decision | Evidence | Source |
|----------|----------|--------|
| Dead code list accurate | grep: 0 production callers for all items | Deep review R1 + R2 (4 agents verified) |
| deployTargetPending is LIVE | Used at deploy.go:154, 164, 273 | Deep review R1 + R2 |
| Strategy gate exists and works | tools/workflow.go:227-248 | Deep review R2 (adversarial refuted) |
| StepCheckResult is generic, reusable | bootstrap_checks.go:6-17, no bootstrap-specific fields | KB-research |
| checkGenerate path resolution is reusable | workflow_checks_generate.go:29, filepath.Dir generic | KB-research |
| DiscoveredEnvVars not on DeployState | Only on BootstrapState | All 4 R2 agents + KB-research |
| Build failure returns buildLogs in deploy response | Live platform verification | KB-verifier claim 2 |
| Service status fields: ACTIVE, RUNNING, STOPPED, READY_TO_DEPLOY | Live platform verification | KB-verifier claim 1 |
| Env var typos = silent literal strings | Verified on live Zerops 2026-03-04 | Memory (verified fact) |
| Events API has hint field | Live API response inspection | KB-verifier claim 7 |
| ErrBootstrapNotActive used in deploy handlers | tools/workflow_deploy.go:29,51,62 | Deep review R2 adversarial M2 |
| resolveStaticGuidance handles only bootstrap steps | guidance.go:56 checks StepGenerate, StepDeploy, StepClose | Deep review R2 adversarial C1 |
| needsRuntimeKnowledge already handles DeployStepPrepare | guidance.go:67 | KB-research |
| Push model wastes tokens, agent may not need injected knowledge | Philosophy discussion with user | Session 2026-03-21 |
| Route should return facts, not recommendations | User: "zcp should be dumb" | Session 2026-03-21 |
| Init instructions must explain container vs local concept | User: "vysvětlit ten koncept toho jak to funguje" | Session 2026-03-21 |
| zerops_discover is the state refresh mechanism | User: "to si říkám že je pokryté v discovery" | Session 2026-03-21 |
