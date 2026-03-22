# ZCP Bootstrap & Deploy Workflow Specification

> **Status**: Authoritative — all code, content, and improvements MUST conform to this document.
> **Scope**: Container mode only. Local mode shares concepts but has its own specifics (not covered here).
> **Environment**: `Engine.Environment()` returns `container` or `local` — currently only `container` path is implemented. Local mode is deferred.
> **Date**: 2026-03-22

---

## 1. Glossary

| Term | Definition |
|------|-----------|
| **Session** | Ephemeral workflow state persisted at `.zcp/state/sessions/{id}.json`. Created on workflow start, deleted on completion. |
| **ServiceMeta** | Persistent per-service evidence file at `.zcp/state/services/{hostname}.json`. Survives session cleanup. Records bootstrap decisions for future workflows. |
| **Mode** | One of `standard`, `dev`, `simple`. Determines service topology, zerops.yml shape, deploy behavior, and iteration model. Chosen during discover step. |
| **Step** | Atomic unit of workflow progress. Has status (pending → in_progress → complete/skipped), attestation (completion proof, min 10 chars), and optional checker. |
| **Attestation** | Agent's self-report of what was accomplished in a step. Minimum 10 characters. Stored in session state. |
| **Iteration** | Reset of generate+deploy steps with incremented counter. Preserves discovery/provision/close context. Max 10 (configurable via `ZCP_MAX_ITERATIONS`). |
| **Managed-only** | Project with zero runtime services — only databases, caches, storage. Skips generate/deploy/close steps. |
| **Immediate workflow** | Stateless workflow (debug, scale, configure) — returns guidance without creating a session. |
| **Runtime class** | Verification classification: Dynamic (nodejs, go, bun...), Implicit (php-nginx, php-apache), Static (nginx, static), Worker (no ports), Managed (postgresql, valkey...). |
| **Strategy** | Deployment method per service: `push-dev`, `ci-cd`, or `manual`. Always explicit — never auto-assigned. Set via `action=strategy` after bootstrap. Required before deploy workflow. |

---

## 2. System Model

```mermaid
graph TB
    Agent["AI Agent"] -->|"zerops_workflow action=..."| Tool["MCP Tool Handler<br/>(tools/workflow.go)"]
    Tool -->|"start/complete/skip/iterate/resume/status"| Engine["Workflow Engine<br/>(workflow/engine.go)"]
    Engine -->|"read/write"| Session["Session State<br/>(.zcp/state/sessions/{id}.json)"]
    Engine -->|"write on completion"| Meta["Service Metas<br/>(.zcp/state/services/{hostname}.json)"]
    Engine -->|"exclusive lock"| Registry["Session Registry<br/>(.zcp/state/registry.json)"]

    Engine -->|"assemble guidance"| Guidance["Guidance Assembly<br/>(bootstrap_guidance.go)"]
    Guidance -->|"extract sections"| Content["Embedded Content<br/>(bootstrap.md / deploy.md)"]
    Guidance -->|"inject knowledge"| Knowledge["Knowledge Store<br/>(BM25 + recipes + schemas)"]

    Agent -->|"operational tools"| OpTools["zerops_discover<br/>zerops_import<br/>zerops_mount<br/>zerops_deploy<br/>zerops_verify<br/>zerops_subdomain"]

    Meta -.->|"read by future sessions"| Engine

    style Session fill:#ffd,stroke:#aa0
    style Meta fill:#dfd,stroke:#0a0
    style Registry fill:#fdd,stroke:#a00
```

The workflow system is a **step-based state machine** that guides an AI agent through infrastructure setup. The engine:
- Manages session lifecycle (create, progress, iterate, resume, complete)
- Injects context-aware guidance at each step (mode-specific, iteration-aware, knowledge-enriched)
- Validates progress through attestations and optional checkers
- Persists decisions as ServiceMeta files that inform future workflows

---

## 3. Bootstrap Flow

### 3.1 Lifecycle Overview

**Entry**: `zerops_workflow action="start" workflow="bootstrap"`
- Creates exclusive session (only one bootstrap at a time, enforced via registry lock)
- Sets first step (discover) to `in_progress`
- Returns available stack catalog for type validation

**Progression**: 5 steps, strictly linear. Each step transitions:
`pending → in_progress → complete | skipped`

**Exit**: All 5 steps complete/skipped → session file deleted, ServiceMeta files written, reflog appended to CLAUDE.md.

**Exclusivity**: Only one bootstrap session can be active per project. Enforced by `InitSessionAtomic()` with file-based lock on `.zcp/state/.registry.lock`.

