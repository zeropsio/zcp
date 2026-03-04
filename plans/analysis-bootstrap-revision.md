# Bootstrap Flow Revision — Comprehensive Analysis

This document captures the full analysis of the bootstrap workflow revision, including all findings, design decisions, trade-offs, and open questions. It serves as a foundation for the implementation plan.

---

## 1. Problem Statement

### 1.1 Current Architecture

The bootstrap workflow orchestrates LLM-driven service creation on Zerops through 11 sequential steps:

```
detect → plan → load-knowledge → generate-import → import-services → mount-dev
→ discover-envs → generate-code → deploy → verify → report
```

Key files:
- `internal/workflow/state.go` — WorkflowState, phase model
- `internal/workflow/session.go` — state persistence to `.zcp/state/zcp_state.json`
- `internal/workflow/bootstrap.go` — BootstrapState, step tracking, conditional skip logic
- `internal/workflow/bootstrap_steps.go` — 11 step definitions with guidance, tools, verification
- `internal/workflow/engine.go` — workflow engine (start, complete, skip, transition)
- `internal/workflow/evidence.go` — evidence persistence to `baseDir/evidence/{sessionID}/`
- `internal/workflow/gates.go` — 5 phase gates (G0-G4) requiring evidence for transitions
- `internal/workflow/validate.go` — ServicePlan, PlannedService, hostname validation
- `internal/content/workflows/bootstrap.md` — 751-line guidance document with section tags

### 1.2 Current State Model

```go
// internal/workflow/state.go
type WorkflowState struct {
    Version   string
    SessionID string                    // random 16-char hex
    ProjectID string
    Workflow  string                    // "bootstrap" or "deploy"
    Phase     Phase                     // INIT→DISCOVER→DEVELOP→DEPLOY→VERIFY→DONE
    Iteration int
    Intent    string
    CreatedAt string
    UpdatedAt string
    Services  map[string]ServiceRef     // EXISTS BUT NEVER POPULATED
    History   []PhaseTransition
    Bootstrap *BootstrapState
}

// internal/workflow/validate.go
type ServicePlan struct {
    Services  []PlannedService  // flat list, no relationships
    CreatedAt string
}
type PlannedService struct {
    Hostname string  // "appdev"
    Type     string  // "php-nginx@8.4"
    Mode     string  // "NON_HA" or "HA"
}
```

### 1.3 Identified Problems

1. **Wrong abstraction**: Flat `[]PlannedService` treats all services equally. No concept of "this runtime depends on that database." No topology, no relationships.

2. **Monolithic session**: One session = one complete bootstrap. No support for incremental operations (add service to existing project).

3. **Dead `Services` map**: `WorkflowState.Services` is defined but never populated anywhere in the codebase. The field exists for a purpose that was never implemented.

4. **No handoff**: After bootstrap completes, `autoCompleteBootstrap()` transitions to DONE and that's it. No record of topology persists. The next LLM session starts near-blind.

5. **Blind trust**: LLM attestation strings (min 10 chars) are the only validation. `autoCompleteBootstrap()` auto-generates evidence with `Failed=0, ServiceResults=nil` — gates can never fail.

6. **11 round-trips**: Each step requires a separate `zerops_workflow action="complete"` MCP call. Many steps are mechanically sequential (load-knowledge must precede generate-import must precede import-services).

7. **Sequential verification**: `zerops_verify` takes one hostname per call. 5 services = 5 × 15-20s = 75-100s.

---

## 2. The Core Insight: Runtime-Centric Bootstrap

### 2.1 The Unit of Work

Bootstrap is NOT "create a list of services." Bootstrap IS:

> "Here's a runtime service to create, and here are the services it needs to connect to (create them first if they don't exist)."

The **runtime service** (+ its dev/stage pair) is the primary object. Managed services (databases, caches, object storage) are **dependencies** — they exist to serve runtimes.

### 2.2 The Real-World Flow

```
User: "Make me a CMS for XY"
         │
    CLARIFICATION
         │  LLM asks: What framework? What DB? Need caching?
         │  (happens BEFORE zerops_workflow starts)
         │  Output: clear intent + service requirements
         │
    BOOTSTRAP (5 steps)
         │  discover → provision → generate → deploy → verify
         │  Registry populated. CLAUDE.md updated.
         │
    HANDOFF
         │  Session: DONE. Registry: has topology.
         │  System prompt: auto-detects CONFORMANT.
         │  Routes to deploy workflow for subsequent changes.
         │
    DEVELOPMENT (repeatable)
         │  "add feature X" → deploy workflow (mount → edit → deploy → verify)
         │  "add caching" → bootstrap with IsExisting=true target
```

### 2.3 Scenario Analysis

Four primary scenarios were identified and analyzed:

#### Scenario A: Fresh — Single Runtime + New Dependencies

User: "Create a PHP app with PostgreSQL"

```
Target: appdev/appstage (php-nginx@8.4)
Dependencies: db (postgresql@16) → CREATE
```

Flow: Full 5-step bootstrap. Import creates all services, code generated from scratch, deployed, verified.

**Current model handles this**: Yes, but as a flat list without topology.

#### Scenario B: Add Runtime to Existing Infrastructure

User has appdev+db+cache. Wants: "Add a Node.js API"

```
Target: apidev/apistage (nodejs@22)
Dependencies: db (EXISTS), cache (EXISTS) → just connect
```

Flow: discover finds existing services. Import creates only apidev+apistage. Code generated for new runtime, wired to existing env vars.

**Current model handles this**: No. The flat plan has no concept of "existing" vs "new." The detect step classifies as CONFORMANT and tells LLM to use deploy workflow instead. There's no path for adding a new runtime to existing infrastructure within bootstrap.

#### Scenario C: Add Managed Service to Existing Runtime

User has appdev+db. Wants: "Add Redis caching"

```
Target: appdev (IsExisting=true — already deployed, needs update)
Dependencies: db (EXISTS), cache (valkey@7.2, CREATE)
```

