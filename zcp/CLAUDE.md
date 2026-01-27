# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## FIRST — ALWAYS

```bash
.zcp/workflow.sh show
```

Follow its output. It tells you exactly what to do next.
**DO NOT pre-plan.** The workflow detects state and adapts.

## Quick Reference

| Situation | Command |
|-----------|---------|
| Start new work | `.zcp/workflow.sh init` |
| Dev only (no deploy) | `.zcp/workflow.sh init --dev-only` |
| Urgent hotfix | `.zcp/workflow.sh init --hotfix` |
| Just exploring | `.zcp/workflow.sh --quick` |
| No services yet | `.zcp/workflow.sh bootstrap --runtime {rt} --services {svc}` |
| Continue after DONE | `.zcp/workflow.sh iterate "summary"` |

## Bootstrap Flow (No Services Yet)

Follow `next` field in each step's JSON output. Run `.zcp/workflow.sh show` anytime for guidance.

```bash
.zcp/workflow.sh bootstrap --runtime go --services postgresql
.zcp/bootstrap.sh step recipe-search      # → generate-import
.zcp/bootstrap.sh step generate-import    # → import-services
.zcp/bootstrap.sh step import-services    # → wait-services
.zcp/bootstrap.sh step wait-services      # → mount-dev
.zcp/bootstrap.sh step mount-dev          # → finalize
.zcp/bootstrap.sh step finalize           # → spawn-subagents
.zcp/bootstrap.sh step spawn-subagents    # → (spawn via Task tool)
.zcp/bootstrap.sh step aggregate-results  # → done
```

### spawn-subagents (CRITICAL)

Outputs JSON with `data.instructions[]`. Each has `subagent_prompt` - complete context for code generation.

**Spawn via Task tool:**
```
For each instruction:
  Task tool: subagent_type="general-purpose", prompt=instruction.subagent_prompt
```

Launch all in parallel (single message, multiple Task calls).

## Your Position

You're on **ZCP** (control plane), not inside app containers.

```
┌─────────────────────────────────────────────────────────────┐
│  ZCP (you are here)                                         │
│    • SSHFS mounts: /var/www/{runtime}/                      │
│    • Tools: zcli, psql, mysql, redis-cli, agent-browser     │
│    • Network access to all services                         │
├─────────────────────────────────────────────────────────────┤
│  Runtime services (go, nodejs, php...)                      │
│    • SSH ✓  →  ssh appdev "go build"                        │
│    • Your code runs here                                    │
│    • NO database tools installed                            │
├─────────────────────────────────────────────────────────────┤
│  Managed services (postgresql, valkey...)                   │
│    • NO SSH — access from ZCP only                          │
│    • psql "$db_connectionString"  ← run from ZCP            │
└─────────────────────────────────────────────────────────────┘
```

| Action | How |
|--------|-----|
| Edit files | `/var/www/{runtime}/...` (direct on ZCP) |
| Run build/app | `ssh {runtime} "command"` |
| Query database | `psql "$db_connectionString"` (from ZCP, NOT via ssh) |
| Check services | `zcli service list -P $projectId` |
| Test in browser | `agent-browser open "$URL"` |

## Variables

```bash
$projectId                  # Available on ZCP
${service}_VAR              # Other service's var (from ZCP)
ssh svc 'echo $VAR'         # Inside service (no prefix)
```

## Security: Environment Variables

⛔ **NEVER dump all env vars**

```bash
# ❌ WRONG — leaks secrets
ssh svc 'env'
ssh svc 'printenv'

# ✅ RIGHT — fetch specific var
ssh svc 'echo $db_connectionString'

# ✅ Helper function (preferred)
source .zcp/lib/env.sh
psql "$(env_from appdev db_connectionString)"
```

## Critical Rules

| Rule | Why |
|------|-----|
| Run `show` first | Workflow tells you what's next |
| `zcli {cmd} --help` before using | Syntax varies, uses IDs not names |
| Deploy from dev container | `ssh dev "zcli push..."` — source files are there |
| Long-running SSH commands | Set `run_in_background=true` in Bash tool, or SSH hangs |
| HTTP 200 ≠ working | Check response content, logs, browser |

## Key Gotchas

| Symptom | Fix |
|---------|-----|
| `psql: command not found` via SSH | Run `psql` from ZCP directly, not via ssh |
| `cd /var/www/appdev: No such file` | SSH lands in `/var/www` — no hostname prefix inside container |
| `https://https://...` | `zeropsSubdomain` is already full URL — don't prepend protocol |
| SSH hangs forever | Set `run_in_background=true` in Bash tool |
| zcli "unauthenticated user" | See zcli auth below |

## zcli Authentication

If zcli fails with "unauthenticated user":
```bash
zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
```

## Help

```bash
.zcp/workflow.sh --help              # Full reference
.zcp/workflow.sh --help cheatsheet   # Quick patterns
.zcp/workflow.sh --help trouble      # All gotchas
.zcp/workflow.sh --help bootstrap    # New project setup
```
