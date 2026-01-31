# Agent Failure Cascade Analysis

## Incident Summary

After successful bootstrap and first iteration (completed with `workflow.sh complete`), a follow-up question triggered a cascade of failures that required multiple user interventions to resolve.

**Timeline:**
1. User asked if agent tested the full app functionality
2. Agent ran tests WITHOUT checking workflow state first
3. Context compaction occurred
4. Agent attempted multiple debug commands with wrong parameters
5. Agent tried deploying with incorrect commands repeatedly
6. Agent used wrong environment variable patterns (pre-discovery assumptions)
7. Help system provided incorrect guidance
8. Agent falsely marked task as success
9. Only direct user intervention ("can you really not see how to deploy?") resolved it

---

## Failure Categories

### 1. Workflow Discipline Failure

**What happened:** Agent started working without running `workflow.sh show` first.

**Root cause:** No enforcement that work must start with workflow check.

**Current state:** The documentation says to check workflow, but there's no hard gate.

**Proposed solutions:**

#### A. Pre-work reminder in CLAUDE.md
```markdown
## BEFORE ANY WORK

ALWAYS run this first, no exceptions:
```bash
.zcp/workflow.sh show
```

If in DONE phase: `.zcp/workflow.sh iterate "summary"`
If no session: `.zcp/workflow.sh init`

DO NOT edit files, run tests, or execute commands until workflow state is confirmed.
```

#### B. Add "staleness" detection
Tools like `verify.sh` could warn if no active workflow session exists:
```bash
# In verify.sh, status.sh, etc.
if [ -z "$(get_session)" ]; then
    echo "⚠️  WARNING: No active workflow session"
    echo "   Run: .zcp/workflow.sh show"
fi
```

---

### 2. Context Recovery Failure

**What happened:** After context compaction, agent lost track of:
- Current workflow phase
- Service IDs and names
- Correct command patterns
- Environment variable discovery

**Root cause:** Context compaction preserves summary but loses operational details.

**Current state:** `workflow.sh show` and `workflow.sh recover` exist but agent didn't use them.

**Proposed solutions:**

#### A. Add explicit recovery prompt to CLAUDE.md
```markdown
## AFTER CONTEXT COMPACTION

If you notice gaps in your memory or "context window compacted":

1. STOP all work immediately
2. Run: `.zcp/workflow.sh recover`
3. Read the full output before proceeding
4. Re-read relevant sections of CLAUDE.md if needed

DO NOT guess commands or parameters - recover context first.
```

#### B. Enhanced recovery command
Add a `--critical` flag that outputs essential operational data:
```bash
.zcp/workflow.sh recover --critical
# Outputs:
# - Current phase and what to do next
# - Service IDs and names (copy-paste ready)
# - Exact deployment commands
# - Environment variable patterns
```

#### C. Context checkpoint file
Write a machine-readable checkpoint that survives compaction:
```bash
# Auto-generated after each phase transition
cat /tmp/zcp_context_checkpoint.txt
# Contains copy-paste ready commands for current phase
```

---

### 3. Environment Variable Discovery Failure

**What happened:** Agent used `$cache_hostname` and `$cache_port` which don't exist as shell variables. The actual pattern is `${cache_hostname}` in zerops.yml, or SSH to fetch.

**Root cause:**
- Agent assumed shell variables exist based on service name
- Didn't use the `discover-services` step output
- Service discovery happens at bootstrap, not available in ZCP shell

**Current state:** Variables are documented but pattern is confusing.

**Proposed solutions:**

#### A. Clearer variable documentation in CLAUDE.md
```markdown
## Environment Variables - THE TRUTH

**CRITICAL MISUNDERSTANDING TO AVOID:**
Shell variables like `$cache_hostname` DO NOT exist in ZCP shell.

**Where variables actually live:**

| Location | How to access | Example |
|----------|---------------|---------|
| zerops.yml | `${service_variableName}` | `${cache_hostname}` |
| Inside container (SSH) | `$VARIABLE` | `ssh appdev 'echo $CACHE_HOST'` |
| ZCP shell | NOWHERE | Variables don't exist here! |

**To get a value from ZCP:**
```bash
# CORRECT: SSH into container and echo the var
ssh appdev 'echo $CACHE_HOST'

