# ZCP Workflow Evolution — Knowledge Document

Comprehensive analysis of current workflow system, context delivery, and design for dynamic flow routing, post-bootstrap strategies, CI/CD pipeline generation, and local dev VPN architecture.

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

**Problem**: Routing in Section D is hardcoded switch/case. No awareness of per-service deploy strategies, no dynamic flow offerings.

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

**Dual delivery problem**: LLM receives BOTH `guidance` (100 words) AND `detailedGuide` (2000+ words) for the same step. No indication which is authoritative.

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

### 3.1 Dual Delivery Mechanism (~2x token waste)

Bootstrap steps deliver guidance through TWO channels simultaneously:
1. **Inline `StepDetail.Guidance`** (bootstrap_steps.go) — compact, ~100 words
2. **`DetailedGuide`** via `ResolveGuidance()` — full `<section>` extract, ~2000 words

Both arrive in `BootstrapStepInfo`. LLM sees both, doesn't know which to trust. Typically uses the longer one, consuming 2x tokens.

### 3.2 Massive Redundancy (~40% of tokens)

Rules repeated across multiple locations:

| Rule | Occurrences | Locations |
|------|-------------|-----------|
| `deployFiles: [.]` for self-deploy | 6x | generate(3), deploy(2), provision(1) |
| Dev uses `start: zsc noop --silent` | 3x | generate, deploy, bootstrap_steps.go |
| Implicit-webserver skip manual start | 4x | generate, deploy(2), deploy subsection |
| Shared storage two-stage mount | 3x | provision, bootstrap_steps.go, deploy |
| Env var discovery protocol | 4x | provision, generate, bootstrap_steps, agent prompt |

### 3.3 Section Boundary Problems

Sections organized by STEP (discover, provision, generate, deploy, verify) not by CONCEPT. Related information scattered:
- "Dev vs stage" explained in generate AND deploy
- "Port assignment" in generate AND in knowledge briefings
- "Env var references" in provision AND generate AND agent prompts

### 3.4 Context Window Load per Bootstrap

| Stage | New tokens | Cumulative |
|-------|-----------|-----------|
| Start (full bootstrap.md) | ~11,000 | 11,000 |
| Step discover (section extract + inline) | ~2,800 | 13,800 |
| Step provision | ~2,800 | 16,600 |
| Step generate | ~3,000 | 19,600 |
| Step deploy | ~2,800 | 22,400 |
| Step verify | ~1,500 | 23,900 |

**~24K tokens** of guidance for a single bootstrap, with ~40% being duplicated content.

### 3.5 Agent Spawning Context Isolation

When parent agent spawns service bootstrap agents (for multi-service projects):
- Each subagent gets 150-line embedded prompt from bootstrap.md
- Subagent does NOT receive: parent decisions, errors from other services, shared context
- 10 services = 1,500 tokens of pure redundancy in agent prompts

### 3.6 Deploy.md Phase Terminology Clash

Deploy.md uses "Phase 1" and "Phase 2" — these conflict with workflow engine phases (INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE). Creates confusion for agents reading both.

### 3.7 Missing Success Criteria

Steps list actions but don't state explicit completion conditions. LLM must infer when a step is "done."

### 3.8 No Post-Bootstrap Strategy Awareness

After bootstrap completes:
- ServiceMeta written but `decisions` map empty
- Deploy workflow returns full deploy.md regardless of how user wants to work
- System prompt has no strategy-specific routing
- No mechanism to ask "how do you want to deploy going forward?"

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
    Mode           runtime.Mode       // container, local, local_vpn
    ProjectState   ProjectState       // FRESH, CONFORMANT, NON_CONFORMANT
    ServiceMetas   []*ServiceMeta     // per-service bootstrap records
    ActiveSessions []SessionEntry     // currently active workflow sessions
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
| NON_CONFORMANT | none | — | bootstrap (p=1), debug (p=2) |
| any | any | any | always include: debug(p=5), scale(p=5), configure(p=5) |

### 4.4 Integration with instructions.go