```mermaid
flowchart TD
    Start([Agent triggers bootstrap]) --> CreateSession["Create session<br/>Generate 16-hex ID<br/>Register in registry"]
    CreateSession --> Discover

    subgraph Bootstrap ["Bootstrap Flow (5 steps)"]
        direction TB
        Discover["1. DISCOVER<br/>─────────────<br/>Detect project state<br/>Identify services<br/>Choose mode<br/>Submit plan"]

        Provision["2. PROVISION<br/>─────────────<br/>Generate import.yml<br/>Create services<br/>Mount dev filesystems<br/>Discover env vars"]

        Generate["3. GENERATE<br/>─────────────<br/>Write zerops.yml<br/>Write app code<br/>Mode-specific rules"]

        Deploy["4. DEPLOY<br/>─────────────<br/>Deploy to services<br/>Start servers (SSH)<br/>Enable subdomains<br/>Full health verification"]

        Close["5. CLOSE<br/>─────────────<br/>Write ServiceMetas<br/>Append reflog<br/>Present strategy selection"]
    end

    Discover -->|"plan submitted"| Provision
    Provision -->|"services created,<br/>env vars discovered"| ModeCheck{Has runtime<br/>services?}

    ModeCheck -->|Yes| Generate
    ModeCheck -->|"No (managed-only)"| SkipGen["SKIP generate"]
    SkipGen --> SkipDeploy["SKIP deploy"]
    SkipDeploy --> SkipClose["SKIP close"]

    Generate -->|"code written"| Deploy
    Deploy -->|"all services healthy"| Close

    Deploy -->|"verification failed"| IterCheck{Iteration<br/>< max?}
    IterCheck -->|Yes| Iterate["ITERATE<br/>Reset steps 2-3<br/>Increment counter"]
    Iterate --> Generate
    IterCheck -->|"No (max reached)"| FailReport["Report failure<br/>to user"]
    FailReport --> Close

    SkipClose --> Complete
    Close --> Complete

    Complete([Bootstrap Complete]) --> Cleanup["Delete session file<br/>Write ServiceMetas<br/>Append reflog<br/>Present strategy selection"]

    style Discover fill:#e8f4fd,stroke:#2196F3
    style Provision fill:#e8f4fd,stroke:#2196F3
    style Generate fill:#fff3e0,stroke:#FF9800
    style Deploy fill:#fce4ec,stroke:#E91E63
    style Close fill:#e8f4fd,stroke:#2196F3
    style SkipGen fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SkipDeploy fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SkipClose fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
```

### 3.2 Step Specifications

---

#### Step 1: DISCOVER (fixed, mandatory)

**Purpose**: Detect project state, identify required services, choose bootstrap mode, submit structured plan.

**Inputs**:
- Project ID (from auth)
- Live API access (zerops_discover)
- User intent (what they want to build)

**Outputs**:
- Project state classification (FRESH / CONFORMANT / NON_CONFORMANT)
- ServicePlan with validated targets
- Chosen mode (standard / dev / simple)

**Procedure**:

1. **Detect project state** — call `zerops_discover` to see existing services:

   | Result | State | Action |
   |--------|-------|--------|
   | No runtime services | FRESH | Full bootstrap |
   | Requested services exist as dev+stage pairs with matching stack | CONFORMANT | Route to deploy workflow (skip bootstrap) |
   | Services exist but don't match expected pattern | NON_CONFORMANT | STOP. Present to user. NEVER auto-delete. |

2. **Identify stack components** from user request:
   - **Runtime services**: type + framework (e.g., `nodejs@22` with Next.js)
   - **Managed services**: type + version (e.g., `postgresql@16`, `valkey@7.2`)
   - If unspecified, ASK. Don't guess frameworks.

3. **Validate all types** against `availableStacks` field in the workflow response.

4. **Choose bootstrap mode** — present to user, get confirmation:
   - **Standard** (default): `{name}dev` + `{name}stage` + shared managed. Dev for iteration, stage for validation.
   - **Dev**: `{name}dev` + managed only. No stage. For prototyping.
   - **Simple**: `{name}` + managed. Real start command, auto-starts. Only if user explicitly requests.

5. **Load recipe** (optional): `zerops_knowledge recipe="{name}"` for framework-specific patterns.

6. **Present plan to user**: "I'll set up: [services]. Mode: [mode]. OK?"

7. **Submit plan** after confirmation:
   ```
   zerops_workflow action="complete" step="discover" plan=[{
     runtime: {devHostname, type, bootstrapMode},
     dependencies: [{hostname, type, mode, resolution}]
   }]
   ```

**Plan validation** (server-side, automatic):
- Hostnames: `[a-z0-9]` only, max 25 chars
- Types: matched against live platform catalog
- Mode: `""` | `"standard"` | `"dev"` | `"simple"` (empty defaults to standard)
- Standard mode: devHostname must end in `"dev"`, stage derived as `{prefix}stage`
- Dependencies: resolution must be `CREATE` | `EXISTS` | `SHARED`
  - CREATE: service must NOT exist
  - EXISTS: service MUST exist
  - SHARED: another target must CREATE this hostname
- Managed mode: auto-defaults to `NON_HA` if omitted
- Empty targets allowed (managed-only projects)

**Mode-specific behavior**:

| Aspect | Standard | Dev | Simple |
|--------|----------|-----|--------|
| Hostnames | `{name}dev` + `{name}stage` | `{name}dev` | `{name}` |
| Plan targets | devHostname + stage derived | devHostname only | hostname only |

**Managed-only behavior**:
- Plan with zero runtime targets (empty `targets` array)
- Route: discover → provision → SKIP generate → SKIP deploy → SKIP close

**Invariants**:
- Plan validated against live API types before storage
- User must confirm plan before submission
- CONFORMANT projects skip bootstrap entirely → deploy workflow
- NON_CONFORMANT projects require explicit user decision

---

#### Step 2: PROVISION (fixed, mandatory)

**Purpose**: Create infrastructure on Zerops, mount dev filesystems, discover environment variables.

**Inputs**:
- ServicePlan from discover step
- Live API access

**Outputs**:
- All services created on Zerops platform
- Dev filesystems mounted via SSHFS at `/var/www/{hostname}/`
- Env var names discovered and stored in session (`BootstrapState.DiscoveredEnvVars`)

**Procedure**:

1. **Generate import.yml** based on plan:

   | Property | Dev service | Stage service | Simple service | Managed |
   |----------|------------|---------------|----------------|---------|
   | `startWithoutCode` | `true` | omit | `true` | N/A |
   | `maxContainers` | `1` | omit (default) | omit | N/A |
   | `enableSubdomainAccess` | `true` | `true` | `true` | N/A |
   | `mount` (shared-storage) | `[{storage}]` | `[{storage}]` | `[{storage}]` | N/A |

