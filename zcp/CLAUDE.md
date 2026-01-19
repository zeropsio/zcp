# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## Start Here — RUN ONE

| Will you write/change code? | Command | Examples |
|-----------------------------|---------|----------|
| **Yes, deploy to stage** | `.zcp/workflow.sh init` | Build feature, fix bug, any code change |
| **Yes, dev only** | `.zcp/workflow.sh init --dev-only` | Prototype, experiment, not ready for stage |
| **Yes, urgent hotfix** | `.zcp/workflow.sh init --hotfix` | Production broken, skip dev verification |
| **No, just looking** | `.zcp/workflow.sh --quick` | Read logs, investigate, understand codebase |

**Run one. READ its output completely. FOLLOW the rules it shows.** The script guides each phase and enforces gates.

## Lost Context? Run This

If resuming work, entering mid-session, or unsure of current state:
```bash
.zcp/workflow.sh show           # State, evidence, next steps
.zcp/workflow.sh show --guidance # Full context recovery (state + phase guidance)
.zcp/workflow.sh recover        # Complete recovery: show + guidance + rules
```
Then follow the NEXT STEPS section in the output.

## Context

**Zerops** is a PaaS. Projects contain services (containers) on a shared private network.

**ZCP** (Zerops Control Plane) is your workspace — a privileged container with:
- SSHFS mounts to runtime service filesystems
- SSH access to runtime containers only (NOT managed services)
- Direct network access to all services
- Tooling: `jq`, `yq`, `psql`, `mysql`, `redis-cli`, `zcli`, `agent-browser`

## Your Position

You are on ZCP, not inside the app containers.

| To... | Do |
|-------|----|
| Edit files | `/var/www/{runtime}/...` (SSHFS mount on ZCP) |
| Run commands | `ssh {runtime} "command"` (lands in `/var/www`) |
| Reach services | `http://{service}:{port}` |
| Test frontend | `agent-browser open "$URL"` |

**Service types:**
- **Runtime** (go, nodejs, php, python, etc.) — SSH ✓, SSHFS ✓, run your code
- **Managed** (postgresql, valkey, nats, etc.) — NO SSH, access via client tools from ZCP

```bash
# Runtime: SSH in to build/run
ssh appdev "go build && ./app"

# Managed: Use client tools from ZCP directly
PGPASSWORD=$db_password psql -h db -U $db_user -d $db_database
```

## Variables

```bash
$projectId                  # Project ID (ZCP has this)
$ZEROPS_ZAGENT_API_KEY      # Auth key for zcli
${service_VAR}              # Other service's var: prefix with hostname
ssh svc 'echo $VAR'         # Inside service: no prefix
```

## Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | zeropsSubdomain is full URL | Don't prepend protocol |
| SSH connection refused | Managed service (db, cache) | Use client tools: `psql`, `redis-cli` from ZCP |
| `cd /var/www/appdev: No such file` | SSH lands in `/var/www` directly | Don't include hostname in path inside container |
| zcli unknown flag | zcli has custom syntax | Check `zcli {cmd} --help` |
| zcli no results | Missing project flag | Use `zcli service list -P $projectId` |
| Files missing on stage | Not in deployFiles | Update zerops.yaml, redeploy |
| SSH hangs forever | Foreground process | `run_in_background=true` |
| Variable empty | Wrong prefix | Use `${hostname}_VAR` |
| Can't transition phase | Missing evidence | `.zcp/workflow.sh show` |

## Critical Rules (memorize these)

| Rule | Pattern |
|------|---------|
| Kill orphan processes | `ssh {svc} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'` |
| Long-running commands | Set `run_in_background=true` in Bash tool |
| HTTP 200 ≠ working | Check response content, logs, browser console |
| Deploy from | Dev container (`ssh {dev} "zcli push..."`), NOT from ZCP |
| deployFiles | Must include ALL artifacts — check before every deploy |
| zeropsSubdomain | Already full URL — don't prepend `https://` |

## Evidence Files

| File | Purpose |
|------|---------|
| `/tmp/claude_session` | Session ID |
| `/tmp/claude_phase` | Current phase |
| `/tmp/discovery.json` | Dev/stage service mapping |
| `/tmp/dev_verify.json` | Dev verification results |
| `/tmp/stage_verify.json` | Stage verification results |
| `/tmp/deploy_evidence.json` | Deployment completion proof |

## Reference

```bash
.zcp/workflow.sh show           # Current phase, what's blocking
.zcp/workflow.sh show --guidance # Status + full phase guidance
.zcp/workflow.sh recover        # Complete context recovery
.zcp/workflow.sh state          # One-line state summary
.zcp/workflow.sh --help         # Full platform reference
```

Help topics (use `--help {topic}`):
- `cheatsheet` — Quick reference (commands, patterns, rules)
- `discover` — Find services, record IDs
- `develop` — Build, test, iterate on dev
- `deploy` — Push to stage, deployFiles checklist
- `verify` — Test stage, browser checks
- `vars` — Environment variable patterns
- `services` — Service naming, hostnames vs types
- `trouble` — Common errors and fixes
- `gates` — Phase transition requirements
- `extend` — Add services mid-project
- `bootstrap` — Create new project from scratch
