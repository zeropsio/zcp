# Zerops Platform Agent System

Guides AI agents operating within the Zerops deployment environment.

## Core Principle

**Dev is for iterating and fixing. Stage is for final validation.**

Agents must test and fix errors on dev before deploying to stage. HTTP 200 doesn't mean the feature works—check response content, logs, and browser console.

## Architecture

```
CLAUDE.md (67 lines)          → Platform fundamentals, always in context
    │
    ▼
workflow.sh --help            → Full reference, contextual phase guidance
verify.sh / status.sh         → Self-documenting tools
```

Knowledge lives in tools via `--help`, surviving context compaction and subagent spawning.

## Files

```
zcp/
├── CLAUDE.md              # Entry point for agents
└── .claude/
    ├── workflow.sh        # Phase orchestration + help system
    ├── verify.sh          # Endpoint testing with evidence
    ├── status.sh          # Deployment status + wait mode
    └── settings.json      # Bash permissions
```

## Workflow

```
INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
```

**DEVELOP phase enforces the iteration loop:**
1. Build & run on dev
2. Test functionality (not just HTTP status)
3. Check for errors (logs, responses, browser console)
4. If errors → Fix → Go to step 1
5. Only when working → deploy to stage

## Usage

```bash
# Start workflow
/var/www/.claude/workflow.sh init      # Enforced (gates)
/var/www/.claude/workflow.sh --quick   # No enforcement

# Get help
/var/www/.claude/workflow.sh --help
/var/www/.claude/workflow.sh --help develop
```

## Key Features

- **Evidence-based gates** - JSON files with session tracking block invalid transitions
- **Self-documenting** - All tools have `--help` with contextual guidance
- **Context-resilient** - Tools always available regardless of context state
- **Platform gotchas** - Troubleshooting table, pre-deployment checklist, inline warnings
