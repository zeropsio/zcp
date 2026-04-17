# Session Design Audit — Develop Workflow

> Standalone reference for redesign work. Every claim is backed by file:line references
> verified against the codebase as of 2026-04-14. This document is self-contained — the
> receiving agent does not need prior conversation context.

---

## 1. Executive Summary

The develop workflow's `DeployState` duplicates data from `ServiceMeta` (disk) and the
Zerops API (live) into a frozen session snapshot. This creates:

- **Dead fields** that are never read (`Strategy`) or never updated (`Status`)
- **Stale data bugs** when the source of truth changes mid-session
- **Unnecessary complexity** (enrichment functions, target-building pipelines)
- **Strategy contradictions** between steps that read fresh vs. stale data

Bootstrap's session design does NOT have this problem — its `Plan` is genuine new state
that doesn't exist elsewhere. The issue is specific to the develop/deploy session.

---

## 2. Architecture Context

### 2.1 Data Stores

| Store | Location | Persistence | What it holds |
|-------|----------|-------------|---------------|
| **ServiceMeta** | `.zcp/state/services/{hostname}.json` | Permanent (survives sessions) | Mode, strategy, stage pairing, environment, bootstrap state |
| **Session state** | `.zcp/state/sessions/{sessionID}.json` | Per-session (deleted on complete/reset) | Steps, attestations, targets, mode |
| **API** | Zerops platform | Live | Service status, type, ports, env vars, subdomains |

### 2.2 Session State Structure

**`workflow/session.go`** — `WorkflowState`:
```go
type WorkflowState struct {
    Version   string          // "1"
    SessionID string
    PID       int
    ProjectID string
    Workflow  string          // "bootstrap", "develop", "recipe"
    Iteration int
    Intent    string
    CreatedAt string
    UpdatedAt string
    Bootstrap *BootstrapState // nil for develop
    Deploy    *DeployState    // nil for bootstrap
    Recipe    *RecipeState    // nil for develop/bootstrap
}
```

**`workflow/deploy.go:29-35`** — `DeployState`:
```go
type DeployState struct {
    Active      bool           `json:"active"`
    CurrentStep int            `json:"currentStep"`
    Steps       []DeployStep   `json:"steps"`
    Targets     []DeployTarget `json:"targets"`
    Mode        string         `json:"mode"`
}
```

**`workflow/deploy.go:46-53`** — `DeployTarget`:
```go
type DeployTarget struct {
    Hostname    string `json:"hostname"`
    RuntimeType string `json:"runtimeType,omitempty"`
    Role        string `json:"role"`
    Status      string `json:"status"`
    Strategy    string `json:"strategy,omitempty"`
    HTTPSupport bool   `json:"httpSupport,omitempty"`
}
```

---

## 3. Field-by-Field Audit of DeployTarget

### 3.1 `Strategy` — DEAD FIELD (never read in production)

**Written at**: `workflow/deploy.go:143` — `BuildDeployTargets()` copies from `ServiceMeta.EffectiveStrategy()`:
```go
targets = append(targets, DeployTarget{
    ...
    Strategy: m.EffectiveStrategy(),
})
```

**Read in production**: **NOWHERE**. The guidance system reads strategy from disk:

```go
// workflow/deploy_guidance.go:24-33
func readCurrentStrategy(stateDir, hostname string) string {
    if stateDir == "" {
        return ""
    }
    meta, err := ReadServiceMeta(stateDir, hostname)
    if err != nil || meta == nil {
        return ""
    }
    return meta.EffectiveStrategy()
}
```

This function is called by `dominantStrategy()` (deploy_guidance.go:36-46), which is
the sole strategy reader for ALL three guidance functions:
- `buildPrepareGuide` (line 60): `strategy := dominantStrategy(stateDir, state.Targets)` — uses targets only for hostname list
- `buildDeployGuide` (line 116): same pattern
- `buildVerifyGuide` (line 63): does NOT read strategy at all

**Read in tests only**: `workflow/deploy_test.go:263-267` — test assertions check the value exists on the target.

**Grep proof**: `grep -rn '\.Strategy' internal/workflow/deploy*.go` shows NO production reads of `target.Strategy` or `t.Strategy`.

