# ZCP Workflow Specification

> **Status**: Authoritative. State model + delivery pipeline in lock-step with `internal/workflow/*.go`.
> **Scope**: Bootstrap, adoption, strategy, develop, recipe, cicd, export — both container and local environments, all modes; plus the envelope/plan/atom pipeline that feeds every workflow-aware response.
> **Date**: 2026-04-19
> **Companion docs**:
> - `docs/spec-scenarios.md` — per-scenario acceptance walkthrough (S1–S13), pinned by `internal/workflow/scenarios_test.go`.
> - `docs/spec-work-session.md` — per-PID Work Session for develop.
> - `docs/spec-knowledge-distribution.md` — atom corpus authoring model (axes, priorities, placeholders).
> - `plans/instruction-delivery-rewrite.md` — architectural reference for the pipeline (Layers 1/2/3, data model, phase inventory).

---

## 1. Lifecycle Overview

### 1.1 Service Lifecycle — Two Phases

Every service on Zerops goes through two phases:

```mermaid
flowchart LR
    subgraph Phase1 ["Phase 1: Enter Evidence (once)"]
        Bootstrap["Bootstrap<br/>(new service)"]
        Adoption["Adoption<br/>(existing service)"]
    end

    subgraph Phase2 ["Phase 2: Development Lifecycle (repeated)"]
        Develop["Develop Flow<br/>knowledge → work → deploy"]
    end

    Bootstrap --> Meta["ServiceMeta<br/>(evidence file)"]
    Adoption --> Meta
    Meta --> Develop
    Develop -->|"strategy read<br/>from meta"| Develop

    style Meta fill:#dfd,stroke:#0a0
```

**Phase 1 — Infrastructure**: Bootstrap creates new services (or adoption registers existing ones) and writes an evidence file (ServiceMeta). Only a **verification server** is deployed — a hello-world proving infrastructure works (/, /health, /status). No application logic, no strategy. Phase 1 answers: "can this service start, respond, and reach its dependencies?"

**Phase 2 — Development**: Develop flow covers ALL code work on the service — implementing the user's actual application, bug fixes, config changes, everything. It discovers what code exists (verification server from bootstrap or existing application), provides runtime knowledge, lets the agent implement what the user wants, and deploys at the end. Strategy is always read fresh from ServiceMeta.

**The boundary is strict**: Bootstrap writes zerops.yaml + infrastructure verification server. The moment the agent needs to write application logic, it must be in develop flow. If the user says "create me an app for uploading photos in Bun", bootstrap creates Bun service + dependencies with a hello-world verification server, then develop flow implements the photo upload app.

### 1.2 Phase Enum — The Single State Variable

The lifecycle above is collapsed into a single typed `Phase` field carried in every `StateEnvelope` (see §1.6). The enum is exhaustive — every tool response resolves to exactly one phase:

| Phase | Meaning | Set by |
|---|---|---|
| `idle` | No active workflow session for this PID. | Default; or after session closes. |
| `bootstrap-active` | A bootstrap session is in progress. | `zerops_workflow action=start workflow=bootstrap`. |
| `develop-active` | A per-PID Work Session is open. | `zerops_workflow action=start workflow=develop`. |
| `develop-closed-auto` | Work Session has `ClosedAt` set and `CloseReason=auto-complete`. Transitional phase — awaits explicit close + next. | Auto-close in `EvaluateAutoClose` when every scope service has a succeeded deploy + passed verify. |
| `recipe-active` | A recipe-authoring session is in progress. | `zerops_workflow action=start workflow=recipe`. |
| `cicd-active` | Stateless immediate workflow (no session) returning CI/CD guidance. | `zerops_workflow action=start workflow=cicd`. |
| `export-active` | Stateless immediate workflow returning export guidance. | `zerops_workflow action=start workflow=export`. |

Invariant: at most one non-idle **stateful** phase per PID at a time. `cicd-active`/`export-active` are stateless — they synthesize guidance and return without touching session state, so they never conflict with an active bootstrap/develop/recipe session.

See `plans/instruction-delivery-rewrite.md` §4.1 for the concrete Go enum.

### Why Verification-First — The Foundational Principle

The two-phase separation (bootstrap verification → develop application) is the foundational architectural decision of the workflow system. It applies to **ALL modes** — standard, dev, and simple — without exception, even when it appears as overhead for simple setups.

**Fault isolation.** When bootstrap and application are separate, failures have unambiguous origin. If verification fails during bootstrap, the problem is infrastructure — service config, env vars, managed service connectivity. If the app fails during develop, infrastructure is already proven healthy. Without this separation, every failure requires diagnosing both layers simultaneously, which is exponentially harder for an AI agent.

**Universal deployment flow.** By always following the same two-phase pattern, every mode behaves predictably. The deploy step's structure (deploy → verify → iterate) is identical regardless of whether it's standard mode with dev/stage pairs or simple mode with a single service. This universality makes the deployment flow stable and eliminates mode-specific edge cases.

**Reduced blast radius.** Infrastructure problems are caught before any application code exists. The verification server is ~50 lines — when it fails, there are very few places the bug can hide. Once infra is proven, the develop workflow adds application complexity on a stable foundation.

**Faster iteration in develop.** Once bootstrap completes, the develop workflow knows that env vars resolve, managed services connect, and the service can start and respond. Develop iterations focus purely on application logic — no re-verification of infrastructure plumbing.

This principle must never be bypassed. An agent that writes application code during bootstrap (even for simple mode) violates this boundary and loses all four benefits above.

### ServiceMeta — The Evidence File

ServiceMeta (`.zcp/state/services/{hostname}.json`) is the persistent evidence that a service is under ZCP management.

```
ServiceMeta {
  Hostname          string  // service identifier
  Mode              string  // "standard" | "dev" | "simple"
  StageHostname     string  // stage pair (standard mode only, empty otherwise)
  DeployStrategy    string  // "push-dev" | "push-git" | "manual" (empty until set)
  StrategyConfirmed bool    // true after user explicitly confirms/sets strategy
  Environment       string  // "container" | "local"
  BootstrapSession  string  // session ID that created this; EMPTY for adoption
  BootstrappedAt    string  // date — empty = incomplete (bootstrap in progress)
}
```

**`BootstrapSession == ""` convention.** Empty (JSON-wise: empty string, not
null) is the adoption marker. Fresh bootstraps set this to the 16-hex
session ID; adoption path writes it as empty. `IsAdopted()` disambiguates
adopted metas from orphan incomplete metas (which also carry an empty
session ID) by requiring `BootstrappedAt` to be set: an adopted meta is
always complete, an orphan never is.

