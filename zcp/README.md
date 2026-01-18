# Zerops Platform Agent System

## Overview

This is a complete implementation of the Zerops Platform Agent specification system designed to guide AI agents operating within the Zerops deployment environment. The system is context-resilient, surviving context compaction and subagent spawning.

## Core Insight

**Documents don't survive context compaction or subagent spawning. Tools do.**

The knowledge lives in the tools themselves via self-documenting help systems, not just in documentation files.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  TIER 1: Mini CLAUDE.md (~50 lines)                         │
│  - Survives ANY context window                              │
│  - Platform fundamentals only                               │
│  - Points to tools for full reference                       │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  TIER 2: Self-Documenting Tools                             │
│  - workflow.sh --help (full reference, ~300 lines)          │
│  - workflow.sh transition_to X (contextual guidance)        │
│  - verify.sh --help, status.sh --help                       │
│  - Tools ALWAYS available regardless of context             │
└─────────────────────────────────────────────────────────────┘
```

## File Structure

```
zcp/
├── README.md           # This file
├── CLAUDE.md           # Tier 1: Mini spec (48 lines)
├── .claude/
│   ├── workflow.sh     # Phase management + embedded help
│   ├── verify.sh       # Endpoint testing + help
│   └── status.sh       # Deployment status + help
```

## Components

### 1. Mini CLAUDE.md (48 lines)

The absolute minimum platform knowledge that survives any context window state.

**Contains:**
- ZCP orientation (you're on control plane, not in containers)
- SSHFS/SSH pattern distinction
- Variable patterns table
- Critical warnings (zeropsSubdomain, variable timing)
- Tool locations and quick start

**Spec compliance:** ✅ 48/50 lines (under limit)

### 2. workflow.sh

Self-documenting phase orchestration with enforcement gates.

**Commands:**
- `init` - Start enforced workflow with session management
- `--quick` - Quick mode with no enforcement gates
- `--help [topic]` - Full reference (~300 lines) or topic-specific help
- `transition_to {phase}` - Advance phase with contextual guidance
- `create_discovery` - Record service discovery
- `show` - Current workflow status
- `complete` - Verify evidence and output completion promise
- `reset` - Clear all state

**Help Topics:**
- `discover`, `develop`, `deploy`, `verify`, `done`
- `vars`, `trouble`, `example`, `gates`

**Features:**
- ✅ Session management with unique IDs
- ✅ Evidence validation with session tracking
- ✅ Gate enforcement (full mode)
- ✅ Contextual guidance per phase
- ✅ Idempotent init
- ✅ Actionable error messages
- ✅ Complete troubleshooting table
- ✅ End-to-end example embedded

### 3. verify.sh

Endpoint verification with JSON evidence generation.

**Features:**
- ✅ HTTP endpoint testing via SSH + curl
- ✅ JSON evidence with session tracking
- ✅ Auto-copy to role-specific files (dev/stage)
- ✅ Frontend detection with browser testing reminder
- ✅ Debug mode with verbose output
- ✅ Pass/fail counting
- ✅ Self-documenting help

**Evidence Format:**
```json
{
  "session_id": "20260118213115-14322-17250",
  "service": "appstage",
  "port": 8080,
  "timestamp": "2026-01-18T21:31:38Z",
  "results": [
    {"endpoint": "/", "status": 201, "pass": true}
  ],
  "passed": 1,
  "failed": 0
}
```

### 4. status.sh

Deployment status checking with optional wait mode.

**Features:**
- ✅ Service list display
- ✅ Running/pending processes
- ✅ Recent notifications
- ✅ Wait mode with polling
- ✅ Unicode table parsing (zcli compatibility)
- ✅ Deployment status logic
- ✅ Timeout support
- ✅ Self-documenting help

## Workflow Phases

```
INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
```

### Phase Flow

1. **INIT** - Initialize session
2. **DISCOVER** - Authenticate and discover services
3. **DEVELOP** - Build and test on dev service
4. **DEPLOY** - Deploy to stage service
5. **VERIFY** - Verify deployment on stage
6. **DONE** - Complete workflow with evidence validation

### Gates (Enforcement)

**DISCOVER → DEVELOP:**
- `/tmp/discovery.json` exists with current session
- `deploy_target != dev service name`

**DEVELOP → DEPLOY:**
- `/tmp/dev_verify.json` exists with current session
- `failures == 0`

**DEPLOY → VERIFY:**
- Manual check via `status.sh` or `zcli`

**VERIFY → DONE:**
- `/tmp/stage_verify.json` exists with current session
- `failures == 0`

## Design Principles Implemented

✅ **Tools Over Documents** - All critical knowledge accessible via tool output
✅ **Contextual Guidance** - Right information at the right time
✅ **Fail-Safe Defaults** - Gates block progress until evidence exists
✅ **Platform Only** - Covers platform mechanics, not user app logic
✅ **Explicit Over Implicit** - States things explicitly with warnings
✅ **Executable Scripts** - All commands are executable scripts, not sourced functions

## Usage Examples

### Quick Start (No Enforcement)

```bash
/var/www/.claude/workflow.sh --quick
# Work freely without gates
```

### Full Workflow (With Enforcement)

```bash
# 1. Initialize
/var/www/.claude/workflow.sh init