2. **Present import.yml** to user for review.

3. **Import services**: `zerops_import content="<import.yml>"` — blocks until all processes complete. Returns per-service status (FINISHED/FAILED).

4. **Verify services**: `zerops_discover` — confirm all services exist in expected states (RUNNING for dev, READY_TO_DEPLOY for stage).

5. **Mount dev filesystems**: `zerops_mount action="mount" serviceHostname="{devHostname}"` for each runtime dev service. NOT stage. NOT managed. Mount path: `/var/www/{hostname}/`.

6. **Discover env vars**: `zerops_discover includeEnvs=true` — single call returns ALL services with actual env var values. **Store NAMES ONLY** in session state:
   ```
   BootstrapState.DiscoveredEnvVars = map[hostname][]varNames
   ```

**Mode-specific behavior**:

| Aspect | Standard | Dev | Simple | Managed-only |
|--------|----------|-----|--------|-------------|
| Services created | dev + stage + managed | dev + managed | service + managed | managed only |
| Stage exists | Yes (READY_TO_DEPLOY) | No | No | No |
| Mounts | dev only | dev only | service | None |
| Env var discovery | All managed services | All managed | All managed | All managed |

**Checker**: Validates all plan services exist in API with correct types, status RUNNING/ACTIVE, and managed env vars discovered.

**Key rules**:
- `mount:` in import.yml only applies to ACTIVE services. Stage is READY_TO_DEPLOY → mount silently ignored. After first stage deploy, connect via `zerops_manage action="connect-storage"`.
- Two kinds of "mount": (1) `zerops_mount` = SSHFS dev tool, (2) shared-storage mount = platform `mount:` in import.yml + zerops.yml.

**Invariants**:
- All plan services exist in API after import
- Dev services mounted and writable
- Env var names (never values) stored in session state

---

#### Step 3: GENERATE (creative, skippable)

**Purpose**: Write zerops.yml and application code to mounted filesystem.

**Skip condition**: No runtime services in plan (managed-only).

**Inputs**:
- Mounted filesystem at `/var/www/{hostname}/`
- Discovered env var names from provision step
- Runtime knowledge (injected automatically)
- Recipe knowledge (if loaded in discover)

**Outputs**:
- `zerops.yml` with correct setup entry
- Application code with required endpoints (`/`, `/health`, `/status`)

**Procedure**:

1. **Write all files to SSHFS mount path** `/var/www/{hostname}/` (NOT `/var/www/`).

2. **Write zerops.yml** per mode (see mode-specific rules below).

3. **Write application code**:
   - HTTP server on port from zerops.yml `run.ports`
   - Read env vars via runtime's native API (NOT `.env` files)
   - Bind to `0.0.0.0`, NOT localhost

4. **Required endpoints**:

   | Endpoint | Response | Purpose |
   |----------|----------|---------|
   | `GET /` | `"Service: {hostname}"` | Smoke test |
   | `GET /health` | `{"status":"ok"}` (200) | Liveness probe |
   | `GET /status` | Connectivity JSON (200) | Proves managed service connections |

   `/status` MUST actually connect to each dependency and report:
   ```json
   {
     "service": "{hostname}",
     "status": "ok",
     "connections": {
       "db": {"status": "ok", "latency_ms": 5},
       "cache": {"status": "ok", "latency_ms": 1}
     }
   }
   ```

5. **Map env vars** in zerops.yml using ONLY discovered names:
   ```yaml
   envVariables:
     DATABASE_URL: ${db_connectionString}
     REDIS_HOST: ${cache_host}
   ```

6. **Run pre-deploy checklist** (mode-specific, see below).

**Checker**: Validates zerops.yml exists with correct setup entries, env var refs match discovered vars, ports defined.

**Mode-specific zerops.yml rules**:

| Property | Standard (dev entry) | Dev | Simple |
|----------|---------------------|-----|--------|
| `deployFiles` | `[.]` ALWAYS | `[.]` ALWAYS | `[.]` ALWAYS |
| `start` | `zsc noop --silent` | `zsc noop --silent` | Real command (`node index.js`, etc.) |
| `healthCheck` | None | None | Required (`httpGet` on app port) |
| `buildCommands` | Deps install only | Deps install only | Deps + compile if needed |
| PHP runtimes | Omit `start:` entirely | Omit `start:` entirely | Omit `start:` entirely |

**Pre-deploy checklist** (all modes):
- [ ] `setup:` hostname matches plan
- [ ] `deployFiles: [.]` — NO EXCEPTIONS for self-deploying services
- [ ] `start:` correct for mode (noop for standard/dev, real for simple; omit for PHP)
- [ ] `run.ports` matches app listen port (omit for PHP)
- [ ] `envVariables` uses ONLY discovered var names
- [ ] App binds to `0.0.0.0:{port}`
- [ ] Simple mode: `healthCheck` present
- [ ] Standard mode: NO stage entry yet (comes after dev verification)

**Invariants**:
- zerops.yml references only discovered env vars
- `deployFiles: [.]` for all self-deploying services
- No `.env` files created (Zerops injects env vars as OS vars)

---

#### Step 4: DEPLOY (branching, skippable)

**Purpose**: Deploy code to runtime services, start servers, enable subdomains, verify health.

**Skip condition**: No runtime services in plan (managed-only).

**Inputs**:
- zerops.yml and code on mounted filesystem
- Discovered env vars in session