```mermaid
stateDiagram-v2
    [*] --> Provisioned: bootstrap provision step<br/>(partial meta, no BootstrappedAt)
    Provisioned --> Evidenced: bootstrap close OR adoption<br/>(BootstrappedAt set)
    Evidenced --> StrategySet: action="strategy"<br/>(DeployStrategy set)
    StrategySet --> StrategySet: action="strategy"<br/>(strategy changed)

    note right of Evidenced
        DeployStrategy empty.
        Develop flow informs agent,
        resolves before deploying.
    end note

    note right of StrategySet
        Strategy = push-dev | push-git | manual.
        Develop flow reads from meta each time.
    end note
```

`IsComplete()` returns true when `BootstrappedAt` is set.
`IsAdopted()` returns true when `BootstrapSession` is empty AND the meta
`IsComplete()`. Strategy is always read from meta at the moment it's
needed — never copied into session state.

### Principles

- **Workflow is NOT a gate.** An agent does not need to start a workflow to call `zerops_scale`, `zerops_manage`, or any other direct tool. Workflows add structure for multi-step operations.
- **Strategy never blocks work.** Agent can always start editing code. Strategy is resolved before deploying, not before working.
- **Tools work independently.** `zerops_discover`, `zerops_verify`, `zerops_knowledge` work without any active workflow.

### 1.3 Delivery Pipeline — Envelope → Plan → Atoms

Every workflow-aware tool response is produced by the same three-stage pipeline, not by ad-hoc guidance assembly.

```
          ┌───────────────┐      ┌────────────┐      ┌──────────────┐
          │ ComputeEnvelope│──▶──▶│ BuildPlan  │      │  Synthesize  │
          │ (state + live  │      │ (Primary,  │  ┌──▶│  (atom filter│
          │  API + session)│      │  Secondary,│  │   │  + compose)  │
          └───────────────┘      │  Alts)     │  │   └──────────────┘
                 │               └────────────┘  │         ▲
                 │                    │          │         │
                 │                    └────────┬─┘         │
                 ▼                             ▼           │
          StateEnvelope  ──────▶──────▶──────▶─┴─── LoadAtomCorpus
                                                     (//go:embed atoms/*.md)
                                 │
                                 ▼
                         ┌────────────────┐
                         │  RenderStatus  │
                         │  (markdown UI) │
                         └────────────────┘
```

**Stage 1 — `ComputeEnvelope`** (`internal/workflow/compute_envelope.go`): the single entry point for state gathering. Reads services from the platform API, service metas from `.zcp/state/services/`, bootstrap session state, the current Work Session, and runtime detection — merging them into a `StateEnvelope`. Called by every workflow-aware tool handler. I/O is parallelised so a tool response pays one round-trip for the envelope regardless of how many state sources are involved.

**Stage 2 — `BuildPlan`** (`internal/workflow/build_plan.go`): a pure function `Plan = BuildPlan(env)`. Deterministic — no I/O, no randomness — so the plan can be reproduced verbatim after LLM context compaction from the same envelope. Branching is a fixed nine-case switch driven by `env.Phase` plus envelope shape (see §1.4).

**Stage 3 — `Synthesize`** (`internal/workflow/synthesize.go`): pure function `guidance = Synthesize(env, corpus)`. Loads the atom corpus once (`LoadAtomCorpus`), filters by axis-match against the envelope, sorts by priority + id, substitutes placeholders from the envelope, and returns the composed bodies. Same compaction-safety invariant: byte-identical output for byte-equal envelopes.

**Stage 4 — `RenderStatus`** (`internal/workflow/render.go`): consumes a `Response{Envelope, Guidance, Plan}` and emits the markdown status block shown to the LLM. The Next section renders the typed `Plan` with priority markers — no free-form Next string, no ad-hoc branching in the renderer.

### 1.4 Plan — Typed Trichotomy

The Plan replaces every piece of "what should the agent do next" that used to live in free-form markdown or `writeStatusNext` branches.

```go
type Plan struct {
    Primary      NextAction   // never zero — if we don't know, we error out upstream
    Secondary    *NextAction  // set only when a second action is commonly done in tandem
    Alternatives []NextAction // genuinely alternative paths
}
```

Dispatch (strict order, first match wins — see `build_plan.go` for the code):

1. `PhaseDevelopClosed` → Primary=close-session, Secondary=start-next.
2. `PhaseDevelopActive`, some service without a successful deploy → Primary=deploy.
3. `PhaseDevelopActive`, deploy done but verify missing → Primary=verify.
4. `PhaseDevelopActive`, last attempt failed → Primary=diagnose-and-retry (reads logs first).
5. `PhaseDevelopActive`, everything green but session still open → Primary=close.
6. `PhaseBootstrapActive` → Primary=continue-bootstrap (route-specific).
7. `PhaseRecipeActive` → Primary=continue-recipe.
8. `PhaseIdle` with no services → Primary=start-bootstrap.
9. `PhaseIdle` with bootstrapped services → Primary=start-develop + alternatives (adopt if any unmanaged, add-more-services always).
10. `PhaseIdle` with only unmanaged runtimes → Primary=adopt-via-develop.

Gate semantics in the Plan are informational, not structural: e.g. `Strategy=unset` does not block the Plan from naming a deploy action; the atom `develop-strategy-unset` surfaces the gate in the body instead. This keeps `BuildPlan` a pure dispatch over envelope shape.

### 1.5 Atom Corpus — Orthogonal Knowledge Matrix

Runtime-dependent guidance lives as ~74 atoms under `internal/content/atoms/*.md`, embedded via `//go:embed`. Each atom has YAML frontmatter declaring its `AxisVector` and a markdown body.

```yaml
---
id: develop-dynamic-runtime-start-container
priority: 2
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
title: "Dynamic runtime — start over SSH after deploy"
---

After a dynamic-runtime deploy, the container is running `zsc noop`. Start
the real server over SSH:
...
```

**Axes** (the knowledge-variation dimensions):

| Axis | Values | Emptiness semantic |
|---|---|---|
| `phases` | `idle`, `bootstrap-active`, `develop-active`, `develop-closed-auto`, `recipe-active`, `cicd-active`, `export-active` | MUST be non-empty. |
| `modes` | `dev`, `stage`, `simple` | Empty = any mode. |
| `environments` | `container`, `local` | Empty = either. |
| `strategies` | `push-dev`, `push-git`, `manual`, `unset` | Empty = any strategy. |
| `runtimes` | `dynamic`, `static`, `implicit-webserver`, `managed`, `unknown` | Empty = any runtime. |
| `routes` | `recipe`, `classic`, `adopt` | Bootstrap-only. Empty = any route. |
| `steps` | bootstrap step names | Bootstrap-only. Empty = any step. |

