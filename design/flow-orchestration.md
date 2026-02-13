# Flow Orchestration: Knowledge-First Workflows via MCP

## Status: FINAL — all three workflows + MCP instruction updated

## 1. Problem

There are two problems. One is hard, one is easy.

### The hard problem: correct configuration

Zerops requires two YAML configs — `import.yml` (infrastructure) and `zerops.yml` (build/deploy/run). These can be written a thousand ways. Many are functional but suboptimal: wrong cache paths, missing `addToRunPrepare` for Python, incorrect deploy files for Next.js, wrong connection string format, missing `mode` for managed services.

Getting these right requires deep, specific Zerops knowledge: runtime build patterns, framework quirks, env var wiring conventions, port ranges, deploy file syntax. The LLM's general training data is insufficient — it needs the Zerops-specific docs loaded into context BEFORE generating YAML.

**Current failure mode**: LLM sees "generate import.yml" → generates from training data → gets it ~70% right → errors at deploy time → debugs blind. The knowledge base has the correct patterns, but the LLM doesn't load them first.

### The easy problem: deployment and verification

Once YAML is correct, deployment is deterministic: import → wait → configure → deploy → verify. For multiple services this can cause context rot (20+ tool calls), but the solution is straightforward — spawn specialist agents with fresh context, one per service.

### Design priority

Configuration is where 90% of failures happen. Deployment and verification are mechanical. The design reflects this — each workflow puts the hard part first.

## 2. Core Insight

**Load knowledge at the right moment. Not before. Not after. Not as a fallback.**

The "right moment" differs per workflow:

| Workflow | Knowledge loads... | Why |
|----------|-------------------|-----|
| Bootstrap | BEFORE generating YAML | You need examples to write correct config |
| Deploy | BEFORE generating/fixing zerops.yml (conditional) | Skip if zerops.yml already exists |
| Debug | AFTER gathering data | You don't know what to search until you see the error |

```
Bootstrap/Deploy:  identify stack → load knowledge → generate config (informed)
Debug:             gather data → identify symptoms → load knowledge → diagnose (informed)
```

This is Anthropic's "explore before acting" pattern, applied contextually per workflow type.

## 3. Architecture

### MCP server as self-contained plugin

```
┌─────────────────────────────────────────┐
│  ZCP MCP Server (single Go binary)      │
│                                         │
│  Tools:     15 atomic operations        │
│  Context:   platform knowledge (lazy)   │
│  Workflows: knowledge-first playbooks   │
│             + embedded agent prompts    │
│  Knowledge: 65 MD files (BM25 search)   │
│                                         │
│  = Complete Zerops capability           │
│  = Zero external file dependencies      │
│  = Works with any MCP client            │
└─────────────────────────────────────────┘
```

### Context loading — three tiers

```
Tier 1: Always in context (~60 tokens, in instructions.go)
┌─────────────────────────────────────────┐
│ MCP instruction:                         │
│ "For multi-step operations (bootstrap,   │
│  deploy, debug), call zerops_workflow    │
│  first"                                  │
└──────────────────┬──────────────────────┘
                   │
Tier 2: On-demand via zerops_workflow (when needed)
┌──────────────────▼──────────────────────┐
│ Workflow response (~1300-1800 tokens):   │
│ - knowledge loading instructions         │
│ - generation rules / diagnostic checklist│
│ - validation loop                        │
│ - deployment steps + agent prompts       │
└──────────────────┬──────────────────────┘
                   │
Tier 3: On-demand via zerops_knowledge (per-component)
┌──────────────────▼──────────────────────┐
│ Specific runtime/service docs:           │
│ - zerops.yml examples for the framework  │
│ - connection string patterns             │
│ - build/deploy patterns                  │
│ - troubleshooting guidance               │
└─────────────────────────────────────────┘
```

### Why not bake Tier 3 into workflow responses?

Workflows are generic — bootstrap works for Node.js + PG, Go + Valkey, Python + MariaDB, or any combination. Debug works for any error type. The workflow tells the LLM HOW to gather knowledge; the LLM determines WHAT to gather based on the user's request and the gathered data.

