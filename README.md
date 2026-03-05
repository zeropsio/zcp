# ZCP — Zerops Control Plane

MCP server that gives an LLM full control over a Zerops project. Runs as a `zcp@1` service inside the project it manages.

## Integration model

```
User ←→ Claude Code (terminal in code-server) ←→ ZCP (MCP over STDIO) ←→ Zerops API
                                                                        ←→ sibling services (SSH/SSHFS over VXLAN)
```

The user opens code-server on the `zcp` service subdomain. Claude Code is preconfigured with ZCP as its MCP server. The user describes what they want, the LLM figures out what to do, calls ZCP tools to make it happen.

ZCP authenticates once at startup (env var or zcli token), discovers which project it's in, and exposes everything as MCP tools. The LLM sees a system prompt with the current project state and a routing table that tells it which workflow to start.

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
                                                 → internal/workflow  (orchestration)
                                                 → internal/knowledge (BM25 search)
                                                 → internal/auth      (token resolution)
```

| Package | Responsibility |
|---------|---------------|
| `cmd/zcp` | Entrypoint, STDIO server |
| `internal/server` | MCP server setup, tool registration, system prompt |
| `internal/tools` | MCP tool handlers (15 tools) |
| `internal/ops` | Business logic — deploy, verify, import, scale |
| `internal/workflow` | Bootstrap orchestration, session state, evidence |
| `internal/platform` | Zerops API client, types, error codes |
| `internal/auth` | Token resolution (env var / zcli), project discovery |
| `internal/knowledge` | BM25 search engine, embedded docs + recipes |
| `internal/content` | Embedded workflow guides (bootstrap.md, deploy.md) |

---

## Bootstrap workflow

Bootstrap is the core flow. It takes a user request ("deploy a Go API with Postgres") and guides the LLM through **5 sequential steps** with hard checks, evidence gates, and an iteration loop.

### The 5 steps

```
┌─────────┐    ┌───────────┐    ┌──────────┐    ┌────────┐    ┌────────┐
│ DISCOVER │───→│ PROVISION │───→│ GENERATE │───→│ DEPLOY │───→│ VERIFY │
└─────────┘    └───────────┘    └──────────┘    └────────┘    └────────┘
   fixed          fixed          creative*       branching*      fixed
                                 (skippable)     (skippable)
```

| Step | What happens | Hard check |
|------|-------------|------------|
| **discover** | Inspect project state, classify (FRESH / CONFORMANT / NON_CONFORMANT), plan services, validate types against live catalog, load platform knowledge, submit plan | — |
| **provision** | Generate import.yml, create services via API, mount dev filesystems via SSHFS, discover env vars from managed services | All services exist with expected status; managed deps have env vars |
| **generate** | Write zerops.yml + app code to mounted dev filesystem using real env vars from provision | — |
| **deploy** | Deploy dev and stage services, enable subdomains, iteration loop (fix → redeploy, max 3) | All runtimes RUNNING; subdomain access enabled |
| **verify** | Independent health verification of all plan targets, present final report with URLs | All plan targets healthy (via `ops.VerifyAll`) |

**generate** and **deploy** are skippable — but only for managed-only projects (no runtime services). The engine enforces this via conditional skip validation.

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
       │    ├─ DevHostname    "appdev"
       │    ├─ Type           "nodejs@22"
       │    ├─ Simple         false (→ standard mode: dev+stage pairs)
       │    └─ StageHostname()  → "appstage" (auto-derived)
       └─ Dependencies[]
            ├─ Hostname       "db"
            ├─ Type           "postgresql@16"
            ├─ Mode           "NON_HA" (auto-defaulted)
            └─ Resolution     "CREATE" | "EXISTS" | "SHARED"
```

**Standard mode** (default): every runtime gets a dev+stage pair. Dev uses `deployFiles: [.]` for fast iteration. Stage gets real build output.

**Simple mode**: single service, no pair. Used when the user explicitly wants one service.

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

### Evidence and phase gates

The bootstrap maps to a 5-phase workflow (INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE). Phase transitions require **evidence** — attestations stored as JSON files:

| Gate | Evidence required | Built from steps |
|------|-------------------|------------------|
| G0: INIT → DISCOVER | `recipe_review` | discover |
| G1: DISCOVER → DEVELOP | `discovery` | provision |
| G2: DEVELOP → DEPLOY | `dev_verify` | generate, deploy, verify |
| G3: DEPLOY → VERIFY | `deploy_evidence` | deploy |
| G4: VERIFY → DONE | `stage_verify` | verify |

When the final bootstrap step completes, the engine **auto-records evidence** from step attestations and transitions through all phases to DONE. No manual phase management needed during bootstrap.

### Iteration loop

When deploy or verify fails, the LLM iterates:

```
deploy → verify → FAIL → fix code on mount → redeploy → re-verify
                                              (max 3 attempts per service)
```

Each iteration archives the previous evidence and resets the workflow phase to DEVELOP.

### Guidance system

Each step includes embedded guidance from `bootstrap.md`, served via `<section>` tags:

```markdown
<section name="provision">
### Step 1 — Generate import.yml, create services, discover env vars
...detailed instructions, constraints, examples...
</section>
```

The `DetailedGuide` field in the step response gives the LLM step-specific instructions, while `PriorContext` carries the plan and attestations from earlier steps.

---

## Project state routing

At startup, ZCP calls the API to list services and classifies project state:

| State | Condition | Action |
|-------|-----------|--------|
| **FRESH** | No runtime services | Route to bootstrap |
| **CONFORMANT** | Dev+stage pairs detected | Route to deploy (if stack matches) |
| **NON_CONFORMANT** | Services exist without dev/stage pattern | Ask user before changes |

This classification is injected into the MCP system prompt so the LLM knows what to do before its first tool call.

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

All workflow state persists locally:

| File | Purpose |
|------|---------|
| `zcp_state.json` | Current session: phase, bootstrap steps, plan, env vars |
| `evidence/{sessionID}/{type}.json` | Phase gate evidence (attestations) |
| `services/{hostname}.json` | Per-service metadata (historical record) |
| `CLAUDE.md` reflog | Append-only bootstrap history (`<!-- ZEROPS:REFLOG -->` markers) |

Sessions survive process restarts. The MCP system prompt shows the active session state so the LLM can resume where it left off.

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