**Synthesizer contract**:

1. Filter: an atom matches iff every non-empty axis permits the envelope. Service-scoped axes (`modes`/`strategies`/`runtimes`) match if *any* service in `env.Services` matches.
2. Sort: priority ascending (1 first), then id lexicographically.
3. Substitute: `{hostname}`, `{stage-hostname}`, `{project-name}` are replaced from the envelope; a whitelist of agent-filled placeholders (`{start-command}`, `{port}`, …) survives untouched. Any unknown `{word}` token is a build-time error.
4. Return: ordered list of rendered bodies.

**Compaction-safety invariant**: for byte-equal envelopes, `Synthesize` MUST return byte-identical output. No map iteration, no timestamps, no randomness leaks into the body.

### 1.6 StateEnvelope — Live Data Contract

`StateEnvelope` is the single typed data structure passed between stages. It is attached verbatim to every workflow-aware tool response, so the LLM can reconstruct state post-compaction.

| Field | Purpose |
|---|---|
| `Phase` | The phase enum from §1.2. Drives atom filtering and plan dispatch. |
| `Environment` | `container` or `local`. Driven by `runtime.Info.InContainer`. |
| `SelfService` | Hostname of the ZCP control-plane container (container env only). |
| `Project` | `{ID, Name}` — project identity. |
| `Services[]` | Sorted snapshots: hostname, type+version, runtime class, status, bootstrapped flag, mode, strategy, stage pair. |
| `WorkSession` | Open develop session summary: intent, scope, deploy/verify attempts, close state. `nil` outside develop. |
| `Recipe` | Recipe session summary. `nil` outside recipe-active. |
| `Bootstrap` | Bootstrap session summary: route, step, iteration. `nil` outside bootstrap-active. |
| `Generated` | Timestamp for the envelope (diagnostics only — not part of synthesis input). |

Slices sort by hostname; attempt lists sort by time; maps use key-sorted encoding. The JSON form is deterministic, which is what makes §1.3's compaction-safety invariant provable.

Full field-level Go definitions live in `internal/workflow/envelope.go` and `plans/instruction-delivery-rewrite.md` §4.

---

## 2. Bootstrap Flow

Bootstrap creates a new service on Zerops and writes the evidence file. That is its only job — it does NOT set deploy strategy.

```mermaid
flowchart TD
    Start([Agent triggers bootstrap]) --> CreateSession["Create session<br/>Generate 16-hex ID<br/>Register in registry"]
    CreateSession --> Discover

    subgraph Bootstrap ["Bootstrap Flow (5 steps)"]
        direction TB
        Discover["1. DISCOVER<br/>─────────────<br/>Classify services<br/>Identify stack<br/>Choose mode<br/>Submit plan"]

        Provision["2. PROVISION<br/>─────────────<br/>Generate import.yaml<br/>Create services<br/>Mount dev filesystems<br/>Discover env vars"]

        Generate["3. GENERATE<br/>─────────────<br/>Write zerops.yaml<br/>Write app code<br/>Mode-specific rules"]

        Deploy["4. DEPLOY<br/>─────────────<br/>Deploy to services<br/>Start servers<br/>Enable subdomains<br/>Full health verification"]

        Close["5. CLOSE<br/>─────────────<br/>Write ServiceMeta<br/>Append reflog"]
    end

    Discover -->|"plan submitted"| Provision
    Provision -->|"services created"| ModeCheck{Has runtime<br/>targets?}

    ModeCheck -->|Yes| Generate
    ModeCheck -->|"No (managed-only)"| SkipGen["SKIP generate"]
    SkipGen --> SkipDeploy["SKIP deploy"]
    SkipDeploy --> SkipClose["SKIP close"]

    Generate -->|"code written"| Deploy
    Deploy -->|"all healthy"| Close

    Deploy -->|"failed"| IterCheck{Iteration<br/>< max?}
    IterCheck -->|Yes| Iterate["ITERATE<br/>Reset steps 2-3"]
    Iterate --> Generate
    IterCheck -->|No| FailReport["Report to user"]

    SkipClose --> Complete
    Close --> Complete

    Complete([Bootstrap Complete<br/>ServiceMeta written<br/>No strategy set])

    style Discover fill:#e8f4fd,stroke:#2196F3
    style Provision fill:#e8f4fd,stroke:#2196F3
    style Generate fill:#fff3e0,stroke:#FF9800
    style Deploy fill:#fce4ec,stroke:#E91E63
    style Close fill:#e8f4fd,stroke:#2196F3
    style SkipGen fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SkipDeploy fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
    style SkipClose fill:#f5f5f5,stroke:#9e9e9e,stroke-dasharray: 5 5
```

### 2.1 Session Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Created: action=start
    Created --> Active: first step in_progress

    Active --> Active: complete/skip step
    Active --> Iterating: action=iterate
    Iterating --> Active: reset steps, continue

    Active --> Completed: all steps done
    Completed --> [*]: session deleted,<br/>ServiceMeta written

    Active --> Suspended: process dies
    Suspended --> Active: action=resume

    Active --> Cancelled: action=reset
    Cancelled --> [*]: session deleted
