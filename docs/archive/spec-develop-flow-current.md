# Develop Flow — Current Implementation (as-coded)

> This document describes what the develop flow **actually does** per the code
> as of 2026-04-14. Every statement has been verified against source. This is
> NOT a design doc — it is a factual record for revalidation before changes.

---

## 1. Entry Points

### 1.1 MCP System Prompt (always visible to agent)

**`server/instructions.go:17-23`** — `baseInstructions` constant:

```
Every code task = one develop workflow. Start before ANY code changes:
  zerops_workflow action="start" workflow="develop"
The workflow refreshes service state, mounts, and guidance.
After deploy, immediately start a new workflow for the next task.
```

This text is injected into **every** MCP instructions response (line 50).
No strategy-awareness — always says "every code task = one develop workflow."

### 1.2 Orientation (per-service detail in system prompt)

**`server/instructions_orientation.go:186-230`** — `writeStrategySection()`:

| Strategy | Text | Deploy hint |
|----------|------|-------------|
| not set | "No strategy set yet. Choose one:" | `action="strategy"` |
| manual | "You control when to deploy." | **"Call zerops_deploy directly."** |
| push-dev | "Deploy via guided workflow:" | `action="start" workflow="develop"` |
| push-git | "Deploy via git push:" | `action="start" workflow="develop"` + cicd |

Manual orientation explicitly says **"Call zerops_deploy directly"** — does NOT mention develop workflow.

### 1.3 Router

**`workflow/router.go:134-165`** — `strategyOfferings()`:

Develop workflow is **ALWAYS offered** at Priority 1 regardless of strategy (line 143-145).
Manual strategy does NOT suppress the develop offering.
Push-git additionally offers cicd (P2) and export (P3).

### 1.4 Workflow Hint (active session in system prompt)

**`server/instructions.go:114-146`** — `buildSessionHint()`:

When a develop session exists:
```
Active workflow: develop | intent: "..." | session: abc123 (step 1/3: prepare)
```

---

## 2. Session Lifecycle

### 2.1 Start

**`workflow/engine.go:118-142`** — `Engine.Start()`:

1. If engine already owns a session (`e.sessionID != ""`):
   - Load existing session state
   - If **completed** (`Deploy != nil && !Deploy.Active`): auto-reset + clear → proceed
   - If **still active** (`Deploy.Active == true`): **error: "start: active session %s, reset first"**
2. Create new session via `InitSessionAtomic` → new SessionID, registered in registry
3. Set `e.sessionID`, persist to `active_session` file

**Edge case:** If `LoadSessionByID` fails (session file missing/corrupted), the code
silently falls through (line 120 `err == nil` is false → skip block). The stale
`e.sessionID` is NOT cleared, but `InitSessionAtomic` (line 135) creates a new session
and `e.setSessionID` (line 139) overwrites the stale reference. Net effect: orphaned
session file is silently abandoned and a new session starts.

**Consequence:** An incomplete develop workflow **blocks** starting any new workflow.
Only escape: `action="reset"` (explicit) or completing all steps (auto-reset).

### 2.2 DeployStart

**`workflow/engine_deploy.go:11-25`** — `Engine.DeployStart()`:

1. Call `e.Start(projectID, "develop", intent)` — creates base session
2. Create `NewDeployState(targets, mode)` — 3 steps, all pending
3. Mark first step (`prepare`) as `in_progress`
4. Save state, return response with prepare guidance

### 2.3 handleDeployStart (tool handler)

**`tools/workflow_develop.go:14-108`**:

1. Read ServiceMetas from disk (lines 17-18)
2. Prune stale metas against live API services (lines 27-45)
3. If no metas: auto-adopt via bootstrap fast path (lines 48-64)
4. Filter to complete runtime metas only (lines 69-89)
5. Build targets + mode from metas (line 91)
6. Enrich with runtime types from API data (line 94)
7. Call `engine.DeployStart(...)` (line 96)
8. **Append strategy status note** — informational, not blocking (line 105)

Strategy status note (`buildStrategyStatusNote`, lines 191-225):
- Unset: "No deploy strategy set for: [hostnames]. Proceed with your code changes. Before deploying, discuss with the user..."
- Set single: "Strategy: [name]. Change anytime via action='strategy'."
- Set multiple: "Strategies: [names]. Change anytime via action='strategy'."

