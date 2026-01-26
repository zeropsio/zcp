# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## FIRST STEP — ALWAYS

⛔ **For EVERY new user request, run this FIRST:**

```bash
.zcp/workflow.sh show
```

Then follow the NEXT STEPS in its output. The show command reveals:
- No workflow → run `init` (or `bootstrap` if no services)
- DONE state → run `iterate` to start new iteration
- Active phase → continue from current phase
- What's blocking → specific guidance

**DO NOT skip this step.** DO NOT pre-plan before checking workflow state.

## Workflow Commands Reference

| Situation | Command |
|-----------|---------|
| Start new work (deploy to stage) | `.zcp/workflow.sh init` |
| Start new work (dev only) | `.zcp/workflow.sh init --dev-only` |
| Urgent hotfix | `.zcp/workflow.sh init --hotfix` |
| Just exploring | `.zcp/workflow.sh --quick` |
| No services yet | `.zcp/workflow.sh bootstrap --runtime go --services postgresql` |
| Previous work done, new task | `.zcp/workflow.sh iterate "summary"` |

## Bootstrap (New Projects)

When no services exist, bootstrap creates infrastructure:

```bash
# Start bootstrap - creates plan, returns immediately
.zcp/workflow.sh bootstrap --runtime go --services postgresql
```

Then run steps individually for visibility and error handling:

```bash
# Step 2: Fetch runtime patterns
.zcp/bootstrap.sh step recipe-search

# Step 3: Generate import.yml
.zcp/bootstrap.sh step generate-import

# Step 4: Import services (sends request)
.zcp/bootstrap.sh step import-services

# Step 5: Wait for services (POLL this - may take 1-5 min)
while true; do
    result=$(.zcp/bootstrap.sh step wait-services)
    status=$(echo "$result" | jq -r '.status')
    if [ "$status" = "complete" ]; then break; fi
    if [ "$status" = "failed" ]; then echo "ERROR"; exit 1; fi
    echo "Waiting... $(echo "$result" | jq -r '.message')"
    sleep 10
done

# Step 6: Mount dev filesystem
.zcp/bootstrap.sh step mount-dev appdev

# Step 7: Create handoff for code generation
.zcp/bootstrap.sh step finalize

# Step 8: Get subagent instructions
result=$(.zcp/bootstrap.sh step spawn-subagents)
# Parse the instructions from result.data.instructions[]

# Step 9: Spawn subagents for code generation
# For EACH service pair in the instructions, spawn a subagent:
# Use Task tool with subagent_type=general-purpose
# Subagent receives: handoff JSON + subagent_prompt from instructions
# Subagent tasks:
#   1. Create zerops.yml (use recipe_patterns for correct structure)
#   2. Deploy config to dev
#   3. Enable subdomain on dev
#   4. Generate status page code (minimal, demonstrates managed service connections)
#   5. Test dev endpoints
#   6. Deploy to stage
#   7. Enable subdomain on stage
#   8. Test stage endpoints
#   9. Mark complete: set_service_state {hostname} phase complete

# Step 10: Wait for all subagents, finalize
while true; do
    result=$(.zcp/bootstrap.sh step aggregate-results)
    status=$(echo "$result" | jq -r '.status')
    if [ "$status" = "complete" ]; then break; fi
    if [ "$status" = "failed" ]; then echo "ERROR"; exit 1; fi
    echo "Waiting... $(echo "$result" | jq -r '.message')"
    sleep 10
done
# Bootstrap complete - workflow is now in DEVELOP phase
```

Each step returns JSON:
```json
{
  "status": "complete|in_progress|failed",
  "step": "step-name",
  "checkpoint": "current-checkpoint",
  "data": { ... },
  "next": "next-step-name",
  "message": "Human readable"
}
```

### Error Handling

If a step fails:
1. Read the error from `.data.error`
2. Check `.data.recovery_options[]` for suggestions
3. Fix the issue
4. Re-run the failed step (state tracks completion)

### Check Progress

```bash
.zcp/bootstrap.sh status      # Full status JSON
.zcp/bootstrap.sh resume      # Returns next step to run
```

**READ output completely. FOLLOW the rules it shows.** The script guides each phase and enforces gates.

⛔ **CRITICAL: Workflow commands tell you what to do next.**
Each workflow command outputs specific guidance. Follow it — don't skip steps.
You can track WHAT the user wants, but let the workflow tell you HOW.
The workflow detects current state and adapts — your pre-made steps cannot.

## Lost Context?

Same as above — run `show`. For more detail:
```bash
.zcp/workflow.sh show --full    # Extended context (intent, notes, last error)
.zcp/workflow.sh recover        # Complete recovery: show + guidance + rules
```

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

## zcli Authentication

If zcli commands fail with "unauthenticated user", run:
```bash
zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
```

## Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| zcli "unexpected EOF" | Transient backend issue (often false positive) | Check if operation succeeded anyway: `zcli service list -P $projectId` |
| zcli "unauthenticated user" | Not logged in | Run zcli login with region (see above) |
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
- `bootstrap` — Create services + scaffolding for new projects
- `import-validation` — Validate import.yml before importing