**Consequence**: `DeployTarget.Strategy` is serialized to session JSON, persisted to disk, loaded back on every status/complete call, and never used. Pure dead weight.

### 3.2 `Status` — DEAD FIELD (never updated after initialization)

**Written at**: `workflow/deploy.go:140` — set to `deployTargetPending` ("pending") at creation.

**Updated at**: `workflow/deploy.go:252-253` — `ResetForIteration()` resets to "pending":
```go
for i := range d.Targets {
    d.Targets[i].Status = deployTargetPending
}
```

**NEVER updated anywhere else**. Not by `CompleteStep()` (lines 182-207), not by `SkipStep()` (lines 210-236), not by any engine method.

**Read at**: `workflow/deploy.go:274-278` — `BuildResponse()` copies to `DeployTargetOut`:
```go
targetOuts := make([]DeployTargetOut, len(d.Targets))
for i, t := range d.Targets {
    targetOuts[i] = DeployTargetOut{
        Hostname: t.Hostname,
        Role:     t.Role,
        Status:   t.Status, // ALWAYS "pending"
    }
}
```

**Consequence**: Every response sent to the agent includes `"status": "pending"` for every target, for the entire session lifetime. This field communicates zero information.

### 3.3 `RuntimeType` — CACHED API DATA (useful but could be fetched on demand)

**Written at**: `tools/workflow_develop.go:173-177` — `enrichTargetRuntimeTypes()` fetches from API response:
```go
for i := range targets {
    if rt, ok := typeMap[targets[i].Hostname]; ok {
        targets[i].RuntimeType = rt
    }
    targets[i].HTTPSupport = httpMap[targets[i].Hostname]
}
```

**Read at**:
- `deploy_guidance.go:197-205` — `buildKnowledgeMap()`: builds runtime-specific knowledge pointers
- `deploy_verify_guidance.go:84,88,90` — `writeDirectVerify()`, `writeAgentVerify()`: shows runtime type in verify prompt

**Assessment**: Legitimate use — needed for personalized guidance. But the data comes from the API
service list, which is already fetched in `handleDeployStart` (line 29) and could be fetched
at guidance time instead of snapshotted.

### 3.4 `HTTPSupport` — CACHED API DATA (useful but could be fetched on demand)

**Written at**: Same as RuntimeType — `enrichTargetRuntimeTypes()`.

**Read at**: `deploy_verify_guidance.go:71` — determines if service gets agent-browser verification:
```go
for _, t := range d.Targets {
    if t.HTTPSupport {
        writeAgentVerify(&sb, t)
    } else {
        writeDirectVerify(&sb, t)
    }
}
```

**Assessment**: Same as RuntimeType — needed but could be fetched on demand from API.

### 3.5 `Role` — DERIVED FROM ServiceMeta (could be computed on demand)

**Written at**: `workflow/deploy.go:138` via `deployRoleFromMode()`:
```go
func deployRoleFromMode(mode, _, stageHostname string) string {
    switch mode {
    case PlanModeSimple:
        return DeployRoleSimple
    case PlanModeDev:
        return DeployRoleDev
    default:
        if stageHostname != "" {
            return DeployRoleDev
        }
        return DeployRoleSimple
    }
}
```

**Read at**: Extensively in guidance — 15+ reads across `deploy_guidance.go` for routing
(dev/stage/simple workflows), pair finding, target summaries. Also in `BuildResponse` for display.

**Assessment**: Heavily used but fully derivable from `ServiceMeta.Mode` + `ServiceMeta.StageHostname`.
The derivation is a pure function (no API call needed).

### 3.6 `Hostname` — FROM ServiceMeta (needed, but list could be stored directly)

**Read everywhere** — every guidance function, every checker, every response.

**Assessment**: Essential, but the session doesn't need a full `DeployTarget` struct to store it.
A `[]string` of hostnames would suffice; the rest is derivable.

### 3.7 `Mode` (on DeployState, not DeployTarget) — DERIVED FROM ServiceMeta

**Written at**: `tools/workflow_develop.go:91` via `BuildDeployTargets()` return value.