---

## 3. State Machine

### 3.1 DeployState Structure

**`workflow/deploy.go:28-35`**:

```go
type DeployState struct {
    Active      bool           // false when all steps done
    CurrentStep int            // 0=prepare, 1=execute, 2=verify
    Steps       []DeployStep   // exactly 3 steps
    Targets     []DeployTarget // per-service tracking
    Mode        string         // "standard"|"dev"|"simple"
}
```

### 3.2 Steps

**`workflow/deploy.go:94-102`** — `deployStepDetails`:

| Index | Name | Tools | Checker |
|-------|------|-------|---------|
| 0 | prepare | zerops_discover, zerops_knowledge | `checkDeployPrepare` (hard gate) |
| 1 | execute | zerops_deploy, zerops_subdomain, zerops_logs, zerops_verify, zerops_manage | `checkDeployResult` (hard gate) |
| 2 | verify | zerops_verify, zerops_discover | **nil** (no checker, always passes) |

### 3.3 Initial State

**`workflow/deploy.go:104-117`** — `NewDeployState()`:

```
Active=true, CurrentStep=0
Steps: [
  {Name:"prepare", Status:"pending"},
  {Name:"execute", Status:"pending"},
  {Name:"verify",  Status:"pending"}
]
```

First step is immediately marked `in_progress` by `DeployStart` (engine_deploy.go:18).

### 3.4 CompleteStep

**`workflow/deploy.go:182-207`**:

Preconditions (all must pass):
1. `Active == true` — else error "not active"
2. `CurrentStep < len(Steps)` — else error "all steps done"
3. `Steps[CurrentStep].Name == name` — else error "expected %q, got %q"
4. `len(attestation) >= 10` — else error "attestation too short"

Actions:
1. `Steps[CurrentStep].Status = "complete"`
2. Store attestation + CompletedAt timestamp
3. `CurrentStep++`
4. If more steps: mark next step `in_progress`
5. **If no more steps: `Active = false`**

### 3.5 SkipStep

**`workflow/deploy.go:209-236`**:

Preconditions (same as CompleteStep, plus):
- `name != "prepare"` — prepare is **mandatory, cannot be skipped** (error: "prepare is mandatory and cannot be skipped")

Skippable steps: **execute, verify** only.

Actions: Same as CompleteStep but Status = "skipped", Attestation = reason.

### 3.6 Session Cleanup on Completion/Skip

**`workflow/engine_deploy.go:59-65`** (DeployComplete) and **lines 92-98** (DeploySkip):

When `Active` becomes false:
1. `ResetSessionByID()` — delete session file + unregister from registry
2. `e.completedState = state` — preserve for final response generation
3. `e.clearSessionID()` — clear in-memory ref + delete `active_session` file

### 3.7 Completion Message

**`workflow/deploy.go:302-306`** — when `CurrentStep >= len(Steps)`:

```
Deploy complete.

Start a new develop workflow for the next task:
  zerops_workflow action="start" workflow="develop"

Each workflow refreshes service state and provides current guidance.
```

### 3.8 Iterate (Retry)

**`workflow/engine.go:159-168`** → `IterateSession` → `DeployState.ResetForIteration`:

**`workflow/deploy.go:238-260`** — `ResetForIteration()`:
- Resets **execute + verify** steps to pending (clears attestation, completedAt)
- Leaves **prepare** untouched (preserves validated state)
- Resets all targets to "pending"
- Sets `CurrentStep` to first reset step (execute)
- Sets `Active = true`

Max iterations: **10** (session.go:14, `defaultMaxIterations`), shared across all workflow types.
Override: env var `ZCP_MAX_ITERATIONS`.

---

## 4. Step Checkers

### 4.1 Prepare Checker

**`tools/workflow_checks_deploy.go:30-105`** — `checkDeployPrepare()`:

Validates:
1. **zerops.yaml exists and parses** — searched at mount path first, then project root
2. **Dev/prod env divergence** — flags bit-identical envVariables in dev vs prod setups
3. **Setup entries match targets** — tries role name ("dev"/"prod"), then hostname
4. **deployFiles paths exist** — skipped for stage targets (cross-deployed)
5. **Env var references valid** — re-discovers from API, validates `${hostname_varName}` syntax