```

**Create**: `zerops_workflow action="start" workflow="bootstrap" intent="..."`
- Generates 16-hex session ID, registers in registry with PID.
- Sets step 0 (discover) to `in_progress`.
- Returns available stack catalog from live API.

**Progress**: `action="complete" step="{name}" attestation="..."` (min 10 chars).
- Optional checker validates before allowing completion. Failure → step stays, agent gets details.

**Skip**: `action="skip" step="{name}" reason="..."`.
- `discover` and `provision`: NEVER skippable.
- `generate`, `deploy`, `close`: skippable only when the plan has no
  runtime targets (managed-only) OR every runtime target has
  `IsExisting=true` (pure-adoption). See §2.8.

**Iterate**: `action="iterate"` resets `generate` + `deploy` to pending. Preserves discover, provision, close, plan, env vars. Max 10 iterations (configurable via `ZCP_MAX_ITERATIONS`).

**Resume**: `action="resume" sessionId="..."` takes over dead session (PID check). Continues from current step.

### 2.2 Exclusivity

Per-service, not global. Multiple bootstraps coexist for different services. Same-hostname lock: incomplete ServiceMeta from alive session blocks new bootstrap for that hostname. Dead PID → auto-unlock.

### 2.3 Step 1: Discover

**Purpose**: Classify services, identify stack, choose mode, submit plan.

**Procedure**:
1. `zerops_discover` — see existing services.
2. Identify runtime + dependencies from user intent.
3. Validate types against `availableStacks`.
4. Choose mode:
   - **Standard** (default): `{name}dev` + `{name}stage` + managed.
   - **Dev**: `{name}dev` + managed.
   - **Simple**: `{name}` + managed.
5. Present plan to user, get confirmation.
6. Submit: `action="complete" step="discover" plan=[...]`

**Plan structure**:
```
ServicePlan {
  Targets: [{
    Runtime: {
      DevHostname    string  // a-z0-9, max 25 chars
      Type           string  // validated against live catalog
      IsExisting     bool    // true = adoption path (see §3)
      BootstrapMode  string  // "standard" | "dev" | "simple" (empty → standard)
      ExplicitStage  string  // optional stage hostname override
    },
    Dependencies: [{
      Hostname    string
      Type        string
      Mode        string  // "HA" | "NON_HA" (defaults to NON_HA for managed)
      Resolution  string  // "CREATE" | "EXISTS" | "SHARED"
    }]
  }]
}
```

**Validation**: Hostnames `[a-z0-9]` max 25 chars. Standard mode: devHostname must end in "dev", stage derived as `{prefix}stage`. Types against live catalog. Resolution: CREATE = must not exist, EXISTS = must exist, SHARED = another target creates it. Hostname lock check. Errors accumulated (all reported at once).

### 2.4 Step 2: Provision

**Purpose**: Create infrastructure, mount filesystems, discover env vars.

**Procedure**:
1. Generate import.yaml → `zerops_import` (blocks until all processes complete).
2. `zerops_discover` — verify services exist.
3. Mount: container = `zerops_mount` per dev runtime at `/var/www/{hostname}/`. Local = none.
4. `zerops_discover includeEnvs=true` — discover env var NAMES only.

**Env var security model**:
- `includeEnvs=true` returns keys and annotations only — SAFE by default.
- `includeEnvValues=true` opt-in exposes actual values — for troubleshooting only.
- Session stores NAMES ONLY — never values.
- Agent uses `${hostname_varName}` references — resolved at container level.

**import.yaml by mode**:

| Property | Dev service | Stage service | Simple service |
|----------|-----------|---------------|----------------|
| `startWithoutCode` | `true` | omit | `true` |
| `maxContainers` | `1` | omit | omit |
| `enableSubdomainAccess` | `true` | `true` | `true` |

**Expected states**: dev → RUNNING, stage → READY_TO_DEPLOY, managed → RUNNING/ACTIVE.

**On completion**: Writes partial ServiceMeta (no BootstrappedAt) — signals bootstrap in-progress, provides hostname lock.

**Checker**: All services exist, types match, status correct, managed dependency env vars discovered.

### 2.5 Step 3: Generate

**Purpose**: Write zerops.yaml and an **infrastructure verification server** proving services are reachable. NOT the user's application.

**Scope boundary**: Generate writes the MINIMUM needed to verify infrastructure:
- zerops.yaml with mode-specific rules
- A verification server with exactly three endpoints (/, /health, /status) — under 50 lines
- Env var wiring to prove dependency connectivity

Generate does NOT write application logic, business features, or the user's actual request. That happens in develop flow (§4). If the user asked for "a photo upload app", generate creates a verification server with /status proving S3 connectivity — the photo upload implementation comes in develop flow.

**Skip**: Only if managed-only.

**Required endpoints** (minimal proof-of-concept):

| Endpoint | Response | Purpose |
|----------|----------|---------|
| `GET /` | `"Service: {hostname}"` | Smoke test |
| `GET /health` | `{"status":"ok"}` (200) | Liveness probe |
| `GET /status` | Connectivity JSON (200) | Proves managed service connections |

`/status` must actually connect to each dependency:
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

**zerops.yaml rules by mode**:

| Property | Standard (dev entry) | Standard (stage entry) | Dev | Simple |
|----------|---------------------|----------------------|-----|--------|
| `start` | `zsc noop --silent` | real command | `zsc noop --silent` | real command |
| `healthCheck` | none | required | none | required |
| `deployFiles` | `[.]` | build output (NOT `[.]`) | `[.]` | `[.]` |
| `buildCommands` | deps install | deps + compile | deps install | deps + compile |
| PHP runtimes | omit `start:` | omit `start:` | omit `start:` | omit `start:` |
| Stage entry | NOT YET (written after dev verified) | — | N/A | N/A |

**Why `zsc noop`**: Manual server lifecycle control. With real start, deploy auto-starts and agent can't iterate without redeploying.

**Why no healthCheck on dev**: `zsc noop` exits immediately — healthCheck would restart container in a loop.

**Why stage entry deferred**: Written after dev verification. Prevents deploying untested config.

**Why stage `deployFiles` is build output, NOT `[.]`**: Stage receives compiled artifacts optimized for production. Dev uses `[.]` because it iterates on source.

**Pre-deploy checklist** (agent verifies before completing step):
- [ ] `setup:` hostname matches plan
- [ ] `deployFiles: [.]` for dev services (NO EXCEPTIONS)
- [ ] `start:` correct for mode (noop for standard/dev, real for simple, omit for PHP)
- [ ] `run.ports` matches app listen port (omit for PHP)
- [ ] `envVariables` uses ONLY discovered var names
- [ ] App binds to `0.0.0.0:{port}` (NOT localhost)
- [ ] Simple mode: `healthCheck` present
- [ ] Standard mode: NO stage entry yet

**Checker**: zerops.yaml exists, setup entries match plan, env refs match discovered vars, ports defined, deployFiles set.

### 2.6 Step 4: Deploy

**Purpose**: Deploy code, start servers, enable subdomains, verify health.

**Skip**: Only if managed-only.

**Standard mode (container)**:
```
Phase 1 — Deploy Dev:
  1. zerops_deploy targetService={dev}     ← blocks until build completes
  2. Start server manually via SSH          ← dev has zsc noop
  3. zerops_subdomain action="enable" serviceHostname={dev}
  4. zerops_verify serviceHostname={dev}

Phase 2 — Deploy Stage (after dev healthy):
  5. Write stage entry in zerops.yaml (real start, healthCheck, deployFiles=build output)
  6. zerops_deploy sourceService={dev} targetService={stage}
  7. zerops_manage action="connect-storage" (if shared-storage)
  8. zerops_subdomain action="enable" serviceHostname={stage}
  9. zerops_verify serviceHostname={stage}

