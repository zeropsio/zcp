# ZCP Workflow Evolution — Knowledge Document (Stress-Tested)

Analysis of current workflow system, context delivery, and validated designs for dynamic flow routing, post-bootstrap strategies, and context optimization. Includes stress-test results from 30+ edge case simulations.

**Extended 2026-03-07**: Deep knowledge delivery analysis by 10 specialized agents across 6 dimensions (progressive disclosure, semantic dedup, gate intelligence, pull-based delivery, context compression, LLM behavioral patterns). Sections 8, 10, and new sections 14-17 contain the integrated results.

---

## 1. Current Architecture Overview

### 1.1 Core Package Map

| Package | File | Lines | Responsibility |
|---------|------|-------|---------------|
| `internal/runtime` | `runtime.go` | 30 | Container detection via `serviceId` env var |
| `internal/server` | `server.go` | 119 | MCP server setup, tool registration |
| `internal/server` | `instructions.go` | 137 | System prompt construction (4 sections) |
| `internal/workflow` | `engine.go` | 341 | Session lifecycle, phase transitions |
| `internal/workflow` | `state.go` | 291 | WorkflowState persistence, phase enum |
| `internal/workflow` | `session.go` | 172 | Session creation, registry management |
| `internal/workflow` | `bootstrap.go` | 172 | Bootstrap conductor, BootstrapResponse |
| `internal/workflow` | `bootstrap_steps.go` | 94 | 5 step definitions with inline guidance |
| `internal/workflow` | `bootstrap_guidance.go` | 34 | `<section>` extraction from bootstrap.md |
| `internal/workflow` | `bootstrap_evidence.go` | 149 | Auto-completion, ServiceMeta writes, reflog |
| `internal/workflow` | `gates.go` | 160 | Phase gate checks (G0-G4), evidence freshness |
| `internal/workflow` | `validate.go` | 224 | Plan validation (hostnames, types, stages) |
| `internal/workflow` | `managed_types.go` | 81 | Project state detection (FRESH/CONFORMANT/NON_CONFORMANT) |
| `internal/workflow` | `service_meta.go` | 67 | Per-service historical metadata (decisions map) |
| `internal/workflow` | `registry.go` | 150+ | Multi-process session registry with file locking |
| `internal/tools` | `workflow.go` | 334 | MCP tool handler, action dispatch |
| `internal/tools` | `convert.go` | ~70 | `jsonResult()`, `textResult()`, `convertError()` |
| `internal/tools` | `guard.go` | — | Workflow guard (requires active session) |
| `internal/tools` | `next_actions.go` | — | 19 NextAction constants for follow-up hints |
| `internal/content` | `content.go` | 58 | Go embed.FS for workflows + templates |
| `internal/content` | `workflows/*.md` | 6 files | Guidance documents (bootstrap, deploy, debug, scale, configure, monitor) |
| `internal/knowledge` | `engine.go` | — | BM25 search, 4 delivery modes |

### 1.2 Runtime Detection

**File**: `internal/runtime/runtime.go` (30 lines)

```go
type Info struct {
    InContainer bool
    ServiceName string  // from env `hostname`
    ServiceID   string  // from env `serviceId`
    ProjectID   string  // from env `projectId`
}
```

- Single signal: `serviceId` env var presence = container mode
- No VPN detection, no local dev mode, no Mode enum
- Resolved once at startup, passed as value

### 1.3 Workflow Types

**Immediate (stateless)**: `debug`, `scale`, `configure`, `monitor`
- Return markdown guidance text only
- No session, no phases, no state

**Orchestrated (stateful)**: `bootstrap`, `deploy`
- Create session in `.zcp/state/sessions/{sessionID}.json`
- Register in `.zcp/state/registry.json`
- Phase gates with evidence requirements

### 1.4 Bootstrap: 5 Steps

| # | Step | Category | Skippable | Tools |
|---|------|----------|-----------|-------|
| 0 | discover | fixed | no | zerops_discover, zerops_knowledge, zerops_workflow |
| 1 | provision | fixed | no | zerops_import, zerops_process, zerops_discover, zerops_mount |
| 2 | generate | creative | if no runtime | zerops_knowledge |
| 3 | deploy | branching | if no runtime | zerops_deploy, zerops_subdomain, zerops_verify, zerops_logs, zerops_manage, zerops_mount |
| 4 | verify | fixed | no | zerops_discover, zerops_verify |

### 1.5 Phase System (6 phases)

```
INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
```

Gates G0-G4 require evidence with 24h freshness (except G0 recipe_review).

### 1.6 Key Data Structures

**WorkflowState** (persisted per session):
```
version, sessionId, pid, projectId, workflow, phase, iteration,
intent, createdAt, updatedAt, history[]PhaseTransition, bootstrap *BootstrapState
```

**BootstrapState**:
```
active, currentStep, steps[]BootstrapStep, plan *ServicePlan, discoveredEnvVars map[string][]string
```

**ServicePlan** (submitted during discover step):
```
targets: []BootstrapTarget{
  runtime: {devHostname, type, isExisting, simple}
  dependencies: []Dependency{hostname, type, mode(HA/NON_HA), resolution(CREATE/EXISTS/SHARED)}
}
```

**ServiceMeta** (written after bootstrap, `.zcp/state/services/{hostname}.json`):
```
hostname, type, mode, stageHostname, deployFlow, dependencies[],
bootstrapSession, bootstrappedAt, decisions map[string]string
```

`decisions` map is the extensible store for per-service choices — currently unpopulated.
`deployFlow` field exists but is never populated in production (only in test fixtures as `"ssh"`).

### 1.7 Project State Detection

**File**: `internal/workflow/managed_types.go`

Three states based on service inventory:
- **FRESH**: No runtime services
- **CONFORMANT**: Runtime services with dev+stage naming pattern
- **NON_CONFORMANT**: Runtime services without dev+stage pattern

Managed service list (not runtime): postgresql, mariadb, valkey, keydb, elasticsearch, meilisearch, rabbitmq, kafka, nats, clickhouse, qdrant, typesense, object-storage, shared-storage.

---

## 2. Context Flow: MCP → LLM

### 2.1 System Prompt (injected at server init)

**File**: `internal/server/instructions.go` — `BuildInstructions()`

**CRITICAL CONSTRAINT**: System prompt is computed ONCE at MCP server startup (`New()` at `server.go:49`). It is passed to `mcp.ServerOptions{Instructions: ...}` and frozen for the lifetime of the MCP connection. Mid-session changes (strategy updates, bootstrap completion) are NOT reflected in the system prompt until MCP restarts.

**Section A** — Base + Routing (static, ~260 chars):
```
ZCP manages Zerops PaaS infrastructure.
IMPORTANT: All Zerops operations are managed through workflow sessions...
Workflow commands:
- Create services: zerops_workflow action="start" workflow="bootstrap"
- Deploy code: zerops_workflow action="start" workflow="deploy"
- Debug/Scale/Configure/Monitor: zerops_workflow/zerops_discover
```

**Section B** — Workflow Hint (dynamic from registry):
- Reads `.zcp/state/registry.json`
- For bootstrap: `Active workflow: bootstrap phase=INIT (step 2/5: provision)`
- Empty on error (graceful fallback)

**Section C** — Runtime Context (conditional):
- Only if in container: `"You are running inside Zerops service '{name}'."`

**Section D** — Project Summary (dynamic from API):
- Lists services: `- appdev (nodejs@22) — ACTIVE`
- Detects state and provides hardcoded routing:
  - FRESH → `REQUIRED: zerops_workflow action="start" workflow="bootstrap"`
  - CONFORMANT → deploy or bootstrap for new stacks
  - NON_CONFORMANT → bootstrap for new services

**Total system prompt**: ~500-1000 chars depending on project state.

**Problem**: Routing in Section D is hardcoded switch/case (`instructions.go:119-132`). No awareness of per-service deploy strategies, no dynamic flow offerings.

### 2.2 Tool Descriptions (15 tools, ~5000 chars total)

Each tool registered with `mcp.NewTool()` including full description and JSON schema. Key descriptions:

| Tool | Description highlights |
|------|----------------------|
| `zerops_workflow` | Action-based dispatch: start, complete, skip, status, transition, evidence, reset, iterate, list |
| `zerops_discover` | Primary env var reader (includeEnvs=true) |
| `zerops_knowledge` | 4 modes: briefing, scope, query, recipe |
| `zerops_deploy` | REQUIRES active workflow. SSH-based. Blocks until build completes |
| `zerops_import` | REQUIRES active workflow. Validates types before API call |
| `zerops_verify` | 5 check types: service_running, error_logs, startup_detection, http_root, status_endpoint |
| `zerops_subdomain` | Critical: enableSubdomainAccess in import != active routing. Must call enable after deploy |

