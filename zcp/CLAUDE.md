# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**
**Workflows iterate. Run `show` anytime — it tells you what to do next.**

⛔ **CRITICAL: Workflow commands tell you what to do next.**
Each workflow command outputs specific guidance. Follow it — don't skip steps.
You can track WHAT the user wants, but let the workflow tell you HOW.
The workflow detects current state and adapts — your pre-made steps cannot.

## Start Here — RUN ONE

| Will you write/change code? | Command | Examples |
|-----------------------------|---------|----------|
| **Yes, deploy to stage** | `.zcp/workflow.sh init` | Build feature, fix bug, any code change |
| **Yes, dev only** | `.zcp/workflow.sh init --dev-only` | Prototype, experiment, not ready for stage |
| **Yes, urgent hotfix** | `.zcp/workflow.sh init --hotfix` | Production broken, skip dev verification |
| **No, just looking** | `.zcp/workflow.sh --quick` | Read logs, investigate, understand codebase |

**Run one. READ its output completely. FOLLOW the rules it shows.** The script guides each phase and enforces gates.

⚠️ **DO NOT pre-plan tasks before running workflow commands.** The workflow output tells you what to do next. Creating your own todo list and ignoring the workflow guidance will cause you to miss critical steps and fail.

## Lost Context? Run This

If resuming work, entering mid-session, or unsure of current state:
```bash
.zcp/workflow.sh show           # State, evidence, next steps
.zcp/workflow.sh show --full    # Extended context (intent, notes, last error)
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
| Check status + builds | `zcli service list -P $projectId` (shows services AND running processes) |

**Service types:**
- **Runtime** (go, nodejs, php, python, etc.) — SSH ✓, SSHFS ✓, run your code
- **Managed** (postgresql, valkey, nats, etc.) — NO SSH, access via client tools from ZCP

⚠️ **Runtime containers are MINIMAL** — they run your app code only. They do NOT have database tools (`psql`, `mysql`, `redis-cli`). Only ZCP has these tools pre-installed.

```bash
# Runtime: SSH in to build/run YOUR CODE
ssh appdev "go build && ./app"

# Database queries: Run from ZCP directly (NOT via ssh!)
psql "$db_connectionString"

# ❌ WRONG - runtime containers don't have psql
# ssh appdev "psql ..."   # Will fail: "psql: command not found"
```

## Variables

```bash
$projectId                  # Project ID (ZCP has this)
$ZEROPS_ZCP_API_KEY      # Auth key for zcli
${service_VAR}              # Other service's var: prefix with hostname
ssh svc 'echo $VAR'         # Inside service: no prefix
```
Full patterns: `.zcp/workflow.sh --help vars`

## Security: Environment Variables

⛔ **NEVER expose secrets in output or commands**

| ❌ WRONG | ✅ RIGHT |
|----------|----------|
| `ssh svc 'env \| grep db'` | `ssh svc 'echo $db_connectionString'` |
| `ssh svc 'printenv'` | Fetch specific var only |
| `psql "postgres://user:PASSWORD@..."` | `psql "$(env_from svc db_connectionString)"` |
| Hardcoding passwords in commands | Pass via substitution |

**Rules:**
1. NEVER use `env`, `printenv`, or `env | grep` via SSH - dumps secrets to output
2. NEVER hardcode credentials in commands - they appear in logs/history
3. ALWAYS fetch specific variables: `ssh svc 'echo $VAR_NAME'`
4. ALWAYS pass secrets via substitution: `cmd "$(ssh svc 'echo $SECRET')"`

**Helper function** (use this):
```bash
source .zcp/lib/env.sh
psql "$(env_from appdev db_connectionString)" -c "SELECT 1"
```

**Why this matters:**
- Command output is displayed/logged
- Hardcoded secrets appear in shell history
- `env` dumps ALL secrets, not just the one you need

## Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | zeropsSubdomain is full URL | Don't prepend protocol |
| `psql: command not found` (via SSH) | Runtime containers don't have DB tools | Run `psql` from ZCP directly, not via ssh |
| SSH connection refused | Managed service (db, cache) | Use client tools: `psql`, `redis-cli` from ZCP |
| `cd /var/www/appdev: No such file` | SSH lands in `/var/www` directly | Don't include hostname in path inside container |
| zcli wrong syntax | Guessed instead of checked | **ALWAYS** run `zcli {cmd} --help` first |
| zcli "service not found" | Used name instead of ID | zcli needs IDs: `-S {service_id}` not `servicename` |
| zcli no results | Missing project flag | Use `zcli service list -P $projectId` |
| Files missing on stage | Not in deployFiles | Update zerops.yaml, redeploy |
| Services in READY_TO_DEPLOY | Missing buildFromGit/startWithoutCode | Fix import.yml, re-import |
| SSH hangs forever | Foreground process | `run_in_background=true` |
| Variable empty | Wrong prefix | Use `${hostname}_VAR` |
| Can't transition phase | Missing evidence | `.zcp/workflow.sh show` |
| `/var/www/{dev}` empty after import | No SSHFS mount (dev only) | `.zcp/mount.sh {dev}` |

## Critical Rules (memorize these)

| Rule | Pattern |
|------|---------|
| zcli syntax | NEVER guess — run `zcli {cmd} --help` FIRST, uses IDs not names |
| Kill orphan processes | `ssh {svc} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'` |
| Long-running commands | Set `run_in_background=true` in Bash tool |
| HTTP 200 ≠ working | Check response content, logs, browser console |
| Deploy from | Dev container (`ssh {dev} "zcli push..."`), NOT from ZCP |
| deployFiles | Must include ALL artifacts — check before every deploy |
| zeropsSubdomain | Already full URL — don't prepend `https://` |

## Reference

```bash
.zcp/workflow.sh show           # Current phase, what's blocking
.zcp/workflow.sh show --full    # Status + extended context (intent, notes, last error)
.zcp/workflow.sh recover        # Complete context recovery
.zcp/workflow.sh --help         # Full platform reference
.zcp/validate-import.sh <file>  # Validate import.yml before importing
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
