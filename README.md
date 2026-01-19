# Zerops Platform Agent System

Guides AI agents operating within the Zerops deployment environment.

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

## Files

```
zcp/
├── CLAUDE.md              # Entry point for agents
├── .zcp/                  # Workflow tools
│   ├── workflow.sh        # Phase orchestration + help system
│   ├── verify.sh          # Endpoint testing with evidence
│   └── status.sh          # Deployment status + wait mode
└── .claude/
    └── settings.json      # Claude Code permissions
```

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
.zcp/workflow.sh reset                   # Clear everything
.zcp/workflow.sh reset --keep-discovery  # Preserve discovery for new session

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

All stored in `/tmp/` with session tracking:

| File | Created By | Contains |
|------|------------|----------|
| `claude_session` | `.zcp/workflow.sh init` | Session ID |
| `claude_mode` | `.zcp/workflow.sh init` | full, dev-only, hotfix, quick |
| `claude_phase` | `.zcp/workflow.sh transition_to` | Current phase |
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