`buildProjectSummary` currently has hardcoded `switch projState`. Replace with:
1. `ListServiceMetas(stateDir)` — new function reading all `services/*.json`
2. `Route(input)` — pure function, no I/O
3. `formatOfferings(offerings)` — render into system prompt text

### 4.5 runtime.Mode Extension

```go
// internal/runtime/runtime.go
type Mode string
const (
    ModeContainer Mode = "container"
    ModeLocal     Mode = "local"
    ModeLocalVPN  Mode = "local_vpn"  // future
)
```

Add `Mode` field to `Info`. Detect: if `serviceId` → container; if `ZCP_MODE=local` → localVPN; else → local.

### 4.6 Key Design Decision

Router is a **pure function**, not a method on Engine. It has no mutable state, no I/O. Data collection happens in `BuildInstructions`; routing is deterministic logic. This makes it trivially testable with table-driven tests.

---

## 5. Design: Post-Bootstrap Deploy Strategies

### 5.1 Three Strategies

| ID | Name | Best For | How it works |
|----|------|----------|-------------|
| `push-dev` | Simple Push | Prototyping, solo dev | SSH push dev→dev, instructed push to stage |
| `ci-cd` | CI/CD Pipeline | Production, teams | Git push → pipeline → stage (RECOMMENDED) |
| `manual` | Manual | Existing tooling | No ZCP flow, monitoring only |

### 5.2 Strategy Capture: 6th Bootstrap Step

Add `strategy` as step 6 after verify. Rationale:
- Clean separation: verify = health check, strategy = future workflow choice
- Explicit step with own attestation and guidance
- Can't be skipped or conflated with verification
- UX: "Everything works → now choose how to work going forward"

```go
// bootstrap_steps.go
{
    Name:     "strategy",
    Category: CategoryFixed,
    Guidance: "Ask user to choose deployment strategy for each runtime service",
    Tools:    []string{"zerops_workflow"},
    Verification: "Deploy strategy recorded for all runtime services",
    Skippable: false,
}
```

### 5.3 Strategy Persistence

Store in `ServiceMeta.Decisions["deployStrategy"]`. Constants:

```go
const (
    StrategyPushDev = "push-dev"
    StrategyCICD    = "ci-cd"
    StrategyManual  = "manual"
    DecisionDeployStrategy = "deployStrategy"
)
```

On `BootstrapComplete(step="strategy")`: engine writes strategy per-target to ServiceMeta.

### 5.4 Strategy Change After Bootstrap

New `zerops_workflow action="strategy"` handler:
- Reads ServiceMeta for given hostname
- Updates `Decisions["deployStrategy"]`
- Returns confirmation + new guidance for chosen strategy

This gives: clean capture during bootstrap + ability to change anytime later.

### 5.5 Strategy-Aware Deploy Guidance

Add to `deploy.md` three `<section>` blocks:

- `<section name="deploy-push-dev">` — SSH push flow, dev-first then stage
- `<section name="deploy-ci-cd">` — Git-centric, pipeline guidance, CI/CD templates
- `<section name="deploy-manual">` — Monitoring-only, available tools list

New `deploy_guidance.go`:
```go
func ResolveDeployGuidance(stateDir, hostname string) string
// Reads ServiceMeta → maps strategy → extracts section from deploy.md
// Falls back to full deploy.md if no strategy set
```

When `action="start" workflow="deploy"`: read ServiceMetas, return strategy-specific section.

---

## 6. Design: CI/CD Pipeline Generation

### 6.1 Approach

When user chooses `ci-cd` strategy, ZCP generates actual pipeline config files (not just guidance).

### 6.2 Templates

Embedded in `internal/content/templates/pipelines/`:

- `github-actions.yml.tmpl` → `.github/workflows/zerops-deploy.yml`
- `gitlab-ci.yml.tmpl` → `.gitlab-ci.yml`
- `bitbucket-pipelines.yml.tmpl` → `bitbucket-pipelines.yml`

Go `text/template` with `{{.Hostnames}}`, `{{.TokenVar}}`, `{{.ZeropsYmlPath}}`.

### 6.3 Generation API

