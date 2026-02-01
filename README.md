# Zerops Platform Agent System (ZCP)

An AI agent workflow orchestration framework for the Zerops PaaS platform. Guides AI agents (primarily Claude) through structured, evidence-based development and deployment workflows.

## Purpose

**Safe, predictable AI-driven deployments through enforced quality gates.**

This system solves the problem of AI agents making mistakes during complex deployment workflows by:
- Enforcing linear phase progression (no skipping steps)
- Requiring evidence files at each gate (proof of work, not promises)
- Making all rules self-documenting (survives context window compaction)
- Providing complete context recovery (agents can resume mid-session)

## Core Principle

**Dev is for iterating and fixing. Stage is for final validation.**

Agents must test and fix errors on dev before deploying to stage. HTTP 200 doesn't mean the feature works—check response content, logs, and browser console.

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              ZEROPS PROJECT                                      │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐      │
│   │   appdev    │    │  appstage   │    │ postgresql  │    │   valkey    │      │
│   │  (runtime)  │    │  (runtime)  │    │  (managed)  │    │  (managed)  │      │
│   │             │    │             │    │             │    │             │      │
│   │  SSH ✓      │    │  SSH ✓      │    │  NO SSH     │    │  NO SSH     │      │
│   │  SSHFS ✓    │    │  SSHFS ✓    │    │  psql only  │    │  redis-cli  │      │
│   └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘      │
│          │                  │                  │                  │             │
│          └──────────────────┴──────────────────┴──────────────────┘             │
│                              Private Network                                     │
│                                     │                                            │
├─────────────────────────────────────┼────────────────────────────────────────────┤
│                                     │                                            │
│   ┌─────────────────────────────────┴────────────────────────────────────────┐   │
│   │                              ZCP                                          │   │
│   │                    (Zerops Control Plane)                                 │   │
│   │                                                                           │   │
│   │   SSHFS Mounts:  /var/www/appdev/   /var/www/appstage/                   │   │
│   │   Tools:         zcli, jq, yq, psql, redis-cli, agent-browser            │   │
│   │   Env Vars:      $projectId, $ZEROPS_ZCP_API_KEY, ${svc}_VAR             │   │
│   │                                                                           │   │
│   │   ┌───────────────────────────────────────────────────────────────────┐   │   │
│   │   │  .zcp/workflow.sh  ←  Agent Entry Point                           │   │   │
│   │   │       │                                                            │   │   │
│   │   │       ├── lib/commands/     (command handlers)                    │   │   │
│   │   │       ├── lib/gates.sh      (transition enforcement)              │   │   │
│   │   │       ├── lib/bootstrap/    (service creation)                    │   │   │
│   │   │       └── lib/state/        (persistent storage)                  │   │   │
│   │   └───────────────────────────────────────────────────────────────────┘   │   │
│   │                                                                           │   │
│   │   ┌───────────────────────────────────────────────────────────────────┐   │   │
│   │   │  CLAUDE.md  ←  Agent reads this FIRST                             │   │   │
│   │   └───────────────────────────────────────────────────────────────────┘   │   │
│   └───────────────────────────────────────────────────────────────────────────┘   │
│                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────┘
```

### Access Patterns

```
┌────────────────────────────────────────────────────────────────────────────────┐
│ From ZCP Container                                                              │
├─────────────────┬──────────────────────────────────────────────────────────────┤
│ Runtime Service │ ssh appdev "go build"          # Execute commands            │
│                 │ /var/www/appdev/               # Direct filesystem access    │
│                 │ curl http://appdev:8080        # Network access              │
├─────────────────┼──────────────────────────────────────────────────────────────┤
│ Managed Service │ psql "$db_connectionString"    # Client tools only           │
│                 │ redis-cli -h valkey            # No SSH, no filesystem       │
├─────────────────┼──────────────────────────────────────────────────────────────┤
│ Variables       │ ${appdev_PORT}                 # ZCP accessing service var   │
│                 │ ssh appdev "echo \$PORT"       # Inside service via SSH      │
└─────────────────┴──────────────────────────────────────────────────────────────┘
```

### Service Types

| Type | SSH | SSHFS | Access From ZCP |
|------|-----|-------|-----------------|
| **Runtime** (go, nodejs, php, python) | ✓ | ✓ | `ssh {service} "command"` |
| **Managed** (postgresql, valkey, nats) | ✗ | ✗ | `psql "$db_connectionString"` |

---

## Repository Structure

```
zcp-repo/                      ← Repository root (you are here)
├── README.md                  ← This file
└── zcp/                       ← DEPLOYMENT PACKAGE (copy this to projects)
    ├── CLAUDE.md              ← Agent entry point
    ├── .zcp/                  ← Workflow tools
    │   ├── recipe-search.sh   ← Recipe discovery tool (Gate 0)
    │   ├── validate-config.sh ← Config validation (Gate 3)
    │   └── ...
    └── .claude/               ← Claude Code settings
