# Flow Orchestration: Context-Rot-Resistant Multi-Step Workflows for ZCP

## 1. Problem

ZCP provides 15 MCP tools for managing Zerops infrastructure. Multi-step operations (bootstrap 5 services, deploy and verify, debug and fix) require many sequential tool calls. During long sessions, Claude loses track: forgets to deploy service 4 of 5, skips verification, self-reports success without checking.

The core requirement: **guarantee that every planned step executes and is verified.**

### Why Context Rot Happens

Context rot is structural. Every tool call response consumes context window. After 20-30 calls, earlier results are compacted or lost. Any solution relying on the LLM "remembering" what to do next is broken for long sessions.

### What zcp-orig Proved

The predecessor system's key insight wasn't gates or state machines — it was **decomposition into fresh-context units of work**. A 733-line `spawn-subagents.sh` generated self-contained prompts for each service. Each subagent got:

- A clean context window (no rot)
- A complete, self-contained prompt (no references to prior conversation)
- A numbered task list with verification built into the steps
- Heartbeat files for progress tracking

---

## 2. Design Principle

**Don't fight context rot. Avoid it.**

Decompose multi-step workflows into **pre-defined specialist agents** that each execute in a clean context with a narrow, bounded task.

```
User: "Bootstrap Go API + PostgreSQL + Valkey"
          │
          ▼
┌─────────────────────────────────────────┐
│  Orchestrator (main agent)              │
│  1. zerops_discover (what exists)       │
│  2. zerops_import (create services)     │
│  3. zerops_process (wait for RUNNING)   │
│  4. Spawn: zerops-configure-service     │──→ [api] fresh context, bounded task
│  5. Spawn: zerops-verify-managed        │──→ [db]  fresh context, bounded task
│  6. Spawn: zerops-verify-managed        │──→ [cache] fresh context, bounded task
│  7. zerops_discover (final check)       │
└─────────────────────────────────────────┘
```

The orchestrator makes 7 tool calls — too few to rot. Each specialist agent handles one service in a clean context with embedded verification.

---

## 3. Architecture: Pre-defined Claude Code Agents

### Why Pre-defined Agents, Not Dynamic Prompts

Claude Code natively supports custom agents via `.claude/agents/*.md` files with YAML frontmatter. These are:

- **Versioned in git** — ship with ZCP, evolve with the project
- **Reusable across sessions** — same agent definition for every bootstrap, deploy, debug
- **Self-contained** — system prompt, tool restrictions, model selection, MCP server access, hooks
- **Automatically delegated** — Claude reads the `description` field and delegates matching tasks without explicit invocation
- **Parameterized at spawn time** — the Task tool's `prompt` passes service-specific context

This is fundamentally better than a `zerops_plan` tool generating prompts dynamically because:

1. The agent definitions are **reviewable and testable** — they're files in the repo, not runtime output
2. They can **restrict tools** — a verify agent only gets read-only tools
3. They can have **their own hooks** — PreToolUse validation per agent type
4. They **don't need a new MCP tool** — zero server-side changes for the agent definitions themselves

### Agent Catalog

```
.claude/agents/
├── zerops-configure-service.md   # Configure + deploy a runtime service
├── zerops-verify-managed.md      # Verify a managed service (DB, cache)
├── zerops-deploy-service.md      # Deploy code to an existing service
├── zerops-debug-service.md       # Debug a failing service
└── zerops-orchestrator.md        # Coordinate multi-service operations
```

### Agent Definitions

#### `zerops-configure-service.md`

For runtime services during bootstrap — configure env, enable subdomain, deploy, verify.