```go
// internal/ops/pipeline.go
type PipelineParams struct {
    Platform  string   // "github-actions", "gitlab-ci", "bitbucket-pipelines"
    Hostnames []string // stage hostnames
    RepoRoot  string   // where to write
}

type PipelineResult struct {
    FilePath string
    Content  string
    TokenVar string // e.g., ZEROPS_TOKEN
}

func GeneratePipeline(params PipelineParams) (*PipelineResult, error)
```

### 6.4 Integration

Strategy step guidance instructs the agent to:
1. Ask which CI/CD platform (GitHub Actions, GitLab CI, Bitbucket Pipelines)
2. Generate pipeline file via `GeneratePipeline()`
3. Tell user to add `ZEROPS_TOKEN` as repository secret

**Decision**: Implement as Go helper first. If needed for reliability, promote to MCP tool (`zerops_pipeline`) later.

---

## 7. Design: Local Dev VPN Flow (Architecture Only)

### 7.1 Concept

Developer works from local machine connected via VPN to Zerops project. No dev service — local machine IS the dev environment. Managed services + stage in project, accessed over VPN.

### 7.2 Key Differences from Container Mode

| Aspect | Container Mode | Local Dev Mode |
|--------|---------------|----------------|
| Code location | Zerops container (SSHFS) | Local filesystem |
| Dev service | Yes | No (local machine) |
| Managed services | Internal DNS | VPN IP/DNS ({hostname}.zerops) |
| Stage service | Deploy from dev via SSH | Deploy from local via API archive |
| Env vars | Container-injected | .env file generation needed |
| Deployment | SSH self-deploy | API 3-step: CreateVersion → Upload → Deploy |

### 7.3 VPN Detection

Explicit over implicit: `ZCP_MODE=local` env var + absence of `serviceId` = `ModeLocalVPN`.

No automatic VPN detection (fragile, OS-specific). Optional connectivity verification (ping managed service) as best-effort check.

### 7.4 Bootstrap Adaptation

| Step | Container | Local VPN |
|------|-----------|-----------|
| discover | unchanged | unchanged |
| provision | dev+stage + mount + env discovery | stage only + managed + .env generation, NO mount |
| generate | write to SSHFS mount | write to local filesystem, zerops.yml for stage only |
| deploy | SSH self-deploy | API archive upload (zerops-go SDK: PostServiceStackAppVersion → PutAppVersionUpload → PutAppVersionDeploy) |
| verify | verify dev+stage | verify stage only |
| strategy | all 3 options | ci-cd or manual only (no push-dev without dev service) |

### 7.5 API Deployer Architecture

```go
// internal/platform/api_deployer.go
type APIDeployer struct { client *Client }
func (d *APIDeployer) Deploy(ctx context.Context, serviceID, sourceDir string) (*DeployResult, error)
// 1. Archive sourceDir as tar.gz (respect .gitignore, .zeropsignore)
// 2. PostServiceStackAppVersion → appVersionId + uploadUrl
// 3. PutAppVersionUpload → upload archive
// 4. PutAppVersionDeploy → trigger build pipeline, return Process
// 5. Poll build status via existing PollBuild mechanism
```

### 7.6 Extension Points to Prepare Now

1. `runtime.Info.Mode` field — add now, defaults derived from InContainer, no behavior change
2. `Deployer` interface in `internal/ops/deploy.go` — extract from current SSH impl
3. `DevMode` field on `WorkflowState` — `omitempty`, unused until implemented
4. `RouterInput.Mode` — router already accepts it, local VPN adds new routing rules later

### 7.7 NOT Implemented Now

- VPN connectivity verification
- API archive deployer
- .env file generation (`GenerateEnvFile()`)
- Bootstrap step adaptations for local mode
- Local-dev guidance sections in workflow markdown

### 7.8 Risk: Archive Format Compatibility

Zerops build pipeline may expect zcli-specific tar.gz format (with `.deploy.zerops` metadata). Need to verify against zerops-go SDK or zcli source. Highest risk item for API deployer.

---

## 8. Context Delivery Optimization Recommendations

### 8.1 Eliminate Dual Delivery