### 2.3 Bootstrap Response Structure

When `zerops_workflow action="start" workflow="bootstrap"`:

```json
{
  "sessionId": "hex16",
  "intent": "...",
  "progress": {"total": 5, "completed": 0, "steps": [...]},
  "current": {
    "name": "discover",
    "index": 0,
    "category": "fixed",
    "guidance": "Short 5-10 line inline text from bootstrap_steps.go",
    "tools": ["zerops_discover", "zerops_knowledge", "zerops_workflow"],
    "verification": "Success criteria string",
    "detailedGuide": "Full <section name='discover'> extract from bootstrap.md (~180 lines)",
    "priorContext": {"attestations": [...], "plan": null},
    "planMode": "standard|simple"
  },
  "availableStacks": "Formatted live stack catalog",
  "message": "Bootstrap started..."
}
```

**Dual delivery problem**: LLM receives BOTH `guidance` (100 words) AND `detailedGuide` (2000+ words) for the same step. Inline guidance is always a subset of detailedGuide — no unique content. LLM doesn't know which is authoritative.

### 2.4 Knowledge Delivery (4 modes)

**File**: `internal/tools/knowledge.go`

| Mode | Input | Output | Token estimate (corrected) |
|------|-------|--------|---------------|
| scope="infrastructure" | — | `universals.md` (1,032 tokens) + `core.md` (7,935 tokens) concatenated | **~8,967** |
| briefing | runtime + services | 7-layer composition: live stacks → runtime guide → recipe hints → service cards → wiring → decisions → version check | **~1,500-4,000** |
| query | search text | BM25 results: [{uri, title, score, snippet}] with title 2.0x boost, keywords 1.5x boost | ~500-2,000 |
| recipe | name | universals (prepended) + auto-detected runtime guide + full recipe markdown | **~2,000-4,500** |

**Embedded Knowledge Base** (total: ~168 KB, ~45-60K tokens):
- `themes/`: core.md (~8K), universals.md (~1K), services.md (~4K), operations.md (~3.5K)
- `runtimes/`: 10+ per-runtime guides (~700-1,200 tokens each)
- `recipes/`: 29 pre-built framework configs (~500-1,500 tokens each)
- `guides/`: 18 topic guides (~500-2,000 tokens each)
- `decisions/`: 2-3 architectural choice docs

**Briefing 7-Layer Composition** (`internal/knowledge/briefing.go`):
1. Live service stacks (from API cache, ~200 tokens)
2. Runtime guide (e.g., `runtimes/nodejs.md`, ~800 tokens)
3. Matching recipe hints (list, ~200 tokens — requires separate `recipe` call for content)
4. Service cards (H2 extracts from `themes/services.md`, ~300 tokens each)
5. Wiring patterns (cross-service reference syntax, ~400 tokens)
6. Decision hints (auto-selected from `themes/operations.md`, ~200 tokens)
7. Version check (live API validation, ~200 tokens)

**Cross-Mode Overlap** (identified by deep analysis):
- Universals prepended to BOTH scope AND recipe → ~1K tokens duplicated if both called
- Runtime guide included in BOTH briefing AND recipe → ~800 tokens duplicated
- Live stacks in briefing L1 AND `injectStacks()` into workflow markdown → ~400 tokens duplicated
- Service cards in briefing L4 AND full scope → ~600 tokens duplicated

### 2.5 Tool Response Patterns

All tools use consistent formatting:
- `jsonResult(v)` — JSON marshaling for structured data
- `textResult(text)` — Plain markdown for guidance
- `convertError(err)` — `{code, error, suggestion, apiCode}` or plain text

**NextActions pattern** — most mutating tools append follow-up guidance:
- Deploy success → "Enable subdomain, check logs"
- Import success → "Verify services, continue workflow"
- Env set → "Reload service"

### 2.6 Stack Injection

**File**: `internal/tools/workflow.go` lines 314-333

Live service stack types fetched from API, formatted, and injected into workflow markdown between `<!-- STACKS:BEGIN -->` / `<!-- STACKS:END -->` markers. Both bootstrap.md and deploy.md have these placeholders.

---

## 3. Current Problems: Context Delivery Analysis

### 3.1 Dual Delivery Mechanism

Bootstrap steps deliver guidance through TWO channels simultaneously:
1. **Inline `StepDetail.Guidance`** (bootstrap_steps.go) — compact, ~120 tokens/step, ~607 tokens total
2. **`DetailedGuide`** via `ResolveGuidance()` — full `<section>` extract, ~2000 tokens/step

Both arrive in `BootstrapStepInfo`. Inline is always a **subset** of detailedGuide — no unique content in inline. LLM sees both, doesn't know which to trust. Typically uses the longer one.

### 3.2 Redundancy in bootstrap.md

Rules repeated across multiple locations:

| Rule | Occurrences | Locations |
|------|-------------|-----------|
| `deployFiles: [.]` for self-deploy | 6x (21 mentions) | generate(3), deploy(2), provision(1) |
| Dev uses `start: zsc noop --silent` | 3x (9 mentions) | generate, deploy, bootstrap_steps.go |
| Implicit-webserver skip manual start | 4x | generate, deploy(2), deploy subsection |
| Shared storage two-stage mount | 3x | provision, bootstrap_steps.go, deploy |
| Env var discovery protocol | 4x | provision, generate, bootstrap_steps, agent prompt |

**Note**: This repetition is partially **intentional reinforcement** — rules stated near point of use are more reliably followed by LLMs than rules loaded in a separate prior call. Deduplication should consolidate (e.g., 21→4 mentions), not eliminate entirely.

### 3.3 Corrected Token Budget (Comprehensive)

**The original 24K estimate was wrong.** It double-counted the legacy path (full bootstrap.md dump) and conductor path (per-step sections), which are mutually exclusive code paths.

Actual measured delivery via conductor path:
| Stage | Tokens |
|-------|--------|
| Inline Guidance (all 5 steps) | ~607 |
| DetailedGuide (all 5 steps) | ~11,737 |
| Deploy section dominates | ~6,901 (59% of total) |
| **Total per bootstrap (guidance only)** | **~12,344** |

**Full session token budget** (all 7 delivery channels):
| Channel | Tokens | When |
|---------|--------|------|
| System prompt (sections A-D) | 400-1,200 | MCP startup (once) |
| DetailedGuide (5 steps cumulative) | 11,737 | Per step transition |
| AvailableStacks (re-injected 5x) | 2,500 | Every step response |
| PriorContext (linear growth) | 0→2,000 | Growing per step |
| Inline Guidance (all 5 steps) | 607 | Per step |
| scope="infrastructure" (pull) | 8,967 | Step 2-3 (once) |
| Briefing (pull) | 1,500-4,000 | Step 1-2 (once) |
| **Grand total first run** | **~28,000-30,000** |  |
| **Iteration 2 cost** | **~20,000-21,000** | Near-full re-delivery |
| **3-iteration total** | **~68,000-71,000** | |

### 3.4 Deploy.md Terminology Clash

Deploy.md uses "Phase 1" and "Phase 2" — these conflict with workflow engine phases (INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE). Creates confusion for agents reading both.

### 3.5 Missing Success Criteria

Steps list actions but don't state explicit completion conditions. `Verification` field exists but is vague (e.g., "All services created, dev mounted, env vars discovered"). LLM must infer when a step is "done."

### 3.6 No Delivery Tracking (NEW — from deep analysis)

**`KnowledgeTracker`** (`internal/ops/knowledge_tracker.go`, 77 lines) is in-memory only:
- Created as `ops.NewKnowledgeTracker()` in `server.go:86`
- Tracks briefing calls (runtime+services) and scope loaded (bool)
- **Lost on process restart** — no persistence to session state
- **Not connected to gates** — gates never check whether LLM loaded required knowledge
- `IsLoaded()` returns true only when BOTH briefing AND scope loaded, but nothing enforces this

**`DetailedGuide` re-sent on every `status` call**: `BuildResponse()` calls `ResolveGuidance(detail.Name)` every time with no caching. Deploy section = 6,901 tokens re-delivered on every status check.

**`AvailableStacks` re-injected on every step**: `populateStacks()` called in `handleBootstrapComplete`, `handleBootstrapSkip`, and `handleBootstrapStatus`. Stacks only needed during discover step but injected 5x.

**No dedup between knowledge modes**: If LLM calls scope then recipe in same session, universals are delivered twice (~1K tokens). No mechanism prevents this.

### 3.7 Linear Context Growth (NEW — from deep analysis)