Phase 3 — Cross-verify:
  10. zerops_verify (batch, all targets)
```

**Dev mode**: Steps 1-4 only.

**Simple mode**: Deploy → auto-starts (healthCheck) → verify.

**Local (any mode)**: Per-target `zcli push` → verify. No SSH.

**Dev iteration cycle** (code-only changes, container):
1. Edit code on SSHFS mount → changes instant on service
2. Kill previous server, start new via SSH
3. Check startup via TaskOutput
4. Test: `ssh {dev} "curl -s localhost:{port}/health"` | jq .
5. Redeploy ONLY if zerops.yaml changed. Code-only → server restart only.

**Multi-service orchestration** (3+ services): Parent agent spawns sub-agents per service pair in parallel. Each gets mount path, env vars, runtime knowledge. Parent runs final cross-verification.

**Checker**: `VerifyAll()` (HTTP + logs + startup) + subdomain access.

**Verification failure diagnosis**:

| Failed check | Diagnosis | Fix |
|-------------|-----------|-----|
| `service_running`: fail | Service not running | Check deploy status, `zerops_logs severity=error` |
| `startup_detected`: fail | App crashed on start | `zerops_logs severity=error since=5m` |
| `error_logs`: info | Advisory — errors found | Read detail. Infra noise → ignore. App errors → investigate. |
| `http_root`: fail | HTTP server returned 5xx or refused connection (4xx passes — proof-of-life check, not an endpoint contract check) | Check port, binding, start command, runtime logs |

Workflow-specific endpoint-shape checks (`/api/health`, `/status`, Laravel `/up`, etc.) are NOT in `zerops_verify` — their paths are framework-dependent. Verify those paths with explicit `curl` commands in the workflow that knows them (bootstrap's `/status` curl, recipe's `feature-sweep-dev` sub-step iterating `plan.Features`).

### 2.7 Step 5: Close

**Purpose**: Write final evidence file. Bootstrap is done.

**On completion** (Active→false):
1. Write final ServiceMeta per runtime target:
   - `BootstrappedAt` = today's date
   - `DeployStrategy` = **empty** (NEVER set during bootstrap)
   - Container: hostname = devHostname
   - Local + standard: hostname = stageHostname (inverted)
2. Append reflog to CLAUDE.md.
3. Delete session, unregister.
4. Return completion message: service list with modes. NO strategy prompt.

**Bootstrap is done. Service is in evidence with verification server deployed. No application code written.**

**Natural transition**: If the user's intent requires application development (e.g., "create an app for X"), the agent should immediately start develop flow (§4) to implement the actual application. Bootstrap proved the infrastructure works; develop flow is where the real code gets written.

### 2.8 Fast Paths — Managed-Only and Pure-Adoption

`validateSkip` allows `generate`, `deploy`, and `close` to be skipped in
either of two shapes:

1. **Managed-only** — the plan has no runtime targets (`len(Targets)==0`).
   Nothing to generate code for, nothing to deploy. No ServiceMeta is
   written (managed services are API-authoritative).
2. **Pure-adoption** — every runtime target in the plan has
   `IsExisting=true` (`plan.IsAllExisting()`). Generate and deploy are
   skipped because the code and running infrastructure already exist.
   Close is skipped because adoption writes ServiceMeta directly from the
   discover step (see §3.2).

In both shapes the bootstrap walks discover → provision → SKIP generate →
SKIP deploy → SKIP close. Mixed plans (some new runtime targets + some
adopted) follow the full flow in §2.3–§2.7 — only the fully-uniform
shapes above qualify for the fast path.

### 2.9 Mode Behavior Matrix

| Aspect | Standard | Dev | Simple |
|--------|----------|-----|--------|
| Services | `{name}dev` + `{name}stage` + managed | `{name}dev` + managed | `{name}` + managed |
| Mounts (container) | dev only | dev only | service |
| zerops.yaml start (dev) | `zsc noop --silent` | `zsc noop --silent` | real command |
| zerops.yaml start (stage) | real command | N/A | N/A |
| healthCheck | none (dev) / required (stage) | none | required |
| deployFiles | `[.]` (dev) / build output (stage) | `[.]` | `[.]` |
| Server start (container) | SSH manual (dev) / auto (stage) | SSH manual | auto |
| Deploy sequence | dev → verify → stage → verify | dev → verify | deploy → verify |
| Subdomain enable | both dev + stage | dev only | service only |
| PHP runtimes | omit `start:` entirely | omit `start:` | omit `start:` |

---

## 3. Adoption Flow

Adoption registers an existing unmanaged service into ZCP management. The outcome is the same as bootstrap: a ServiceMeta with mode and BootstrappedAt.

### 3.1 When Adoption Applies

- Project has runtime services with `managedByZCP=false` (no complete ServiceMeta).
- Init instructions label these as "needs ZCP adoption."
- `zerops_workflow action="route"` offers adoption.

### 3.2 What Happens

Adoption is a simplified process:

1. **Discover**: Agent classifies the existing service. Determines mode from hostname patterns (dev+stage → standard, dev-only → dev, no suffix → simple).
2. **Verify**: Confirm the service is running and healthy (`zerops_verify`).
3. **Write evidence**: Create ServiceMeta with:
   - Hostname, Mode, StageHostname (if standard)
   - Environment (container/local)
   - `BootstrapSession` = empty (not created by bootstrap — the
     adoption marker; combined with `IsComplete()` this makes
     `IsAdopted()` return true, see §1.1 and invariant E7)
   - `BootstrappedAt` = today's date
   - `DeployStrategy` = empty

No import, no code generation, no deploy. The service already exists and runs.

### 3.3 Mixed Adoption + New

When the user wants to adopt existing services AND create new ones, this goes through bootstrap (§2) with `isExisting: true` on adopted targets. Each target follows its path:
- New targets: full bootstrap (import, generate, deploy)
- Existing targets: verify-only, write meta

**Pure-adoption fast path**: When *every* runtime target in the plan has
`IsExisting=true`, bootstrap routes through the fast path in §2.8 —
generate, deploy, and close are all skippable. Mixed plans (any new
runtime target) follow the full flow regardless of adopted targets.

### 3.4 Outcome

ServiceMeta identical in structure to bootstrap output. The service is now "managed by ZCP" and can enter develop flow.

---

## 4. Develop Flow

Develop flow is the **development lifecycle** for any service under ZCP management. It is the MANDATORY wrapper for any code work on runtime services — implementing features, fixing bugs, changing config. No code change should happen outside of this flow.

```mermaid
flowchart TD
    Start([Agent wants to work<br/>with service code]) --> ReadMeta["Read ServiceMeta<br/>from evidence file"]
    ReadMeta --> CheckEvidence{ServiceMeta<br/>exists?}
    
    CheckEvidence -->|No| NeedBootstrap["Service not in evidence.<br/>Run bootstrap or adoption first."]
    CheckEvidence -->|Yes| StartFlow

    StartFlow["START PHASE<br/>──────────────<br/>Provide knowledge<br/>Report strategy status"] --> Work

    Work["WORK PHASE<br/>──────────────<br/>Agent edits code<br/>ZCP stays out of the way"] --> PreDeploy

    PreDeploy["PRE-DEPLOY<br/>──────────────<br/>Read strategy from meta"] --> StratCheck

    StratCheck{Strategy<br/>in meta?}
    StratCheck -->|"Empty"| AskUser["Discuss with user:<br/>push-dev / push-git / manual<br/>→ action='strategy'"]
    AskUser --> SetStrategy["Write strategy to meta"]
    SetStrategy --> ExecuteDeploy
    
    StratCheck -->|"Set"| ExecuteDeploy

    ExecuteDeploy["DEPLOY<br/>──────────────<br/>Execute per strategy<br/>(read fresh from meta)"]
    ExecuteDeploy --> Verify

    Verify["VERIFY<br/>──────────────<br/>zerops_verify per target"]
    Verify --> Done([Deploy complete])
    
    Verify -->|"Failed"| Iterate{Iteration<br/>< max?}
    Iterate -->|Yes| Work
    Iterate -->|No| UserHelp["Present to user"]

    style StartFlow fill:#e8f4fd,stroke:#2196F3
    style Work fill:#fff3e0,stroke:#FF9800
    style ExecuteDeploy fill:#fce4ec,stroke:#E91E63