```

When deploying to a Zerops project, copy the contents of `zcp/` to the project root.

### File Structure

```
zcp/
├── CLAUDE.md              # Entry point for agents
├── .zcp/                  # Workflow tools
│   ├── workflow.sh        # Main entry point + command router
│   ├── recipe-search.sh   # Recipe & docs search tool (Gate 0)
│   ├── validate-config.sh # zerops.yml validation (Gate 3)
│   ├── verify.sh          # Endpoint testing with evidence + auto context capture
│   ├── status.sh          # Deployment status + wait mode + auto context capture
│   └── lib/               # Modular components (AI-readable)
│       ├── utils.sh       # State management, persistence, locking, context capture
│       ├── gates.sh       # Phase transition gate checks (Gates 0-7, S)
│       ├── state.sh       # WIGGUM state management
│       ├── help.sh        # Help system loader
│       ├── commands.sh    # Commands loader
│       ├── help/
│       │   ├── full.sh    # Full platform reference
│       │   └── topics.sh  # Topic-specific help (discover, develop, etc.)
│       ├── commands/
│       │   ├── init.sh       # init, quick commands
│       │   ├── transition.sh # transition_to, phase guidance
│       │   ├── discovery.sh  # create_discovery, refresh_discovery
│       │   ├── status.sh     # show, complete, reset
│       │   ├── extend.sh     # extend, upgrade-to-full, record_deployment, import evidence
│       │   ├── iterate.sh    # iterate command (post-DONE continuation)
│       │   ├── retarget.sh   # retarget command (change deployment target)
│       │   └── context.sh    # intent, note commands (rich context)
│       ├── state.sh       # Single source of truth for state (session, phase, bootstrap)
│       ├── view.sh        # WIGGUM display layer (progress, evidence, next action)
│       ├── bootstrap/     # Bootstrap orchestration (new project setup)
│       │   ├── output.sh        # JSON response formatting
│       │   ├── detect.sh        # Project state detection (FRESH/CONFORMANT/NON_CONFORMANT)
│       │   ├── import-gen.sh    # Import.yml generation
│       │   └── steps/           # Individual bootstrap steps
│       │       ├── plan.sh              # Create bootstrap plan
│       │       ├── recipe-search.sh     # Fetch runtime patterns
│       │       ├── generate-import.sh   # Generate import.yml
│       │       ├── import-services.sh   # Import via zcli
│       │       ├── wait-services.sh     # Poll until RUNNING
│       │       ├── mount-dev.sh         # SSHFS mount
│       │       ├── discover-services.sh # Discover actual env vars (NEW)
│       │       ├── finalize.sh          # Create handoff data
│       │       ├── spawn-subagents.sh   # Output subagent instructions
│       │       └── aggregate-results.sh # Wait for completion, create discovery
│       └── state/         # Persistent storage (survives container restart)
│           ├── workflow/  # Current workflow state
│           │   ├── evidence/      # Persisted evidence files
│           │   ├── iterations/    # Archived iteration history
│           │   ├── context.json   # Last error, notes
│           │   └── intent.txt     # Workflow intent
│           └── archive/   # Completed/abandoned workflows
└── .claude/
    └── settings.json      # Claude Code permissions
```

---

## Master Decision Tree

**Start every request here.** This tree determines the correct workflow path.

```
                              User Request
                                   │
                                   ▼
                        .zcp/workflow.sh show
                                   │
           ┌───────────────────────┼───────────────────────┐
           │                       │                       │
           ▼                       ▼                       ▼
    No Workflow Active      Active Workflow          DONE State
           │                       │                       │
           ▼                       │                       ▼
  zcli service list -P $projectId  │            .zcp/workflow.sh iterate
           │                       │                  "summary"
    ┌──────┴──────┐                │                       │
    │             │                │                       ▼
    ▼             ▼                │              Returns to DEVELOP
No Services  Services Exist        │             (preserves discovery)
    │             │                │
    ▼             ▼                │
 BOOTSTRAP    STANDARD             │
    │             │                │
    ▼             ▼                │
bootstrap     init [flags]         │
--runtime X   --dev-only           │
--services Y  --hotfix             │
              --quick              │
                                   │
                                   ▼
                          ┌────────────────────────────────────────┐
                          │         PHASE-SPECIFIC ACTIONS         │
                          ├────────────────────────────────────────┤
                          │ INIT      → transition_to DISCOVER     │
                          │ DISCOVER  → create_discovery, then     │
                          │             transition_to DEVELOP      │
                          │ DEVELOP   → code, verify.sh, then      │
                          │             transition_to DEPLOY       │
                          │ DEPLOY    → zcli push, status.sh, then │
                          │             transition_to VERIFY       │
                          │ VERIFY    → verify.sh, browser, then   │
                          │             transition_to DONE         │
                          │ BOOTSTRAP → .zcp/bootstrap.sh resume   │
                          └────────────────────────────────────────┘
