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
│   ├── verify.sh          # Endpoint testing with evidence + auto context capture
│   ├── status.sh          # Deployment status + wait mode + auto context capture
│   └── lib/               # Modular components (AI-readable)
│       ├── utils.sh       # State management, persistence, locking, context capture
│       ├── gates.sh       # Phase transition gate checks
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
│           ├── extend.sh     # extend, upgrade-to-full, record_deployment
│           ├── iterate.sh    # iterate command (post-DONE continuation)
│           ├── retarget.sh   # retarget command (change deployment target)
│           └── context.sh    # intent, note commands (rich context)
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

## Gates

Each transition requires evidence from the current session:

| Transition | Required Evidence |
|------------|-------------------|
| DISCOVER → DEVELOP | `discovery.json` with matching session_id, dev ≠ stage |
| DEVELOP → DEPLOY | `dev_verify.json` with 0 failures |
| DEPLOY → VERIFY | `deploy_evidence.json` from .zcp/status.sh --wait |
| VERIFY → DONE | `stage_verify.json` with 0 failures |

Backward transitions (`--back`) invalidate downstream evidence.

## Commands

```bash
# Initialize
.zcp/workflow.sh init                    # Full mode with gates
.zcp/workflow.sh init --dev-only         # No deployment phase
.zcp/workflow.sh init --hotfix           # Skip to DEVELOP, reuse discovery
.zcp/workflow.sh --quick                 # No enforcement

# Transitions
.zcp/workflow.sh transition_to DEVELOP
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
.zcp/workflow.sh extend {import.yml}     # Add services to project
.zcp/workflow.sh upgrade-to-full         # Convert dev-only to full deployment

# Help
.zcp/workflow.sh --help
.zcp/workflow.sh --help {topic}          # discover, develop, deploy, verify, done,
                                         # vars, services, trouble, example, gates,
                                         # extend, bootstrap
```

## Evidence Files

All stored in `/tmp/` (with write-through to `.zcp/state/` for persistence):

| File | Created By | Contains |
|------|------------|----------|
| `claude_session` | `.zcp/workflow.sh init` | Session ID |
| `claude_mode` | `.zcp/workflow.sh init` | full, dev-only, hotfix, quick |
| `claude_phase` | `.zcp/workflow.sh transition_to` | Current phase |
| `claude_iteration` | `.zcp/workflow.sh iterate` | Current iteration number |
| `claude_context.json` | Auto-captured | Last error, notes |
| `claude_intent.txt` | `.zcp/workflow.sh intent` | Workflow intent |
| `discovery.json` | `.zcp/workflow.sh create_discovery` | Dev/stage service mapping |
| `dev_verify.json` | `.zcp/verify.sh {dev}` | Dev endpoint test results |
| `stage_verify.json` | `.zcp/verify.sh {stage}` | Stage endpoint test results |
| `deploy_evidence.json` | `.zcp/status.sh --wait` | Deployment completion proof |

## Key Features

- **Session-scoped evidence** — Prevents stale evidence from previous sessions
- **Atomic file writes** — Temp file + mv pattern prevents corruption
- **Multi-platform date parsing** — Supports both GNU and BSD date
- **Staleness warnings** — Evidence >24h old triggers warning (not blocker)
- **Single-service mode** — Explicit opt-in for dev=stage with risk acknowledgment
- **Backward transitions** — Go back phases with automatic evidence invalidation
- **Self-documenting** — All tools have `--help` with contextual guidance

### Resilient Workflow Features (New)

- **Persistent storage** — State survives container restarts via `.zcp/state/`
- **Automatic context capture** — Verification failures and deploy errors recorded automatically
- **Iteration support** — Continue work after DONE without losing history
- **Rich context** — Intent and notes for better resumability after long breaks
- **BSD-compatible locking** — File locking works on both Linux (flock) and macOS (mkdir)
- **Write-through caching** — Evidence written to both `/tmp/` and persistent storage
- **Graceful degradation** — Falls back to `/tmp/` if persistent storage unavailable

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

1. Add check function in `.zcp/lib/gates.sh`
2. Wire into `transition.sh` logic
3. Document required evidence in help

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
| `set_phase`, `set_iteration` | Update state |
| `safe_write_json` | Atomic JSON write with error handling |
| `write_evidence` | Write-through to /tmp/ and persistent storage |
| `auto_capture_context` | Record errors for workflow continuity |
| `with_lock` | BSD-compatible file locking |
| `check_evidence_session` | Validate evidence belongs to current session |
| `check_evidence_freshness` | Warn if evidence >24h old |