```

### 4.1 When Develop Flow Starts

Develop flow MUST start for ANY work on runtime service code:

- **After bootstrap**: Service has only a verification server — develop flow implements the user's actual application
- **Implementing features**: User said "add photo upload" → develop flow
- **Bug fixes**: "Login doesn't work" → develop flow
- **Config changes**: "Change the port" → develop flow
- **Any code modification**: If it touches a runtime service's files → develop flow

Develop flow discovers what code exists on the service (verification server from bootstrap or existing application) and acts accordingly. Bootstrap created infrastructure; develop flow is for all development.

**Agent MUST NOT** edit runtime service code outside of develop flow. The flow ensures the agent has platform knowledge, knows the deploy strategy, and deploys + verifies at the end.

### 4.2 Start Phase — Strategy from Meta

At the start of develop flow, the system reads ServiceMeta and informs the agent about strategy status. This is **informational, not blocking**.

**If strategy is NOT set** (DeployStrategy empty in meta):
> "No deploy strategy is configured for this service. Proceed with your code changes. Before deploying, discuss with the user how they want to deploy (push-dev / push-git / manual)."

**If strategy IS set** (DeployStrategy in meta has value):
> "Deploy strategy: {strategy}. Will deploy according to this strategy. If you want to change it, let me know at any time."

**Key principle**: Strategy never blocks the start of work. Agent can always begin editing code immediately. Strategy is read from meta — if user changed it since last deploy, the new value is used automatically.

### 4.3 Deploy Strategies

Three strategies determine how code gets to Zerops:

#### push-dev
- **Container**: `zerops_deploy targetService="{hostname}"` — SSH self-deploy. Blocks until build completes.
- **Local**: `zcli push` — pushes code from local machine.
- **After deploy**: Manual server start via SSH for dev (zsc noop). Stage and simple auto-start.
- **Good for**: Quick iterations, prototyping, direct control.

#### push-git
- **Mechanism**: Commit code, push to external git remote (GitHub/GitLab).
- **First time**: Requires setup — GIT_TOKEN, .netrc, remote URL configuration.
- **Command**: `zerops_deploy targetService="{hostname}" strategy="git-push" remoteUrl="{url}"`
- **Subsequent**: `zerops_deploy targetService="{hostname}" strategy="git-push"`
- **Optional CI/CD**: GitHub Actions workflow or webhook for automatic deploys on push.
- **Good for**: Team development, CI/CD pipelines, code in git.

#### manual
- **Mechanism**: User manages deployments themselves.
- **Agent role**: Tell user what needs to happen. User executes.
- **Good for**: Experienced users, external CI/CD systems.

### 4.4 Strategy Setting and Changing

```
zerops_workflow action="strategy" strategies={"appdev": "push-dev"}
```

- Validates: must be `push-dev`, `push-git`, or `manual`.
- Writes to ServiceMeta.DeployStrategy.
- Can be called at ANY time — before, during, or between develop flows.
- Subsequent develop flow reads the updated value from meta.
- Returns guidance for the chosen strategy.

**Strategy is always read from meta, never cached in deploy session.** This means:
- User changes strategy between deploys → next deploy uses new strategy automatically.
- User changes strategy mid-flow → pre-deploy phase reads the current value.
- No "session strategy" concept — meta is the single source of truth.

### 4.5 Pre-Deploy Phase

Before actual deployment, the system:
1. Reads current strategy from ServiceMeta (fresh read, not cached).
2. If empty → agent must discuss with user and set via `action="strategy"`.
3. If set → proceed with deployment.

**Deploy checker** (`checkDeployPrepare`):
- zerops.yaml exists and parses.
- Setup entries match targets (tries role name: "dev"/"stage"/"prod", then hostname).
- deployFiles paths exist on filesystem.
- Env var references (`${hostname_varName}`) re-discovered from API and validated.

### 4.6 Mode-Specific Deploy Behavior

**Standard mode** (container):
1. Deploy dev → manual start → verify dev
2. Write stage entry (real start, healthCheck, deployFiles=build output)
3. Deploy stage (from dev) → auto-starts → verify stage

**Standard mode** (local):
1. `zcli push` per target → verify

**Dev mode**: Dev deploy + start + verify only.

**Simple mode**: Deploy → auto-starts → verify.

**Deploy result checker** (`checkDeployResult`):
- `RUNNING`/`ACTIVE` → pass
- `READY_TO_DEPLOY` → fail: "container didn't start — check start command, ports, env vars"
- Other status → fail: "check zerops_logs severity=error"
- Subdomain access check for services with ports

### 4.7 Iteration on Failure

When deploy fails, the agent can iterate. Escalating guidance tiers live in `internal/workflow/iteration_delta.go` and are shared by bootstrap and develop deploys:

| Iteration | Tier | Guidance |
|---|---|---|
| 1-2 | DIAGNOSE | `zerops_logs severity=error since=5m`, fix the specific error, redeploy + verify. |
| 3-4 | SYSTEMATIC | "PREVIOUS FIXES FAILED" — walk the env-vars / bind-address / deployFiles / ports / start checklist. |
| 5 | STOP | Present to user: what was tried, current error, "should I continue or will you debug manually?" — do NOT attempt another fix. |

`defaultMaxIterations = 5` caps the session, so the STOP tier fires exactly once and then the session closes with `CloseReason=iteration-cap`. Continuing requires a fresh `zerops_workflow action=start`, making continuation an explicit user decision (fixes defect D5 in `plans/instruction-delivery-rewrite.md`).

### 4.8 Operational Details

- `zerops_deploy` blocks until build completes. Returns DEPLOYED or BUILD_FAILED.
- `zerops_subdomain action="enable"` must be called once after first deploy of new service. Persists across re-deploys.
- Dev server start via SSH needed after every deploy (container, dynamic runtimes only). NOT for PHP/nginx/static (implicit-webserver auto-starts).
- Stage entry written AFTER dev verified (standard mode).
- `zerops_deploy sourceService={dev} targetService={stage}` for cross-deploy.
- `zerops_manage action="connect-storage"` after first stage deploy (if shared-storage).

---

## 5. Environment Differences

Both environments follow the same flows but with different mechanisms.

### 5.1 Container Mode

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

- **Detection**: `serviceId` env var present.
- **Code access**: SSHFS mounts at `/var/www/{hostname}/`.
- **Deploy (push-dev)**: SSH into service, git init + zcli push from inside.
- **Deploy (push-git)**: SSH into service, git commit + push to remote.
- **Server start**: Manual via SSH for dev (zsc noop). Auto for stage/simple.
- **Commands**: `ssh {hostname} "cd /var/www && {command}"`.
- **Mount tool**: Available.
- **ServiceMeta hostname**: devHostname (standard), hostname (dev/simple).

### 5.2 Local Mode

```
┌─────────────────────────────────────┐
│  Developer's machine                │
│                                     │
│  Code in working directory          │
│  zerops.yaml at repository root     │
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

