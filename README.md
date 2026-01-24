# Zerops Platform Agent System (ZCP)

An AI agent workflow orchestration framework for the Zerops PaaS platform. Guides AI agents (primarily Claude) through structured, evidence-based development and deployment workflows.

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
All paths in this document assume you're working within the `zcp/` directory.

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

## Architecture

```
CLAUDE.md                     → Platform fundamentals, always in context
    │
    ▼
.zcp/workflow.sh --help       → Full reference, contextual phase guidance
.zcp/verify.sh / status.sh    → Self-documenting tools
```

Knowledge lives in tools via `--help`, surviving context compaction and subagent spawning.

### Design Philosophy

1. **Evidence over trust** — Gates check for JSON evidence files, not agent assertions
2. **Session isolation** — Evidence files contain session IDs; stale evidence is rejected
3. **Atomic operations** — File writes use temp+mv pattern to prevent corruption
4. **Self-documentation** — All tools have `--help`; agents never need external docs

## Files

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
│       ├── state.sh       # WIGGUM¹ state management (synthesis mode)
│       ├── help.sh        # Help system loader
│       ├── commands.sh    # Commands loader
│       ├── help/
│       │   ├── full.sh    # Full platform reference
│       │   └── topics.sh  # Topic-specific help (discover, develop, etc.)
│       └── commands/
│           ├── init.sh       # init, quick commands
│           ├── transition.sh # transition_to, phase guidance
│           ├── discovery.sh  # create_discovery, refresh_discovery
│           ├── status.sh     # show, complete, reset
│           ├── extend.sh     # extend, upgrade-to-full, record_deployment, import evidence
│           ├── iterate.sh    # iterate command (post-DONE continuation)
│           ├── retarget.sh   # retarget command (change deployment target)
│           ├── context.sh    # intent, note commands (rich context)
│           └── compose.sh    # Synthesis commands (compose, verify_synthesis)
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

The modular structure allows AI agents to read only relevant files when investigating specific functionality.

## Workflow Modes

### Full Mode (default)
```
INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
```

### Dev-Only Mode
```
INIT → DISCOVER → DEVELOP → DONE
```
For rapid prototyping without deployment. Upgrade later with `upgrade-to-full`.

### Hotfix Mode
```
DEVELOP → DEPLOY → VERIFY → DONE
```
Reuses recent discovery (<24h), skips dev verification. For urgent fixes.

### Quick Mode
No gates, no enforcement. For exploration and debugging.

### Synthesis Mode (Bootstrap from Scratch)
```
INIT → COMPOSE → EXTEND → SYNTHESIZE → DEVELOP → DEPLOY → VERIFY → DONE
```
For creating new services from scratch when no runtime services exist. Generates infrastructure definitions and validates agent-created code before standard development flow.

## Bootstrap vs Standard Flow (Auto-Detected)

The workflow **automatically detects** whether runtime services exist and provides appropriate guidance:

| Situation | Detection | Flow |
|-----------|-----------|------|
| **Services exist** | `zcli service list` finds runtime services | Standard: just record discovery |
| **No services** | No runtime services found | Bootstrap: must create services first |

**How it works:**
1. Run `.zcp/workflow.sh init`
2. Run `.zcp/workflow.sh transition_to DISCOVER`
3. The system checks for existing runtime services
4. Based on detection, shows either **Standard** or **Bootstrap** guidance

### Standard Flow (Services Exist)

```
INIT → recipes → DISCOVER → record discovery → DEVELOP → ...
```

When runtime services already exist, you skip service creation:
1. Review recipes (Gate 0)
2. List services: `zcli service list -P $projectId`
3. Record: `.zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}`

### Bootstrap Flow (No Services)

```
INIT → recipes → plan → import.yml → extend → DISCOVER → record → DEVELOP → ...
```

When no runtime services exist, you must create them:
1. **Review recipes** (Gate 0): `.zcp/recipe-search.sh quick {runtime} [managed-service]`
2. **Create import.yml** with `zeropsSetup` linking services to zerops.yml configs
3. **Import services**: `.zcp/workflow.sh extend import.yml`
4. **Restart ZCP** to get new environment variables
5. **Record discovery**: `.zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage`

### Key Insight

> **There is no "bootstrap" phase.** Projects always start with ZCP already present.
> The workflow detects project state and guides appropriately.
> **Init = Extend** — always adding to an existing project.

## Typical Workflow

A complete workflow with all gates looks like this:

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

**Minimum required flow**: Steps 1, 2, 3, 4, 5, 6, 7, 8

## Gates

The workflow enforces quality gates at each phase transition.

### Gate Overview