```

---

## Workflow Modes

### Full Mode (Default)

The complete evidence-gated workflow for production deployments.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  .zcp/workflow.sh init                                                          │
│                                                                                  │
│  INIT ──────────────────────────────────────────────────────────────────────►   │
│    │                                                                             │
│    │  Gate 0: recipe_review.json (recipe-search.sh quick)                       │
│    ▼                                                                             │
│  DISCOVER ──────────────────────────────────────────────────────────────────►   │
│    │  • zcli service list -P $projectId                                         │
│    │  • create_discovery {dev_id} appdev {stage_id} appstage                    │
│    │                                                                             │
│    │  Gate 1: discovery.json                                                    │
│    ▼                                                                             │
│  DEVELOP ───────────────────────────────────────────────────────────────────►   │
│    │  • Edit code at /var/www/appdev/                                           │
│    │  • ssh appdev "go build && ./app"                                          │
│    │  • .zcp/verify.sh appdev 8080 / /health                                    │
│    │                                                                             │
│    │  Gate 2: dev_verify.json + config validation                               │
│    ▼                                                                             │
│  DEPLOY ────────────────────────────────────────────────────────────────────►   │
│    │  • ssh appdev "zcli push {stage_id} --setup=prod"                          │
│    │  • .zcp/status.sh --wait appstage                                          │
│    │                                                                             │
│    │  Gate 3: deploy_evidence.json                                              │
│    ▼                                                                             │
│  VERIFY ────────────────────────────────────────────────────────────────────►   │
│    │  • .zcp/verify.sh appstage 8080 / /health                                  │
│    │  • agent-browser open "$URL" (visual confirmation)                         │
│    │                                                                             │
│    │  Gate 4: stage_verify.json                                                 │
│    ▼                                                                             │
│  DONE ──────────────────────────────────────────────────────────────────────►   │
│    │  • .zcp/workflow.sh complete                                               │
│    │                                                                             │
│    └──► .zcp/workflow.sh iterate "next task" ──► back to DEVELOP                │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### Alternative Modes

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                                                                  │
│  DEV-ONLY MODE (.zcp/workflow.sh init --dev-only)                               │
│  ───────────────────────────────────────────────────────────────────────────    │
│  INIT ──► DISCOVER ──► DEVELOP ──► DONE                                         │
│                            │                                                     │
│                            └── No DEPLOY/VERIFY phases                          │
│                            └── upgrade-to-full available later                  │
│                                                                                  │
│  Use case: Rapid prototyping, no deployment needed yet                          │
│                                                                                  │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  HOTFIX MODE (.zcp/workflow.sh init --hotfix)                                   │
│  ───────────────────────────────────────────────────────────────────────────    │
│  INIT ──► DEVELOP ──► DEPLOY ──► VERIFY ──► DONE                                │
│              │                                                                   │
│              └── Skips DISCOVER (reuses <24h discovery)                         │
│              └── Skips dev_verify gate                                          │
│                                                                                  │
│  Use case: Urgent fixes, skip verification, speed is critical                   │
│                                                                                  │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  QUICK MODE (.zcp/workflow.sh --quick)                                          │
│  ───────────────────────────────────────────────────────────────────────────    │
│  No phases, no gates, no evidence required                                      │
│  Agent can do anything without workflow tracking                                │
│                                                                                  │
│  Use case: Exploration, debugging, reading code                                 │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## Bootstrap Flow (New Project Setup)

When no runtime services exist, the agent orchestrates service creation step-by-step.

### Bootstrap vs Standard (Auto-Detected)

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                                                                  │
│  .zcp/workflow.sh init  →  System checks: zcli service list -P $projectId       │
│                                                                                  │
│         ┌───────────────────────────┬────────────────────────────────┐          │
│         │                           │                                │          │
│         ▼                           ▼                                ▼          │
│     FRESH                      CONFORMANT                    NON_CONFORMANT     │
│  (No services)              (Has dev/stage pairs)         (Has services, but    │
│         │                           │                      no proper pairs)     │
│         │                           │                                │          │
│         ▼                           ▼                                ▼          │
│    BOOTSTRAP                    STANDARD                        BOOTSTRAP       │
│    (create all)                 (init)                      (add missing)       │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

> **Key Insight:** There is no "bootstrap" phase. Projects always start with ZCP already present.
> The workflow detects project state and guides appropriately. **Init = Extend** — always adding to an existing project.

### Bootstrap Orchestration

**Agent-orchestrated**: The agent runs each step individually for visibility and error handling.

```
.zcp/workflow.sh bootstrap --runtime go --services postgresql
        │
        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│  BOOTSTRAP STEPS (Agent calls each individually)                                 │
│                                                                                  │
│  ┌─────────┐    ┌──────────────┐    ┌─────────────────┐    ┌───────────────┐    │
│  │  plan   │───►│recipe-search │───►│ generate-import │───►│import-services│    │
│  │(instant)│    │  (2-3 sec)   │    │    (instant)    │    │  (60-120s)    │    │
│  └─────────┘    └──────────────┘    └─────────────────┘    └───────────────┘    │
│                                                                   │              │
│  ┌──────────────────────────────────────────────────────────────┐ │              │
│  │                                                              │ │              │
│  │  ┌──────────┐   ┌─────────────┐   ┌───────────┐  ┌────────┐ │ │              │
│  │  │ finalize │◄──│discover-svc │◄──│ mount-dev │◄─│wait-svc│◄┘ │              │
│  │  │(instant) │   │  (instant)  │   │ (instant) │  │ (poll) │   │              │
│  │  └──────────┘   └─────────────┘   └───────────┘  └────────┘   │              │
│  │       │              (NEW)                                    │              │
│  └───────┼───────────────────────────────────────────────────────┘              │
│          │                                                                       │
│          ▼                                                                       │
│  ┌────────────────┐                                                             │
│  │spawn-subagents │─── Returns instructions for agent to spawn subagents        │
│  │   (instant)    │    (one per service pair, can run in parallel)              │
│  └────────────────┘                                                             │
│          │                                                                       │
│          ▼                                                                       │
│  ┌────────────────────────────────────────────────────────────┐                 │
│  │  SUBAGENTS (spawned via Task tool, run in parallel)        │                 │
│  │  Each subagent:                                            │                 │
│  │    1. Creates zerops.yml                                   │                 │
│  │    2. Deploys config to dev                                │                 │
│  │    3. Generates status page code                           │                 │
│  │    4. Tests dev                                            │                 │
│  │    5. Deploys to stage                                     │                 │
│  │    6. Tests stage                                          │                 │
│  │    7. Writes /tmp/{hostname}_complete.json (URLs, verify)  │                 │
│  │    8. Marks complete via mark-complete.sh                  │                 │
│  └────────────────────────────────────────────────────────────┘                 │
│          │                                                                       │
│          ▼                                                                       │
│  ┌───────────────────┐                                                          │
│  │ aggregate-results │─── Reads completion files, creates discovery.json        │
│  │   (display-only)  │    URLs from subagent data (no re-testing)               │
│  └───────────────────┘                                                          │
│          │                                                                       │
│          ▼                                                                       │
│  Standard Workflow unlocked (DEVELOP phase)                                      │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### Step-by-Step Execution

