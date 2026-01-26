# ZCP System Overview

> **Visual architecture guide for the Zerops Control Plane agent workflow system.**
>
> This document provides architectural diagrams and decision flows. For complete command reference, evidence file details, and implementation specifics, see the main [README](../README.md).

---

## Document Purpose

| Reading Mode | Use Case |
|--------------|----------|
| **Standalone** | Quick architectural understanding, agent onboarding, flow visualization |
| **With README** | Complete system comprehension, reference lookup during implementation |

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
│  │(instant)│    │  (2-3 sec)   │    │    (instant)    │    │   (instant)   │    │
│  └─────────┘    └──────────────┘    └─────────────────┘    └───────────────┘    │
│                                                                   │              │
│  ┌──────────────────────────────────────────────────────────────┐ │              │
│  │                                                              │ │              │
│  │  ┌──────────┐    ┌───────────┐    ┌──────────┐              │ │              │
│  │  │ finalize │◄───│ mount-dev │◄───│wait-svc  │◄─────────────┘ │              │
│  │  │(instant) │    │ (instant) │    │ (poll)   │                │              │
│  │  └──────────┘    └───────────┘    └──────────┘                │              │
│  │       │                                                       │              │
│  └───────┼───────────────────────────────────────────────────────┘              │
│          │                                                                       │
│          ▼                                                                       │
│  Agent writes code, deploys, then:                                              │
│  .zcp/workflow.sh bootstrap-done                                                │
│          │                                                                       │
│          ▼                                                                       │
│  Standard Workflow unlocked                                                      │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### Step-by-Step Execution

```bash
# Initialize (creates plan, returns immediately)
.zcp/workflow.sh bootstrap --runtime go --services postgresql

# Agent runs steps individually for visibility
.zcp/bootstrap.sh step recipe-search      # Fetch patterns (2-3 sec)
.zcp/bootstrap.sh step generate-import    # Create import.yml (instant)
.zcp/bootstrap.sh step import-services    # Send to Zerops API (instant)
.zcp/bootstrap.sh step wait-services      # Poll until services ready
.zcp/bootstrap.sh step mount-dev appdev   # SSHFS mount (instant)
.zcp/bootstrap.sh step finalize           # Create handoff data

# Agent writes code, deploys, then marks complete
.zcp/workflow.sh bootstrap-done
```

### Bootstrap Response Format

Every step returns structured JSON for agent parsing:

```json
{
  "status": "complete|in_progress|failed",
  "step": "step-name",
  "data": { "...step-specific..." },
  "next": "next-step-name",
  "message": "Human readable status"
}
```

### Bootstrap Scenarios

| Scenario | Command/Action | Outcome |
|----------|---------------|---------|
| Fresh project | `bootstrap --runtime go` | Creates dev/stage pairs |
| Interrupted | `.zcp/bootstrap.sh resume` | Returns next pending step |
| Step fails | Fix issue, re-run same step | Idempotent recovery |
| Check progress | `.zcp/bootstrap.sh status` | Shows checkpoint, pending steps |
| Already conformant | `bootstrap ...` | Returns "use init" guidance |

---

## Gate System

Gates enforce quality checkpoints. Each gate blocks until evidence exists.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              GATE ENFORCEMENT                                    │
├────────┬─────────────────────────┬─────────────────────────┬────────────────────┤
│ Gate   │ Transition              │ Evidence File           │ Created By         │
├────────┼─────────────────────────┼─────────────────────────┼────────────────────┤
│ 0      │ INIT → DISCOVER         │ recipe_review.json      │ recipe-search.sh   │
│ 0.5    │ extend command          │ import_validated.json   │ validate-import.sh │
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

## Further Reading

- **[README.md](../README.md)** — Complete reference: all commands, evidence files, troubleshooting
- **[CLAUDE.md](../zcp/CLAUDE.md)** — Agent entry point with platform fundamentals
- **`.zcp/workflow.sh --help`** — Contextual help for current phase
- **`.zcp/workflow.sh --help {topic}`** — Topic-specific help (discover, develop, deploy, verify, gates, bootstrap)