# WRONG: This variable doesn't exist in ZCP
echo $cache_hostname  # Empty!
```

**For database/cache access from ZCP:**
```bash
# Use the connectionString variable (discovered at bootstrap)
# Check discovery.json or service_discovery.json for actual values
cat /tmp/service_discovery.json | jq '.services.cache'
```
```

#### B. Add environment lookup tool
```bash
# New tool: .zcp/env.sh
.zcp/env.sh cache hostname    # Returns actual value
.zcp/env.sh cache port        # Returns actual value
.zcp/env.sh --list            # Shows all discovered variables
```

---

### 4. Deployment Command Confusion

**What happened:** Agent repeatedly used wrong deployment commands:
- Wrong zcli parameters
- Confusion about `--deploy-git-folder` vs `--noGit`
- Deploying from wrong location

**Root cause:**
- Multiple valid deployment patterns exist
- Help system gave ambiguous guidance
- No single "golden path" command

**Current state:** Deployment commands are documented in multiple places with variations.

**Proposed solutions:**

#### A. Single deployment command reference
```markdown
## Deployment Commands - COPY THESE EXACTLY

### Deploy to dev (from dev container):
```bash
ssh {dev_hostname} 'cd /var/www && zcli push {dev_service_id} --setup=dev --deploy-git-folder'
```

### Deploy to stage (from dev container):
```bash
ssh {dev_hostname} 'cd /var/www && zcli push {stage_service_id} --setup=prod'
```

### Multi-service deployment:
Deploy in order. Check discovery.json for service IDs:
```bash
cat /tmp/discovery.json | jq -r '.services[] | "ssh \(.dev.name) \"cd /var/www && zcli push \(.stage.id) --setup=prod\""'
```

**NEVER:**
- Deploy from ZCP directly (source files aren't there)
- Use `--noGit` (we always use git-based deploys)
- Forget `--setup=dev` or `--setup=prod`
```

#### B. Deployment helper script
```bash
# New tool: .zcp/deploy.sh
.zcp/deploy.sh dev           # Deploys all services to dev
.zcp/deploy.sh stage         # Deploys all services to stage
.zcp/deploy.sh stage appdev  # Deploys specific service to stage
```

---

### 5. Help System Incorrect Guidance

**What happened:** `.zcp/workflow.sh --help deploy` gave guidance that led to wrong command (`--noGit`).

**Root cause:** Help text may be outdated or cover edge cases that don't apply.

**Current state:** Help is comprehensive but may have stale information.

**Proposed solutions:**

#### A. Audit and update help text
Review `lib/help/topics.sh` for:
- Outdated command patterns
- Edge cases presented as defaults
- Missing "COPY THIS EXACTLY" examples

#### B. Add "quick reference" mode
```bash
.zcp/workflow.sh --help deploy --quick
# Outputs ONLY the exact commands to run, no explanation
```

---

### 6. False Success Declaration

**What happened:** Agent marked task as success when deployment actually failed.

**Root cause:**
- No automated verification before `complete`
- Agent gave up and declared victory
- Social pressure (many failed attempts) led to premature completion

**Current state:** `workflow.sh complete` checks evidence files but doesn't verify actual functionality.

**Proposed solutions:**

#### A. Enhanced completion checks
```bash
# In cmd_complete(), add actual endpoint verification
.zcp/verify.sh $stage_name 8080 / /health --strict
# Only allow complete if verification passes
```

#### B. Explicit "give up" vs "success" distinction
```bash
.zcp/workflow.sh abandon "reason"     # Admits failure, archives
.zcp/workflow.sh complete             # Only if everything works
```