**Read at**: `deploy_guidance.go` — 9 reads for mode-specific checklist items and workflow routing.

**Assessment**: Derivable from ServiceMeta. The mode is per-meta, aggregated at session start.
Could be re-derived from the same metas when needed.

---

## 4. The Enrichment Pipeline (Unnecessary Complexity)

The current flow from ServiceMeta to session target:

```
ServiceMeta (disk)
  ↓ ListServiceMetas()
  ↓ Filter IsComplete() + has Mode/StageHostname
  ↓ BuildDeployTargets() — copies hostname, derives role, copies strategy, derives mode
  ↓ enrichTargetRuntimeTypes() — fetches API, sets RuntimeType + HTTPSupport
  ↓ NewDeployState(targets, mode) — wraps in session struct
  ↓ saveSessionState() — serializes to JSON on disk
  ↓
Session JSON (disk) ← targets are now a frozen snapshot
  ↓ loadState()
  ↓ BuildResponse() / buildGuide()
  ↓
Agent gets response with stale target data
```

Functions that exist SOLELY to populate the snapshot:
- `BuildDeployTargets()` — `workflow/deploy.go:121-157` (37 lines)
- `deployRoleFromMode()` — `workflow/deploy.go:159-171` (13 lines)
- `enrichTargetRuntimeTypes()` — `tools/workflow_develop.go:160-178` (19 lines)
- `NewDeployState()` — `workflow/deploy.go:105-117` (13 lines)

These ~82 lines of code exist to transform ServiceMeta+API data into a session snapshot
that is then partially ignored (Strategy, Status) and partially bypassed (guidance reads
strategy from disk anyway).

---

## 5. Bug: Strategy Stale Data Vector

### 5.1 The Contradiction Within Guidance

`buildPrepareGuide` (deploy_guidance.go:60) reads strategy **fresh from disk**:
```go
strategy := dominantStrategy(stateDir, state.Targets)
// → calls readCurrentStrategy() → reads ServiceMeta from disk
```

`buildDeployGuide` (deploy_guidance.go:116) does the **same**:
```go
strategy := dominantStrategy(stateDir, state.Targets)
```

So guidance already uses live strategy. But `buildStrategyStatusNote` in
`tools/workflow_develop.go:193-225` — appended at session start — reads from metas
at start time only. And the session's `DeployTarget.Strategy` is frozen at start.

The response to the agent contains:
1. **Live strategy** (in guidance text) — from `readCurrentStrategy()`
2. **Frozen strategy** (in target JSON) — from `DeployTarget.Strategy`
3. **Start-time strategy** (in strategy status note) — from `buildStrategyStatusNote()`

Three potentially different values for the same concept, from three different read points.

### 5.2 Mid-Session Strategy Change Scenario

1. Develop starts with empty strategy → target.Strategy = "", guidance says "not set"
2. Agent calls `action="strategy"` to set push-dev → ServiceMeta updated on disk
3. Agent calls `action="status"` → guidance reads fresh (shows push-dev), target JSON shows ""
4. Agent calls `action="complete" step="prepare"` → checker uses `state.Targets` for hostname iteration but re-discovers env vars from API

The guidance text and the JSON data in the same response tell different stories.
This is not a theoretical concern — it's the normal flow when strategy is set during develop.

---

## 6. Bug: Abandoned Bootstrap Leaves Hostname in Limbo

### 6.1 Root Cause

`writeProvisionMetas()` (bootstrap_outputs.go:69-99) writes incomplete ServiceMeta files
(no `BootstrappedAt`) after the provision step. `Engine.Reset()` (engine.go:145-156)
deletes the session file and unregisters from the registry but does NOT delete ServiceMeta files.

### 6.2 Exact Failure Sequence