| Gate | Transition | Evidence File | Created By | Prevents |
|------|------------|---------------|------------|----------|
| **Gate 0** | INIT → DISCOVER | `recipe_review.json` | `.zcp/recipe-search.sh quick` | Invalid versions, wrong YAML structure |
| **Gate 0.5** | extend command | `import_validated.json` | `.zcp/validate-import.sh` | Invalid import.yml structure |
| **Gate 1** | DISCOVER → DEVELOP | `discovery.json` | `.zcp/workflow.sh create_discovery` | Wrong service targeting |
| **Gate 2** | DEVELOP → DEPLOY | `dev_verify.json` + config | `.zcp/verify.sh {dev}` | Deploying broken code, config errors |
| **Gate 3** | DEPLOY → VERIFY | `deploy_evidence.json` | `.zcp/status.sh --wait` | Verifying incomplete deploy |
| **Gate 4** | VERIFY → DONE | `stage_verify.json` | `.zcp/verify.sh {stage}` | Shipping broken features |

**Synthesis Mode Gates** (used when bootstrapping new services):

| Gate | Transition | Evidence File | Created By | Prevents |
|------|------------|---------------|------------|----------|
| **Gate C→E** | COMPOSE → EXTEND | `synthesis_plan.json` | `.zcp/workflow.sh compose` | Missing infrastructure plan |
| **Gate E→S** | EXTEND → SYNTHESIZE | `services_imported.json` | `.zcp/workflow.sh extend` | Deploying to non-existent services |
| **Gate S** | SYNTHESIZE → DEVELOP | `synthesis_complete.json` | `.zcp/workflow.sh verify_synthesis` | Invalid code structure |

**Bold gates** are enforced (blocking). Others are optional but create audit trails.

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

The DISCOVER transition is blocked until recipe review is complete.

### Gate 3: Config Validation

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

Backward transitions (`--back`) invalidate downstream evidence.

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

# Synthesis (Bootstrap from scratch)
.zcp/workflow.sh compose --runtime {rt} [--services {s}]  # Generate synthesis plan
.zcp/workflow.sh verify_synthesis                          # Validate synthesized code

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
                                         # extend, bootstrap, cheatsheet, import-validation, synthesis
```

## Evidence Files

All stored in `$ZCP_TMP_DIR` (defaults to `/tmp/`, with write-through to `.zcp/state/` for persistence):

### State Files
| File | Created By | Contains |
|------|------------|----------|
| `claude_session` | `.zcp/workflow.sh init` | Session ID |
| `claude_mode` | `.zcp/workflow.sh init` | full, dev-only, hotfix, quick |
| `claude_phase` | `.zcp/workflow.sh transition_to` | Current phase |
| `claude_iteration` | `.zcp/workflow.sh iterate` | Current iteration number |
| `claude_intent.txt` | `.zcp/workflow.sh intent` | Workflow intent/goal |
| `claude_context.json` | Auto-captured | Last error, notes |

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
| `services_imported.json` | Synthesis | `.zcp/workflow.sh extend` | Import audit trail |
| `synthesis_plan.json` | Gate C→E | `.zcp/workflow.sh compose` | Service topology, env mappings |
| `synthesized_import.yml` | Gate C→E | `.zcp/workflow.sh compose` | Import file for extend |
| `synthesis_complete.json` | Gate S | `.zcp/workflow.sh verify_synthesis` | Code structure validation |
| `workflow_state.json` | — | Auto-generated | WIGGUM workflow state (synthesis mode) |

## Key Features

- **Session-scoped evidence** — Prevents stale evidence from previous sessions
- **Atomic file writes** — Temp file + mv pattern prevents corruption
- **Multi-platform date parsing** — Supports both GNU and BSD date
- **Evidence freshness** — Evidence >24h old is blocked (except in hotfix mode)
- **Single-service mode** — Explicit opt-in for dev=stage with risk acknowledgment
- **Backward transitions** — Go back phases with automatic evidence invalidation
- **Self-documenting** — All tools have `--help` with contextual guidance

### Resilient Workflow Features (New)

- **Persistent storage** — State survives container restarts via `.zcp/state/`
- **Automatic context capture** — Verification failures and deploy errors recorded automatically
- **Iteration support** — Continue work after DONE without losing history
- **Rich context** — Intent and notes for better resumability after long breaks
- **BSD-compatible locking** — File locking works on both Linux (flock) and macOS (mkdir)
- **Write-through caching** — Evidence written to both temp dir and persistent storage
- **Graceful degradation** — Falls back to temp dir if persistent storage unavailable
- **Configurable temp directory** — Set `ZCP_TMP_DIR` env var to override default `/tmp/`

## Environment Context

**Zerops** is a PaaS where projects contain services (containers) on a shared private network.

**ZCP (Zerops Control Plane)** is the agent's workspace — a privileged container with:
- **SSHFS mounts** to runtime service filesystems at `/var/www/{service}/`
- **SSH access** to runtime containers (NOT managed services like databases)
- **Direct network access** to all services via hostname
- **Pre-installed tools**: `jq`, `yq`, `psql`, `mysql`, `redis-cli`, `zcli`, `agent-browser`

### Service Types

| Type | SSH | SSHFS | Access From ZCP |
|------|-----|-------|-----------------|
| **Runtime** (go, nodejs, php, python) | ✓ | ✓ | `ssh {service} "command"` |
| **Managed** (postgresql, valkey, nats) | ✗ | ✗ | `psql "$db_connectionString"` |

### Variable Patterns

| Context | Pattern | Example |
|---------|---------|---------|
| ZCP accessing service var | `${service}_VAR` | `${appdev_PORT}` |
| Inside service via SSH | `$VAR` | `ssh appdev "echo \$PORT"` |
| Database connection | `$db_*` | `$db_connectionString` |

## Technology Stack

- **Shell**: Bash 4+ (GNU/BSD compatible)
- **Data formats**: JSON (via `jq`), YAML (via `yq`)
- **CLI tools**: `zcli` (Zerops CLI), `psql`, `mysql`, `redis-cli`, `curl`
- **Browser testing**: `agent-browser` for visual verification

## Critical Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | `zeropsSubdomain` is already full URL | Don't prepend protocol |
| SSH connection refused | Managed service (db, cache) | Use client tools from ZCP |
| SSH hangs forever | Foreground process | Set `run_in_background=true` |
| Variable empty | Wrong prefix | Use `${hostname}_VAR` from ZCP |
| Files missing on stage | Not in `deployFiles` | Update zerops.yaml |
| zcli no results | Missing project flag | Use `-P $projectId` |

## Deployment Pattern

```bash
# Always deploy FROM dev container TO stage service
ssh {dev} "zcli push {stage_service_id} --setup={setup}"

