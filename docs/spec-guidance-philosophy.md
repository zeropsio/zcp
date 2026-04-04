# ZCP Guidance & Knowledge Delivery Specification

> **Status**: Authoritative — all guidance assembly, knowledge injection, init instructions, and workflow responses MUST conform to this document.
> **Scope**: All ZCP workflows and tool responses. Both container and local environments.
> **Environment**: Currently only container mode is implemented. Local mode is deferred; `Engine.Environment()` is the switching point.
> **Date**: 2026-03-22

---

## 1. Purpose

This document defines HOW and WHEN ZCP delivers knowledge and guidance to the AI agent. It is the canonical reference for:
- What information gets injected into workflow responses
- What information the agent pulls on demand
- How init instructions orient the agent
- How the system adapts to different user scenarios
- How transitions between workflows work

**Companion documents**:
- `docs/spec-bootstrap-deploy.md` — workflow step specifications (WHAT happens)
- This document — guidance delivery philosophy (HOW knowledge flows)

---

## 2. Core Principles

### 2.1 Help, Don't Gatekeep

**We don't know what the user wants from their application.** We don't know how it should work, what state the code is in, whether they want health checks, or what "healthy" means. We DO know:

- Mode of services (standard/dev/simple) — and they may want to change it
- Strategy (push-dev/push-git/manual) — and they may want to switch
- Zerops platform mechanics — how the deploy pipeline works, what survives deploy, env var behavior, zerops.yml schema

ZCP validates **platform integration** (always objectively correct/incorrect) and provides **contextual guidance** (Zerops-specific knowledge). It never imposes assumptions about application correctness.

### 2.2 Dumb Data, Smart Agent

ZCP provides **correct, structured data** and lets the LLM decide what to do with it. ZCP does NOT:
- Recommend which workflow to run
- Suggest what the user should build
- Decide whether code is ready to deploy
- Judge whether an application is "working"

ZCP DOES:
- Return factual state (services, status, env vars, health checks)
- Validate platform integration (zerops.yml syntax, hostname matching, env var references)
- Deliver Zerops-specific knowledge the agent cannot infer (container lifecycle, env var behavior, mode-specific workflows)
- Point to where additional knowledge is available

### 2.3 Reactive, Not Proactive

ZCP responds to what the agent asks for. It does not push information preemptively.

- Agent starts deploy workflow → ZCP returns personalized guidance for THEIR setup
- Agent encounters error → ZCP provides diagnostic guidance based on WHERE it failed
- Agent needs runtime knowledge → agent calls `zerops_knowledge`
- Agent doesn't need Zerops → ZCP stays out of the way

### 2.4 Inject Rules, Point to Knowledge

Two categories of information, two delivery methods:

| Category | Delivery | Rationale |
|----------|----------|-----------|
| **Platform mechanics** (container lifecycle, env var behavior, deploy pipeline) | INJECT — always included in workflow responses | Agent cannot infer these. Always relevant. Compact (10-15 lines). |
| **Mode/strategy workflow** (step sequence, commands) | INJECT — personalized to current setup | Agent MUST follow correct order. Specific to their mode/strategy. |
| **Runtime knowledge** (Node.js specifics, Go specifics) | POINT — "zerops_knowledge query=..." (session-aware: auto-filters by mode) | Agent may not need it. Verbose. Available on demand. |
| **Recipe patterns** (Next.js, Laravel) | POINT — "zerops_knowledge recipe=..." (session-aware: mode-adapted headers) | Framework-specific. Agent pulls when relevant. |
| **Schema details** (zerops.yml full schema) | POINT — "zerops_knowledge query='zerops.yml'" | Verbose reference. Agent pulls when modifying config. |
| **Environment data** (env vars, service status) | POINT — "zerops_discover" | Dynamic. Agent calls when it needs current state. |

### 2.5 Personalized, Not Generic

Guidance is assembled from the agent's ACTUAL state (DeployState, ServiceMeta, Environment), not from generic templates. The agent sees:

- Their specific hostnames, types, and modes
- Their specific workflow steps (for standard: dev→verify→stage; for simple: deploy→verify)
- Their specific strategy commands
- Pointers to knowledge for THEIR specific runtime types

Never: generic "here's how standard mode works" without context. Always: "YOUR services are appdev (nodejs@22) → appstage. Step 1: deploy to appdev..."

### 2.6 Workflows for Orchestration, Tools for Operations

ZCP operations follow a two-tier model:

| Tier | When | Examples | Why |
|------|------|----------|-----|
| **Workflow** | Multi-step, needs state/coordination, platform-specific ordering | bootstrap, deploy, cicd | Agents need guidance on step ordering, env var discovery, deploy sequencing, verification loops. Deploy covers investigation/fixing. |
| **Direct tool** | Single operation, self-contained, tool schema is sufficient | zerops_scale, zerops_manage, zerops_env, zerops_subdomain | The tool's schema describes parameters, nextActions guides follow-up, zerops_knowledge provides domain context on demand |

**Workflow is NOT a gate.** An agent does not need to start a workflow to call `zerops_scale` or `zerops_manage`. These tools work independently. Workflows exist to prevent the specific failure modes of complex operations (writing zerops.yml before discovering env vars, deploying before verifying, etc.).

**The test**: if an operation is a single API call with self-guiding nextActions and all domain knowledge is accessible via `zerops_knowledge`, it belongs in the direct tool tier. If it requires multi-tool coordination, state tracking, or non-obvious ordering, it needs a workflow.

---

## 3. Knowledge Taxonomy

### 3.1 Categories

| Category | Examples | Source | Stability |
|----------|----------|--------|-----------|
| **Platform mechanics** | Container lifecycle, env var resolution, build vs run container, deployFiles behavior | Verified against live platform | Stable (changes with Zerops releases) |
| **Mode workflows** | Standard: dev→stage flow. Dev: dev-only. Simple: auto-start. | Defined in ZCP code | Stable (changes with ZCP releases) |
| **Strategy procedures** | push-dev: SSH self-deploy. push-git: git webhook. manual: user-managed. | Defined in ZCP code | Stable |
| **Runtime knowledge** | Node.js zerops.yml patterns, Go build config, PHP implicit webserver | Knowledge store (text search + recipes) | Updated periodically |
| **Recipe patterns** | Next.js, Laravel, Django framework-specific configs | Knowledge store (recipes/) | Updated periodically |
| **Operational data** | Service status, env var values, health check results, logs | Live API (zerops_discover, zerops_verify, zerops_logs) | Dynamic (changes constantly) |

### 3.2 Layered Knowledge Composition

The knowledge base uses a layered architecture where each layer adds specificity. Layers compose at delivery time — updating a lower layer automatically improves everything above it.

```
┌─────────────────────────────────────────────────────┐
│  Layer 4: Recipes (recipes/*.md)                    │  Framework-specific
│  Laravel, Next.js, Django, Phoenix, ...             │  refinements
├─────────────────────────────────────────────────────┤
│  Layer 3: Runtime Guides (recipes/{rt}-hello-world) │  General per-runtime
│  nodejs, php, go, python, elixir, ... + bases/*.md │  knowledge
├─────────────────────────────────────────────────────┤
│  Layer 2: Service Cards (themes/services.md)        │  Managed service
│  PostgreSQL, Valkey, Kafka, Object Storage, ...    │  reference
├─────────────────────────────────────────────────────┤
│  Layer 1: Core Reference (themes/core.md)           │  YAML schemas,
│  import.yml + zerops.yml schemas, deploy semantics  │  platform rules
├─────────────────────────────────────────────────────┤
│  Layer 0: Universals (Platform Constraints H2 from  │  Platform truths
│  themes/model.md) — bind, deployFiles, no .env, zsc│  for ALL services
└─────────────────────────────────────────────────────┘
```

**Design principles:**