1. `BootstrapStart()` creates session S1
2. `BootstrapCompletePlan()` submits plan with target hostname H
3. `BootstrapComplete("provision")` → `writeProvisionMetas()` writes `services/H.json` with `BootstrappedAt: ""`
4. User calls `Reset()` → session S1 file deleted, unregistered. `services/H.json` persists.
5. User starts develop (`handleDeployStart`):
   - `PruneServiceMetas()` (service_meta.go:136-152): prunes by **liveness**, not completeness. H is live → meta survives.
   - `ListServiceMetas()` returns incomplete meta for H → `len(metas) >= 1`
   - `adoptUnmanagedServices()` (workflow_develop.go:48-50): only called when `len(metas) == 0` → **SKIPPED**
   - `IsComplete()` filter (workflow_develop.go:72-73): `BootstrappedAt == ""` → `false` → skipped, added to `skippedIncomplete`
   - `len(runtimeMetas) == 0` → error: `"No deployable services — [H] still bootstrapping (incomplete)"`
   - Suggestion: `"Finish bootstrap for those services first, then start deploy"`

### 6.3 Recovery Path

Starting a **new** bootstrap for the same hostname DOES work:
- `checkHostnameLocks()` (engine.go:425-458): looks up orphaned meta's `BootstrapSession` in registry → not found → `inRegistry=false` → lock passes
- New bootstrap proceeds, eventually writes complete meta

But the error message says "Finish bootstrap" (implying resume), not "Start a new bootstrap."
The old session is gone — there's nothing to finish.

### 6.4 Relevance to Session Design

This bug exists because `Reset()` treats session files and ServiceMeta files as independent.
`writeProvisionMetas` creates a persistent artifact (ServiceMeta) that outlives the session
that created it. The incomplete meta then blocks the develop flow's auto-adopt gate
(`len(metas) == 0` check), which was designed assuming metas only exist when bootstrap completes.

---

## 7. Bug: Execute Guidance is Strategy-Blind

### 7.1 Evidence

`buildDeployGuide()` (deploy_guidance.go:112-186) reads strategy at line 116 but only uses it
for the **header** text:
```go
strategy := dominantStrategy(stateDir, state.Targets)
if strategy != "" {
    fmt.Fprintf(&sb, "## Execute — %s mode, %s\n\n", state.Mode, strategy)
} else {
    fmt.Fprintf(&sb, "## Execute — %s mode, strategy pending\n\n", state.Mode)
}
```

The actual workflow instructions (lines 130-143) switch on `state.Mode` only:
```go
switch state.Mode {
case PlanModeStandard:
    writeStandardWorkflow(&sb, state.Targets) // always shows deploy commands
case PlanModeDev:
    writeDevWorkflow(&sb, state.Targets)     // always shows deploy commands
case PlanModeSimple:
    writeSimpleWorkflow(&sb, state.Targets)  // always shows deploy commands
}
```

None of `writeStandardWorkflow`, `writeDevWorkflow`, or `writeSimpleWorkflow` check strategy.
They ALL emit `zerops_deploy` commands regardless of whether strategy is `manual`.

### 7.2 Contradiction with Prepare Step

`buildPrepareGuide` → `writeDevelopmentWorkflow()` (deploy_guidance.go:215-244) IS strategy-aware:
```go
switch strategy {
case StrategyManual:
    sb.WriteString("Edit code on the SSHFS mount. Tell the user when changes are ready to deploy.\n")
    sb.WriteString("User controls deployment timing.\n\n")
```

So prepare step says "user controls deployment timing" and execute step says
"Deploy: `zerops_deploy targetService=...`". Active contradiction within the same workflow.

### 7.3 Verify Step

`buildVerifyGuide()` (deploy_verify_guidance.go:63-80) reads NO strategy information.
It always generates verify prompts. This is less problematic — if a deploy happened
(regardless of who triggered it), verification makes sense.

---

## 8. Comparison: Bootstrap Session Design (Good Model)

Bootstrap's `BootstrapState` (bootstrap.go:34-40):
```go
type BootstrapState struct {
    Active            bool
    CurrentStep       int
    Steps             []BootstrapStep
    Plan              *ServicePlan              // ← GENUINE NEW STATE
    DiscoveredEnvVars map[string][]string       // ← SESSION-SPECIFIC
}
```

**Why bootstrap's design is correct**:
- `Plan` is user-submitted, validated intent. It doesn't exist anywhere else. The plan is
  created during bootstrap and consumed by checkers and guidance. Legitimate session state.