If any check fails: `Passed=false` → **step completion blocked**, agent gets failure details.

### 4.2 Execute Checker

**`tools/workflow_checks_deploy.go:107-179`** — `checkDeployResult()`:

Validates:
1. **Service status** — RUNNING/ACTIVE = pass, READY_TO_DEPLOY = fail, other = fail
2. **Subdomain access** — for services with ports, flags if not enabled

If any check fails: `Passed=false` → **step completion blocked**.

### 4.3 Verify Checker

**`tools/workflow_checks_deploy.go:26-28`**:

```go
// deploy verify step has nil checker (informational, not blocking).
return nil
```

**No validation.** Agent calls `action="complete" step="verify"` with any 10+ char attestation → always succeeds.

### 4.4 Checker Enforcement Mechanism

**`workflow/engine_deploy.go:38-50`**:

```go
if checker != nil {
    result, checkErr := checker(ctx, state.Deploy)
    if result != nil && !result.Passed {
        resp.CheckResult = result
        resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", step, result.Summary)
        return resp, nil  // BLOCKS — CompleteStep NOT called
    }
}
```

When checker fails: `CompleteStep()` is never called → step stays at current position → session remains active.

---

## 5. Guidance Delivery

### 5.1 Dispatch

**`workflow/deploy.go:314-324`** — `buildGuide()`:

```go
switch step {
case DeployStepPrepare: return buildPrepareGuide(d, env, stateDir)
case DeployStepExecute: return buildDeployGuide(d, iteration, env, stateDir)
case DeployStepVerify:  return buildVerifyGuide(d)
}
```

### 5.2 Prepare Guidance

**`workflow/deploy_guidance.go:48-110`** — `buildPrepareGuide()`:

Writes 7 sections in order:

| # | Section | Strategy-aware | Env-aware | Mode-aware |
|---|---------|---------------|-----------|------------|
| 1 | Header: "implement what the user wants, then deploy and verify" | no | no | no |
| 2 | Target summary + "Mode: X \| Strategy: Y" | **YES** (reads via dominantStrategy) | no | no |
| 3 | Checklist (zerops.yaml requirements) | no | no | **YES** |
| 4 | Development workflow | **YES** (switch on strategy) | **YES** | **YES** |
| 5 | Platform rules | no | **YES** | no |
| 6 | Strategy note | **YES** | no | no |
| 7 | Knowledge map | no | no | no |

### 5.3 Prepare — Development Workflow Section (key strategy behavior)

**`workflow/deploy_guidance.go:215-244`** — `writeDevelopmentWorkflow()`:

| Strategy | Environment | Output |
|----------|-------------|--------|
| any | EnvLocal | "Edit code locally. Test locally, then deploy when ready." |
| push-git | EnvContainer | "Edit on mount. 1. Commit 2. Push via zerops_deploy strategy=git-push 3. CI/CD" |
| **manual** | **EnvContainer** | **"Edit code on SSHFS mount. Tell user when ready. User controls deployment timing."** |
| (empty) | EnvContainer | "Implement changes. Set deploy strategy before deploying: action=strategy" |
| push-dev | EnvContainer | writePushDevWorkflow() — mode-specific iteration cycle (lines 246-275) |

### 5.4 Execute Guidance

**`workflow/deploy_guidance.go:112-186`** — `buildDeployGuide()`:

| # | Section | Strategy-aware |
|---|---------|---------------|
| 1 | Header: "Execute — {mode} mode, {strategy}" | **YES** (label only) |
| 2 | Iteration escalation (if iteration > 0) | no |
| 3 | **Workflow steps (deploy commands)** | **NO** |
| 4 | Key facts | no |
| 5 | Code-only changes shortcut | no |
| 6 | Diagnostic pointers | no |

**Section 3 is critical:** `buildDeployGuide()` always writes deploy commands regardless of strategy.

For EnvContainer, switches on **mode** (not strategy):
- Standard: "1. Deploy to dev 2. Start server 3. Verify dev 4. Deploy to stage 5. Verify stage"
- Dev: "1. Deploy 2. Start server 3. Verify"
- Simple: "1. Deploy 2. Verify"

For EnvLocal: "1. Deploy 2. Verify" per target.

**Manual strategy gets the same deploy commands as push-dev in execute step.**

### 5.5 Verify Guidance

