# Bootstrap Flow — Current Implementation (as-coded)

> ⚠️ **SUPERSEDED** — Bootstrap is being rewritten into a 3-route design (Recipe / Classic / Adopt) per `plans/instruction-delivery-rewrite.md` §8. The code behavior described below is scheduled for replacement in Phase 4. Retained as baseline for the audit pipeline (§10 of the plan) to ensure no facts are lost during migration.
>
> **Scenarios under the new architecture**: see `docs/spec-scenarios.md` §2 (Phase: bootstrap-active).

> This document describes what the bootstrap flow **actually does** per the code
> as of 2026-04-14. Every statement has been verified against source. This is
> NOT a design doc — it is a factual record for revalidation before changes.

---

## 1. Entry Points

### 1.1 MCP System Prompt (always visible to agent)

**`server/instructions.go:17-23`** — `baseInstructions` constant:

```
Every code task = one develop workflow. Start before ANY code changes:
  zerops_workflow action="start" workflow="develop"
```

Bootstrap is NOT mentioned in base instructions. It appears only through:
- Router offerings (Section 1.3)
- Orientation "Runtime services needing adoption" section

### 1.2 Router Offerings

**`workflow/router.go:28-96`** — `Route()`:

| Condition | Offering | Priority |
|-----------|----------|----------|
| Incomplete metas exist | bootstrap (resume) | 1 |
| Unmanaged runtimes exist | bootstrap (adopt) | 1 |
| Complete metas exist + no unmanaged | bootstrap (add new) | 3 |
| Nothing at all | bootstrap (create first) | 1 |

Bootstrap is offered at P1 only when services need adoption or nothing exists.
When complete metas exist, bootstrap drops to P3 (behind develop at P1).

### 1.3 Develop Workflow Auto-Adopt

**`tools/workflow_develop.go:48-65`** — `handleDeployStart()`:

When develop starts and no ServiceMetas exist but live services are found,
it calls `adoptUnmanagedServices()` which runs the full bootstrap adoption path:
`BootstrapStart` → `BootstrapCompletePlan` → `BootstrapComplete("provision")` → fast path.

This is a **transparent auto-adopt** — the develop workflow silently bootstraps
existing services without the agent knowing bootstrap occurred.

---

## 2. Session Lifecycle

### 2.1 Session Creation

**`workflow/engine.go:118-142`** — `Engine.Start()`:

1. If `sessionID != ""`, attempts to load existing session
2. If existing session's workflow is completed (`!Active`), auto-resets it
3. If existing session is still active, returns error: `"active session %s, reset first"`
4. If `sessionID != ""` but `LoadSessionByID` fails (file gone), falls through
   - Stale `sessionID` is not explicitly cleared but gets overwritten by `InitSessionAtomic`
5. Creates new session via `InitSessionAtomic` (atomic: prunes dead sessions, creates state file, registers — all under lock)

### 2.2 BootstrapStart

**`workflow/engine.go:196-210`** — `Engine.BootstrapStart()`:

1. Calls `e.Start(projectID, WorkflowBootstrap, intent)`
2. Creates `NewBootstrapState()` — 5 steps, all pending
3. Sets first step (`discover`) to `in_progress`
4. Saves state to session file
5. Returns `BuildResponse()` with first step guidance

### 2.3 Session Exclusivity

Sessions are per-engine. One engine can own one session at a time.
`InitSessionAtomic` does NOT enforce cross-engine exclusivity for bootstrap
at the workflow level — it only enforces hostname locks via `checkHostnameLocks`.

Multiple bootstrap sessions can coexist if they target different hostnames.

### 2.4 PID-Based Recovery

**`workflow/engine.go:31-52`** — `NewEngine()`:

On startup, engine checks for `active_session` file. If found:
- Loads session state
- If PID is dead and not ours → claims session (updates PID)
- If PID is alive or ours → skip (don't steal)
- If session file gone → clears stale reference

---

## 3. The 5 Bootstrap Steps

### 3.1 Step Details

**`workflow/bootstrap_steps.go:18-44`** — `stepDetails`:

| # | Step | Tools | Skip | Checker |
|---|------|-------|------|---------|
| 1 | discover | zerops_discover, zerops_knowledge, zerops_workflow | NO (mandatory) | Plan validation (via `BootstrapCompletePlan`) |
| 2 | provision | zerops_import, zerops_process, zerops_discover | NO (mandatory) | `checkProvision`: services exist, types match, env vars recorded |
| 3 | generate | zerops_knowledge | YES (adoption/managed-only) | `checkGenerate`: zerops.yaml exists, entries match, env refs valid |
| 4 | deploy | zerops_deploy, zerops_discover, zerops_subdomain, zerops_logs, zerops_mount, zerops_verify, zerops_manage | YES (adoption/managed-only) | `checkDeploy`: VerifyAll (HTTP, logs, startup) + subdomain |
| 5 | close | zerops_workflow | YES (adoption/managed-only) | nil (administrative) |

### 3.2 Skip Rules

**`workflow/bootstrap.go:322-334`** — `validateSkip()`:

- `discover` / `provision`: **ALWAYS mandatory** — returns error if skip attempted
- `generate` / `deploy` / `close`: Skippable only when:
  - `plan == nil` (no plan submitted yet), OR
  - `len(plan.Targets) == 0` (managed-only, no runtime targets), OR
  - `plan.IsAllExisting()` (all targets are existing services AND all deps have resolution EXISTS)

If runtime targets exist with `IsExisting=false`, these steps CANNOT be skipped.

---

## 4. Step Completion Mechanics

### 4.1 CompleteStep

**`workflow/bootstrap.go:156-182`** — `BootstrapState.CompleteStep()`:

1. Validates: active, not past last step, step name matches current
2. Validates: attestation >= 10 chars
3. Sets status to `complete`, records attestation + timestamp
4. Advances `CurrentStep++`
5. If more steps remain: next step → `in_progress`
6. If all done: `Active = false`

### 4.2 SkipStep

**`workflow/bootstrap.go:185-212`** — `BootstrapState.SkipStep()`:

Same as CompleteStep but:
- Calls `validateSkip()` first — may reject
- Sets status to `skipped` (not `complete`)
- Records `SkipReason` (not `Attestation`)

### 4.3 Engine-Level Completion

**`workflow/engine.go:216-285`** — `Engine.BootstrapComplete()`:

1. Loads state, validates bootstrap is active
2. Defense-in-depth: non-discover steps require plan to exist
3. Runs checker (if non-nil):
   - If checker fails (`!Passed`): returns response with `CheckResult`, does NOT advance step
   - If checker passes or nil: proceeds
4. Calls `state.Bootstrap.CompleteStep()`
5. **Post-provision special handling**:
   - Writes provision metas (`writeProvisionMetas`)
   - **Adoption fast path**: if `plan.IsAllExisting()`, auto-skips generate/deploy/close
6. If `!state.Bootstrap.Active` (all done):
   - Calls `writeBootstrapOutputs()` → final ServiceMetas
   - Calls `ResetSessionByID()` → deletes session file + unregisters
   - Stores `completedState` for immediate post-completion queries
   - Clears session ID
7. If still active: saves state

### 4.4 Plan Submission (Discover Completion)

**`workflow/engine.go:288-349`** — `Engine.BootstrapCompletePlan()`:

1. Validates: bootstrap active, current step is `discover`
2. Calls `ValidateBootstrapTargets()` — validates hostnames, types, modes against live catalog
3. Calls `checkHostnameLocks()` — rejects if another live session owns a target hostname
4. Builds attestation string from plan targets
5. Completes `discover` step
6. Stores plan in `state.Bootstrap.Plan`
7. Saves state

---

## 5. Step Checkers (Hard Gates)

### 5.1 Provision Checker

**`tools/workflow_checks.go:53-156`** — `checkProvision()`:

For each target in plan:
1. Dev runtime exists and is RUNNING/ACTIVE
2. Runtime type matches plan type
3. Stage runtime (if any) exists in any alive status (NEW, READY_TO_DEPLOY, RUNNING, ACTIVE)
4. Dependencies: exist, RUNNING, type matches
5. Managed (non-storage) deps: have env vars → stores them via `engine.StoreDiscoveredEnvVars()`

All checks must pass for step advancement.

### 5.2 Generate Checker

**`tools/workflow_checks_generate.go:22-74`** — `checkGenerate()`:

For each non-adopted target:
1. zerops.yaml exists (at mount path or project root)
2. Has setup entry matching `RecipeSetupForMode(mode)`
3. Env var references validate against discovered vars

Adopted targets (`IsExisting=true`) are **skipped** — they keep their own config.

### 5.3 Deploy Checker

**`tools/workflow_checks.go:159-239`** — `checkDeploy()`:

1. `ops.VerifyAll()` — HTTP health, logs, startup checks for all plan services
2. Subdomain access enabled for services with ports

### 5.4 Close Checker

**`tools/workflow_checks.go:49-51`** — `buildStepChecker()`:

Returns `nil` for close step. No checker — administrative trigger only.

---

## 6. ServiceMeta Lifecycle

### 6.1 writeProvisionMetas (after provision step)

**`workflow/bootstrap_outputs.go:69-99`** — `Engine.writeProvisionMetas()`:

Writes **incomplete** ServiceMeta for each runtime target:
```go
ServiceMeta{
    Hostname:         metaHostname,
    Mode:             target.Runtime.EffectiveMode(),
    StageHostname:    stageHostname,
    DeployStrategy:   "",          // NOT SET — field absent
    Environment:      string(e.environment),
    BootstrapSession: bootstrapSession, // empty for adopted (IsExisting=true)
    BootstrappedAt:   "",          // NOT SET — marks as incomplete
}
```

Key: `BootstrappedAt == ""` → `IsComplete()` returns `false`.

These metas signal "bootstrap in progress" and enable hostname locking.

### 6.2 writeBootstrapOutputs (after all steps done)

**`workflow/bootstrap_outputs.go:13-62`** — `Engine.writeBootstrapOutputs()`:

Writes **complete** ServiceMeta for each runtime target:
```go
ServiceMeta{
    Hostname:         metaHostname,
    Mode:             mode,
    StageHostname:    stageHostname,
    DeployStrategy:   "",          // ALWAYS EMPTY — deliberate
    StrategyConfirmed: false,      // zero value — not set
    Environment:      string(e.environment),
    BootstrapSession: bootstrapSession, // empty for adopted (IsExisting=true)
    BootstrappedAt:   now,         // SET — marks as complete
}
```

Key observations:
- **`DeployStrategy` is ALWAYS `""`** — bootstrap never sets a strategy
- **`StrategyConfirmed` is `false`** (zero value, not explicitly set)
- `BootstrappedAt` is set → `IsComplete()` returns `true`
- Adopted services get empty `BootstrapSession` to distinguish from fresh bootstrap

### 6.3 Local Environment Hostname Swap

**`workflow/bootstrap_outputs.go:29-33`** (provision) and **`workflow/bootstrap_outputs.go:28-33`** (outputs):

When `environment == EnvLocal` and `stageHostname != ""`:
- `metaHostname = stageHostname` (meta is written for stage)
- `stageHostname = ""` (cleared in meta)

This swaps the perspective: local env writes the stage service meta because
the dev service doesn't physically exist locally.

### 6.4 EffectiveStrategy Backward Compat

**`workflow/service_meta.go:44-49`** — `ServiceMeta.EffectiveStrategy()`:

```go
func (m *ServiceMeta) EffectiveStrategy() string {
    if m.DeployStrategy == StrategyPushDev && !m.StrategyConfirmed {
        return "" // old bootstrap default, not a user choice
    }
    return m.DeployStrategy
}
```

For **new metas** (post-fix): `DeployStrategy == ""` → returns `""`. No-op.
For **old metas** (pre-fix): `DeployStrategy == "push-dev"` + `!StrategyConfirmed` → returns `""`.

This means after bootstrap, `EffectiveStrategy()` ALWAYS returns `""` regardless of meta age.

---

## 7. Adoption Fast Path

### 7.1 In-Bootstrap Adoption

**`workflow/engine.go:254-262`** — inside `BootstrapComplete()` after provision:

```go
if state.Bootstrap.Plan != nil && state.Bootstrap.Plan.IsAllExisting() {
    for _, skip := range []string{StepGenerate, StepDeploy, StepClose} {
        state.Bootstrap.SkipStep(skip, "all targets adopted")
    }
}
```

When ALL targets have `IsExisting=true` AND all deps have `resolution=EXISTS`:
- generate, deploy, close are auto-skipped with reason "all targets adopted"
- Bootstrap completes immediately after provision
- `writeBootstrapOutputs` is called (writes complete metas)

### 7.2 Auto-Adopt in Develop Start

**`tools/workflow_develop.go:231-284`** — `adoptUnmanagedServices()`:

Triggered when develop starts but no metas exist and live services are found:

1. Builds `AdoptCandidate` list from live services (excludes system + self)
2. Calls `InferServicePairing()` to create `BootstrapTarget` list
3. Runs: `BootstrapStart` → `BootstrapCompletePlan` → `BootstrapComplete("provision")`
4. Provision triggers fast path → skips generate/deploy/close
5. Calls `autoMountTargets()` for runtime services
6. Returns true → develop reads freshly-written metas

### 7.3 InferServicePairing

**`workflow/adopt.go:20-98`** — `InferServicePairing()`:

- Separates runtimes from managed services
- Detects dev/stage pairs by `*dev` / `*stage` suffix convention
- Paired services: `BootstrapMode=standard`, stage claimed by dev
- Unpaired services: `BootstrapMode=dev`
- All targets: `IsExisting=true`
- Managed services become `dependencies` with `resolution=EXISTS`

---

## 8. Transition to Develop

### 8.1 Bootstrap Completion Message

**`workflow/bootstrap_guide_assembly.go:77-111`** — `BuildTransitionMessage()`:

Three variants based on plan type:

**Full bootstrap** (new services):
```
Bootstrap complete.
## Services
- {hostname} ({type}, {mode} mode)
## Deploy Model (read before developing)
- Deploy = new container...
- Code on SSHFS mount...
To implement the user's application, start the develop workflow:
  zerops_workflow action="start" workflow="develop"
## What's Next?
A) Develop → zerops_workflow action="start" workflow="develop"
B) Cicd → zerops_workflow action="start" workflow="cicd"
```

**Adoption**:
```
Bootstrap complete. Services adopted — existing code and configuration preserved.
## Services
## Deploy Model
## What's Next?
```

**Managed-only**:
```
Bootstrap complete. Managed services provisioned. No runtime services to deploy.
```

### 8.2 Strategy Status at Develop Start

**`tools/workflow_develop.go:193-225`** — `buildStrategyStatusNote()`:

When develop starts, it appends a note to the response:
- If any metas have `EffectiveStrategy() == ""`:
  ```
  No deploy strategy set for: {hostnames}.
  Proceed with your code changes. Before deploying, discuss with the user:
  - push-dev (SSH self-deploy, quick iterations)
  - push-git (git remote + optional CI/CD)
  - manual (user controls deployments)
  Set via: zerops_workflow action="strategy" strategies={...}
  ```
- If all set: `"Strategy: {name}. Change anytime via action=\"strategy\"."`

### 8.3 Strategy Setting

**`tools/workflow_strategy.go:22-86`** — `handleStrategy()`:

- **No session check** — works with or without active workflow
- Reads existing ServiceMeta (or creates minimal one if none exists)
- Sets `DeployStrategy = strategy` and `StrategyConfirmed = true`
- Returns strategy-specific guidance extracted from `develop.md`
- Next-step hint varies by strategy:
  - manual: `zerops_deploy targetService="..." (manual strategy — deploy directly)`
  - push-git: develop workflow + cicd
  - default: `zerops_workflow action="start" workflow="develop"`

---

## 9. Iteration and Reset

### 9.1 ResetForIteration

**`workflow/bootstrap.go:108-126`** — `BootstrapState.ResetForIteration()`:

Resets `generate` and `deploy` steps to pending. Preserves:
- discover step (plan stays)
- provision step
- close step
- Plan and ServiceMetas

### 9.2 Engine.Iterate

**`workflow/engine.go:159-168`** — `Engine.Iterate()`:

1. Loads state, checks max iterations (default 10)
2. Calls `IterateSession()` → increments counter, resets bootstrap/deploy/recipe steps
3. Returns updated state

---

## 10. Behavioral Analysis

### 10.1 Strategy is ALWAYS empty after bootstrap

Both `writeProvisionMetas` and `writeBootstrapOutputs` write `DeployStrategy: ""`.
There is no code path in bootstrap that sets a strategy. The old `push-dev` default
was removed; `EffectiveStrategy()` backward compat handles old metas.

**Consequence**: After bootstrap completes, `EffectiveStrategy()` returns `""` for all
services. The agent sees "No deploy strategy set" when starting develop workflow.

This is **intentional by design** — strategy is a post-bootstrap user decision,
not an infrastructure default.

### 10.2 Bootstrap never mentions strategy

The bootstrap close step guidance says: "strategy selection happens during deploy
or cicd workflows, not here." The transition message offers develop/cicd workflows
but does not prompt for strategy setting.

Strategy only enters the picture when:
1. Develop starts → `buildStrategyStatusNote()` shows "No deploy strategy set"
2. User explicitly calls `action="strategy"`

### 10.3 Adoption fast path is transparent

When `adoptUnmanagedServices` runs during develop start:
- It creates and completes a full bootstrap session internally
- The bootstrap session is reset/cleaned up before develop state is created
- The agent only sees the develop workflow response
- Service metas appear as if they were always bootstrapped

### 10.4 Hostname locking prevents concurrent bootstrap

`checkHostnameLocks()` prevents two bootstrap sessions from targeting the same hostname:
- Reads ServiceMeta for each target hostname
- If meta is incomplete AND from a different session AND that session's PID is alive → error
- Dead/missing sessions → orphaned meta, safe to overwrite

### 10.5 Session cleanup on completion

When bootstrap completes (all steps done or fast-pathed):
1. `writeBootstrapOutputs()` — final metas written
2. `ResetSessionByID()` — session file deleted + unregistered
3. `completedState` stored in engine memory
4. Session ID cleared

The engine holds no session after bootstrap completes. This allows
an immediate `develop` workflow start without manual reset.

---

## 11. Bugs and Issues

### 11.1 Close step has no checker — no enforcement

The close step has a `nil` checker. It exists only as an administrative trigger
to call `writeBootstrapOutputs()`. The step can be completed with any attestation
(>10 chars) and no verification occurs.

**Impact**: Low. The close step is the final step — its only purpose is to trigger
output writing. The real enforcement is in deploy checker (step 4).

### 11.2 Adoption creates minimal meta for unknown hostnames

**`tools/workflow_strategy.go:50-53`**:
```go
if meta == nil {
    meta = &workflow.ServiceMeta{Hostname: hostname}
}
```

If `handleStrategy` is called for a hostname with no existing meta, it creates
a minimal meta with only `Hostname` set. This meta has:
- `Mode: ""`, `BootstrappedAt: ""` → `IsComplete()` returns false
- No environment, no bootstrap session

**Impact**: This meta would be treated as "incomplete bootstrap" by hostname locking
and develop flow filtering. It would be pruned by `PruneServiceMetas` if the hostname
doesn't exist on the platform.

### 11.3 No strategy prompt in transition

When bootstrap completes, the transition message does NOT prompt the user to set
a deploy strategy. It goes straight to "start develop workflow." The user discovers
the strategy requirement only when develop starts and shows "No deploy strategy set."

**Impact**: Medium. The agent proceeds to develop without strategy context, then gets
interrupted with strategy information at develop start. This is a UX gap, not a bug —
the code works as written.

### 11.4 Abandoned bootstrap leaves hostname in degraded limbo

If bootstrap is started, provision completes (metas written), but then bootstrap is
reset/abandoned before completion, the incomplete metas remain on disk. `Engine.Reset()`
deletes session files but does NOT delete ServiceMeta files.

These orphaned incomplete metas:
- Have `BootstrappedAt == ""` (incomplete) → `IsComplete()` returns false
- Are filtered out by develop flow (`IsComplete()` check at line 73)
- Are NOT pruned by `PruneServiceMetas` (prunes by liveness, not completeness)
- Prevent auto-adopt: `len(metas) >= 1` makes `adoptUnmanagedServices` unreachable (line 48)

Result: develop fails with "No deployable services — [hostname] still bootstrapping (incomplete)"
with suggestion "Finish bootstrap for those services first" — but there's no bootstrap to finish.

**Recovery**: Start a fresh bootstrap — `checkHostnameLocks` correctly identifies the orphaned
meta (session gone from registry) and allows overwriting. But the error message doesn't
suggest this path.

**Impact**: MEDIUM-HIGH. Not permanent, but creates a confusing degraded state.
**Root cause**: `Reset()` doesn't clean up provision-step artifacts.
