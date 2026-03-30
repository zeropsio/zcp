# ZCP — Zerops Control Plane

MCP server that gives an LLM full control over a Zerops project. Runs as a `zcp@1` service inside the project it manages.

## Integration model

```
User ←→ Claude Code (terminal in code-server) ←→ ZCP (MCP over STDIO) ←→ Zerops API
                                                                        ←→ sibling services (SSH/SSHFS over VXLAN)
```

The user opens code-server on the `zcp` service subdomain. Claude Code is preconfigured with ZCP as its MCP server. The user describes what they want, the LLM figures out what to do, calls ZCP tools to make it happen.

ZCP authenticates once at startup (env var or zcli token), discovers which project it's in, and exposes everything as MCP tools. The LLM sees a system prompt with the environment concept, current service classification, and available workflows — the LLM decides what to do.

## What the LLM can do

Through ZCP tools, the LLM can:

- **Bootstrap a full stack** — from "I need a Node.js app with PostgreSQL" to running services with health checks, in one conversation
- **Deploy code** — writes files via SSHFS mount, triggers build pipeline via SSH push
- **Debug** — read logs, check events, verify service health
- **Scale** — adjust CPU, RAM, disk, container count
- **Configure** — manage env vars, subdomains, shared storage connections
- **Monitor** — discover services, check statuses

---

## Architecture

```
cmd/zcp/main.go → internal/server  → MCP tools  → internal/ops      → internal/platform → Zerops API
                                                 → internal/workflow  (orchestration + routing)
                                                 → internal/knowledge (BM25 search)
                                                 → internal/auth      (token resolution)
```

| Package | Responsibility |
|---------|---------------|
| `cmd/zcp` | Entrypoint, STDIO server |
| `internal/server` | MCP server setup, tool registration, system prompt |
| `internal/tools` | MCP tool handlers (15 tools) |
| `internal/ops` | Business logic — deploy, verify, import, scale |
| `internal/workflow` | Bootstrap/deploy conductors, personalized guidance, checkers, session state, router |
| `internal/platform` | Zerops API client, types, error codes |
| `internal/auth` | Token resolution (env var / zcli), project discovery |
| `internal/knowledge` | BM25 search, embedded docs + recipes, session-aware briefings |
| `internal/content` | Embedded workflow guides (bootstrap.md, deploy.md) |

---

## Flow routing

Every conversation starts with ZCP injecting a system prompt built from four layers:

1. **Base instructions** — workflow-first rules (always start a session before writing config)
2. **Workflow hint** — active sessions from registry (resume prompts)
3. **Environment concept** — container vs local: where code lives, how mounts work, deploy = rebuild
4. **Project summary + Router** — factual state (services, statuses, available workflows)

### Router

The router is a pure function that returns **factual data** — no recommendations, no intent matching. The LLM decides what to do:

```
Route(RouterInput) → []FlowOffering{Workflow, Priority, Hint}
```

| Service classification | Primary | Secondary |
|----------------------|---------|-----------|
| **Empty project** (no runtime services) | bootstrap (p1) | — |
| **All managed** (all runtimes have ZCP state) | strategy-based deploy (p1) | bootstrap (p2) |
| **Unmanaged runtimes exist** (services without ZCP state) | strategy-based or debug (p1-2) | bootstrap (p2) |

Strategy-based routing reads `ServiceMeta.DeployStrategy` persisted from prior bootstraps. Utility offerings (debug, configure, zerops_scale) are always appended at priority 5. Scale is a direct tool — no workflow needed. Stale metas (hostnames deleted from API) are filtered out automatically.

---

## Workflow types

### Immediate (stateless)

**debug**, **scale**, **configure** — return guidance markdown, no session tracking.

### Orchestrated (session-tracked)

**bootstrap** and **deploy** — create a session with state persistence, checker-based validation, and iteration support.

---

## Bootstrap workflow

Bootstrap is the core flow. It takes a user request ("deploy a Go API with Postgres") and guides the LLM through **5 sequential steps** with hard checks and an iteration loop.

### The 5 steps

```
┌──────────┐   ┌───────────┐   ┌──────────┐   ┌────────┐   ┌───────┐
│ DISCOVER │──▶│ PROVISION │──▶│ GENERATE │──▶│ DEPLOY │──▶│ CLOSE │
│  (fixed) │   │  (fixed)  │   │(creative)│   │(branch)│   │(fixed)│
└──────────┘   └───────────┘   └──────────┘   └────────┘   └───────┘
                                (skippable)    (skippable)   (skip.)
```

| Step | What happens | Hard check |
|------|-------------|------------|
| **discover** | Classify services (via `managedByZCP`/`isInfrastructure` fields), plan services, validate types against live catalog, submit plan | — |
| **provision** | Generate import.yml, create services via API, mount dev filesystems via SSHFS, discover env vars from managed services | All services exist with expected status; managed deps have env vars |
| **generate** | Write zerops.yml + app code to mounted dev filesystem using real env vars from provision | zerops.yml valid, hostname match, env var refs valid |
| **deploy** | Deploy dev and stage services, enable subdomains, verify health, iteration loop (fix → redeploy) | All runtimes RUNNING; subdomain access enabled; health checks pass |
| **close** | Administrative closure — writes ServiceMeta files, presents strategy selection | — |

**generate**, **deploy**, and **close** are skippable — but only for managed-only projects (no runtime services). Strategy selection happens after close via `action="strategy"`.

### Step categories

- **fixed** — deterministic, always the same sequence of tool calls
- **creative** — LLM generates code; requires judgment and knowledge
- **branching** — per-service iteration with retry loops

### Plan and service model

The **discover** step produces a **plan** that drives all subsequent steps:

```
ServicePlan
  └─ Targets[]
       ├─ Runtime
       │    ├─ DevHostname      "appdev"
       │    ├─ Type             "nodejs@22"
       │    ├─ BootstrapMode    "standard" | "dev" | "simple" (empty → standard)
       │    └─ StageHostname()  → "appstage" (auto-derived for standard mode)
       └─ Dependencies[]
            ├─ Hostname       "db"
            ├─ Type           "postgresql@16"
            ├─ Mode           "NON_HA" (auto-defaulted)
            └─ Resolution     "CREATE" | "EXISTS" | "SHARED"
```

**Standard mode** (default): every runtime gets a dev+stage pair. Dev uses `deployFiles: [.]` for fast iteration. Stage gets real build output.

**Dev mode**: single dev service, no stage. For prototyping and quick iterations.

**Simple mode**: single service with real start command + healthCheck. Auto-starts after deploy.

### Hard checks

Before a step can complete, the engine runs a **StepChecker** — a function that queries the Zerops API to verify the step's postconditions:

```
LLM calls: zerops_workflow action="complete" step="provision" attestation="..."
  │
  ├─ Engine runs checkProvision()
  │    ├─ dev runtime RUNNING?
  │    ├─ stage runtime NEW or READY_TO_DEPLOY?
  │    ├─ dependencies RUNNING?
  │    └─ managed deps have env vars?
  │
  ├─ All pass → step completes, advance to next
  └─ Any fail → return CheckResult (not error), LLM can fix and retry
```

This prevents the LLM from advancing past a broken step. The check result is returned in the response so the LLM knows exactly what failed.

### Iteration loop

When deploy fails, the LLM iterates:

```
deploy → FAIL → fix code on mount → redeploy → re-verify
                                     (max 10 attempts, configurable via ZCP_MAX_ITERATIONS)
```

Each iteration resets generate+deploy steps and increments the counter. Escalating diagnostic guidance is delivered on each retry.

### Guidance delivery

Bootstrap and deploy use different guidance models:

- **Bootstrap** = creative workflow — injects full knowledge (runtime briefings, schema, env vars) because the agent is creating configuration from scratch.
- **Deploy** = operational workflow — injects compact personalized guidance (15-55 lines) with knowledge pointers. Agent pulls knowledge on demand via `zerops_knowledge`.
- **On-demand knowledge** = session-aware. `zerops_knowledge` auto-detects the active workflow mode and filters runtime guides (Dev/Prod patterns) and recipes (mode-adapted headers) accordingly. Agent can override with explicit `mode` parameter.

Deploy guidance is assembled from `DeployState` + `ServiceMeta` — the agent sees their actual hostnames, mode-specific workflow steps, and strategy commands. Not generic templates.

See `docs/spec-guidance-philosophy.md` for the full guidance delivery specification.

---

## Post-bootstrap: ServiceMeta persistence

Bootstrap writes per-service metadata at two points:

| When | What |
|------|------|
| After provision | Partial meta (hostname, mode, stage pairing — no `BootstrappedAt`) |
| After close step | Complete meta (`BootstrappedAt` set — marks bootstrap as finished) |

Strategy is set separately via `action="strategy"` after bootstrap (never auto-assigned).

Stored at `{stateDir}/services/{hostname}.json`. These metas persist across conversations — the deploy workflow reads them on start for mode, strategy, and preflight validation.

---

## Deploy mechanics

ZCP sits on the same VXLAN network as all project services. It deploys via SSH:

1. SSHFS mount gives filesystem access to the target container
2. LLM writes code + zerops.yml directly to the mount path
3. `zerops_deploy` SSHes into the target, initializes git, runs `zcli push`
4. Zerops build pipeline picks it up from there

Dev services get source-deployed (`deployFiles: [.]`). Stage services get proper build output. Dev uses `startWithoutCode: true` so the container is already running before the first deploy.

## Knowledge system

Platform knowledge is compiled into the binary. The LLM queries it before generating any configuration:

- **Briefings** — stack-specific rules (e.g., "Node.js must bind 0.0.0.0, use these env var patterns for PostgreSQL wiring")
- **Recipes** — complete framework configs (Laravel, Next.js, Django, etc.) with zerops.yml + import.yml
- **Infrastructure scope** — full import.yml and zerops.yml schema reference
- **Text search** — BM25 search across all embedded docs

This prevents the LLM from guessing Zerops-specific syntax. It reads the rules, then generates config.

## Session persistence

All workflow state persists locally at `.zcp/state/`:

| File | Purpose |
|------|---------|
| `sessions/{id}.json` | Session state: bootstrap/deploy steps, plan, env vars, iteration |
| `services/{hostname}.json` | Per-service metadata (mode, strategy, stage pairing) |
| `registry.json` | Active session tracking with PID-based ownership |

Sessions survive process restarts. The MCP system prompt shows the active session state so the LLM can resume where it left off. Dead sessions (stale PID) can be taken over via `zerops_workflow action="resume"`.

---

## Development

```bash
go test ./... -count=1 -short    # All tests, fast
go test ./... -count=1 -race     # All tests with race detection
go build -o bin/zcp ./cmd/zcp    # Build
make lint-fast                   # Lint (~3s)
```

E2E tests need a real Zerops project: `go test ./e2e/ -tags e2e` (requires `ZCP_API_KEY` or zcli login).

## Release

```bash
make release        # Minor bump (v2.62.0 → v2.63.0)
make release-patch  # Patch bump (v2.62.0 → v2.62.1)
```

Both run tests before tagging. If tests fail, the release is aborted. Requires a clean worktree (no uncommitted changes to tracked files; untracked files are ignored).