```markdown
---
name: zerops-configure-service
description: Configure and deploy a single Zerops runtime service. Use when bootstrapping or reconfiguring a service that needs env vars, subdomain, deploy, and verification.
model: sonnet
maxTurns: 20
mcpServers:
  - zcp
---

# Zerops Service Configuration Agent

You configure and deploy a single Zerops runtime service. You receive the service hostname and context as your task prompt.

## Mandatory Workflow

Execute these steps IN ORDER. Do not skip any step. Each step has a verification tool call that proves success.

| # | Step | Tool | Verification |
|---|------|------|-------------|
| 1 | Discover service state | zerops_discover service="{hostname}" includeEnvs=true | Service exists, note current status |
| 2 | Set environment variables | zerops_env action="set" | zerops_discover includeEnvs=true — vars present |
| 3 | Enable subdomain (if web-facing) | zerops_subdomain action="enable" | zerops_discover — subdomain URL present |
| 4 | Deploy code | zerops_deploy | zerops_events — deploy event shows FINISHED |
| 5 | Check for errors | zerops_logs severity="error" since="5m" | No error-level logs |
| 6 | Final verification | zerops_discover service="{hostname}" | Status is RUNNING |

## Rules

- **Every step requires its verification tool call.** Do not self-report success.
- If a step fails, retry once. If it fails again, report the failure clearly — do not skip to the next step.
- Use `zerops_knowledge` if you need runtime-specific guidance (build commands, zerops.yml structure).
- Environment variable cross-references use underscores: `${service_hostname}`, not `${service-hostname}`.
- Internal service connections use `http://`, never `https://`.
```

#### `zerops-verify-managed.md`

For managed services (PostgreSQL, Valkey, etc.) — just verify they're running.

```markdown
---
name: zerops-verify-managed
description: Verify a managed Zerops service (database, cache, message queue) is running correctly. Use for managed services that don't need code deployment.
model: haiku
maxTurns: 5
mcpServers:
  - zcp
---

# Zerops Managed Service Verification Agent

You verify a single managed Zerops service is operational.

## Mandatory Workflow

| # | Step | Tool | Verification |
|---|------|------|-------------|
| 1 | Check service status | zerops_discover service="{hostname}" | Status is RUNNING |
| 2 | Check for errors | zerops_logs serviceHostname="{hostname}" severity="error" since="1h" | No errors |

## Rules

- Managed services (PostgreSQL, MariaDB, Valkey, Elasticsearch, etc.) don't need deployment.
- Report the service status and any errors found. That's it.
- If the service is not RUNNING, report the issue — do not attempt to fix it.
```

#### `zerops-deploy-service.md`

For deploying code updates to an existing service.

```markdown
---
name: zerops-deploy-service
description: Deploy code to an existing Zerops service and verify the deployment succeeded. Use when pushing code updates, not for initial setup.
model: sonnet
maxTurns: 15
mcpServers:
  - zcp
---

# Zerops Deploy Agent

You deploy code to a single Zerops service and verify it works.

## Mandatory Workflow

| # | Step | Tool | Verification |
|---|------|------|-------------|
| 1 | Verify service exists | zerops_discover service="{hostname}" | Status RUNNING or READY_TO_DEPLOY |
| 2 | Deploy | zerops_deploy | Process ID returned |
| 3 | Monitor deployment | zerops_events serviceHostname="{hostname}" limit=5 | Deploy event FINISHED |
| 4 | Check for errors | zerops_logs serviceHostname="{hostname}" severity="error" since="5m" | No error logs |
| 5 | Verify running | zerops_discover service="{hostname}" | Status is RUNNING |

## Rules

- Ensure `zerops.yml` exists in the working directory before deploying.
- If deploy fails, check `zerops_logs` for the reason before retrying.
- Report the deployment result clearly — URL if subdomain is enabled.
```

#### `zerops-debug-service.md`

For investigating issues with a service.

```markdown
---
name: zerops-debug-service
description: Debug and diagnose issues with a Zerops service. Use when a service is failing, unhealthy, or behaving unexpectedly.
model: sonnet
maxTurns: 15
mcpServers:
  - zcp
---

# Zerops Debug Agent

You diagnose issues with a single Zerops service.

## Mandatory Workflow

| # | Step | Tool | What to look for |
|---|------|------|-----------------|
| 1 | Check status | zerops_discover service="{hostname}" | Status, container count, resources |
| 2 | Recent events | zerops_events serviceHostname="{hostname}" limit=10 | Failed deploys, restarts, scaling |
| 3 | Error logs | zerops_logs serviceHostname="{hostname}" severity="error" since="1h" | Error messages, stack traces |
| 4 | Warning logs | zerops_logs serviceHostname="{hostname}" severity="warning" since="1h" | Connection issues, retries |
| 5 | Check env vars | zerops_discover service="{hostname}" includeEnvs=true | Missing or misconfigured vars |
| 6 | Search for patterns | zerops_logs search="{error pattern}" since="24h" | Recurring issues |

