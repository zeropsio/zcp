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

⛔ **Never `ssh svc 'env'` or `printenv`** — leaks secrets. Use helpers:

```bash
source .zcp/lib/env.sh
psql "$(env_from appdev db_connectionString)"    # Safe var fetch
curl "$(get_service_url appstage)/health"        # Get service URL
```

## Critical Rules

| Rule | Why |
|------|-----|
| Run `show` first | Workflow tells you what's next |
| `zcli {cmd} --help` before using | Syntax varies, uses IDs not names |
| Deploy from dev container | `ssh dev "zcli push..."` — source files are there |
| HTTP 200 ≠ working | Check response content, logs, browser |

**`run_in_background=true`** — ONLY for commands that **block indefinitely**:
```bash
# ✅ YES - these block forever (server processes)
ssh dev "./app"                    # Server runs indefinitely
ssh dev "tail -f /tmp/app.log"     # Follows forever

# ❌ NO - these complete on their own (run synchronously to see logs!)
ssh dev "zcli push ..."            # Streams build logs, completes
ssh dev "go build ..."             # Completes when compiled
ssh dev "npm install"              # Completes when deps installed
```

## Dev vs Stage

| | Dev | Stage |
|-|-----|-------|
| Start command | `zsc noop --silent` (nothing runs) | Real command (auto-starts) |
| After deploy | Start server manually | Zerops starts it |
| Purpose | Debug, iterate | Final validation |

```bash
# Start dev server
ssh {dev} "cd /var/www && nohup ./app > /tmp/app.log 2>&1 &"

# Wait for it (first startup may take 60-120s for deps)
.zcp/wait-for-server.sh {hostname} {port} 120
```

## Key Gotchas

| Symptom | Fix |
|---------|-----|
| `psql: command not found` via SSH | Run from ZCP directly, not via ssh |
| SSH hangs on server start | `run_in_background=true` (server blocks forever) |
| `https://https://...` | `zeropsSubdomain` is already full URL |
| HTTP 000 on dev | Server not running — start manually |
| zcli "unauthenticated" | See zcli auth below |

More gotchas: `.zcp/workflow.sh --help trouble`

## zcli Authentication

Tokens expire unpredictably. Re-authenticate when you see "unauthenticated":

```bash
zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
```

**For SSH deploy commands:** Combine auth + push in single call — tokens don't persist:
```bash
ssh {dev} 'cd /var/www && zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "$ZEROPS_ZCP_API_KEY" && zcli push {id} --setup={setup} --deploy-git-folder'
```

## Bootstrap (No Services Yet)

```bash
.zcp/workflow.sh bootstrap --runtime go --services postgresql
.zcp/workflow.sh bootstrap --runtime go,bun --prefix app,bun --services postgresql,valkey
```

Follow `next` field in each step's output. At `spawn-subagents` step:
- Output contains `data.instructions[]` with `subagent_prompt` for each runtime
- Launch via Task tool: `subagent_type="general-purpose"`, `prompt=instruction.subagent_prompt`
- Launch all in parallel (single message, multiple Task calls)

## Help

```bash
.zcp/workflow.sh --help              # Full reference
.zcp/workflow.sh --help cheatsheet   # Quick patterns
.zcp/workflow.sh --help trouble      # All gotchas
.zcp/workflow.sh --help bootstrap    # New project setup
.zcp/workflow.sh --help vars         # Variable patterns
.zcp/workflow.sh --help services     # Service naming
```
