# ZCP Workflow Evolution — Knowledge Document (Stress-Tested)

Analysis of current workflow system, context delivery, and validated designs for dynamic flow routing, post-bootstrap strategies, and context optimization. Includes stress-test results from 30+ edge case simulations.

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

| Mode | Input | Output | Token estimate |
|------|-------|--------|---------------|
| scope="infrastructure" | — | Complete platform reference: YAML schemas, env var system, build/deploy lifecycle | ~3000-4000 |
| briefing | runtime + services | Stack-specific: binding rules, ports, env vars, wiring, version check | ~1500-2500 |
| query | search text | BM25 results: [{uri, title, score, snippet}] | ~500-1000 |
| recipe | name | Full recipe markdown: title, keywords, TL;DR, YAML, gotchas | ~800-1500 |

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

### 3.3 Corrected Token Budget

**The original 24K estimate was wrong.** It double-counted the legacy path (full bootstrap.md dump) and conductor path (per-step sections), which are mutually exclusive code paths.

Actual measured delivery via conductor path:
| Stage | Tokens |
|-------|--------|
| Inline Guidance (all 5 steps) | ~607 |
| DetailedGuide (all 5 steps) | ~11,737 |
| Deploy section dominates | ~6,901 (59% of total) |
| **Total per bootstrap** | **~12,344** |

### 3.4 Deploy.md Terminology Clash

Deploy.md uses "Phase 1" and "Phase 2" — these conflict with workflow engine phases (INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE). Creates confusion for agents reading both.

### 3.5 Missing Success Criteria

Steps list actions but don't state explicit completion conditions. `Verification` field exists but is vague (e.g., "All services created, dev mounted, env vars discovered"). LLM must infer when a step is "done."

### 3.6 No Post-Bootstrap Strategy Awareness

After bootstrap completes:
- ServiceMeta written but `decisions` map empty, `deployFlow` always empty
- Deploy workflow returns full deploy.md regardless of how user wants to work
- System prompt has no strategy-specific routing
- No mechanism to ask "how do you want to deploy going forward?"

### 3.7 Abandoned Bootstrap Data Loss

**BUG**: `writeBootstrapOutputs` in `bootstrap_evidence.go` only fires when `Bootstrap.Active` becomes `false` (after ALL steps complete via `autoCompleteBootstrap`). If user abandons bootstrap after 4 successful steps → zero ServiceMeta files written to disk. All context from the session is lost.

Additionally:
- No session resume mechanism exists. Orphaned session JSON files persist but cannot be re-attached.
- `pruneDeadSessions` removes from registry but does NOT delete `sessions/{id}.json` or `evidence/{id}/`.

### 3.8 Bootstrap Exclusivity TOCTOU Race

`ListSessions()` + `InitSession()` in `engine.go` are not in the same lock scope. Two processes can both pass the "no active bootstrap" check and both start bootstrap. Window is small but exists.

### 3.9 ServiceMeta Overwrites for Shared Dependencies

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

## 8. Context Delivery Optimization — Validated Changes

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

The deploy section in bootstrap.md is ~6,901 tokens — 59% of total guidance. Split into subsections loadable by planMode (standard vs simple). When a service uses Simple mode, serve only the simple deployment guidance.

**Implementation**: Split `<section name="deploy">` into `<section name="deploy-standard">` and `<section name="deploy-simple">`. Modify `ResolveGuidance()` to accept optional planMode parameter. `bootstrap.go:221` passes `planMode` from BootstrapState.

**Savings**: ~1,500-2,500 tokens per deploy step depending on mode.

### 8.3 Reduce Deploy Section Redundancy (IMPLEMENT)

Consolidate repeated rules within bootstrap.md:
- `deployFiles: [.]` — from 21 mentions to 4 (section header, agent prompt, simple mode, iteration loop)
- `zsc noop --silent` — from 9 mentions to 3 (generate, deploy, simple mode)
- Preserve contextual proximity — keep rules near point of use, just eliminate verbatim repetition

**Savings**: ~500-800 tokens. More importantly, reduces maintenance burden.
**Risk**: Zero (markdown-only change, no code impact).

### 8.4 Expand Verification with SUCCESS WHEN (IMPLEMENT)

Replace vague verification strings with explicit criteria:

Before: `"All services created, dev mounted, env vars discovered"`
After: `"SUCCESS WHEN: all plan services exist in API with ACTIVE status AND dev filesystems mounted AND env vars recorded in session state. NEXT: proceed to generate step."`

**Cost**: ~250 additional tokens total. **Benefit**: Improves attestation quality, gives LLM clear completion signal.

### 8.5 Fix Phase Terminology (IMPLEMENT)

Rename deploy.md's "Phase 1" / "Phase 2" to "Part 1: Configuration Check" / "Part 2: Deploy and Monitor". Prevents confusion with engine phases.

### 8.6 DROPPED Optimizations