## Rules

- Gather ALL information before diagnosing. Do not jump to conclusions after step 1.
- Use `zerops_knowledge` if you need Zerops-specific troubleshooting guidance.
- Report findings as: Problem, Evidence, Recommended Fix.
- Common issues: `https://` for internal connections (should be `http://`), dashes in env var refs (should be underscores), ports outside 10-65435 range.
```

#### `zerops-orchestrator.md`

For coordinating multi-service operations. This is the agent that spawns the others.

```markdown
---
name: zerops-orchestrator
description: Coordinate multi-service Zerops operations like bootstrap, multi-deploy, or project-wide debugging. Use when an operation involves more than 2 services.
model: opus
maxTurns: 30
mcpServers:
  - zcp
---

# Zerops Orchestrator Agent

You coordinate multi-service Zerops operations by delegating to specialist agents.

## Bootstrap Workflow

1. `zerops_discover` — check what already exists
2. `zerops_import` with dry run — validate infrastructure definition
3. `zerops_import` — create services
4. `zerops_process` — wait for all services to reach RUNNING
5. For each **runtime service**: spawn `zerops-configure-service` agent with the service hostname and discovered env vars
6. For each **managed service**: spawn `zerops-verify-managed` agent with the service hostname
7. After all agents complete: `zerops_discover` — verify everything is RUNNING
8. Report results to user

## Deploy-All Workflow

1. `zerops_discover` — list all runtime services
2. For each runtime service: spawn `zerops-deploy-service` agent
3. After all complete: `zerops_discover` — verify all RUNNING
4. Report results

## Debug-All Workflow

1. `zerops_discover` — list services with issues
2. For each problematic service: spawn `zerops-debug-service` agent
3. Collect findings, report summary

## Rules

- **Always discover first.** Don't assume what services exist.
- **Spawn agents in parallel** when services are independent.
- **Always run a final zerops_discover** after all agents complete — this is your verification, not their self-report.
- Classify services: runtime (nodejs, go, python, etc.) needs configure/deploy agents. Managed (postgresql, valkey, etc.) needs verify agents.
- If any agent reports failure, investigate before declaring the operation complete.
```

---

## 4. How Agents Get Service Context

The agents are generic — they don't know which service to work on until spawned. The orchestrator (or main agent) passes context via the Task tool's `prompt` parameter:

```
Task(
  subagent_type: "zerops-configure-service",
  prompt: "Configure service 'api' (nodejs@22).
           Env vars needed: DATABASE_URL=${db_connectionString}, CACHE_URL=redis://${cache_hostname}:${cache_port}
           Enable subdomain.
           Deploy from /Users/me/project with setup='api'."
)
```

The agent receives this as its task, combined with its pre-defined system prompt (the workflow table, rules, etc.). The system prompt defines **how** to work. The task prompt defines **what** to work on.

This is the same pattern as zcp-orig's subagents, but cleaner:
- zcp-orig: 360-line generated bash prompt per service
- ZCP: 40-line static agent definition + 5-line task prompt per service

---

## 5. What Stays Unchanged

- **All 15 MCP tools** — no changes
- **Server architecture** — stateless, no new tools needed
- **All hooks** — no changes
- **Workflow guides** — still useful for single-service operations
- **Knowledge base** — agents use `zerops_knowledge` when needed
- **Ralph Loop** — still available as an orthogonal mechanism

---

## 6. Implementation Spec

### New Files

| File | Purpose |
|------|---------|
| `.claude/agents/zerops-configure-service.md` | Configure + deploy runtime service |
| `.claude/agents/zerops-verify-managed.md` | Verify managed service |
| `.claude/agents/zerops-deploy-service.md` | Deploy code to service |
| `.claude/agents/zerops-debug-service.md` | Debug failing service |
| `.claude/agents/zerops-orchestrator.md` | Coordinate multi-service ops |

### Modified Files

| File | Change |
|------|--------|
| `internal/content/templates/CLAUDE.md` | Add agent usage guidance |
| `internal/content/workflows/bootstrap.md` | Mention orchestrator agent for multi-service |

### New Go Code

**Zero.** The agents are markdown files. The MCP tools they call already exist.

### CLAUDE.md Template Update

```markdown
# Zerops