**Outputs**:
- All runtime services deployed AND verified healthy
- Subdomains enabled with URLs

**Core principle**: Deploy first — env vars activate at deploy time. Dev is for iterating and fixing. Stage (standard mode) is for final validation.

**Checker**: Runs `ops.VerifyAll()` — full health checks (HTTP, logs, startup detection) filtered to plan targets. Also validates subdomain access for services with ports. Returns per-service breakdown.

**Procedure by mode**:

##### Standard mode

```
Phase 1: Deploy Dev
  1. zerops_deploy targetService={devHostname}
  2. Start server via SSH (Bash run_in_background=true)
  3. zerops_subdomain action="enable" serviceHostname={devHostname}
  4. zerops_verify serviceHostname={devHostname}

Phase 2: Deploy Stage (after dev healthy)
  5. Generate stage entry in zerops.yml (real start, healthCheck)
  6. zerops_deploy sourceService={dev} targetService={stage}
  7. zerops_manage action="connect-storage" (if shared-storage)
  8. zerops_subdomain action="enable" serviceHostname={stage}
  9. zerops_verify serviceHostname={stage}

Phase 3: Cross-Verify
  10. zerops_verify (batch) — all services

Complete:
  action="complete" step="deploy" attestation="All services deployed and verified"
```

##### Dev mode

```
Phase 1: Deploy Dev
  1. zerops_deploy targetService={devHostname}
  2. Start server via SSH
  3. zerops_subdomain action="enable"
  4. zerops_verify serviceHostname={devHostname}

Phase 2: Cross-Verify
  5. zerops_verify (batch)

Complete:
  action="complete" step="deploy"
```

##### Simple mode

```
Phase 1: Deploy
  1. zerops_deploy targetService={hostname} (server auto-starts)
  2. zerops_subdomain action="enable"
  3. zerops_verify serviceHostname={hostname}

Phase 2: Cross-Verify
  4. zerops_verify (batch)

Complete:
  action="complete" step="deploy"
```

**Operational details**:

| Operation | Behavior |
|-----------|----------|
| `zerops_deploy targetService=X` | SSH self-deploy: auto-infers sourceService, forces includeGit=true, SSHes into container, runs `git init` + `zcli push`. **Blocks** until build completes. Returns DEPLOYED or BUILD_FAILED with build logs. |
| `zerops_deploy sourceService=X targetService=Y` | Cross-deploy: pushes from source container to target. Target runs its own build pipeline. Stage has real `start:` → server auto-starts. |
| `zerops_subdomain action="enable"` | **MUST be called after every deploy** even if `enableSubdomainAccess` was in import. Returns `subdomainUrls` — the ONLY source for URLs. Idempotent. |
| `zerops_verify` | 6 checks for dynamic runtime, fewer for static/implicit/managed. Returns healthy/degraded/unhealthy with `checks` array. |
| SSH server start | `Bash run_in_background=true`. Kill previous first. NOT needed for PHP/nginx/static (implicit-webserver auto-starts). NOT needed for simple mode (real start command auto-starts). |

**Dev iteration cycle** (standard + dev modes):
1. Edit code on mount path → changes appear instantly in container
2. Kill previous server, start new one via SSH
3. Check startup via TaskOutput
4. Test: `ssh {dev} "curl -s localhost:{port}/health"` | jq .
5. Redeploy ONLY if zerops.yml changed. Code-only changes → server restart only.

**Stage deploy rules** (standard mode only):
- Stage entry written AFTER dev passes verification
- `start:` = real production command (NOT `zsc noop`)
- `healthCheck` required — server auto-starts and auto-restarts
- `deployFiles` = build output (NOT `[.]`) — stage receives compiled artifacts
- `envVariables` copied from dev (already proven via /status)
- Connect shared-storage after first stage deploy via `zerops_manage`

**Multi-service orchestration** (2+ runtime services):
- Parent agent imports all, mounts all, discovers all env vars
- Spawns Service Bootstrap Agent per runtime service pair (in parallel)
- Each subagent gets: mount path, env vars, runtime knowledge, service bootstrap prompt
- Parent runs final verification after all subagents complete

**Invariants**:
- Dev deployed and verified BEFORE stage
- Server started via SSH after every dev deploy (container restarts kill server)
- Subdomain enabled after every deploy (even if set in import)
- Simple mode: no SSH start needed (real start command + healthCheck auto-starts)
- Implicit-webserver runtimes: no SSH start needed (auto-starts)
- Checker runs VerifyAll (HTTP + logs + startup) — not just API status

---

#### Step 5: CLOSE (fixed, skippable)

**Purpose**: Administrative closure of bootstrap. Pure trigger point — completion writes ServiceMetas and presents strategy selection.

**Skip condition**: No runtime services in plan (managed-only).

**Inputs**: Completed deploy step (all services healthy)

**Outputs**:
- ServiceMeta files written (with `BootstrappedAt` timestamp)
- Reflog entry appended to CLAUDE.md
- Strategy selection prompt presented to user

**Checker**: Nil. Close doesn't validate infrastructure — it's the administrative trigger point.

**Mechanism**: When close step completes, `Active` becomes `false`, which triggers `writeBootstrapOutputs()` in `engine.go`. This:
1. Writes final ServiceMeta files for each runtime target (strategy stays empty — no auto-assign)
2. Appends reflog entry to CLAUDE.md

**Transition message**: The close step response includes `BuildTransitionMessage()` which:
- Lists all bootstrapped services with modes
- Presents strategy selection with equal explanation of all 3 options
- Provides `action=strategy` command hint