## 4. Workflows

Three workflows rewritten with the knowledge-first pattern. Each has a hard phase (knowledge-dependent) and an easy phase (deterministic).

### Decision tree

```
User request
  │
  ├─ Single tool call? (check status, read logs, scale)
  │  → Use tool directly. No workflow.
  │
  ├─ Single service, multi-step? (deploy one, debug one)
  │  → zerops_workflow → follow steps (agents optional)
  │
  ├─ Creating infrastructure?
  │  → zerops_workflow workflow="bootstrap"
  │
  └─ Multiple services?
     → zerops_workflow → orchestrate with specialist agents
```

### Bootstrap (`bootstrap.md`)

The most complex workflow. Two full phases.

**Phase 1 — Configuration (6 steps):**

```
Step 1: zerops_discover                    → what exists already
Step 2: Identify stack components          → runtime + framework, managed services
Step 3: zerops_knowledge (parallel)        → load docs for EACH component
        "nodejs nextjs zerops.yml"         ← MANDATORY before any YAML generation
        "postgresql import connection"
        "valkey import"
Step 4: Generate import.yml               → using loaded knowledge + workflow rules
        (mode, priority, env wiring, enableSubdomainAccess, preprocessor)
Step 5: Generate zerops.yml               → using loaded runtime examples as base
        (buildCommands, deployFiles, cache, addToRunPrepare, ports, start)
Step 6: zerops_import dryRun=true         → validate, max 2 retries
        Present both YAMLs to user for review
```

**Phase 2 — Deployment + 6-point verification:**

```
For 1-2 services: direct tool calls (import → wait → env sync check → deploy → build polling → 7-point verify)
For 3+ services: agent orchestration
  Step 7: zerops_import                   → create all services
  Step 8: zerops_process                  → wait for RUNNING
  Step 9: Spawn agents in parallel:
          Task(sonnet) per runtime service → 11-step configure + deploy + poll + verify
            (discover → env vars → verify env sync → trigger deploy → poll build completion
             → check errors → confirm startup → post-startup errors → get subdomain
             → HTTP health check → managed svc connectivity)
          Task(haiku) per managed service  → verify status + check errors
  Step 10: zerops_discover                → orchestrator's own final verification

Build polling: 10s interval, 300s timeout. zerops_deploy returns BUILD_TRIGGERED before build completes.

7-point verification protocol (per runtime service):
  1. Build/deploy FINISHED (zerops_events — poll 10s, max 300s)
  2. Service RUNNING (zerops_discover)
  3. No error logs (zerops_logs severity="error")
  4. Startup confirmed (zerops_logs search="listening|started|ready")
  5. No post-startup errors (zerops_logs severity="error" since="2m")
  6. HTTP health check (curl /health — skip if no endpoint, step 4 = final gate)
  7. Managed svc connectivity (curl /status or log search — skip if no managed svcs)
```

### Deploy (`deploy.md`)

Conditional knowledge loading — skip if zerops.yml already exists.

**Phase 1 — Configuration check (4 steps):**

```
Step 1: zerops_discover service="{hostname}" includeEnvs=true
        → note type, status (RUNNING = re-deploy, READY_TO_DEPLOY = first deploy)
Step 2: Check if zerops.yml exists with setup: {hostname}
        → YES + re-deploy: skip to Phase 2
        → NO or first deploy: continue
Step 3: zerops_knowledge query="{runtime} {framework} zerops.yml"
        Also: "deploy patterns" for tilde syntax, multi-base patterns
        ← MANDATORY before generating zerops.yml
Step 4: Generate/fix zerops.yml using loaded example as base
        Key: deployFiles (#1 error source), build.cache, run.start, run.ports
        Three patterns: single-base, multi-base (→static), multi-runtime (→alpine)
        Present to user for review
```

**Phase 2 — Deploy and verify:**

```
Single service:
  zerops_deploy → zerops_events (FINISHED) → zerops_logs (no errors) → zerops_discover (RUNNING)
  If fails: check logs, fix zerops.yml, max 2 retries

Multiple services (3+):
  zerops_discover → list services
  Spawn Task(sonnet) per service in parallel
  zerops_discover → final verification
```