```bash
# Initialize (creates plan, returns immediately)
.zcp/workflow.sh bootstrap --runtime go --services postgresql

# Agent runs steps individually for visibility
.zcp/bootstrap.sh step recipe-search      # Fetch patterns (use timeout:60000)
.zcp/bootstrap.sh step generate-import    # Create import.yml
.zcp/bootstrap.sh step import-services    # Send to Zerops API (use timeout:180000)
.zcp/bootstrap.sh step wait-services --wait  # Polls automatically until ready!
.zcp/bootstrap.sh step mount-dev appdev   # SSHFS mount
.zcp/bootstrap.sh step discover-services  # Discover actual env vars (NEW)
.zcp/bootstrap.sh step finalize           # Create handoff data
.zcp/bootstrap.sh step spawn-subagents    # Get subagent instructions

# For each service pair, spawn a subagent via Task tool
# Subagent: creates zerops.yml, deploys, writes code, tests, marks complete
# Subagent MUST run: .zcp/mark-complete.sh {hostname} when done

# Wait for all subagents to complete (with polling and auto-detection)
.zcp/wait-for-subagents.sh --timeout 600
# OR poll manually:
.zcp/bootstrap.sh step aggregate-results  # Poll until all complete
# Creates discovery.json, sets workflow to DEVELOP phase
```

### CRITICAL: Never Write Shell Loops

**DO NOT write `while` loops in bash commands.** They fail with `(eval):1: parse error`.

```bash
# WRONG - will fail with parse error:
while true; do
    result=$(.zcp/bootstrap.sh step wait-services)
    ...
done

# CORRECT - use --wait flag:
.zcp/bootstrap.sh step wait-services --wait
```

All polling commands have `--wait` flags that handle looping internally:
- `.zcp/bootstrap.sh step wait-services --wait` - Wait for services to be RUNNING
- `.zcp/wait-for-subagents.sh` - Wait for subagents to complete
- `.zcp/status.sh --wait {service}` - Wait for deployment to complete

### Subagent State Tracking

Subagents mark completion by running `.zcp/mark-complete.sh {hostname}`. If this fails:

```bash
# aggregate-results auto-detects: if zerops.yml + source code exist → auto-mark complete
.zcp/bootstrap.sh step aggregate-results

# Manual recovery if needed:
.zcp/mark-complete.sh appdev
.zcp/bootstrap.sh step aggregate-results
```

### Bootstrap Scenarios

| Scenario | Command/Action | Outcome |
|----------|---------------|---------|
| Fresh project | `bootstrap --runtime go` | Creates dev/stage pairs |
| Interrupted | `.zcp/bootstrap.sh resume` | Returns next pending step |
| Step fails | Fix issue, re-run same step | Idempotent recovery |
| Check progress | `.zcp/bootstrap.sh status` | Shows checkpoint, pending steps |
| Already conformant | `bootstrap ...` | Returns "use init" guidance |
| Subagents pending | `aggregate-results` | Returns in_progress, poll again |
| **State file missing** | `mark-complete {hostname}` | Manually mark, then aggregate |
| All subagents done | `aggregate-results` | Creates discovery.json, completes |
| Wait with polling | `wait-for-subagents.sh` | Polls until complete or timeout |

---

## Typical Workflow

A complete workflow with all gates:

```bash
# 1. INIT: Start workflow session
.zcp/workflow.sh init

# 2. Gate 0: Review recipes FIRST (mandatory)
.zcp/recipe-search.sh quick go postgresql
#    → Creates /tmp/recipe_review.json

# 3. DISCOVER: Transition (Gate 0 checked)
.zcp/workflow.sh transition_to DISCOVER
zcli service list -P $projectId
.zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage

# 4. DEVELOP: Transition (Gate 1 checked)
.zcp/workflow.sh transition_to DEVELOP
# Edit code at /var/www/appdev/
# Build and test: ssh appdev "go build && ./app"

# 5. Verify dev works
.zcp/verify.sh appdev 8080 / /status /api/health
#    → Creates /tmp/dev_verify.json

# 6. DEPLOY: Transition (Gate 2 checked - dev_verify.json + config validation)
.zcp/workflow.sh transition_to DEPLOY
ssh appdev "zcli push {stage_service_id} --setup=prod"
.zcp/status.sh --wait appstage
#    → Creates /tmp/deploy_evidence.json

# 7. VERIFY: Transition (Gate 3 checked)
.zcp/workflow.sh transition_to VERIFY
.zcp/verify.sh appstage 8080 / /status /api/health
#    → Creates /tmp/stage_verify.json

# 8. DONE: Transition (Gate 4 checked)
.zcp/workflow.sh transition_to DONE
.zcp/workflow.sh complete
```