Flow: Import creates only cache. Discover its env vars. Update appdev's zerops.yml to reference cache vars. Redeploy appdev.

**Current model handles this**: No. There's no concept of bootstrapping with an existing runtime as the target. Bootstrap assumes all runtimes are new.

#### Scenario D: Multiple Runtimes from Day 1

User: "PHP frontend + Node.js API + shared DB and cache"

```
Targets:
  1. webdev/webstage (php-nginx@8.4), deps: db (CREATE), cache (CREATE)
  2. apidev/apistage (nodejs@22), deps: db (EXISTS*), cache (EXISTS*)

* EXISTS because target 1 creates them
```

Flow: One bootstrap session with multiple targets. Provision imports everything in one call. Generate creates code per-target. Deploy per-target.

**Current model handles this**: Partially — as a flat list it can create all services, but there's no structured relationship between runtimes and their dependencies.

#### Additional Scenarios Evaluated

**Mid-way failure recovery**: Current model handles well. Bootstrap stays at current step, LLM retries. The deploy step has an iteration loop (max 3). **No changes needed for this.**

**External changes between sessions**: Current model handles acceptably. `BuildInstructions()` re-discovers services from API on every session start. `engine.Start()` auto-resets DONE sessions. **Missing**: staleness timeout for abandoned sessions (state file from crashed session blocks new bootstrap).

**Destructive re-bootstrap**: Current model has no support. No mechanism for deleting services during bootstrap. LLM must exit bootstrap, use `zerops_delete` individually, then restart. **Not addressed in this revision** — explicit user confirmation required for deletion, handled outside bootstrap flow.

---

## 3. Design: Runtime-Centric Plan Structure

### 3.1 BootstrapTarget (The Input)

Replaces flat `ServicePlan` / `[]PlannedService`.

```go
// BootstrapTarget defines what we're bootstrapping.
// Runtime is the primary unit. Dependencies are what it connects to.
type BootstrapTarget struct {
    Runtime      RuntimeTarget `json:"runtime"`
    Dependencies []Dependency  `json:"dependencies,omitempty"`
}

type RuntimeTarget struct {
    DevHostname string `json:"devHostname"`         // "appdev"
    Type        string `json:"type"`                 // "php-nginx@8.4"
    IsExisting  bool   `json:"isExisting,omitempty"` // true = already deployed, needs update
}

// StageHostname derives stage from dev: "appdev" → "appstage"
func (r RuntimeTarget) StageHostname() string {
    if base, ok := strings.CutSuffix(r.DevHostname, "dev"); ok {
        return base + "stage"
    }
    return ""
}

type Dependency struct {
    Hostname   string `json:"hostname"`        // "db"
    Type       string `json:"type"`            // "postgresql@16"
    Mode       string `json:"mode,omitempty"`  // "NON_HA" (default) or "HA"
    Resolution string `json:"resolution"`      // "CREATE" or "EXISTS"
}

type ServicePlan struct {
    Targets   []BootstrapTarget `json:"targets"`
    CreatedAt string            `json:"createdAt"`
}
```

### 3.2 Plan Validation

`ValidateBootstrapPlan()` replaces `ValidateServicePlan()`:

- All hostnames pass `ValidateHostname()` (existing function)
- All types exist in live catalog (existing check)
- Dev/stage pairing enforced via `StageHostname()` convention
- `CREATE` dependencies must NOT exist in live services
- `EXISTS` dependencies MUST exist in live services
- Dependencies shared across targets: if target 1 creates `db` (CREATE), target 2 can reference it as EXISTS
- Managed service modes default to NON_HA (existing behavior)

### 3.3 How Each Step Works with Targets

The engine doesn't need fresh/incremental awareness. Same 5 steps, LLM adapts:

| Step | What happens |
|---|---|
| **discover** | LLM determines targets and dependencies from user intent + live API. Submits structured plan. |
| **provision** | Import YAML generated for CREATE deps + non-existing runtimes. Mount dev. Discover env vars. |
| **generate** | Code for each target runtime. IsExisting targets get config updates, not full code gen. |
| **deploy** | Deploy each target runtime (dev first, then stage). |
| **verify** | Verify ALL services (not just targets — catches regressions). |

### 3.4 Multi-Runtime Handling

Multiple targets in one session. Dependencies are pooled:
- One `import.yml` covers all CREATE services (managed + runtime pairs)
- Generate iterates per target, each getting its dependency env vars
- Deploy iterates per target
- For 2+ targets, subagent pattern (one agent per target pair) used in generate/deploy

---

## 4. Design: State Model — Registry + Session Split

### 4.1 The Problem with Session-Centric State

Current `WorkflowState` conflates two concerns:
1. **What ZCP has set up** (persistent topology) — which services were bootstrapped, how they connect, where they're mounted
2. **What's happening right now** (transient session) — which step we're on, what the current plan is, iteration count

When the session ends, concern #1 is lost. This is the handoff gap.

### 4.2 Registry: What the API Cannot Tell Us

The Zerops API provides:
- Service existence, types, statuses (`ListServices()`)
- Environment variables (`GetServiceEnv()`)
- Resource allocation, ports (`GetService()`)