1. **General knowledge in runtimes, specific refinements in recipes.** A runtime guide (e.g., `nodejs.md`) contains knowledge valid for ANY Node.js app on Zerops — binding, deploy patterns, common mistakes. A recipe (e.g., `nextjs.md`) adds framework-specific config that builds ON TOP of the runtime knowledge.

2. **Recipes inherit universals.** When `GetRecipe()` delivers a recipe, it automatically prepends the platform constraints (`briefing.go:prependRecipeContext`). Runtime guides are NOT prepended — each recipe is standalone with its own knowledge.

3. **Briefings compose dynamically.** When `GetBriefing()` assembles knowledge for a specific stack, it layers: live stacks → runtime guide → recipe hints → service cards → wiring syntax → decision hints → version check. Each layer is optional — a stack with no managed services skips the service card layer.

4. **Recipes MUST NOT contradict their parent runtime.** A recipe refines — it does not override platform truths or runtime conventions. If a runtime says "bind 0.0.0.0:8000", the recipe's zerops.yml must include that binding. Contradictions between layers indicate a bug.

5. **Runtime guides cover dev AND prod patterns.** Each runtime guide includes both deploy patterns (dev: `deployFiles: [.]` + `zsc noop --silent`, prod: optimized build + compiled output). Mode adaptation is handled by `prependModeAdaptation()` in `briefing.go`, which adds a mode-specific header directing the agent to the right setup block.

**Composition flows (code references):**

| Entry point | Composition | Code |
|-------------|-------------|------|
| `GetRecipe(name, mode)` | universals + recipe | `briefing.go:105-139` |
| `GetBriefing(runtime, services, mode, liveTypes)` | stacks → runtime → recipes → cards → wiring → decisions → versions | `briefing.go:18-101` |
| `GetCore()` | core reference only (platform model + YAML schemas) | `engine.go:197-203` |
| `GetModel()` + `GetCore()` (via scope handler) | platform model + core reference | `tools/knowledge.go:102-125` |

**Recipe-to-runtime mapping** (`runtimeRecipeHints` in `engine.go:215-226`): Maps runtime base names to recipe name prefixes. Used for two purposes: (1) suggesting relevant recipes in briefings, and (2) auto-detecting the parent runtime when loading a recipe via `detectRecipeRuntime`.

### 3.3 What Agent Cannot Infer

These facts are Zerops-specific and MUST be communicated. The agent has no way to derive them from general knowledge:

1. **Deploy creates a new container.** All local files are lost. Only `deployFiles` content persists. This is NOT a restart — it's container replacement.

2. **`${hostname_varName}` typos become silent literal strings.** No error from the API. No deploy failure. The platform provides zero protection against env var reference mistakes.

3. **Build container ≠ run container.** Different environment. Packages installed during build are NOT available at runtime unless included in `deployFiles`.

4. **Dev mode uses `zsc noop --silent`.** Server doesn't start automatically. Agent must start it manually via SSH after every deploy. Exception: implicit-webserver runtimes (php-nginx, php-apache, nginx, static) auto-start.

5. **Stage mode auto-starts.** Real start command + healthCheck. Server monitored by Zerops, auto-restarts on failure.

6. **Subdomain must be explicitly enabled** once per new service after first deploy, even if set in import.yml. Re-deploys do NOT deactivate it. `zerops_subdomain action="enable"` is idempotent. `zerops_discover` shows current status.

7. **`zerops_deploy` blocks until pipeline completes.** Returns DEPLOYED or BUILD_FAILED with build logs. No manual polling needed.

8. **SSHFS mount path is local to zcpx container.** `/var/www/{hostname}/` is the mount path on the ZCP container. Inside the target container, code lives at `/var/www/`. Never use mount path as `workingDir` in zerops_deploy.

---

## 4. Environment Model

### 4.1 Container Mode (zcpx running on Zerops)