---

## Gate System

Gates enforce quality checkpoints. Each gate blocks until evidence exists.

### Gate Overview

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              GATE ENFORCEMENT                                    │
├────────┬─────────────────────────┬─────────────────────────┬────────────────────┤
│ Gate   │ Transition              │ Evidence File           │ Created By         │
├────────┼─────────────────────────┼─────────────────────────┼────────────────────┤
│ 0      │ INIT → DISCOVER         │ recipe_review.json      │ recipe-search.sh   │
│ 0.5    │ extend command          │ import_validated.json   │ validate-import.sh │
│ B      │ BOOTSTRAP → WORKFLOW    │ zcp_state.json          │ complete_bootstrap │
│ 1      │ DISCOVER → DEVELOP      │ discovery.json          │ create_discovery   │
│ 2      │ DEVELOP → DEPLOY        │ dev_verify.json + cfg   │ verify.sh {dev}    │
│ 3      │ DEPLOY → VERIFY         │ deploy_evidence.json    │ status.sh --wait   │
│ 4      │ VERIFY → DONE           │ stage_verify.json       │ verify.sh {stage}  │
└────────┴─────────────────────────┴─────────────────────────┴────────────────────┘

Evidence Validation:
  • Session ID must match current session (prevents stale evidence)
  • Timestamp must be < 24h old (freshness check)
  • Backward transitions (--back) invalidate downstream evidence
```

### Gate Flow Visualization

```
                   Gate 0                    Gate 1                    Gate 2
                      │                         │                         │
    ┌─────┐     ┌─────▼─────┐     ┌─────┐     ┌─▼───┐     ┌─────┐     ┌──▼──┐
    │INIT │────►│recipe.json│────►│DISC │────►│disc │────►│ DEV │────►│ dev │
    └─────┘     │  exists?  │     │OVER │     │.json│     │ELOP │     │ ver │
                └───────────┘     └─────┘     │exist│     └─────┘     │ify? │
                      │NO              ▲      └─────┘           ▲     └─────┘
                      ▼                │           │            │         │NO
                   BLOCKED             │           │            │         ▼
                                       │           └────────────┘      BLOCKED
                                       │                  │
                                       └──────────────────┘
                                            Gate checks
```

### Gate 0: Recipe Discovery (Critical)

Gate 0 alone prevents 5 of the 13 documented common mistakes. **Run this first**:

```bash
.zcp/recipe-search.sh quick {runtime} [managed-service]
# Example: .zcp/recipe-search.sh quick go postgresql
```

This creates `/tmp/recipe_review.json` with:
- Valid version strings (`go@1` not `go@latest`)
- Correct YAML structure (`zerops:` wrapper, `setup:` names)
- Production patterns (alpine runtime, `cache: true`)
- Environment variable patterns (granular, not connection strings)

### Gate 2: Config Validation

Before deployment, validate your `zerops.yml`:

```bash
.zcp/validate-config.sh /var/www/{app}/zerops.yml
```

Checks:
- Has `zerops:` top-level wrapper
- Separate dev and prod setups
- `cache: true` in both builds (5-10x faster rebuilds)
- Prod uses alpine runtime (40x smaller images)
- Dev uses `zsc noop --silent` (manual control)
- Explicit `envVariables` (not implicit)
- Granular env vars (`DB_HOST`, not `DATABASE_URL`)

### Mistakes Prevented by Gates

| # | Mistake | Gate |
|---|---------|------|
| 1 | `go@latest` instead of `go@1` | Gate 0 |
| 2 | Invalid YAML field `buildFromGit: false` | Gate 0 |
| 3 | Missing `zerops:` wrapper | Gate 2 (config) |
| 4 | Wrong build command syntax | Gate 0 |
| 5 | No `cache: true` | Gate 2 (config) |
| 6 | Go runtime for prod (not alpine) | Gate 2 (config) |
| 7 | Connection string vs granular vars | Gate 2 (config) |
| 8 | Identical dev/prod setups | Gate 2 (config) |
| 9 | Implicit build (no main.go ref) | Gate 0 |
| 10 | No `zeropsSetup` service linking | Gate 0.5 |
| 11-13 | Runtime/workflow errors | Gates 2-4 |

**Gates 0 + 2 together catch 77% of all documented mistakes.**

---

## State Management

### State Files (Persistent)

```
.zcp/state/                         ← Survives container restart
├── workflow/
│   ├── session                     ← Current session ID
│   ├── mode                        ← full|dev-only|hotfix|quick
│   ├── phase                       ← Current phase
│   ├── iteration                   ← Iteration counter
│   ├── intent.txt                  ← Workflow goal
│   ├── context.json                ← Last error, notes
│   └── evidence/                   ← Persisted gate evidence
├── iterations/                     ← Archived iteration history
└── archive/                        ← Completed workflows
```

### State Lifecycle

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                          STATE SYNCHRONIZATION                                │
│                                                                               │
│    /tmp/ (fast)                                .zcp/state/ (persistent)       │
│         │                                              │                      │
│         │◄────── restore_from_persistent ──────────────┤ (on startup)        │
│         │                                              │                      │
│         ├─────── write_evidence ──────────────────────►│ (write-through)     │
│         │                                              │                      │
│         ├─────── sync_to_persistent ──────────────────►│ (periodic)          │
│         │                                              │                      │
│    [All tools read/write here]            [Survives restart]                 │
│                                                                               │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## Continuity Operations

### Iteration (After DONE)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  .zcp/workflow.sh iterate "Add delete button"                                 │
│                                                                               │
│     DONE state                                                                │
│         │                                                                     │
│         ▼                                                                     │
│  ┌─────────────────────┐                                                     │
│  │  Archives evidence  │                                                     │
│  │  Increments counter │                                                     │
│  │  Preserves discovery│                                                     │
│  │  Resets to DEVELOP  │                                                     │
│  └─────────────────────┘                                                     │
│         │                                                                     │
│         ▼                                                                     │
│     DEVELOP ──► DEPLOY ──► VERIFY ──► DONE                                   │
│                                                                               │
│  Options:                                                                     │
│    iterate --to VERIFY "CSS fix"    ← Skip to specific phase                 │
│    iterate --to DEPLOY "Config"     ← Skip to deployment                     │
│                                                                               │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Retarget (Change Deployment Target)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  .zcp/workflow.sh retarget stage {new_id} {new_name}                         │
│                                                                               │
│     Current state                                                             │
│         │                                                                     │
│         ▼                                                                     │
│  ┌─────────────────────────────────┐                                         │
│  │  Updates discovery.json         │                                         │
│  │  Preserves dev_verify.json      │   ← Dev work is still valid            │
│  │  Invalidates deploy_evidence    │   ← Must redeploy to new target        │
│  │  Invalidates stage_verify       │   ← Must re-verify                     │
│  └─────────────────────────────────┘                                         │
│                                                                               │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Recovery (After Crash/Restart)

```bash
# Always run this first when resuming
.zcp/workflow.sh show