### Debug (`debug.md`)

Knowledge loads AFTER data gathering — you need to see the error before you know what to search.

**Phase 1 — Data gathering (5 steps):**

```
Step 1: zerops_discover service="{hostname}" includeEnvs=true
        → status, containers, resources, env vars
Step 2: zerops_events serviceHostname="{hostname}" limit=10
        → timeline: deploys, restarts, scaling, env changes
Step 3: zerops_logs severity="error" since="1h"
        → stack traces, connection errors, port failures
Step 4: zerops_logs severity="warning" since="1h"
        → retries, memory pressure, slow queries
Step 5: zerops_logs search="{error pattern}" since="24h"
        → recurring vs new issue
```

**Phase 2 — Diagnosis (3 steps):**

```
Step 6: Match against common Zerops issues (embedded in workflow)
        7 known patterns cover ~80% of cases:
        - Connection refused (https:// internal → use http://)
        - Service not starting (bad start, port range, bind address)
        - Env vars not resolving (dashes → underscores)
        - Build failures (missing deps, wrong commands)
        - Database connection (wrong format, localhost, DB not running)
        - Port binding (reserved ports, config mismatch)
        - Deploy succeeds but app broken (missing env vars, runtime deps)

Step 7: zerops_knowledge query="{error category}"
        ← ONLY if Step 6 didn't match. Loads targeted knowledge.
        "common gotchas" for full list of 40+ known pitfalls.

Step 8: Report as structured diagnosis:
        Problem → Evidence → Root Cause → Recommended Fix
```

**Multi-service debug (3+ services):**

```
zerops_discover → identify problematic services
Spawn Task(sonnet) per service in parallel
Collect findings, produce summary
```

## 5. Agent Prompt Templates

Embedded in workflow MD files. Each workflow includes only the templates it needs. Agents handle deterministic per-service work in fresh context windows.

| Agent | In workflow | Model | Steps | Purpose |
|-------|-----------|-------|-------|---------|
| Configure-Service | bootstrap.md | sonnet | 11 | discover → env vars → verify env sync → trigger deploy → poll build completion → check errors → confirm startup → post-startup errors → get subdomain → HTTP health check → managed svc connectivity |
| Verify-Managed | bootstrap.md | haiku | 2 | discover RUNNING → check error logs |
| Deploy-Service | deploy.md | sonnet | 7 | discover → trigger deploy → poll build completion → check errors → confirm startup → verify RUNNING → HTTP health |
| Debug-Service | debug.md | sonnet | 5 | discover → events → error logs → warning logs → pattern search |