- `DiscoveredEnvVars` is discovery results accumulated during the session. Also legitimate.
- Bootstrap does NOT duplicate ServiceMeta data into its session. It reads metas fresh when
  needed (e.g., `writeBootstrapOutputs` reads plan targets directly).

**The key difference**: Bootstrap creates new state (plan, env vars). Deploy snapshots existing state.

---

## 9. What Guidance Actually Reads (Evidence Table)

Tracing every data access in the three guidance functions:

### 9.1 `buildPrepareGuide` (deploy_guidance.go:49-110)

| Data needed | Current source | Could come from |
|-------------|---------------|-----------------|
| Target hostnames | `state.Targets[].Hostname` | `ServiceMeta` list |
| Target roles | `state.Targets[].Role` | Derive from `ServiceMeta.Mode` |
| Strategy | `readCurrentStrategy(stateDir, hostname)` — **ALREADY reads from disk** | Same |
| Mode | `state.Mode` | Derive from `ServiceMeta.Mode` |
| RuntimeType | `state.Targets[].RuntimeType` via `buildKnowledgeMap()` | API |
| Environment | `env` parameter | Engine field |

### 9.2 `buildDeployGuide` (deploy_guidance.go:112-186)

| Data needed | Current source | Could come from |
|-------------|---------------|-----------------|
| Target hostnames | `state.Targets[].Hostname` | `ServiceMeta` list |
| Target roles | `state.Targets[].Role` (heavily — pair finding, workflow routing) | Derive from `ServiceMeta.Mode` |
| Strategy | `dominantStrategy()` — **ALREADY reads from disk** | Same |
| Mode | `state.Mode` | Derive from `ServiceMeta.Mode` |
| Environment | `env` parameter | Engine field |

### 9.3 `buildVerifyGuide` (deploy_verify_guidance.go:63-80)

| Data needed | Current source | Could come from |
|-------------|---------------|-----------------|
| Target hostnames | `state.Targets[].Hostname` | `ServiceMeta` list |
| RuntimeType | `state.Targets[].RuntimeType` | API |
| HTTPSupport | `state.Targets[].HTTPSupport` | API |

### 9.4 Checkers

**`checkDeployPrepare`** (tools/workflow_checks_deploy.go:33-105):
- Uses `state.Targets` for: hostname iteration (line 65), role check (line 67, 85), mount path derivation (line 226)
- Re-discovers ALL env vars from API fresh (lines 279-302) — does NOT use session-cached data
- Could use hostname list + derive role from ServiceMeta

**`checkDeployResult`** (tools/workflow_checks_deploy.go:109-179):
- Uses `state.Targets` for: hostname iteration (line 130)
- Fetches ALL service status from API fresh (lines 118-125) — does NOT use session-cached data
- Could use hostname list only

---

## 10. `DeployTarget.Status` Is Never Updated — Full Proof

### 10.1 Writes

| Location | What happens |
|----------|--------------|
| `deploy.go:140` | Init: `Status: deployTargetPending` ("pending") |
| `deploy.go:252-253` | `ResetForIteration()`: reset to "pending" |

### 10.2 Non-Writes (methods that SHOULD update target status but don't)

| Method | File:Line | What it does | Touches target Status? |
|--------|-----------|-------------|----------------------|
| `CompleteStep` | deploy.go:182-207 | Updates `Steps[].Status` to "complete" | **NO** |
| `SkipStep` | deploy.go:210-236 | Updates `Steps[].Status` to "skipped" | **NO** |
| `DeployComplete` | engine_deploy.go:29-75 | Calls `CompleteStep`, manages cleanup | **NO** |
| `DeploySkip` | engine_deploy.go:78-107 | Calls `SkipStep`, manages cleanup | **NO** |

### 10.3 Reads

| Location | What happens |
|----------|--------------|
| `deploy.go:276` | `BuildResponse()`: copies to `DeployTargetOut.Status` (always "pending") |

**Conclusion**: `DeployTarget.Status` is always "pending". It is written twice (init, reset) and
read once (display). It never changes. The agent sees `"status": "pending"` for every target
in every response for the entire session. Zero information content.