```
┌─────────────────────────────────────┐
│  zcpx container (ZCP service)       │
│                                     │
│  SSHFS mounts:                      │
│    /var/www/appdev/  ──────────┐    │
│    /var/www/apidev/  ──────┐   │    │
│                            │   │    │
│  Agent edits code here     │   │    │
│  Changes appear instantly  │   │    │
│  on target containers      │   │    │
└────────────────────────────┼───┼────┘
                             │   │
                    ┌────────┘   └────────┐
                    ▼                     ▼
           ┌──────────────┐     ┌──────────────┐
           │  apidev      │     │  appdev      │
           │  container   │     │  container   │
           │  /var/www/   │     │  /var/www/   │
           └──────────────┘     └──────────────┘
```

**Key facts for agent**:
- Code is accessible via mount paths on the zcpx container
- File changes are immediate (no transfer needed)
- Deploy runs the full build pipeline (not just file sync)
- Deploy when: zerops.yml changes, need clean rebuild, promote dev→stage
- Code-only changes on dev: just restart the server via SSH

### 4.2 Local Mode (ZCP running on developer's machine)

```
┌─────────────────────────────────────┐
│  Developer's machine                │
│                                     │
│  Code in working directory          │
│  zerops.yml at repository root      │
│                                     │
│  Deploy pushes code via zcli push   │
└─────────────────────────────────────┘
           │
           │ zcli push
           ▼
    ┌──────────────┐
    │  Zerops      │
    │  service     │
    │  container   │
    └──────────────┘
```

**Key facts for agent**:
- Code is local, no mounts
- Deploy = `zcli push` (pushes code to Zerops)
- Each deploy = full rebuild + new container
- zerops.yml must be at repository root

### 4.3 Environment Detection

ZCP detects the environment at startup (`internal/runtime/runtime.go`). The detected environment affects:
- How deploy commands work (SSH self-deploy vs zcli push)
- Whether mount paths are available
- What init instructions are relevant

---

## 5. Agent Lifecycle Scenarios

### 5.1 Scenario A: Goal-Oriented ("Create an image upload app")

```
User intent: functional goal, not infrastructure-specific

Agent flow:
1. Agent recognizes infrastructure is needed (from init instructions)
2. Plans required services (app server + storage + maybe DB)
3. Starts bootstrap workflow → ZCP guides infrastructure creation
4. Bootstrap completes → ServiceMeta written → strategy selection prompted
5. Agent sets strategy
6. Agent writes application code (NO ZCP involvement needed)
7. Agent starts deploy workflow → gets personalized guidance
8. Agent deploys → iterates if needed → done
```

**ZCP's role**: Guide infrastructure creation (bootstrap), validate platform integration (deploy checkers), provide personalized workflow steps (deploy guidance). Stay out of the way during code writing.

### 5.2 Scenario B: Infrastructure-Explicit ("Create Laravel + DB + MQ + object storage")

```
User intent: infrastructure specification, app development later

Agent flow:
1. Starts bootstrap with specified services
2. Bootstrap completes → strategy selection
3. Agent sets strategy → "Infrastructure ready."
4. ... time passes ...
5. User returns: "Add feature X"
6. Agent writes code, starts deploy workflow when ready
```

**ZCP's role**: Bootstrap only. Deploy workflow available when agent needs it.

### 5.3 Scenario C: Continuing Work (bootstrapped project, new session)

```
User intent: unknown — could be anything

Agent flow:
1. Agent has init instructions + CLAUDE.md
2. User: "Add Redis caching"
3. Agent checks current state via zerops_discover or zerops_workflow action="route"
4. Decides: need new service → bootstrap. Or: modify existing code → edit + deploy.
5. Acts accordingly
```

**ZCP's role**: Provide factual state when asked. Don't push knowledge preemptively. Agent decides what workflow to use based on what user wants.

### 5.4 Scenario D: Unknown / Edge Cases

```
User intent: doesn't fit A/B/C patterns

Agent flow:
1. Agent does whatever user asks
2. Uses ZCP tools individually when needed (discover, verify, logs, knowledge)
3. Uses workflows when structured flow is needed
4. ZCP adapts — returns relevant data for whatever is called
```

