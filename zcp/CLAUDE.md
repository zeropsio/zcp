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
│    • NO database tools installed                            │
├─────────────────────────────────────────────────────────────┤
│  Managed services (postgresql, valkey...)                   │
│    • NO SSH — access from ZCP only                          │
│    • psql "$db_connectionString"  ← run from ZCP            │
└─────────────────────────────────────────────────────────────┘
```

---

## Environment Variables

Shell variables like `$cache_hostname` **do not exist** in ZCP shell.

| Location | Access pattern |
|----------|----------------|
| Inside container | `ssh appdev 'echo $PORT'` |
| From ZCP | `.zcp/env.sh {service} {var}` |
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

## Gotchas

| Symptom | Cause |
|---------|-------|
| `zcli project services list` | Wrong command → `zcli service list -P $projectId` |
| `psql: command not found` via SSH | Run from ZCP, not via ssh |
| Variable is empty | Use `.zcp/env.sh` or SSH to fetch |
| HTTP 000 on dev | Server not running |
| `https://https://...` | `zeropsSubdomain` already includes protocol |
| SSH: Connection refused | Container OOM/restarting — check below |

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