**`workflow/deploy_verify_guidance.go:60-80`** — `buildVerifyGuide()`:

| Component | Strategy-aware |
|-----------|---------------|
| Base (from develop.md "deploy-verify" section) | no |
| Per-service (web → agent-browser, non-web → zerops_verify) | no |
| Verdict protocol | no |

**Completely strategy-blind.**

### 5.6 Guidance Strategy-Awareness Summary

```
                    Strategy consulted?
                    ───────────────────
Prepare step:       YES (writeDevelopmentWorkflow, writeStrategyNote)
Execute step:       NO  (only mode label in header)
Verify step:        NO
```

---

## 6. Strategy Interaction

### 6.1 Strategy Setting

**`tools/workflow_strategy.go:22-86`** — `handleStrategy()`:

1. Validates strategy values (push-dev, push-git, manual)
2. Writes to ServiceMeta per hostname (`StrategyConfirmed = true`)
3. Extracts guidance from develop.md sections
4. Returns next-step hint:

| Strategy | Next-step hint |
|----------|---------------|
| default/push-dev | `zerops_workflow action="start" workflow="develop"` |
| **manual** | **`zerops_deploy targetService="..." (manual strategy — deploy directly)`** |
| push-git | `zerops_workflow action="start" workflow="develop"` + cicd |

**Does NOT check for active workflow session.** Strategy updates are stateless.
Can be called during an active develop workflow without affecting session state.

### 6.2 EffectiveStrategy

**`workflow/service_meta.go:44-49`**:

Old metas with `DeployStrategy="push-dev"` + `StrategyConfirmed=false` return `""` (backward compat).
Only `StrategyConfirmed=true` metas return their strategy.

### 6.3 Strategy in Deploy Targets

**`workflow/deploy.go:138-154`** — `BuildDeployTargets()`:

Each target gets `Strategy: m.EffectiveStrategy()` copied from its meta.
This value is stored in the session state but **never used** by the step logic or checkers.

---

## 7. zerops_deploy Tool Interaction

### 7.1 Gate

**`tools/deploy_local.go:58`** and **`tools/deploy_ssh.go:76`**:

Both call `requireWorkflow(engine)` — blocks if no active workflow session.
Error: "No active workflow session. This tool requires a workflow session."

### 7.2 State Updates

zerops_deploy does **NOT**:
- Call `engine.DeployComplete()`
- Update workflow step status
- Close or advance the workflow session

The deploy tool is **self-contained**. It returns a `DeployResult` (DEPLOYED/BUILD_FAILED)
but the agent must **manually** call `action="complete" step="execute"` to advance the workflow.

---

## 8. Transition Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                    DEVELOP WORKFLOW SESSION                        │
│                                                                     │
│  START                                                              │
│    ↓                                                                │
│  ┌──────────────────────────────────────────────────────┐          │
│  │ STEP 0: PREPARE (mandatory, cannot be skipped)       │          │
│  │                                                      │          │
│  │ Agent: codes, validates zerops.yaml                  │          │
│  │ Guidance: strategy-aware development workflow        │          │
│  │ Checker: zerops.yaml + entries + env refs            │          │
│  │                                                      │          │
│  │ complete → checker MUST pass → advance               │          │
│  │ skip → ERROR (prepare is mandatory)                  │          │
│  └──────────────┬───────────────────────────────────────┘          │
│                 ↓                                                    │
│  ┌──────────────────────────────────────────────────────┐          │
│  │ STEP 1: EXECUTE (skippable)                          │          │
│  │                                                      │          │
│  │ Agent: deploys via zerops_deploy                     │          │
│  │ Guidance: strategy-BLIND deploy commands             │          │
│  │ Checker: service status + subdomain                  │          │
│  │                                                      │          │
│  │ complete → checker MUST pass → advance               │          │
│  │ skip → advance (allowed)                             │          │
│  └──────────────┬───────────────────────────────────────┘          │
│                 ↓                                                    │
│  ┌──────────────────────────────────────────────────────┐          │
│  │ STEP 2: VERIFY (skippable)                           │          │
│  │                                                      │          │
│  │ Agent: health check via zerops_verify                │          │
│  │ Guidance: strategy-BLIND verify protocol             │          │
│  │ Checker: nil (always passes)                         │          │
│  │                                                      │          │
│  │ complete → always succeeds → Active=false            │          │
│  │ skip → Active=false                                  │          │
│  └──────────────┬───────────────────────────────────────┘          │
│                 ↓                                                    │
│  SESSION CLEANUP: file deleted, registry cleared                   │
│  RESPONSE: "Deploy complete. Start new workflow."                  │
│                                                                     │
│  ── ITERATE (at any point after prepare) ──                        │
│  Resets execute+verify to pending, preserves prepare.              │
│  Increments iteration counter (max 10).                            │
│                                                                     │
│  ── RESET (at any point) ──                                        │
│  Deletes session entirely. No trace.                               │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 9. Identified Behavioral Gaps

