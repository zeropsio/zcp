# Bootstrap Flow Revision — Comprehensive Analysis

This document is the single self-contained reference for the bootstrap workflow revision. It captures the full analysis, all design decisions, current source code state, and the complete implementation plan. No external documents are required.

---

## 1. Problem Statement

### 1.1 Current Architecture

The bootstrap workflow orchestrates LLM-driven service creation on Zerops through 11 sequential steps:

```
detect -> plan -> load-knowledge -> generate-import -> import-services -> mount-dev
-> discover-envs -> generate-code -> deploy -> verify -> report
```

Key files:
- `internal/workflow/state.go` (91 lines) — WorkflowState, phase model
- `internal/workflow/session.go` (146 lines) — state persistence to `.zcp/state/zcp_state.json`
- `internal/workflow/bootstrap.go` (280 lines) — BootstrapState, step tracking, conditional skip logic
- `internal/workflow/bootstrap_steps.go` (275 lines) — 11 step definitions with guidance, tools, verification
- `internal/workflow/engine.go` (255 lines) — workflow engine (start, complete, skip, transition)
- `internal/workflow/evidence.go` (145 lines) — evidence persistence to `baseDir/evidence/{sessionID}/`
- `internal/workflow/gates.go` (160 lines) — 5 phase gates (G0-G4) requiring evidence for transitions
- `internal/workflow/validate.go` (113 lines) — ServicePlan, PlannedService, hostname validation
- `internal/workflow/bootstrap_evidence.go` (84 lines) — autoCompleteBootstrap, evidence map
- `internal/content/workflows/bootstrap.md` (~751 lines) — guidance document with section tags
- `internal/ops/verify.go` (156 lines) — service verification (6 sequential checks per runtime)
- `internal/ops/verify_checks.go` (229 lines) — individual check functions
- `internal/ops/deploy_validate.go` (152 lines) — pre-deploy zerops.yml validation
- `internal/ops/progress.go` (180 lines) — build/process polling with adaptive intervals
- `internal/tools/verify.go` (43 lines) — MCP tool for zerops_verify (single hostname only)
- `internal/tools/workflow.go` (321 lines) — MCP tool for zerops_workflow
- `internal/tools/workflow_bootstrap.go` (106 lines) — bootstrap step handlers
- `internal/server/server.go` (119 lines) — MCP server setup, tool registration
- `internal/server/instructions.go` (126 lines) — BuildInstructions for system prompt

### 1.2 Current State Model

```go
// internal/workflow/state.go
type WorkflowState struct {
    Version   string
    SessionID string                    // random 16-char hex
    ProjectID string
    Workflow  string                    // "bootstrap" or "deploy"
    Phase     Phase                     // INIT->DISCOVER->DEVELOP->DEPLOY->VERIFY->DONE
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

### 1.3 Current Bootstrap State Machine

```go
// internal/workflow/bootstrap.go
type BootstrapState struct {
    Active      bool            `json:"active"`
    CurrentStep int             `json:"currentStep"`
    Steps       []BootstrapStep `json:"steps"`       // 11 steps
    Plan        *ServicePlan    `json:"plan,omitempty"`
}

type BootstrapResponse struct {
    SessionID       string             `json:"sessionId"`
    Intent          string             `json:"intent"`
    Progress        BootstrapProgress  `json:"progress"`
    Current         *BootstrapStepInfo `json:"current,omitempty"`
    Message         string             `json:"message"`
    AvailableStacks string             `json:"availableStacks,omitempty"`
}

// Step constants for conditional skip
const (
    stepDiscoverEnvs = "discover-envs"
    stepMountDev     = "mount-dev"
    stepGenerateCode = "generate-code"
    stepDeploy       = "deploy"
)
```

### 1.4 Current Engine Flow

```go
// internal/workflow/engine.go
func (e *Engine) BootstrapComplete(stepName, attestation string) (*BootstrapResponse, error) { ... }
func (e *Engine) BootstrapCompletePlan(services []PlannedService, liveTypes []platform.ServiceStackType) (*BootstrapResponse, error) {
    // Checks CurrentStepName() == "plan"
    // Calls ValidateServicePlan()
    // Stores plan in BootstrapState.Plan
}
```

### 1.5 Current Evidence & Gates

```go
// internal/workflow/bootstrap_evidence.go
var bootstrapEvidenceMap = map[string][]string{
    "recipe_review":   {"detect", "plan", "load-knowledge"},
    "discovery":       {"discover-envs"},
    "dev_verify":      {"generate-code", "deploy", "verify"},
    "deploy_evidence": {"deploy"},
    "stage_verify":    {"verify", "report"},
}
// autoCompleteBootstrap() generates evidence with Failed=0, ServiceResults=nil ALWAYS
// -> gates can NEVER fail