- **Detection**: `serviceId` env var absent.
- **Code access**: Working directory.
- **Deploy (push-dev)**: `zcli push` from local machine.
- **Deploy (push-git)**: git commit + push from local.
- **Server start**: Real start command in zerops.yaml. healthCheck always.
- **Mount tool**: Not available.
- **ServiceMeta hostname**: stageHostname for standard (inverted), hostname for dev/simple.

### 5.3 Guidance Adaptation

Environment-specific guidance is handled at the atom level, not in conductor code: atoms tagged `environments: [local]` cover local-only guidance (e.g. `bootstrap-generate-local`, `bootstrap-deploy-local`, `develop-local-workflow`), atoms tagged `environments: [container]` cover container-only guidance, and atoms with an empty `environments` axis apply to both. The synthesizer picks the right combination per turn — no hand-coded addendum/replacement logic in Go. See `docs/spec-knowledge-distribution.md` for the authoring model.

---

## 6. Workflow Routing

`zerops_workflow action="route"` returns prioritized offerings based on project state.

**Priority ordering**:
1. (P1) Incomplete bootstrap → resume/start hint
2. (P1) Unmanaged runtimes → adoption offering
3. (P1-P2) Managed services with push-dev/push-git → deploy offering
4. (P1-P2) Managed services with push-git → CI/CD setup hint
5. (P3) Add new services → bootstrap hint
6. (P4-P5) Utilities → recipe, scale

Manual strategy → no deploy offering (user manages directly).

Route returns **facts, not recommendations**.

---

## 7. Session Management

ZCP has **two independent session kinds**, owned by different layers and
governed by different lifetimes. Full philosophical treatment in
`spec-work-session.md`.

### 7.1 Infrastructure Sessions (Bootstrap / Recipe)

Stored at `.zcp/state/sessions/{id}.json`:
```
WorkflowState {
  Version    "1"
  SessionID  16-hex random
  PID        process owner
  ProjectID  Zerops project
  Workflow   "bootstrap" | "recipe"
  Iteration  counter
  Intent     user's goal
  CreatedAt  RFC3339
  UpdatedAt  RFC3339
  Bootstrap  *BootstrapState
  Recipe     *RecipeState
}
```

Lifetime = workflow duration. Survives process restart via registry
claim-on-boot (dead-PID auto-recovery).

### 7.2 Work Sessions (Develop)

Stored at `.zcp/state/work/{pid}.json`, one per process:
```
WorkSession {
  Version         "1"
  PID             int
  ProjectID       string
  Environment     "container" | "local"
  Intent          string
  Services        []hostname
  CreatedAt       RFC3339
  LastActivityAt  RFC3339
  Deploys         map[hostname][]DeployAttempt  // capped at 10
  Verifies        map[hostname][]VerifyAttempt  // capped at 10
  ClosedAt        RFC3339 (empty = open)
  CloseReason     "explicit" | "auto-complete"
}
```

Lifetime = one LLM task per process. Does **not** survive restart — code
work survives in git and on disk. Dead-PID work sessions are pruned on
engine boot, never claimed.

### 7.3 Registry

`.zcp/state/registry.json` — tracks both infrastructure sessions
(`SessionID` = 16-hex) and work sessions (`SessionID` = `work-{pid}`),
with file lock via `.registry.lock`. Auto-prunes dead PIDs and sessions
>24h old on new-session creation. Replaces the legacy `active_session`
side-channel file.

### 7.4 Actions

| Action | Applies to | Effect |
|--------|-----------|--------|
| `start workflow=bootstrap\|recipe` | infra | Creates infra session |
| `start workflow=develop` | work | Creates Work Session for current PID |
| `complete step=...` | infra | Advances infra step |
| `iterate` | infra | Resets generate+deploy (bootstrap) |
| `status` | both | Returns Work Session if present, else infra |
| `close workflow=develop` | work | Closes Work Session, deletes file |
| `reset` | both | Deletes active session(s) |
| `resume sessionId=...` | infra | Claims dead-PID infra session |

Develop has **no** `iterate` or `complete step` — it is stateless by
design; deploy/verify attempts accumulate in the Work Session for
visibility.

---

## 8. Invariants

### Evidence