# Extended context (intent, notes, error history)
.zcp/workflow.sh show --full

# Full recovery (restores from persistent state)
.zcp/workflow.sh recover
```

---

## Commands

```bash
# Initialize
.zcp/workflow.sh init                    # Full mode with gates
.zcp/workflow.sh init --dev-only         # No deployment phase
.zcp/workflow.sh init --hotfix           # Skip to DEVELOP, reuse discovery
.zcp/workflow.sh --quick                 # No enforcement

# Recipe Discovery (Gate 0 - run FIRST)
.zcp/recipe-search.sh quick {runtime} [managed-service]
.zcp/recipe-search.sh pattern {service-type}
.zcp/recipe-search.sh version {service-type}
.zcp/recipe-search.sh field {yaml-section}

# Import Validation (Gate 0.5 - automatic with extend)
.zcp/validate-import.sh {import.yml}     # Validates before import

# Bootstrap (New project from scratch)
.zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey
.zcp/bootstrap.sh step {step-name}       # Run individual step
.zcp/bootstrap.sh status                 # Check progress
.zcp/bootstrap.sh resume                 # Get next step to run
.zcp/wait-for-subagents.sh               # Wait for subagents with polling
.zcp/mark-complete.sh {hostname}         # Mark service bootstrap complete
.zcp/mark-complete.sh --status           # Show all service states

# Transitions
.zcp/workflow.sh transition_to DISCOVER  # Requires Gate 0 (recipe review)
.zcp/workflow.sh transition_to DEVELOP   # Requires Gate 1 (discovery)
.zcp/workflow.sh transition_to --back DEVELOP  # Go backward, invalidates evidence

# Discovery
.zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
.zcp/workflow.sh create_discovery --single {id} {name}  # Same service for dev/stage
.zcp/workflow.sh refresh_discovery       # Validate existing discovery

# State management
.zcp/workflow.sh show                    # Current status
.zcp/workflow.sh show --full             # Status + extended context (intent, notes)
.zcp/workflow.sh reset                   # Clear everything
.zcp/workflow.sh reset --keep-discovery  # Preserve discovery for new session

# Workflow Continuity (post-DONE iteration)
.zcp/workflow.sh iterate "summary"       # Start new iteration from DONE
.zcp/workflow.sh iterate --to VERIFY     # Skip to specific phase
.zcp/workflow.sh retarget stage {id} {n} # Change deployment target
.zcp/workflow.sh intent "goal"           # Set/show workflow intent
.zcp/workflow.sh note "observation"      # Add timestamped note

# Extensions
.zcp/workflow.sh extend {import.yml}     # Add services to project (creates Gate 2 evidence)
.zcp/workflow.sh upgrade-to-full         # Convert dev-only to full deployment

# Help
.zcp/workflow.sh --help
.zcp/workflow.sh --help {topic}          # discover, develop, deploy, verify, done,
                                         # vars, services, trouble, example, gates,
                                         # extend, bootstrap, cheatsheet, import-validation