The API does NOT know:
- Dev/stage pairing (sees `appdev` and `appstage` as unrelated services)
- Dependencies (doesn't know `appdev` uses `db`)
- Mount paths (local SSHFS state)
- Bootstrap lifecycle (whether ZCP completed its workflow for a service)
- Intent (why services were created)

### 4.3 Proposed State Model

```go
type WorkflowState struct {
    Version   string                    `json:"version"`
    ProjectID string                    `json:"projectId"`

    // PERSISTENT — survives across sessions
    Registry  map[string]*ServiceRecord `json:"registry"`

    // TRANSIENT — cleared when session completes or resets
    Session   *WorkflowSession          `json:"session,omitempty"`
}

type ServiceRecord struct {
    Hostname  string   `json:"hostname"`
    Type      string   `json:"type"`                   // "bun@1.2", "postgresql@16"
    Role      string   `json:"role"`                   // "runtime-dev", "runtime-stage", "managed"
    PairWith  string   `json:"pairWith,omitempty"`     // dev↔stage link
    DependsOn []string `json:"dependsOn,omitempty"`    // managed service hostnames
    MountPath string   `json:"mountPath,omitempty"`    // SSHFS path (dev only)
    AddedBy   string   `json:"addedBy"`                // session ID that created it
    AddedAt   string   `json:"addedAt"`                // RFC3339
    Lifecycle string   `json:"lifecycle"`              // planned → created → deployed → verified
}

type WorkflowSession struct {
    SessionID string            `json:"sessionId"`
    Workflow  string            `json:"workflow"`
    Phase     Phase             `json:"phase"`
    Iteration int               `json:"iteration"`
    Intent    string            `json:"intent"`
    CreatedAt string            `json:"createdAt"`
    UpdatedAt string            `json:"updatedAt"`
    History   []PhaseTransition `json:"history"`
    Bootstrap *BootstrapState   `json:"bootstrap,omitempty"`
}
```

### 4.4 Input vs Output Separation

- **BootstrapTarget** = INPUT to bootstrap (transient, lives in `BootstrapState.Plan` during session)
- **Registry** = OUTPUT of bootstrap (persistent, lives in `WorkflowState.Registry`)

After each bootstrap step:
- provision → registry entries with `lifecycle: "created"`
- generate → entries updated to `lifecycle: "configured"`
- deploy → entries updated to `lifecycle: "deployed"`
- verify → entries updated to `lifecycle: "verified"`

For `IsExisting=true` targets: existing registry entries get `DependsOn` updated.

### 4.5 Registry Reconciliation

At session start (`discover` step), reconcile registry against live API:
- Service in registry but deleted from Zerops → remove from registry
- Service in Zerops but not in registry → note as "externally added" in discover response
- Service in registry with matching live service → keep (topology preserved)

This is a read-repair pattern. Cheap (one API call), prevents stale entries.

### 4.6 Session Lifecycle

- `InitSession`: preserves registry, creates new Session
- `ResetSession`: clears Session only, preserves registry
- `CompleteSession`: Session → nil, registry stays

Key difference from current: `ResetSession` currently deletes the entire `zcp_state.json`. New behavior preserves registry.

### 4.7 Registry for BuildInstructions

`BuildInstructions()` in `internal/server/instructions.go` currently provides:
```
Current services:
- appdev (php-nginx@8.4) — RUNNING
- appstage (php-nginx@8.4) — RUNNING
- db (postgresql@16) — RUNNING
```

With registry, it can provide:
```
Current services:
- appdev (php-nginx@8.4) — RUNNING [dev, stage: appstage, deps: db+cache, mount: /var/www/appdev/]
- appstage (php-nginx@8.4) — RUNNING [stage of appdev]
- db (postgresql@16) — RUNNING [managed, used by: appdev]
- cache (valkey@7.2) — RUNNING [managed, used by: appdev]
```

This gives the next LLM session full topology context without any manual explanation.

---

## 5. Design: Post-Bootstrap CLAUDE.md Recording

### 5.1 The Staleness Problem

Written knowledge goes stale. Env var values change, services can be added/removed via dashboard, resources get scaled. Any detailed record creates false confidence.

### 5.2 The Two-Layer Solution

| Layer | Content | Staleness Risk | Where |
|---|---|---|---|
| **Topology** (immutable) | hostnames, types, roles, dependencies, mount paths, env var names | Very low | CLAUDE.md |
| **State** (live) | status, env values, URLs, container counts | High | `zerops_discover` via API |

`BuildInstructions()` already handles layer 2. CLAUDE.md handles layer 1.

### 5.3 What to Record (Only Immutable Facts)

**Record:**
- Service hostnames (immutable once created)
- Service types/versions (immutable once created)
- Dev/stage pairing topology
- Cross-service dependencies (which runtime uses which managed service)
- Mount paths (deterministic: `/var/www/{hostname}/`)
- Env var NAMES from managed services (names are stable)
- User intent / service purpose
- Bootstrap timestamp

**Do NOT record:**
- Env var VALUES (can change)
- Service status (changes constantly)
- Subdomain URLs (can be enabled/disabled)
- Container counts, resource limits (scaling changes these)
- Build/deploy configuration details (zerops.yml is source of truth)

### 5.4 Format

Appended to project's CLAUDE.md with idempotent markers for regeneration:

```markdown
<!-- ZEROPS:BEGIN -->
## Zerops Infrastructure

Bootstrapped: 2026-03-03 | Intent: Bun API with PostgreSQL and Valkey

> For live state (status, env values, URLs), use `zerops_discover`.

### Services

| Hostname | Type | Role | Notes |
|----------|------|------|-------|
| appdev | bun@1.2 | runtime (dev) | Mount: /var/www/appdev/ |
| appstage | bun@1.2 | runtime (stage) | Deploys from appdev |
| db | postgresql@16 | managed | |
| cache | valkey@7.2 | managed | |

### Dependencies

- **appdev/appstage** → db (connectionString, host, port, user, password), cache (connectionString, host, port)

<!-- ZEROPS:END -->
```

### 5.5 Regeneration Strategy

Always **regenerate from current state** (registry + live API), never append. Reasons:
- External changes (dashboard) could have added/removed services
- Appending creates duplicates on re-bootstrap
- Regeneration is trivial

### 5.6 Why Both Registry AND CLAUDE.md

**Registry** (zcp_state.json):
- Machine-readable, used programmatically by ZCP
- Enriches `BuildInstructions()` system prompt
- Lives in `.zcp/state/` directory (local, not in git)
- Has lifecycle tracking and session provenance

**CLAUDE.md** (project documentation):
- Human/LLM-readable at session start
- Checked into git (survives machine changes, visible to team)
- Provides context even without ZCP's state directory
- Contains minimal topology, explicitly defers to `zerops_discover` for live state

They're complementary, not redundant.

### 5.7 Staleness Mitigation

Three mechanisms:
1. **Only immutable facts** — hostnames, types, versions cannot change after creation
2. **Explicit scope declaration** — "For live state, use `zerops_discover`" tells LLM not to trust values
3. **Cross-check** — `BuildInstructions()` lists services from live API. If CLAUDE.md mentions a service not in the live list, LLM can detect inconsistency

### 5.8 Implementation

- `internal/workflow/report.go` (new, ~80 lines) — `GenerateInfraSection(registry, intent) string`
- `internal/workflow/claudemd.go` (new, ~60 lines) — `UpdateCLAUDEMD(projectDir, section) error` with marker-based idempotent replacement
- `internal/content/templates/claude.md` — add `<!-- ZEROPS:BEGIN --><!-- ZEROPS:END -->` markers
- `internal/workflow/engine.go` — hook into bootstrap completion (after `autoCompleteBootstrap`)
- `internal/server/instructions.go` — `BuildInstructions` reads registry for richer system prompt

---

## 6. Design: Step Consolidation (11 → 5)

### 6.1 Step Mapping

| # | New Step | Old Steps | Category |
|---|----------|-----------|----------|
| 0 | **discover** | detect + plan + load-knowledge | creative |
| 1 | **provision** | generate-import + import + mount + discover-envs | creative |
| 2 | **generate** | generate-code | creative |
| 3 | **deploy** | deploy | branching |
| 4 | **verify** | verify + report | fixed |

The `report` step is absorbed into `verify` + auto-CLAUDE.md write.

### 6.2 Per-Step Detail

#### Step 0: discover

**Input**: User intent (natural language)
**Output**: `[]BootstrapTarget` with Resolution filled in

Actions:
1. Call `zerops_discover` to get all existing services
2. Read registry for existing topology
3. Parse user intent into runtime + dependency requirements
4. If unclear, guidance says: ask the user before proceeding
5. For each required service, check if it already exists → resolution
6. Validate plan (hostnames, types, pairing, resolution consistency)
7. Submit structured plan via `zerops_workflow action="complete" step="discover" plan=[...]`

Hard checks:
- All hostnames pass `ValidateHostname()` (existing function)
- All types exist in live catalog
- Dev/stage pairing: every RuntimeTarget has valid StageHostname
- Resolution consistency: CREATE services don't exist, EXISTS services do exist
- Knowledge loaded for ALL target runtime types

#### Step 1: provision

**Input**: Validated plan
**Output**: All services exist, dev mounted, env vars known

Actions:
1. Load knowledge: runtime briefing + infrastructure scope for each target type
2. Generate import.yml for all CREATE services + new runtime pairs
3. Execute import: `zerops_import`
4. Mount dev filesystems: `zerops_mount` for each non-IsExisting target
5. Discover env vars: `zerops_discover service=X includeEnvs=true` for each dependency

Hard checks:
- All planned services exist (ListServices)
- Dev runtimes: status = RUNNING (startWithoutCode)
- Managed services: status = RUNNING
- Stage runtimes: status = NEW or READY_TO_DEPLOY
- Dev services: SSHFS mount active
- Each managed service has non-empty env vars

#### Step 2: generate

**Input**: Mounted dev filesystem, discovered env vars, loaded knowledge
**Output**: zerops.yml + app code on dev filesystem

Actions:
1. Write zerops.yml with entries for both dev and stage hostnames
2. Write application code with `/`, `/health`, `/status` endpoints
3. Wire env vars using EXACT variables from provision step
4. For IsExisting targets: update existing code, don't regenerate from scratch

Hard checks:
- For each target: zerops.yml exists on mount path
- Has `setup:` entries for both dev and stage hostnames
- Dev entries: no healthCheck, no readinessCheck
- All entries: `run.start` non-empty (except PHP/implicit-webserver runtimes)
- All entries: `run.ports` non-empty (except non-HTTP workers)
- Env ref validation: `${hostname_var}` patterns reference valid hostnames
- Stage entries: start is NOT `zsc noop --silent`

#### Step 3: deploy

**Input**: Code on dev filesystem
**Output**: Running, verified services

Actions:
1. Deploy dev: self-deploy via SSH
2. Enable subdomain: `zerops_subdomain action="enable"`
3. Verify dev: `ops.Verify()` health checks
4. Deploy stage: cross-deploy from dev
5. Enable subdomain + verify stage
6. Iteration loop on failure (max 3 attempts)

Hard checks:
- `ops.Verify()` per deployed target (parallel)
- `service_running` = pass
- `http_health` = pass (if subdomain enabled)
- `http_status` = pass
- Degraded tolerated, only unhealthy = fail
- Server-side `checkAttempts` counter (not just LLM-enforced)

#### Step 4: verify

**Input**: All targets deployed
**Output**: Final report, registry updated, CLAUDE.md written

Actions:
1. Run `VerifyAll()` on all project services (parallel)
2. Update registry: all target services → `lifecycle: "verified"`
3. Generate CLAUDE.md infrastructure section
4. Present final report with service URLs and statuses

Hard checks:
- `VerifyAll()` — all services healthy or degraded (not unhealthy)
- Registry fully populated with verified entries
- CLAUDE.md written successfully

### 6.3 Skip Logic

Simplified from current `validateConditionalSkip()`:
- `generate` + `deploy` can be skipped (if targets are managed-only, but this is rare)
- `discover`, `provision`, `verify` cannot be skipped

---

## 7. Design: Server-Side Hard Checks

### 7.1 Concept

Each `zerops_workflow action="complete" step=X` triggers deterministic server-side validation before advancing. If checks fail → step NOT completed → response includes specific failures → LLM fixes → retries.

### 7.2 Types

```go
// StepCheckResult holds the outcome of server-side step validation.
type StepCheckResult struct {
    Passed  bool        `json:"passed"`
    Checks  []StepCheck `json:"checks"`
    Summary string      `json:"summary"`  // "4/4 passed" or "2/4 passed, 2 failed"
}

type StepCheck struct {
    Name   string `json:"name"`   // "service_exists:appdev"
    Status string `json:"status"` // "pass", "fail", "skip"
    Detail string `json:"detail,omitempty"`
}

// StepChecker performs server-side validation for a bootstrap step.
type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
```

### 7.3 Key Design Decision

Hard check failure is NOT a Go error. It returns a normal response with `CheckResult` populated. The LLM sees what failed, fixes it, calls complete again. This prevents the need for manual error recovery.

### 7.4 Gaps Found in Original Plan

- **No dev/stage pairing enforcement**: `ValidateServicePlan()` checks hostnames and types but not pairing. The discover hard check must validate `StageHostname()` convention.
- **Single-runtime knowledge check**: `KnowledgeTracker.IsLoaded()` checks if ANY runtime briefing loaded, not that briefings for ALL target types loaded. Multi-runtime bootstrap needs per-type checking.
- **Non-HTTP services**: Generate hard check requires `run.ports` non-empty for all entries, but workers might not have HTTP ports. Need conditional check based on service purpose.
- **LLM-only retry limit**: Deploy iteration max (3 attempts) is guidance-enforced, not engine-enforced. Need `checkAttempts` counter in BootstrapState for server-side enforcement.

---

## 8. Design: Performance Improvements

### 8.1 Batch Verify (VerifyAll)

Current: `zerops_verify` takes one hostname per call. 5 services × 15-20s = 75-100s.
Proposed: `serviceHostname` optional. Without it → verify ALL in parallel.

```go
type VerifyAllResult struct {
    Summary  string         `json:"summary"`
    Status   string         `json:"status"`   // healthy/degraded/unhealthy
    Services []VerifyResult `json:"services"`
}
```

Savings: 75s → ~15s.

### 8.2 Verify Internal Speedup

Current: 3 separate log fetches per service.
Proposed: Batch to 2 + parallelize log and HTTP groups.

- Fetch 1: `severity=error, since=5m` → derive `no_error_logs` + `no_recent_errors`
- Fetch 2: `search="listening|started|ready"` → `startup_detected`
- Parallel: log group + HTTP group (health + status)

Savings: Per-service 15-20s → 7-10s.

### 8.3 Build Polling Speedup

Current defaults: initial=3s, stepUp=10s, stepUpAfter=60s.
Proposed: initial=1s, stepUp=5s, stepUpAfter=30s.

### 8.4 Validation Improvements

- Stage-specific checks in `deploy_validate.go`: `start: zsc noop --silent` on stage → warning
- Env var reference validation: parse `${hostname_var}`, validate hostname exists as service

### 8.5 Content Deduplication

Reference appendix in bootstrap.md for repeated content:
- /status endpoint specification (duplicated 3×)
- Hostname rules (duplicated 4×)
- Dev vs stage configuration matrix
- PHP runtime exceptions

---

## 9. Phase Gate Analysis

### 9.1 Current Gate System

5 gates (G0-G4) require evidence before phase transitions:

| Gate | Transition | Required Evidence | Freshness |
|------|-----------|-------------------|-----------|
| G0 | INIT → DISCOVER | recipe_review | — |
| G1 | DISCOVER → DEVELOP | discovery | 24h |
| G2 | DEVELOP → DEPLOY | dev_verify | 24h |
| G3 | DEPLOY → VERIFY | deploy_evidence | 24h |
| G4 | VERIFY → DONE | stage_verify | 24h |

### 9.2 The Redundancy Problem

`autoCompleteBootstrap()` (bootstrap_evidence.go:18-83) generates synthetic evidence from step attestations and auto-transitions through ALL phases in one shot when bootstrap completes. Gates never actually gate anything for bootstrap — they pass trivially with synthetic evidence.

Hard checks at step boundaries provide strictly stronger validation because they run real API calls and health checks.

### 9.3 Recommendation

Keep gates for non-bootstrap workflows (deploy, debug, scale, configure). For bootstrap, hard checks ARE the gates. `autoCompleteBootstrap()` can still auto-transition phases for compatibility, but no need for separate evidence when hard checks already validated.

---

## 10. Open Questions & Areas for Further Discussion

### 10.1 Stage Hostname Convention

Current design assumes `devHostname` ends in "dev" → stage = replace with "stage":
- `appdev` → `appstage` ✓
- `webdev` → `webstage` ✓
- `apidev` → `apistage` ✓

What about edge cases?
- `myapp` → no stage (simple mode, no pair)
- `dev` → `stage` (bare, works but unusual)
- User explicitly names differently?

**Decision needed**: Is the convention strict (enforced) or advisory (LLM can override)?

### 10.2 IsExisting Runtime Updates

When adding a managed service to an existing runtime, the generate step "updates existing code." What does this mean concretely?
- Update zerops.yml envVariables with new dependency refs
- Update /status endpoint to check new dependency
- Add usage code for new dependency (e.g., cache client initialization)

How much code change should the LLM make? Full app update or minimal wiring?

**Decision needed**: Scope of IsExisting updates — minimal wiring vs comprehensive integration.

### 10.3 Registry Cleanup

What happens to registry entries for services that are deleted?
- Option A: Auto-remove on reconciliation (clean but lossy)
- Option B: Mark as `lifecycle: "removed"` (preserves history but adds clutter)

**Decision needed**: Which approach for deleted services?

### 10.4 Multi-Target Ordering

For multi-runtime bootstraps (Scenario D), targets share dependencies. The first target's CREATE dependencies become the second target's EXISTS dependencies. This ordering is implicit in the plan structure.

Should the engine enforce ordering? Or trust the LLM to submit targets in the right order?

**Decision needed**: Engine-enforced vs LLM-determined target ordering.

### 10.5 Non-Bootstrap Managed Service Addition

"Add Redis caching" when there's no code change needed (runtime already supports it, just needs env var). Is this really a bootstrap? Or should it be a simpler flow:

```
zerops_import → zerops_env → zerops_manage action="reload"
```

**Decision needed**: Is adding a managed service always a bootstrap, or sometimes a simpler configure operation?

### 10.6 Shared Storage Dependencies

Shared storage has a two-stage process:
1. `mount: [hostname]` in import.yml
2. `mount: [hostname]` in zerops.yml `run:` section
3. `zerops_manage action="connect-storage"` after stage becomes ACTIVE

This is more complex than a simple database dependency. How does it fit the Dependency model?

**Decision needed**: Shared storage as a Dependency with special handling, or a separate concept?

### 10.7 CLAUDE.md Location

Options:
- Section in existing project CLAUDE.md (checked into git, visible to all)
- Separate `ZEROPS.md` file
- `.claude/infrastructure.md` (private, not in git)

**Decision made**: Section in CLAUDE.md with markers. Checked into git. Visible to all. Auto-read by Claude Code.

### 10.8 Evidence System Future

With hard checks, per-step evidence becomes less important (checks validate deterministically). But evidence still has value for:
- Audit trail (what was done when)
- Iteration history (what was tried before)
- Gate compatibility for non-bootstrap workflows

Should evidence be simplified? Per-service? Per-target? Per-session?

**Open**: Evidence model may need revision once hard checks are implemented.

---

## 11. Current Codebase Reference

### Key Files

| File | Lines | Purpose |
|------|-------|---------|
| `internal/workflow/state.go` | 55 | WorkflowState, ServiceRef, PhaseTransition types |
| `internal/workflow/session.go` | 145 | State persistence (InitSession, LoadSession, ResetSession, saveState) |
| `internal/workflow/engine.go` | 255 | Workflow engine (Start, Transition, BootstrapComplete, etc.) |
| `internal/workflow/bootstrap.go` | 270 | BootstrapState, step tracking, conditional skip, BuildResponse |
| `internal/workflow/bootstrap_steps.go` | 276 | 11 step definitions with guidance/tools/verification |
| `internal/workflow/bootstrap_evidence.go` | 83 | autoCompleteBootstrap, evidence map |
| `internal/workflow/bootstrap_guidance.go` | 33 | Section extraction from bootstrap.md |
| `internal/workflow/evidence.go` | 66 | Evidence types, atomic persistence |
| `internal/workflow/gates.go` | 114 | 5 phase gates (G0-G4) |
| `internal/workflow/validate.go` | 100+ | ServicePlan, PlannedService, ValidateServicePlan |
| `internal/tools/workflow.go` | 330+ | MCP tool handler (zerops_workflow) |
| `internal/tools/workflow_bootstrap.go` | 35 | Plan routing, BootstrapCompletePlan delegation |
| `internal/server/instructions.go` | 125 | BuildInstructions, buildProjectSummary |
| `internal/content/workflows/bootstrap.md` | 751 | Full bootstrap guidance with section tags |
| `internal/content/workflows/deploy.md` | 214 | Deploy workflow guidance |
| `internal/content/content.go` | 58 | Embedded content (embed.FS) |
| `internal/content/templates/claude.md` | 2 | CLAUDE.md template (just "# Zerops") |
| `internal/init/init.go` | 141 | `zcp init` subcommand |
| `internal/ops/verify.go` | 152 | Service verification (health checks) |
| `internal/ops/verify_checks.go` | 200+ | Individual check functions |
| `internal/ops/deploy_validate.go` | 74 | Deploy validation |
| `internal/ops/progress.go` | 44 | Polling intervals |
| `internal/ops/discover.go` | 102 | Service discovery |
| `internal/ops/helpers.go` | 151 | Env var parsing, cross-ref detection |
| `internal/platform/types.go` | 58 | Service types |
| `internal/platform/client.go` | 100+ | API client interface |

### Existing Functions to Reuse

- `ValidateHostname()` in `validate.go` — hostname validation
- `isManagedService()` in `managed_types.go` — type classification
- `hasImplicitWebServer()` in `deploy_validate.go` — PHP/nginx detection
- `DetectProjectState()` in engine — FRESH/CONFORMANT/NON_CONFORMANT
- `ops.Verify()` / `ops.VerifyAll()` (to be created) — health checks
- `buildProjectSummary()` in `instructions.go` — system prompt generation
- `extractSection()` in `bootstrap_guidance.go` — section tag extraction
- `KnowledgeTracker.IsLoaded()` — knowledge load validation
- `saveState()` in `session.go` — atomic write pattern (temp+rename)

---

## 12. Hard Check Implementation Detail

### 12.1 Why Hard Checks Replace Attestation

The original 11-step design provided safety through granularity — separate detect, load-knowledge, and discover-envs steps acted as implicit validation gates. Comparison with `../zcp-main` shows these exist to prevent: (1) duplicate services, (2) code generation with unvetted knowledge, (3) wrong env var names, (4) trusting deploy self-reports.

With hard checks, these validations become explicit and stronger:

| Original 11-step guarantee | Replaced by hard check |
|---|---|
| detect prevents duplicate services | **discover** hard check: `ListServices()` → if CONFORMANT/NON_CONFORMANT, block with structured response before any mutations |
| load-knowledge gates generate-import | **discover** hard check: verify knowledge tracker loaded runtime + infrastructure before completing step |
| discover-envs gates generate-code | **provision** hard check: verify each managed service has discoverable env vars (non-empty) |
| verify doesn't trust deploy | **deploy** hard check: `ops.Verify()` on each deployed service; **verify** hard check: `VerifyAll()` batch |

The hard check approach is strictly better because:
- 11-step attestations are strings LLM writes — always "pass", never validated
- Hard checks run real API calls / file reads — deterministic, cannot be faked
- Hard check failures return structured data (what failed, why, how to fix)

### 12.2 StepChecker Type

**File**: `internal/workflow/bootstrap_checks.go` (new)

```go
// StepCheckResult holds the outcome of server-side step validation.
type StepCheckResult struct {
    Passed  bool          `json:"passed"`
    Checks  []StepCheck   `json:"checks"`
    Summary string        `json:"summary"` // "4/4 passed" or "2/4 passed, 2 failed"
}

type StepCheck struct {
    Name   string `json:"name"`   // e.g. "service_exists:appdev"
    Status string `json:"status"` // "pass", "fail", "skip"
    Detail string `json:"detail,omitempty"`
}

// StepChecker performs server-side validation for a bootstrap step.
// Returns nil result if no hard checks are applicable (creative-only steps).
type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
```

### 12.3 Integration with BootstrapComplete()

**File**: `internal/workflow/engine.go`

```go
func (e *Engine) BootstrapComplete(stepName, attestation string, checker StepChecker) (*BootstrapResponse, error) {
    // ... existing validation ...

    // Run hard checks if checker provided
    if checker != nil {
        checkResult, err := checker(ctx, state.Bootstrap.Plan)
        if err != nil {
            return nil, fmt.Errorf("step check error: %w", err)
        }
        if checkResult != nil && !checkResult.Passed {
            // Return response with check failures — step NOT advanced
            resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent)
            resp.CheckResult = checkResult
            resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", stepName, checkResult.Summary)
            return resp, nil  // NOT an error — structured failure
        }
    }

    // Existing: advance step
    state.Bootstrap.CompleteStep(stepName, attestation)
    // ...
}
```

Key design: Hard check failure is NOT a Go error — it returns a normal response with `CheckResult` populated. LLM sees what failed, fixes it, calls complete again.

### 12.4 Building Checkers in Tool Layer

**File**: `internal/tools/workflow_bootstrap.go`

```go
func buildStepChecker(ctx context.Context, step string, client platform.Client,
    fetcher platform.LogFetcher, projectID string) workflow.StepChecker {

    switch step {
    case "provision":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkProvision(ctx, client, projectID, plan)
        }
    case "generate":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkGenerate(ctx, client, projectID, plan)
        }
    case "deploy":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkDeploy(ctx, client, fetcher, projectID, plan)
        }
    case "verify":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkVerify(ctx, client, fetcher, projectID, plan)
        }
    default:
        return nil // discover: plan validation handled separately in BootstrapCompletePlan
    }
}
```

Each `checkXxx` function lives in `internal/tools/workflow_checks.go` (new file).

### 12.5 Per-Step Check Details

**Step 0 (discover)** — plan validation + safety checks:
- **Duplicate prevention** (replaces old detect step): `ListServices()` → if runtime services exist, classify as CONFORMANT/NON_CONFORMANT and return structured response. LLM cannot proceed to provision without addressing existing services.
- **Knowledge validation** (replaces old load-knowledge gate): check `KnowledgeTracker.IsLoaded()` — must be loaded for ALL target runtime types, not just "any one loaded".
- **Plan validation**: all hostnames pass `ValidateHostname()`, types exist in live catalog, dev/stage pairing correct via `StageHostname()`, managed services have `mode`.
- **Resolution validation**: CREATE dependencies must not exist, EXISTS dependencies must exist.

**Step 1 (provision)** — infrastructure verification:
- `ListServices()` → all planned services exist
- Dev runtime services: status = RUNNING (startWithoutCode)
- Managed services: status = RUNNING
- Stage services: status = NEW or READY_TO_DEPLOY
- Dev services: SSHFS mount is active (`zerops_mount action=status`)
- **Env var gating** (replaces old discover-envs gate): for each managed service (dependency), call `Discover(hostname, includeEnvs=true)` → verify non-empty env vars returned. If empty → block with "managed service {hostname} has no env vars — check if service is RUNNING"

**Step 2 (generate)** — zerops.yml validation:
- For each target's dev service: read zerops.yml from mount path
- Has `setup:` entry for BOTH dev and stage hostnames
- Dev entries: `deployFiles` contains `.`
- Dev entries: no `healthCheck`, no `readinessCheck`
- All entries: `run.start` non-empty (except PHP/implicit-webserver runtimes — use `hasImplicitWebServer()`)
- All entries: `run.ports` non-empty (except non-HTTP workers — conditional check)
- **Env ref validation**: parse `${hostname_var}` patterns from `envVariables`, validate referenced hostnames exist as services
- Stage entries: `start` is NOT `zsc noop --silent`

**Step 3 (deploy)** — health verification:
- Run `ops.Verify()` on each deployed runtime service (parallel)
- Require `service_running` = pass
- Require `http_health` = pass (if subdomain enabled)
- Require `http_status` = pass (connectivity proof)
- `degraded` tolerated (advisory checks), only `unhealthy` = fail
- Verify errors → `skip` (network failure ≠ broken service)
- **Server-side `checkAttempts` counter** per target (not just LLM-enforced max 3)

**Step 4 (verify)** — batch verify all + registry + CLAUDE.md:
- Run `VerifyAll()` on all project services (parallel)
- Populate `Evidence.ServiceResults` with real health data
- Set `Evidence.Failed` from actual failures
- Update registry: all services → `lifecycle: "verified"`
- Generate and write CLAUDE.md infrastructure section
- Auto-generate final report (service list + URLs + statuses)

### 12.6 Hard Check Files Summary

- `internal/workflow/bootstrap_checks.go` (new) — `StepChecker` type, `StepCheckResult`, `StepCheck`
- `internal/workflow/engine.go` — add `checker StepChecker` param to `BootstrapComplete()`
- `internal/workflow/bootstrap.go` — add `CheckResult *StepCheckResult` to `BootstrapResponse`
- `internal/tools/workflow_checks.go` (new) — `buildStepChecker()`, `checkProvision()`, `checkGenerate()`, `checkDeploy()`, `checkVerify()`
- `internal/tools/workflow_bootstrap.go` — call `buildStepChecker()` in `handleBootstrapComplete()`
- `internal/tools/workflow.go` — thread `logFetcher` to `handleBootstrapComplete()`
- `internal/server/server.go` — pass `logFetcher` to `RegisterWorkflow()`

---

## 13. Batch Verify Implementation Detail

### 13.1 Problem

`zerops_verify` takes one hostname. For 5 services → 5 sequential calls → 75-100s.

### 13.2 Solution: VerifyAll()

Make `serviceHostname` optional. Without it → verify ALL project services in parallel.

```go
type VerifyAllResult struct {
    Summary  string         `json:"summary"`
    Status   string         `json:"status"`   // healthy/degraded/unhealthy
    Services []VerifyResult `json:"services"`
}

func VerifyAll(ctx context.Context, client platform.Client, fetcher platform.LogFetcher,
    httpClient HTTPDoer, projectID string) (*VerifyAllResult, error) {
    services, _ := client.ListServices(ctx, projectID)
    // Get log access once (shared across all)
    logAccess, logErr := client.GetProjectLog(ctx, projectID)
    // Run Verify per service in parallel (errgroup, max 5 concurrent)
    // Collect, compute summary
}
```

Tool change: `internal/tools/verify.go` — make `ServiceHostname` optional, dispatch to `VerifyAll` or `Verify`.

### 13.3 Files

- `internal/ops/verify.go` — add `VerifyAll()`, `VerifyAllResult`
- `internal/ops/verify_test.go` — `TestVerifyAll_*`
- `internal/tools/verify.go` — optional hostname, batch dispatch
- `internal/tools/verify_test.go` — `TestVerifyTool_BatchMode`

Savings: 5 × ~15s = 75s → 1 × ~15s (parallel) = ~60s saved.

---

## 14. Verify Internal Speedup Detail

### 14.1 Batch Log Checks: 3 calls → 2

New `batchLogChecks()` in `verify_checks.go`:
- Fetch 1: `severity=error, since=5m` → derive `no_error_logs` + `no_recent_errors` (filter `Timestamp > now-2m`)
- Fetch 2: `search="listening|started|ready"` → `startup_detected`

### 14.2 Parallelize Log + HTTP Groups

In `verify.go`, run concurrently:
- Group A: batchLogChecks() → 3 results
- Group B: checkHTTPHealth() + checkHTTPStatus() → 2 results (also parallel)

### 14.3 Files

- `internal/ops/verify_checks.go` — `batchLogChecks()`, `filterRecent()`
- `internal/ops/verify.go:117-152` — parallel restructure

Savings: Per-service ~15-20s → ~7-10s.

---

## 15. Build Polling Speedup Detail

`progress.go:39-44` new defaults:
```go
initialInterval: 1 * time.Second,   // was 3s
stepUpInterval:  5 * time.Second,   // was 10s
stepUpAfter:     30 * time.Second,  // was 60s
```

Files: `internal/ops/progress.go`, `internal/ops/progress_test.go`

---

## 16. Validation Improvements Detail

### 16.1 Stage Warnings

In `deploy_validate.go` after line 74, add stage-specific checks:
- `start: zsc noop --silent` on stage → warning ("stage should have real start command")
- This is a warning, not an error — allows deploy to proceed but alerts the LLM

### 16.2 Env Var Reference Validation

New function `ValidateEnvReferences()`:
- Parse `${hostname_var}` patterns from zerops.yml `envVariables` section
- Extract hostname prefix (before first `_`)
- Validate hostname exists as a service in the project
- Return warnings for unresolvable references

Files: `internal/ops/deploy_validate.go`, `internal/ops/deploy_validate_test.go`

---

## 17. Content Deduplication Detail

Add `## Reference` appendix in bootstrap.md to deduplicate repeated content:

- **A. /status Endpoint Specification** — currently duplicated 3× across step sections. Consolidate into one reference, link from steps.
- **B. Hostname Rules** — currently duplicated 4×. Single reference.
- **C. Dev vs Stage Configuration Matrix** — table showing what differs between dev and stage setup entries (start command, healthCheck, readinessCheck, deployFiles).
- **D. PHP/Implicit-Webserver Runtime Exceptions** — what's different for php-nginx, php-apache, nginx, static runtimes (no start/ports needed).

Merge section tags (`<section name="...">`) to match 5-step model. Remove old 11-step section tags.

Files: `internal/content/workflows/bootstrap.md`, `internal/workflow/bootstrap_guidance_test.go`

---

## 18. Estimated Impact

| Metric | Before | After |
|--------|--------|-------|
| Workflow round-trips | 11 | 5 |
| Verify all services | N × 15-20s sequential | 1 × 7-10s parallel |
| Gate evidence quality | LLM attestation (always passes) | Real health checks (can fail) |
| Stage misconfig | Caught at runtime | Caught before deploy |
| Env var typos | Caught at runtime | Caught at generate step |
| Typical bootstrap time | 4-6 min | 2-3 min |
| Handoff context | Lost between sessions | Registry + CLAUDE.md |
| Incremental support | Not possible | Same engine, IsExisting target |

---

## 19. Summary of Design Decisions Made

| Decision | Choice | Rationale |
|---|---|---|
| Primary abstraction | Runtime-centric (target + dependencies) | Matches real-world: bootstrap = set up a runtime with its dependencies |
| State model | Registry (persistent) + Session (transient) | Registry provides topology for handoff; session is ephemeral |
| Incremental support | Same engine, LLM adapts via registry context | No Scope/Action fields needed; simplest possible model |
| CLAUDE.md recording | Both registry AND CLAUDE.md section | Complementary: programmatic + durable handoff |
| What to record | Only immutable facts (topology) | Prevents staleness; live state via API |
| Step count | 11 → 5 | Reduces round-trips while preserving decision points |
| Hard checks | Server-side, per-step, deterministic | Replaces trusted LLM attestations with real validation |
| Phase gates | Simplified for bootstrap (hard checks replace) | Gates are redundant with hard checks |
| Migration | Not needed (dev phase) | No backward compatibility required |
| Multi-runtime | Multiple targets in one session | Efficient (one import), shared dependencies |
| Stage naming | Convention: `*dev` → `*stage` | Simple, derivable, no separate field needed |