| ID | Invariant |
|----|-----------|
| E1 | Every managed runtime service has a ServiceMeta with Mode and BootstrappedAt |
| E2 | Bootstrap creates ServiceMeta with empty DeployStrategy |
| E3 | Adoption creates ServiceMeta with empty BootstrapSession (marker for the adoption path) |
| E4 | IsComplete() = BootstrappedAt is non-empty |
| E5 | Partial meta (no BootstrappedAt) signals bootstrap in-progress |
| E6 | Only runtime services get ServiceMeta — managed services are API-authoritative |
| E7 | IsAdopted() = BootstrapSession is empty AND IsComplete() — disambiguates adopted metas from orphan incomplete metas |

### Bootstrap

| ID | Invariant |
|----|-----------|
| B1 | 5 steps in strict order: discover → provision → generate → deploy → close |
| B2 | discover/provision always mandatory |
| B3 | generate/deploy/close skippable when the plan has no runtime targets (managed-only) OR every runtime target has IsExisting=true (pure-adoption); see §2.8 |
| B4 | Attestation ≥ 10 chars on completion |
| B5 | Checker failure blocks step advancement |
| B6 | Per-service exclusivity via hostname lock |
| B7 | Bootstrap does NOT set deploy strategy |
| B8 | Non-discover steps require plan from discover step (defense-in-depth) |
| B9 | Generate writes an infrastructure verification server (hello-world, under 50 lines), NOT application logic |
| B10 | After bootstrap, agent starts develop flow for all application development |

### Develop Flow / Work Session

| ID | Invariant |
|----|-----------|
| D1 | Develop flow requires ServiceMeta with BootstrappedAt |
| D0 | ALL code changes to runtime services MUST go through develop flow |
| D2 | Strategy is informational at start, not a gate |
| D3 | Strategy read from meta at deploy time, never cached in Work Session |
| D4 | Strategy resolved before actual deployment (within flow) |
| D5 | Strategy can be changed at any time via action="strategy" |
| D6 | push-git includes optional CI/CD setup |
| D7 | manual strategy: agent informs, user executes |
| D8 | Deploy checkers validate platform integration, not application correctness |
| D9 | Checker failure blocks step advancement — agent receives CheckResult with details |
| D10 | Mixed strategies across targets in single deploy session are rejected |
| W1 | Work Session is per-PID, stored at `.zcp/state/work/{pid}.json` |
| W2 | Work Session stores only intent + scope + deploy/verify history — never strategy, mode, or service status (those are read fresh) |
| W3 | Work Session does NOT survive process restart; dead-PID files are pruned, never claimed |
| W4 | Deploy and verify tools append to Work Session as side-effects, capped at 10 entries per hostname |
| W5 | Work Session auto-closes when every service in scope has a succeeded deploy AND a passed verify |
| W6 | Work Session is advisory (Lifecycle Status in system prompt); it does not gate tool calls |

### Strategy

| ID | Invariant |
|----|-----------|
| S1 | Three values: push-dev, push-git, manual |
| S2 | Never auto-assigned |
| S3 | Set via explicit action="strategy", writes to ServiceMeta |
| S4 | Develop flow always reads fresh from meta |

### Operational

| ID | Invariant |
|----|-----------|
| O1 | zerops_deploy blocks until build completes |
| O2 | zerops_import blocks until all processes complete |
| O3 | zerops_subdomain called once after first deploy (persists) |
| O4 | Dev server started manually via SSH after every deploy (container, dynamic runtimes) |
| O5 | Stage entry written AFTER dev verified (standard mode) |
| O6 | Stage deployFiles = build output, NOT [.] |

### Pipeline

| ID | Invariant |
|----|-----------|
| P1 | `ComputeEnvelope` is the single entry point for state gathering — no tool handler reads `.zcp/state/` or the platform API directly for envelope fields. |
| P2 | `BuildPlan(env)` is pure: no I/O, no time, no randomness. Same envelope JSON → same Plan. |
| P3 | `Synthesize(env, corpus)` is pure under the same contract as P2. Same envelope JSON → byte-identical composed guidance. |
| P4 | Every workflow-aware tool response is a `Response{Envelope, Guidance, Plan}` triple. No free-form Next strings anywhere in a response. |
| P5 | `Plan.Primary` is never zero. If dispatch finds no branch, an empty Plan is returned and treated as a construction bug — callers MUST error, not silently continue. |
| P6 | Each atom declares a non-empty `phases` axis. Atoms with empty phases are rejected at corpus load (`LoadAtomCorpus`). |
| P7 | Unknown `{placeholder}` tokens in atom bodies are build-time errors — none leak to the LLM as literal braces. |
| P8 | `cicd-active` and `export-active` are stateless phases: they synthesize guidance from the atom corpus and return without touching session state. |
| P9 | Recipe authoring (`workflow=recipe`) uses its own section-parser pipeline (`recipe_block_parser.go`, `recipe_decisions.go`, …), NOT the atom synthesizer. The pipelines are intentionally independent. |

---

## §9 Planned Features

### 9.1 Mode Expansion (simple/dev → standard)

**Status**: Planned, not implemented.

**Problem**: A service bootstrapped in simple or dev mode needs to expand to standard (dev+stage). This requires creating a new stage service, updating zerops.yaml with a stage entry, deploying to stage, and updating ServiceMeta.

**Proposed approach**: Bootstrap in expansion mode. The existing service is treated as adopted (`isExisting: true`), and the new stage service is created. Plan example:

```json
{
  "runtime": {
    "devHostname": "app",
    "type": "bun@1.3",
    "isExisting": true,
    "bootstrapMode": "standard",
    "stageHostname": "appstage"
  }
}
```

Bootstrap adoption path handles it: adopted target skips generate+deploy (code already exists), new stage gets created and deployed via cross-deploy from dev. ServiceMeta updates from simple/dev → standard with new stageHostname.

**Why bootstrap, not deploy**: Creating services is infrastructure work. Bootstrap already handles service creation, ServiceMeta writes, and import.yaml generation. Develop flow handles code changes, not infrastructure topology changes.

---

## Appendix A: Recovery Patterns

| Symptom | Cause | Fix |
|---------|-------|-----|
| Build FAILED: "command not found" | Wrong buildCommands | Check runtime knowledge |
| Build FAILED: "module not found" | Missing deps | Add to buildCommands |
| App crash: "EADDRINUSE" | Port conflict | Match port to zerops.yaml |
| App crash: "connection refused" | Wrong env var | Check envVariables vs discovered |
| HTTP 502 | Subdomain not active | `zerops_subdomain action="enable"` |
| Empty response | Not on 0.0.0.0 | Fix binding |
| READY_TO_DEPLOY after deploy | Start failed | Check start command, runtime version |