**PriorContext is append-only**: `buildPriorContext()` at `bootstrap.go:235-251` collects ALL attestations from ALL completed steps. By step 5 (verify), carries discover + provision + generate + deploy attestations (each ~100-300 tokens).

**Iterations re-deliver everything**: `IterateSession()` resets to `PhaseDevelop` but next `status`/`complete` call re-sends full detailedGuide + priorContext + stacks. The iteration only archives evidence files — does not compress carried context.

**No summarization or compression**: Old step context kept verbatim, never condensed. Step 5 context is O(n * attestation_length) instead of O(1).

### 3.8 Gates Are Knowledge-Blind (NEW — from deep analysis)

Current gate system (`gates.go`, 160 lines):
- 5 gates (G0-G4) check evidence existence + freshness (flat 24h for all)
- **Never checks whether LLM loaded required knowledge** (scope, briefing) before producing evidence
- A gate can pass even if the LLM never called `zerops_knowledge` at all
- Gate failures return bare `fmt.Errorf("transition: gate %s failed, missing evidence: %v", ...)` — no remediation guidance
- No complexity-based simplification — simple single-service project goes through identical gates as complex multi-service
- **Flat freshness**: A typo fix and an architecture change both require 24h re-validation

### 3.9 No Post-Bootstrap Strategy Awareness

After bootstrap completes:
- ServiceMeta written but `decisions` map empty, `deployFlow` always empty
- Deploy workflow returns full deploy.md regardless of how user wants to work
- System prompt has no strategy-specific routing
- No mechanism to ask "how do you want to deploy going forward?"

### 3.10 Abandoned Bootstrap Data Loss

**BUG**: `writeBootstrapOutputs` in `bootstrap_evidence.go` only fires when `Bootstrap.Active` becomes `false` (after ALL steps complete via `autoCompleteBootstrap`). If user abandons bootstrap after 4 successful steps → zero ServiceMeta files written to disk. All context from the session is lost.

Additionally:
- No session resume mechanism exists. Orphaned session JSON files persist but cannot be re-attached.
- `pruneDeadSessions` removes from registry but does NOT delete `sessions/{id}.json` or `evidence/{id}/`.

### 3.11 Bootstrap Exclusivity TOCTOU Race

`ListSessions()` + `InitSession()` in `engine.go` are not in the same lock scope. Two processes can both pass the "no active bootstrap" check and both start bootstrap. Window is small but exists.

### 3.12 ServiceMeta Overwrites for Shared Dependencies

`writeBootstrapOutputs` writes ServiceMeta for ALL dependencies regardless of `Resolution`. If service A and B share postgres (Resolution: SHARED), re-bootstrapping B overwrites postgres's ServiceMeta with the new session ID, losing provenance from A's bootstrap.

---

## 4. Design: Dynamic Flow Router

### 4.1 Concept

Replace hardcoded routing in `instructions.go` with a pure function that takes environmental signals and returns ranked workflow offerings.

### 4.2 Data Model

```go
// internal/workflow/router.go

type FlowOffering struct {
    Workflow string `json:"workflow"`
    Priority int    `json:"priority"`    // 1 = most recommended
    Reason   string `json:"reason"`
    Hint     string `json:"hint"`        // action hint for LLM
}

type RouterInput struct {
    ProjectState   ProjectState       // FRESH, CONFORMANT, NON_CONFORMANT, UNKNOWN
    ServiceMetas   []*ServiceMeta     // per-service bootstrap records (filtered against live services)
    ActiveSessions []SessionEntry     // currently active workflow sessions
    LiveServices   []string           // hostnames from API (for stale meta filtering)
}

func Route(input RouterInput) []FlowOffering
```

### 4.3 Routing Decision Table

| ProjectState | ActiveSessions | ServiceMetas | Top Offering |
|---|---|---|---|
| FRESH | none | none | bootstrap (p=1) |
| FRESH | bootstrap active | — | resume bootstrap hint |
| CONFORMANT | none | with strategy=ci-cd | "Push to git for CI/CD" (p=1), bootstrap-new (p=2) |
| CONFORMANT | none | with strategy=push-dev | "zerops_workflow deploy" (p=1), bootstrap-new (p=2) |
| CONFORMANT | none | with strategy=manual | "Deploy manually" (p=1), bootstrap-new (p=2) |
| CONFORMANT | none | no metas | deploy (p=1), bootstrap (p=2) |
| CONFORMANT | none | mixed (some metas, some without) | strategy-based for covered services, deploy for uncovered |
| NON_CONFORMANT | none | some metas | strategy-based for covered, bootstrap for uncovered |
| NON_CONFORMANT | none | no metas | bootstrap (p=1), debug (p=2) |
| UNKNOWN | — | — | all workflows at equal priority, note that state could not be determined |
| any | any | any | always include: debug(p=5), scale(p=5), configure(p=5) |

### 4.4 Integration with instructions.go

`buildProjectSummary` currently has hardcoded `switch projState` at lines 119-132. Replace with:
1. `ListServiceMetas(stateDir)` — new function reading all `services/*.json`
2. Cross-reference metas against live services from API → filter stale entries
3. `Route(input)` — pure function, no I/O
4. `formatOfferings(offerings)` — render into system prompt text

### 4.5 Stale Meta Filtering

Router receives `LiveServices []string` (from API) alongside `ServiceMetas`. Before routing, filter out any ServiceMeta whose `Hostname` has no match in LiveServices. This prevents recommending strategies for deleted services.

### 4.6 ProjectState Extension

Add `StateUnknown ProjectState = "UNKNOWN"` for API failure cases. When `client.ListServices` fails, `buildProjectSummary` passes `StateUnknown` to the router instead of silently returning empty string.

### 4.7 System Prompt Staleness

**Constraint**: Router output is computed once at MCP startup and frozen. Strategy changes mid-session are NOT reflected in the system prompt.

**Mitigation (v1)**: Accept staleness for system prompt. Router value is primarily **cross-session** (new MCP connection picks up latest ServiceMeta). For mid-session strategy awareness, tool responses from `zerops_workflow` already include strategy-specific guidance — this is the live delivery channel.

**Future (v2)**: If MCP spec adds dynamic instructions support, switch to live evaluation. Alternatively, add `action="route"` to `zerops_workflow` that returns current router output on demand.

### 4.8 Key Design Decision

Router is a **pure function**, not a method on Engine. It has no mutable state, no I/O. Data collection happens in `BuildInstructions`; routing is deterministic logic. This makes it trivially testable with table-driven tests.

---

## 5. Design: Post-Bootstrap Deploy Strategies

### 5.1 Three Strategies

| ID | Name | Best For | How it works |
|----|------|----------|-------------|
| `push-dev` | Simple Push | Prototyping, solo dev | SSH push dev→dev, instructed push to stage |
| `ci-cd` | CI/CD Pipeline | Production, teams | Git push → pipeline → stage (RECOMMENDED) |
| `manual` | Manual | Existing tooling | No ZCP flow, monitoring only |

### 5.2 Pre-requisite: Unify DeployFlow and Decisions

**Current state**: `ServiceMeta` has BOTH `DeployFlow string` (line 19) and `Decisions map[string]string` (line 23). `DeployFlow` is never populated in production (only test fixtures set `"ssh"`). The plan stores strategy in `Decisions["deployStrategy"]`, creating two fields for the same concept.

**Required action**: Delete `DeployFlow` field entirely. Migrate any test fixtures to use `Decisions["deployStrategy"]`. This MUST happen before strategy step implementation.

### 5.3 Strategy Capture: 6th Bootstrap Step

Add `strategy` as step 6 after verify:

```go
// bootstrap_steps.go
{
    Name:     "strategy",
    Category: CategoryFixed,
    Guidance: "Ask user to choose deployment strategy for each runtime service",
    Tools:    []string{"zerops_workflow"},
    Verification: "SUCCESS WHEN: Decisions[\"deployStrategy\"] recorded for all runtime services in plan. NEXT: bootstrap complete.",
    Skippable: true,  // auto-skip when no runtime services (managed-only projects)
}
```

**Critical**: Must be `Skippable: true` with auto-skip logic in `validateConditionalSkip()` for managed-only projects. Otherwise managed-only bootstrap deadlocks — there's nothing to choose a strategy for.

### 5.4 Structured Strategy Storage in BootstrapState

Strategy must be captured in a **structured field**, not parsed from free-text attestation. `writeBootstrapOutputs` cannot extract structured data from attestation strings.

**Design**: Add `Strategies map[string]string` field to `BootstrapState` (keyed by hostname). Populated via `action="complete" step="strategy" strategies={"appdev":"ci-cd","appstage":"ci-cd"}`. `writeBootstrapOutputs` reads from this field when constructing ServiceMeta.

