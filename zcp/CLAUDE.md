```bash
.zcp/workflow.sh show
```

Do what it outputs. Now.

The flow:
- knows the state (don't check first)
- provides the plan (don't plan first)
- gives exact commands (don't invent)

Context lost? `.zcp/workflow.sh recover`

---

# Zerops Platform

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
│    • Minimal: no jq, psql, redis-cli — those are on ZCP     │
├─────────────────────────────────────────────────────────────┤
│  Managed services (postgresql, valkey...)                   │
│    • NO SSH — access from ZCP only                          │
│    • psql "$db_connectionString"  ← run from ZCP            │
└─────────────────────────────────────────────────────────────┘
```

---

## Source of Truth: /tmp/discovery.json

**Everything is here.** Service IDs, names, URLs, env vars. Check first, don't guess.

```bash
jq '.' /tmp/discovery.json   # Full context
```

## Environment Variables

Shell variables like `$cache_hostname` **do not exist** in ZCP shell.

| Priority | Access pattern |
|----------|----------------|
| 1. Discovery | `jq '.discovered_env_vars.db' /tmp/discovery.json` |
| 2. SSH (if needed) | `ssh appdev 'echo $db_connectionString'` |
| In zerops.yml | `${service_variableName}` (template syntax) |

---

## Dev vs Stage

| | Dev | Stage |
|-|-----|-------|
| Purpose | Debug, iterate | Final validation |
| Server | Start manually | Auto-starts on deploy |
| Fix errors | Here | Never here |

---

## Logs

| Environment | Access |
|-------------|--------|
| Dev | `ssh {dev} "tail -50 /tmp/app.log"` |
| Stage | `zcli service log -S {stage_id} -P $projectId` |

**Why the difference:** In dev, agent starts the server manually with `>> /tmp/app.log 2>&1`. In stage, Zerops runs the unit — no file-based logs, use zcli.

---

## Rules

- zcli uses service IDs, not hostnames — the flow provides correct values
- Deploys run from dev container (source files live there), not from ZCP
- HTTP 200 ≠ working — check response content, not just status
- `run_in_background=true` — **only** for commands that block forever (servers), **not** for builds/deploys (you need the logs)
- **Before deploy:** `cat zerops.yml` and verify deployFiles, envVariables, start command

## Verification

**verify.sh records what YOU verified.** It does not automatically verify anything.

### Flow
1. Run `.zcp/verify.sh {service}` - see available tools
2. Use the tools to verify your work
3. Run `.zcp/verify.sh {service} "what you verified"`

### What To Verify (You Decide)

| You Wrote | Verify With |
|-----------|-------------|
| TypeScript/Bun | `ssh {dev} "bun x tsc --noEmit"` |
| Go code | `ssh {dev} "go build -n ."` |
| HTTP endpoint | `curl` + check response content |
| SSE endpoint | Check `content-type: text/event-stream` |
| Worker/cron | `ps aux \| grep {proc}`, check logs |
| Frontend | `agent-browser errors` must be empty |

### Attestation Examples

```bash
# Good - specific, shows what was actually checked
.zcp/verify.sh bundev "tsc clean, /events returns text/event-stream, tail -30 logs clean"
.zcp/verify.sh goworker "process running (ps aux), processed test message, no panics in log"
.zcp/verify.sh frontend "browser errors empty, screenshot shows correct layout"

# Bad - vague, doesn't show verification
.zcp/verify.sh bundev "looks good"
.zcp/verify.sh bundev "tested"
```

### Helper Commands

| Command | Purpose |
|---------|---------|
| `.zcp/verify.sh {service}` | Show verification tools |
| `.zcp/verify.sh --check {service} {port}` | Check if port listening |
| `.zcp/verify.sh {service} "..."` | Record attestation |

## Gotchas

| Symptom | Cause |
|---------|-------|
| `zcli project services list` | Wrong command → `zcli service list -P $projectId` |
| `jq: command not found` via SSH | Pipe to ZCP: `ssh dev "curl ..." \| jq .` |
| `psql: command not found` via SSH | Run from ZCP, not via ssh |
| Variable is empty | Check `/tmp/discovery.json` first, then SSH |
| HTTP 000 on dev | Server not running |
| `https://https://...` | `zeropsSubdomain` already includes protocol |
| SSH: Connection refused | Container OOM/restarting — check below |
| Gate fails after verify.sh | Empty attestation — include what you actually verified |

---

## Container Crashes / OOM

**If SSH fails repeatedly or process keeps dying**, the container is likely OOMing:

```bash
# Check CONTAINER logs (not app logs!) — shows OOM kills
zcli service log -S {service_id} -P $projectId --limit 50

# Scale up RAM temporarily (30 min)
ssh {dev} "zsc scale ram 4GiB 30m"

# Or scale CPU
ssh {dev} "zsc scale cpu 2 30m"
```

**Don't retry SSH blindly** — if connection is refused, diagnose with `zcli service log` first.

---

## Help

```bash
.zcp/workflow.sh --help
.zcp/workflow.sh --help trouble
```