```

---

## Evidence Files

All stored in `$ZCP_TMP_DIR` (defaults to `/tmp/`, with write-through to `.zcp/state/` for persistence):

### State Files

| File | Created By | Contains |
|------|------------|----------|
| `zcp_session` | `.zcp/workflow.sh init` | Session ID |
| `zcp_mode` | `.zcp/workflow.sh init` | full, dev-only, hotfix, quick |
| `zcp_phase` | `.zcp/workflow.sh transition_to` | Current phase |
| `zcp_iteration` | `.zcp/workflow.sh iterate` | Current iteration number |
| `zcp_intent.txt` | `.zcp/workflow.sh intent` | Workflow intent/goal |
| `zcp_context.json` | Auto-captured | Last error, notes |

### Gate Evidence Files

| File | Gate | Created By | Purpose |
|------|------|------------|---------|
| `recipe_review.json` | Gate 0 | `.zcp/recipe-search.sh quick` | Recipe patterns, versions, validation rules |
| `import_validated.json` | Gate 0.5 | `.zcp/validate-import.sh` | Import file validation |
| `discovery.json` | Gate 1 | `.zcp/workflow.sh create_discovery` | Dev/stage service mapping |
| `dev_verify.json` | Gate 2 | `.zcp/verify.sh {dev}` | Dev endpoint test results |
| `config_validated.json` | Gate 2 | Integrated in DEVELOP→DEPLOY | Config validation (automatic) |
| `deploy_evidence.json` | Gate 3 | `.zcp/status.sh --wait` | Deployment completion proof |
| `stage_verify.json` | Gate 4 | `.zcp/verify.sh {stage}` | Stage endpoint test results |
| `services_imported.json` | — | `.zcp/workflow.sh extend` | Import audit trail |

### Bootstrap Evidence Files

| File | Created By | Purpose |
|------|------------|---------|
| `bootstrap_plan.json` | `.zcp/workflow.sh bootstrap` | Runtime + services specification |
| `bootstrap_import.yml` | Bootstrap orchestrator | Generated import.yml |
| `bootstrap_coordination.json` | Bootstrap orchestrator | Checkpoint tracking for resume |
| `service_discovery.json` | `discover-services` step | Actual env vars from running services |
| `bootstrap_handoff.json` | `finalize` step | Per-service handoff data for subagents |
| `{hostname}_complete.json` | Subagent (Task 17) | URLs, verification data for handoff |
| `bootstrap_complete.json` | `aggregate-results` step | Bootstrap completion evidence |
| `workflow_state.json` | Auto-generated | WIGGUM workflow state |

---

## Quick Reference

### Command Cheatsheet

| Action | Command |
|--------|---------|
| **Start** | `.zcp/workflow.sh init` |
| **Check state** | `.zcp/workflow.sh show` |
| **Recipe review** | `.zcp/recipe-search.sh quick {runtime} [service]` |
| **Transition** | `.zcp/workflow.sh transition_to {PHASE}` |
| **Record discovery** | `.zcp/workflow.sh create_discovery {dev_id} {dev} {stage_id} {stage}` |
| **Verify endpoints** | `.zcp/verify.sh {service} {port} / /health` |
| **Wait for deploy** | `.zcp/status.sh --wait {service}` |
| **Continue after DONE** | `.zcp/workflow.sh iterate "summary"` |
| **Get help** | `.zcp/workflow.sh --help [topic]` |

### Mode Selection

| Situation | Mode | Command |
|-----------|------|---------|
| Production deployment | Full | `init` |
| Prototyping | Dev-only | `init --dev-only` |
| Emergency fix | Hotfix | `init --hotfix` |
| Exploration | Quick | `--quick` |
| New project | Bootstrap | `bootstrap --runtime X --services Y` |

### Service Access

| Service Type | Access Method | Example |
|--------------|---------------|---------|
| Runtime | SSH + SSHFS | `ssh appdev "go build"` |
| PostgreSQL | psql | `psql "$db_connectionString"` |
| Valkey/Redis | redis-cli | `redis-cli -h valkey` |
| MySQL | mysql | `mysql -h mysql -u $db_user -p` |

### Variable Patterns

| Context | Pattern | Example |
|---------|---------|---------|
| ZCP accessing service var | `${service}_VAR` | `${appdev_PORT}` |
| Inside service via SSH | `$VAR` | `ssh appdev "echo \$PORT"` |
| Database connection | `$db_*` | `$db_connectionString` |

---

## Design Principles

| Principle | Implementation |
|-----------|----------------|
| **Evidence over trust** | Gates check JSON files, not agent assertions |
| **Session isolation** | Evidence contains session ID; stale = rejected |
| **Self-documenting** | All tools have `--help`; survives context loss |
| **Dev first** | Fix errors on dev, stage is for validation only |
| **Atomic operations** | File writes use temp+mv pattern |
| **Persistent state** | `.zcp/state/` survives container restart |
| **Agent visibility** | Bootstrap returns control after each step |

---

## Common Patterns

### Deploy from Dev to Stage

```bash
# Always deploy FROM dev container TO stage service
ssh {dev} "zcli push {stage_service_id} --setup=prod"

# Never deploy from ZCP directly - source files are on dev container
```

### Verify Before Deploy

```bash
# Run on dev first
.zcp/verify.sh appdev 8080 / /api/health /api/status

# Check response content, not just HTTP 200
# HTTP 200 with error body = still broken
```

### Handle Long-Running Processes

```bash
# SSH will hang if process runs in foreground
# Use background execution
ssh appdev "nohup ./myapp &"