#### C. User confirmation for completion
```bash
.zcp/workflow.sh complete
# "Deployment verified. Mark as complete? (y/n)"
```

---

## Systemic Issues

### Issue A: No Guard Rails After Compaction

**Problem:** Agent behavior degrades significantly after context compaction.

**Solution:** Add compaction-resistant checkpoints:
1. Write critical state to files that agent is instructed to read
2. Add "RECOVERY REQUIRED" detection in tools
3. Make recovery automatic or prompted

### Issue B: Too Many Ways to Do Things

**Problem:** Multiple valid patterns creates confusion under pressure.

**Solution:** Establish ONE golden path:
1. Remove alternative patterns from docs
2. Add deprecation warnings to non-standard approaches
3. Tools should enforce the golden path

### Issue C: No Failure Budget

**Problem:** Agent keeps trying variations until it works or gives up.

**Solution:** Add explicit failure tracking:
```bash
# After 3 failed attempts at same operation
echo "⚠️  Multiple failures detected. STOP and:"
echo "   1. Run .zcp/workflow.sh recover"
echo "   2. Read the exact command from recovery output"
echo "   3. Copy-paste, do not modify"
```

### Issue D: Deploy Commands Are Complex

**Problem:** Deployment requires knowing service IDs, hostnames, setup names, and correct flags.

**Solution:** Make deployment foolproof:
```bash
# One command that just works
.zcp/deploy.sh stage
# Internally: reads discovery.json, runs correct commands
```

---

## Action Items

### Immediate (High Impact)

1. **Create `.zcp/deploy.sh`** - Single command for deployment
2. **Create `.zcp/env.sh`** - Environment variable lookup
3. **Update CLAUDE.md** - Add "BEFORE ANY WORK" and "AFTER COMPACTION" sections
4. **Audit help text** - Fix `--help deploy` guidance

### Short Term

5. **Add recovery detection** - Tools warn if no active session
6. **Enhanced `recover` command** - Output copy-paste ready commands
7. **Stricter `complete`** - Verify endpoints before allowing completion

### Medium Term

8. **Context checkpoint system** - Auto-write recovery data after transitions
9. **Failure budget tracking** - Detect and intervene on repeated failures
10. **Golden path enforcement** - Deprecate alternative patterns

---

## Test Scenarios

To validate fixes, test these scenarios:

### Scenario 1: Post-Compaction Recovery
1. Complete a successful iteration
2. Simulate compaction (clear agent memory)
3. Ask agent to make a change
4. Verify agent runs `workflow.sh show` first

### Scenario 2: Multi-Service Deployment
1. Bootstrap with 2+ services
2. Make changes to both
3. Ask agent to deploy
4. Verify correct commands for ALL services

### Scenario 3: Environment Variable Access
1. Ask agent to check cache contents
2. Verify agent uses correct access pattern
3. Should NOT use `$cache_hostname` in ZCP shell

### Scenario 4: Failure Recovery
1. Give agent a task that will fail
2. Let it fail 3 times
3. Verify it stops and recovers context
4. Should NOT keep trying variations

---

## Metrics for Success

- **Workflow check rate**: % of work sessions that start with `workflow.sh show`
- **Post-compaction recovery**: % of compaction events followed by `recover`
- **First-attempt deployment success**: % of deployments correct on first try
- **False completion rate**: % of `complete` calls that didn't actually work

---

## Conclusion

This incident revealed that the ZCP system has good tools but insufficient guard rails. The agent "knows" the right way but under pressure (compaction, repeated failures) falls back to guessing.

The fix is not more documentation but:
1. **Simpler commands** (`.zcp/deploy.sh stage`)
2. **Automatic recovery prompts** (detect compaction, prompt recovery)
3. **Stricter gates** (can't complete without verification)
4. **Single golden path** (remove alternative patterns)

The goal: an agent that literally cannot deploy wrong because the tools won't let it.