# Never deploy from ZCP directly - source files are on dev container
```

## Workflow Continuity

The system is designed for interrupted, non-linear work across sessions.

### Recovery After Interruption

```bash
# Always run this when starting or resuming work
.zcp/workflow.sh show

# For extended context (intent, notes, history)
.zcp/workflow.sh show --full
```

The system automatically captures:
- Verification failures (endpoint, status code, response body)
- Deploy failures and timeouts
- All displayed in `show` output

### Iteration After DONE

When a workflow completes but you need changes:

```bash
# Start new iteration (archives current evidence, resets to DEVELOP)
.zcp/workflow.sh iterate "Fix delete button"

# Skip directly to VERIFY (no code changes needed)
.zcp/workflow.sh iterate --to VERIFY "Fix CSS alignment"

# Skip to DEPLOY (only need to redeploy)
.zcp/workflow.sh iterate --to DEPLOY "Config change"
```

### Changing Targets Mid-Workflow

```bash
# Change stage target without full reset
.zcp/workflow.sh retarget stage {new_service_id} {new_service_name}
# Preserves dev verification, invalidates deploy/stage evidence
```

### Rich Context for Resumability

```bash
# Set intent at workflow start
.zcp/workflow.sh intent "Add user authentication with JWT"

# Add notes when encountering issues
.zcp/workflow.sh note "Token encoding might be wrong - check jwt.go:42"
```

### Persistence

State survives container restarts via `.zcp/state/`:
- Restored automatically on startup if `/tmp/` is empty
- Write-through: every state change writes to both locations
- Keeps last 10 iterations, auto-cleans older ones

## Claude Code Integration

The `.claude/settings.json` file pre-authorizes common commands:
- Workflow tools: `workflow.sh`, `verify.sh`, `status.sh`
- SSH and zcli operations
- Database clients: `psql`, `mysql`, `redis-cli`
- Build tools: `npm`, `node`, `go`
- Browser testing: `agent-browser`

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

# WIGGUM state files (Synthesis mode)
WORKFLOW_STATE_FILE="${ZCP_TMP_DIR}/workflow_state.json"
SYNTHESIS_PLAN_FILE="${ZCP_TMP_DIR}/synthesis_plan.json"
SYNTHESIS_COMPLETE_FILE="${ZCP_TMP_DIR}/synthesis_complete.json"
SYNTHESIZED_IMPORT_FILE="${ZCP_TMP_DIR}/synthesized_import.yml"
```

---

¹ **WIGGUM** = Workflow Infrastructure for Guided Gates and Unified Management