**ZCP's role**: Be a reliable toolkit. Individual tools work independently. Workflows add structure when needed. Nothing breaks if agent doesn't use workflows.

### 5.5 Cross-Scenario Invariants

| Rule | Applies to |
|------|-----------|
| ZCP never tells the agent WHAT to build | All scenarios |
| ZCP never recommends which workflow to start | All scenarios |
| Route returns facts (services, states, metas), not recommendations | All scenarios |
| Individual tools work without active workflow | All scenarios |
| Agent can always refresh state via zerops_discover | All scenarios |
| Workflow guidance is personalized to current setup | A, B, C when using workflows |

---

## 6. Guidance Assembly Model

### 6.1 Structure

Every workflow step response contains guidance assembled from three layers:

```
┌─────────────────────────────────────────────┐
│  Layer 1: MUST-KNOW (injected, compact)     │
│  Platform mechanics + mode/strategy workflow │
│  Personalized to current DeployState        │
│  10-30 lines                                │
├─────────────────────────────────────────────┤
│  Layer 2: CONTEXTUAL (injected, conditional)│
│  First deploy vs redeploy                   │
│  Iteration escalation (iter 1/2/3)          │
│  0-15 lines (only when applicable)          │
├─────────────────────────────────────────────┤
│  Layer 3: KNOWLEDGE MAP (pointers)          │
│  Runtime knowledge: zerops_knowledge query  │
│  Recipes: zerops_knowledge recipe           │
│  Env vars: zerops_discover includeEnvs      │
│  5-10 lines                                 │
└─────────────────────────────────────────────┘
```

**Total guidance per step**: 15-55 lines. Never 200+.

### 6.2 Layer 1: Must-Know (Always Injected)

Assembled from `DeployState` + `ServiceMeta` + `Environment`:

**Setup summary** (personalized):
```
Your services: appdev (nodejs@22, dev) → appstage (stage)
Mode: standard | Strategy: push-dev
```

**Workflow steps** (mode + strategy specific):
```
1. Deploy to dev: zerops_deploy targetService="appdev"
2. Start server manually (dev uses zsc noop)
3. Verify: zerops_verify serviceHostname="appdev"
4. Promote to stage: zerops_deploy sourceService="appdev" targetService="appstage"
5. Stage auto-starts (real start command + healthCheck)
6. Verify stage: zerops_verify serviceHostname="appstage"
```

**Platform facts** (always):
```
- Deploy = new container, local files lost, only deployFiles survives
- ${hostname_varName} typo = silent literal string, no error
- Dev: start server manually after deploy (except php-nginx, nginx, static)
- Stage: auto-starts, Zerops monitors via healthCheck
- zerops_deploy blocks until pipeline complete — returns DEPLOYED or BUILD_FAILED
```

**Strategy note** (brief, non-forcing):
```
Currently: push-dev (SSH self-deploy from dev container)
Other options: push-git (auto-deploy on git push), manual (you manage)
Change: zerops_workflow action="strategy" strategies={"appdev":"push-git"}
```

### 6.3 Layer 2: Contextual (Conditional)

Injected only when specific conditions are met:

| Condition | Content |
|-----------|---------|
| First deploy (service status READY_TO_DEPLOY) | "This is the first deploy. zerops.yml needs creation. Load runtime knowledge first." |
| Redeploy (service RUNNING, zerops.yml exists) | "Service already running. If config unchanged, deploy directly." |
| Iteration 1 (first failure) | "Check zerops_logs severity=error. Build failed? → build log. Container crash? → runtime log, start command, env vars." |
| Iteration 2 | "Systematic check: zerops.yml (ports, start, deployFiles), env var references, runtime version." |
| Iteration 3+ | "Present diagnostic summary to user: exact error, current config, env var values. User decides." |

### 6.4 Layer 3: Knowledge Map (Pointers)

Assembled from ServiceMeta runtime types and dependency types:

```
### Knowledge on demand
- appdev (nodejs@22): zerops_knowledge query="nodejs"
- Recipes: zerops_knowledge recipe="nextjs" (if framework known)
- db (postgresql@16): env vars via zerops_discover includeEnvs=true
- zerops.yml help: zerops_knowledge query="zerops.yml schema"
```

The agent decides whether to call these. ZCP never injects the knowledge content itself.

**Routing-aware on-demand knowledge**: When the agent calls `zerops_knowledge` during an active workflow session, the tool auto-detects the session mode (standard/dev/simple) and filters knowledge accordingly:
- **Runtime guides**: Deploy Patterns filtered to Dev (standard/dev mode), both (simple mode), or Prod (stage override)
- **Recipes**: Mode adaptation header prepended — runtime-aware (PHP: "omit start", others: "use zsc noop")
- **No session**: Full unfiltered content (agent exploring, needs complete reference)

The agent can override with explicit `mode` parameter (e.g., `mode="stage"` to see prod patterns during dev workflow). Auto-routing covers 95% of cases; explicit override for the rest.

---

## 7. Init Instructions Role

### 7.1 MCP Plugin Model

**ZCP operates as a clean MCP plugin.** All agent instructions are delivered via MCP init instructions (`BuildInstructions` in `server/instructions.go`). ZCP does NOT generate or modify the user's `CLAUDE.md` for instruction delivery.

| File | ZCP's relationship | Rule |
|------|-------------------|------|
| `CLAUDE.md` | **Reflog only** — append-only bootstrap history entries (ZEROPS:REFLOG markers). Never instructions. | `zcp init` generates a minimal CLAUDE.md. Reflog entries appended after bootstrap completion. |
| MCP init instructions | **All agent instructions** — environment model, persistence rules, tool overview, workflow routing, service classification | Source of truth for how the agent should behave with ZCP |
| Workflow responses | **Step-specific guidance** — personalized deploy/bootstrap instructions | Returned per workflow action call |

**Rationale**: MCP init instructions flow into the agent's context identically to CLAUDE.md. Keeping instructions in MCP init means ZCP is a self-contained plugin — users can add/remove ZCP without it polluting their project configuration. CLAUDE.md remains the user's space; ZCP only appends historical records (reflog) that help future sessions understand what was bootstrapped.

### 7.2 What Init Instructions Cover

Init instructions are loaded once at agent session start. They provide the foundational mental model:

1. **Environment concept**: Container vs local, where code lives, how mounts work
2. **Persistence model**: What survives deploy, what doesn't, when deploy is required
3. **Available tools**: What ZCP tools exist and when to use each
4. **State refresh**: "zerops_discover always returns current state"
5. **Workflow entry points**: "When you need to deploy → zerops_workflow action='start' workflow='deploy'"
6. **Knowledge access**: "When you need Zerops-specific knowledge → zerops_knowledge query='...'"

### 7.3 What Init Instructions Do NOT Cover

