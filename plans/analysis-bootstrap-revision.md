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
    Simple      bool   `json:"simple,omitempty"`     // true = no stage pair, single service
}

// StageHostname derives stage from dev: "appdev" -> "appstage"
// Returns "" when Simple=true or DevHostname doesn't end in "dev"
func (r RuntimeTarget) StageHostname() string {
    if r.Simple {
        return ""
    }
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
- Dev/stage pairing enforced via `StageHostname()` convention (skipped when `Simple=true`)
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

## 4. Design: State Model — Session-Only + Decision Metadata

### 4.1 Reality-First Principle

> **No document should describe project state.** The API is the source of truth. Every new session discovers reality from the live API. Documents that pretend to track "current state" are unmaintainable — anything can happen between sessions (services deleted via dashboard, scaled externally, env vars changed).

This is a key departure from earlier drafts that proposed a persistent registry. The registry was dropped because it creates a state document that must be reconciled with reality — an unmaintainable path.

### 4.2 What the API Already Provides (Sufficient)

The Zerops API provides on every session start:
- Service existence, types, statuses (`ListServices()`)
- Environment variables (`GetServiceEnv()`)
- Resource allocation, ports (`GetService()`)

What the API does NOT know (dev/stage pairing, dependencies, intent) is handled by:
- **Per-service decision metadata** (`.zcp/services/{hostname}.json`) — records decisions made during bootstrap
- **CLAUDE.md reflog** — historical record of what was bootstrapped and when
- Neither of these pretends to be current state. They are historical records.

### 4.3 State Model — Session Only

The `WorkflowState` structure remains flat and session-scoped. No persistent registry. No nesting change needed:

```go
type WorkflowState struct {
    Version   string           `json:"version"`
    SessionID string           `json:"sessionId"`
    ProjectID string           `json:"projectId"`
    Workflow  string           `json:"workflow"`
    Phase     Phase            `json:"phase"`
    Iteration int              `json:"iteration"`
    Intent    string           `json:"intent"`
    CreatedAt string           `json:"createdAt"`
    UpdatedAt string           `json:"updatedAt"`
    History   []PhaseTransition `json:"history"`
    Bootstrap *BootstrapState  `json:"bootstrap,omitempty"`
    // NO Registry — reality comes from API on every session start
}
```

No migration needed. No nesting. No `WorkflowSession` wrapper. The existing flat structure is sufficient because the state file IS the session — when the session ends (DONE + auto-reset), the state file goes away.

### 4.4 Per-Service Lifecycle — Session-Scoped

Per-service lifecycle tracking lives within `BootstrapState` during the bootstrap session:

```go
const (
    LifecyclePlanned    = "planned"    // after discover — service is in the plan
    LifecycleCreated    = "created"    // after provision — import succeeded
    LifecycleConfigured = "configured" // after generate — code/zerops.yml written
    LifecycleDeployed   = "deployed"   // after deploy — build ACTIVE
    LifecycleVerified   = "verified"   // after verify — health checks pass
    LifecycleReady      = "ready"      // terminal — READY_TO_DEVELOP
)
```

This tracks progress **within the session only**. When the session ends, lifecycle tracking ends. Next session discovers reality from API.

After each bootstrap step, the `BootstrapState.Plan.Targets` entries get their lifecycle updated:
- provision → targets with created dependencies get `lifecycle: "created"`
- generate → targets get `lifecycle: "configured"`
- deploy → targets get `lifecycle: "deployed"`
- verify → targets passing health checks get `lifecycle: "verified"` then `"ready"`

When ALL target services reach `ready`, bootstrap completes.

### 4.5 Per-Service Decision Metadata

Certain decisions made during bootstrap affect future behavior but are NOT state:
- Deploy flow chosen (SSH self-deploy vs other)
- Mode (standard vs simple)
- Dev workflow preferences
- Dependencies wired at bootstrap time
- Framework chosen

These are recorded as **per-service decision files** at `.zcp/services/{hostname}.json`:

```json
{
  "hostname": "appdev",
  "type": "bun@1.2",
  "mode": "standard",
  "stageHostname": "appstage",
  "deployFlow": "ssh",
  "dependencies": ["db", "cache"],
  "bootstrapSession": "a1b2c3d4",
  "bootstrappedAt": "2026-03-03T10:00:00Z",
  "decisions": {
    "devWorkflow": "mount-edit-deploy",
    "framework": "hono"
  }
}
```

Key properties:
- **Written once at bootstrap completion.** Not updated continuously.
- **Records decisions, not state.** "We chose SSH deploy flow" is a decision. "Service is RUNNING" is state (from API).
- **Optional.** If the file doesn't exist (service created via dashboard), LLM discovers everything from API and makes fresh decisions.
- **Informs, doesn't control.** LLM reads it as context and can choose differently if circumstances changed.

**Difference from the dropped registry:** The registry was a project-level state document trying to be the source of truth for topology. Decision metadata files are per-service historical records ("we decided X") — they don't need reconciliation because decisions don't go stale.

Implementation: `internal/workflow/service_meta.go` (new, ~40 lines) — `WriteServiceMeta()`, `ReadServiceMeta()`

### 4.6 BuildInstructions — Live API Only

`BuildInstructions()` in `internal/server/instructions.go` continues to:
1. Call `ListServices(ctx, projectID)` — live API, always fresh
2. Classify project state (FRESH/CONFORMANT/NON_CONFORMANT)
3. Show active workflow hint if session exists

No enrichment from stored topology. The system prompt shows what the API returns, period. If `.zcp/services/` metadata files exist, the deploy workflow guidance can read them for context — but they do NOT affect the system prompt.

### 4.7 Session Lifecycle — No Changes Needed

Current `InitSession`, `LoadSession`, `ResetSession`, `IterateSession` remain unchanged:
- `InitSession`: creates new state file (as before)
- `ResetSession`: deletes state file (as before — no registry to preserve)
- Auto-reset on DONE: continues to work (as before)

No migration. No helper methods needed. The flat structure is the right abstraction.

---

## 5. Design: Post-Bootstrap CLAUDE.md Reflog

### 5.1 Core Principle: Reflog, Not State Document

> CLAUDE.md entries are a **reflog** — historical records of what happened, like `git reflog`. They do NOT describe current state. Anything can happen between sessions: services deleted via dashboard, dependencies changed, env vars modified. The reflog says "on date X, this was bootstrapped." The LLM treats it as a hint and verifies current reality via `zerops_discover`.

This replaces the earlier "topology snapshot with regeneration" approach. No snapshot. No regeneration. No reconciliation. Just append-only history.

### 5.2 What Gets Written

After bootstrap verify step passes, append to CLAUDE.md:

```markdown
<!-- ZEROPS:REFLOG -->
### 2026-03-03 — Bootstrap: Bun API + PostgreSQL + Valkey

- **Runtime:** appdev/appstage (bun@1.2)
- **Dependencies:** db (postgresql@16), cache (valkey@7.2)
- **Evidence:** .zcp/evidence/{sessionID}/
- **Mode:** standard (dev+stage)

> This is a historical record. Verify current state via `zerops_discover`.
<!-- /ZEROPS:REFLOG -->
```

### 5.3 What Does NOT Get Written

- No service status, no URLs, no env var values, no env var names
- No "current topology" table
- No attempt to stay in sync with reality
- No regeneration/reconciliation logic

### 5.4 When It Gets Written

- Once, at the end of a successful bootstrap (after verify hard checks pass)
- Never updated or regenerated
- If user runs another bootstrap (add new runtime), a new reflog entry is appended below
- Each entry is independent — multiple entries accumulate like a git log

### 5.5 Where It Gets Written

- Project's `CLAUDE.md` (checked into git — visible to team, survives machine changes)
- Alternatively `CLAUDE.local.md` if user prefers not to commit it

### 5.6 How the LLM Uses It

1. LLM starts a new session, reads CLAUDE.md (automatically loaded by Claude Code)
2. Sees reflog: "March 3rd: bootstrapped bun+postgres"
3. Calls `zerops_discover` to verify what actually exists today
4. If services still exist — the reflog provides useful context (intent, relationships, mode)
5. If services were deleted — the reflog is just history, LLM works with current reality

The reflog gives the LLM a **starting hypothesis** ("these services were probably set up together") that it validates against the live API. This is faster than discovering everything from scratch, but doesn't create false confidence.

### 5.7 Why Reflog Instead of Snapshot

| Approach | Problem |
|---|---|
| **Snapshot** (previous design) | Must be regenerated when state changes. If regeneration doesn't happen (external changes), snapshot lies. Creates reconciliation complexity. |
| **Reflog** (current design) | Never lies — "on March 3rd, X happened" is always true. No regeneration needed. No reconciliation. Trivially simple. |

### 5.8 Relationship to Decision Metadata

- **CLAUDE.md reflog** = human/LLM-readable history, checked into git, gives context to anyone reading the project
- **`.zcp/services/{hostname}.json`** = machine-readable decisions, local to workstation, informs ZCP tooling

They serve different audiences. The reflog is for LLM context at session start. Decision metadata is for workflow tooling to provide appropriate guidance.

### 5.9 Implementation

- `internal/workflow/reflog.go` (new, ~50 lines) — `AppendReflogEntry(claudeMDPath, intent, targets, sessionID, timestamp) error`
- Called from the bootstrap completion path (after verify step passes)
- Pure append, no markers for replacement — each bootstrap is a new entry

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

**Internal sequence (knowledge first, then clarify):**

The discover step has an internal flow that the LLM follows via guidance:

1. **Gather context**: Call `zerops_discover` to inspect current project state
2. **Load knowledge**: Call `zerops_knowledge` for runtime briefings and recipes relevant to user intent
3. **Form understanding**: Based on user intent + loaded knowledge + project state, form a preliminary picture
4. **Clarify with user** (if needed): Armed with context, ask informed questions:
   - RUNTIME: language + framework — use loaded recipes to suggest specific options
   - MANAGED SERVICES: databases, caches, storage
   - MODE: standard (dev+stage, recommended) or simple (single service)
   - Only ask if ambiguous. If intent is clear, proceed without asking.
5. **Submit structured plan**: `zerops_workflow action="complete" step="discover" plan=[...]`

The clarification is NOT a separate step — it's guidance content within discover. The LLM asks informed questions (after loading knowledge), not cold generic ones.

Hard checks:
- All hostnames pass `ValidateHostname()` (existing function)
- All types exist in live catalog
- Dev/stage pairing: every RuntimeTarget has valid StageHostname (unless `Simple=true`)
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

Lifecycle update: target services → `lifecycle: "created"` (session-scoped)
**Auto-completable**: Yes — when all hard checks pass, step auto-completes without LLM calling `action="complete"`

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

Lifecycle update: target services → `lifecycle: "configured"` (session-scoped)
**NOT auto-completable**: Creative step — LLM decides when code is ready, calls `action="complete"`. Hard check validates but does not auto-confirm.

**Mode-aware guidance**: The detailed guide is filtered by plan mode. Standard mode → only dev+stage template. Simple mode → only single-service template. Prevents LLM from mixing templates.

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

Lifecycle update: target services → `lifecycle: "deployed"` (session-scoped)
**NOT auto-completable**: Creative/branching step — LLM manages deploy order and iteration. Build status (ACTIVE/BUILD_FAILED) is deterministic, but verify is separate.

**Creative → Verify flow**: The LLM's creative work (generate + deploy) is always followed by objective verification. The LLM decides "I think it's done" and calls `action="complete"` for deploy. Then verify objectively validates. This pattern ensures creative judgment + hard verification.

#### Step 4: verify

**Input**: All targets deployed
**Output**: Final report, service metadata written, CLAUDE.md reflog written

Actions:
1. Run `VerifyAll()` on all project services (parallel)
2. Update target services → `lifecycle: "verified"` then `"ready"` (session-scoped)
3. Write per-service decision metadata to `.zcp/services/{hostname}.json`
4. Append reflog entry to CLAUDE.md
5. Present final report with service URLs and statuses

Hard checks:
- `VerifyAll()` -- all services healthy or degraded (not unhealthy)
- Verify checks endpoints against predefined expectations (/health returns 2xx, /status returns JSON with connection checks)
- Cross-reference: /status `connections` keys should match plan dependencies (warning if mismatch)

**Auto-completable**: Yes — when all hard checks pass, step auto-completes and triggers completion sequence (metadata write, reflog write, session → DONE)

### 6.4 Skip Logic

Simplified from current `validateConditionalSkip()`:
- `generate` + `deploy` can be skipped (if targets are managed-only, but this is rare)
- `discover`, `provision`, `verify` cannot be skipped

### 6.5 Step Completion Model

| Step | Completion | Evidence Source |
|------|-----------|----------------|
| **discover** | Explicit — LLM submits structured plan | Plan validation result |
| **provision** | Auto — hard checks pass after tool calls | Import result + ListServices + env var presence |
| **generate** | Explicit — LLM says code is ready | zerops.yml structural validation |
| **deploy** | Explicit — LLM manages iteration loop | Deploy result (build status) |
| **verify** | Auto — VerifyAll() passes | Health check results |

Evidence is populated from actual tool results (not LLM attestation strings). The evidence map for audit trail:

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

### 7.3 Two Completion Modes

Hard checks serve two distinct purposes depending on step type:

**Auto-completion (mechanical steps: provision, verify):**
Hard checks run server-side. When all pass, the step auto-completes — the LLM does NOT need to call `action="complete"`. The response includes the next step's guidance, reducing round-trips.

**Validation (creative steps: discover, generate, deploy):**
LLM calls `action="complete"` when it believes work is done. Hard checks validate the creative output. If checks fail, the response includes structured failure data — the LLM fixes and retries. If checks pass, step completes.

### 7.4 Integration with BootstrapComplete()

```go
// internal/workflow/engine.go
func (e *Engine) BootstrapComplete(stepName string, checker StepChecker) (*BootstrapResponse, error) {
    // ... existing validation ...

    // Run hard checks if checker provided
    if checker != nil {
        checkResult, err := checker(ctx, state.Bootstrap.Plan)
        if err != nil {
            return nil, fmt.Errorf("step check error: %w", err)
        }
        if checkResult != nil && !checkResult.Passed {
            resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent)
            resp.CheckResult = checkResult
            resp.Message = fmt.Sprintf("Step %q: %s -- fix issues and retry", stepName, checkResult.Summary)
            return resp, nil  // NOT an error -- structured failure
        }
    }

    state.Bootstrap.CompleteStep(stepName, checkResult.Summary)
    e.updateLifecycle(state, stepName) // session-scoped lifecycle update
    // ...
}
```

Key design: Hard check failure is NOT a Go error. It returns a normal response with `CheckResult` populated. LLM sees what failed, fixes it, calls complete again.

### 7.5 Creative → Verify Pattern

Creative steps (generate, deploy) are validated by the LLM's self-assessment AND subsequent hard verification:

```
LLM generates code → LLM: "I think it's ready" → action="complete" step="generate"
  → Hard check validates zerops.yml structure → pass/fail

LLM deploys → LLM: "build looks good" → action="complete" step="deploy"
  → Hard check validates build status ACTIVE → pass/fail

Both creative steps followed by:
  → Verify step: VerifyAll() checks endpoints against expectations
  → /health returns 2xx, /status returns JSON with connection checks
  → If verify fails → iteration loop (fix → redeploy → re-verify)
```

This ensures creative judgment is always followed by objective verification. The LLM can't skip verify — it's a non-skippable step.

### 7.6 Building Checkers in Tool Layer

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

### 7.7 Registration Changes

```go
// internal/tools/workflow.go
func RegisterWorkflow(srv, client, projectID, cache, engine, tracker, logFetcher) { ... }
// ADD logFetcher parameter -> needed by hard checks

// internal/server/server.go
tools.RegisterWorkflow(s.server, s.client, projectID, stackCache, wfEngine, knowledgeTracker, s.logFetcher)
// ADD s.logFetcher parameter
```

### 7.8 Phase Gate Simplification

Phase gates (G0-G4) are redundant for bootstrap when hard checks exist. `autoCompleteBootstrap()` currently generates synthetic evidence and auto-transitions. Hard checks at step boundaries provide strictly stronger validation.

**Decision**: Keep gates for non-bootstrap workflows (deploy, debug, scale, configure). For bootstrap, hard checks ARE the gates.

### 7.9 Failure Handling

- **Partial success (3/5 imported, 2 failed):** Evidence records actual `Failed` count from tool result. Gate blocks. LLM sees per-service details.
- **Timeout:** Treated as `inconclusive` — gates block. LLM must investigate manually.
- **Deploy ACTIVE but app crashes:** Caught by separate verify step (not auto-confirmed from deploy).
- **Partial verify (2/3 healthy, 1 unhealthy):** Bootstrap can complete with partial success if at least one target is verified. Failed services recorded in evidence. Reflog reflects actual outcomes.

### 7.10 What Gets Eliminated

- `autoCompleteBootstrap()` in its current form (synthetic evidence with Failed=0 always)
- Attestation string requirement for mechanical steps (provision, verify)
- The 5 separate phase gate transitions for bootstrap (hard checks at step boundaries provide strictly stronger validation)

### 7.11 What Stays

- Gate system for non-bootstrap workflows (deploy, debug, scale, configure)
- Evidence files as audit trail (now populated from real tool results, not synthetic data)
- Phase transitions (still INIT→DONE, but driven by hard check results)

### 7.12 Gaps Found in Original Plan (Resolved)

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

**Decision: Strict convention enforced (standard mode). Skipped in simple mode.**

Standard mode: `devHostname` must end in "dev" → stage = replace with "stage":
- `appdev` → `appstage`
- `webdev` → `webstage`
- `apidev` → `apistage`

`StageHostname()` returns "" if `Simple=true` OR DevHostname doesn't end in "dev" → validation error in standard mode, valid in simple mode.

### 10.2 IsExisting Runtime Updates

**Decision: Minimal wiring scope.**

When adding a managed service to an existing runtime:
- Update zerops.yml `envVariables` with new dependency refs
- Update `/status` endpoint to check new dependency
- Minimal code changes -- LLM determines extent based on context

### 10.3 ~~Registry Cleanup~~ — REMOVED

Registry dropped entirely. No reconciliation needed. API is source of truth on every session start.

### 10.4 Multi-Target Ordering

**Decision: LLM-determined, engine validates.**

LLM submits targets in order. Engine validates cross-target dependency resolution (CREATE in target 1 satisfies EXISTS in target 2) but doesn't enforce ordering.

### 10.5 Non-Bootstrap Managed Service Addition

**Decision: Always bootstrap.**

Adding a managed service involves code changes (env vars, /status endpoint). Bootstrap handles this via `IsExisting=true` target. Simple configure-only additions (no code) remain outside bootstrap.

### 10.6 Shared Storage Dependencies

**Decision: Dependency with special handling.**

Shared storage is a `Dependency` like any other, but with extra steps in provision (mount in import.yml) and deploy (connect-storage after stage ACTIVE). The generate hard check validates `mount:` in zerops.yml `run:` section.

### 10.7 CLAUDE.md Approach

**Decision: Reflog (append-only), not snapshot.**

Each bootstrap appends a historical entry to CLAUDE.md. No markers for replacement. No regeneration. The entry is a point-in-time record ("on date X, this was bootstrapped"). LLM verifies current state via `zerops_discover`.

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

### New Files

| File | Purpose | ~Lines |
|------|---------|--------|
| `internal/workflow/reflog.go` | `AppendReflogEntry()` — append bootstrap record to CLAUDE.md | ~50 |
| `internal/workflow/service_meta.go` | `WriteServiceMeta()`, `ReadServiceMeta()` — per-service decision files | ~40 |
| `internal/workflow/bootstrap_checks.go` | `StepCheckResult`, `StepCheck`, `StepChecker` types + check implementations | ~150 |
| `internal/tools/workflow_checks.go` | `buildStepChecker()` — constructs checkers in tool layer | ~80 |

---

## 12. Implementation Order

Dependency order (all in one pass, TDD at each step):

```
1. A: BootstrapTarget types + validation                     <- foundation (types used everywhere)
2. B: Session-scoped ServiceRecord lifecycle                  <- within BootstrapState
3. F+G: Verify speedup + polling speedup                     <- independent, low risk (parallel)
4. H: Stage validation + env ref validation                  <- independent (parallel with 3)
5. E: Batch verify (VerifyAll)                               <- uses optimized Verify from #3
6. D: Hard checks (StepChecker) + auto-completion            <- uses Verify/VerifyAll
7. C: Step consolidation (11 -> 5)                           <- integrates hard checks, new types
8. K: Per-service decision metadata (.zcp/services/)         <- written at bootstrap completion
9. I: CLAUDE.md reflog writing                               <- append-only, after verify passes
10. L: Clarification guidance in discover step               <- content change (knowledge first)
11. M: Mode-aware generate guidance                          <- content change
12. J: Content deduplication                                 <- section names from #7
```

Items 3-4 run in parallel. Item 1 must be first. Items 6-7 are the core value. Items 8-12 are outputs/content.

---

## 13. Estimated Impact

| Metric | Before | After |
|--------|--------|-------|
| Workflow round-trips | 11 explicit | 2-3 explicit (creative) + 2 auto (mechanical) |
| Verify all services | N x 15-20s sequential | 1 x 7-10s parallel |
| Gate evidence quality | LLM attestation (always passes) | Real hard checks (can fail) |
| Stage misconfig | Caught at runtime | Caught before deploy |
| Env var typos | Caught at runtime | Caught at generate step |
| Typical bootstrap time | 4-6 min | 2-3 min |
| Cross-session context | Lost | Reflog in CLAUDE.md + decision metadata in .zcp/services/ |
| State management | Persistent registry (stale risk) | API is source of truth (always fresh) |
| Scenario support | Fresh only | Fresh, add-runtime, add-managed, multi-runtime |
| Build poll responsiveness | 3s initial, 10s step-up | 1s initial, 5s step-up |

---

## 14. Design Decisions Summary

| Decision | Choice | Rationale |
|---|---|---|
| Primary abstraction | Runtime-centric (target + dependencies) | Matches real-world: bootstrap = set up a runtime with its dependencies |
| State model | Session-only (no persistent registry) | API is source of truth. No state documents. Reality discovered on every session start. |
| Per-service decisions | `.zcp/services/{hostname}.json` | Records decisions (deploy flow, mode), not state. Historical, not current. |
| CLAUDE.md approach | Reflog (append-only history) | "On date X, this happened." Not a snapshot. No regeneration. |
| Step count | 11 → 5 | Reduces round-trips while preserving decision points |
| Step completion | Auto (mechanical) + Explicit (creative) | Provision/verify auto-complete. Discover/generate/deploy need LLM. |
| Creative → Verify | Creative steps always followed by hard verification | LLM judgment + objective validation = reliable outcomes |
| Clarification timing | Knowledge first, then ask user | LLM loads knowledge before asking informed questions |
| Full vs Simple mode | `RuntimeTarget.Simple` field | Standard (dev+stage) default. Simple (no stage) when user wants it. |
| Hard checks | Server-side, per-step, deterministic | Replaces trusted LLM attestations with real validation |
| Phase gates | Simplified for bootstrap (hard checks replace) | Gates are redundant with hard checks |
| Subdomain activation | Explicit (not auto-enable) | User decided: keep explicit |
| Stage naming | Convention: `*dev` → `*stage`, strict enforcement | Simple, derivable. Skipped when `Simple=true`. |
| Multi-runtime | Multiple targets in one session | Efficient (one import), shared dependencies |
| Migration | Not needed | State model stays flat — no structural change |

---

## 15. Verification Plan

1. `go test ./internal/workflow/... -count=1 -v` — BootstrapTarget validation, session-scoped lifecycle, 5-step model, hard checks, service metadata, reflog
2. `go test ./internal/ops/... -count=1 -v` — verify speedup, batch verify, validation improvements
3. `go test ./internal/tools/... -count=1 -v` — step checkers, auto-completion, batch verify tool, plan routing
4. `go test ./integration/... -count=1 -v` — full 5-step bootstrap (fresh + add-runtime + add-managed scenarios)
5. `go test ./... -count=1 -short` — full suite green
6. `make lint-fast` — no lint issues
7. Manual E2E:
   - Fresh: "PHP + PostgreSQL" → 5 steps → reflog written → decision metadata created → new session discovers from API
   - Add runtime: "add Node.js API" → existing deps resolved → new runtime bootstrapped → new reflog entry appended
   - Add managed: "add Valkey" → IsExisting target → cache created → appdev redeployed → reflog entry appended
   - External change: delete service via dashboard → new session sees current state from API, reflog is just history