# 2. Discover services
zcli service list -P $projectId
/var/www/.claude/workflow.sh create_discovery \
    "abc123" "appdev" "def456" "appstage"
/var/www/.claude/workflow.sh transition_to DISCOVER

# 3. Develop
/var/www/.claude/workflow.sh transition_to DEVELOP
# ... build and test ...
/var/www/.claude/verify.sh appdev 8080 / /status /api/items

# 4. Deploy
/var/www/.claude/workflow.sh transition_to DEPLOY
# ... pre-deployment checks ...
ssh appdev "zcli push stage_id --setup=api"
/var/www/.claude/status.sh --wait appstage

# 5. Verify
/var/www/.claude/workflow.sh transition_to VERIFY
/var/www/.claude/verify.sh appstage 8080 / /status /api/items

# 6. Complete
/var/www/.claude/workflow.sh transition_to DONE
/var/www/.claude/workflow.sh complete
```

### Get Help Anytime

```bash
/var/www/.claude/workflow.sh --help          # Full reference
/var/www/.claude/workflow.sh --help deploy   # Deploy phase
/var/www/.claude/workflow.sh --help trouble  # Troubleshooting
/var/www/.claude/verify.sh --help            # Verify help
/var/www/.claude/status.sh --help            # Status help
```

## Key Features

### Context Resilience
- **Mini CLAUDE.md survives any compaction** (48 lines)
- **Tools always callable** regardless of context state
- **Help systems embedded** in the scripts themselves
- **Subagents can discover** everything via `--help`

### Evidence-Based Gates
- **Session tracking** prevents stale evidence
- **JSON evidence files** with structured data
- **Automatic validation** before phase transitions
- **Clear error messages** with fix instructions

### Platform Gotchas Documented
- All 20+ platform gotchas catalogued
- Inline warnings at point of use
- Troubleshooting table with causes and fixes
- Pre-deployment checklist for common failures

### Self-Documenting
- Every script has `--help`
- Contextual guidance per phase
- Complete example workflows embedded
- No external documentation required

## Compliance with Specification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| Mini CLAUDE.md ≤50 lines | ✅ | 48 lines |
| Full reference via --help | ✅ | ~300 lines |
| Transition contextual output | ✅ | Phase-specific guidance |
| Evidence session tracking | ✅ | JSON with session_id |
| Gate enforcement | ✅ | Blocks invalid transitions |
| Idempotent init | ✅ | Safe multiple runs |
| Unicode table parsing | ✅ | status.sh handles zcli |
| Frontend detection | ✅ | verify.sh checks |
| Wait mode | ✅ | status.sh --wait |
| Debug mode | ✅ | verify.sh --debug |
| Troubleshooting table | ✅ | workflow.sh --help trouble |
| Complete example | ✅ | workflow.sh --help example |
| All help topics | ✅ | 8 topics implemented |
| Exit codes | ✅ | 0=success, 1/2/3=errors |

## Future Enhancements

Reserved for future implementation:
- `workflow.sh bootstrap` - New project creation
- `workflow.sh bootstrap --help` - Bootstrap reference

## Notes

- All scripts are self-contained and executable
- No sourcing required - call scripts directly
- Works from any directory
- All state in `/tmp/` for easy cleanup
- Session IDs prevent confusion across runs
- Evidence files track which session created them