### 5.5 Strategy Constants and Persistence

```go
// service_meta.go
const (
    StrategyPushDev        = "push-dev"
    StrategyCICD           = "ci-cd"
    StrategyManual         = "manual"
    DecisionDeployStrategy = "deployStrategy"
)
```

Store in `ServiceMeta.Decisions["deployStrategy"]`. On `BootstrapComplete(step="strategy")`: engine reads `BootstrapState.Strategies`, writes per-target to ServiceMeta.

### 5.6 Strategy Change After Bootstrap

New `zerops_workflow action="strategy"` handler:
- Reads ServiceMeta for given hostname
- Validates strategy value against constants
- Updates `Decisions["deployStrategy"]`
- Returns confirmation + strategy-specific guidance section from deploy.md

This gives: clean capture during bootstrap + ability to change anytime later.

### 5.7 Strategy-Aware Deploy Guidance

Add to `deploy.md` three `<section>` blocks:

- `<section name="deploy-push-dev">` — SSH push flow, dev-first then stage
- `<section name="deploy-ci-cd">` — Git-centric, pipeline guidance
- `<section name="deploy-manual">` — Monitoring-only, available tools list

New `deploy_guidance.go`:
```go
func ResolveDeployGuidance(stateDir, hostname string) string
// Reads ServiceMeta → maps strategy → extracts section from deploy.md
// Falls back to full deploy.md if no strategy set
```

Reuses `extractSection()` pattern from `bootstrap_guidance.go:20-32`.

When `action="start" workflow="deploy"`: read ServiceMetas, return strategy-specific section. **This is the mid-session delivery channel** — tool responses always reflect current state, unlike the frozen system prompt.

---

## 6. Design: CI/CD Pipeline Generation — NEEDS REDESIGN

### 6.1 Core Problem

The original plan proposes `GeneratePipeline()` as a Go helper function. But the LLM cannot call Go functions directly — it can only call MCP tools. The helper has no caller unless embedded inside another MCP tool.

### 6.2 Design Options

**Option A: MCP tool from day 1** (`zerops_pipeline` or `zerops_workflow action="pipeline"`)
- Clean, follows existing patterns
- LLM calls tool → gets pipeline content → writes file
- Adds to tool catalog (currently 15 tools)

**Option B: Guidance-only** (no Go code)
- Strategy guidance includes full pipeline template inline in deploy.md section
- LLM reads guidance, writes pipeline file itself
- Zero code, maximum flexibility, but less reliable