Choose ONE source per step: either `StepDetail.Guidance` (inline) or `DetailedGuide` (section extract). Not both.

**Recommendation**: Keep only `DetailedGuide` from `<section>` tags. Remove inline `Guidance` field from `StepDetail` or reduce it to a single-sentence summary.

### 8.2 Extract Reference Rules into Shared Knowledge

Create a separate "Zerops Rules Reference" that's cited, not embedded in every section:
- `deployFiles: [.]` rule
- Implicit webserver behavior
- Shared storage two-stage mount
- Env var reference syntax
- Port assignment defaults

This reference would be in `zerops_knowledge scope="rules"` or a dedicated section, loaded once and referenced by name in step guidance.

### 8.3 Add Explicit Success Criteria

Every step guidance should end with:
```
SUCCESS WHEN: [explicit condition]
NEXT: [what to do]
```

### 8.4 Improve Agent Handoff

Parent agents should pass a decision log to subagents:
- What has been tried
- Errors encountered on other services
- Shared decisions (e.g., "all services use port 3000")

### 8.5 Restructure Docs for Retrieval

Organize by concept (env var patterns, dev iteration, deployment) not by step order. Use `<section>` tags for concept-based extraction.

### 8.6 Fix Phase Terminology

Rename deploy.md's "Phase 1/2" to "Stage 1/2" or "Part 1/2" to avoid collision with workflow engine phases.

### 8.7 Estimated Token Savings

| Change | Current | After | Saving |
|--------|---------|-------|--------|
| Remove dual delivery | ~24K | ~13K | ~11K (46%) |
| Deduplicate rules | ~13K | ~9K | ~4K (31%) |
| Strategy-specific deploy | ~2.4K full | ~0.8K section | ~1.6K (67%) |
| Total bootstrap flow | ~24K | ~9K | ~15K (63%) |

---

## 9. File Inventory for Changes

### New Files

| File | Purpose | Est. Lines |
|------|---------|-----------|
| `internal/workflow/router.go` | Pure routing function + types | ~130 |
| `internal/workflow/router_test.go` | Table-driven routing tests | ~200 |
| `internal/workflow/deploy_guidance.go` | Strategy-aware section resolver | ~50 |
| `internal/ops/pipeline.go` | CI/CD pipeline generator | ~150 |
| `internal/ops/pipeline_test.go` | Pipeline generation tests | ~100 |
| `internal/content/templates/pipelines/*.tmpl` | 3 pipeline templates | ~30 each |

### Modified Files

| File | Changes |
|------|---------|
| `internal/runtime/runtime.go` | Add Mode type + field (+12 lines) |
| `internal/workflow/service_meta.go` | Add ListServiceMetas(), strategy constants, helper (+40) |
| `internal/workflow/bootstrap_steps.go` | Add step 6 "strategy" (+15) |
| `internal/workflow/bootstrap.go` | Strategy completion logic (+30) |
| `internal/workflow/bootstrap_evidence.go` | Write strategy to ServiceMeta on complete (+10) |
| `internal/server/instructions.go` | Replace hardcoded routing with Route() (~50 changed) |
| `internal/tools/workflow.go` | Add "strategy" action, deploy routing (+40) |
| `internal/content/workflows/bootstrap.md` | Add `<section name="strategy">` (+40) |
| `internal/content/workflows/deploy.md` | Add 3 strategy sections (+100) |

### Existing Reusable Code

| Function | File | Reuse For |
|----------|------|-----------|
| `ReadServiceMeta` / `WriteServiceMeta` | `service_meta.go` | Strategy persistence |
| `ResolveGuidance(step)` | `bootstrap_guidance.go` | Deploy section extraction |
| `DetectProjectState()` | `managed_types.go` | Router input |
| `ListSessions()` | `registry.go` | Router input |
| `populateStacks()` / `injectStacks()` | `tools/workflow.go` | Stack catalog injection |
| `jsonResult()` / `textResult()` | `tools/convert.go` | Response formatting |
| `content.GetWorkflow()` | `content/content.go` | Markdown loading |

---

## 10. MCP Tool Catalog (Complete)

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