// internal/workflow/gates.go
var gates = []gateDefinition{
    {"G0", PhaseInit, PhaseDiscover, []string{"recipe_review"}, 0},
    {"G1", PhaseDiscover, PhaseDevelop, []string{"discovery"}, 24h},
    {"G2", PhaseDevelop, PhaseDeploy, []string{"dev_verify"}, 24h},
    {"G3", PhaseDeploy, PhaseVerify, []string{"deploy_evidence"}, 24h},
    {"G4", PhaseVerify, PhaseDone, []string{"stage_verify"}, 24h},
}
```

### 1.6 Current Verification

```go
// internal/ops/verify.go
func Verify(ctx, client, fetcher, httpClient, projectID, hostname) (*VerifyResult, error) {
    // 6 SEQUENTIAL checks for runtime, 1 for managed
    // Checks: service_running, no_error_logs(5m), startup_detected,
    //         no_recent_errors(2m), http_health, http_status
    // 3 separate log fetches, 2 HTTP calls -> 15-20s per service
}

// internal/ops/verify_checks.go
// checkErrorLogs2m() just calls checkErrorLogs with 2m duration -> redundant fetch

// internal/tools/verify.go
type VerifyInput struct {
    ServiceHostname string `json:"serviceHostname"` // REQUIRED, no batch mode
}
```

### 1.7 Current Polling

```go
// internal/ops/progress.go
var defaultBuildPollConfig = pollConfig{
    initialInterval: 3 * time.Second,    // too slow
    stepUpInterval:  10 * time.Second,   // too slow
    stepUpAfter:     60 * time.Second,   // too slow
    timeout:         15 * time.Minute,
}
```

### 1.8 Current Tool Registration

```go
// internal/server/server.go
tools.RegisterWorkflow(s.server, s.client, projectID, stackCache, wfEngine, knowledgeTracker)
// Note: logFetcher NOT passed -> needed for hard checks

// internal/tools/workflow.go
func RegisterWorkflow(srv, client, projectID, cache, engine, tracker) { ... }
// handleBootstrapComplete: routes input.Step == "plan" && len(input.Plan) > 0
```

### 1.9 Identified Problems

1. **Wrong abstraction**: Flat `[]PlannedService` treats all services equally. No concept of "this runtime depends on that database." No topology, no relationships.

2. **Monolithic session**: One session = one complete bootstrap. No support for incremental operations (add service to existing project).

3. **Dead `Services` map**: `WorkflowState.Services` is defined but never populated anywhere in the codebase. The field exists for a purpose that was never implemented.

4. **No handoff**: After bootstrap completes, `autoCompleteBootstrap()` transitions to DONE and that's it. No record of topology persists. The next LLM session starts near-blind.

5. **Blind trust**: LLM attestation strings (min 10 chars) are the only validation. `autoCompleteBootstrap()` auto-generates evidence with `Failed=0, ServiceResults=nil` — gates can never fail.

6. **11 round-trips**: Each step requires a separate `zerops_workflow action="complete"` MCP call. Many steps are mechanically sequential (load-knowledge must precede generate-import must precede import-services).

7. **Sequential verification**: `zerops_verify` takes one hostname per call. 5 services = 5 x 15-20s = 75-100s.

8. **Redundant log fetches**: `checkErrorLogs(5m)` and `checkErrorLogs2m()` make two separate API calls that could be one.

9. **Missing stage validation**: No warning for `start: zsc noop --silent` on stage, no env ref validation.

---

## 2. The Core Insight: Runtime-Centric Bootstrap

### 2.1 The Unit of Work

Bootstrap is NOT "create a list of services." Bootstrap IS:

> "Here's a runtime service to create, and here are the services it needs to connect to (create them first if they don't exist)."

The **runtime service** (+ its dev/stage pair) is the primary object. Managed services (databases, caches, object storage) are **dependencies** — they exist to serve runtimes.

### 2.2 The Real-World Flow

```
User: "Make me a CMS for XY"
         |
    CLARIFICATION
         |  LLM asks: What framework? What DB? Need caching?
         |  (happens BEFORE zerops_workflow starts)
         |  Output: clear intent + service requirements
         |
    BOOTSTRAP (5 steps)
         |  discover -> provision -> generate -> deploy -> verify
         |  Registry populated. CLAUDE.md updated.
         |
    HANDOFF
         |  Session: DONE. Registry: has topology.
         |  System prompt: auto-detects CONFORMANT.
         |  Routes to deploy workflow for subsequent changes.
         |
    DEVELOPMENT (repeatable)
         |  "add feature X" -> deploy workflow (mount -> edit -> deploy -> verify)
         |  "add caching" -> bootstrap with IsExisting=true target