---

## 11. Proposed Minimal DeployState

```go
type DeployState struct {
    Active      bool         `json:"active"`
    CurrentStep int          `json:"currentStep"`
    Steps       []DeployStep `json:"steps"`
    Hostnames   []string     `json:"hostnames"` // just the service list
    Mode        string       `json:"mode"`       // aggregate mode (derivable, but cheap to store)
}
```

**Removed**: `DeployTarget` struct entirely (Hostname, RuntimeType, Role, Status, Strategy, HTTPSupport).
**Kept**: `Hostnames` as a simple string list — needed to know what we're working on.
**Kept**: `Mode` — while derivable, it's set once at start and never changes. Cheap to store.

### 11.1 What Changes

| Current | Proposed |
|---------|----------|
| `BuildDeployTargets(metas) → []DeployTarget` | `collectHostnames(metas) → []string` |
| `enrichTargetRuntimeTypes(services, targets)` | Delete — fetch at guidance time |
| `NewDeployState(targets, mode)` | `NewDeployState(hostnames, mode)` |
| `state.Targets[i].Hostname` | `state.Hostnames[i]` |
| `state.Targets[i].Role` | `deriveRole(meta)` — read ServiceMeta on demand |
| `state.Targets[i].Strategy` | Already deleted — guidance already reads from disk |
| `state.Targets[i].RuntimeType` | Fetch from API when guidance needs it |
| `state.Targets[i].HTTPSupport` | Fetch from API when guidance needs it |
| `state.Targets[i].Status` | Delete — never used |
| `DeployTargetOut` in response | `[]string` hostnames, or omit entirely |

### 11.2 Guidance Function Signature Changes

Current:
```go
func buildPrepareGuide(state *DeployState, env Environment, stateDir string) string
func buildDeployGuide(state *DeployState, iteration int, env Environment, stateDir string) string
func buildVerifyGuide(d *DeployState) string
```

The guidance functions currently receive `*DeployState` and access `state.Targets` for:
hostname, role, runtimeType, HTTPSupport. They'd need either:
- Option A: Receive `[]ServiceMeta` + `[]ServiceInfo` (API data) as additional params
- Option B: Receive a `GuidanceContext` struct that combines hostnames + fresh data
- Option C: Receive a function/interface that resolves hostname → meta/info on demand

Option A is simplest and most explicit.

### 11.3 Checker Function Signature

Current:
```go
type DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)
```

Checkers use `state.Targets` for hostname iteration only. They'd receive `hostnames []string`
or keep receiving `*DeployState` (which now has `Hostnames []string`).

### 11.4 Response Format Change

Current `DeployResponse.Targets`:
```go
Targets []DeployTargetOut `json:"targets"` // [{hostname, role, status}]
```

Options:
- A: `Hostnames []string` — simplest, agent knows what services are in scope
- B: Build `DeployTargetOut` on demand from ServiceMeta — preserves current response shape
- C: Add a `Services` field with fresh data from ServiceMeta + API

Option B maintains backward compat if external consumers depend on the response format.

---

## 12. Impact Assessment

### 12.1 Files That Change

| File | Change |
|------|--------|
| `workflow/deploy.go` | Remove `DeployTarget` struct, simplify `DeployState`, update `BuildDeployTargets` → `collectHostnames`, remove `ResetForIteration` target reset, update `BuildResponse` |
| `workflow/deploy_guidance.go` | Change guidance functions to receive metas/API data instead of reading `state.Targets` |
| `workflow/deploy_verify_guidance.go` | Change `buildVerifyGuide` to receive API service info |
| `workflow/engine_deploy.go` | Update `DeployStart` to pass hostnames instead of targets |
| `tools/workflow_develop.go` | Remove `enrichTargetRuntimeTypes`, simplify `handleDeployStart` |
| `tools/workflow_checks_deploy.go` | Update checkers to use hostnames list |

### 12.2 Files That DON'T Change