**Strategy is NEVER auto-assigned.** All modes require explicit `action=strategy` after bootstrap before deploying.

**Iteration note**: Close is excluded from iteration resets. `ResetForIteration()` resets steps 2-3 (generate + deploy) only. Close at index 4 is not retried.

---

### 3.3 Managed-Only Fast Path

For projects with only managed services (databases, caches, storage) and no runtime:

```mermaid
flowchart LR
    D["1. DISCOVER<br/>Plan with empty targets"] --> P["2. PROVISION<br/>Import managed services<br/>Discover env vars"]
    P --> SG["3. SKIP generate"]
    SG --> SD["4. SKIP deploy"]
    SD --> SC["5. SKIP close"]
    SC --> Done["Complete"]

    style SG fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SD fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SC fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
```

**Key differences**:
- Plan submitted with empty `targets` array
- No SSHFS mounts (no runtime filesystem to mount)
- Env vars still discovered (available for future runtime additions)
- No ServiceMeta files written (managed services are API-authoritative)

---

## 4. Deploy Flow

### 4.1 Relationship to Bootstrap

Deploy is a **derived subset** of bootstrap. It operates on services that already exist (created by bootstrap) and uses ServiceMeta files for context.

```mermaid
flowchart LR
    subgraph Bootstrap ["Bootstrap (5 steps)"]
        B1["discover"] --> B2["provision"] --> B3["generate"] --> B4["deploy"] --> B5["close"]
    end

    subgraph Deploy ["Deploy (3 steps)"]
        D1["prepare"] --> D2["deploy"] --> D3["verify"]
    end

    B5 -->|"ServiceMeta files<br/>persist on disk"| StratCheck

    StratCheck{"Strategy set?"}
    StratCheck -->|Yes| D1
    StratCheck -->|"No"| StratSelect["Strategy selection<br/>(conversational)"]
    StratSelect -->|"action=strategy"| StratCheck

    B3 -.->|"equivalent to"| D1
    B4 -.->|"equivalent to"| D2

    style Bootstrap fill:#e3f2fd
    style Deploy fill:#e8f5e9
```

| Aspect | Bootstrap Deploy (step 4) | Deploy Workflow |
|--------|--------------------------|-----------------|
| Context source | Plan + session state | ServiceMeta files from prior bootstrap |
| Session | Part of bootstrap session | Own separate session |
| Prerequisites | Provision done, filesystems mounted, code written | Services exist, zerops.yml exists, **strategy set** |
| Mode detection | From plan targets | From ServiceMeta.Mode field |
| Deploy mechanism | Same (SSH) | Same (SSH) |
| Iteration | Resets bootstrap steps 2-3 | Resets deploy steps 1-2 |
| Strategy required | No (set after bootstrap) | **Yes** — deploy won't start without strategy |

### 4.2 Strategy Gate in Deploy Workflow

When `action="start" workflow="deploy"` is called, `handleDeployStart()`:

1. Reads ServiceMeta files
2. Filters to runtime services (those with Mode or StageHostname)
3. Checks each runtime meta for `DeployStrategy`
4. **If any service has empty strategy** → returns conversational strategy selection guidance (NOT an error)
5. **If all strategies set** → creates deploy session and proceeds

The strategy selection response explains all 3 options equally:

| Strategy | How it works | Good for | Trade-off |
|----------|-------------|----------|-----------|
| `push-dev` | SSH push from dev container, you trigger each deploy | Prototyping, quick iterations | Manual process |
| `ci-cd` | Automatic deploys on git push via webhook | Team dev, production workflows | Requires pipeline setup |
| `manual` | You manage deployments yourself | Existing CI/CD, custom tooling | ZCP won't guide deploys |

### 4.3 CI/CD Strategy Gate

`handleCICDStart()` is a hard gate — only services with `ci-cd` strategy are included. If no services have `ci-cd`, the workflow returns an error asking the user to set strategy first.

### 4.4 Guidance Model

Deploy uses a **personalized guidance model** — different from bootstrap's knowledge injection approach.

| Aspect | Bootstrap | Deploy |
|--------|-----------|--------|
| Guidance source | deploy.md sections + injected knowledge | Programmatic from DeployState + ServiceMeta |
| Runtime knowledge | Injected (agent creating config for first time) | Pointed to (agent pulls on demand) |
| zerops.yml schema | Injected | Pointed to: `zerops_knowledge query="zerops.yml"` |
| Env vars | Injected from DiscoveredEnvVars | Pointed to: `zerops_discover includeEnvs=true` |
| Personalization | Mode-filtered sections | Actual hostnames, commands, steps |
| Guidance size | 100-200+ lines | 15-55 lines |

**Rationale**: Bootstrap is a creative workflow — the agent needs knowledge to create configuration from scratch. Deploy is operational — services and config likely exist. The agent pulls knowledge when needed, not forced.

See `docs/spec-guidance-philosophy.md` for the full guidance delivery specification.

### 4.5 Preflight Gates

`handleDeployStart()` runs 5 sequential gates before creating a session:

| # | Gate | Check | Behavior |
|---|------|-------|----------|
| 1 | Metas exist | `ListServiceMetas()` | Error: "Run bootstrap first" |
| 2 | Metas complete | `IsComplete()` — `BootstrappedAt != ""` | Error: "bootstrap didn't complete" |
| 3 | Runtime services | `Mode != "" \|\| StageHostname != ""` | Error: "nothing to deploy" |
| 4 | Strategy set | `DeployStrategy != ""` | Soft gate: returns strategy selection guidance |
| 5 | Strategy consistent | All targets same strategy | Error: "mixed strategies not supported" |