### 9.1 Manual Strategy Deadlock

**Sequence:**
1. Agent starts develop workflow → session created (Active=true, step=prepare)
2. Prepare guidance says: "Edit code on mount. Tell user when ready. **User controls deployment timing.**"
3. Agent codes, completes prepare → checker validates zerops.yaml → advance to execute
4. Execute guidance says: "1. Deploy to dev: zerops_deploy..." (same as push-dev)
5. **Contradiction:** Prepare said "user controls timing" but execute says "deploy now"
6. If agent waits for user → session stuck at execute → blocks all future workflows

**Root cause:** Execute step guidance (`buildDeployGuide`) is strategy-blind.
Manual strategy only affects prepare step guidance.

### 9.2 No Auto-Close for Manual

There is no mechanism to:
- Auto-skip execute+verify when strategy is manual
- Auto-close the workflow when strategy is manual
- Detect manual strategy and adjust step behavior

The 3-step structure is identical for all strategies.

### 9.3 No Deploy Enforcement for Non-Manual

For push-dev/push-git, nothing **forces** the agent to:
- Complete the prepare step after coding
- Actually deploy during the execute step
- Advance through the workflow at all

The only enforcement is:
- Guidance text (soft prompt, can be ignored by LLM)
- Session blocking (prevents new workflow until current completes)
- Deploy tool gate (zerops_deploy requires active session, but doesn't advance it)

### 9.4 zerops_deploy is Session-Decoupled

zerops_deploy requires a workflow session but does not advance it.
After successful deploy, the agent must separately call `action="complete" step="execute"`.
These are two independent actions with no coupling.

### 9.5 handleStrategy Conflict During Active Workflow

If agent calls `action="strategy"` during active develop workflow and sets manual:
- ServiceMeta updated on disk
- Response says: "zerops_deploy targetService=... (manual strategy — deploy directly)"
- Workflow session is still active with execute step waiting
- Agent is told to bypass workflow, but session remains stuck

### 9.6 Iteration Guidance vs Hard Limit Mismatch

Deploy guidance says "after 3 failed iterations: stop and report to user" (deploy_guidance.go:407-410).
Hard limit is 10 iterations (session.go:14).
The 3-iteration threshold is **guidance only** — the system allows up to 10.

---

## 10. File Reference

| File | Responsibility |
|------|---------------|
| `workflow/deploy.go` | DeployState, steps, CompleteStep, SkipStep, ResetForIteration, BuildResponse |
| `workflow/engine_deploy.go` | DeployStart, DeployComplete, DeploySkip, DeployStatus |
| `workflow/engine.go` | Start (exclusivity), Reset, Iterate |
| `workflow/deploy_guidance.go` | buildPrepareGuide, buildDeployGuide, writeDevelopmentWorkflow |
| `workflow/deploy_verify_guidance.go` | buildVerifyGuide |
| `workflow/session.go` | IterateSession, maxIterations, ResetSessionByID |
| `workflow/service_meta.go` | EffectiveStrategy, strategy constants |
| `workflow/router.go` | strategyOfferings (always offers develop) |
| `tools/workflow.go` | handleWorkflowAction dispatch, detectActiveWorkflow |
| `tools/workflow_develop.go` | handleDeployStart, handleDeployComplete, handleDeploySkip |
| `tools/workflow_strategy.go` | handleStrategy (stateless, no session check) |
| `tools/workflow_checks_deploy.go` | checkDeployPrepare, checkDeployResult |
| `server/instructions.go` | baseInstructions, buildWorkflowHint |
| `server/instructions_orientation.go` | writeStrategySection |
| `content/workflows/develop.md` | Embedded guidance sections |