| File | Why |
|------|-----|
| `workflow/engine.go` | Session lifecycle mechanics are correct |
| `workflow/session.go` | State persistence is correct |
| `workflow/service_meta.go` | Source of truth — unchanged |
| `tools/workflow_strategy.go` | Already correct (reads/writes ServiceMeta directly) |
| `workflow/bootstrap.go` | Different design, correct |
| `workflow/bootstrap_outputs.go` | Correct (except the limbo bug, separate fix) |

### 12.3 Bugs Fixed by This Redesign

| Bug | How it's fixed |
|-----|---------------|
| Strategy stale data | Eliminated — no strategy in session, guidance already reads from disk |
| Dead `Status` field | Eliminated — field removed |
| Dead `Strategy` field | Eliminated — field removed |
| Execute strategy-blindness | Separate fix needed — redesign doesn't automatically make `buildDeployGuide` strategy-aware, but removes the stale-data excuse. Strategy is now always fresh. |

### 12.4 Bugs NOT Fixed (Require Separate Work)

| Bug | Why |
|-----|-----|
| Abandoned bootstrap limbo | Root cause is `Reset()` not cleaning ServiceMeta — unrelated to deploy session design |
| Execute strategy-blindness | The guidance text itself needs to be rewritten to check strategy — redesign enables but doesn't implement this |
| Missing strategy prompt in transition | Bootstrap's `BuildTransitionMessage` needs updating — unrelated to deploy session |
| Verify step nil checker | Step checker design — unrelated to session data model |

---

## 13. Test Impact

### 13.1 Tests That Need Updating

- `workflow/deploy_test.go` — tests for `BuildDeployTargets`, target assertions
- `tools/workflow_develop_test.go` / `tools/workflow_test.go` — `enrichTargetRuntimeTypes`, strategy on target
- `tools/workflow_checks_deploy_test.go` — checker tests that build `DeployState` with targets

### 13.2 Tests That Become Unnecessary

- Tests asserting `DeployTarget.Strategy` value (dead field)
- Tests asserting `DeployTarget.Status` value (dead field)
- `enrichTargetRuntimeTypes` tests (function removed)

### 13.3 New Tests Needed

- Guidance functions with fresh ServiceMeta input
- `collectHostnames` (simpler than `BuildDeployTargets`)
- Integration: strategy change mid-session → guidance reflects new strategy (currently broken)

---

## 14. Related: `handleDeployStart` Full Flow (For Context)

**`tools/workflow_develop.go:16-108`** — `handleDeployStart()`:

```
1. Read ServiceMetas from disk
2. If API available: fetch live services, prune stale metas
3. If len(metas) == 0: try auto-adopt (adoptUnmanagedServices)
4. Filter metas: IsComplete() + has Mode/StageHostname → runtimeMetas
5. BuildDeployTargets(runtimeMetas) → targets, mode        ← BUILDS SNAPSHOT
6. enrichTargetRuntimeTypes(liveServices, targets)          ← ENRICHES SNAPSHOT
7. engine.DeployStart(projectID, intent, targets, mode)     ← FREEZES SNAPSHOT
8. Append buildStrategyStatusNote(runtimeMetas)             ← READS FRESH FROM METAS
```

Steps 5-7 create the snapshot. Step 8 reads fresh — within the same function.
This inconsistency is the design smell that motivated this audit.

---

## 15. Summary of Evidence

| Claim | Proof |
|-------|-------|
| `DeployTarget.Strategy` is never read in production | `grep -rn '\.Strategy' workflow/deploy*.go` — zero hits outside tests |
| `DeployTarget.Status` is never updated | `grep -rn 'Targets\[.*Status' workflow/` — only init + reset writes |
| Guidance reads strategy from disk, not session | `readCurrentStrategy()` at deploy_guidance.go:24-33 reads ServiceMeta |
| Checkers re-discover data from API | `checkDeployPrepare` lines 279-302, `checkDeployResult` lines 118-125 |
| Bootstrap session stores genuine new state | `BootstrapState.Plan` — user-submitted, validated, exists nowhere else |
| Deploy session stores duplicated state | `DeployTarget` — copies of ServiceMeta + API data |
| Enrichment pipeline exists only for snapshot | `BuildDeployTargets` + `enrichTargetRuntimeTypes` + `NewDeployState` = ~82 lines |