### 4.6 Step Specifications

**Entry**: `zerops_workflow action="start" workflow="deploy"`

---

#### Deploy Step 1: PREPARE

**Purpose**: Validate platform integration — zerops.yml syntax, hostname match, env var references.

**Guidance**: Personalized setup summary + platform rules + knowledge pointers (15-30 lines).

**Checker**: `checkDeployPrepare` — validates:
1. zerops.yml exists and parses
2. `setup:` entries match deploy target hostnames
3. Env var references (`${hostname_varName}`) valid — re-discovered from API via `client.GetServiceEnv()`

**Checker type**: `DeployStepChecker func(ctx context.Context, state *DeployState) (*StepCheckResult, error)` — separate from bootstrap's `StepChecker` (deploy has no ServicePlan/BootstrapState).

**On failure**: Step does NOT advance. Agent receives `CheckResult` with specific failures. Fixes and retries.

---

#### Deploy Step 2: DEPLOY

**Purpose**: Execute deployments per mode with personalized workflow steps.

**Guidance**: Mode-specific workflow with actual hostnames + strategy commands + platform facts (20-45 lines). Iteration escalation on retries.

**Checker**: `checkDeployResult` — diagnostic feedback based on service status:
- `RUNNING`/`ACTIVE` → pass
- `READY_TO_DEPLOY` (still) → "container didn't start, check start command, ports, env vars"
- Other status → "check zerops_logs severity=error, review build output"
- Subdomain access check for services with ports

**Iteration escalation** (when `action="iterate"` resets deploy+verify):
- Iteration 1: "Check zerops_logs, build failure vs runtime crash"
- Iteration 2: "Systematic check: zerops.yml, env var refs, runtime version"
- Iteration 3+: "Present diagnostic summary to user"

---

#### Deploy Step 3: VERIFY

**Purpose**: Informational health confirmation. Agent runs `zerops_verify` per target.

**Checker**: nil — verify is informational, not blocking. Agent attests completion.

**Guidance**: Diagnostic patterns (from deploy.md `deploy-verify` section).

---

## 5. State Model

### 5.1 Session State (ephemeral)

```mermaid
stateDiagram-v2
    [*] --> Created: action=start
    Created --> Active: first step in_progress

    Active --> Active: complete/skip step
    Active --> Iterating: action=iterate
    Iterating --> Active: reset steps, continue

    Active --> Completed: all steps done
    Completed --> [*]: session file deleted,<br/>ServiceMeta written

    Active --> Suspended: process dies
    Suspended --> Active: action=resume

    Active --> Cancelled: action=reset
    Cancelled --> [*]: session file deleted
```

**Structure** (`.zcp/state/sessions/{id}.json`):
```
WorkflowState {
  Version:    "1"
  SessionID:  16-hex random
  PID:        process owner (liveness signal)
  ProjectID:  Zerops project ID
  Workflow:   "bootstrap" | "deploy" | "cicd"
  Iteration:  cycle counter (0, 1, 2, ...)
  Intent:     user's stated goal
  CreatedAt:  RFC3339
  UpdatedAt:  RFC3339
  Bootstrap:  *BootstrapState  (if bootstrap)
  Deploy:     *DeployState     (if deploy)
  CICD:       *CICDState       (if cicd)
}
```

**Session registry** (`.zcp/state/registry.json`):
- Tracks all active sessions with PID, workflow, project
- Exclusive lock via `.registry.lock` file
- Auto-prunes dead PIDs and sessions >24h old

### 5.2 ServiceMeta (persistent evidence)

**Structure** (`.zcp/state/services/{hostname}.json`):
```
ServiceMeta {
  Hostname:         string   // immutable
  Mode:             string   // "standard" | "dev" | "simple" (empty for managed)
  StageHostname:    string   // derived stage (standard mode only)
  DeployStrategy:   string   // "push-dev" | "ci-cd" | "manual" (empty until explicitly set)
  BootstrapSession: string   // session ID that created this
  BootstrappedAt:   string   // date — empty means bootstrap incomplete
}
```

**Write lifecycle**:

```mermaid
stateDiagram-v2
    [*] --> Provisioned: provision step (partial meta, no BootstrappedAt)
    Provisioned --> Bootstrapped: close step triggers writeBootstrapOutputs
    Bootstrapped --> StrategySet: user calls action=strategy

    note right of Bootstrapped
        DeployStrategy empty.
        Must set before deploy workflow.
    end note

    note right of StrategySet
        DeployStrategy = push-dev | ci-cd | manual.
        Deploy workflow can proceed.
    end note
```

**Rules**:
- Only runtime services get ServiceMeta — managed deps are API-authoritative
- EXISTS/SHARED dependencies: never overwrite existing ServiceMeta files
- Strategy is NEVER auto-assigned — always empty until explicit `action=strategy`
- `IsComplete()` returns true only when `BootstrappedAt` is set

### 5.3 Discovered Environment Variables

**Storage**: `BootstrapState.DiscoveredEnvVars = map[hostname][]varNames`

**Flow**:
1. `zerops_discover includeEnvs=true` returns actual values (passwords, connection strings) — TRANSIENT
2. Session stores NAMES ONLY — PERSISTENT during session
3. Agent guidance uses `${hostname_varName}` references — SAFE (no secrets in prompts)
4. Agents write references in zerops.yml `envVariables` — resolved at container level

**Security**: Discover tool exposes unmasked values. This is by design for validation purposes. Agent prompts receive names only. Values never persisted in session state.

---

## 6. Mode Behavior Matrix