**Option C: Embedded in strategy completion** (current plan's implicit approach)
- `zerops_workflow action="complete" step="strategy"` returns pipeline content when strategy=ci-cd
- Mixes orchestration with generation

**Recommendation**: Option A is cleanest. Implement as `zerops_workflow action="pipeline"` accepting `platform` (github-actions|gitlab-ci|bitbucket-pipelines) and `hostnames` (stage services to deploy).

### 6.3 Prerequisite: zcli Scope Clarification

`CLAUDE.local.md` says "NEVER use zcli for anything" — but CI/CD pipelines literally run `zcli push`. The prohibition scope must be clarified to "development machine only; CI/CD pipelines and remote SSH commands are exempt." This is a documentation change, not a code change.

### 6.4 Templates

Embedded in `internal/content/templates/pipelines/`:
- `github-actions.yml.tmpl` → `.github/workflows/zerops-deploy.yml`
- Start with GitHub Actions only. GitLab/Bitbucket deferred until demand exists.

Go `text/template` with `{{.Hostnames}}`, `{{.TokenVar}}`, `{{.ZeropsYmlPath}}`.

### 6.5 Edge Cases to Handle

- **Existing pipeline files**: Check before writing, warn if conflict detected
- **Multi-service**: Template loops `{{range .Hostnames}}` with per-service `zcli push --service {{.}}`
- **Monorepo**: Add `{{.WorkDir}}` template var for subdirectory support
- **zcli version pinning**: Pin specific version in template, not `@latest`
- **Token management**: Guide user to create ZEROPS_TOKEN via Zerops GUI. No API for token generation exists.

### 6.6 Status

**DEFERRED** until Wave 3 is complete (strategy system fully working). Dependencies:
- Strategy step implemented (section 5.3)
- zcli scope clarified in CLAUDE.local.md (section 6.3)
- Design decision on Option A vs B finalized

---

## 7. Design: Local Dev VPN Flow — DEFERRED

### 7.1 Status: Not Ready for Implementation

Stress-testing identified critical blockers:

1. **Archive format unknown** (CRITICAL): Zerops build pipeline may expect zcli-specific tar.gz format with `.deploy.zerops` metadata. Without reverse-engineering zcli source, API deploys may silently fail. This blocks the entire VPN flow.

2. **`DevHostname` semantic mismatch** (HIGH): `RuntimeTarget.DevHostname` field name assumes dev service exists. VPN has no dev service. This ripples through `validate.go`, `bootstrap_evidence.go`, `bootstrap_steps.go`, `managed_types.go`, `deploy.md`, and all tests.

3. **ProjectState detection breaks** (HIGH): VPN-mode projects with only stage services would be detected as `StateNonConformant`, triggering wrong routing.

### 7.2 What to Prepare Now (Zero Behavior Change)

Add `Mode` field to `runtime.Info`:
```go
type Mode string
const (
    ModeContainer Mode = "container"
    ModeLocal     Mode = "local"
)
// ModeLocalVPN = "local_vpn" — future, when VPN flow is implemented
```

Derive from existing `InContainer`: if true → `ModeContainer`, else → `ModeLocal`. No behavior change, just structured extension point.

### 7.3 Reference: VPN Architecture (for future implementation)

Preserved from original analysis for when blockers are resolved:

| Aspect | Container Mode | Local Dev Mode |
|--------|---------------|----------------|
| Code location | Zerops container (SSHFS) | Local filesystem |
| Dev service | Yes | No (local machine) |
| Managed services | Internal DNS | VPN IP/DNS ({hostname}.zerops) |
| Stage service | Deploy from dev via SSH | Deploy from local via API archive |
| Env vars | Container-injected | .env file generation needed |
| Deployment | SSH self-deploy | API 3-step: CreateVersion → Upload → Deploy |

**Before implementing**: Reverse-engineer zcli archive format, resolve DevHostname semantics, design .env file generation with .gitignore safety, add VPN connectivity pre-flight check.

---

## 8. Context Delivery Optimization — Validated Changes (EXPANDED)

### 8.0 4-Layer Progressive Delivery Architecture (NEW)

Knowledge should be organized into four layers delivered just-in-time:

| Layer | Content | Delivery | Tokens |
|-------|---------|----------|--------|
| **L0: Routing** | System prompt + step name + tools + verification criteria | Always pushed | ~200/step |
| **L1: Procedural** | Compact step guidance (current `Guidance` field) | Pushed per step | ~150/step |
| **L2: Detailed** | Mode-filtered sections from bootstrap.md | Pushed on first delivery, stub on repeat | ~1000-3500/step |
| **L3: Reference** | Scope, briefings, recipes | Pull-based (LLM-initiated) | Variable |

**Key Principle**: "Deliver once, track delivery, compress history"

### 8.1 Remove Dual Delivery (IMPLEMENT)

Stop serializing `guidance` field to `BootstrapStepInfo` JSON responses. Keep `detailedGuide` as sole authoritative channel. The `StepDetail.Guidance` field stays in the Go struct for documentation/fallback, just not sent to LLM.

**Implementation**: In `bootstrap.go:216` (BuildResponse), stop copying `detail.Guidance` to `BootstrapStepInfo.Guidance`. Either add `json:"-"` tag or set to empty string.

**Fallback safety**: If `ResolveGuidance()` returns `""` (section extraction fails), LLM still gets: step name, tools list, verification criteria, and priorContext. This is minimal but sufficient for an experienced LLM. `ResolveGuidance` only fails if bootstrap.md is missing from embed.FS — which would mean a broken build.

**Test impact**: 4 test assertions check `Guidance != ""` — these need updating:
- `TestStepDetails_AllStepsCovered` at `bootstrap_test.go:19-20`
- `TestStepDetails_DiscoverGuidance_ThreeStates` at `bootstrap_test.go:51-64`
- `TestBuildResponse_FirstStep` at `bootstrap_test.go:297-298`

**Savings**: ~607 tokens total (modest, but eliminates LLM confusion).

### 8.2 Conditional Deploy Section by PlanMode (IMPLEMENT)

The deploy section in bootstrap.md is ~6,901 tokens — 59% of total guidance. Split into subsections loadable by planMode AND plan context:

**Sub-section breakdown** (from deep analysis token measurement):
| Sub-section | Tokens | When to deliver |
|-------------|--------|----------------|
| `<section name="deploy-overview">` | 426 | Always |
| `<section name="deploy-standard">` | 526 | PlanMode=standard |
| `<section name="deploy-iteration">` | 869 | Dynamic runtimes only (not implicit-webserver) |
| `<section name="deploy-simple">` | 612 | PlanMode=simple |
| `<section name="deploy-agents">` | 757 | 2+ runtime targets in plan |
| `<section name="deploy-status-spec">` | 2,521 | Managed services exist in plan |
| `<section name="deploy-verify-loop">` | 619 | Only after first verification failure (reactive) |

**Implementation**: New `ResolveProgressiveGuidance(step, plan, failureCount)` in `bootstrap_guidance.go`:
```go
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int) string {
    if step != StepDeploy {
        return ResolveGuidance(step) // non-deploy steps are reasonably sized
    }
    var parts []string
    parts = append(parts, resolveSubSection("deploy-overview"))
    mode := planMode(plan) // existing function at bootstrap.go:254
    if mode == PlanModeStandard {
        parts = append(parts, resolveSubSection("deploy-standard"))
        if hasNonImplicitWebserverRuntime(plan) {
            parts = append(parts, resolveSubSection("deploy-iteration"))
        }
    } else {
        parts = append(parts, resolveSubSection("deploy-simple"))
    }
    if len(plan.Targets) > 1 {
        parts = append(parts, resolveSubSection("deploy-agents"))
    }
    if hasManagedServices(plan) {
        parts = append(parts, resolveSubSection("deploy-status-spec"))
    }
    if failureCount > 0 {
        parts = append(parts, resolveSubSection("deploy-verify-loop"))
    }
    return strings.Join(parts, "\n\n---\n\n")
}
```

**Standard mode with managed**: overview(426) + standard(526) + iteration(869) + status-spec(2,521) = **4,342 tokens** (was 6,901)
**Simple mode with managed**: overview(426) + simple(612) + status-spec(2,521) = **3,559 tokens**
**Savings**: 2,600-3,400 tokens per deploy step.

### 8.3 Reduce Deploy Section Redundancy (IMPLEMENT)

Consolidate repeated rules within bootstrap.md using a "Key Rules" block pattern:

**Optimal repetition** (from LLM behavioral analysis):
- **Critical rules** (deployFiles, 0.0.0.0 binding): 2-3 full + N short references
- **Operational rules** (noop start): 2 full + N short references
- **Exception rules** (implicit-webserver): 1 full + N inline tags

**Concrete targets**:
- `deployFiles: [.]` — from 21 mentions to 5 (full text in Key Rules block + YAML template + agent prompt; short reference elsewhere)
- `zsc noop --silent` — from 9 mentions to 3 (generate, deploy, simple mode)
- `implicit-webserver` — from 5 to 2 (one full definition, one inline exception)
- `/status` spec — from 3-4 full to 1 full + 2 cross-references

**Positive phrasing** (from LLM behavioral model): Rephrase negation rules as directives:
- "Do NOT generate hello-world apps" → "Generate apps with /status endpoint that proves real managed service connectivity"
- "NEVER write lock files" → "Write manifests only (go.mod, package.json) — build commands generate locks"
- "Do NOT add variables that don't exist" → "Map ONLY variables listed in the discovery response"

**Savings**: ~3,000 tokens (21→5 deployFiles mentions × ~40 tokens each = ~640 saved; similar for other rules).
**Risk**: Zero (markdown-only change, no code impact).

### 8.4 Expand Verification with SUCCESS WHEN (IMPLEMENT)

Replace vague verification strings with explicit criteria:

Before: `"All services created, dev mounted, env vars discovered"`
After: `"SUCCESS WHEN: all plan services exist in API with ACTIVE status AND dev filesystems mounted AND env vars recorded in session state. NEXT: proceed to generate step."`

**Cost**: ~250 additional tokens total. **Benefit**: Improves attestation quality, gives LLM clear completion signal.

### 8.5 Fix Phase Terminology (IMPLEMENT)

Rename deploy.md's "Phase 1" / "Phase 2" to "Part 1: Configuration Check" / "Part 2: Deploy and Monitor". Prevents confusion with engine phases.

### 8.6 Delivery Tracking via ContextDelivery (NEW — IMPLEMENT)

**Problem**: No mechanism tracks what knowledge was delivered to the LLM. `KnowledgeTracker` is in-memory only, lost on restart, not visible to gates.

**Solution**: Add persistent `ContextDelivery` struct to `BootstrapState`:

```go
// internal/workflow/state.go
type ContextDelivery struct {
    GuideSentFor  map[string]int `json:"guideSentFor,omitempty"`  // step → delivery count
    StacksSentAt  string         `json:"stacksSentAt,omitempty"`  // timestamp
    ScopeLoaded   bool           `json:"scopeLoaded,omitempty"`
    BriefingFor   string         `json:"briefingFor,omitempty"`   // "nodejs@22+postgresql@16"
    RecipesViewed []string       `json:"recipesViewed,omitempty"`
    IterationNum  int            `json:"iterationNum,omitempty"`
}
```

**Behavior**:
1. **Gate DetailedGuide re-delivery**: First delivery → full `ResolveGuidance()`, mark `GuideSentFor[step]=1`. Repeat → stub: `"[Guide for {step} already delivered. Tools: X. Verification: Y]"` (~50 tokens). Add `forceGuide` param for recovery.
2. **Gate AvailableStacks**: Only inject during discover step or if never sent. Saves ~2,000 tokens (500/step × 4 steps).
3. **Persist knowledge calls**: When `zerops_knowledge` called with active session, write scope/briefing flags to `ContextDelivery`. Survives process restart.
4. **Briefing dedup**: Skip universals if scope already loaded; skip runtime guide if briefing already loaded. Saves ~2,000 tokens.

**Savings**: ~14,000-21,000 tokens across typical bootstrap (2-3 status calls per step × 6,901 deploy section).

### 8.7 PriorContext Compression (NEW — IMPLEMENT)

Replace `buildPriorContext()` with sliding-window version:

```go
func (b *BootstrapState) buildPriorContext() *StepContext {
    attestations := make(map[string]string)
    for i := 0; i < b.CurrentStep && i < len(b.Steps); i++ {
        if b.Steps[i].Attestation == "" { continue }
        if i == b.CurrentStep-1 {
            attestations[b.Steps[i].Name] = b.Steps[i].Attestation // full
        } else {
            att := b.Steps[i].Attestation
            if len(att) > 80 { att = att[:77] + "..." }
            attestations[b.Steps[i].Name] = fmt.Sprintf("[%s: %s]", b.Steps[i].Status, att) // compressed
        }
    }
    return &StepContext{Plan: b.Plan, Attestations: attestations}
}
```

**Safety**: Plan always included in full (referenced by every subsequent step). Only old attestations compressed. Current step's attestation (N-1) kept verbatim.

**Savings**: ~510 tokens at step 5, compounds across iterations.

### 8.8 Iteration Delta Guidance (NEW — IMPLEMENT)

When `ContextDelivery.IterationNum > 0`, replace full DetailedGuide with focused delta:

```
"ITERATION N for step {step}.
Previous attempt: {last attestation from failed step}.
Focus: identify root cause from logs/errors, fix, and retry.
Tools: zerops_logs severity=ERROR since=5m, then fix and redeploy."
```

~100 tokens instead of ~6,900 tokens (deploy section).

**Savings**: ~6,800 tokens per iteration. Over 2 iterations: ~13,600 tokens.

### 8.9 Knowledge-Aware Gates (NEW — IMPLEMENT)

**8.9.1 Rich Gate Failure Responses**

Add `Remediation` to `GateResult`:
```go
type RemediationStep struct {
    Action      string `json:"action"`      // "load_knowledge", "record_evidence"
    Tool        string `json:"tool"`        // "zerops_knowledge", "zerops_workflow"
    Params      string `json:"params"`      // "scope=\"infrastructure\""
    Explanation string `json:"explanation"` // "Platform knowledge required before generating YAML"
}
```

Return structured JSON instead of flat error string. LLM knows exactly what to do.

**8.9.2 Gate Knowledge Prerequisites**

Add `knowledgePrereqs []string` to `gateDefinition`:
- G2 (DEVELOP→DEPLOY) requires `scope` + `briefing` loaded (checked via `ContextDelivery`)
- Prevents generating code without platform knowledge

**8.9.3 Complexity-Based Gate Simplification**

Add `skippableFor []string`:
- `managed_only` projects skip G2, G3 (no code to write/deploy)
- Complexity derived from `state.Bootstrap.Plan` after discover step

**8.9.4 Adaptive Freshness**

- Iteration 0: 24h (current behavior)
- Iteration 1+: 1h for `dev_verify`/`deploy_evidence`, 24h for `discovery`
- Benefit: typo fix doesn't require full re-validation

### 8.10 Cross-Workflow Content Dedup (NEW — IMPLEMENT)

- Extract shared deploy/verify protocol (~120 lines) from bootstrap.md and deploy.md into `<section name="deploy-common">`
- Deduplicate universals-core overlap: remove verbatim duplicates in core.md networking/filesystem sections
- Savings: ~2,300 tokens

### 8.11 DROPPED Optimizations

| Proposal | Why Dropped |
|----------|-------------|
| Extract rules to separate `zerops_knowledge scope="rules"` | Adds mandatory tool call dependency. LLMs may skip it. Loses contextual proximity. Saves only ~500 tokens — achievable via in-place dedup instead. |
| Concept-based doc restructure | Breaks 1:1 step-to-section mapping in `ResolveGuidance(step)`. 13 tests need rewriting. No actual token savings — multiple section extracts per step would be equal or larger. |
| Agent handoff decision log | `PriorContext` at `bootstrap.go:235-251` already collects prior attestations and plan. Adding separate decision log is redundant. |
| Full pull-based model (no push) | Too risky — LLM may skip critical knowledge. Progressive push (first delivery full, repeat delivery stub) is safer than pure pull. |
| Scope sub-sectioning (split core.md) | High risk, requires restructuring core.md with section tags + new Provider interface method. Savings achievable more simply via briefing dedup. Defer to future. |

### 8.12 Corrected Token Savings Estimate (COMPREHENSIVE)

**First-run bootstrap (standard mode, 1 service pair)**:
| Change | Current | Proposed | Savings |
|--------|---------|----------|---------|
| DetailedGuide (mode-filtered) | 11,785 | ~7,500 | 36% |
| AvailableStacks (5x→1x) | 2,500 | 500 | 80% |
| PriorContext (compressed) | 2,000 | 600 | 70% |
| Inline Guidance (removed) | 607 | 0 | 100% |
| Content dedup (bootstrap.md) | — | — | ~3,000 saved |
| Cross-workflow dedup | — | — | ~2,300 saved |
| **Total** | **~28,961** | **~20,567** | **29%** |

**Iteration 2 (deploy fails, retry)**:
| Change | Current | Proposed | Savings |
|--------|---------|----------|---------|
| DetailedGuide (delta) | 7,547 | ~200 | 97% |
| scope/briefing (tracked) | ~12,000 | 0 | 100% |
| PriorContext | 800 | 200 | 75% |
| Stacks | 500 | 0 | 100% |
| **Total** | **~20,847** | **~400** | **98%** |

**3-iteration total**: Current ~69K → Proposed ~21K (**70% reduction**).

Realistic improvement from 12,344 → ~7,000-8,000 tokens per bootstrap (first run). Iteration cost drops from ~20K to ~400 tokens.

---

## 9. Existing Bugs to Fix (Independent of Evolution)

### 9.1 ServiceMeta Not Written on Partial Bootstrap

`writeBootstrapOutputs` only fires on full completion. Abandoned bootstrap = zero ServiceMeta.

**Fix**: Write ServiceMeta incrementally. After provision step completes (services exist in API), write meta with `Decisions: {}` (empty). After strategy step, update with strategy. Need a `Status` field on ServiceMeta to distinguish partial from complete: `"provisioned"` vs `"bootstrapped"`.

### 9.2 Shared Dependency Meta Overwrite

`writeBootstrapOutputs` writes meta for ALL dependencies. Re-bootstrap for new service overwrites shared dep's meta.

**Fix**: Skip `WriteServiceMeta` for dependencies with `Resolution: EXISTS` or `Resolution: SHARED` — they already have metas from original bootstrap.

### 9.3 Bootstrap Exclusivity TOCTOU

`ListSessions()` + `InitSession()` not in same lock scope in `engine.go`.

**Fix**: Move both into single `withRegistryLock` call.

### 9.4 ServiceMeta Not Cleaned on Delete

`zerops_delete` success does not remove ServiceMeta file. Stale metas accumulate.

**Fix**: Add `DeleteServiceMeta(baseDir, hostname)` function. Call from `zerops_delete` success path in `tools/delete.go`.

### 9.5 Orphaned Session Files

`pruneDeadSessions` removes from registry but leaves `sessions/{id}.json` and `evidence/{id}/` on disk.

**Fix**: Optionally clean up orphaned files during prune, or add separate `CleanOrphanedSessions()`.

---

## 10. Implementation Waves (REVISED — Integrated Knowledge Optimization)

### Wave 1 — Cleanup + Content Dedup (no dependencies, all S-scope)

| # | Action | Files | Section |
|---|--------|-------|---------|
| 1 | Delete `DeployFlow` field, migrate test fixtures to `Decisions["deployStrategy"]` | `service_meta.go`, `service_meta_test.go`, `bootstrap_evidence.go` | 5.2 |
| 2 | Stop serializing inline `Guidance` to LLM responses | `bootstrap.go:216`, update 4 tests | 8.1 |
| 3 | Rename "Phase 1/2" to "Part 1/2" in deploy.md | `deploy.md` | 8.5 |
| 4 | Expand `Verification` strings with `SUCCESS WHEN:` criteria | `bootstrap_steps.go` | 8.4 |
| 5 | Add `ListServiceMetas(stateDir)` function + tests | `service_meta.go`, `service_meta_test.go` | 4.4 |
| 6 | Add strategy constants (`StrategyPushDev`, `StrategyCICD`, `StrategyManual`, `DecisionDeployStrategy`) | `service_meta.go` | 5.5 |
| 7 | **Consolidate bootstrap.md rules** (deployFiles 21→5, noop 9→3, positive phrasing) | `bootstrap.md` | 8.3 |
| 8 | **Factor shared deploy logic** between bootstrap.md and deploy.md | `bootstrap.md`, `deploy.md` | 8.10 |
| 9 | **Deduplicate universals-core overlap** | `themes/core.md` | 8.10 |

### Wave 2 — Delivery Tracking + Router (depends on wave 1)

| # | Action | Files | Scope |
|---|--------|-------|-------|
| 10 | **Add `ContextDelivery` struct to BootstrapState** | `state.go` | M |
| 11 | **Gate DetailedGuide re-delivery** (first=full, repeat=stub) | `bootstrap.go` | M |
| 12 | **Gate AvailableStacks delivery** (discover step only) | `tools/workflow.go` | S |
| 13 | **Compress PriorContext** (N-1 full, older compressed) | `bootstrap.go` | S |
| 14 | **Iteration delta guidance** (delta instead of full guide on iter 2+) | `bootstrap.go` | M |
| 15 | **Persist knowledge tracking to session** | `tools/knowledge.go`, `engine.go`, `knowledge_tracker.go` | M |
| 16 | Implement `Route()` pure function with table-driven tests | New: `router.go`, `router_test.go` | M |
| 17 | Add strategy-specific `<section>` tags to deploy.md | `deploy.md` | S |
| 18 | Create `ResolveDeployGuidance()` reusing `extractSection` pattern | New: `deploy_guidance.go`, `deploy_guidance_test.go` | S |
| 19 | Add `DeleteServiceMeta()` + hook in `zerops_delete` | `service_meta.go`, `tools/delete.go` | S |
| 20 | Skip shared-dep meta overwrite for EXISTS/SHARED deps | `bootstrap_evidence.go` | S |

### Wave 3 — Mode-Filtered Knowledge + Strategy System (depends on wave 2)

| # | Action | Files | Scope |
|---|--------|-------|-------|
| 21 | **Split deploy section into progressive sub-sections** (7 sub-sections) | `bootstrap.md` | M |
| 22 | **Implement `ResolveProgressiveGuidance()`** | `bootstrap_guidance.go`, `bootstrap.go` | M |
| 23 | **Briefing dedup via tracker** (skip universals/runtime if already loaded) | `tools/knowledge.go` | S |
| 24 | Wire router into `buildProjectSummary`, add `StateUnknown` | `instructions.go`, `managed_types.go` | M |
| 25 | Add "strategy" step 6 with `Skippable: true` + auto-skip | `bootstrap_steps.go`, `bootstrap.go` | M |
| 26 | Add `Strategies` map to BootstrapState for structured capture | `state.go`, `bootstrap.go` | S |
| 27 | Add `action="strategy"` handler for post-bootstrap changes | `tools/workflow.go` | M |
| 28 | Implement incremental ServiceMeta writes (after provision) | `bootstrap_evidence.go`, `service_meta.go` | M |
| 29 | Fix bootstrap exclusivity TOCTOU race | `engine.go` | S |

### Wave 4 — Knowledge-Aware Gates + Pipeline (depends on wave 3)

| # | Action | Files | Scope |
|---|--------|-------|-------|
| 30 | **Rich gate failure responses** (Remediation in GateResult) | `gates.go`, `tools/workflow.go` | M |
| 31 | **Gate knowledge prerequisites** (G2 requires scope+briefing) | `gates.go`, `engine.go` | M |
| 32 | **Complexity-based gate simplification** (managed_only skips G2,G3) | `gates.go` | S |
| 33 | **Adaptive freshness** (iteration-aware: 1h vs 24h) | `gates.go` | S |
| 34 | Clarify zcli prohibition scope in CLAUDE.local.md | User confirmation needed | S |
| 35 | Implement pipeline generation (MCP tool or guidance-only) | Design decision on approach | M |
| 36 | Add `Mode` field to `runtime.Info` (no-op extension) | `runtime.go` | S |

### Dependency Graph

```
Wave 1: [1,2,3,4,5,6,7,8,9] ─── all independent, parallel (content + cleanup)
           │
Wave 2: [10, 11←10, 12, 13, 14←10, 15←10, 16←5, 17, 18←17, 19, 20]
           │     context tracking              router + deploy guidance
           │
Wave 3: [21←7, 22←21, 23←15, 24←16, 25←1+4, 26, 27←25, 28, 29]
           │   progressive guidance    strategy system + bugfixes
           │
Wave 4: [30←15, 31←15+30, 32←31, 33, 34, 35←25+34, 36]
             knowledge-aware gates        pipeline + future
```

---

## 11. Behavioral Changes Summary

### In-Session Flow (user stays after bootstrap)

1. Bootstrap completes step 6 (strategy) → `Decisions["deployStrategy"] = "ci-cd"` written to ServiceMeta
2. User says "deploy new version"
3. System prompt is stale (still shows bootstrap info) — **no change here**
4. LLM calls `zerops_workflow action="start" workflow="deploy"`
5. Engine reads **fresh** ServiceMeta from disk → returns ONLY `<section name="deploy-ci-cd">` (~800 tokens)
6. **This is the live channel** — tool responses always reflect current state

### Cross-Session (new MCP connection)

1. New session starts → `BuildInstructions()` runs
2. Router reads ServiceMetas + live services from API
3. System prompt shows: `"Recommended: Push to git for CI/CD (appdev)"` instead of generic `"use deploy or bootstrap"`
4. **This is where router provides primary value**

### Managed-Only Project Bootstrap

Before: discover → provision → (skip generate) → (skip deploy) → verify → DONE
After: discover → provision → (skip generate) → (skip deploy) → verify → **(skip strategy)** → DONE
No behavioral change — strategy step auto-skips.

### What Does NOT Change

- MCP tool API (names, parameters) — fully backward compatible
- 5 core bootstrap steps — strategy is additive step 6
- Workflow markdown files remain — sections added, nothing removed
- Container vs local detection — existing behavior unchanged
- All existing tests pass (with minor assertions updates for dual delivery removal)

---

## 12. Existing Reusable Code

| Function | File | Reuse For |
|----------|------|-----------|
| `ReadServiceMeta` / `WriteServiceMeta` | `service_meta.go` | Strategy persistence |
| `ResolveGuidance(step)` | `bootstrap_guidance.go` | Deploy section extraction pattern |
| `extractSection(md, name)` | `bootstrap_guidance.go:20-32` | Direct reuse for deploy guidance |
| `DetectProjectState()` | `managed_types.go` | Router input |
| `ListSessions()` | `registry.go` | Router input |
| `validateConditionalSkip()` | `bootstrap.go` | Strategy step skip logic |
| `populateStacks()` / `injectStacks()` | `tools/workflow.go` | Stack catalog injection |
| `jsonResult()` / `textResult()` | `tools/convert.go` | Response formatting |
| `content.GetWorkflow()` | `content/content.go` | Markdown loading |
| `buildPriorContext()` | `bootstrap.go:235-251` | Already serves as decision log |

---

## 13. MCP Tool Catalog (Complete Reference)

### Read-only Tools

| Tool | Input | Output | Notes |
|------|-------|--------|-------|
| `zerops_discover` | service?, includeEnvs? | JSON: project + services[] with resources, ports, envs | Primary env var reader |
| `zerops_knowledge` | scope/query/runtime+services/recipe | Text or JSON depending on mode | 4 delivery modes, BM25 search |
| `zerops_logs` | serviceHostname, severity?, since?, limit?, search? | JSON: entries[] with timestamp/severity/message | Runtime diagnostics |
| `zerops_events` | serviceHostname?, limit? | JSON: events[] with type/action/status | Project activity timeline |
| `zerops_verify` | serviceHostname? | JSON: checks[] with name/status/detail | 5 check types, advisory info level |
| `zerops_process` | processId, action?(status/cancel) | JSON: process status or cancellation | Historical check / cancel |

### Mutating Tools

| Tool | Input | Output | Guard |
|------|-------|--------|-------|
| `zerops_workflow` | action + various params | JSON: session state, guidance text, bootstrap response | none |
| `zerops_deploy` | sourceService?, targetService, workingDir?, includeGit? | JSON: status, buildLogs, nextActions | requires active workflow |
| `zerops_import` | content/filePath | JSON: processes[], summary, nextActions | requires active workflow |
| `zerops_manage` | action, serviceHostname, storageHostname? | JSON: process status | none |
| `zerops_env` | action, serviceHostname/project, variables[] | JSON: process status | none |
| `zerops_scale` | serviceHostname + cpu/ram/disk/container params | JSON: process + appliedConfig | none |
| `zerops_subdomain` | serviceHostname, action(enable/disable) | JSON: subdomainUrls[] | none |
| `zerops_mount` | action(mount/unmount/status), serviceHostname? | JSON: mount status | container only |
| `zerops_delete` | serviceHostname, confirmHostname | JSON: process status | requires exact hostname match |

### Error Format (all tools)

```json
{"code": "ERROR_CODE", "error": "message", "suggestion": "fix hint", "apiCode": "optional"}
```

Error codes: AUTH_REQUIRED, SERVICE_NOT_FOUND, INVALID_PARAMETER, WORKFLOW_REQUIRED, BOOTSTRAP_NOT_ACTIVE, etc. (see `internal/platform/errors.go`).

---

## 14. LLM Behavioral Model for Knowledge Delivery (NEW)

### 14.1 Eight Hypotheses (from deep analysis)

**H1: Recency Bias** — LLMs weight the most recently received content (last ~2K tokens) much more than earlier context. When `zerops_knowledge scope="infrastructure"` delivers 9K tokens after step guidance, rules from step guidance are displaced. Rules that appear in BOTH scope AND step guidance survive displacement; rules only in step guidance may be forgotten.

**H2: Repetition Diminishing Returns** — First 2-3 mentions of a rule establish it. Mentions 4-7 reinforce at different action points. Mentions 8+ create cognitive clutter — LLM spends attention processing "is this new or same?" instead of following the rule. **Optimal: 3-5 mentions per knowledge dump.** Beyond that, each additional mention has negative marginal value. `deployFiles` at 21 mentions is well past the crossover.

**H3: Structured > Unstructured** — Rules in structured format (tables, `ALWAYS`/`NEVER` prefix, checklists, JSON) are followed more reliably than rules in prose. The `core.md` "Rules & Pitfalls" section with `**ALWAYS**`/`**NEVER** + REASON` format maps cleanly to LLM pattern-matching. The causal chains table is particularly effective as a direct lookup.

**H4: Volume Penalty Beyond ~4K Tokens** — The deploy section (6,986 tokens) causes decreased compliance on rules stated early in the dump vs rules near the action items. The provision section (~1,128 tokens) has much higher rule compliance. **Recommendation: cap step guidance at ~3,500 tokens.**

**H5: Example > Description** — YAML templates are the most copied artifact. LLMs reproduce examples almost verbatim. If the template shows `deployFiles: [.]`, that's followed — even if prose elsewhere contradicts. The template in the subagent prompt is the most effective knowledge delivery per token. **Implication: invest in correct templates, not verbose prose.**

**H6: Context Rot in Multi-Step Workflows** — By step 4, the LLM has consumed 12K+ tokens of knowledge. PriorContext attestations from steps 0-3 become noise. The discover attestation is useful at step 1; by step 4 it's an historical artifact consuming tokens without driving action.

**H7: Gate Failure Messaging Too Abstract** — `"Record required evidence before transitioning"` is procedurally correct but behaviorally insufficient. The LLM may record evidence without performing verification. Structured failure with `{action, tool, params, explanation}` drives correct recovery.

**H8: Subagent Prompt is Gold Standard** — The Service Bootstrap Agent Prompt (~3,250 tokens) is the most effective knowledge delivery because: (1) self-contained, (2) uses concrete pre-resolved values, (3) numbered task table with verification, (4) recovery patterns table, (5) combines rules and examples in one document. All other delivery should aspire to this pattern.

### 14.2 Delivery Pattern Guidelines

| Aspect | Current | Recommended |
|--------|---------|-------------|
| Guidance size per step | 500-7,000 tokens | Max 3,500 tokens |
| Rule repetition per dump | Up to 21 | 3-5 (2-3 full + N short refs) |
| Critical rule placement | Scattered in prose | Near action point + in YAML template + in verification error |
| Rule phrasing | 40% negation ("Do NOT") | Positive directives ("Map ONLY variables from discovery") |
| Iteration guidance | Full re-delivery | Delta only (what changed, what to try) |
| PriorContext | All attestations verbatim | Last step full, older compressed |
| Gate failures | "missing evidence: X" | Structured remediation with tool+params |

### 14.3 Anti-Patterns to Avoid

1. **Knowledge dump without action anchor**: `scope="infrastructure"` returns 9K tokens of reference. LLMs are action-oriented — "do X" is processed more reliably than "here is everything you might need"
2. **Conflicting authority between layers**: `Guidance` and `DetailedGuide` can diverge since maintained in different files
3. **Negation-heavy rules**: "Do NOT" is less reliable than "Do X instead"
4. **Exception proliferation**: `"Implicit-webserver runtimes (php-nginx, php-apache, nginx, static)"` repeated 11+ times creates visual noise; define once, reference by label
5. **Volume-error correlation**: Steps with ~500 tokens guidance (discover, verify) have low error rates; steps with ~7K tokens (deploy) are primary error source — volume exacerbates inherent difficulty

---

## 15. Complete Knowledge Delivery Channel Map (NEW)

### 15.1 Seven Delivery Channels

| # | Channel | Trigger | Tokens | Push/Pull | LLM Control |
|---|---------|---------|--------|-----------|-------------|
| 1 | System prompt | MCP startup | 400-1,200 | Push | None |
| 2 | Bootstrap step guidance | Step transition | 200-7,000/step | Push | Partial (controls step progression) |
| 3 | Workflow markdown | `zerops_workflow` | 400-12,000 | Push | Yes (chooses workflow) |
| 4 | Knowledge system | `zerops_knowledge` | 500-9,000 | Pull | Yes (chooses mode + params) |
| 5 | State + prior context | Every step response | 0-2,000 (growing) | Push | None |
| 6 | Next actions | Every tool response | 50-200 | Push | None |
| 7 | Tool responses | Any tool call | 100-5,000 | Pull | Yes (chooses which tools) |

### 15.2 Per-Step Token Delivery

| Step | DetailedGuide | PriorContext | Stacks | scope/briefing | Total |
|------|--------------|-------------|--------|---------------|-------|
| 0 discover | 1,368 | 0 | 500 | — | 1,868 |
| 1 provision | 1,128 | 200 | 500 | — | 1,828 |
| 2 generate | 1,742 | 400 | 500 | 8,967+3,000 | 14,609 |
| 3 deploy | 6,986 | 600 | 500 | — | 8,086 |
| 4 verify | 561 | 800 | 500 | — | 1,861 |
| **Total** | **11,785** | **2,000** | **2,500** | **~12,000** | **~28,252** |

### 15.3 Cross-Channel Redundancy Map

| Content | Channels Present | Overlap Tokens |
|---------|-----------------|---------------|
| Universals | scope(4) + recipe(4) | ~1,000 |
| Runtime guide | briefing(4) + recipe(4) | ~800 |
| Live stacks | briefing(4) + workflow(3) + bootstrap response(2) | ~400 |
| Service cards | briefing(4) + scope(4) | ~600 |
| Deploy rules | step guidance(2) + scope(4) | ~500 |
| **Total cross-channel waste** | | **~3,300** |

### 15.4 Knowledge Storage Structure

```
internal/knowledge/
├── themes/       core.md(8K), universals.md(1K), services.md(4K), operations.md(3.5K)
├── runtimes/     10+ per-runtime guides (700-1,200 tokens each)
├── recipes/      29 framework configs (500-1,500 tokens each)
├── guides/       18 topic guides (500-2,000 tokens each)
└── decisions/    2-3 architectural choice docs

internal/content/workflows/
├── bootstrap.md  785 lines (~12K tokens) — 5 <section> tags for step extraction
├── deploy.md     213 lines (~3K tokens)
├── debug.md      161 lines (~500 tokens)
├── configure.md  124 lines (~400 tokens)
├── monitor.md    144 lines (~450 tokens)
└── scale.md      114 lines (~350 tokens)
```

---

## 16. Comprehensive Token Budget — Before vs After (NEW)

### 16.1 First-Run Bootstrap (standard, 1 service pair, with managed services)

| Component | Current | After Wave 1 | After Wave 2 | After Wave 3 | After Wave 4 |
|-----------|---------|-------------|-------------|-------------|-------------|
| DetailedGuide (5 steps) | 11,785 | 8,785 (-3K dedup) | 8,785 | ~5,400 (mode-filtered) | ~5,400 |
| scope="infrastructure" | 8,967 | 8,167 (-800 dedup) | 8,167 | 8,167 | 8,167 |
| Briefing | 3,000 | 3,000 | 3,000 | ~1,800 (dedup) | ~1,800 |
| AvailableStacks | 2,500 | 2,500 | 500 (1x only) | 500 | 500 |
| PriorContext | 2,000 | 2,000 | 600 (compressed) | 600 | 600 |
| Inline Guidance | 607 | 607 | 0 (removed) | 0 | 0 |
| **Total** | **28,859** | **25,059** | **21,052** | **16,467** | **16,467** |
| **Savings** | — | **13%** | **27%** | **43%** | **43%** |

### 16.2 Iteration 2 (deploy fails, retry)

| Component | Current | After Wave 2 | After Wave 3 |
|-----------|---------|-------------|-------------|
| DetailedGuide | 7,547 | ~200 (delta) | ~200 |
| scope/briefing re-calls | 12,000 | 0 (tracked) | 0 |
| PriorContext | 800 | 200 | 200 |
| Stacks | 500 | 0 | 0 |
| **Total** | **20,847** | **~400** | **~400** |

### 16.3 Three-Iteration Cumulative

| Scenario | Current | After All Waves |
|----------|---------|----------------|
| Run 1 | 28,859 | 16,467 |
| Iteration 2 | 20,847 | 400 |
| Iteration 3 | 20,847 | 400 |
| **Total** | **70,553** | **17,267** |
| **Savings** | — | **76%** |

---

## 17. Extended Reusable Code Reference (NEW)

| Function | File | Reuse For |
|----------|------|-----------|
| `ReadServiceMeta` / `WriteServiceMeta` | `service_meta.go` | Strategy persistence |
| `ResolveGuidance(step)` | `bootstrap_guidance.go` | Base for progressive resolver |
| `extractSection(md, name)` | `bootstrap_guidance.go:20-32` | Deploy sub-section extraction, deploy guidance |
| `DetectProjectState()` | `managed_types.go` | Router input |
| `ListSessions()` | `registry.go` | Router input |
| `validateConditionalSkip()` | `bootstrap.go` | Strategy step skip logic, gate simplification model |
| `populateStacks()` / `injectStacks()` | `tools/workflow.go` | Stack catalog injection + gating |
| `jsonResult()` / `textResult()` | `tools/convert.go` | Response formatting |
| `content.GetWorkflow()` | `content/content.go` | Markdown loading |
| `buildPriorContext()` | `bootstrap.go:235-251` | Modify for compression |
| `planMode()` | `bootstrap.go:254` | Already computes standard/simple for progressive guidance |
| `KnowledgeTracker` | `ops/knowledge_tracker.go` | Extend with session persistence + dedup |
| `Document.H2Sections()` | `knowledge/documents.go` | Lazy section parsing for scope sub-sections (future) |
| `FormatStackList()` / `FormatServiceStacks()` | `knowledge/versions.go` | Two formats for different contexts |
| `GetBriefing()` | `knowledge/briefing.go` | 7-layer composition, extend with depth/dedup |
| `prependUniversals()` | `knowledge/briefing.go` | Guard with tracker to prevent double-delivery |
| `GateResult` | `gates.go` | Extend with Remediation field |
| `CheckGate()` | `gates.go` | Extend with ContextDelivery + complexity params |