# Or use Zerops runtime management
ssh appdev "zsc start"
```

---

## Critical Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | `zeropsSubdomain` is already full URL | Don't prepend protocol |
| SSH connection refused | Managed service (db, cache) | Use client tools from ZCP |
| SSH hangs forever | Foreground process | Set `run_in_background=true` |
| Variable empty | Wrong prefix | Use `${hostname}_VAR` from ZCP |
| Files missing on stage | Not in `deployFiles` | Update zerops.yaml |
| zcli no results | Missing project flag | Use `-P $projectId` |

---

## Key Features

- **Session-scoped evidence** — Prevents stale evidence from previous sessions
- **Atomic file writes** — Temp file + mv pattern prevents corruption
- **Multi-platform date parsing** — Supports both GNU and BSD date
- **Evidence freshness** — Evidence >24h old is blocked (except in hotfix mode)
- **Single-service mode** — Explicit opt-in for dev=stage with risk acknowledgment
- **Backward transitions** — Go back phases with automatic evidence invalidation
- **Self-documenting** — All tools have `--help` with contextual guidance
- **Persistent storage** — State survives container restarts via `.zcp/state/`
- **Automatic context capture** — Verification failures and deploy errors recorded automatically
- **Iteration support** — Continue work after DONE without losing history
- **Rich context** — Intent and notes for better resumability after long breaks
- **BSD-compatible locking** — File locking works on both Linux (flock) and macOS (mkdir)
- **Write-through caching** — Evidence written to both temp dir and persistent storage
- **Graceful degradation** — Falls back to temp dir if persistent storage unavailable
- **Configurable temp directory** — Set `ZCP_TMP_DIR` env var to override default `/tmp/`

---

## Claude Code Integration

The `.claude/settings.json` file pre-authorizes common commands:
- Workflow tools: `workflow.sh`, `verify.sh`, `status.sh`
- SSH and zcli operations
- Database clients: `psql`, `mysql`, `redis-cli`
- Build tools: `npm`, `node`, `go`
- Browser testing: `agent-browser`

---

## For Agent Developers

### Adding New Commands

1. Create handler in `.zcp/lib/commands/{name}.sh`
2. Source in `commands.sh` loader
3. Add case in `workflow.sh` command router
4. Add help topic in `.zcp/lib/help/topics.sh` (optional)

### Adding New Gates

1. Add evidence file path in `.zcp/lib/utils.sh`
2. Add check function in `.zcp/lib/gates.sh`
3. Wire into `transition.sh` logic
4. Create command/script that generates evidence (if not standalone)
5. Register command in `workflow.sh` and `commands.sh`
6. Document required evidence in help and README

### Testing Changes

Run workflows in quick mode (`--quick`) to bypass gates during development, then test full mode for gate validation.

```bash
# Syntax check all scripts
cd .zcp && bash -n workflow.sh && bash -n lib/utils.sh && bash -n lib/commands/*.sh
```

### Utility Functions (lib/utils.sh)

Key functions available for new commands:

| Function | Purpose |
|----------|---------|
| `get_session`, `get_mode`, `get_phase` | Read current state |
| `get_iteration`, `set_iteration` | Manage iteration counter |
| `set_phase` | Update workflow phase |
| `safe_write_json` | Atomic JSON write with error handling |
| `write_evidence` | Write-through to /tmp/ and persistent storage |
| `auto_capture_context` | Record errors for workflow continuity |
| `with_lock` | BSD-compatible file locking |
| `check_evidence_session` | Validate evidence belongs to current session |
| `check_evidence_freshness` | Warn if evidence >24h old |
| `sync_to_persistent` | Sync /tmp/ state to persistent storage |
| `restore_from_persistent` | Restore state after container restart |
| `init_persistent_storage` | Initialize .zcp/state/ directories |

### Evidence File Variables (lib/utils.sh)

```bash
# Configurable temp directory (defaults to /tmp)
ZCP_TMP_DIR="${ZCP_TMP_DIR:-/tmp}"

# Gate evidence files
DISCOVERY_FILE="${ZCP_TMP_DIR}/discovery.json"
DEV_VERIFY_FILE="${ZCP_TMP_DIR}/dev_verify.json"
STAGE_VERIFY_FILE="${ZCP_TMP_DIR}/stage_verify.json"
DEPLOY_EVIDENCE_FILE="${ZCP_TMP_DIR}/deploy_evidence.json"

# Gate 0 and 0.5 evidence
RECIPE_REVIEW_FILE="${ZCP_TMP_DIR}/recipe_review.json"
IMPORT_VALIDATED_FILE="${ZCP_TMP_DIR}/import_validated.json"
SERVICES_IMPORTED_FILE="${ZCP_TMP_DIR}/services_imported.json"
CONFIG_VALIDATED_FILE="${ZCP_TMP_DIR}/config_validated.json"

# WIGGUM state files
WORKFLOW_STATE_FILE="${ZCP_TMP_DIR}/workflow_state.json"

# Bootstrap evidence files
BOOTSTRAP_PLAN_FILE="${ZCP_TMP_DIR}/bootstrap_plan.json"
BOOTSTRAP_IMPORT_FILE="${ZCP_TMP_DIR}/bootstrap_import.yml"
BOOTSTRAP_COORDINATION_FILE="${ZCP_TMP_DIR}/bootstrap_coordination.json"
BOOTSTRAP_HANDOFF_FILE="${ZCP_TMP_DIR}/bootstrap_handoff.json"
BOOTSTRAP_COMPLETE_FILE="${ZCP_TMP_DIR}/bootstrap_complete.json"
```

---

## Technology Stack

- **Shell**: Bash 4+ (GNU/BSD compatible)
- **Data formats**: JSON (via `jq`), YAML (via `yq`)
- **CLI tools**: `zcli` (Zerops CLI), `psql`, `mysql`, `redis-cli`, `curl`
- **Browser testing**: `agent-browser` for visual verification

---

WIGGUM = Workflow Infrastructure for Guided Gates and Unified Management