```

### 2.3 Scenario Analysis

Four primary scenarios were identified and analyzed:

#### Scenario A: Fresh — Single Runtime + New Dependencies

User: "Create a PHP app with PostgreSQL"

```
Target: appdev/appstage (php-nginx@8.4)
Dependencies: db (postgresql@16) -> CREATE
```

Flow: Full 5-step bootstrap. Import creates all services, code generated from scratch, deployed, verified.

**Current model handles this**: Yes, but as a flat list without topology.

#### Scenario B: Add Runtime to Existing Infrastructure

User has appdev+db+cache. Wants: "Add a Node.js API"

```
Target: apidev/apistage (nodejs@22)
Dependencies: db (EXISTS), cache (EXISTS) -> just connect
```

Flow: discover finds existing services. Import creates only apidev+apistage. Code generated for new runtime, wired to existing env vars.

**Current model handles this**: No. The flat plan has no concept of "existing" vs "new." The detect step classifies as CONFORMANT and tells LLM to use deploy workflow instead. There's no path for adding a new runtime to existing infrastructure within bootstrap.

#### Scenario C: Add Managed Service to Existing Runtime

User has appdev+db. Wants: "Add Redis caching"

```
Target: appdev (IsExisting=true -- already deployed, needs update)
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

// StageHostname derives stage from dev: "appdev" -> "appstage"
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

`ValidateBootstrapTargets()` replaces `ValidateServicePlan()`:

- All hostnames pass `ValidateHostname()` (existing function in `platform` package)
- All types exist in live catalog (existing check)
- Dev/stage pairing enforced via `StageHostname()` convention
- `CREATE` dependencies must NOT exist in live services
- `EXISTS` dependencies MUST exist in live services
- Dependencies shared across targets: if target 1 creates `db` (CREATE), target 2 can reference it as EXISTS
- Managed service modes default to NON_HA (existing behavior)
- Remove `PlannedService` type entirely

### 3.3 How Each Step Works with Targets

The engine doesn't need fresh/incremental awareness. Same 5 steps, LLM adapts:

| Step | What happens |
|---|---|
| **discover** | LLM determines targets and dependencies from user intent + live API. Submits structured plan. |
| **provision** | Import YAML generated for CREATE deps + non-existing runtimes. Mount dev. Discover env vars. |
| **generate** | Code for each target runtime. IsExisting targets get config updates, not full code gen. |
| **deploy** | Deploy each target runtime (dev first, then stage). |
| **verify** | Verify ALL services (not just targets -- catches regressions). |

### 3.4 Multi-Runtime Handling

Multiple targets in one session. Dependencies are pooled:
- One `import.yml` covers all CREATE services (managed + runtime pairs)
- Generate iterates per target, each getting its dependency env vars
- Deploy iterates per target
- For 2+ targets, subagent pattern (one agent per target pair) used in generate/deploy

### 3.5 MCP Tool Input Changes

```go
// internal/tools/workflow.go — WorkflowInput changes
Plan []workflow.PlannedService  `json:"plan,omitempty"` // REMOVE
Plan []workflow.BootstrapTarget `json:"plan,omitempty"` // REPLACE

// internal/tools/workflow_bootstrap.go — routing changes
// OLD: input.Step == "plan" && len(input.Plan) > 0
// NEW: input.Step == "discover" && len(input.Plan) > 0

// internal/workflow/engine.go — BootstrapCompletePlan changes
// OLD: checks CurrentStepName() == "plan"
// NEW: checks CurrentStepName() == "discover"
// OLD: accepts []PlannedService
// NEW: accepts []BootstrapTarget
```

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

    // PERSISTENT -- survives across sessions
    Registry  map[string]*ServiceRecord `json:"registry"`

    // TRANSIENT -- cleared when session completes or resets
    Session   *WorkflowSession          `json:"session,omitempty"`
}

