# Subagent Issues Analysis

Analysis of issues with **spawned subagents** during ZCP bootstrap workflows.

## Context: Subagent Architecture

During bootstrap, the main agent spawns subagents via the Task tool:

```
spawn-subagents step
       │
       ▼
Task tool spawns subagents (one per service pair)
       │
       ▼
Each subagent receives:
  - Task prompt with instructions
  - bootstrap_handoff.json data
  - NO access to CLAUDE.md
  - NO access to conversation history
  - NO access to ZCP architecture knowledge
```

**The issues below stem from subagents lacking context that the main agent has.**

---

## Issue 1: Deployment Status vs Application Running

### Observed Behavior

```bash
# Agent ran this and saw "Deployment complete!"
.zcp/status.sh --wait godev
# Output: [0/300s] ✅ Deployment complete!

# But then had to manually start the app
ssh godev "nohup go run . > /tmp/app.log 2>&1 &"
```

### Root Cause

The agent conflated two different concepts:

| Concept | What It Means | How to Check |
|---------|---------------|--------------|
| **Deployment complete** | Container built, service ACTIVE in Zerops | `status.sh --wait` |
| **Application running** | Process serving HTTP requests inside container | `verify.sh` or `curl` |

### Why This Happens

Dev services use `zsc noop --silent` as their start command (visible in logs):

```
━━━━  🙏 zsc noop --silent  ━━━━
```

This is "manual control mode" - the container runs but no application process starts automatically. This is intentional for dev workflows where you want to iterate manually.

### The Gap

`status.sh --wait` reports success when:
- ✅ Container is built
- ✅ Service status is ACTIVE
- ❌ Does NOT check if application is actually responding

### Recommended Fix

The fix must be in the **subagent prompt or handoff data**, since subagents don't read CLAUDE.md.

**Option A: Add to spawn-subagents instructions**

In `.zcp/lib/bootstrap/steps/spawn-subagents.sh`, add explicit guidance:

```markdown
CRITICAL: Dev services use `zsc noop --silent` (manual start mode).
After deployment completes:
1. The container is ACTIVE but NO application is running
2. You MUST start the application via SSH before testing
3. `status.sh --wait` only confirms deployment, not app health

Example workflow:
  ssh {dev} "nohup ./app > /tmp/app.log 2>&1 &"  # Start app
  .zcp/verify.sh {dev} 8080 /                     # Then verify
```

**Option B: Add to bootstrap_handoff.json**

Include a `critical_notes` field in the handoff data:

```json
{
  "dev_service": "godev",
  "critical_notes": [
    "Dev services use manual start - app won't run automatically",
    "Start app via SSH before running verify.sh"
  ]
}
```

**Option C: Enhance status.sh output (defense in depth)**

Even though subagents should know this, add a reminder:
```
✅ Deployment complete!
⚠️  Dev services use manual start. Start your app via SSH before verifying.
```

---

## Issue 2: Tool Availability (jq inside containers)

### Observed Behavior

```bash
# Agent tried to run jq inside the service container
ssh bundev "curl -s localhost:8080/health | jq ."
# Output: /bin/bash: line 1: jq: command not found

# Correct approach: pipe to jq on ZCP
ssh bundev "curl -s localhost:8080/health" | jq .
# Output: { "status": "ok" }
```

### Root Cause

The agent didn't understand the tool availability matrix:

| Tool | ZCP Container | Runtime Services (via SSH) |
|------|---------------|---------------------------|
| `jq` | ✅ Available | ❌ Not installed |
| `yq` | ✅ Available | ❌ Not installed |
| `curl` | ✅ Available | ✅ Available |
| `psql` | ✅ Available | ❌ Use from ZCP |
| `redis-cli` | ✅ Available | ❌ Use from ZCP |

### Why This Matters

Runtime service containers are minimal - they contain only what's needed to run the application. Developer tools like `jq` are available on the ZCP container, which is the orchestration layer.

### Recommended Fix

The fix must be in the **subagent prompt**, since subagents don't have CLAUDE.md context.

**Option A: Add to spawn-subagents instructions**

In `.zcp/lib/bootstrap/steps/spawn-subagents.sh`, add tool guidance:

```markdown
TOOL AVAILABILITY:
- jq, yq, psql, redis-cli are on ZCP only, NOT in service containers
- To parse JSON from a service:
  WRONG:  ssh {dev} "curl ... | jq ."     # jq not found
  RIGHT:  ssh {dev} "curl ..." | jq .     # pipe to ZCP's jq

- Use verify.sh instead of manual curl - it handles JSON correctly
```

**Option B: Add to bootstrap_handoff.json**

```json
{
  "dev_service": "godev",
  "tool_notes": [
    "jq/yq are on ZCP only - pipe SSH output to them",
    "Use: ssh {dev} 'curl ...' | jq .  (NOT inside SSH)"
  ]
}
```

**Option C: Recommend verify.sh in subagent prompt**

```markdown
For endpoint testing, prefer:
  .zcp/verify.sh {service} {port} /endpoint

This handles JSON parsing correctly and creates evidence files.
Avoid manual curl+jq patterns.
```

---

## Summary

| Issue | Core Problem | Fix Location |
|-------|--------------|--------------|
| Deployment vs Running | Subagent thinks ACTIVE = app responding | spawn-subagents.sh prompt |
| Tool availability | Subagent runs ZCP tools inside containers | spawn-subagents.sh prompt |

**Root Cause: Subagent context gap**

The main agent has full ZCP architecture knowledge from CLAUDE.md. Spawned subagents do not - they only receive:
- The Task tool prompt
- bootstrap_handoff.json data

**Solution: Enrich subagent handoff**

The `spawn-subagents` step must include critical operational knowledge:
1. Dev services use manual start (`zsc noop --silent`)
2. Tool availability matrix (jq/yq on ZCP only)
3. Correct patterns for SSH + tool piping

This knowledge transfer is currently missing, causing subagents to make incorrect assumptions about the environment they're operating in.