For guided step-by-step procedures, use `zerops_workflow` to see available workflows.

## Multi-Service Operations

For operations involving 2+ services (bootstrap, multi-deploy, project-wide debug), use the `zerops-orchestrator` agent. It spawns specialist agents per service, ensuring every service is configured and verified.

Single-service operations (deploy one service, debug one service) can use workflow guides directly or the matching specialist agent.
```

---

## 7. How It Solves Each Problem

| Problem | Solution |
|---------|----------|
| Forgetting services | Orchestrator discovers all services, spawns one agent per service. Can't forget — each is a separate agent. |
| Skipping verification | Each agent has a mandatory workflow table with verification tool calls. |
| Losing the plan | Orchestrator has 7 steps. Each specialist has 5-6 steps. Too short to lose. |
| Repeated work | Agents don't overlap. Each handles exactly one service. |
| Self-reporting success | Orchestrator runs its own `zerops_discover` after all agents complete. |
| Context rot | Each agent runs in a fresh context. Nothing to rot. |

---

## 8. Trade-offs

### Gains

- **Zero server changes** — agents are markdown files, not Go code
- **Reviewable** — agent behavior is visible in `.claude/agents/`, versioned in git
- **Testable** — spawn an agent against a real project, check if it completes correctly
- **Composable** — agents use existing MCP tools, workflows, knowledge base
- **Evolvable** — edit a markdown file to change behavior, no recompile
- **Model-appropriate** — Haiku for simple verification, Sonnet for configuration, Opus for orchestration

### Losses

- **No enforcement at server level** — agents are guidance, not gates. Claude can ignore them, though the descriptions and CLAUDE.md strongly push toward using them.
- **Static prompts** — agent definitions don't include discovered env vars or service metadata. The spawner must pass this context via the task prompt. (This is actually fine — it's the same pattern as passing arguments to a function.)
- **Requires Claude Code agent support** — only works with Claude Code, not other MCP clients.

### When to Use Agents vs Direct Workflow

| Scenario | Approach |
|----------|----------|
| Bootstrap (2+ services) | `zerops-orchestrator` spawns specialists |
| Deploy single service | Workflow guide or `zerops-deploy-service` directly |
| Deploy multiple services | `zerops-orchestrator` |
| Debug single service | `zerops-debug-service` or workflow guide |
| Scale/configure single service | Workflow guide (3-4 tool calls) |

---

## 9. Verification Strategy

### Manual Testing

1. Create a Zerops project with 3 services (Go API + PostgreSQL + Valkey)
2. Invoke the orchestrator: "Bootstrap this project"
3. Verify: orchestrator discovers services, spawns 3 agents
4. Verify: configure-service agent completes all 6 steps for the API service
5. Verify: verify-managed agents check DB and cache
6. Verify: orchestrator's final discover shows all services RUNNING

### Agent Definition Review

For each agent definition, verify:
- Workflow table has verification column for every step
- Rules section covers failure handling
- MCP server access is correct (`zcp`)
- Model selection is appropriate (haiku for simple, sonnet for complex)
- maxTurns is sufficient but bounded

### Regression Testing

After any agent definition change:
- Run the full bootstrap scenario against a test project
- Verify no steps are skipped
- Verify verification tool calls actually happen (check logs)

---

## Appendix: Evolution Path

If enforcement becomes necessary (agents being ignored), the next step is straightforward:

1. Add a **PreToolUse hook** on `zerops_deploy` that checks whether the service was discovered first (a file written by zerops_discover)
2. Add a **Stop hook** that checks whether all discovered services have been verified
3. These are incremental additions to the hook system — not a new architecture

The agents-first approach is the simplest solution. Enforcement hooks are the escape hatch if it's not enough.