Complete cross-reference of how each mode affects every aspect of the workflow:

| Aspect | Standard | Dev | Simple | Managed-only |
|--------|----------|-----|--------|-------------|
| **Services created** | `{name}dev` + `{name}stage` + managed | `{name}dev` + managed | `{name}` + managed | managed only |
| **import.yml dev** | `startWithoutCode: true`, `maxContainers: 1` | same | `startWithoutCode: true` | N/A |
| **import.yml stage** | No `startWithoutCode` (stays READY_TO_DEPLOY) | N/A | N/A | N/A |
| **SSHFS mounts** | dev only | dev only | service only | None |
| **zerops.yml entries** | Dev first, stage AFTER dev verified | Dev only | Single entry | N/A |
| **zerops.yml start** | `zsc noop --silent` (dev) / real (stage) | `zsc noop --silent` | Real command | N/A |
| **zerops.yml healthCheck** | None (dev) / required (stage) | None | Required | N/A |
| **zerops.yml deployFiles** | `[.]` (dev) / build output (stage) | `[.]` | `[.]` | N/A |
| **Server start method** | SSH manual (dev) / auto (stage) | SSH manual | Auto (healthCheck) | N/A |
| **Subdomain enable** | Both dev + stage | Dev only | Service only | N/A |
| **Deploy flow** | dev→verify→stage→verify→cross-verify | dev→verify→cross-verify | deploy→verify→cross-verify | N/A |
| **Iteration resets** | Steps 2-3 (generate + deploy) | Same | Same | N/A |
| **Strategy** | Explicit choice required | Explicit choice required | Explicit choice required | N/A |
| **Skip generate** | No | No | No | Yes |
| **Skip deploy** | No | No | No | Yes |
| **Skip close** | No | No | No | Yes |
| **PHP runtimes** | Omit `start:` entirely | Same | Same | N/A |

---

## 7. Flow Transitions & Resumption

### 7.1 Bootstrap → Strategy → Deploy Transition

```mermaid
sequenceDiagram
    participant Agent
    participant Engine
    participant Disk

    Note over Agent,Engine: Bootstrap completes (all 5 steps)
    Engine->>Disk: Write ServiceMeta files (strategy=empty)
    Engine->>Disk: Delete session file
    Engine->>Disk: Unregister from registry
    Engine->>Agent: Transition message:<br/>"Choose deployment strategy for each service"

    Note over Agent,Engine: User chooses strategy
    Agent->>Engine: zerops_workflow action="strategy"<br/>strategies={"appdev":"push-dev"}
    Engine->>Disk: Update ServiceMeta (DeployStrategy=push-dev)

    Note over Agent,Engine: User requests deploy
    Agent->>Engine: zerops_workflow action="start" workflow="deploy"
    Engine->>Disk: Read ServiceMeta files
    Engine->>Engine: Check strategy set ✓
    Engine->>Engine: Build deploy targets from metas
    Engine->>Disk: Create deploy session
    Engine->>Agent: DeployResponse with targets + guidance
```

### 7.2 Resumption After Interruption

```mermaid
sequenceDiagram
    participant Agent as New Agent Session
    participant Engine
    participant Disk

    Agent->>Engine: zerops_workflow action="status"
    Engine->>Disk: Read registry → find active sessions
    Engine->>Agent: Session found: {id}, step 3/5, workflow=bootstrap

    Agent->>Engine: zerops_workflow action="resume" sessionId="{id}"
    Engine->>Disk: Load session state
    Engine->>Engine: Check PID: dead → take over
    Engine->>Disk: Update PID to current process
    Engine->>Agent: Full state with guidance for current step

    Note over Agent: Agent continues from where it left off
```

**PID-based ownership**: Session bound to creating process via PID. If PID is dead, any process can resume. If PID is alive, resume fails (session owned by another process).

### 7.3 Iteration

**Bootstrap iteration** (`action="iterate"`):
- Increments `Iteration` counter
- Resets steps 2-3 (generate, deploy) to `pending`
- Sets `CurrentStep = 2` (generate), marks `in_progress`
- **Preserves**: discover attestation, provision attestation + env vars, plan, ServiceMetas, close step
- Close (step 4) is NOT reset — it's administrative, not retryable

**Deploy iteration** (`action="iterate"`):
- Increments `Iteration` counter
- Resets steps 1-2 (deploy, verify) to `pending`
- Resets all target statuses to `pending`
- Sets `CurrentStep = 1` (deploy)
- **Preserves**: prepare step

**Escalating guidance** — iteration tiers differ by workflow:

| Tier | Bootstrap (`BuildIterationDelta`) | Deploy (`writeIterationEscalation`) |
|------|----------------------------------|-------------------------------------|
| Diagnose | Iterations 1-2 | Iteration 1 |
| Systematic check | Iterations 3-4 | Iteration 2 |
| Stop, ask user | Iterations 5+ | Iteration 3+ |

Bootstrap has wider ranges because it allows up to 10 iterations (configurable via `ZCP_MAX_ITERATIONS`). Deploy tiers escalate faster.

---

## 8. Invariants & Contracts

### Session Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| I1 | Only one bootstrap session active at any time | `InitSessionAtomic()` with registry lock |
| I2 | Step completion requires attestation ≥ 10 chars | `CompleteStep()` validation |
| I3 | Steps progress strictly in order | `CompleteStep()` name-matching check |
| I4 | discover and provision cannot be skipped | `SkipStep()` checks `Skippable` field |
| I5 | generate, deploy, close cannot be skipped when runtime targets exist | `validateConditionalSkip()` |
| I6 | Session file atomically written (temp + rename) | `SaveSessionState()` |
| I7 | Completed sessions cleaned up (file deleted, registry entry removed) | `ResetSessionByID()` at completion |
| I8 | Non-discover steps require plan from discover step | `BootstrapComplete()` Plan!=nil check |