All agents follow the same pattern:
- Table-driven mandatory workflow (step + tool + verification)
- No self-reporting — every step has an explicit verification tool call
- Retry once on failure, then report clearly
- Zerops-specific rules embedded (http://, underscores, port range)

Spawned via: `Task(subagent_type="general-purpose", model=<model>, prompt=<template with filled placeholders>)`

## 6. Implementation

### Modified files (DONE)

| File | Change |
|------|--------|
| `internal/server/instructions.go` | Added zerops_workflow nudge to MCP Instructions (Tier 1) |
| `internal/content/workflows/bootstrap.md` | Complete rewrite: knowledge-first Phase 1 (6 steps) + orchestrated Phase 2 with configure/verify agent prompts |
| `internal/content/workflows/deploy.md` | Complete rewrite: conditional Phase 1 (config check, 4 steps) + Phase 2 (deploy/verify) with deploy agent prompt |
| `internal/content/workflows/debug.md` | Complete rewrite: Phase 1 (data gathering, 5 steps) + Phase 2 (diagnosis with 7 common issues + knowledge fallback) with debug agent prompt |

### Unchanged workflows

| File | Why unchanged |
|------|--------------|
| `internal/content/workflows/configure.md` | Single-service env/port/subdomain ops — short, no knowledge loading needed |
| `internal/content/workflows/monitor.md` | Read-only monitoring — deterministic tool calls, no YAML generation |
| `internal/content/workflows/scale.md` | Single tool call with parameters — no knowledge loading needed |

### New Go code

**Zero.** Workflow content is embedded markdown. `zerops_workflow` tool already serves it.

### New files

**Zero.** No `.claude/agents/` files. No new Go packages.

## 7. Design Decisions

Key decisions and why:

1. **Embedded prompts, not `.claude/agents/` files** — Agent files can't spawn other agents (Claude Code nesting limit), require file deployment per project, and have permanent context cost. Embedded prompts in workflow markdown avoid all three.
2. **Knowledge-first, not generate-then-fix** — Loading Zerops docs before YAML generation prevents ~70% of errors. 2-4 extra tool calls is cheap vs. 5-10 debug/fix cycles.
3. **Main conversation as orchestrator** — No orchestrator agent. The main conversation spawns specialist agents directly via Task tool. Simpler, no nesting issues.
4. **MCP = complete plugin** — Single binary, zero file dependencies. Works with any MCP client, not just Claude Code.
5. **Guidance over enforcement** — Workflows are advisory playbooks, not gates. Evolution path (Section 9) adds hooks if enforcement becomes necessary.
6. **Contextual knowledge loading** — Bootstrap/Deploy load knowledge before generation. Debug loads after data gathering. Each workflow loads at the moment that matters.
7. **7-point verification protocol** — Evidence-depth matching the original shell-based ZCP using MCP tools + agent bash curl. Checks build completion (with polling), service status, error logs, startup confirmation, post-startup errors, HTTP health, and managed service connectivity. Degrades gracefully if app has no `/health` endpoint (startup log confirmation becomes the final gate).
8. **Asynchronous build monitoring** — zerops_deploy returns before build completes (status=BUILD_TRIGGERED). Workflows embed polling protocol (10s interval, 300s timeout) with build failure detection via zcli --showBuildLogs fallback.

## 8. Alignment with Research

| Principle (source) | How this design aligns |
|-------------------|---------------|
| "Explore before acting" (Anthropic agentic coding) | Knowledge loading before YAML generation; data gathering before diagnosis |
| "Smallest set of high-signal tokens" (Anthropic context eng.) | Three-tier lazy loading — nothing loads until needed |
| "Start with simplest solution" (Anthropic agents guide) | Main conversation = orchestrator, no framework |
| "Orchestrator-workers for multi-service" (Anthropic) | Task tool + general-purpose agents for per-service work |
| "Fresh context avoids rot" (Anthropic) | Each agent = clean context window |
| "Narrow specialization wins" (UC Berkeley, 600+ deployments) | Each agent handles exactly one service |
| "Worse is better" (industry consensus) | Guidance over enforcement, no framework |
| "Bounded retry loops" (industry anti-patterns) | Max 2 retries for YAML validation, then ask user |
| "Systematic before creative" (Anthropic debug patterns) | Debug gathers ALL data before diagnosing |

## 9. Evolution Path

If guidance proves insufficient:

1. **PreToolUse hook** on `zerops_import` — check that zerops_knowledge was called first
2. **PreToolUse hook** on `zerops_deploy` — check that service was discovered first
3. **Stop hook** — check all discovered services were verified
4. These are incremental hook additions — not a new architecture

## 10. Trade-offs

### Gains
- Solves the actual hard problem (YAML generation quality)
- Debug workflow includes diagnostic checklist (7 common issues = ~80% coverage)
- No nesting problem, no file dependencies, no permanent context cost
- MCP server is self-contained plugin
- Single source of truth (embedded markdown in Go binary)
- Knowledge loads contextually per workflow type

### Losses
- No tool restrictions on agents (guidance only)
- No auto-delegation by agent description (one extra zerops_workflow call)
- Knowledge loading adds 2-4 tool calls before YAML generation
- Debug common issues list needs manual maintenance as new gotchas emerge

### Acceptable because
- Tool restrictions: agents are guidance, not gates — enforcement via hooks if needed (Section 9)
- Extra workflow call: negligible latency vs. the value of correct YAML
- Knowledge loading: 2-4 calls is cheap; prevents 5-10 debug/fix cycles later
- Common issues list: 40+ gotchas in knowledge base as fallback; workflow list covers the top 7