- What the user should build
- Which workflow to start
- Runtime-specific knowledge (that's in the knowledge store)
- Detailed workflow step guidance (that's in workflow responses)
- Current service state (that's from zerops_discover)

### 7.4 Container Mode Init Instructions (conceptual template)

```
## Your Role

Orchestrator. This container is the control plane — it does NOT serve user traffic.
Your job is to create, configure, deploy, and manage OTHER services in the project.

### Code Access
Runtime services are SSHFS-mounted:
  /var/www/{hostname}/ — edit code here, changes appear on the service container
Mount is read/write, changes immediate. No file transfer needed.
IMPORTANT: /var/www/ (no hostname) is THIS container — writing there has NO effect on any service.

### Persistence Model
File edits via SSH or SSHFS mount are TEMPORARY:
- Edits SURVIVE: container restarts, reloads, stop/start, vertical scaling
- Edits DESTROYED: next deploy (creates new container)
After completing code changes, you MUST deploy to persist them.
Start a deploy workflow: zerops_workflow action="start" workflow="deploy"

### Deploy = Rebuild
Editing files does NOT trigger deploy. Deploy runs the full pipeline
(build → deployFiles → start) and creates a NEW container.
Deploy when: zerops.yml changes, need clean rebuild, or promote dev → stage.
Code-only changes on dev: just restart the server via SSH.

### Commands on Services
Edit source files on the SSHFS mount. Run heavy commands via SSH on the target:
ssh {hostname} "cd /var/www && {command}"
Running installs over the SSHFS network mount is orders of magnitude slower.

### Tools
- zerops_discover — current state of all services (call anytime for refresh)
- zerops_workflow — orchestrated flows (bootstrap, deploy, cicd)
- zerops_scale — scale a service directly (no workflow needed)
- zerops_manage — lifecycle operations: start, stop, restart, reload, connect/disconnect storage
- zerops_knowledge — Zerops-specific knowledge (runtimes, recipes, schemas)
- zerops_verify — health checks
- zerops_logs — runtime and build logs
- zerops_deploy — deploy code to a service

### When to Use What
- Creating new infrastructure → zerops_workflow action="start" workflow="bootstrap"
- Deploying, fixing, or investigating → zerops_workflow action="start" workflow="deploy"
- Setting up CI/CD → zerops_workflow action="start" workflow="cicd"
- Env vars → zerops_env action="set|delete" (reload after: zerops_manage action="reload")
- Scaling a service → zerops_scale (simple) or zerops_knowledge query="scaling" (need guidance)
- Restarting/reloading → zerops_manage action="restart" serviceHostname="..."
- Need Zerops knowledge → zerops_knowledge query="..."
- Need current state → zerops_discover
```

### 7.5 Local Mode Init Instructions (conceptual template)

```
## Your Environment

You're running locally. Code is in the working directory.

### Deploy
Deploy pushes code from local to Zerops via zcli push.
zerops.yml must be at repository root.
Each deploy = full rebuild + new container.

### Tools
[same as container mode, minus mount/SSH-specific details]
```

---

## 8. Workflow Guidance Specifications

### 8.1 Bootstrap Guidance

Bootstrap is a CREATIVE workflow — the agent is building from scratch. Knowledge injection is appropriate because the agent needs it to create correct configuration.

Bootstrap guidance model (unchanged from current):
- **Discover**: Stack catalog, mode explanations, plan validation rules
- **Provision**: import.yml schema, service creation procedure
- **Generate**: Runtime briefing, dependency wiring, env var names, zerops.yml schema
- **Deploy**: Mode-specific deploy procedure, operational details
- **Close**: Strategy selection prompt

This is correct because bootstrap generates NEW configuration. The agent genuinely needs schema knowledge to write zerops.yml for the first time.

### 8.2 Deploy Guidance

Deploy is an OPERATIONAL workflow — services already exist, config may already exist. Knowledge is available on demand, not injected.

Deploy guidance model (redesigned):
- **Prepare**: Setup summary + platform facts + knowledge pointers (Layer 1 + Layer 3)
- **Deploy**: Personalized workflow steps + contextual info + diagnostics (Layer 1 + Layer 2)
- **Verify**: Diagnostic patterns (existing deploy.md verify section)

See Section 6 for detailed layer definitions.

### 8.3 Immediate Workflows

CI/CD is the only remaining immediate workflow — returns guidance text directly. No session state, no checkers. Debug and configure were consolidated into the deploy workflow (v6.33.0). Scale, env, subdomain, and manage are direct tools.

---

## 9. Transitions Between Workflows

### 9.1 Bootstrap → Strategy → Deploy

```
Bootstrap complete
  → output includes: "Services ready. Choose deploy strategy for each service."
  → provides strategy selection guidance (all 3 options equally presented)
  → provides command: zerops_workflow action="strategy" strategies={...}

Strategy set
  → output includes: "Strategies configured. When code is ready to deploy:"
  → provides command: zerops_workflow action="start" workflow="deploy"

Agent writes code (NO ZCP involvement)

Agent starts deploy
  → deploy workflow returns personalized guidance
```

### 9.2 Deploy → Iteration → Deploy

```
Deploy step fails (checker or agent attestation)
  → agent calls: zerops_workflow action="iterate"
  → prepare step preserved, deploy+verify reset
  → iteration counter incremented
  → agent gets escalating diagnostic guidance (Layer 2 contextual)
```

### 9.3 New Session → State Discovery

```
Agent starts new session on existing project
  → init instructions tell agent about available tools
  → agent calls zerops_discover or zerops_workflow action="route" to understand current state
  → route returns FACTS: services, statuses, metas, active sessions
  → agent decides what to do based on user request + discovered state
```

### 9.4 Transition Invariants

| Rule | Enforced by |
|------|-------------|
| Bootstrap outputs always include strategy selection prompt | `BuildTransitionMessage()` |
| Strategy outputs always include deploy entry command | `handleStrategy()` response |
| Route returns data, never recommendations | `Route()` returns facts only |
| Deploy workflow requires strategy set (conversational if not) | `handleDeployStart()` strategy gate |
| Agent can always refresh state via zerops_discover | Tool always available |

---

## 10. State & Refresh Model

### 10.1 State Sources

| Source | What it provides | When to use |
|--------|-----------------|-------------|
| ServiceMeta files | Bootstrap decisions (mode, strategy, stage pairing) | Read by deploy workflow on start |
| Session state | Current workflow progress, iteration count | Read by workflow engine on every action |
| Live API (zerops_discover) | Current service status, env vars, resources | Agent calls when it needs fresh data |
| Live API (zerops_verify) | Health check results | Agent calls to check service health |
| Live API (zerops_logs) | Runtime and build logs | Agent calls to diagnose issues |

### 10.2 State Staleness

During a session, state can become stale:
- Agent deploys → service status changes (READY_TO_DEPLOY → ACTIVE)
- Agent scales → resources change
- External events → services may change independently

**Refresh mechanism**: `zerops_discover` always returns current state. Agent should call it when:
- Before starting a deploy workflow (to confirm current service state)
- After a deploy (to verify new state)
- When diagnosis needs current information
- Whenever it's uncertain about current state

Init instructions should communicate: "zerops_discover always returns the CURRENT state. Call it whenever you need to refresh your understanding."

---

## 11. Guidance Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| G1 | Deploy guidance is personalized to current setup (hostnames, mode, strategy, runtime) | Guidance assembly reads DeployState + ServiceMeta |
| G2 | Platform mechanics always injected (container lifecycle, env var behavior) | Layer 1 always included |
| G3 | Runtime knowledge never injected in deploy — always pointed to | Layer 3 uses pointers only |
| G4 | Strategy alternatives mentioned briefly, never forced | Layer 1 includes 2-line strategy note |
| G5 | Route returns facts, never recommendations | Route handler returns structured data |
| G6 | Init instructions explain environment concept, not workflow details | Init instructions scope |
| G7 | Bootstrap MAY inject full knowledge (creative workflow) | assembleKnowledge for bootstrap steps |
| G8 | Deploy MUST NOT inject full knowledge (operational workflow) | assembleKnowledge redesigned for deploy |
| G9 | Guidance total per step: 15-55 lines, never 200+ | Guidance assembly limits |
| G10 | Agent can always pull knowledge on demand via zerops_knowledge | Tool always available |
| G11 | Contextual guidance (first deploy, iteration) injected only when condition met | Layer 2 conditional checks |
| G12 | State refresh available via zerops_discover at any time | Tool always available |
| G13 | On-demand knowledge (zerops_knowledge) is session-aware — auto-filters by mode when active session exists, unfiltered otherwise | `resolveKnowledgeMode()` in knowledge.go reads Engine state |
| G14 | Agent can override auto-detected mode with explicit `mode` parameter on zerops_knowledge | `input.Mode` takes priority over session mode |
