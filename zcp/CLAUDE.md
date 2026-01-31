# Zerops Platform

```bash
.zcp/workflow.sh show
```

**The flow is the authority.** Do what it says. Nothing else.

When the flow shows options, pick one and run it. Don't verify which applies — just try. The command will error if wrong.

When the flow shows a pattern, adapt it to your request:
- Flow: `--runtime go --services postgresql`
- Request: "go + bun + postgres + valkey + nats"
- Run: `--runtime go,bun --services postgresql,valkey,nats`

**Never** run commands to "check first" (zcli service list, ls, glob). The flow already knows the state.

After context compaction: `.zcp/workflow.sh recover`

---

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

## Rules

- `zcli {cmd} --help` before using — syntax varies, uses IDs not names
- Deploy from dev container — `ssh dev "zcli push..."` — source files are there
- HTTP 200 ≠ working — check response content, not just status
- `run_in_background=true` — **only** for commands that block forever (servers), **not** for builds/deploys (you need the logs)

## Gotchas

| Symptom | Cause |
|---------|-------|
| `psql: command not found` via SSH | Run from ZCP, not via ssh |
| Variable is empty | Use `.zcp/env.sh` or SSH to fetch |
| HTTP 000 on dev | Server not running |
| `https://https://...` | `zeropsSubdomain` already includes protocol |

---

## Help

```bash
.zcp/workflow.sh --help
.zcp/workflow.sh --help trouble
```