### State Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| S1 | ServiceMeta written at two lifecycle points: provisioned (partial) and bootstrapped (complete) | `writeProvisionMetas()` and `writeBootstrapOutputs()` |
| S2 | Only runtime services get ServiceMeta — managed deps are API-authoritative | `writeBootstrapOutputs()` iterates plan.Targets only |
| S3 | Env var names (not values) stored in session state | `DiscoveredEnvVars` type is `map[string][]string` (names) |
| S4 | Strategy is never auto-assigned — always empty until explicit `action=strategy` | `writeBootstrapOutputs()` reads Strategies map without fallback |

### Knowledge Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| K1 | Bootstrap generate step receives: runtime briefing, dependency wiring, discovered env vars, zerops.yml schema | `assembleKnowledge()` for generate step |
| K2 | Bootstrap deploy step receives: schema rules | `assembleKnowledge()` for deploy step |
| K3 | Bootstrap provision step receives: import.yml schema | `assembleKnowledge()` for provision step |
| K4 | Bootstrap iteration > 0 returns escalating recovery guidance | `BuildIterationDelta()` |
| K5 | Deploy workflow prepare and deploy steps are programmatic from state (hostnames, mode, strategy). Verify step uses deploy.md diagnostic patterns (generic, no personalization needed). | `buildPrepareGuide()`, `buildDeployGuide()`, `buildVerifyGuide()` in deploy_guidance.go |
| K6 | Deploy workflow NEVER injects runtime briefings or schema — uses knowledge pointers only | `buildKnowledgeMap()` returns pointers, not content |
| K7 | Deploy guidance ≤ 55 lines per step | Personalized builder limits |

### Flow Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| F1 | Deploy workflow requires existing ServiceMeta files with strategy set | `handleDeployStart()` reads metas + checks DeployStrategy |
| F2 | CI/CD workflow requires at least one service with ci-cd strategy | `handleCICDStart()` filters by StrategyCICD |
| F3 | Router returns facts only — no recommendations, no intent matching | `Route()` returns FlowOffering without Reason field |
| F4 | Immediate workflows (debug, scale, configure) are stateless | `IsImmediateWorkflow()` check |
| F5 | Bootstrap auto-resets completed sessions on new start | `engine.Start()` checks Active=false |
| F6 | Deploy strategy selection is conversational (guidance, not error) | `buildStrategySelectionResponse()` |
| F7 | Deploy checkers validate platform integration, not application correctness | `checkDeployPrepare()`, `checkDeployResult()` |
| F8 | Deploy env var validation re-discovers from API (standalone, no BootstrapState) | `validateDeployEnvRefs()` calls `client.GetServiceEnv()` |

### Operational Invariants

| ID | Invariant | Enforced by |
|----|-----------|-------------|
| O1 | `zerops_deploy` blocks until build completes | `PollBuild()` in ops/deploy.go |
| O2 | `zerops_import` blocks until all processes complete | Process polling in ops/import.go |
| O3 | `zerops_subdomain` must be called after every deploy | Documented in guidance + deploy step |
| O4 | `zerops_verify` runtime checks depend on runtime class | `classifyRuntime()` in ops/verify.go |
| O5 | Server must be started via SSH after every dev deploy | Container restart kills server; guidance enforces |

---

## 9. Recovery & Error Handling

### Verification Failure Diagnosis

| Failed check | Diagnosis | Fix |
|-------------|-----------|-----|
| service_running: fail | Service not running | Check deploy status, read `zerops_logs severity=error` |
| startup_detected: fail | App crashed on start | `zerops_logs severity=error since=5m` |
| error_logs: info | Advisory — errors found | Read detail. SSH/infra noise → ignore. App errors → investigate. |
| http_root: fail | App not responding on / | Check port, binding, start command |
| http_status: fail | Managed service connectivity | Check env var mapping vs discovered vars |

### Common Fix Patterns

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands | Check runtime knowledge |
| Build FAILED: "module not found" | Missing dependency install | Add install to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Ensure port matches zerops.yml |
| App crashes: "connection refused" | Wrong env var name | Check envVariables vs discovered vars |
| HTTP 502 | Subdomain not activated | Call `zerops_subdomain action="enable"` |
| Empty response | Not listening on 0.0.0.0 | Fix app binding |
| HTTP 500 | App error | Check `zerops_logs` + framework log files FIRST |

---

## 10. Known Gaps & Future Work

| Gap | Impact | Status |
|-----|--------|--------|
| Import error surfacing (per-service API errors) | Partial import failures hard to diagnose | Better error detail needed |
| Least-privilege discover mode | Discover unnecessarily returns actual secret values | Future: names-only mode option |
| ~~Env var reference validation~~ | ~~Invalid refs silently kept as literals~~ | **RESOLVED**: `checkDeployPrepare` re-discovers and validates |
| ~~Router strategy offering~~ | ~~Router should suggest strategy when empty~~ | **RESOLVED**: Route returns facts; handleDeployStart has strategy gate |
| ~~Deploy guidance too verbose~~ | ~~200+ lines injected per step~~ | **RESOLVED**: Personalized guidance, 15-55 lines, knowledge pointers |
| ~~No deploy checkers~~ | ~~Agent self-attests all steps~~ | **RESOLVED**: `checkDeployPrepare` + `checkDeployResult` |