| Proposal | Why Dropped |
|----------|-------------|
| Extract rules to separate `zerops_knowledge scope="rules"` | Adds mandatory tool call dependency. LLMs may skip it. Loses contextual proximity. Saves only ~500 tokens — achievable via in-place dedup instead. |
| Concept-based doc restructure | Breaks 1:1 step-to-section mapping in `ResolveGuidance(step)`. 13 tests need rewriting. No actual token savings — multiple section extracts per step would be equal or larger. |
| Agent handoff decision log | `PriorContext` at `bootstrap.go:235-251` already collects prior attestations and plan. Adding separate decision log is redundant. |

### 8.7 Corrected Token Savings Estimate

| Change | Savings | Risk |
|--------|---------|------|
| Remove dual delivery (inline guidance) | ~607 tokens | Low (4 tests to update) |
| Conditional deploy by planMode | ~1,500-2,500 tokens | Low (section split + ResolveGuidance change) |
| Deduplicate repeated rules | ~500-800 tokens | Zero (markdown only) |
| Strategy-specific deploy sections | ~1,600 tokens (for deploy workflow) | Low (new sections in deploy.md) |
| Total | **~4,200-5,500 tokens (~35-45%)** | |

Realistic improvement from 12,344 → ~7,000-8,000 tokens per bootstrap.

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

## 10. Implementation Waves

### Wave 1 — Cleanup (no dependencies, all S-scope)

| # | Action | Files |
|---|--------|-------|
| 1 | Delete `DeployFlow` field, migrate test fixtures to `Decisions["deployStrategy"]` | `service_meta.go`, `service_meta_test.go`, `bootstrap_evidence.go` |
| 2 | Stop serializing inline `Guidance` to LLM responses | `bootstrap.go:216`, update 4 tests |
| 3 | Rename "Phase 1/2" to "Part 1/2" in deploy.md | `deploy.md` |
| 4 | Expand `Verification` strings with `SUCCESS WHEN:` criteria | `bootstrap_steps.go` |
| 5 | Add `ListServiceMetas(stateDir)` function + tests | `service_meta.go`, `service_meta_test.go` |
| 6 | Add strategy constants (`StrategyPushDev`, `StrategyCICD`, `StrategyManual`, `DecisionDeployStrategy`) | `service_meta.go` |

### Wave 2 — Router + Deploy Guidance (depends on wave 1)

| # | Action | Files | Scope |
|---|--------|-------|-------|
| 7 | Implement `Route()` pure function with table-driven tests | New: `router.go`, `router_test.go` | M |
| 8 | Add strategy-specific `<section>` tags to deploy.md | `deploy.md` | S |
| 9 | Create `ResolveDeployGuidance()` reusing `extractSection` pattern | New: `deploy_guidance.go`, `deploy_guidance_test.go` | S |
| 10 | Add `DeleteServiceMeta()` + hook in `zerops_delete` | `service_meta.go`, `tools/delete.go` | S |
| 11 | Skip shared-dep meta overwrite for EXISTS/SHARED deps | `bootstrap_evidence.go` | S |
| 12 | Deduplicate repeated rules in bootstrap.md | `bootstrap.md` | S |

### Wave 3 — Strategy System (depends on wave 2)

| # | Action | Files | Scope |
|---|--------|-------|-------|
| 13 | Wire router into `buildProjectSummary`, add `StateUnknown` | `instructions.go`, `managed_types.go` | M |
| 14 | Add "strategy" step 6 with `Skippable: true` + auto-skip | `bootstrap_steps.go`, `bootstrap.go` | M |
| 15 | Add `Strategies` map to BootstrapState for structured capture | `state.go`, `bootstrap.go` | S |
| 16 | Add `action="strategy"` handler for post-bootstrap changes | `tools/workflow.go` | M |
| 17 | Implement incremental ServiceMeta writes (after provision) | `bootstrap_evidence.go`, `service_meta.go` | M |
| 18 | Fix bootstrap exclusivity TOCTOU race | `engine.go` | S |
| 19 | Conditional deploy section by planMode | `bootstrap_guidance.go`, `bootstrap.go`, `bootstrap.md` | M |

### Wave 4 — Pipeline + Future (depends on wave 3 + design decisions)

| # | Action | Blocker |
|---|--------|---------|
| 20 | Clarify zcli prohibition scope in CLAUDE.local.md | User confirmation needed |
| 21 | Implement pipeline generation (MCP tool or guidance-only) | Design decision on approach |
| 22 | Add `Mode` field to `runtime.Info` (no-op extension) | None |

### Dependency Graph

```
Wave 1: [1,2,3,4,5,6] ─── all independent, parallel
           │
Wave 2: [7←5, 8, 9←8, 10, 11, 12] ─── 7 needs ListServiceMetas; 9 needs sections
           │
Wave 3: [13←7, 14←1+4, 15, 16←14, 17, 18, 19] ─── router wiring needs router; strategy step needs DeployFlow cleanup
           │
Wave 4: [20, 21←14+20, 22] ─── pipeline needs strategy system + zcli clarification
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