type ServiceRecord struct {
    Hostname  string   `json:"hostname"`
    Type      string   `json:"type"`                   // "bun@1.2", "postgresql@16"
    Role      string   `json:"role"`                   // "runtime-dev", "runtime-stage", "managed"
    PairWith  string   `json:"pairWith,omitempty"`     // dev<->stage link
    DependsOn []string `json:"dependsOn,omitempty"`    // managed service hostnames
    MountPath string   `json:"mountPath,omitempty"`    // SSHFS path (dev only)
    AddedBy   string   `json:"addedBy"`                // session ID that created it
    AddedAt   string   `json:"addedAt"`                // RFC3339
    Lifecycle string   `json:"lifecycle"`              // planned -> created -> deployed -> verified
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
- provision -> registry entries with `lifecycle: "created"`
- generate -> entries updated to `lifecycle: "configured"`
- deploy -> entries updated to `lifecycle: "deployed"`
- verify -> entries updated to `lifecycle: "verified"`

For `IsExisting=true` targets: existing registry entries get `DependsOn` updated.

### 4.5 Registry Reconciliation

At session start (`discover` step), reconcile registry against live API:
- Service in registry but deleted from Zerops -> remove from registry
- Service in Zerops but not in registry -> note as "externally added" in discover response
- Service in registry with matching live service -> keep (topology preserved)

This is a read-repair pattern. Cheap (one API call), prevents stale entries.

### 4.6 Session Lifecycle Changes

Current `InitSession` creates a new `WorkflowState` (destroys previous). Must change to:
- `InitSession`: preserves registry, creates new `Session` within existing state
- `ResetSession`: clears Session only, preserves registry
- `CompleteSession`: Session -> nil, registry stays

Key difference from current: `ResetSession` currently deletes the entire `zcp_state.json`. New behavior preserves registry.

### 4.7 State File Migration

Current state file has flat `sessionId, workflow, phase, etc.` at top level. New state nests them in `Session`. Need version check + migration on `LoadSession`:
```go
if state.Version == "1" { /* migrate flat -> nested */ }
```

### 4.8 Impact on Existing Code

Many functions access `state.SessionID`, `state.Phase`, etc. directly. These move to `state.Session.SessionID`:
- `engine.go`: all `BootstrapXxx()` methods
- `bootstrap_evidence.go`: `autoCompleteBootstrap()`
- `gates.go`: uses `sessionID` from state
- `session.go`: `InitSession`, `IterateSession`
- `instructions.go`: `buildWorkflowHint()`
- `tools/workflow.go`: `handleTransition`, `handleEvidence`
- `tools/workflow_bootstrap.go`: `handleBootstrapComplete`

Consider adding helper methods:
```go
func (s *WorkflowState) ActiveSessionID() string { ... }
func (s *WorkflowState) HasActiveSession() bool { ... }
```

### 4.9 Registry for BuildInstructions

`BuildInstructions()` in `internal/server/instructions.go` currently provides:
```
Current services:
- appdev (php-nginx@8.4) -- RUNNING
- appstage (php-nginx@8.4) -- RUNNING
- db (postgresql@16) -- RUNNING
```

With registry, it can provide:
```
Current services:
- appdev (php-nginx@8.4) -- RUNNING [dev, stage: appstage, deps: db+cache, mount: /var/www/appdev/]
- appstage (php-nginx@8.4) -- RUNNING [stage of appdev]
- db (postgresql@16) -- RUNNING [managed, used by: appdev]
- cache (valkey@7.2) -- RUNNING [managed, used by: appdev]
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

- **appdev/appstage** -> db (connectionString, host, port, user, password), cache (connectionString, host, port)

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
- `internal/workflow/engine.go` — hook into bootstrap completion (after verify step)
- `internal/server/instructions.go` — `BuildInstructions` reads registry for richer system prompt (add Section E between workflow hint and project summary)

---

## 6. Design: Step Consolidation (11 -> 5)

### 6.1 Step Mapping

| # | New Step | Old Steps | Category | Skippable |
|---|----------|-----------|----------|-----------|
| 0 | **discover** | detect + plan + load-knowledge | creative | no |
| 1 | **provision** | generate-import + import + mount + discover-envs | creative | no |
| 2 | **generate** | generate-code | creative | yes |
| 3 | **deploy** | deploy | branching | yes |
| 4 | **verify** | verify + report | fixed | no |

The `report` step is absorbed into `verify` + auto-CLAUDE.md write.

### 6.2 Two Fundamental Phases

**Phase A: Infrastructure (steps 0-1)** — create services, wire them up

**Phase B: Deploy & Activate (steps 2-4)** — write code, deploy, verify

This split reflects two distinct activities: infrastructure provisioning vs. application deployment.

### 6.3 Per-Step Detail

#### Step 0: discover

**Input**: User intent (natural language)
**Output**: `[]BootstrapTarget` with Resolution filled in

Actions:
1. Call `zerops_discover` to get all existing services
2. Read registry for existing topology
3. Parse user intent into runtime + dependency requirements
4. If unclear, guidance says: ask the user before proceeding
5. For each required service, check if it already exists -> resolution
6. Validate plan (hostnames, types, pairing, resolution consistency)
7. Submit structured plan via `zerops_workflow action="complete" step="discover" plan=[...]`

Hard checks:
- All hostnames pass `ValidateHostname()` (existing function)
- All types exist in live catalog
- Dev/stage pairing: every RuntimeTarget has valid StageHostname
- Resolution consistency: CREATE services don't exist, EXISTS services do exist
- Knowledge loaded for ALL target runtime types (not just any one)

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

Registry update: entries with `lifecycle: "created"`

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
- Dev entries: no healthCheck, no readinessCheck, deployFiles contains `.`
- All entries: `run.start` non-empty (except PHP/implicit-webserver runtimes -- use `hasImplicitWebServer()`)
- All entries: `run.ports` non-empty (except non-HTTP workers)
- Env ref validation: `${hostname_var}` patterns reference valid hostnames
- Stage entries: start is NOT `zsc noop --silent`

Registry update: entries updated to `lifecycle: "configured"`

#### Step 3: deploy

**Input**: Code on dev filesystem
**Output**: Running, verified services

Actions:
1. Deploy dev: self-deploy via SSH
2. Enable subdomain: `zerops_subdomain action="enable"` (explicit, not auto)
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

Registry update: entries updated to `lifecycle: "deployed"`

#### Step 4: verify

**Input**: All targets deployed
**Output**: Final report, registry updated, CLAUDE.md written

Actions:
1. Run `VerifyAll()` on all project services (parallel)
2. Update registry: all target services -> `lifecycle: "verified"`
3. Generate CLAUDE.md infrastructure section
4. Present final report with service URLs and statuses

Hard checks:
- `VerifyAll()` -- all services healthy or degraded (not unhealthy)
- Registry fully populated with verified entries
- CLAUDE.md written successfully

### 6.4 Skip Logic

Simplified from current `validateConditionalSkip()`:
- `generate` + `deploy` can be skipped (if targets are managed-only, but this is rare)
- `discover`, `provision`, `verify` cannot be skipped

### 6.5 Evidence Map Update

```go
var bootstrapEvidenceMap = map[string][]string{
    "recipe_review":   {"discover"},
    "discovery":       {"provision"},
    "dev_verify":      {"generate", "deploy", "verify"},
    "deploy_evidence": {"deploy"},
    "stage_verify":    {"verify"},
}
```

---

## 7. Design: Server-Side Hard Checks

### 7.1 Why Hard Checks Replace Attestation

The original 11-step design (visible in `../zcp-main`) provided safety through **granularity** -- separate detect, load-knowledge, and discover-envs steps acted as implicit validation gates:

| Original 11-step guarantee | Replaced by hard check |
|---|---|
| detect prevents duplicate services | **discover** hard check: `ListServices()` -> if CONFORMANT/NON_CONFORMANT, block with structured response |
| load-knowledge gates generate-import | **discover** hard check: verify `KnowledgeTracker.IsLoaded()` for ALL target runtime types |
| discover-envs gates generate-code | **provision** hard check: verify each managed service has non-empty env vars |
| verify doesn't trust deploy | **deploy** hard check: `ops.Verify()` on each service; **verify** hard check: `VerifyAll()` batch |

Hard checks are **strictly better**:
- 11-step attestations are strings LLM writes -- always "pass", never validated
- Hard checks run real API calls / file reads -- deterministic, cannot be faked
- Hard check failures return structured data (what failed, why, how to fix)

### 7.2 Types

```go
// internal/workflow/bootstrap_checks.go (new)
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

type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
```

### 7.3 Integration with BootstrapComplete()

```go
// internal/workflow/engine.go
func (e *Engine) BootstrapComplete(stepName, attestation string, checker StepChecker) (*BootstrapResponse, error) {
    // ... existing validation ...

    // Run hard checks if checker provided
    if checker != nil {
        checkResult, err := checker(ctx, state.Session.Bootstrap.Plan)
        if err != nil {
            return nil, fmt.Errorf("step check error: %w", err)
        }
        if checkResult != nil && !checkResult.Passed {
            resp := state.Session.Bootstrap.BuildResponse(state.ActiveSessionID(), state.Session.Intent)
            resp.CheckResult = checkResult
            resp.Message = fmt.Sprintf("Step %q: %s -- fix issues and retry", stepName, checkResult.Summary)
            return resp, nil  // NOT an error -- structured failure
        }
    }

    state.Session.Bootstrap.CompleteStep(stepName, attestation)
    e.updateRegistryLifecycle(state, stepName)
    // ...
}
```

Key design: Hard check failure is NOT a Go error. It returns a normal response with `CheckResult` populated. LLM sees what failed, fixes it, calls complete again.

### 7.4 Building Checkers in Tool Layer

```go
// internal/tools/workflow_checks.go (new)
func buildStepChecker(ctx context.Context, step string, client platform.Client,
    fetcher platform.LogFetcher, projectID string, tracker *ops.KnowledgeTracker) workflow.StepChecker {

    switch step {
    case "discover":
        return nil // plan validation handled by BootstrapCompletePlan
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
        return nil
    }
}
```

### 7.5 Registration Changes

```go
// internal/tools/workflow.go
func RegisterWorkflow(srv, client, projectID, cache, engine, tracker, logFetcher) { ... }
// ADD logFetcher parameter -> needed by hard checks

// internal/server/server.go
tools.RegisterWorkflow(s.server, s.client, projectID, stackCache, wfEngine, knowledgeTracker, s.logFetcher)
// ADD s.logFetcher parameter
```

### 7.6 Phase Gate Simplification

Phase gates (G0-G4) are redundant for bootstrap when hard checks exist. `autoCompleteBootstrap()` currently generates synthetic evidence and auto-transitions. Hard checks at step boundaries provide strictly stronger validation.

**Decision**: Keep gates for non-bootstrap workflows (deploy, debug, scale, configure). For bootstrap, hard checks ARE the gates.

### 7.7 Gaps Found in Original Plan (Resolved)

- **No dev/stage pairing enforcement**: Resolved -- discover hard check validates `StageHostname()` convention
- **Single-runtime knowledge check**: Resolved -- must be loaded for ALL target types, not just any one
- **Non-HTTP services**: Resolved -- conditional check based on service purpose (workers skip port check)
- **LLM-only retry limit**: Resolved -- deploy hard check has server-side `checkAttempts` counter

---

## 8. Design: Performance Improvements

### 8.1 Batch Verify (VerifyAll)

Current: `zerops_verify` takes one hostname per call. 5 services x 15-20s = 75-100s.
Proposed: `serviceHostname` optional. Without it -> verify ALL in parallel.

```go
type VerifyAllResult struct {
    Summary  string         `json:"summary"`
    Status   string         `json:"status"`   // healthy/degraded/unhealthy
    Services []VerifyResult `json:"services"`
}

func VerifyAll(ctx, client, fetcher, httpClient, projectID) (*VerifyAllResult, error) {
    // ListServices, get log access once, run Verify per service in parallel (errgroup, max 5)
}
```

Savings: 75s -> ~15s.

### 8.2 Verify Internal Speedup

Current: 3 separate log fetches per service (checkErrorLogs 5m, checkStartupDetected, checkErrorLogs2m).
Proposed: Batch to 2 + parallelize log and HTTP groups.

- Fetch 1: `severity=error, since=5m` -> derive `no_error_logs` + `no_recent_errors` (filter locally for 2m)
- Fetch 2: `search="listening|started|ready"` -> `startup_detected`
- Parallel: log group + HTTP group (health + status)

New `batchLogChecks()` in `verify_checks.go`. Parallel restructure in `verify.go`.

Savings: Per-service 15-20s -> 7-10s.

### 8.3 Build Polling Speedup

Current defaults: initial=3s, stepUp=10s, stepUpAfter=60s.
Proposed: initial=1s, stepUp=5s, stepUpAfter=30s.

### 8.4 Validation Improvements

- Stage-specific checks in `deploy_validate.go`: `start: zsc noop --silent` on stage -> warning
- Env var reference validation: new `ValidateEnvReferences()` -- parse `${hostname_var}`, validate hostname exists as service

### 8.5 Content Deduplication

Reference appendix in bootstrap.md for repeated content:
- /status endpoint specification (duplicated 3x)
- Hostname rules (duplicated 4x)
- Dev vs stage configuration matrix
- PHP runtime exceptions

Merge section tags to match 5-step model. Remove old 11-step section tags.

---

## 9. Phase Gate Analysis

### 9.1 Current Gate System

5 gates (G0-G4) require evidence before phase transitions:

| Gate | Transition | Required Evidence | Freshness |
|------|-----------|-------------------|-----------|
| G0 | INIT -> DISCOVER | recipe_review | -- |
| G1 | DISCOVER -> DEVELOP | discovery | 24h |
| G2 | DEVELOP -> DEPLOY | dev_verify | 24h |
| G3 | DEPLOY -> VERIFY | deploy_evidence | 24h |
| G4 | VERIFY -> DONE | stage_verify | 24h |

### 9.2 The Redundancy Problem

`autoCompleteBootstrap()` (bootstrap_evidence.go:18-83) generates synthetic evidence from step attestations and auto-transitions through ALL phases in one shot when bootstrap completes. Gates never actually gate anything for bootstrap -- they pass trivially with synthetic evidence.

Hard checks at step boundaries provide strictly stronger validation because they run real API calls and health checks.

### 9.3 Decision

Keep gates for non-bootstrap workflows (deploy, debug, scale, configure). For bootstrap, hard checks ARE the gates. `autoCompleteBootstrap()` can still auto-transition phases for compatibility, but no need for separate evidence when hard checks already validated.

---

## 10. Resolved Questions

### 10.1 Stage Hostname Convention

**Decision: Strict convention enforced.**

`devHostname` must end in "dev" -> stage = replace with "stage":
- `appdev` -> `appstage`
- `webdev` -> `webstage`
- `apidev` -> `apistage`

`StageHostname()` returns "" if DevHostname doesn't end in "dev" -> validation error.

### 10.2 IsExisting Runtime Updates

**Decision: Minimal wiring scope.**

When adding a managed service to an existing runtime:
- Update zerops.yml `envVariables` with new dependency refs
- Update `/status` endpoint to check new dependency
- Minimal code changes -- LLM determines extent based on context

### 10.3 Registry Cleanup

**Decision: Auto-remove on reconciliation.**

Reconciliation at session start removes stale entries. Git history preserves deleted service info if needed. Clean registry is more useful than historical clutter.

### 10.4 Multi-Target Ordering

**Decision: LLM-determined, engine validates.**

LLM submits targets in order. Engine validates cross-target dependency resolution (CREATE in target 1 satisfies EXISTS in target 2) but doesn't enforce ordering.

### 10.5 Non-Bootstrap Managed Service Addition

**Decision: Always bootstrap.**

Adding a managed service involves code changes (env vars, /status endpoint). Bootstrap handles this via `IsExisting=true` target. Simple configure-only additions (no code) remain outside bootstrap.

### 10.6 Shared Storage Dependencies

**Decision: Dependency with special handling.**

Shared storage is a `Dependency` like any other, but with extra steps in provision (mount in import.yml) and deploy (connect-storage after stage ACTIVE). The generate hard check validates `mount:` in zerops.yml `run:` section.

### 10.7 CLAUDE.md Location

**Decision: Section in CLAUDE.md with markers.**

`<!-- ZEROPS:BEGIN -->` / `<!-- ZEROPS:END -->` in project CLAUDE.md. Checked into git. Auto-read by Claude Code.

### 10.8 Evidence System Future

**Decision: Keep for audit trail, simplify for bootstrap.**

Evidence still records what was done (audit). For bootstrap, hard checks replace evidence as validation mechanism. Evidence is auto-generated from hard check results for gate compatibility.

---

## 11. Existing Functions to Reuse

| Function | File | Purpose |
|----------|------|---------|
| `ValidateHostname()` | `internal/platform/validate.go` | Hostname validation |
| `isManagedService()` | `internal/workflow/managed_types.go` | Type classification |
| `isManagedTypeWithLive()` | `internal/workflow/validate.go` | Live type classification |
| `hasImplicitWebServer()` | `internal/ops/deploy_validate.go` | PHP/nginx detection |
| `DetectProjectState()` | `internal/workflow/engine.go` | FRESH/CONFORMANT/NON_CONFORMANT |
| `ValidateZeropsYml()` | `internal/ops/deploy_validate.go` | Pre-deploy validation |
| `ops.Verify()` | `internal/ops/verify.go` | Per-service health checks |
| `checkServiceRunning()` | `internal/ops/verify_checks.go` | RUNNING/ACTIVE check |
| `checkHTTPHealth()` | `internal/ops/verify_checks.go` | GET /health check |
| `checkHTTPStatus()` | `internal/ops/verify_checks.go` | GET /status check |
| `aggregateStatus()` | `internal/ops/verify_checks.go` | Overall health from checks |
| `resolveSubdomainURL()` | `internal/ops/verify_checks.go` | Subdomain URL construction |
| `buildProjectSummary()` | `internal/server/instructions.go` | System prompt generation |
| `extractSection()` | `internal/workflow/bootstrap_guidance.go` | Section tag extraction |
| `KnowledgeTracker.IsLoaded()` | `internal/ops/knowledge_tracker.go` | Knowledge load validation |
| `saveState()` | `internal/workflow/session.go` | Atomic write (temp+rename) |
| `knowledge.ManagedBaseNames()` | `internal/knowledge/engine.go` | Managed type detection from live catalog |

---

## 12. Implementation Order

Dependency order (all in one pass, TDD at each step):

```
1. A: BootstrapTarget types + validation                    <- foundation (types used everywhere)
2. B: Registry + Session state model + migration            <- foundation (state used by steps)
3. F+G: Verify speedup + polling speedup                    <- independent, low risk (parallel)
4. H: Stage validation + env ref validation                 <- independent (parallel with 3)
5. E: Batch verify (VerifyAll)                              <- uses optimized Verify from #3
6. D: Hard checks (StepChecker)                             <- uses Verify/VerifyAll, registry
7. C: Step consolidation (11 -> 5)                          <- integrates hard checks, new types
8. I: CLAUDE.md recording + enriched BuildInstructions       <- uses registry from #2
9. J: Content deduplication                                  <- section names from #7
```

Items 3-4 run in parallel. Items 1+2 must be first. Items 6-7 are the core value.

---

## 13. Estimated Impact

| Metric | Before | After |
|--------|--------|-------|
| Workflow round-trips | 11 | 5 |
| Verify all services | N x 15-20s sequential | 1 x 7-10s parallel |
| Gate evidence quality | LLM attestation (always passes) | Real health checks (can fail) |
| Stage misconfig | Caught at runtime | Caught before deploy |
| Env var typos | Caught at runtime | Caught at generate step |
| Typical bootstrap time | 4-6 min | 2-3 min |
| Cross-session topology | Lost | Registry + CLAUDE.md |
| Scenario support | Fresh only | Fresh, add-runtime, add-managed, multi-runtime |
| Build poll responsiveness | 3s initial, 10s step-up | 1s initial, 5s step-up |

---

## 14. Design Decisions Summary

| Decision | Choice | Rationale |
|---|---|---|
| Primary abstraction | Runtime-centric (target + dependencies) | Matches real-world: bootstrap = set up a runtime with its dependencies |
| State model | Registry (persistent) + Session (transient) | Registry provides topology for handoff; session is ephemeral |
| Incremental support | Same engine, LLM adapts via registry context | No Scope/Action fields needed; simplest possible model |
| CLAUDE.md recording | Both registry AND CLAUDE.md section | Complementary: programmatic + durable handoff |
| What to record | Only immutable facts (topology) | Prevents staleness; live state via API |
| Step count | 11 -> 5 | Reduces round-trips while preserving decision points |
| Two phases | Infrastructure (discover+provision) + Deploy (generate+deploy+verify) | Reflects two distinct activities |
| Hard checks | Server-side, per-step, deterministic | Replaces trusted LLM attestations with real validation |
| Phase gates | Simplified for bootstrap (hard checks replace) | Gates are redundant with hard checks |
| Subdomain activation | Explicit (not auto-enable) | User decided: keep explicit |
| Stage naming | Convention: `*dev` -> `*stage`, strict enforcement | Simple, derivable, validation error if not matching |
| Registry cleanup | Auto-remove on reconciliation | Clean > historical; git preserves history |
| Multi-runtime | Multiple targets in one session | Efficient (one import), shared dependencies |
| Migration | Version check + auto-migrate flat -> nested | Handles existing state files |

---

## 15. Verification Plan

1. `go test ./internal/workflow/... -count=1 -v` — BootstrapTarget validation, registry persistence, 5-step model, hard checks, state migration, CLAUDE.md generation
2. `go test ./internal/ops/... -count=1 -v` — verify speedup, batch verify, validation improvements
3. `go test ./internal/tools/... -count=1 -v` — step checkers, batch verify tool, plan routing
4. `go test ./integration/... -count=1 -v` — full 5-step bootstrap (fresh + add-runtime + add-managed scenarios)
5. `go test ./... -count=1 -short` — full suite green
6. `make lint-fast` — no lint issues
7. Manual E2E:
   - Fresh: "PHP + PostgreSQL" -> 5 steps -> registry populated -> CLAUDE.md updated
   - Add runtime: "add Node.js API" -> existing deps resolved -> new runtime bootstrapped -> registry updated
   - Add managed: "add Valkey" -> IsExisting target -> cache created -> appdev redeployed -> CLAUDE.md regenerated
